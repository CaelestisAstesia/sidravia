//go:build windows

package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
	"sidravia/internal/release"
	"sidravia/internal/update/setup/internal/innopeer"
	"sidravia/internal/update/setup/internal/receiptstate"
	"sidravia/internal/update/transaction"
)

// Begin validates all bytes before launching Inno. Inno must authenticate its
// short trusted source CTL and wait for this updater and its caller to exit
// before stopping the application or publishing files.
func Begin(ctx context.Context, layout productlayout.Layout, version, build string, old release.VerifiedRelease, oldAsset release.Candidate, index []byte, next release.VerifiedRelease, c release.Candidate, archive string) (transaction.Record, error) {
	if layout.Mode != productlayout.ModeInstalled {
		return transaction.Record{}, ErrInvalid
	}
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return transaction.Record{}, err
	}
	hasGUI := false
	if i, e := os.Lstat(filepath.Join(layout.ExecutableDirectory, "Sidravia.exe")); e == nil {
		if !i.Mode().IsRegular() {
			return transaction.Record{}, ErrChanged
		}
		hasGUI = true
	} else if !os.IsNotExist(e) {
		return transaction.Record{}, e
	}
	if _, err = VerifyInstalled(ctx, layout.ExecutableDirectory, old, oldAsset, index, hasGUI); err != nil {
		return transaction.Record{}, err
	}
	asset, e := next.Asset(c)
	if e != nil {
		return transaction.Record{}, e
	}
	if asset.OS != "windows" || asset.Arch != runtime.GOARCH || asset.Kind != "setup" || hasGUI && asset.PayloadKind != "gui" {
		return transaction.Record{}, release.ErrUnsupported
	}
	root, err := productlayout.OperationRoot()
	if err != nil {
		return transaction.Record{}, err
	}
	store, err := transaction.Open(root, layout.ExecutableDirectory, release.PublisherTrust())
	if err != nil {
		return transaction.Record{}, err
	}
	if previous, exists, e := store.Read(ctx); e != nil {
		return transaction.Record{}, e
	} else if exists && previous.Phase != transaction.Completed && previous.Phase != transaction.Aborted && previous.Phase != transaction.ResumeFailed {
		return transaction.Record{}, transaction.ErrConflict
	}
	pinned, err := pin(archive, c.Size, c.SHA256)
	if err != nil {
		return transaction.Record{}, err
	}
	defer pinned.Close()
	m, err := old.VerifyFiles(oldAsset, index)
	if err != nil {
		return transaction.Record{}, err
	}
	var helperHash string
	var helperSize int64
	for _, f := range m.Files {
		if f.Path == "sidraviactl.exe" {
			helperHash = f.SHA256
			helperSize = f.Size
		}
	}
	if helperHash == "" {
		return transaction.Record{}, ErrInvalid
	}
	helper, err := pin(filepath.Join(layout.ExecutableDirectory, "sidraviactl.exe"), helperSize, helperHash)
	if err != nil {
		return transaction.Record{}, err
	}
	defer helper.Close()
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return transaction.Record{}, err
	}
	id := hex.EncodeToString(token)
	record, err := transaction.New(id, change.Target{ID: layout.ExecutableDirectory, Version: version, BuildID: build, Layout: "installed"}, c, next, time.Now())
	if err != nil {
		return record, err
	}
	owner, err := winproc.Observe(uint32(os.Getpid()))
	if err != nil {
		return record, err
	}
	defer owner.Close()
	manifest, sig := old.Proof()
	session := receiptstate.Session{SchemaVersion: 1, OperationID: id, Source: record.Source, Updater: receiptstate.ProcessID{owner.PID, owner.Created}, Namespace: ns.String(), HasGUI: hasGUI, Expires: time.Now().Add(30 * time.Minute), SourceManifest: manifest, SourceSignature: sig, SourceIndex: index, Integration: "not_started"}
	if err = saveSession(ctx, session); err != nil {
		return record, err
	}
	if _, err = store.Create(ctx, record); err != nil {
		return record, err
	}
	abort := func(cause error) (transaction.Record, error) {
		_, _, e := store.Advance(context.WithoutCancel(ctx), id, transaction.Verified, transaction.Aborted, "preparation_failed", time.Now())
		return record, errors.Join(cause, e)
	}
	if _, _, err = store.Advance(ctx, id, transaction.Created, transaction.Verified, "", time.Now()); err != nil {
		return record, err
	}
	offer, err := innopeer.NewOffer(id, helperHash)
	if err != nil {
		return abort(err)
	}
	defer offer.Close()
	cmd, err := winproc.StartIndependent(archive, "/SP-", "/NORESTART", "/RESTARTEXITCODE=3010", "/DIR="+layout.ExecutableDirectory, "/SIDRAVIAUPDATE="+id, "/SIDRAVIAUPDATER="+strconv.Itoa(os.Getpid()))
	if err != nil {
		return abort(errors.Join(ErrHandoff, err))
	}
	launched, err := winproc.Observe(uint32(cmd.Process.Pid))
	cmd.Process.Release()
	if err != nil {
		return record, err
	}
	defer launched.Close()
	handoff, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	coordinator, err := offer.Await(handoff, launched)
	if err != nil {
		return record, err
	}
	defer coordinator.Close()
	session.Coordinator = receiptstate.ProcessID{coordinator.PID, coordinator.Created}
	if err = saveSession(context.WithoutCancel(ctx), session); err != nil {
		return record, err
	}
	record, _, err = store.Read(context.WithoutCancel(ctx))
	return record, err
}

func pin(path string, size int64, hash string) (*os.File, error) {
	file, err := winproc.PinExecutable(path, size, hash)
	if errors.Is(err, winproc.ErrImageChanged) {
		return nil, ErrChanged
	}
	return file, err
}
