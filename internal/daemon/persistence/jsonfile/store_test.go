package jsonfile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	"sidravia/internal/daemon/persistence"
)

type fakeWritableTemp struct {
	operations *fakeSecureFileOperations
	name       string
	data       []byte
	writeLimit int
	writeErr   error
	syncErr    error
	closeErr   error
	closed     bool
}

func (temp *fakeWritableTemp) Write(data []byte) (int, error) {
	temp.operations.calls = append(temp.operations.calls, "write")
	if temp.writeErr != nil {
		return 0, temp.writeErr
	}
	count := len(data)
	if temp.writeLimit >= 0 && count > temp.writeLimit {
		count = temp.writeLimit
	}
	temp.data = append(temp.data, data[:count]...)
	return count, nil
}
func (temp *fakeWritableTemp) Sync() error {
	temp.operations.calls = append(temp.operations.calls, "sync")
	return temp.syncErr
}
func (temp *fakeWritableTemp) Close() error {
	temp.operations.calls = append(temp.operations.calls, "close")
	temp.closed = true
	return temp.closeErr
}
func (temp *fakeWritableTemp) Name() string { return temp.name }

type fakeSecureFileOperations struct {
	calls                []string
	owner                resolvedOwner
	resolveErr           error
	ensureErr            error
	unprotectedEnsureErr error
	inspectExists        bool
	inspectErr           error
	hardenErr            error
	openErr              error
	readData             []byte
	createErr            error
	commitErr            error
	commitResult         bool
	removeErr            error
	temp                 *fakeWritableTemp
	destination          []byte
	liveTemp             bool
	cancelBeforeCommit   context.CancelFunc
	cancelAfterCommit    context.CancelFunc
	tempNameOverride     string
	createAttempts       int
	collisionsBeforeOpen int
}

func (operations *fakeSecureFileOperations) resolveOwner(string) (resolvedOwner, error) {
	operations.calls = append(operations.calls, "resolve-owner")
	return operations.owner, operations.resolveErr
}
func (operations *fakeSecureFileOperations) ensureDirectory(string, resolvedOwner) error {
	operations.calls = append(operations.calls, "ensure-directory")
	return operations.ensureErr
}
func (operations *fakeSecureFileOperations) inspectDestination(string) (bool, error) {
	operations.calls = append(operations.calls, "inspect-destination")
	return operations.inspectExists, operations.inspectErr
}
func (operations *fakeSecureFileOperations) hardenDestination(string, resolvedOwner) error {
	operations.calls = append(operations.calls, "harden-destination")
	return operations.hardenErr
}
func (operations *fakeSecureFileOperations) openForRead(string) (io.ReadCloser, error) {
	operations.calls = append(operations.calls, "open-read")
	if operations.openErr != nil {
		return nil, operations.openErr
	}
	return io.NopCloser(bytes.NewReader(operations.readData)), nil
}
func (operations *fakeSecureFileOperations) createTemp(directory string, _ resolvedOwner) (writableTemp, error) {
	operations.calls = append(operations.calls, "create-temp")
	if operations.collisionsBeforeOpen >= 16 {
		operations.createAttempts = 16
		return nil, errors.New("collisions exhausted")
	}
	operations.createAttempts = operations.collisionsBeforeOpen + 1
	if operations.createErr != nil {
		return nil, operations.createErr
	}
	name := filepath.Join(directory, ".sidravia-test.tmp")
	if operations.tempNameOverride != "" {
		name = operations.tempNameOverride
	}
	operations.temp = &fakeWritableTemp{operations: operations, name: name, writeLimit: -1}
	operations.liveTemp = true
	return operations.temp, nil
}
func (operations *fakeSecureFileOperations) ensureUnprotectedDirectory(string) error {
	operations.calls = append(operations.calls, "ensure-unprotected-directory")
	return operations.unprotectedEnsureErr
}
func (operations *fakeSecureFileOperations) createUnprotectedTemp(directory string) (writableTemp, error) {
	operations.calls = append(operations.calls, "create-unprotected-temp")
	return operations.createTemp(directory, resolvedOwner{})
}
func (operations *fakeSecureFileOperations) commit(_, _ string, _ bool) (bool, error) {
	operations.calls = append(operations.calls, "commit")
	if operations.cancelBeforeCommit != nil {
		operations.cancelBeforeCommit()
	}
	if operations.commitResult {
		operations.destination = append([]byte(nil), operations.temp.data...)
		operations.liveTemp = false
	}
	if operations.cancelAfterCommit != nil {
		operations.cancelAfterCommit()
	}
	return operations.commitResult, operations.commitErr
}
func (operations *fakeSecureFileOperations) removeTemp(string) error {
	operations.calls = append(operations.calls, "remove-temp")
	operations.liveTemp = false
	return operations.removeErr
}

func newFakeStore(t *testing.T, operations *fakeSecureFileOperations) *SecureStore {
	t.Helper()
	store, err := newSecureStoreWithOperations(SecureStoreOptions{}, operations)
	if err != nil {
		t.Fatalf("newSecureStoreWithOperations() error = %v", err)
	}
	operations.calls = nil
	return store
}

func testDestination(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}

func requireFailureCode(t *testing.T, err error, code persistence.FailureCode) {
	t.Helper()
	if got := failureCode(t, err); got != code {
		t.Fatalf("failure code = %q, want %q", got, code)
	}
	if err != nil && (bytes.Contains([]byte(err.Error()), []byte("state.json")) || bytes.Contains([]byte(err.Error()), []byte("write denied"))) {
		t.Fatalf("public error leaked path or platform text: %q", err)
	}
}

func TestSecureStoreSatisfiesStore(t *testing.T) {
	var _ Store = (*SecureStore)(nil)
}

func TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		data []byte
		code persistence.FailureCode
	}{
		{name: "relative", path: "state.json", data: []byte(`{}`), code: persistence.FailureInvalidArgument},
		{name: "unclean", path: filepath.Dir(testDestination(t)) + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "state.json", data: []byte(`{}`), code: persistence.FailureInvalidArgument},
		{name: "invalid JSON", path: testDestination(t), data: []byte(`{`), code: persistence.FailureInvalidDocument},
		{name: "multiple JSON", path: testDestination(t), data: []byte(`{} {}`), code: persistence.FailureInvalidDocument},
		{name: "newline suffix", path: testDestination(t), data: []byte("{}\n"), code: persistence.FailureInvalidDocument},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := &fakeSecureFileOperations{}
			store := newFakeStore(t, operations)
			requireFailureCode(t, store.Replace(context.Background(), test.path, test.data), test.code)
			if len(operations.calls) != 0 {
				t.Fatalf("operations after validation failure = %v", operations.calls)
			}
		})
	}
}

func TestSecureStoreReadSecuresDirectoryAndExistingDestinationBeforeOpen(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, readData: []byte(`{}`)}
	store := newFakeStore(t, operations)
	data, exists, err := store.Read(context.Background(), testDestination(t), 16)
	if err != nil || !exists || string(data) != `{}` {
		t.Fatalf("Read() = %q, %v, %v", data, exists, err)
	}
	want := []string{"ensure-directory", "inspect-destination", "harden-destination", "open-read"}
	if !reflect.DeepEqual(operations.calls, want) {
		t.Fatalf("calls = %v, want %v", operations.calls, want)
	}
}

func TestSecureStoreReadDistinguishesMissingEmptyAndOversizedFiles(t *testing.T) {
	missing := &fakeSecureFileOperations{}
	data, exists, err := newFakeStore(t, missing).Read(context.Background(), testDestination(t), 1)
	if err != nil || exists || data != nil {
		t.Fatalf("missing Read() = %q, %v, %v", data, exists, err)
	}
	empty := &fakeSecureFileOperations{inspectExists: true}
	data, exists, err = newFakeStore(t, empty).Read(context.Background(), testDestination(t), 1)
	if err != nil || !exists || len(data) != 0 {
		t.Fatalf("empty Read() = %q, %v, %v", data, exists, err)
	}
	large := &fakeSecureFileOperations{inspectExists: true, readData: []byte("ab")}
	_, exists, err = newFakeStore(t, large).Read(context.Background(), testDestination(t), 1)
	if !exists {
		t.Fatal("oversized existing file reported missing")
	}
	requireFailureCode(t, err, persistence.FailureSizeLimitExceeded)
}

func TestSecureStoreReplaceUsesSameDirectorySecureTempAndOneNewline(t *testing.T) {
	operations := &fakeSecureFileOperations{commitResult: true}
	path := testDestination(t)
	if err := newFakeStore(t, operations).Replace(context.Background(), path, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(operations.temp.name) != filepath.Dir(path) || string(operations.destination) != "{\"a\":1}\n" {
		t.Fatalf("temp = %q, destination = %q", operations.temp.name, operations.destination)
	}
}

func TestSecureStoreReplaceSensitiveEnforcesPersistedSizeLimitBeforeWriting(t *testing.T) {
	operations := &fakeSecureFileOperations{commitResult: true}
	store := newFakeStore(t, operations)
	path := testDestination(t)
	document := []byte(`{"a":1}`)

	// The document persists with one trailing newline, so the persisted size is
	// one byte above the caller's data and that is what a maximum limits.
	if err := store.ReplaceSensitive(context.Background(), path, document, int64(len(document))+1, true); err != nil {
		t.Fatalf("ReplaceSensitive(at persisted limit) error = %v", err)
	}
	if string(operations.destination) != "{\"a\":1}\n" {
		t.Fatalf("destination = %q", operations.destination)
	}

	operations.calls = nil
	requireFailureCode(t, store.ReplaceSensitive(context.Background(), path, document, int64(len(document)), true), persistence.FailureSizeLimitExceeded)
	if len(operations.calls) != 0 {
		t.Fatalf("rejected replacement touched the filesystem: %v", operations.calls)
	}
	if string(operations.destination) != "{\"a\":1}\n" {
		t.Fatalf("rejected replacement changed the destination to %q", operations.destination)
	}
}

func TestSecureStoreReplaceOrdersWriteSyncCloseContextAndCommit(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, commitResult: true}
	if err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	want := []string{"ensure-directory", "inspect-destination", "harden-destination", "create-temp", "write", "sync", "close", "commit"}
	if !reflect.DeepEqual(operations.calls, want) {
		t.Fatalf("calls = %v, want %v", operations.calls, want)
	}
}

func TestSecureStoreShortWritePreservesDestinationAndCleansTemp(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old")}
	store := newFakeStore(t, operations)
	operationsWithZero := &zeroWriteOperations{fakeSecureFileOperations: operations}
	store, _ = newSecureStoreWithOperations(SecureStoreOptions{}, operationsWithZero)
	operations.calls = nil
	err := store.Replace(context.Background(), testDestination(t), []byte(`{}`))
	requireFailureCode(t, err, persistence.FailureAtomicWrite)
	if string(operations.destination) != "old" || operations.liveTemp {
		t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
	}
}

type zeroWriteOperations struct{ *fakeSecureFileOperations }

func (operations *zeroWriteOperations) createTemp(directory string, owner resolvedOwner) (writableTemp, error) {
	temp, err := operations.fakeSecureFileOperations.createTemp(directory, owner)
	if err == nil {
		operations.temp.writeLimit = 0
	}
	return temp, err
}

func TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination(t *testing.T) {
	for _, failurePoint := range []string{"write", "sync", "close"} {
		t.Run(failurePoint, func(t *testing.T) {
			operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old")}
			store := newFakeStore(t, operations)
			wrapped := &failureTempOperations{fakeSecureFileOperations: operations, point: failurePoint}
			store, _ = newSecureStoreWithOperations(SecureStoreOptions{}, wrapped)
			operations.calls = nil
			err := store.Replace(context.Background(), testDestination(t), []byte(`{}`))
			requireFailureCode(t, err, persistence.FailureAtomicWrite)
			if string(operations.destination) != "old" || operations.liveTemp {
				t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
			}
		})
	}
}

type failureTempOperations struct {
	*fakeSecureFileOperations
	point string
}

func (operations *failureTempOperations) createTemp(directory string, owner resolvedOwner) (writableTemp, error) {
	temp, err := operations.fakeSecureFileOperations.createTemp(directory, owner)
	if err != nil {
		return nil, err
	}
	switch operations.point {
	case "write":
		operations.temp.writeErr = errors.New("write denied")
	case "sync":
		operations.temp.syncErr = errors.New("sync denied")
	case "close":
		operations.temp.closeErr = errors.New("close denied")
	}
	return temp, nil
}

func TestSecureStoreDirectoryAndDestinationHardeningFailuresPrecedeTempCreation(t *testing.T) {
	for _, operations := range []*fakeSecureFileOperations{
		{ensureErr: errors.New("denied")},
		{inspectExists: true, hardenErr: errors.New("denied")},
	} {
		err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`))
		requireFailureCode(t, err, persistence.FailurePermissionDenied)
		for _, call := range operations.calls {
			if call == "create-temp" {
				t.Fatal("temp created before hardening completed")
			}
		}
	}
}

func TestSecureStoreCancellationBeforeFinalCommitPreservesDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old")}
	wrapped := &cancelOnCloseOperations{fakeSecureFileOperations: operations, cancel: cancel}
	store, _ := newSecureStoreWithOperations(SecureStoreOptions{}, wrapped)
	operations.calls = nil
	err := store.Replace(ctx, testDestination(t), []byte(`{}`))
	requireFailureCode(t, err, persistence.FailureAtomicWrite)
	if string(operations.destination) != "old" || operations.liveTemp {
		t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
	}
}

type cancelOnCloseOperations struct {
	*fakeSecureFileOperations
	cancel context.CancelFunc
}

func (operations *cancelOnCloseOperations) createTemp(directory string, owner resolvedOwner) (writableTemp, error) {
	temp, err := operations.fakeSecureFileOperations.createTemp(directory, owner)
	if err != nil {
		return nil, err
	}
	return &cancelOnCloseTemp{writableTemp: temp, cancel: operations.cancel}, nil
}

type cancelOnCloseTemp struct {
	writableTemp
	cancel context.CancelFunc
}

func (temp *cancelOnCloseTemp) Close() error {
	err := temp.writableTemp.Close()
	temp.cancel()
	return err
}

func TestSecureStorePreCommitFailurePreservesDestinationAndCleansTemp(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old"), commitErr: errors.New("rename denied")}
	err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`))
	requireFailureCode(t, err, persistence.FailureAtomicWrite)
	if string(operations.destination) != "old" || operations.liveTemp {
		t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
	}
}

func TestSecureStoreCancellationAfterKnownCommitStillReturnsSuccess(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old"), commitResult: true}
	ctx, cancelContext := context.WithCancel(context.Background())
	operations.cancelAfterCommit = cancelContext
	if err := newFakeStore(t, operations).Replace(ctx, testDestination(t), []byte(`{}`)); err != nil {
		t.Fatalf("Replace() after known commit = %v", err)
	}
}

func TestSecureStoreKnownCommitWithDirectorySyncErrorStillReturnsSuccess(t *testing.T) {
	operations := &fakeSecureFileOperations{commitResult: true, commitErr: errors.New("directory sync failed")}
	if err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`)); err != nil {
		t.Fatalf("Replace() committed with diagnostic = %v", err)
	}
}

func TestSecureStoreInitialCreationFailureLeavesNoDestination(t *testing.T) {
	operations := &fakeSecureFileOperations{commitErr: errors.New("move failed")}
	err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`))
	requireFailureCode(t, err, persistence.FailureAtomicWrite)
	if operations.destination != nil || operations.liveTemp {
		t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
	}
}

func TestSecureStoreTempNameCollisionRetriesWithDeterministicRandomSeam(t *testing.T) {
	operations := &fakeSecureFileOperations{collisionsBeforeOpen: 2, commitResult: true}
	if err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if operations.createAttempts != 3 {
		t.Fatalf("secure temp attempts = %d, want 3", operations.createAttempts)
	}
}

func TestSecureStoreExhaustedTempNameCollisionsFailBeforePayloadWrite(t *testing.T) {
	operations := &fakeSecureFileOperations{collisionsBeforeOpen: 16}
	err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{}`))
	requireFailureCode(t, err, persistence.FailureAtomicWrite)
	if operations.temp != nil || operations.liveTemp {
		t.Fatal("payload temp exists after collision exhaustion")
	}
	if operations.createAttempts != 16 {
		t.Fatalf("secure temp attempts = %d, want 16", operations.createAttempts)
	}
}

func TestSecureStoreCommittedPostCommitFlushDiagnosticStillReturnsSuccess(t *testing.T) {
	operations := &fakeSecureFileOperations{inspectExists: true, destination: []byte("old"), commitResult: true, commitErr: errors.New("post-commit flush")}
	if err := newFakeStore(t, operations).Replace(context.Background(), testDestination(t), []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	if string(operations.destination) != "{\"new\":true}\n" || operations.liveTemp {
		t.Fatalf("destination = %q, liveTemp = %v", operations.destination, operations.liveTemp)
	}
}
