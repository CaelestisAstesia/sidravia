//go:build windows

package jsonfile

import (
	"fmt"
	"syscall"
	"unsafe"

	"sidravia/internal/daemon/persistence"
)

const localSystemSID = "S-1-5-18"

func currentTokenOwnerSID() (string, error) {
	var token syscall.Handle
	result, _, callErr := procOpenProcessToken.Call(^uintptr(0), tokenQuery, uintptr(unsafe.Pointer(&token)))
	if result == 0 {
		return "", callErr
	}
	defer win32CloseHandle(token)

	var required uint32
	result, _, firstErr := procGetTokenInformation.Call(uintptr(token), tokenUser, 0, 0, uintptr(unsafe.Pointer(&required)))
	if result != 0 || firstErr != syscall.ERROR_INSUFFICIENT_BUFFER || required < uint32(unsafe.Sizeof(uintptr(0))) {
		if result == 0 {
			return "", firstErr
		}
		return "", errorInvalidData
	}
	buffer := make([]byte, required)
	result, _, callErr = procGetTokenInformation.Call(
		uintptr(token), tokenUser, uintptr(unsafe.Pointer(&buffer[0])), uintptr(required), uintptr(unsafe.Pointer(&required)),
	)
	if result == 0 {
		return "", callErr
	}
	sid := *(*unsafe.Pointer)(unsafe.Pointer(&buffer[0]))
	return win32SIDString(sid)
}

func canonicalSID(value string) (string, error) {
	sid, err := win32StringSID(value)
	if err != nil {
		return "", err
	}
	defer win32LocalFree(sid)
	return win32SIDString(sid)
}

func isLocalSystemSID(value string) (bool, error) {
	left, err := win32StringSID(value)
	if err != nil {
		return false, err
	}
	defer win32LocalFree(left)
	right, err := win32StringSID(localSystemSID)
	if err != nil {
		return false, err
	}
	defer win32LocalFree(right)
	return win32EqualSID(left, right), nil
}

func resolveWindowsOwner(explicitSID string) (resolvedOwner, error) {
	value := explicitSID
	var err error
	if value == "" {
		value, err = currentTokenOwnerSID()
		if err != nil {
			return resolvedOwner{}, persistence.NewFailure(persistence.FailurePermissionDenied, err)
		}
	}
	value, err = canonicalSID(value)
	if err != nil {
		return resolvedOwner{}, persistence.NewFailure(persistence.FailurePermissionDenied, err)
	}
	if explicitSID == "" {
		system, compareErr := isLocalSystemSID(value)
		if compareErr != nil {
			return resolvedOwner{}, persistence.NewFailure(persistence.FailurePermissionDenied, compareErr)
		}
		if system {
			return resolvedOwner{}, persistence.NewFailure(persistence.FailurePermissionDenied, nil)
		}
	}
	return resolvedOwner{sid: value}, nil
}

// directorySecurityDescriptor builds a protected DACL for a secured directory.
// The DACL is protected so the directory does not inherit permissive ACEs from
// its parent, and carries exactly two explicit allow ACEs (intended owner and
// LocalSystem, both Full Control). Both ACEs carry object-inherit and
// container-inherit flags so Windows applies them to existing and future child
// files and directories when SetNamedSecurityInfoW installs the descriptor.
func directorySecurityDescriptor(owner resolvedOwner) (unsafe.Pointer, error) {
	if owner.sid == "" {
		return nil, errorInvalidSID
	}
	sddl := fmt.Sprintf("O:%sG:%sD:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)", owner.sid, owner.sid, owner.sid)
	return win32SecurityDescriptor(sddl)
}

// fileSecurityDescriptor builds a protected DACL for a secured file. The DACL
// is protected and carries exactly two non-inheritable allow ACEs (intended
// owner and LocalSystem, both Full Control). A file must not propagate ACEs to
// children, so its ACEs carry no object/container inheritance flags.
func fileSecurityDescriptor(owner resolvedOwner) (unsafe.Pointer, error) {
	if owner.sid == "" {
		return nil, errorInvalidSID
	}
	sddl := fmt.Sprintf("O:%sG:%sD:P(A;;FA;;;%s)(A;;FA;;;SY)", owner.sid, owner.sid, owner.sid)
	return win32SecurityDescriptor(sddl)
}

// applyProtectedSecurityDescriptor installs a protected owner/group/DACL on the
// named object. An object's owner implicitly has WRITE_DAC, but not WRITE_OWNER,
// so the protected owner-only DACL is installed first to authorize the checked
// owner/group update that follows.
func applyProtectedSecurityDescriptor(path string, descriptor unsafe.Pointer) error {
	defer win32LocalFree(descriptor)
	ownerSID, groupSID, dacl, err := win32DescriptorParts(descriptor)
	if err != nil {
		return err
	}
	if err := setNamedSecurityInfoWCall(
		path, seFileObject, daclSecurityInfo|protectedDaclInfo, nil, nil, dacl,
	); err != nil {
		return err
	}
	return setNamedSecurityInfoWCall(
		path, seFileObject,
		ownerSecurityInfo|groupSecurityInfo|daclSecurityInfo|protectedDaclInfo,
		ownerSID, groupSID, dacl,
	)
}

func hardenWindowsDirectory(path string, owner resolvedOwner) error {
	descriptor, err := directorySecurityDescriptor(owner)
	if err != nil {
		return err
	}
	return applyProtectedSecurityDescriptor(path, descriptor)
}

func hardenWindowsFile(path string, owner resolvedOwner) error {
	descriptor, err := fileSecurityDescriptor(owner)
	if err != nil {
		return err
	}
	return applyProtectedSecurityDescriptor(path, descriptor)
}

type windowsACE struct {
	sid   string
	mask  uint32
	flags uint8
}

type windowsSecuritySnapshot struct {
	ownerSID      string
	groupSID      string
	daclProtected bool
	allowACECount int
	allowACEs     map[string]uint32
	allowACEFlags map[string]uint8
	allowACEList  []windowsACE
}

func inspectWindowsSecurity(path string) (windowsSecuritySnapshot, error) {
	descriptor, owner, group, dacl, err := win32GetNamedSecurity(path)
	if err != nil {
		return windowsSecuritySnapshot{}, err
	}
	defer win32LocalFree(descriptor)
	ownerText, err := win32SIDString(owner)
	if err != nil {
		return windowsSecuritySnapshot{}, err
	}
	groupText, err := win32SIDString(group)
	if err != nil {
		return windowsSecuritySnapshot{}, err
	}
	var control uint16
	var revision uint32
	result, _, callErr := procGetSDControl.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if result == 0 {
		return windowsSecuritySnapshot{}, callErr
	}
	var information aclSizeInformationData
	result, _, callErr = procGetAclInformation.Call(
		uintptr(dacl), uintptr(unsafe.Pointer(&information)), unsafe.Sizeof(information), aclSizeInformation,
	)
	if result == 0 {
		return windowsSecuritySnapshot{}, callErr
	}
	allow := make(map[string]uint32)
	allowFlags := make(map[string]uint8)
	allowList := make([]windowsACE, 0, int(information.aceCount))
	allowCount := 0
	for index := uint32(0); index < information.aceCount; index++ {
		var ace unsafe.Pointer
		result, _, callErr = procGetAce.Call(uintptr(dacl), uintptr(index), uintptr(unsafe.Pointer(&ace)))
		if result == 0 {
			return windowsSecuritySnapshot{}, callErr
		}
		if *(*byte)(ace) != accessAllowedAceType {
			continue
		}
		allowCount++
		flags := *(*uint8)(unsafe.Add(ace, 1))
		mask := *(*uint32)(unsafe.Add(ace, 4))
		sidText, sidErr := win32SIDString(unsafe.Add(ace, 8))
		if sidErr != nil {
			return windowsSecuritySnapshot{}, sidErr
		}
		allow[sidText] = mask
		allowFlags[sidText] = flags
		allowList = append(allowList, windowsACE{sid: sidText, mask: mask, flags: flags})
	}
	return windowsSecuritySnapshot{
		ownerSID: ownerText, groupSID: groupText,
		daclProtected: control&seDaclProtected != 0,
		allowACECount: allowCount,
		allowACEs:     allow,
		allowACEFlags: allowFlags,
		allowACEList:  allowList,
	}, nil
}
