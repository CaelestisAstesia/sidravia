package cli

import (
	"bytes"
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
	if err := runCommand([]string{"gui", "bootstrap", "--owner-pid", "0"}, deps); err == nil {
		t.Fatal("expected invalid PID")
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

func mustTestIdentity(t *testing.T) clientbootstrap.Identity {
	t.Helper()
	identity, err := clientbootstrap.NewIdentity("1.0.0", "build")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
