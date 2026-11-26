//go:build windows

package setup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
	"sidravia/internal/update/windowsplatform"
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
	if asset.OS != "windows" || asset.Arch != "amd64" || asset.Kind != "setup" || hasGUI && asset.PayloadKind != "gui" {
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
	owner, err := windowsplatform.Observe(uint32(os.Getpid()))
	if err != nil {
		return record, err
	}
	defer owner.Close()
	manifest, sig := old.Proof()
	session := Session{SchemaVersion: 1, OperationID: id, Source: record.Source, Updater: ProcessID{owner.PID, owner.Created}, Namespace: ns.String(), HasGUI: hasGUI, Expires: time.Now().Add(30 * time.Minute), SourceManifest: manifest, SourceSignature: sig, SourceIndex: index, Integration: "not_started"}
	if err = SaveSession(ctx, session); err != nil {
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
	offer, err := windowsplatform.NewOffer(id, helperHash)
	if err != nil {
		return abort(err)
	}
	defer offer.Close()
	cmd, err := windowsplatform.StartIndependent(archive, "/SP-", "/NORESTART", "/RESTARTEXITCODE=3010", "/DIR="+layout.ExecutableDirectory, "/SIDRAVIAUPDATE="+id, "/SIDRAVIAUPDATER="+strconv.Itoa(os.Getpid()))
	if err != nil {
		return abort(errors.Join(ErrHandoff, err))
	}
	launched, err := windowsplatform.Observe(uint32(cmd.Process.Pid))
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
	session.Coordinator = ProcessID{coordinator.PID, coordinator.Created}
	if err = SaveSession(context.WithoutCancel(ctx), session); err != nil {
		return record, err
	}
	record, _, err = store.Read(context.WithoutCancel(ctx))
	return record, err
}

// Holding this ordinary read handle denies write/delete sharing until handoff;
// neither a mutable cache path nor a replaced old helper can change the launch.
func pin(path string, size int64, hash string) (*os.File, error) {
	if err := plainPath(path); err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		f.Close()
		return nil, ErrChanged
	}
	if err := singleLink(f); err != nil {
		f.Close()
		return nil, err
	}
	hsh := sha256.New()
	n, err := io.Copy(hsh, io.LimitReader(f, size+1))
	if err != nil || n != size || hex.EncodeToString(hsh.Sum(nil)) != hash {
		f.Close()
		return nil, ErrChanged
	}

	return f, nil
}
