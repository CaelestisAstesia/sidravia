package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

func TestGUIBootstrapIsHiddenFromNormalHelp(t *testing.T) {
	var output bytes.Buffer
	deps := commandDependencies{identity: mustTestIdentity(t), output: &output, guiBootstrap: fakeUnsupportedBootstrap}
	if err := runCommand([]string{"--help"}, deps); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "gui") || strings.Contains(output.String(), "bootstrap") || strings.Contains(output.String(), "owner-pid") {
		t.Fatalf("normal help exposed hidden bootstrap: %s", output.String())
	}
}

func TestGUIBootstrapInvalidPIDWritesNoSuccessBytes(t *testing.T) {
	var output bytes.Buffer
	deps := commandDependencies{identity: mustTestIdentity(t), output: &output, guiBootstrap: fakeUnsupportedBootstrap}
	for _, args := range [][]string{
		{"gui", "bootstrap"},
		{"gui", "bootstrap", "--owner-pid", "0"},
		{"gui", "bootstrap", "--unknown", "value"},
	} {
		err := runCommand(args, deps)
		assertGUIBootstrapFailure(t, err, guiBootstrapInvalidArgumentsCode, 2)
	}
	if output.Len() != 0 {
		t.Fatalf("stdout=%q", output.String())
	}
}

func fakeUnsupportedBootstrap(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
	return clientbootstrap.DesktopBootstrapResult{}, clientbootstrap.ErrDesktopUnsupported
}

func TestGUIBootstrapRendersExactMachineJSON(t *testing.T) {
	identity := mustTestIdentity(t)
	owner := 42
	var output bytes.Buffer
	err := guiBootstrap(identity, owner, &output, func(got clientbootstrap.Identity, pid int) (clientbootstrap.DesktopBootstrapResult, error) {
		if got != identity || pid != owner {
			t.Fatalf("identity=%+v pid=%d", got, pid)
		}
		return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(7), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 7, Mode: "desktop", DesktopOwnerPID: &owner}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":1,"endpoint":"ws://127.0.0.1:65530/ipc","token":"` + strings.Repeat("a", 64) + `","productVersion":"1.0.0","buildId":"build","daemonPid":7,"mode":"desktop"}` + "\n"
	if output.String() != want {
		t.Fatalf("output=%q want=%q", output.String(), want)
	}
}

func TestGUIBootstrapFailureNeverWritesSuccessJSON(t *testing.T) {
	identity := mustTestIdentity(t)
	owner := 42
	for _, tc := range []struct {
		name      string
		bootstrap guiBootstrapper
		writer    interface{ Write([]byte) (int, error) }
		code      string
		exitCode  int
	}{
		{"unsupported", fakeUnsupportedBootstrap, &bytes.Buffer{}, guiBootstrapUnsupportedCode, 11},
		{"mode conflict", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{}, clientbootstrap.ErrModeConflict
		}, &bytes.Buffer{}, guiBootstrapModeConflictCode, 12},
		{"incompatible", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{}, clientbootstrap.ErrIncompatibleGeneration
		}, &bytes.Buffer{}, guiBootstrapIncompatibleCode, 13},
		{"unconfirmed owner", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(7), Status: contract.StatusResult{ProductVersion: "other", BuildID: "build", PID: 7, Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		}, &bytes.Buffer{}, guiBootstrapUnconfirmedCode, 14},
		{"unconfirmed zero status PID", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(7), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 0, Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		}, &bytes.Buffer{}, guiBootstrapUnconfirmedCode, 14},
		{"unconfirmed mismatched daemon PID", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(7), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 8, Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		}, &bytes.Buffer{}, guiBootstrapUnconfirmedCode, 14},
		{"underlying", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{}, errors.New("private-token-marker")
		}, &bytes.Buffer{}, guiBootstrapFailedCode, 15},
		{"write", func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(7), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 7, Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		}, zeroWriter{err: errors.New("private-write-marker")}, guiBootstrapOutputFailedCode, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			writer := tc.writer
			if _, ok := writer.(*bytes.Buffer); ok {
				writer = &output
			}
			err := guiBootstrap(identity, owner, writer, tc.bootstrap)
			assertGUIBootstrapFailure(t, err, tc.code, tc.exitCode)
			if output.Len() != 0 {
				t.Fatalf("stdout=%q", output.String())
			}
			assertGUIBootstrapFailureDocument(t, err, tc.code)
		})
	}
}

func TestGUIBootstrapInvalidIdentityHasMachineContractOnlyOnHiddenPath(t *testing.T) {
	err := RunWithIdentity([]string{"gui", "bootstrap", "--owner-pid", "42"}, "", "build")
	assertGUIBootstrapFailure(t, err, guiBootstrapInvalidIdentityCode, 10)
	assertGUIBootstrapFailureDocument(t, err, guiBootstrapInvalidIdentityCode)

	err = RunWithIdentity([]string{"daemon", "status"}, "", "build")
	if !errors.Is(err, clientbootstrap.ErrInvalidIdentity) || ExitCode(err) != 1 {
		t.Fatalf("ordinary invalid identity err=%v exit=%d", err, ExitCode(err))
	}
}

func assertGUIBootstrapFailure(t *testing.T, err error, wantCode string, wantExitCode int) {
	t.Helper()
	var failure *guiBootstrapFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T %v is not a GUI bootstrap failure", err, err)
	}
	if failure.code != wantCode || ExitCode(err) != wantExitCode {
		t.Fatalf("code=%q exit=%d, want %q/%d", failure.code, ExitCode(err), wantCode, wantExitCode)
	}
}

func assertGUIBootstrapFailureDocument(t *testing.T, err error, wantCode string) {
	t.Helper()
	var output bytes.Buffer
	if writeErr := WriteError(&output, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	want := `{"schemaVersion":1,"error":{"code":"` + wantCode + `","message":"`
	var failure *guiBootstrapFailure
	if !errors.As(err, &failure) {
		t.Fatal("missing GUI bootstrap failure")
	}
	want += failure.message + `"}}` + "\n"
	if output.String() != want {
		t.Fatalf("document=%q want=%q", output.String(), want)
	}
	if strings.Contains(output.String(), "private-token-marker") || strings.Contains(output.String(), "private-write-marker") {
		t.Fatalf("machine document leaked private detail: %q", output.String())
	}
}

func mustTestIdentity(t *testing.T) clientbootstrap.Identity {
	t.Helper()
	identity, err := clientbootstrap.NewIdentity("1.0.0", "build")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
