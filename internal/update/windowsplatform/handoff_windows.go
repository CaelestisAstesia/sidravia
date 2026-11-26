//go:build windows

package windowsplatform

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"sidravia/internal/installation/change"
)

type Offer struct {
	handle     windows.Handle
	id         string
	helperHash string
	owner      *Process
}

func NewOffer(id, helperHash string) (*Offer, error) {
	if !change.ValidID(id) || len(helperHash) != 64 {
		return nil, change.ErrInvalid
	}
	owner, err := Observe(uint32(os.Getpid()))
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + owner.SID + ")")
	if err != nil {
		owner.Close()
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, _ := windows.UTF16PtrFromString(pipeName(id))
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_NOWAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 1024, 1024, 0, &sa)
	if err != nil {
		owner.Close()
		return nil, err
	}
	return &Offer{h, id, helperHash, owner}, nil
}
func (o *Offer) Close() error {
	var pipeErr, ownerErr error
	if o.handle != 0 {
		pipeErr = windows.CloseHandle(o.handle)
		o.handle = 0
	}
	if o.owner != nil {
		ownerErr = o.owner.Close()
		o.owner = nil
	}
	return errors.Join(pipeErr, ownerErr)
}
func pipeName(id string) string { return `\\.\pipe\Sidravia.Update.` + id }

// Await authenticates a short-lived package host and its exact installer parent.
// Callers still must verify signed artifacts and their target-bound operation.
func (o *Offer) Await(ctx context.Context, launched *Process) (*Process, error) {
	if launched == nil {
		return nil, ErrPeer
	}
	for {
		err := windows.ConnectNamedPipe(o.handle, nil)
		if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			break
		}
		if !errors.Is(err, windows.ERROR_PIPE_LISTENING) {
			return nil, err
		}
		if err = pause(ctx); err != nil {
			return nil, err
		}
	}
	var peerID uint32
	if err := windows.GetNamedPipeClientProcessId(o.handle, &peerID); err != nil {
		return nil, err
	}
	peer, err := Observe(peerID)
	if err != nil {
		return nil, err
	}
	defer peer.Close()
	if peer.SID != o.owner.SID || peer.Integrity != o.owner.Integrity {
		return nil, ErrPeer
	}
	hash, err := FileHash(peer.Path)
	if err != nil || hash != o.helperHash {
		return nil, ErrPeer
	}
	msg, err := readMessage(ctx, o.handle)
	if err != nil {
		return nil, err
	}
	if len(msg) != 36 || string(msg[:32]) != o.id {
		return nil, ErrPeer
	}
	coordinator, err := Observe(binary.LittleEndian.Uint32(msg[32:]))
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*Process, error) { coordinator.Close(); return nil, e }
	parent, err := parentPID(peerID)
	if err != nil || parent != coordinator.PID || coordinator.Created > peer.Created || coordinator.SID != o.owner.SID || coordinator.Integrity != o.owner.Integrity {
		return fail(ErrPeer)
	}
	// Inno's loader may delegate to one worker. The worker must belong to
	// the exact still-live process launched by this updater, not any host parent.
	status, waitErr := windows.WaitForSingleObject(launched.handle, 0)
	if waitErr != nil || status != uint32(windows.WAIT_TIMEOUT) || coordinator.Created < launched.Created {
		return fail(ErrPeer)
	}
	if coordinator.PID != launched.PID {
		loader, parentErr := parentPID(coordinator.PID)
		if parentErr != nil || loader != launched.PID {
			return fail(ErrPeer)
		}
	} else if coordinator.Created != launched.Created {
		return fail(ErrPeer)
	}
	if err = writeMessage(ctx, o.handle, []byte("accepted")); err != nil {
		return fail(err)
	}
	done, err := readMessage(ctx, o.handle)
	if err != nil {
		return fail(err)
	}
	if string(done) != "received" {
		return fail(ErrPeer)
	}
	return coordinator, nil
}

// Acknowledge is called by the installer's short package host. It confirms the
// server PID before sending anything; a forged pipe cannot stop the application.
func Acknowledge(ctx context.Context, id string, serverID, coordinatorID uint32) error {
	if !change.ValidID(id) {
		return change.ErrInvalid
	}
	name, _ := windows.UTF16PtrFromString(pipeName(id))
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var actual uint32
	if err = windows.GetNamedPipeServerProcessId(h, &actual); err != nil {
		return err
	}
	if actual != serverID {
		return ErrPeer
	}
	owner, err := Observe(serverID)
	if err != nil {
		return err
	}
	defer owner.Close()
	self, err := Observe(uint32(os.Getpid()))
	if err != nil {
		return err
	}
	defer self.Close()
	if self.SID != owner.SID || self.Integrity != owner.Integrity {
		return ErrPeer
	}
	mode := uint32(windows.PIPE_READMODE_MESSAGE | windows.PIPE_NOWAIT)
	if err = windows.SetNamedPipeHandleState(h, &mode, nil, nil); err != nil {
		return err
	}
	msg := append([]byte(id), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(msg[32:], coordinatorID)
	if err = writeMessage(ctx, h, msg); err != nil {
		return err
	}
	reply, err := readMessage(ctx, h)
	if err != nil {
		return err
	}
	if string(reply) != "accepted" {
		return ErrPeer
	}
	return writeMessage(ctx, h, []byte("received"))
}
func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil
	}
}
func readMessage(ctx context.Context, h windows.Handle) ([]byte, error) {
	b := make([]byte, 1024)
	for {
		var n uint32
		err := windows.ReadFile(h, b, &n, nil)
		if err == nil && n > 0 {
			return b[:n], nil
		}
		if err != nil && !errors.Is(err, windows.ERROR_NO_DATA) {
			return nil, err
		}
		if err = pause(ctx); err != nil {
			return nil, err
		}
	}
}
func writeMessage(ctx context.Context, h windows.Handle, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var n uint32
	if err := windows.WriteFile(h, b, &n, nil); err != nil {
		return err
	}
	if int(n) != len(b) {
		return ErrPeer
	}
	return nil
}
