//go:build windows

package jsonfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"sidravia/internal/daemon/persistence"
)

func windowsTestOwner(t *testing.T) resolvedOwner {
	t.Helper()
	operations := newWindowsSecureFileOperations()
	owner, err := operations.resolveOwner("")
	if err == nil {
		return owner
	}
	if failureCode(t, err) != persistence.FailurePermissionDenied {
		t.Fatalf("resolveOwner() = %v", err)
	}
	owner, err = operations.resolveOwner("S-1-5-21-1-2-3-1001")
	if err != nil {
		t.Fatalf("resolve explicit test owner = %v", err)
	}
	return owner
}

func newWindowsTestStore(t *testing.T, owner resolvedOwner) *SecureStore {
	t.Helper()
	store, err := NewSecureStore(SecureStoreOptions{IntendedOwnerSID: owner.sid})
	if err != nil {
		t.Fatalf("NewSecureStore() = %v", err)
	}
	return store
}

func TestWindowsOwnerDefaultsToCurrentNonSystemToken(t *testing.T) {
	operations := newWindowsSecureFileOperations()
	current, err := currentTokenOwnerSID()
	if err != nil {
		t.Fatal(err)
	}
	owner, resolveErr := operations.resolveOwner("")
	if current == localSystemSID {
		requireFailureCode(t, resolveErr, persistence.FailurePermissionDenied)
		return
	}
	if resolveErr != nil || owner.sid != current {
		t.Fatalf("resolveOwner() = %#v, %v, current = %q", owner, resolveErr, current)
	}
}

func TestWindowsSystemTokenRequiresExplicitIntendedOwner(t *testing.T) {
	if current, err := currentTokenOwnerSID(); err != nil || current != localSystemSID {
		t.Skip("test process is not LocalSystem")
	}
	_, err := newWindowsSecureFileOperations().resolveOwner("")
	requireFailureCode(t, err, persistence.FailurePermissionDenied)
}

func TestWindowsExplicitOwnerSIDSeamAcceptsCanonicalUserSID(t *testing.T) {
	owner := windowsTestOwner(t)
	resolved, err := newWindowsSecureFileOperations().resolveOwner(owner.sid)
	if err != nil || resolved.sid != owner.sid {
		t.Fatalf("resolveOwner(%q) = %#v, %v", owner.sid, resolved, err)
	}
}

func TestWindowsProtectionClassificationExcludesOrdinaryFailures(t *testing.T) {
	for _, unsupported := range []error{
		errorNotSupported,
		errorInvalidFunction,
		errorCallNotImplemented,
	} {
		if !errors.Is(classifyWindowsProtection(unsupported), ProtectionUnsupported) {
			t.Fatalf("%v was not classified unsupported", unsupported)
		}
	}
	for _, ordinary := range []error{
		syscall.ERROR_ACCESS_DENIED,
		errorInvalidParameter,
		errorInvalidName,
		syscall.ERROR_PATH_NOT_FOUND,
		syscall.Errno(0x7fff),
	} {
		if errors.Is(classifyWindowsProtection(ordinary), ProtectionUnsupported) {
			t.Fatalf("%v was classified unsupported", ordinary)
		}
	}
}

func TestWindowsDirectoryAndFileDACLIsProtectedOwnerAndSystemOnly(t *testing.T) {
	owner := windowsTestOwner(t)
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	if err := newWindowsTestStore(t, owner).Replace(context.Background(), path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	assertWindowsProtectedOwnerSystemOnly(t, filepath.Dir(path), owner.sid)
	assertWindowsProtectedOwnerSystemOnly(t, path, owner.sid)
}

func TestWindowsSecurityDescriptorOwnerAndGroupAreIntendedOwner(t *testing.T) {
	owner := windowsTestOwner(t)
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	if err := newWindowsTestStore(t, owner).Replace(context.Background(), path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectWindowsSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ownerSID != owner.sid || snapshot.groupSID != owner.sid {
		t.Fatalf("owner/group = %q/%q, want %q", snapshot.ownerSID, snapshot.groupSID, owner.sid)
	}
}

func TestWindowsSecureTempHasFinalDACLBeforeAnyPayloadWrite(t *testing.T) {
	owner := windowsTestOwner(t)
	directory := filepath.Join(t.TempDir(), "secure")
	operations := newWindowsSecureFileOperations()
	if err := operations.ensureDirectory(directory, owner); err != nil {
		t.Fatal(err)
	}
	temp, err := operations.createTemp(directory, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer operations.removeTemp(temp.Name())
	defer temp.Close()
	info, err := os.Stat(temp.Name())
	if err != nil || info.Size() != 0 {
		t.Fatalf("temp stat = %v, %v", info, err)
	}
	assertWindowsProtectedOwnerSystemOnly(t, temp.Name(), owner.sid)
}

func TestWindowsSecureTempRetriesNameCollisions(t *testing.T) {
	owner := windowsTestOwner(t)
	directory := filepath.Join(t.TempDir(), "secure")
	operations := newWindowsSecureFileOperations()
	if err := operations.ensureDirectory(directory, owner); err != nil {
		t.Fatal(err)
	}
	oldRandom := secureTempRandom
	defer func() { secureTempRandom = oldRandom }()
	first := bytes.Repeat([]byte{0x11}, secureTempRandomBytes)
	second := bytes.Repeat([]byte{0x22}, secureTempRandomBytes)
	secureTempRandom = &sequenceReader{data: append(first, second...)}
	collision := filepath.Join(directory, secureTempBasename(first))
	if err := os.WriteFile(collision, nil, 0600); err != nil {
		t.Fatal(err)
	}
	temp, err := operations.createTemp(directory, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer operations.removeTemp(temp.Name())
	defer temp.Close()
	if got, want := filepath.Base(temp.Name()), secureTempBasename(second); got != want {
		t.Fatalf("temp basename = %q, want %q", got, want)
	}
}

func TestWindowsSecureTempExhaustsNameCollisionsBeforePayloadWrite(t *testing.T) {
	owner := windowsTestOwner(t)
	directory := filepath.Join(t.TempDir(), "secure")
	operations := newWindowsSecureFileOperations()
	if err := operations.ensureDirectory(directory, owner); err != nil {
		t.Fatal(err)
	}
	oldRandom := secureTempRandom
	defer func() { secureTempRandom = oldRandom }()
	random := bytes.Repeat([]byte{0x33}, secureTempRandomBytes)
	secureTempRandom = bytes.NewReader(bytes.Repeat(random, secureTempAttempts))
	collision := filepath.Join(directory, secureTempBasename(random))
	if err := os.WriteFile(collision, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := operations.createTemp(directory, owner); err == nil {
		t.Fatal("createTemp succeeded after bounded collisions")
	}
	data, err := os.ReadFile(collision)
	if err != nil || string(data) != "sentinel" {
		t.Fatalf("collision contents = %q, %v", data, err)
	}
}

func TestWindowsReplacementDoesNotWidenACL(t *testing.T) {
	owner := windowsTestOwner(t)
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	store := newWindowsTestStore(t, owner)
	if err := store.Replace(context.Background(), path, []byte(`{"old":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(context.Background(), path, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	assertWindowsProtectedOwnerSystemOnly(t, path, owner.sid)
}

func TestWindowsPermissiveParentInheritanceIsNeutralized(t *testing.T) {
	owner := windowsTestOwner(t)
	path := filepath.Join(t.TempDir(), "child", "state.json")
	if err := newWindowsTestStore(t, owner).Replace(context.Background(), path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	assertWindowsProtectedOwnerSystemOnly(t, filepath.Dir(path), owner.sid)
	assertWindowsProtectedOwnerSystemOnly(t, path, owner.sid)
}

func TestWindowsACLFailureLeavesOldBytesAndNoPlaintextDestination(t *testing.T) {
	owner := windowsTestOwner(t)
	directory := filepath.Join(t.TempDir(), "secure")
	path := filepath.Join(directory, "state.json")
	operations := newWindowsSecureFileOperations()
	if err := operations.ensureDirectory(directory, owner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	oldSet := setNamedSecurityInfoWCall
	setNamedSecurityInfoWCall = func(string, uintptr, uintptr, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) error {
		return syscall.ERROR_ACCESS_DENIED
	}
	defer func() { setNamedSecurityInfoWCall = oldSet }()
	store, err := newSecureStoreWithOperations(SecureStoreOptions{IntendedOwnerSID: owner.sid}, operations)
	if err != nil {
		t.Fatal(err)
	}
	requireFailureCode(t, store.Replace(context.Background(), path, []byte(`{"secret":true}`)), persistence.FailurePermissionDenied)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	matches, _ := filepath.Glob(filepath.Join(directory, ".sidravia-*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("live temps = %v", matches)
	}
}

func TestWindowsReplaceFileCommitsExistingDestination(t *testing.T) {
	owner := windowsTestOwner(t)
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	store := newWindowsTestStore(t, owner)
	if err := store.Replace(context.Background(), path, []byte(`{"old":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(context.Background(), path, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{\"new\":true}\n" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}

func TestWindowsReplaceFileUsesExactlyZeroFlags(t *testing.T) {
	oldCall := replaceFileWCall
	defer func() { replaceFileWCall = oldCall }()
	var flags uint32
	replaceFileWCall = func(_, _, _ string, got uint32) error { flags = got; return nil }
	committed, err := newWindowsSecureFileOperations().commit(`C:\temp`, `C:\destination`, true)
	if !committed || flags != 0 {
		t.Fatalf("commit = %v, %v; flags = %#x", committed, err, flags)
	}
}

func TestWindowsMoveFileExWriteThroughCommitsInitialDestination(t *testing.T) {
	oldCall := moveFileExWCall
	defer func() { moveFileExWCall = oldCall }()
	var flags uint32
	moveFileExWCall = func(_, _ string, got uint32) error { flags = got; return nil }
	committed, err := newWindowsSecureFileOperations().commit(`C:\temp`, `C:\destination`, false)
	if !committed || flags != moveFileWriteThrough {
		t.Fatalf("commit = %v, %v; flags = %#x", committed, err, flags)
	}
}

func TestWindowsDirectoryACEsInheritWhileFileACEsDoNot(t *testing.T) {
	owner := windowsTestOwner(t)
	directory := filepath.Join(t.TempDir(), "secure")
	path := filepath.Join(directory, "state.json")
	if err := newWindowsTestStore(t, owner).Replace(context.Background(), path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	dirSnapshot, err := inspectWindowsSecurity(directory)
	if err != nil {
		t.Fatal(err)
	}
	wantDirFlags := uint8(objectInheritAce | containerInheritAce)
	if got := dirSnapshot.allowACEFlags[owner.sid]; got != wantDirFlags {
		t.Fatalf("directory owner ACE flags = %#x, want OI|CI %#x", got, wantDirFlags)
	}
	if got := dirSnapshot.allowACEFlags[localSystemSID]; got != wantDirFlags {
		t.Fatalf("directory SYSTEM ACE flags = %#x, want OI|CI %#x", got, wantDirFlags)
	}

	fileSnapshot, err := inspectWindowsSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileSnapshot.allowACEFlags[owner.sid]; got != 0 {
		t.Fatalf("file owner ACE flags = %#x, want no inheritance flags", got)
	}
	if got := fileSnapshot.allowACEFlags[localSystemSID]; got != 0 {
		t.Fatalf("file SYSTEM ACE flags = %#x, want no inheritance flags", got)
	}
}

// applyWindowsNonInheritedOwnerReadDACL installs a non-protected DACL with a
// single non-inheritable owner read ACE and no LocalSystem ACE. It is test-only
// machinery for placing a child directory in a known initial state (lacking
// inheritable owner/LocalSystem ACEs) so a nested file does not inherit them.
func applyWindowsNonInheritedOwnerReadDACL(t *testing.T, path string, owner resolvedOwner) {
	t.Helper()
	sddl := fmt.Sprintf("O:%sG:%sD:(A;;FR;;;%s)", owner.sid, owner.sid, owner.sid)
	descriptor, err := win32SecurityDescriptor(sddl)
	if err != nil {
		t.Fatalf("build non-inherited DACL: %v", err)
	}
	defer win32LocalFree(descriptor)
	ownerSID, groupSID, dacl, err := win32DescriptorParts(descriptor)
	if err != nil {
		t.Fatalf("descriptor parts: %v", err)
	}
	if err := setNamedSecurityInfoWCall(
		path, seFileObject, ownerSecurityInfo|groupSecurityInfo|daclSecurityInfo, ownerSID, groupSID, dacl,
	); err != nil {
		t.Fatalf("apply non-inherited DACL: %v", err)
	}
}

func inheritedAllowACE(snapshot windowsSecuritySnapshot, sid string) (uint32, bool) {
	for _, ace := range snapshot.allowACEList {
		if ace.sid == sid && ace.flags&inheritedAce != 0 {
			return ace.mask, true
		}
	}
	return 0, false
}

func assertWindowsInheritedOwnerSystemFullControl(t *testing.T, path, intendedOwner string) {
	t.Helper()
	snapshot, err := inspectWindowsSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	if mask, ok := inheritedAllowACE(snapshot, intendedOwner); !ok || mask != fileAllAccess {
		t.Fatalf("inherited owner Full Control missing on %s: ACEs = %#v", path, snapshot.allowACEList)
	}
	if mask, ok := inheritedAllowACE(snapshot, localSystemSID); !ok || mask != fileAllAccess {
		t.Fatalf("inherited LocalSystem Full Control missing on %s: ACEs = %#v", path, snapshot.allowACEList)
	}
}

func assertWindowsNoInheritedOwnerSystem(t *testing.T, path, intendedOwner string) {
	t.Helper()
	snapshot, err := inspectWindowsSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	if mask, ok := inheritedAllowACE(snapshot, intendedOwner); ok {
		t.Fatalf("unexpected inherited owner ACE on %s: mask = %#x, ACEs = %#v", path, mask, snapshot.allowACEList)
	}
	if mask, ok := inheritedAllowACE(snapshot, localSystemSID); ok {
		t.Fatalf("unexpected inherited LocalSystem ACE on %s: mask = %#x, ACEs = %#v", path, mask, snapshot.allowACEList)
	}
}

// TestWindowsPreExistingNestedChildrenReceiveInheritedAccess proves that
// securing a parent directory with the new inheritable descriptor propagates
// usable intended-owner/LocalSystem Full Control to pre-existing nested
// Profile-like children (an institution-profiles directory and a jlu.json file
// inside it). The parent is first isolated with the old non-inheritable model so
// the children start without inherited intended-owner/LocalSystem access.
func TestWindowsPreExistingNestedChildrenReceiveInheritedAccess(t *testing.T) {
	owner := windowsTestOwner(t)
	operations := newWindowsSecureFileOperations()
	parent := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := hardenWindowsFile(parent, owner); err != nil {
		t.Fatalf("isolate parent with old model: %v", err)
	}
	profileDir := filepath.Join(parent, "institution-profiles")
	if err := os.Mkdir(profileDir, 0700); err != nil {
		t.Fatal(err)
	}
	applyWindowsNonInheritedOwnerReadDACL(t, profileDir, owner)
	profileFile := filepath.Join(profileDir, "jlu.json")
	if err := os.WriteFile(profileFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	assertWindowsNoInheritedOwnerSystem(t, profileDir, owner.sid)
	assertWindowsNoInheritedOwnerSystem(t, profileFile, owner.sid)

	if err := operations.ensureDirectory(parent, owner); err != nil {
		t.Fatalf("secure parent directory: %v", err)
	}
	assertWindowsInheritedOwnerSystemFullControl(t, profileDir, owner.sid)
	assertWindowsInheritedOwnerSystemFullControl(t, profileFile, owner.sid)
}

// TestWindowsDirectoryPreparationRepairsEmptyInheritedChild reproduces the old
// protected, non-inheritable parent model and proves the new directory
// preparation repairs an existing child that otherwise has an empty inherited
// DACL: the child starts with no inherited intended-owner/LocalSystem access,
// and after the new inheritable directory preparation it gains both.
func TestWindowsDirectoryPreparationRepairsEmptyInheritedChild(t *testing.T) {
	owner := windowsTestOwner(t)
	operations := newWindowsSecureFileOperations()
	parent := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := hardenWindowsFile(parent, owner); err != nil {
		t.Fatalf("reproduce old non-inheritable parent model: %v", err)
	}
	child := filepath.Join(parent, "institution-profiles")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	assertWindowsNoInheritedOwnerSystem(t, child, owner.sid)

	if err := operations.ensureDirectory(parent, owner); err != nil {
		t.Fatalf("repair directory: %v", err)
	}
	assertWindowsInheritedOwnerSystemFullControl(t, child, owner.sid)
}

type sequenceReader struct {
	data []byte
}

func (reader *sequenceReader) Read(destination []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, errors.New("sequence exhausted")
	}
	count := copy(destination, reader.data)
	reader.data = reader.data[count:]
	return count, nil
}

func assertWindowsProtectedOwnerSystemOnly(t *testing.T, path, intendedOwner string) {
	t.Helper()
	snapshot, err := inspectWindowsSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.daclProtected {
		t.Fatal("DACL is not protected")
	}
	want := map[string]uint32{intendedOwner: fileAllAccess, localSystemSID: fileAllAccess}
	if snapshot.allowACECount != len(want) || len(snapshot.allowACEs) != len(want) {
		t.Fatalf("allow ACEs = %#v", snapshot.allowACEs)
	}
	for sid, mask := range snapshot.allowACEs {
		if want[sid] != mask {
			t.Fatalf("allow ACE %q mask = %#x, approved = %#x", sid, mask, want[sid])
		}
	}
}
