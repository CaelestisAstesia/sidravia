package securefile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"

	"sidravia/internal/document"
	"sidravia/internal/document/jsondoc"
)

type Store interface {
	Read(context.Context, string, int64) (data []byte, exists bool, err error)
	Replace(context.Context, string, []byte) error
}

// SensitiveStore exposes the protected aggregate operations needed by a document owner.
type SensitiveStore interface {
	Store
	ReplaceSensitive(context.Context, string, []byte, int64, bool) error
	PreserveSensitive(context.Context, string, []byte, int64, bool) error
	ProtectionStatus() ProtectionStatus
}

type SecureStoreOptions struct {
	IntendedOwnerSID                   string
	AllowUnsupportedProtectionFallback bool
	OnUnprotected                      func()
	// OnPostCommitDiagnostic observes an original error after known commit, without
	// changing successful writes. It runs synchronously, possibly under a caller's
	// Catalog lock. It must return promptly, must not panic, reenter Store/Catalog,
	// perform product operations or wait for external work. Nil disables observation.
	OnPostCommitDiagnostic func(error)
}

type writableTemp interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

type resolvedOwner struct {
	sid string
}

type secureFileOperations interface {
	resolveOwner(explicitSID string) (resolvedOwner, error)
	ensureDirectory(path string, owner resolvedOwner) error
	inspectDestination(path string) (exists bool, err error)
	hardenDestination(path string, owner resolvedOwner) error
	openForRead(path string) (io.ReadCloser, error)
	createTemp(directory string, owner resolvedOwner) (writableTemp, error)
	ensureUnprotectedDirectory(path string) error
	createUnprotectedTemp(directory string) (writableTemp, error)
	commit(tempPath string, destinationPath string, destinationExists bool) (committed bool, err error)
	removeTemp(path string) error
}

type SecureStore struct {
	operations             secureFileOperations
	owner                  resolvedOwner
	mu                     sync.Mutex
	protection             ProtectionStatus
	allowFallback          bool
	onUnprotected          func()
	onPostCommitDiagnostic func(error)
}

func NewSecureStore(options SecureStoreOptions) (*SecureStore, error) {
	return newSecureStoreWithOperations(options, platformOperationsFactory())
}

var platformOperationsFactory = func() secureFileOperations { return nil }

func newSecureStoreWithOperations(options SecureStoreOptions, operations secureFileOperations) (*SecureStore, error) {
	if operations == nil {
		return nil, document.NewFailure(document.FailureInvalidArgument, nil)
	}
	owner, err := operations.resolveOwner(options.IntendedOwnerSID)
	if err != nil {
		return nil, document.NewFailure(document.FailurePermissionDenied, diagnosticCause(err))
	}
	return &SecureStore{
		operations:             operations,
		owner:                  owner,
		protection:             ProtectionProtected,
		allowFallback:          options.AllowUnsupportedProtectionFallback,
		onUnprotected:          options.OnUnprotected,
		onPostCommitDiagnostic: options.OnPostCommitDiagnostic,
	}, nil
}

func (store *SecureStore) ProtectionStatus() ProtectionStatus {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.protection
}

// persistedTrailingBytes is the exact number of bytes replace appends to the
// caller's document. The persisted size of a replacement is therefore
// len(data)+persistedTrailingBytes, and a size limit must be applied to those
// bytes so that any successful replacement can be read back with the same
// limit.
const persistedTrailingBytes = 1

func persistedSize(data []byte) int64 {
	return int64(len(data)) + persistedTrailingBytes
}

// ReplaceSensitive replaces the destination with data plus the persisted
// trailing bytes. A maximum above zero is enforced against those exact bytes
// before any directory, temporary file, protection call or replacement, so a
// rejected document has no filesystem effect. Zero means no limit.
func (store *SecureStore) ReplaceSensitive(ctx context.Context, destination string, data []byte, maximum int64, allowUnprotected bool) error {
	if maximum > 0 && persistedSize(data) > maximum {
		return document.NewFailure(document.FailureSizeLimitExceeded, nil)
	}
	return store.replace(ctx, destination, data, true, allowUnprotected, false)
}

// PreserveSensitive creates a directly restorable migration copy using the same
// protection and atomic-write policy, without normalizing the original bytes.
func (store *SecureStore) PreserveSensitive(ctx context.Context, destination string, data []byte, maximum int64, allowUnprotected bool) error {
	if maximum <= 0 || int64(len(data)) > maximum {
		return document.NewFailure(document.FailureSizeLimitExceeded, nil)
	}
	return store.replace(ctx, destination, data, true, allowUnprotected, true)
}

func (store *SecureStore) Read(ctx context.Context, destination string, maximum int64) ([]byte, bool, error) {
	if !validDestination(destination) || maximum <= 0 {
		return nil, false, document.NewFailure(document.FailureInvalidArgument, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, document.NewFailure(document.FailureInvalidArgument, err)
	}
	if err := store.prepareDirectory(filepath.Dir(destination), true); err != nil {
		return nil, false, operationFailure(document.FailurePermissionDenied, err)
	}
	exists, err := store.operations.inspectDestination(destination)
	if err != nil {
		return nil, false, operationFailure(document.FailurePermissionDenied, err)
	}
	if !exists {
		return nil, false, nil
	}
	if store.ProtectionStatus() == ProtectionProtected {
		if err := store.operations.hardenDestination(destination, store.owner); err != nil {
			if !store.enterUnprotected(err, true) {
				return nil, true, operationFailure(document.FailurePermissionDenied, err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, true, operationFailure(document.FailurePermissionDenied, err)
	}
	reader, err := store.operations.openForRead(destination)
	if err != nil {
		return nil, true, operationFailure(document.FailurePermissionDenied, err)
	}
	data, readErr := jsondoc.ReadLimited(reader, maximum)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, true, operationFailure(document.FailureInvalidDocument, readErr)
	}
	if closeErr != nil {
		return nil, true, document.NewFailure(document.FailureInvalidDocument, diagnosticCause(closeErr))
	}
	if err := ctx.Err(); err != nil {
		return nil, true, document.NewFailure(document.FailureInvalidDocument, err)
	}
	return append([]byte(nil), data...), true, nil
}

func (store *SecureStore) Replace(ctx context.Context, destination string, data []byte) error {
	return store.replace(ctx, destination, data, false, true, false)
}

func (store *SecureStore) replace(ctx context.Context, destination string, data []byte, sensitive, allowUnprotected, exactCopy bool) error {
	if !validDestination(destination) {
		return document.NewFailure(document.FailureInvalidArgument, nil)
	}
	if !exactCopy && bytes.HasSuffix(data, []byte("\n")) {
		return document.NewFailure(document.FailureInvalidDocument, nil)
	}
	if err := jsondoc.ValidateValue(data); err != nil {
		return document.NewFailure(document.FailureInvalidDocument, err)
	}
	if err := ctx.Err(); err != nil {
		return document.NewFailure(document.FailureAtomicWrite, err)
	}

	directory := filepath.Dir(destination)
	if sensitive && store.ProtectionStatus() == ProtectionUnprotected && !allowUnprotected {
		return insecureStorageFailure()
	}
	if err := store.prepareDirectory(directory, !sensitive || allowUnprotected); err != nil {
		if sensitive && !allowUnprotected && store.allowFallback && errors.Is(err, ProtectionUnsupported) {
			return insecureStorageFailure()
		}
		return operationFailure(document.FailurePermissionDenied, err)
	}
	destinationExists, err := store.operations.inspectDestination(destination)
	if err != nil {
		return operationFailure(document.FailurePermissionDenied, err)
	}
	if exactCopy && destinationExists {
		return document.NewFailure(document.FailureConflict, nil)
	}
	if destinationExists && store.ProtectionStatus() == ProtectionProtected {
		if err := store.operations.hardenDestination(destination, store.owner); err != nil {
			if !store.enterUnprotected(err, !sensitive || allowUnprotected) {
				if errors.Is(err, ProtectionUnsupported) && sensitive && !allowUnprotected && store.allowFallback {
					return insecureStorageFailure()
				}
				return operationFailure(document.FailurePermissionDenied, err)
			}
		}
	}

	var temp writableTemp
	if store.ProtectionStatus() == ProtectionUnprotected {
		if sensitive && !allowUnprotected {
			return insecureStorageFailure()
		}
		temp, err = store.operations.createUnprotectedTemp(directory)
	} else {
		temp, err = store.operations.createTemp(directory, store.owner)
		if err != nil && errors.Is(err, ProtectionUnsupported) {
			if sensitive && !allowUnprotected && store.allowFallback {
				return insecureStorageFailure()
			}
			if store.enterUnprotected(err, !sensitive || allowUnprotected) {
				temp, err = store.operations.createUnprotectedTemp(directory)
			}
		}
	}
	if err != nil {
		return operationFailure(document.FailureAtomicWrite, err)
	}
	tempPath := temp.Name()
	if !validDirectChild(directory, tempPath) {
		_ = temp.Close()
		_ = store.operations.removeTemp(tempPath)
		return document.NewFailure(document.FailureAtomicWrite, nil)
	}
	cleanup := true
	tempOpen := true
	defer func() {
		if cleanup {
			if tempOpen {
				_ = temp.Close()
			}
			_ = store.operations.removeTemp(tempPath)
		}
	}()

	payload := append([]byte(nil), data...)
	if !exactCopy {
		payload = append(payload, '\n')
	}
	for len(payload) > 0 {
		written, writeErr := temp.Write(payload)
		if written < 0 || written > len(payload) {
			return document.NewFailure(document.FailureAtomicWrite, io.ErrShortWrite)
		}
		if writeErr != nil {
			return document.NewFailure(document.FailureAtomicWrite, diagnosticCause(writeErr))
		}
		if written == 0 {
			return document.NewFailure(document.FailureAtomicWrite, io.ErrShortWrite)
		}
		payload = payload[written:]
	}
	if err := temp.Sync(); err != nil {
		return document.NewFailure(document.FailureAtomicWrite, diagnosticCause(err))
	}
	closeErr := temp.Close()
	tempOpen = false
	if closeErr != nil {
		return document.NewFailure(document.FailureAtomicWrite, diagnosticCause(closeErr))
	}
	if err := ctx.Err(); err != nil {
		return document.NewFailure(document.FailureAtomicWrite, err)
	}

	committed, commitErr := store.operations.commit(tempPath, destination, destinationExists)
	if !committed {
		return document.NewFailure(document.FailureAtomicWrite, diagnosticCause(commitErr))
	}
	cleanup = false
	if commitErr != nil && store.onPostCommitDiagnostic != nil {
		store.onPostCommitDiagnostic(commitErr)
	}
	return nil
}

func (store *SecureStore) prepareDirectory(directory string, allowTransition bool) error {
	if store.ProtectionStatus() == ProtectionUnprotected {
		return store.operations.ensureUnprotectedDirectory(directory)
	}
	err := store.operations.ensureDirectory(directory, store.owner)
	if err == nil {
		return nil
	}
	if !store.enterUnprotected(err, allowTransition) {
		return err
	}
	return store.operations.ensureUnprotectedDirectory(directory)
}

func (store *SecureStore) enterUnprotected(err error, allowTransition bool) bool {
	if !allowTransition || !store.allowFallback || !errors.Is(err, ProtectionUnsupported) {
		return false
	}
	store.mu.Lock()
	if store.protection == ProtectionUnprotected {
		store.mu.Unlock()
		return true
	}
	store.protection = ProtectionUnprotected
	callback := store.onUnprotected
	store.mu.Unlock()
	if callback != nil {
		callback()
	}
	return true
}

func insecureStorageFailure() error {
	return document.NewFailure(document.FailurePermissionDenied, ErrInsecureStorageConfirmationRequired)
}

func validDestination(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func validDirectChild(directory, path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Dir(path) == directory
}

func operationFailure(defaultCode document.FailureCode, err error) error {
	if err == nil {
		return nil
	}
	var failure *document.Failure
	if errors.As(err, &failure) {
		return failure
	}
	return document.NewFailure(defaultCode, diagnosticCause(err))
}

func diagnosticCause(err error) error {
	if err == nil {
		return nil
	}
	var failure *document.Failure
	if errors.As(err, &failure) {
		return failure.DiagnosticCause()
	}
	return err
}
