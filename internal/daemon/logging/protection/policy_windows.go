//go:build windows

package protection

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const localSystemSID = "S-1-5-18"

type policy struct {
	ownerSID *windows.SID
}

// New prepares the log protection policy for the current process owner.
func New() (Policy, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user.User.Sid.String() == localSystemSID {
		return nil, fmt.Errorf("LocalSystem 不能作为 sidraviad 日志所有者")
	}
	return policy{ownerSID: user.User.Sid}, nil
}

func (protection policy) Directory(path string) error {
	return protection.apply(path, true)
}

func (protection policy) File(path string) error {
	return protection.apply(path, false)
}

func (protection policy) apply(path string, directory bool) error {
	owner := protection.ownerSID.String()
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("O:%sG:%sD:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)", owner, owner, inheritance, owner, inheritance),
	)
	if err != nil {
		return err
	}
	ownerSID, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	groupSID, _, err := descriptor.Group()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	information := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, information, nil, nil, dacl, nil); err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		information|windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION,
		ownerSID,
		groupSID,
		dacl,
		nil,
	)
}
