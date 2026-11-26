//go:build windows

package filetxn

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"sidravia/internal/update/windowsplatform"
	"strings"
	"unsafe"
)

func SingleFile(f *os.File, _ os.FileInfo) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrChanged
	}
	return nil
}
func matchesMode(info os.FileInfo, _ uint32) bool { return info.Mode().IsRegular() }
func applicationPlatform(i Inventory) error {
	if i.manifest.OS != "windows" || i.manifest.Arch != runtime.GOARCH || runtime.GOARCH != "amd64" || (i.manifest.PayloadKind != "core" && i.manifest.PayloadKind != "gui") {
		return ErrUnsupported
	}
	return nil
}

// NTFS has no supported per-directory FlushFileBuffers equivalent. Payload,
// seal and secured records are flushed separately; interrupted writes remain
// sealed and require explicit full inventory verification/manual repair.
func syncDirectory(*os.Root, string) error { return nil }
func availableSpace(root *os.Root, required int64) error {
	path := root.Name()
	if strings.HasPrefix(path, `\\`) {
		return ErrUnsupported
	}
	volume := filepath.VolumeName(path) + `\`
	p, err := windows.UTF16PtrFromString(volume)
	if err != nil {
		return err
	}
	var fs [64]uint16
	if err = windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil {
		return err
	}
	if windows.UTF16ToString(fs[:]) != "NTFS" {
		return ErrUnsupported
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var free uint64
	if err = windows.GetDiskFreeSpaceEx(name, &free, nil, nil); err != nil {
		return err
	}
	if required < 0 || free < uint64(required) {
		return windows.ERROR_DISK_FULL
	}
	return nil
}

// The staged and target trees share a Canonical parent; reparse mounts are
// refused separately. The handle-relative kernel rename is the volume check.
func sameDevice(os.FileInfo, os.FileInfo) bool { return true }
func applyGuard() (func(), error)              { return windowsplatform.AcquireInstallation() }
func holdStartupSeal() bool                    { return true }

func ensureQuiescent(target string) error {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(h, &entry); err == nil; err = windows.Process32Next(h, &entry) {
		proc, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if e != nil {
			continue
		} // protected unrelated system processes expose no target path
		name := make([]uint16, 32768)
		n := uint32(len(name))
		e = windows.QueryFullProcessImageName(proc, 0, &name[0], &n)
		windows.CloseHandle(proc)
		if e != nil {
			continue
		}
		path := strings.ToLower(windows.UTF16ToString(name[:n]))
		base := strings.ToLower(target) + string(filepath.Separator)
		if strings.HasPrefix(path, base) {
			return ErrChanged
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

// Open both parents through os.Root, then rename the exact source handle into
// the retained destination directory. New paths are kernel no-replace; no
// POSIX replace semantics are used to bypass Windows executable sharing locks.
func publish(root *os.Root, old, new string, replace bool) error {
	src, err := root.OpenRoot(filepath.Dir(old))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := root.OpenRoot(filepath.Dir(new))
	if err != nil {
		return err
	}
	defer dst.Close()
	sf, err := src.Open(".")
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := dst.Open(".")
	if err != nil {
		return err
	}
	defer df.Close()
	name, err := windows.NewNTUnicodeString(filepath.Base(old))
	if err != nil {
		return err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(sf.Fd()), ObjectName: name}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var io windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.DELETE|windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES, &oa, &io, nil, 0, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrChanged
	}
	leaf, err := windows.UTF16FromString(filepath.Base(new))
	if err != nil {
		return err
	}
	type renameInfo struct {
		Replace uint32
		Root    windows.Handle
		Length  uint32
		Name    [1]uint16
	}
	var header renameInfo
	size := int(unsafe.Offsetof(header.Name)) + (len(leaf)-1)*2
	buf := make([]byte, size)
	r := (*renameInfo)(unsafe.Pointer(&buf[0]))
	r.Root = windows.Handle(df.Fd())
	r.Length = uint32((len(leaf) - 1) * 2)
	if replace {
		r.Replace = 1
	}
	copy(unsafe.Slice(&r.Name[0], len(leaf)-1), leaf)
	return windows.NtSetInformationFile(handle, &io, &buf[0], uint32(size), windows.FileRenameInformation)
}

func PlainPlatformPath(path string) bool {
	for {
		p, e := windows.UTF16PtrFromString(path)
		if e != nil {
			return false
		}
		a, e := windows.GetFileAttributes(p)
		if e != nil || a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return true
		}
		path = parent
	}
}
