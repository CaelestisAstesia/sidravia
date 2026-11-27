//go:build windows

package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/launch"
	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/layout"
	"sidravia/internal/installation/winmutex"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/process/winproc"
	"sidravia/internal/release"
	"sidravia/internal/release/qtrepository"
	"sidravia/internal/update/internal/request"
)

func applyQt(ctx context.Context, cacheRoot string, installation layout.Layout, identity daemonctl.Identity,
	candidate release.Candidate, proof release.VerifiedRelease, archive string) (_ *applyResult, err error) {
	unlock, err := winmutex.AcquireInstallation()
	if err != nil {
		return nil, errors.Join(errOperation, err)
	}
	err = qtrepository.ClearStaged(cacheRoot)
	unlock()
	if err != nil {
		return nil, err
	}
	tool := filepath.Join(installation.ExecutableDirectory, "sidravia-maintenance.exe")
	info, err := os.Lstat(tool)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<30 {
		return nil, errSourceProof
	}
	pin, err := winproc.PinExecutable(tool, info.Size(), request.QtMaintenanceDigest)
	if err != nil {
		return nil, errors.Join(errSourceProof, err)
	}
	defer pin.Close()
	repository, err := qtrepository.Stage(ctx, archive, cacheRoot, request.QtMaintenanceDigest, proof, candidate)
	if err != nil {
		return nil, err
	}
	proceed := false
	defer func() {
		if !proceed {
			err = errors.Join(err, os.RemoveAll(repository))
		}
	}()

	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	operation := hex.EncodeToString(token[:])
	event := "Local\\Sidravia-IFW-" + operation
	readyHandle, err := openQtHandoffEvent(event + "-ready")
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(readyHandle)
	proceedHandle, err := openQtHandoffEvent(event + "-proceed")
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(proceedHandle)

	self, err := winproc.Observe(uint32(os.Getpid()))
	if err != nil {
		return nil, err
	}
	defer self.Close()
	waitFor := operation + ";" + strconv.Itoa(os.Getpid())
	if parent, err := winproc.ObserveParent(); err == nil {
		defer parent.Close()
		if ownsQtProcess(parent, self, installation, "sidraviactl.exe") {
			waitFor += ";" + strconv.FormatUint(uint64(parent.PID), 10)
		}
	}
	repositoryURL := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(repository)}).String()
	outcomePath := filepath.Join(cacheRoot, qtOutcomeFilename)
	env := append(os.Environ(), "SIDRAVIA_IFW_HANDOFF="+waitFor,
		"SIDRAVIA_IFW_OUTCOME="+outcomePath, "SIDRAVIA_IFW_VERSION="+candidate.Version)
	// Qt can outlive this updater in the ordinary Windows user job. It
	// waits for the exact updater and launcher to exit before writing files.
	args := []string{"--set-temp-repository", repositoryURL,
		"--accept-licenses", "--reject-messages", "--confirm-command", "update"}
	log, err := openQtLog(cacheRoot)
	if err != nil {
		return nil, err
	}
	child, err := winproc.StartApplicationWithLog(tool, args, env, log)
	_ = log.Close()
	if err != nil {
		return nil, errors.Join(errOperation, err)
	}
	defer child.Process.Release()

	wait, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := waitQtReady(wait, readyHandle); err != nil {
		return nil, err
	}
	if err := writeQtOutcome(wait, outcomePath, candidate.Version, "pending"); err != nil {
		return nil, err
	}
	defer func() {
		if !proceed {
			outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			err = errors.Join(err, writeQtOutcome(outcomeCtx, outcomePath, candidate.Version, "failed"))
		}
	}()
	if err := quiesceQt(wait, installation, identity, self, operation); err != nil {
		return nil, err
	}
	if err := windows.SetEvent(proceedHandle); err != nil {
		return nil, err
	}
	proceed = true
	return &applyResult{HandedOver: true, ApplicationRecovery: "pending", Authentication: "not_evaluated"}, nil
}

func openQtHandoffEvent(name string) (windows.Handle, error) {
	value, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateEvent(nil, 1, 0, value)
	if err != nil {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return 0, err
	}
	return handle, nil
}

func waitQtReady(ctx context.Context, event windows.Handle) error {
	for ctx.Err() == nil {
		result, err := windows.WaitForSingleObject(event, 100)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
	}
	return ctx.Err()
}

func ownsQtProcess(process, self *winproc.Process, installation layout.Layout, name string) bool {
	return process.SID == self.SID && process.Integrity == self.Integrity &&
		strings.EqualFold(process.Path, filepath.Join(installation.ExecutableDirectory, name))
}

func quiesceQt(ctx context.Context, installation layout.Layout, identity daemonctl.Identity, self *winproc.Process, operation string) error {
	namespace, err := layout.NewNamespace(os.Getenv(launch.EnvNamespace))
	if err != nil {
		return err
	}
	probe, err := daemonctl.ProbeApplication(ctx, identity, namespace)
	if err != nil {
		return err
	}
	if probe.State == daemonctl.ProbeStopped {
		return nil
	}
	if probe.State != daemonctl.ProbeReachable || probe.Status == nil {
		return errOperation
	}
	daemon, err := winproc.Observe(uint32(probe.Info.PID))
	if err != nil {
		return err
	}
	defer daemon.Close()
	if !ownsQtProcess(daemon, self, installation, "sidraviad.exe") {
		return winproc.ErrPeer
	}
	var gui *winproc.Process
	if probe.Status.Mode == "desktop" {
		if probe.Status.DesktopOwnerPID == nil {
			return winproc.ErrPeer
		}
		gui, err = winproc.Observe(uint32(*probe.Status.DesktopOwnerPID))
		if err != nil {
			return err
		}
		defer gui.Close()
		if !ownsQtProcess(gui, self, installation, "Sidravia.exe") {
			return winproc.ErrPeer
		}
	}
	client, err := daemonctl.Connect(ctx, identity, probe.Info)
	if err != nil {
		return err
	}
	defer client.Close()
	call := func(ctx context.Context, method string, body any, view *maintenance.View) error {
		request, err := json.Marshal(body)
		if err != nil {
			return err
		}
		response, err := client.Call(ctx, method, request)
		if err != nil {
			return err
		}
		if !response.OK {
			return errOperation
		}
		if view == nil {
			return nil
		}
		return json.Unmarshal(response.Result, view)
	}
	interactive := true
	payload := contract.UpdateOperationPayload{OperationID: operation}
	var state maintenance.View
	if err := call(ctx, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{
		OperationID: operation, CoordinatorPID: self.PID, Interactive: &interactive,
	}, &state); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			abort, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = call(abort, contract.MethodUpdateAbort, payload, nil)
		}
	}()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for state.OperationID == operation && state.Phase == "requested" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
			if err := call(ctx, contract.MethodUpdateStatus, struct{}{}, &state); err != nil {
				return err
			}
		}
	}
	if state.OperationID != operation || state.Phase != "prepared" {
		return errOperation
	}
	if err := call(ctx, contract.MethodUpdateCommit, payload, nil); err != nil {
		return err
	}
	committed = true
	if gui != nil {
		if err := gui.Wait(ctx); err != nil {
			return err
		}
	}
	return daemon.Wait(ctx)
}
