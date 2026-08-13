//go:build windows

package clientbootstrap

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const daemonLogLocalSystemSID = "S-1-5-18"

type windowsDaemonLogProtection struct {
	ownerSID *windows.SID
}

func newDaemonLogProtection() (daemonLogProtection, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user.User.Sid.String() == daemonLogLocalSystemSID {
		return nil, fmt.Errorf("LocalSystem 不能作为 sidraviad 日志所有者")
	}
	return windowsDaemonLogProtection{ownerSID: user.User.Sid}, nil
}

func (protection windowsDaemonLogProtection) protectDirectory(path string) error {
	return protection.apply(path, true)
}

func (protection windowsDaemonLogProtection) protectFile(path string) error {
	return protection.apply(path, false)
}

func (protection windowsDaemonLogProtection) apply(path string, directory bool) error {
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
