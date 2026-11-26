package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/userservice"
)

const debEraseState = "/var/lib/sidravia/deb-maintenance"

func admitDebErase(packageRoot string) (func(), error) {
	parent := os.Getppid()
	uid := os.Geteuid()
	if parent <= 1 || uid == 0 {
		return nil, userservice.ErrServiceConflict
	}
	parentInfo, err := os.Stat(filepath.Join("/proc", strconv.Itoa(parent)))
	if err != nil || !ownedDebEraseNode(parentInfo, 0) {
		return nil, userservice.ErrServiceConflict
	}
	journalFD, journalErr := strconv.Atoi(os.Getenv("SIDRAVIA_DEB_ERASE_JOURNAL_FD"))
	lockFD, lockErr := strconv.Atoi(os.Getenv("SIDRAVIA_DEB_ERASE_LOCK_FD"))
	if journalErr != nil || lockErr != nil || journalFD < 3 || lockFD < 3 || journalFD == lockFD {
		return nil, userservice.ErrServiceConflict
	}
	journal := os.NewFile(uintptr(journalFD), "Debian removal journal")
	lock := os.NewFile(uintptr(lockFD), "Debian maintenance lock")
	unix.CloseOnExec(journalFD)
	unix.CloseOnExec(lockFD)
	release := func() {
		_ = journal.Close()
		_ = lock.Close()
	}

	executable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		release()
		return nil, err
	}
	executableInfo, err := os.Stat("/proc/self/exe")
	if err != nil {
		release()
		return nil, err
	}
	if err := verifyDebErase(packageRoot, debEraseState, executable, executableInfo, journal, lock, parent, uid, 0); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func ownedDebEraseNode(info os.FileInfo, owner uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == owner && info.Mode().Perm()&0022 == 0
}

func trustedDebEraseFile(info os.FileInfo, owner uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && info.Mode().IsRegular() && ownedDebEraseNode(info, owner)
}

func verifyDebErase(root, state, executable string, exe os.FileInfo, journal, lock *os.File, parent, uid int, owner uint32) error {
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

	if !currentDebEraseFile(journal, filepath.Join(state, "transaction.json"), owner, 0600) ||
		!currentDebEraseFile(lock, filepath.Join(state, "lock"), owner, 0644) ||
		!holdsDebEraseLock(lock, parent) {
		return userservice.ErrServiceConflict
	}
	block := filepath.Join(state, "block")
	info, err := os.Lstat(block)
	if err != nil || !trustedDebEraseFile(info, owner) || info.Mode().Perm() != 0644 || info.Size() != 5 {
		return userservice.ErrServiceConflict
	}
	if content, err := os.ReadFile(block); err != nil || string(content) != "dpkg\n" {
		return userservice.ErrServiceConflict
	}

	var removal struct {
		Package     string `json:"package"`
		Action      string `json:"action"`
		Phase       string `json:"phase"`
		CleanupUIDs []int  `json:"cleanupUIDs"`
	}
	// Each UID child inherits the same open file description. Read the
	// journal independently of its shared offset.
	section := io.NewSectionReader(journal, 0, 256<<10)
	data, err := io.ReadAll(section)
	if err != nil || len(data) == 256<<10 || json.Unmarshal(data, &removal) != nil ||
		(removal.Package != "sidravia" && removal.Package != "sidravia-core") ||
		removal.Action != "remove" || removal.Phase != "quiesced" ||
		(removal.CleanupUIDs != nil && !slices.Contains(removal.CleanupUIDs, uid)) {
		return userservice.ErrServiceConflict
	}
	return nil
}

func currentDebEraseFile(f *os.File, path string, owner uint32, mode os.FileMode) bool {
	current, err := os.Lstat(path)
	if err != nil || !trustedDebEraseFile(current, owner) || current.Mode().Perm() != mode {
		return false
	}
	inherited, err := f.Stat()
	return err == nil && os.SameFile(current, inherited)
}

func holdsDebEraseLock(lock *os.File, parent int) bool {
	info, err := lock.Stat()
	if err != nil || info.Size() != 0 {
		return false
	}
	// fdinfo reports locks attached to this open file description, including
	// the owning PID. Testing contention alone would also accept another
	// caller's unrelated flock on the readable lock file.
	data, err := os.ReadFile(filepath.Join("/proc/self/fdinfo", strconv.FormatUint(uint64(lock.Fd()), 10)))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 9 && fields[0] == "lock:" && fields[2] == "FLOCK" &&
			fields[3] == "ADVISORY" && fields[4] == "WRITE" && fields[5] == strconv.Itoa(parent) &&
			fields[7] == "0" && fields[8] == "EOF" {
			return true
		}
	}
	return false
}
