//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsDaemonLogsUseProtectedOwnerAndSystemDACL(t *testing.T) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if user.User.Sid.String() == daemonLogLocalSystemSID {
		t.Skip("current process is LocalSystem")
	}

	logPath := filepath.Join(t.TempDir(), "logs", "sidraviad.log")
	file, err := prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(logPath, daemonLogRotateThreshold); err != nil {
		t.Fatal(err)
	}
	file, err = prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	assertWindowsDaemonLogDACL(t, filepath.Dir(logPath), user.User.Sid.String(), true)
	assertWindowsDaemonLogDACL(t, logPath, user.User.Sid.String(), false)
	assertWindowsDaemonLogDACL(t, logPath+".1", user.User.Sid.String(), false)
}

func assertWindowsDaemonLogDACL(t *testing.T, path, owner string, directory bool) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	actualOwner, _, err := descriptor.Owner()
	if err != nil || actualOwner.String() != owner {
		t.Fatalf("owner = %v, %v; want %s", actualOwner, err, owner)
	}
	group, _, err := descriptor.Group()
	if err != nil || group.String() != owner {
		t.Fatalf("group = %v, %v; want %s", group, err, owner)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL control = %#x, %v; want protected", control, err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount != 2 {
		t.Fatalf("ACE count = %d, want 2", dacl.AceCount)
	}
	wantFlags := uint8(0)
	if directory {
		wantFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	want := map[string]bool{owner: false, daemonLogLocalSystemSID: false}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != 0 || ace.Mask != windows.ACCESS_MASK(0x001F01FF) || ace.Header.AceFlags != wantFlags {
			t.Fatalf("ACE[%d] = %#v, want Full Control with flags %#x", index, ace, wantFlags)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if _, ok := want[sid]; !ok || want[sid] {
			t.Fatalf("unexpected or duplicate ACE SID %q", sid)
		}
		want[sid] = true
	}
	for sid, found := range want {
		if !found {
			t.Fatalf("missing ACE for %s", sid)
		}
	}
}
