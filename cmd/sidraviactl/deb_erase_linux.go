package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/userservice"
)

const debEraseState = "/var/lib/sidravia/deb-maintenance"
const debEraseMessagePrefix = "sidravia-deb-erase-v1"

func admitDebErase(packageRoot string) error {
	parent, uid := os.Getppid(), os.Geteuid()
	if parent <= 1 || uid == 0 {
		return userservice.ErrServiceConflict
	}
	fd, err := strconv.Atoi(os.Getenv("SIDRAVIA_DEB_ERASE_AUTH_FD"))
	if err != nil || fd < 3 {
		return userservice.ErrServiceConflict
	}
	unix.CloseOnExec(fd)
	defer unix.Close(fd)

	if err := readDebEraseAuthorization(fd, parent, uid, 0); err != nil {
		return err
	}
	executable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return err
	}
	executableInfo, err := os.Stat("/proc/self/exe")
	if err != nil {
		return err
	}
	if os.Getppid() != parent {
		return userservice.ErrServiceConflict
	}
	return verifyDebErase(packageRoot, debEraseState, executable, executableInfo, 0)
}

func readDebEraseAuthorization(fd, parent, uid int, rootUID uint32) error {
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil || domain != unix.AF_UNIX {
		return userservice.ErrServiceConflict
	}
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_SEQPACKET {
		return userservice.ErrServiceConflict
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Uid != rootUID || int(peer.Pid) != parent {
		return userservice.ErrServiceConflict
	}

	// The root parent attests that this account is eligible for the active
	// removal. The packet conveys only the package and this UID.
	var packet [64]byte
	n, _, flags, _, err := unix.Recvmsg(fd, packet[:], nil, unix.MSG_DONTWAIT)
	if err != nil || n == 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return userservice.ErrServiceConflict
	}
	version, tail, found := strings.Cut(string(packet[:n]), "\x00")
	pkg, account, delimited := strings.Cut(tail, "\x00")
	if !found || !delimited || version != debEraseMessagePrefix ||
		(pkg != "sidravia" && pkg != "sidravia-core") || account != strconv.Itoa(uid)+"\x00" {
		return userservice.ErrServiceConflict
	}
	// A pre-authorized endpoint has no further packets and its write half is
	// already closed. Do not let a malformed peer block a dpkg callback.
	n, _, flags, _, err = unix.Recvmsg(fd, packet[:], nil, unix.MSG_DONTWAIT)
	if err != nil || n != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return userservice.ErrServiceConflict
	}
	return nil
}

func ownedDebEraseNode(info os.FileInfo, owner uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == owner && info.Mode().Perm()&0022 == 0
}

func trustedDebEraseFile(info os.FileInfo, owner uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && info.Mode().IsRegular() && ownedDebEraseNode(info, owner)
}

func verifyDebErase(root, state, executable string, exe os.FileInfo, owner uint32) error {
	erase := filepath.Join(state, "erase")
	if executable != filepath.Join(erase, "sidraviactl") || !trustedDebEraseFile(exe, owner) || exe.Mode().Perm() != 0755 {
		return userservice.ErrServiceConflict
	}
	for _, dir := range []string{filepath.Dir(state), state, erase} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || !ownedDebEraseNode(info, owner) ||
			(dir == erase && info.Mode().Perm() != 0700) {
			return userservice.ErrServiceConflict
		}
	}
	// The marker alone does not establish erasure. Interrupted removal can
	// leave the original daemon, runner or CLI installed without its marker.
	for _, dir := range []string{root, filepath.Join(root, "gui-runtime")} {
		if info, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) &&
			(err != nil || !info.IsDir() || !ownedDebEraseNode(info, owner)) {
			return userservice.ErrServiceConflict
		}
	}
	for _, name := range []string{
		"sidravia.deb", "sidravia.deb-core", "sidravia.deb-lifecycle",
		"sidraviad", "sidraviactl", "sidravia_gui", "gui-runtime/sidravia_gui",
	} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			return userservice.ErrServiceConflict
		}
	}

	// The journal remains root-private. The sender verifies its contents and
	// holds the lock throughout cleanup. Only the durable files' identity
	// and permissions are checked here.
	if !trustedDebEraseStateFile(filepath.Join(state, "transaction.json"), owner, 0600, -1) ||
		!trustedDebEraseStateFile(filepath.Join(state, "lock"), owner, 0644, 0) {
		return userservice.ErrServiceConflict
	}
	block := filepath.Join(state, "block")
	if !trustedDebEraseStateFile(block, owner, 0644, 5) {
		return userservice.ErrServiceConflict
	}
	if content, err := os.ReadFile(block); err != nil || string(content) != "dpkg\n" {
		return userservice.ErrServiceConflict
	}
	return nil
}

func trustedDebEraseStateFile(path string, owner uint32, mode os.FileMode, size int64) bool {
	info, err := os.Lstat(path)
	return err == nil && trustedDebEraseFile(info, owner) && info.Mode().Perm() == mode &&
		(size < 0 || info.Size() == size)
}
