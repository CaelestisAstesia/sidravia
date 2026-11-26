//go:build windows

package shellintegration

import (
	"errors"
	"io/fs"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type registryPath struct{}

const pathOwnershipKey = `Software\CaelestisAstesia\Sidravia\PathOwnership`

var (
	ktm                     = windows.NewLazySystemDLL("ktmw32.dll")
	createPathTransaction   = ktm.NewProc("CreateTransaction")
	commitPathTransaction   = ktm.NewProc("CommitTransaction")
	advapi                  = windows.NewLazySystemDLL("advapi32.dll")
	openPathTransactedKey   = advapi.NewProc("RegOpenKeyTransactedW")
	createPathTransactedKey = advapi.NewProc("RegCreateKeyTransactedW")
)

func readPathValue(key registry.Key) (pathValue, error) {
	text, kind, err := key.GetStringValue("Path")
	if errors.Is(err, fs.ErrNotExist) {
		return pathValue{}, nil
	}
	if err != nil {
		return pathValue{}, err
	}
	if kind != registry.SZ && kind != registry.EXPAND_SZ {
		return pathValue{}, errors.New("unsupported PATH registry type")
	}
	return pathValue{text: text, kind: kind, exists: true}, nil
}

func readPathOwnership(key registry.Key, directory string) (pathOwnership, error) {
	n, kind, err := key.GetIntegerValue(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return pathUnowned, nil
	}
	if err != nil {
		return pathUnowned, err
	}
	if kind != registry.DWORD || (n != uint64(pathAppended) && n != uint64(pathCreated)) {
		return pathUnowned, errors.New("unrecognized PATH ownership record")
	}
	return pathOwnership(n), nil
}

func (registryPath) read() (pathValue, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return pathValue{}, nil
	}
	if err != nil {
		return pathValue{}, err
	}
	defer key.Close()
	return readPathValue(key)
}

func (registryPath) ownership(directory string) (pathOwnership, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, pathOwnershipKey, registry.QUERY_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return pathUnowned, nil
	}
	if err != nil {
		return pathUnowned, err
	}
	defer key.Close()
	return readPathOwnership(key, directory)
}

func transactedPathKey(transaction windows.Handle, name string, create bool) (registry.Key, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	const access = registry.QUERY_VALUE | registry.SET_VALUE
	var key registry.Key
	var status uintptr
	if create {
		status, _, _ = createPathTransactedKey.Call(uintptr(registry.CURRENT_USER), uintptr(unsafe.Pointer(path)), 0,
			0, 0, access, 0, uintptr(unsafe.Pointer(&key)), 0, uintptr(transaction), 0)
	} else {
		status, _, _ = openPathTransactedKey.Call(uintptr(registry.CURRENT_USER), uintptr(unsafe.Pointer(path)), 0,
			access, uintptr(unsafe.Pointer(&key)), uintptr(transaction), 0)
	}
	if status != 0 {
		return 0, syscall.Errno(status)
	}
	return key, nil
}

func checkTransactedPath(key registry.Key, before pathValue) error {
	if key == 0 {
		if before.exists {
			return errPathChanged
		}
		return nil
	}
	current, err := readPathValue(key)
	if err != nil {
		return err
	}
	if current != before {
		return errPathChanged
	}
	return nil
}

func checkTransactedOwnership(key registry.Key, directory string, old pathOwnership) error {
	if key == 0 {
		if old != pathUnowned {
			return errPathChanged
		}
		return nil
	}
	current, err := readPathOwnership(key, directory)
	if err != nil {
		return err
	}
	if current != old {
		return errPathChanged
	}
	return nil
}

func writeTransactedPath(key registry.Key, value pathValue) error {
	if !value.exists {
		return key.DeleteValue("Path")
	}
	if value.kind == registry.EXPAND_SZ {
		return key.SetExpandStringValue("Path", value.text)
	}
	return key.SetStringValue("Path", value.text)
}

func writeTransactedOwnership(key registry.Key, directory string, next pathOwnership) error {
	if next == pathUnowned {
		return key.DeleteValue(directory)
	}
	return key.SetDWordValue(directory, uint32(next))
}

func startPathTransaction() (windows.Handle, error) {
	for _, api := range []*windows.LazyProc{createPathTransaction, commitPathTransaction, openPathTransactedKey, createPathTransactedKey} {
		if err := api.Find(); err != nil {
			return windows.InvalidHandle, err
		}
	}
	handle, _, err := createPathTransaction.Call(0, 0, 0, 0, 0, 10000, 0)
	if windows.Handle(handle) == windows.InvalidHandle {
		return windows.InvalidHandle, err
	}
	return windows.Handle(handle), nil
}

func finishPathTransaction(transaction windows.Handle) error {
	if result, _, err := commitPathTransaction.Call(uintptr(transaction)); result == 0 {
		return err
	}
	return nil
}

func (registryPath) replace(directory string, before, desired pathValue, old, next pathOwnership) error {
	// One transaction commits the registry value and the ownership together.
	// Closing the transaction on any failure rolls both back.
	transaction, err := startPathTransaction()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(transaction)
	environment, err := transactedPathKey(transaction, "Environment", desired.exists && !before.exists)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if environment != 0 {
		defer environment.Close()
	}
	if err = checkTransactedPath(environment, before); err != nil {
		return err
	}
	ownership, err := transactedPathKey(transaction, pathOwnershipKey, old == pathUnowned && next != pathUnowned)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if ownership != 0 {
		defer ownership.Close()
	}
	if err = checkTransactedOwnership(ownership, directory, old); err != nil {
		return err
	}
	if desired != before {
		if err = writeTransactedPath(environment, desired); err != nil {
			return err
		}
	}
	if next != old {
		if err = writeTransactedOwnership(ownership, directory, next); err != nil {
			return err
		}
	}
	return finishPathTransaction(transaction)
}
