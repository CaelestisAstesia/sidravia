//go:build windows

// This executable is a synthetic updater/short CTL host, never a product entry.
// It only operates on a caller-created fixture root bearing a random marker.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/resume"
	"sidravia/internal/installation/startgate"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/process/winproc"
	"sidravia/internal/update/setup/internal/innopeer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, resume.ErrConsumed) {
			os.Exit(22)
		}
		os.Exit(1)
	}
}
func write(root, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	path := filepath.Join(root, name)
	if err = os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func number(s string) uint32 { n, _ := strconv.ParseUint(s, 10, 32); return uint32(n) }
func run() error {
	if len(os.Args) < 4 {
		return errors.New("fixture arguments required")
	}
	role, root, id := os.Args[1], os.Args[2], os.Args[3]
	if !change.ValidID(id) {
		return change.ErrInvalid
	}
	marker, err := os.ReadFile(filepath.Join(root, "fixture-owner"))
	if err != nil || string(marker) != id {
		return errors.New("unowned fixture")
	}
	duration := 15 * time.Second
	if role == "real-lifecycle" {
		duration = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	switch role {
	case "real-lifecycle":
		return realLifecycle(ctx, root, id)
	case "job":
		if len(os.Args) != 6 {
			return errors.New("job arguments")
		}
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return err
		}
		defer func() {
			if job != 0 {
				windows.CloseHandle(job)
			}
		}()
		limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if os.Args[5] != "no-breakaway" {
			limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
		}
		if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
			return err
		}
		exe := filepath.Join(root, "target", "updater.exe")
		args := []string{exe, "offer", root, id, os.Args[4], os.Args[5]}
		line := ""
		for _, a := range args {
			if line != "" {
				line += " "
			}
			line += syscall.EscapeArg(a)
		}
		app, _ := windows.UTF16PtrFromString(exe)
		command, _ := windows.UTF16PtrFromString(line)
		si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
		var pi windows.ProcessInformation
		if err = windows.CreateProcess(app, command, nil, nil, false, windows.CREATE_SUSPENDED, nil, nil, &si, &pi); err != nil {
			return err
		}
		defer windows.CloseHandle(pi.Process)
		defer windows.CloseHandle(pi.Thread)
		if err = windows.AssignProcessToJobObject(job, pi.Process); err != nil {
			windows.TerminateProcess(pi.Process, 1)
			return err
		}
		if _, err = windows.ResumeThread(pi.Thread); err != nil {
			windows.TerminateProcess(pi.Process, 1)
			return err
		}
		var installer *winproc.Process
		defer func() {
			if installer != nil {
				ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
				defer cancel2()
				if installer.Wait(ctx2) != nil {
					owned, _ := os.FindProcess(int(installer.PID))
					_ = owned.Kill()
					waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
					_ = installer.Wait(waitCtx)
					waitCancel()
				}
				installer.Close()
			}
		}()
		until := time.Now().Add(18 * time.Second)
		for time.Now().Before(until) {
			data, readErr := os.ReadFile(filepath.Join(root, "launched.json"))
			if readErr == nil {
				var identity struct {
					PID     uint32 `json:"pid"`
					Created uint64 `json:"created"`
				}
				if err = json.Unmarshal(data, &identity); err != nil {
					return err
				}
				installer, err = winproc.Observe(identity.PID)
				if err != nil {
					return err
				}
				if installer.Created != identity.Created {
					return errors.New("installer identity changed")
				}
				if err = os.WriteFile(filepath.Join(root, "monitor-ready"), []byte("ready"), 0600); err != nil {
					return err
				}
				break
			}
			state, _ := windows.WaitForSingleObject(pi.Process, 0)
			if state == windows.WAIT_OBJECT_0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		status, err := windows.WaitForSingleObject(pi.Process, 20000)
		if err != nil {
			return err
		}
		if status != windows.WAIT_OBJECT_0 {
			return errors.New("fixture updater timed out")
		}
		var code uint32
		if err = windows.GetExitCodeProcess(pi.Process, &code); err != nil {
			return err
		}
		if os.Args[5] == "no-breakaway" {
			if code == 0 {
				return errors.New("unbreakable job unexpectedly accepted")
			}
		} else if code != 0 {
			return fmt.Errorf("fixture updater failed %d", code)
		}
		// Close the kill-on-close parent job BEFORE waiting for the installer.
		if err = windows.CloseHandle(job); err != nil {
			return err
		}
		job = 0
		if installer != nil {
			if err = installer.Wait(ctx); err != nil {
				return err
			}
			exit, err := installer.ExitCode()
			if err != nil {
				return err
			}
			if err = write(root, "installer-exit.json", map[string]any{"exitCode": exit, "pid": installer.PID, "created": installer.Created}); err != nil {
				return err
			}
		} else if os.Args[5] != "no-breakaway" {
			return errors.New("installer not observed")
		}
		return write(root, "job.json", map[string]any{"updaterExit": code, "mode": os.Args[5]})

	case "offer":
		if len(os.Args) != 6 {
			return errors.New("offer arguments")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		hash, err := winproc.FileHash(exe)
		if err != nil {
			return err
		}
		offer, err := innopeer.NewOffer(id, hash)
		if err != nil {
			return err
		}
		defer offer.Close()
		cmd, err := winproc.StartIndependent(os.Args[4], "/VERYSILENT", "/SUPPRESSMSGBOXES", "/SP-", "/NORESTART", "/DIR="+filepath.Join(root, "target"), "/ROOT="+root, "/OP="+id, "/UPDATER="+strconv.Itoa(os.Getpid()), "/LOG="+filepath.Join(root, "setup.log"))
		if err != nil {
			if os.Args[5] == "no-breakaway" && errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				if e := write(root, "launch-rejected.json", map[string]any{"reason": "job_breakaway_denied"}); e != nil {
					return e
				}
			}
			return err
		}
		launched, err := winproc.Observe(uint32(cmd.Process.Pid))
		if err != nil {
			return err
		}
		defer launched.Close()
		if err = write(root, "launched.json", map[string]any{"pid": launched.PID, "created": launched.Created}); err != nil {
			return err
		}
		for {
			if _, e := os.Stat(filepath.Join(root, "monitor-ready")); e == nil {
				break
			}
			if e := ctx.Err(); e != nil {
				return e
			}
			time.Sleep(10 * time.Millisecond)
		}
		// The installer owns its lifetime; do not keep inherited pipe handles or wait.
		if err = cmd.Process.Release(); err != nil {
			return err
		}
		coordinator, err := offer.Await(ctx, launched)
		if err != nil {
			return err
		}
		defer coordinator.Close()
		if err = write(root, "handoff.json", map[string]any{"phase": "handed_over", "coordinatorPid": coordinator.PID, "created": coordinator.Created}); err != nil {
			return err
		}
		if os.Args[5] == "stuck" {
			time.Sleep(3 * time.Second)
		}
		return nil
	case "accept":
		if len(os.Args) != 6 {
			return errors.New("accept arguments")
		}
		return innopeer.Acknowledge(ctx, id, number(os.Args[4]), number(os.Args[5]))
	case "inspect":
		target := filepath.Join(root, "target")
		layout, err := productlayout.ResolveTarget(target, productlayout.Namespace{})
		if err != nil {
			return err
		}
		if layout.Mode != productlayout.ModePortable || layout.ConfigurationsPath != filepath.Join(target, "config", "configurations.json") {
			return errors.New("host resolved wrong layout")
		}
		return write(root, "layout.json", layout)
	case "finalize":
		var intent maintenance.Intent
		b, err := os.ReadFile(filepath.Join(root, "intent.json"))
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &intent); err != nil {
			return err
		}
		ticket := resume.Ticket{OperationID: id, Target: change.Target{ID: id, Version: "2.0.0", BuildID: "synthetic", Layout: "portable"}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), Intent: intent}
		if _, err = resume.Plan(ticket, ticket.Target, true, time.Now()); err != nil {
			return err
		}
		return write(root, "ticket.json", ticket)
	case "resume":
		var ticket resume.Ticket
		b, err := os.ReadFile(filepath.Join(root, "ticket.json"))
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &ticket); err != nil {
			return err
		}
		plan, err := resume.Claim(root, ticket, ticket.Target, true, time.Now())
		if err != nil {
			return err
		}
		return write(root, "restored.json", plan)
	default:
		return errors.New("unknown fixture role")
	}
}

// Only fresh portable fixture binaries and namespace data are used here. The
// application/IPC/CTL are the real product entry points, with no credentials.
func realLifecycle(ctx context.Context, root, id string) error {
	// Ordinary applications can inherit a job that disallows breakaway. Only
	// installer handoff requires escaping its updater's kill-on-close job.
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return err
	}
	target := filepath.Join(root, "target")
	ns := "update-native-" + id[:12]
	layout, err := productlayout.ResolveTarget(target, mustNamespace(ns))
	if err != nil {
		return err
	}
	if err = os.MkdirAll(layout.InstitutionProfilesDirectory, 0700); err != nil {
		return err
	}
	cache := filepath.Join(root, "cache")
	if err = os.MkdirAll(cache, 0700); err != nil {
		return err
	}
	env := []string{}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "SIDRAVIA_") && !strings.EqualFold(key, "LOCALAPPDATA") {
			env = append(env, e)
		}
	}
	env = append(env, "SIDRAVIA_NAMESPACE="+ns, "SIDRAVIA_SKIP_AUTO_LOGIN=1", "SIDRAVIA_LOG_LEVEL=info", "LOCALAPPDATA="+cache)
	guiMode := len(os.Args) > 4 && os.Args[4] == "gui"
	if err = startupAdmission(ctx, target, env, guiMode); err != nil {
		return err
	}
	filename := "sidraviad.exe"
	args := []string{}
	if guiMode {
		filename = "Sidravia.exe"
		args = []string{"--skip-auto-login", "--start-visible"}
	}
	daemon := exec.Command(filepath.Join(target, filename), args...)
	daemon.Env = env
	log, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	daemon.Stdout = log
	daemon.Stderr = log
	if err = daemon.Start(); err != nil {
		return err
	}
	originalPID := daemon.Process.Pid
	if originalPID <= 0 {
		return errors.New("fixture child PID unavailable")
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Wait() }()
	defer func() {
		daemon.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}()
	version, buildID := "0.4.0-alpha.1", "fixture"
	// Exact packaged candidates carry the ordinary immutable build contract.
	// Fixture-only identities are used when no candidate BUILD-INFO is supplied.
	if data, e := os.ReadFile(filepath.Join(target, "BUILD-INFO.txt")); e == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}
			if key == "ProductVersion" {
				version = strings.TrimSpace(value)
			}
			if key == "BuildID" {
				buildID = strings.TrimSpace(value)
			}
		}
	}
	identity, err := daemonctl.NewIdentity(version, buildID)
	if err != nil {
		return err
	}
	readInfo := func() (contract.RuntimeInfo, error) {
		data, e := os.ReadFile(layout.RuntimeInfoPath)
		if e != nil {
			return contract.RuntimeInfo{}, e
		}
		return contract.DecodeRuntimeInfo(data)
	}
	ready := func() (contract.RuntimeInfo, error) {
		for {
			info, e := readInfo()
			if e == nil {
				conn, e := daemonctl.Connect(ctx, identity, info)
				if e == nil {
					conn.Close()
					return info, nil
				}
			}
			select {
			case <-ctx.Done():
				return contract.RuntimeInfo{}, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	info, err := ready()
	if err != nil {
		return err
	}
	originalDaemonPID := info.PID
	// Retain dead process objects just as an external watcher may. Recovery
	// must not need the image/token of an already exited generation.
	originalObserver, err := winproc.Observe(uint32(originalDaemonPID))
	if err != nil {
		return err
	}
	defer originalObserver.Close()
	if guiMode {
		originalGUI, e := winproc.Observe(uint32(originalPID))
		if e != nil {
			return e
		}
		defer originalGUI.Close()
	}
	stage := func(action, op string) error {
		cmd := exec.Command(filepath.Join(target, "sidraviactl.exe"), "update-internal", action, "--operation", op, "--coordinator", strconv.Itoa(os.Getpid()))
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil {
			return fmt.Errorf("%s: %w: %s", action, e, out)
		}
		return nil
	}
	if err = stage("prepare", id); err != nil {
		return err
	}
	conn, err := daemonctl.Connect(ctx, identity, info)
	if err != nil {
		return err
	}
	response, err := conn.Call(ctx, contract.MethodSessionStartConfiguration, json.RawMessage(`{"configurationId":"never-dispatch"}`))
	if err != nil {
		conn.Close()
		return err
	}
	if response.OK || response.Error == nil || response.Error.Code != contract.ErrorCodeMaintenance {
		conn.Close()
		return errors.New("real daemon admitted mutation while prepared")
	}
	conn.Close()
	if err = stage("abort", id); err != nil {
		return err
	}
	after, err := readInfo()
	if err != nil || after.PID != originalDaemonPID {
		return errors.New("abort restarted prepared daemon")
	}
	// A second operation proves normal stop then cancellation recovery with a
	// cold skip-auto-login generation. It must not restore by reusing the old PID.
	second := "abcdef0123456789" + id[:16]
	if err = stage("prepare", second); err != nil {
		return err
	}
	if err = stage("stop", second); err != nil {
		return err
	}
	select {
	case e := <-done:
		if e != nil {
			return e
		}
		close(done)
	case <-ctx.Done():
		return ctx.Err()
	}
	if err = stage("abort", second); err != nil {
		return err
	}
	restored, err := ready()
	if err != nil {
		return err
	}
	if restored.PID == originalDaemonPID {
		return errors.New("old process reused")
	}
	if err = stage("abort", second); err != nil {
		return err
	}
	repeated, err := readInfo()
	if err != nil || repeated.PID != restored.PID {
		return errors.New("repeated abort replaced the recovered generation")
	}
	restoredProcess, err := winproc.Observe(uint32(restored.PID))
	if err != nil {
		return err
	}
	defer restoredProcess.Close()
	// The fixture owns and stops its exact restored generation before cleanup.
	if guiMode {
		third := "fedcba9876543210" + id[:16]
		if err = stage("prepare", third); err != nil {
			return err
		}
		if err = stage("stop", third); err != nil {
			return err
		}
		return write(root, "real-inno.json", map[string]any{"preparedAbortSamePID": true, "committedExitConfirmed": true, "coldResumeConfirmed": true, "desktop": true, "namespace": ns, "productVersion": version, "buildId": buildID})
	}
	conn, err = daemonctl.Connect(ctx, identity, restored)
	if err != nil {
		return err
	}
	stopped, err := conn.Call(ctx, contract.MethodDaemonStop, json.RawMessage(`{}`))
	conn.Close()
	if err != nil || !stopped.OK {
		return errors.New("restored generation stop failed")
	}
	if err = restoredProcess.Wait(ctx); err != nil {
		return err
	}
	return write(root, "real-inno.json", map[string]any{"preparedAbortSamePID": true, "committedExitConfirmed": true, "coldResumeConfirmed": true, "namespace": ns, "productVersion": version, "buildId": buildID})
}
func mustNamespace(value string) productlayout.Namespace {
	ns, _ := productlayout.NewNamespace(value)
	return ns
}

// New process admission must be bounded and non-modal while Inno owns the
// account mutex. Internal short-host capabilities remain callable under it.
func startupAdmission(ctx context.Context, target string, env []string, gui bool) error {
	release, err := startgate.AcquireStart()
	if err != nil {
		return err
	}
	defer release()
	cases := []struct {
		name    string
		args    []string
		success bool
	}{
		{"sidraviad.exe", nil, false}, {"sidraviaupdater.exe", []string{"check"}, false}, {"sidraviactl.exe", []string{"--help"}, false}, {"sidraviactl.exe", []string{"update-internal", "capabilities"}, true},
	}
	if gui {
		cases = append(cases, struct {
			name    string
			args    []string
			success bool
		}{"Sidravia.exe", nil, false})
	}
	for _, item := range cases {
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		cmd := exec.CommandContext(bounded, filepath.Join(target, item.name), item.args...)
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		expired := bounded.Err()
		cancel()
		if expired != nil {
			return fmt.Errorf("maintenance admission not bounded: %s: %w", item.name, expired)
		}
		if (e == nil) != item.success {
			return fmt.Errorf("maintenance admission %s: %v %s", item.name, e, out)
		}
	}
	return nil
}
