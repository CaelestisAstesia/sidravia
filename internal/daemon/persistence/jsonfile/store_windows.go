//go:build windows

package jsonfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	secureTempAttempts    = 16
	secureTempRandomBytes = 16
)

var secureTempRandom io.Reader = rand.Reader

type windowsSecureFileOperations struct{}

func init() {
	platformOperationsFactory = func() secureFileOperations { return newWindowsSecureFileOperations() }
}

func newWindowsSecureFileOperations() *windowsSecureFileOperations {
	return &windowsSecureFileOperations{}
}

func (*windowsSecureFileOperations) resolveOwner(explicitSID string) (resolvedOwner, error) {
	return resolveWindowsOwner(explicitSID)
}

func (*windowsSecureFileOperations) ensureDirectory(path string, owner resolvedOwner) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return classifyWindowsProtection(hardenWindowsPath(path, owner))
}

func (*windowsSecureFileOperations) inspectDestination(path string) (bool, error) {
	name, err := win32UTF16(path)
	if err != nil {
		return false, err
	}
	attributes, _, callErr := procGetFileAttributesW.Call(uintptr(unsafe.Pointer(name)))
	if uint32(attributes) == invalidFileAttributes {
		if callErr == syscall.ERROR_FILE_NOT_FOUND || callErr == syscall.ERROR_PATH_NOT_FOUND {
			return false, nil
		}
		return false, callErr
	}
	if uint32(attributes)&(fileAttributeDirectory|fileAttributeReparse|fileAttributeDevice) != 0 {
		return false, errorInvalidData
	}
	return true, nil
}

func (*windowsSecureFileOperations) hardenDestination(path string, owner resolvedOwner) error {
	return classifyWindowsProtection(hardenWindowsPath(path, owner))
}

func (*windowsSecureFileOperations) openForRead(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (*windowsSecureFileOperations) createTemp(directory string, owner resolvedOwner) (writableTemp, error) {
	descriptor, err := ownerSecurityDescriptor(owner)
	if err != nil {
		return nil, classifyWindowsProtection(err)
	}
	defer win32LocalFree(descriptor)
	attributes := securityAttributes{
		length:             uint32(unsafe.Sizeof(securityAttributes{})),
		securityDescriptor: descriptor,
	}
	for attempt := 0; attempt < secureTempAttempts; attempt++ {
		random := make([]byte, secureTempRandomBytes)
		if _, err := io.ReadFull(secureTempRandom, random); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, secureTempBasename(random))
		if !validDirectChild(directory, path) {
			return nil, errorInvalidName
		}
		name, err := win32UTF16(path)
		if err != nil {
			return nil, err
		}
		handle, _, callErr := procCreateFileW.Call(
			uintptr(unsafe.Pointer(name)), genericWrite, 0, uintptr(unsafe.Pointer(&attributes)),
			createNew, fileAttributeNormal, 0,
		)
		if handle == ^uintptr(0) {
			if callErr == syscall.ERROR_FILE_EXISTS || callErr == syscall.ERROR_ALREADY_EXISTS {
				continue
			}
			return nil, callErr
		}
		return &windowsWritableTemp{handle: syscall.Handle(handle), path: path}, nil
	}
	return nil, syscall.ERROR_FILE_EXISTS
}

func (*windowsSecureFileOperations) ensureUnprotectedDirectory(path string) error {
	return os.MkdirAll(path, 0700)
}

func (*windowsSecureFileOperations) createUnprotectedTemp(directory string) (writableTemp, error) {
	for attempt := 0; attempt < secureTempAttempts; attempt++ {
		random := make([]byte, secureTempRandomBytes)
		if _, err := io.ReadFull(secureTempRandom, random); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, secureTempBasename(random))
		if !validDirectChild(directory, path) {
			return nil, errorInvalidName
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return file, nil
	}
	return nil, syscall.ERROR_FILE_EXISTS
}

func classifyWindowsProtection(err error) error {
	if err == errorNotSupported || err == errorInvalidFunction || err == errorCallNotImplemented {
		return errors.Join(ProtectionUnsupported, err)
	}
	return err
}

func secureTempBasename(random []byte) string {
	return ".sidravia-" + hex.EncodeToString(random) + ".tmp"
}

func (*windowsSecureFileOperations) commit(tempPath, destinationPath string, destinationExists bool) (bool, error) {
	var err error
	if destinationExists {
		err = replaceFileWCall(destinationPath, tempPath, "", 0)
	} else {
		err = moveFileExWCall(tempPath, destinationPath, moveFileWriteThrough)
	}
	if err != nil {
		return false, err
	}
	return true, flushWindowsDestination(destinationPath)
}

func flushWindowsDestination(path string) error {
	name, err := win32UTF16(path)
	if err != nil {
		return err
	}
	handle, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(name)), genericWrite,
		fileShareRead|fileShareWrite|fileShareDelete, 0, openExisting, fileAttributeNormal, 0,
	)
	if handle == ^uintptr(0) {
		return callErr
	}
	flushErr := win32FlushFileBuffers(syscall.Handle(handle))
	closeErr := win32CloseHandle(syscall.Handle(handle))
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func (*windowsSecureFileOperations) removeTemp(path string) error {
	name, err := win32UTF16(path)
	if err != nil {
		return err
	}
	result, _, callErr := procDeleteFileW.Call(uintptr(unsafe.Pointer(name)))
	if result == 0 && callErr != syscall.ERROR_FILE_NOT_FOUND {
		return callErr
	}
	return nil
}

type windowsWritableTemp struct {
	handle syscall.Handle
	path   string
	closed bool
}

func (temp *windowsWritableTemp) Write(data []byte) (int, error) {
	if temp.closed {
		return 0, errorInvalidHandle
	}
	var written uint32
	err := syscall.WriteFile(temp.handle, data, &written, nil)
	return int(written), err
}

func (temp *windowsWritableTemp) Sync() error {
	if temp.closed {
		return errorInvalidHandle
	}
	return win32FlushFileBuffers(temp.handle)
}

func (temp *windowsWritableTemp) Close() error {
	if temp.closed {
		return nil
	}
	temp.closed = true
	return win32CloseHandle(temp.handle)
}

func (temp *windowsWritableTemp) Name() string { return temp.path }
