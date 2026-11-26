//go:build linux

package environment

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestReadSystemHostInformationLinuxRealCall proves the real Linux path returns
// all four fields nonempty with the Linux family.
func TestReadSystemHostInformationLinuxRealCall(t *testing.T) {
	info, err := ReadSystemHostInformation()
	if err != nil {
		t.Fatalf("ReadSystemHostInformation = %v, want nil", err)
	}
	if info.HostName == "" {
		t.Error("HostName is empty")
	}
	if info.OperatingSystemFamily != "Linux" {
		t.Errorf("OperatingSystemFamily = %q, want Linux", info.OperatingSystemFamily)
	}
	if info.OperatingSystemRelease == "" {
		t.Error("OperatingSystemRelease is empty")
	}
	if info.MachineArchitecture == "" {
		t.Error("MachineArchitecture is empty")
	}
}

func TestReadSystemHostInformationInjectsKnownValues(t *testing.T) {
	info, err := readSystemHostInformation(
		func() (string, error) { return "myhost", nil },
		func(uts *unix.Utsname) error {
			copy(uts.Release[:], "6.1.0-test\x00")
			return nil
		},
		"arm64",
	)
	if err != nil {
		t.Fatalf("readSystemHostInformation = %v, want nil", err)
	}
	if info.HostName != "myhost" {
		t.Errorf("HostName = %q, want myhost", info.HostName)
	}
	if info.OperatingSystemRelease != "6.1.0-test" {
		t.Errorf("OperatingSystemRelease = %q, want 6.1.0-test", info.OperatingSystemRelease)
	}
	if info.OperatingSystemFamily != "Linux" {
		t.Errorf("OperatingSystemFamily = %q, want Linux", info.OperatingSystemFamily)
	}
	if info.MachineArchitecture != "arm64" {
		t.Errorf("MachineArchitecture = %q, want arm64", info.MachineArchitecture)
	}
}

func TestReadSystemHostInformationRejectsEmptyHostname(t *testing.T) {
	_, err := readSystemHostInformation(
		func() (string, error) { return "", nil },
		func(*unix.Utsname) error { return nil },
		"amd64",
	)
	if err == nil {
		t.Fatal("expected error for empty host name")
	}
	if !strings.Contains(err.Error(), "主机名") {
		t.Errorf("error = %v, want host-name label", err)
	}
}

func TestReadSystemHostInformationPreservesHostnameCause(t *testing.T) {
	cause := errors.New("injected hostname failure")
	_, err := readSystemHostInformation(
		func() (string, error) { return "", cause },
		func(*unix.Utsname) error { return nil },
		"amd64",
	)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want hostname cause preserved", err)
	}
	if !strings.Contains(err.Error(), "读取主机名") {
		t.Errorf("error = %v, want hostname operation label", err)
	}
}

func TestReadSystemHostInformationPreservesUnameCause(t *testing.T) {
	cause := errors.New("injected uname failure")
	_, err := readSystemHostInformation(
		func() (string, error) { return "host", nil },
		func(*unix.Utsname) error { return cause },
		"amd64",
	)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want uname cause preserved", err)
	}
	if !strings.Contains(err.Error(), "读取内核版本") {
		t.Errorf("error = %v, want uname operation label", err)
	}
}

func TestReadSystemHostInformationRejectsEmptyRelease(t *testing.T) {
	_, err := readSystemHostInformation(
		func() (string, error) { return "host", nil },
		func(uts *unix.Utsname) error { return nil },
		"amd64",
	)
	if err == nil {
		t.Fatal("expected error for empty kernel release")
	}
	if !strings.Contains(err.Error(), "内核版本") {
		t.Errorf("error = %v, want kernel release label", err)
	}
}
