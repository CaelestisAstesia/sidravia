// Package setup owns the signed Windows installation contract. Inno owns file
// publication and integration; the application owns exit and connection intent.
package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sidravia/internal/update/setup/internal/receiptstate"

	"sidravia/internal/release"

	packagefiles "sidravia/internal/release/files"
)

var ErrInvalid = receiptstate.ErrInvalid
var ErrChanged = errors.New("setup product files changed")
var ErrHandoff = errors.New("installer handoff unavailable")
var ErrRecovery = errors.New("installed program recovery unconfirmed")

func sum(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// CoreFile mirrors the producer's fixed core ownership, not a heuristic based
// on file presence. A GUI-capable setup can install just its core component.
func CoreFile(name string) bool {
	switch name {
	case "sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe", "sidraviaupdater.exe", "LICENSE", "BUILD-INFO.txt", "app_icon.ico":
		return true
	}
	return strings.HasPrefix(name, "institution-profiles/")
}

func VerifyInstalled(ctx context.Context, target string, v release.VerifiedRelease, c release.Candidate, index []byte, hasGUI bool) (packagefiles.Manifest, error) {
	return verifyInstalled(ctx, target, v, c, index, hasGUI, nil)
}
func verifyInstalled(ctx context.Context, target string, v release.VerifiedRelease, c release.Candidate, index []byte, hasGUI bool, retired map[string]bool) (packagefiles.Manifest, error) {
	m, err := v.VerifyFiles(c, index)
	if err != nil {
		return m, err
	}
	a, err := v.Asset(c)
	if err != nil || a.OS != "windows" || a.Arch != runtime.GOARCH || a.Kind != "setup" || (hasGUI && m.PayloadKind != "gui") {
		return m, ErrInvalid
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return m, err
	}
	defer root.Close()
	// The ownership index excludes itself, but Inno replaces it too. Authenticate
	// its exact bytes and single-link identity just like every listed payload.
	indexInfo, e := root.Lstat(packagefiles.Name)
	if e != nil || !indexInfo.Mode().IsRegular() || indexInfo.Size() != int64(len(index)) {
		return m, ErrChanged
	}
	if e = plainPath(filepath.Join(target, packagefiles.Name)); e != nil {
		return m, e
	}
	indexFile, e := root.Open(packagefiles.Name)
	if e != nil {
		return m, e
	}
	current, e := indexFile.Stat()
	if e != nil || !os.SameFile(indexInfo, current) {
		indexFile.Close()
		return m, ErrChanged
	}
	if e = singleLink(indexFile); e != nil {
		indexFile.Close()
		return m, e
	}
	indexBytes, e := io.ReadAll(io.LimitReader(indexFile, int64(len(index))+1))
	closeErr := indexFile.Close()
	if e != nil || closeErr != nil {
		return m, errors.Join(e, closeErr)
	}
	if !bytes.Equal(indexBytes, index) {
		return m, ErrChanged
	}

	// No alias, symlink or junction may redirect a verified file or ancestor.
	wanted := map[string]bool{}
	for _, f := range m.Files {
		wanted[strings.ToLower(f.Path)] = true
		path := filepath.FromSlash(f.Path)
		info, e := root.Lstat(path)
		if m.PayloadKind == "gui" && !hasGUI && !CoreFile(f.Path) {
			if !os.IsNotExist(e) {
				return m, ErrChanged
			}
			continue
		}
		if e != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
			return m, errors.Join(ErrChanged, e)
		}
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			i, e := root.Lstat(parent)
			if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
				return m, ErrChanged
			}
			if e := plainPath(filepath.Join(target, parent)); e != nil {
				return m, e
			}
		}
		if e := plainPath(filepath.Join(target, path)); e != nil {
			return m, e
		}
		file, e := root.Open(path)
		if e != nil {
			return m, e
		}
		current, e := file.Stat()
		if e != nil || !os.SameFile(info, current) {
			file.Close()
			return m, ErrChanged
		}
		if e := singleLink(file); e != nil {
			file.Close()
			return m, e
		}
		h := sha256.New()
		n, e := io.Copy(h, &contextReader{ctx, io.LimitReader(file, f.Size+1)})
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return m, errors.Join(e, closeErr)
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return m, ErrChanged
		}
	}
	// Unknown executable code is never adopted as part of a GUI upgrade. User
	// documents are preserved; Inno's own generated uninstaller is outside payload.
	entries, e := os.ReadDir(target)
	if e != nil {
		return m, e
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if installerLeaf(name) {
			path := filepath.Join(target, entry.Name())
			if e = plainPath(path); e != nil {
				return m, e
			}
			f, e := os.Open(path)
			if e != nil {
				return m, e
			}
			info, e := f.Stat()
			if e != nil || !info.Mode().IsRegular() {
				f.Close()
				return m, ErrChanged
			}
			linkErr := singleLink(f)
			closeErr := f.Close()
			if e = errors.Join(linkErr, closeErr); e != nil {
				return m, e
			}
		}

		ext := filepath.Ext(name)
		if (ext == ".exe" || ext == ".dll") && !wanted[name] && !uninstaller(name) && !retired[name] {
			return m, ErrChanged
		}
	}
	return m, nil
}
func uninstaller(name string) bool {
	return len(name) == 12 && strings.HasPrefix(name, "unins") && strings.HasSuffix(name, ".exe") && name[5] >= '0' && name[5] <= '9' && name[6] >= '0' && name[6] <= '9' && name[7] >= '0' && name[7] <= '9'
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func installerLeaf(name string) bool {
	if len(name) != 12 {
		return false
	}
	extension := filepath.Ext(name)
	if extension != ".exe" && extension != ".dat" && extension != ".msg" {
		return false
	}
	return uninstaller(strings.TrimSuffix(name, extension) + ".exe")
}
