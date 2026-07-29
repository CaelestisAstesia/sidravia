//go:build windows

package jsonfile

import (
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenProcessToken       = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation    = advapi32.NewProc("GetTokenInformation")
	procConvertSidToStringSidW = advapi32.NewProc("ConvertSidToStringSidW")
	procConvertStringSidToSidW = advapi32.NewProc("ConvertStringSidToSidW")
	procEqualSid               = advapi32.NewProc("EqualSid")
	procConvertStringSDToSDW   = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procGetNamedSecurityInfoW  = advapi32.NewProc("GetNamedSecurityInfoW")
	procSetNamedSecurityInfoW  = advapi32.NewProc("SetNamedSecurityInfoW")
	procGetSDControl           = advapi32.NewProc("GetSecurityDescriptorControl")
	procGetSDOwner             = advapi32.NewProc("GetSecurityDescriptorOwner")
	procGetSDGroup             = advapi32.NewProc("GetSecurityDescriptorGroup")
	procGetSDDacl              = advapi32.NewProc("GetSecurityDescriptorDacl")
	procGetAclInformation      = advapi32.NewProc("GetAclInformation")
	procGetAce                 = advapi32.NewProc("GetAce")
	procCreateFileW            = kernel32.NewProc("CreateFileW")
	procFlushFileBuffers       = kernel32.NewProc("FlushFileBuffers")
	procCloseHandle            = kernel32.NewProc("CloseHandle")
	procReplaceFileW           = kernel32.NewProc("ReplaceFileW")
	procMoveFileExW            = kernel32.NewProc("MoveFileExW")
	procGetFileAttributesW     = kernel32.NewProc("GetFileAttributesW")
	procDeleteFileW            = kernel32.NewProc("DeleteFileW")
	procLocalFree              = kernel32.NewProc("LocalFree")
)

const (
	tokenQuery              = 0x0008
	tokenUser               = 1
	sddlRevision1           = 1
	seFileObject            = 1
	ownerSecurityInfo       = 0x00000001
	groupSecurityInfo       = 0x00000002
	daclSecurityInfo        = 0x00000004
	protectedDaclInfo       = 0x80000000
	seDaclProtected         = 0x1000
	aclSizeInformation      = 2
	accessAllowedAceType    = 0
	fileAllAccess           = 0x001F01FF
	genericWrite            = 0x40000000
	fileShareRead           = 0x00000001
	fileShareWrite          = 0x00000002
	fileShareDelete         = 0x00000004
	createNew               = 1
	openExisting            = 3
	fileAttributeNormal     = 0x00000080
	fileAttributeDirectory  = 0x00000010
	fileAttributeReparse    = 0x00000400
	fileAttributeDevice     = 0x00000040
	invalidFileAttributes   = 0xFFFFFFFF
	moveFileWriteThrough    = 0x00000008
	errorInvalidFunction    = syscall.Errno(1)
	errorInvalidHandle      = syscall.Errno(6)
	errorInvalidData        = syscall.Errno(13)
	errorNotSupported       = syscall.Errno(50)
	errorInvalidParameter   = syscall.Errno(87)
	errorCallNotImplemented = syscall.Errno(120)
	errorInvalidName        = syscall.Errno(123)
	errorInvalidSID         = syscall.Errno(1337)
	errorInvalidSecurity    = syscall.Errno(1338)
)

type securityAttributes struct {
	length             uint32
	securityDescriptor unsafe.Pointer
	inheritHandle      int32
}

type aclSizeInformationData struct {
	aceCount      uint32
	aclBytesInUse uint32
	aclBytesFree  uint32
}

func win32UTF16(value string) (*uint16, error) { return syscall.UTF16PtrFromString(value) }

func win32CloseHandle(handle syscall.Handle) error {
	result, _, callErr := procCloseHandle.Call(uintptr(handle))
	if result == 0 {
		return callErr
	}
	return nil
}

func win32FlushFileBuffers(handle syscall.Handle) error {
	result, _, callErr := procFlushFileBuffers.Call(uintptr(handle))
	if result == 0 {
		return callErr
	}
	return nil
}

func win32LocalFree(allocation unsafe.Pointer) {
	if allocation != nil {
		procLocalFree.Call(uintptr(allocation))
	}
}

func win32SIDString(sid unsafe.Pointer) (string, error) {
	var stringSID unsafe.Pointer
	result, _, callErr := procConvertSidToStringSidW.Call(uintptr(sid), uintptr(unsafe.Pointer(&stringSID)))
	if result == 0 {
		return "", callErr
	}
	defer win32LocalFree(stringSID)
	return utf16PointerString((*uint16)(stringSID)), nil
}

func win32StringSID(value string) (unsafe.Pointer, error) {
	text, err := win32UTF16(value)
	if err != nil {
		return nil, err
	}
	var sid unsafe.Pointer
	result, _, callErr := procConvertStringSidToSidW.Call(uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(&sid)))
	if result == 0 {
		return nil, callErr
	}
	return sid, nil
}

func win32EqualSID(left, right unsafe.Pointer) bool {
	result, _, _ := procEqualSid.Call(uintptr(left), uintptr(right))
	return result != 0
}

func utf16PointerString(pointer *uint16) string {
	if pointer == nil {
		return ""
	}
	length := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(pointer), uintptr(length)*2)) != 0 {
		length++
	}
	return syscall.UTF16ToString(unsafe.Slice(pointer, length))
}

func win32SecurityDescriptor(sddl string) (unsafe.Pointer, error) {
	text, err := win32UTF16(sddl)
	if err != nil {
		return nil, err
	}
	var descriptor unsafe.Pointer
	result, _, callErr := procConvertStringSDToSDW.Call(
		uintptr(unsafe.Pointer(text)), sddlRevision1, uintptr(unsafe.Pointer(&descriptor)), 0,
	)
	if result == 0 {
		return nil, callErr
	}
	return descriptor, nil
}

func win32DescriptorParts(descriptor unsafe.Pointer) (owner, group, dacl unsafe.Pointer, err error) {
	var ownerDefaulted, groupDefaulted, daclPresent, daclDefaulted int32
	result, _, callErr := procGetSDOwner.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&ownerDefaulted)))
	if result == 0 {
		return nil, nil, nil, callErr
	}
	result, _, callErr = procGetSDGroup.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&group)), uintptr(unsafe.Pointer(&groupDefaulted)))
	if result == 0 {
		return nil, nil, nil, callErr
	}
	result, _, callErr = procGetSDDacl.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&daclPresent)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&daclDefaulted)))
	if result == 0 {
		return nil, nil, nil, callErr
	}
	if daclPresent == 0 || dacl == nil {
		return nil, nil, nil, errorInvalidSecurity
	}
	return owner, group, dacl, nil
}

func win32GetNamedSecurity(path string) (descriptor, owner, group, dacl unsafe.Pointer, err error) {
	name, err := win32UTF16(path)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	result, _, _ := procGetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, ownerSecurityInfo|groupSecurityInfo|daclSecurityInfo,
		uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)), uintptr(unsafe.Pointer(&dacl)), 0,
		uintptr(unsafe.Pointer(&descriptor)),
	)
	if result != 0 {
		return nil, nil, nil, nil, syscall.Errno(result)
	}
	return descriptor, owner, group, dacl, nil
}

func rawSetNamedSecurityInfoW(path string, objectType, information uintptr, owner, group, dacl unsafe.Pointer) error {
	name, err := win32UTF16(path)
	if err != nil {
		return err
	}
	result, _, _ := procSetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(name)), objectType, information, uintptr(owner), uintptr(group), uintptr(dacl), 0,
	)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}

var setNamedSecurityInfoWCall = rawSetNamedSecurityInfoW

func rawReplaceFileW(destination, replacement, backup string, flags uint32) error {
	destinationPointer, err := win32UTF16(destination)
	if err != nil {
		return err
	}
	replacementPointer, err := win32UTF16(replacement)
	if err != nil {
		return err
	}
	var backupPointer *uint16
	if backup != "" {
		backupPointer, err = win32UTF16(backup)
		if err != nil {
			return err
		}
	}
	result, _, callErr := procReplaceFileW.Call(
		uintptr(unsafe.Pointer(destinationPointer)), uintptr(unsafe.Pointer(replacementPointer)),
		uintptr(unsafe.Pointer(backupPointer)), uintptr(flags), 0, 0,
	)
	if result == 0 {
		return callErr
	}
	return nil
}

var replaceFileWCall = rawReplaceFileW

func rawMoveFileExW(existing, destination string, flags uint32) error {
	existingPointer, err := win32UTF16(existing)
	if err != nil {
		return err
	}
	destinationPointer, err := win32UTF16(destination)
	if err != nil {
		return err
	}
	result, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(existingPointer)), uintptr(unsafe.Pointer(destinationPointer)), uintptr(flags),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

var moveFileExWCall = rawMoveFileExW
