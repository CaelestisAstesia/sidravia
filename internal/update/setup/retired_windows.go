//go:build windows

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
	"sidravia/internal/update/transaction"
)

// Retired executable code is not a user file merely because the new installer
// stopped listing it. Authenticate BOTH ownership indices and every new byte
// before removing only unchanged old exe/DLL leaves. Other unknown files remain.
func retireInstalled(ctx context.Context, r transaction.Record, s Session) error {
	next, c, index, e := targetEvidence(r)
	if e != nil {
		return e
	}
	newFiles, e := next.VerifyFiles(c, index)
	if e != nil {
		return e
	}
	old, oldAsset, e := s.SourceProof(release.PublisherTrust())
	if e != nil {
		return e
	}
	oldFiles, e := old.VerifyFiles(oldAsset, s.SourceIndex)
	if e != nil {
		return e
	}
	wanted := map[string]bool{}
	for _, f := range newFiles.Files {
		wanted[strings.ToLower(f.Path)] = true
	}
	retired := map[string]bool{}
	var leaves []packagefiles.File
	for _, f := range oldFiles.Files {
		name := strings.ToLower(f.Path)
		extension := filepath.Ext(name)
		if wanted[name] || extension != ".exe" && extension != ".dll" || oldFiles.PayloadKind == "gui" && !s.HasGUI && !CoreFile(f.Path) {
			continue
		}
		retired[name] = true
		leaves = append(leaves, f)
	}
	// No deletion occurs unless all new selected payload bytes are already exact.
	if _, e = verifyInstalled(ctx, r.Target.ID, next, c, index, s.HasGUI, retired); e != nil {
		return e
	}
	for _, f := range leaves {
		if e = removeRetiredFile(ctx, r.Target.ID, f); e != nil {
			return e
		}
	}
	return nil
}
func removeRetiredFile(ctx context.Context, target string, expected packagefiles.File) (err error) {
	path := filepath.Join(target, filepath.FromSlash(expected.Path))
	if !packagefiles.SafePath(expected.Path) {
		return ErrInvalid
	}
	if _, e := os.Lstat(path); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	if e := plainPath(path); e != nil {
		return e
	}
	name, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	h, e := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return e
	}
	file := os.NewFile(uintptr(h), path)
	defer func() { err = errors.Join(err, file.Close()) }()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != expected.Size {
		return ErrChanged
	}
	if e = singleLink(file); e != nil {
		return e
	}
	buffer := make([]uint16, 32768)
	n, e := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
	if e != nil || n >= uint32(len(buffer)) {
		return errors.Join(ErrChanged, e)
	}
	actual := strings.TrimPrefix(windows.UTF16ToString(buffer[:n]), `\\?\`)
	if !strings.EqualFold(filepath.Clean(actual), filepath.Clean(path)) {
		return ErrChanged
	}
	hash := sha256.New()
	nbytes, e := io.Copy(hash, &contextReader{ctx, io.LimitReader(file, expected.Size+1)})
	if e != nil {
		return e
	}
	if nbytes != expected.Size || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return ErrChanged
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	// Delete through the verified handle, with rename/write sharing denied.
	disposition := byte(1)
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&disposition)), 1)
}
