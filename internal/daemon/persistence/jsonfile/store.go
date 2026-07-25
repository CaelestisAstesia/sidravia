package jsonfile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"

	"sidravia/internal/daemon/persistence"
)

type Store interface {
	Read(context.Context, string, int64) (data []byte, exists bool, err error)
	Replace(context.Context, string, []byte) error
}

type SecureStoreOptions struct {
	IntendedOwnerSID string
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
	commit(tempPath string, destinationPath string, destinationExists bool) (committed bool, err error)
	removeTemp(path string) error
}

type SecureStore struct {
	operations secureFileOperations
	owner      resolvedOwner
}

func NewSecureStore(options SecureStoreOptions) (*SecureStore, error) {
	return newSecureStoreWithOperations(options, platformOperationsFactory())
}

var platformOperationsFactory = func() secureFileOperations { return nil }

func newSecureStoreWithOperations(options SecureStoreOptions, operations secureFileOperations) (*SecureStore, error) {
	if operations == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	owner, err := operations.resolveOwner(options.IntendedOwnerSID)
	if err != nil {
		return nil, persistence.NewFailure(persistence.FailurePermissionDenied, diagnosticCause(err))
	}
	return &SecureStore{operations: operations, owner: owner}, nil
}

func (store *SecureStore) Read(ctx context.Context, destination string, maximum int64) ([]byte, bool, error) {
	if !validDestination(destination) || maximum <= 0 {
		return nil, false, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}
	if err := store.operations.ensureDirectory(filepath.Dir(destination), store.owner); err != nil {
		return nil, false, operationFailure(persistence.FailurePermissionDenied, err)
	}
	exists, err := store.operations.inspectDestination(destination)
	if err != nil {
		return nil, false, operationFailure(persistence.FailurePermissionDenied, err)
	}
	if !exists {
		return nil, false, nil
	}
	if err := store.operations.hardenDestination(destination, store.owner); err != nil {
		return nil, true, operationFailure(persistence.FailurePermissionDenied, err)
	}
	reader, err := store.operations.openForRead(destination)
	if err != nil {
		return nil, true, operationFailure(persistence.FailurePermissionDenied, err)
	}
	data, readErr := ReadLimited(reader, maximum)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, true, operationFailure(persistence.FailureInvalidDocument, readErr)
	}
	if closeErr != nil {
		return nil, true, persistence.NewFailure(persistence.FailureInvalidDocument, diagnosticCause(closeErr))
	}
	if err := ctx.Err(); err != nil {
		return nil, true, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	return append([]byte(nil), data...), true, nil
}

func (store *SecureStore) Replace(ctx context.Context, destination string, data []byte) error {
	if !validDestination(destination) {
		return persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	if bytes.HasSuffix(data, []byte("\n")) {
		return persistence.NewFailure(persistence.FailureInvalidDocument, nil)
	}
	if err := validateOneValue(data); err != nil {
		return persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	if err := ctx.Err(); err != nil {
		return persistence.NewFailure(persistence.FailureAtomicWrite, err)
	}

	directory := filepath.Dir(destination)
	if err := store.operations.ensureDirectory(directory, store.owner); err != nil {
		return operationFailure(persistence.FailurePermissionDenied, err)
	}
	destinationExists, err := store.operations.inspectDestination(destination)
	if err != nil {
		return operationFailure(persistence.FailurePermissionDenied, err)
	}
	if destinationExists {
		if err := store.operations.hardenDestination(destination, store.owner); err != nil {
			return operationFailure(persistence.FailurePermissionDenied, err)
		}
	}

	temp, err := store.operations.createTemp(directory, store.owner)
	if err != nil {
		return operationFailure(persistence.FailureAtomicWrite, err)
	}
	tempPath := temp.Name()
	if !validDirectChild(directory, tempPath) {
		_ = temp.Close()
		_ = store.operations.removeTemp(tempPath)
		return persistence.NewFailure(persistence.FailureAtomicWrite, nil)
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

	payload := make([]byte, len(data)+1)
	copy(payload, data)
	payload[len(data)] = '\n'
	for len(payload) > 0 {
		written, writeErr := temp.Write(payload)
		if written < 0 || written > len(payload) {
			return persistence.NewFailure(persistence.FailureAtomicWrite, io.ErrShortWrite)
		}
		if writeErr != nil {
			return persistence.NewFailure(persistence.FailureAtomicWrite, diagnosticCause(writeErr))
		}
		if written == 0 {
			return persistence.NewFailure(persistence.FailureAtomicWrite, io.ErrShortWrite)
		}
		payload = payload[written:]
	}
	if err := temp.Sync(); err != nil {
		return persistence.NewFailure(persistence.FailureAtomicWrite, diagnosticCause(err))
	}
	closeErr := temp.Close()
	tempOpen = false
	if closeErr != nil {
		return persistence.NewFailure(persistence.FailureAtomicWrite, diagnosticCause(closeErr))
	}
	if err := ctx.Err(); err != nil {
		return persistence.NewFailure(persistence.FailureAtomicWrite, err)
	}

	committed, commitErr := store.operations.commit(tempPath, destination, destinationExists)
	if !committed {
		return persistence.NewFailure(persistence.FailureAtomicWrite, diagnosticCause(commitErr))
	}
	cleanup = false
	return nil
}

func validDestination(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func validDirectChild(directory, path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Dir(path) == directory
}

func operationFailure(defaultCode persistence.FailureCode, err error) error {
	if err == nil {
		return nil
	}
	var failure *persistence.Failure
	if errors.As(err, &failure) {
		return failure
	}
	return persistence.NewFailure(defaultCode, diagnosticCause(err))
}

func diagnosticCause(err error) error {
	if err == nil {
		return nil
	}
	var failure *persistence.Failure
	if errors.As(err, &failure) {
		return failure.DiagnosticCause()
	}
	return err
}
