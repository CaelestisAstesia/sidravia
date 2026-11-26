//go:build windows

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/document/jsondoc"
	"sidravia/internal/document/securefile"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
	"sidravia/internal/release"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
)

var ErrHandoff = errors.New("portable update handoff unavailable")

// HandoffCommitError means acknowledgment publication may have committed. The
// caller must report uncertainty and status, never claim cancellation/no action.
type HandoffCommitError struct{ Cause error }

func (e *HandoffCommitError) Error() string { return "portable handoff commit unconfirmed" }
func (e *HandoffCommitError) Unwrap() error { return e.Cause }

type runnerID struct {
	PID     uint32 `json:"pid"`
	Created uint64 `json:"created"`
}
type windowsSession struct {
	Operation    string        `json:"operation"`
	Source       change.Target `json:"source"`
	Updater      runnerID      `json:"updater"`
	Caller       runnerID      `json:"caller"`
	Namespace    string        `json:"namespace"`
	Archive      string        `json:"archive"`
	OldManifest  []byte        `json:"oldManifest"`
	OldSignature []byte        `json:"oldSignature"`
	OldIndex     []byte        `json:"oldIndex"`
	Expires      time.Time     `json:"expires"`
}

func runnerDirectory(target, id string) (string, error) {
	if !change.ValidID(id) || !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return "", ErrInvalid
	}
	root, err := productlayout.OperationRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "portable", digest([]byte(strings.ToLower(target))), id), nil
}
func savePrivate(ctx context.Context, path string, value any) error {
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return store.Replace(ctx, path, b)
}
func readPrivate(ctx context.Context, path string, value any) error {
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if err != nil {
		return err
	}
	b, exists, err := store.Read(ctx, path, 4<<20)
	if err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}
	return jsondoc.DecodeStrict(b, value)
}
func readWindowsSession(ctx context.Context, target, id string) (windowsSession, string, error) {
	dir, err := runnerDirectory(target, id)
	if err != nil {
		return windowsSession{}, "", err
	}
	var s windowsSession
	if err = readPrivate(ctx, filepath.Join(dir, "session.json"), &s); err != nil {
		return s, dir, err
	}
	if s.Operation != id || s.Source.ID != target || s.Source.Layout != "portable" || s.Source.Validate() != nil || s.Updater.PID == 0 || s.Updater.Created == 0 {
		return s, dir, ErrInvalid
	}
	return s, dir, nil
}
func sourceInventory(s windowsSession) (filetxn.Inventory, error) {
	proof, err := release.PublisherTrust().Verify(s.OldManifest, s.OldSignature, s.Source.Version)
	if err != nil {
		return filetxn.Inventory{}, err
	}
	if proof.BuildID() != s.Source.BuildID {
		return filetxn.Inventory{}, ErrInvalid
	}
	// The signed source payload kind determines its portable channel.
	var m struct {
		PayloadKind string `json:"payloadKind"`
	}
	if json.Unmarshal(s.OldIndex, &m) != nil {
		return filetxn.Inventory{}, ErrInvalid
	}
	c, err := proof.InstalledAsset("windows", "amd64", m.PayloadKind+"-portable")
	if err != nil {
		return filetxn.Inventory{}, err
	}
	return filetxn.Authenticate(proof, c, s.OldIndex)
}
func pinnedRunner(path string, entrySize int64, hash string) (*os.File, error) {
	if !filetxn.PlainPlatformPath(path) {
		return nil, ErrChanged
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != entrySize {
		f.Close()
		return nil, ErrChanged
	}
	if err = filetxn.SingleFile(f, info); err != nil {
		f.Close()
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, entrySize+1))
	if err != nil || digest(data) != hash {
		f.Close()
		return nil, ErrChanged
	}
	return f, nil
}

// BeginWindows verifies both inventories and the complete archive before it
// hands execution to an authenticated temporary copy outside the target. The
// helper does not touch the product until the exact updater/calling CTL exit.
func BeginWindows(ctx context.Context, layout productlayout.Layout, version, build string, oldProof release.VerifiedRelease, oldAsset release.Candidate, oldIndex []byte, next release.VerifiedRelease, c release.Candidate, archive string) (record transaction.Record, resultErr error) {
	if layout.Mode != productlayout.ModePortable {
		return transaction.Record{}, ErrInvalid
	}
	old, err := filetxn.Authenticate(oldProof, oldAsset, oldIndex)
	if err != nil {
		return transaction.Record{}, err
	}
	if old.Identity().Version != version || old.Identity().BuildID != build {
		return transaction.Record{}, ErrInvalid
	}
	payload, err := filetxn.ExtractRelease(ctx, layout.ExecutableDirectory, archive, next, c)
	if err != nil {
		return transaction.Record{}, err
	}
	err = filetxn.Preflight(ctx, layout.ExecutableDirectory, old, payload)
	err = errors.Join(err, payload.Close())
	if err != nil {
		return transaction.Record{}, err
	}
	root, err := productlayout.OperationRoot()
	if err != nil {
		return transaction.Record{}, err
	}
	store, err := transaction.Open(root, layout.ExecutableDirectory, release.PublisherTrust())
	if err != nil {
		return transaction.Record{}, err
	}
	previous, exists, err := store.Read(ctx)
	if err != nil {
		return transaction.Record{}, err
	}
	if exists {
		if previous.Phase != transaction.Completed && previous.Phase != transaction.Aborted && previous.Phase != transaction.ResumeFailed {
			return transaction.Record{}, transaction.ErrConflict
		}
		if err = cleanupWindowsRunner(ctx, previous); err != nil {
			return transaction.Record{}, err
		}
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return transaction.Record{}, err
	}
	id := hex.EncodeToString(idBytes)
	r, err := transaction.New(id, change.Target{ID: layout.ExecutableDirectory, Layout: "portable", Version: version, BuildID: build}, c, next, time.Now())
	if err != nil {
		return r, err
	}
	self, err := winproc.Observe(uint32(os.Getpid()))
	if err != nil {
		return r, err
	}
	defer self.Close()
	if !strings.EqualFold(self.Path, filepath.Join(layout.ExecutableDirectory, "sidraviaupdater.exe")) {
		return r, ErrInvalid
	}
	manifest, sig := oldProof.Proof()
	s := windowsSession{Operation: id, Source: r.Source, Updater: runnerID{self.PID, self.Created}, Namespace: os.Getenv(launchcontract.EnvNamespace), Archive: archive, OldManifest: manifest, OldSignature: sig, OldIndex: oldIndex, Expires: time.Now().Add(30 * time.Minute)}
	if _, err = productlayout.NewNamespace(s.Namespace); err != nil {
		return r, err
	}
	caller, e := winproc.ObserveParent()
	if e == nil {
		defer caller.Close()
		if strings.EqualFold(caller.Path, filepath.Join(layout.ExecutableDirectory, "sidraviactl.exe")) {
			s.Caller = runnerID{caller.PID, caller.Created}
		}
	}
	dir, err := runnerDirectory(layout.ExecutableDirectory, id)
	if err != nil {
		return r, err
	}
	if err = savePrivate(ctx, filepath.Join(dir, "session.json"), s); err != nil {
		return r, err
	}
	entry, ok := old.Entry("sidraviaupdater.exe")
	if !ok {
		return r, ErrInvalid
	}
	original, err := pinnedRunner(self.Path, entry.Size, entry.SHA256)
	if err != nil {
		return r, err
	}
	defer original.Close()
	if _, err = original.Seek(0, io.SeekStart); err != nil {
		return r, err
	}
	path := filepath.Join(dir, "runner.exe")
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return r, err
	}
	_, err = io.Copy(out, original)
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		return r, err
	}
	pinned, err := pinnedRunner(path, entry.Size, entry.SHA256)
	if err != nil {
		return r, err
	}
	defer pinned.Close()
	if _, err = store.Create(ctx, r); err != nil {
		return r, err
	}
	if _, _, err = store.Advance(ctx, id, transaction.Created, transaction.Verified, "", time.Now()); err != nil {
		return r, err
	}
	commitStarted := false
	defer func() {
		if resultErr == nil || commitStarted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, e := store.Advance(cleanup, id, transaction.Verified, transaction.Aborted, "preparation_failed", time.Now())
		resultErr = errors.Join(resultErr, e)
	}()
	abort := func(cause error) (transaction.Record, error) {
		_, _, e := store.Advance(context.WithoutCancel(ctx), id, transaction.Verified, transaction.Aborted, "preparation_failed", time.Now())
		return r, errors.Join(cause, e)
	}
	cmd, err := winproc.StartIndependent(path, "portable-execute", "--target", layout.ExecutableDirectory, "--operation", id, "--updater", strconv.Itoa(os.Getpid()))
	if err != nil {
		return abort(errors.Join(ErrHandoff, err))
	}
	child, err := winproc.Observe(uint32(cmd.Process.Pid))
	cmd.Process.Release()
	if err != nil {
		return r, err
	}
	defer child.Close()
	wait, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		var ready runnerID
		if err = readPrivate(wait, filepath.Join(dir, "ready.json"), &ready); err == nil {
			if ready != (runnerID{child.PID, child.Created}) {
				return r, ErrChanged
			}
			if err = ctx.Err(); err != nil {
				return abort(err)
			}
			// Acknowledgment is the handoff commit point. From here cancellation may
			// not be reported as proof that no work will happen.
			commitStarted = true
			commit, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = savePrivate(commit, filepath.Join(dir, "accepted.json"), ready)
			cancel()
			if err != nil {
				return r, &HandoffCommitError{err}
			}
			return r, nil
		}
		if !os.IsNotExist(err) {
			return r, err
		}
		if exited, err := child.PollExit(); exited || err != nil {
			return abort(errors.Join(ErrHandoff, err))
		}
		if err = pause(wait); err != nil {
			return r, err
		}
	}
}

func ExecuteWindows(ctx context.Context, target, id string, updater uint32, version, build string) (resultErr error) {
	s, dir, err := readWindowsSession(ctx, target, id)
	if err != nil {
		return err
	}
	if updater != s.Updater.PID || s.Namespace != os.Getenv(launchcontract.EnvNamespace) || !time.Now().Before(s.Expires) || version != s.Source.Version || build != s.Source.BuildID {
		return ErrInvalid
	}
	parent, err := winproc.ObserveParent()
	if err != nil {
		return err
	}
	defer parent.Close()
	if (runnerID{parent.PID, parent.Created}) != s.Updater || !strings.EqualFold(parent.Path, filepath.Join(target, "sidraviaupdater.exe")) {
		return ErrInvalid
	}
	if exited, err := parent.PollExit(); exited || err != nil {
		return errors.Join(ErrInvalid, err)
	}
	self, err := winproc.Observe(uint32(os.Getpid()))
	if err != nil {
		return err
	}
	defer self.Close()
	if !strings.EqualFold(self.Path, filepath.Join(dir, "runner.exe")) || self.SID != parent.SID || self.Integrity != parent.Integrity {
		return ErrInvalid
	}
	old, err := sourceInventory(s)
	if err != nil {
		return err
	}
	entry, ok := old.Entry("sidraviaupdater.exe")
	if !ok {
		return ErrInvalid
	}
	pin, err := pinnedRunner(self.Path, entry.Size, entry.SHA256)
	if err != nil {
		return err
	}
	defer pin.Close()
	root, err := productlayout.OperationRoot()
	if err != nil {
		return err
	}
	store, err := transaction.Open(root, target, release.PublisherTrust())
	if err != nil {
		return err
	}
	r, exists, err := store.Read(ctx)
	if err != nil {
		return err
	}
	if !exists || r.ID != id || r.Source != s.Source || r.Phase != transaction.Verified {
		return ErrInvalid
	}
	defer func() {
		if resultErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		current, exists, e := store.Read(cleanup)
		if e == nil && exists && current.ID == id && current.Phase == transaction.Verified {
			_, _, e = store.Advance(cleanup, id, transaction.Verified, transaction.Aborted, "preparation_failed", time.Now())
		}
		resultErr = errors.Join(resultErr, e)
	}()
	var caller *winproc.Process
	if s.Caller.PID != 0 {
		caller, err = winproc.Observe(s.Caller.PID)
		if err != nil {
			return err
		}
		defer caller.Close()
		if caller.Created != s.Caller.Created || caller.SID != parent.SID || !strings.EqualFold(caller.Path, filepath.Join(target, "sidraviactl.exe")) {
			return ErrInvalid
		}
	}
	if err = savePrivate(ctx, filepath.Join(dir, "ready.json"), runnerID{self.PID, self.Created}); err != nil {
		return err
	}
	accepted, cancelAcceptance := context.WithTimeout(ctx, 15*time.Second)
	defer cancelAcceptance()
	for {
		var acknowledgment runnerID
		err = readPrivate(accepted, filepath.Join(dir, "accepted.json"), &acknowledgment)
		if err == nil {
			if acknowledgment != (runnerID{self.PID, self.Created}) {
				return ErrChanged
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		if exited, err := parent.PollExit(); exited || err != nil {
			return errors.Join(ErrHandoff, err)
		}
		if err = pause(accepted); err != nil {
			return err
		}
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err = parent.Wait(wait); err != nil {
		return err
	}
	if caller != nil {
		if err = caller.Wait(wait); err != nil {
			return err
		}
	}
	proof, err := release.PublisherTrust().Verify(r.Manifest, r.Signature, r.Target.Version)
	if err != nil {
		return err
	}
	c := release.Candidate{ID: r.Package.CandidateID, Version: r.Target.Version, Name: r.Package.Name, Size: r.Package.Size, SHA256: r.Package.SHA256}
	payload, err := filetxn.ExtractRelease(ctx, target, s.Archive, proof, c)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, payload.Close()) }()
	return execute(ctx, execution{Store: store, OperationID: id, Target: target, Old: old, Payload: payload, Namespace: s.Namespace})
}

// A running Windows image cannot delete itself. The next update removes only
// the previous exact, signed helper after its generation is confirmed dead.
// Unknown files/receipts are retained; there is no recursive cache deletion.
func cleanupWindowsRunner(ctx context.Context, r transaction.Record) error {
	if r.Source.Layout != "portable" {
		return nil
	}
	s, dir, err := readWindowsSession(ctx, r.Source.ID, r.ID)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var ready runnerID
	if err = readPrivate(ctx, filepath.Join(dir, "ready.json"), &ready); err == nil {
		running, e := winproc.GenerationRunning(ready.PID, ready.Created)
		if e != nil {
			return e
		}
		if running {
			return transaction.ErrConflict
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	old, err := sourceInventory(s)
	if err != nil {
		return err
	}
	entry, ok := old.Entry("sidraviaupdater.exe")
	if !ok {
		return ErrInvalid
	}
	path := filepath.Join(dir, "runner.exe")
	pin, err := pinnedRunner(path, entry.Size, entry.SHA256)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = pin.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}
