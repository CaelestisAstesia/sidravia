// Package portable owns authenticated product-file application. It neither
// chooses user data nor owns application shutdown/recovery or release policy.
package portable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/release"

	packagefiles "sidravia/internal/release/files"
)

var (
	ErrInvalid     = errors.New("invalid portable update input")
	ErrChanged     = errors.New("portable product files changed or are unsafe")
	ErrUnsupported = errors.New("portable application is not supported for this platform or payload")
)

// Inventory contains only a signed, verified ownership index. Callers cannot
// substitute an arbitrary local list to authorize deletion of user files.
type Inventory struct {
	manifest  packagefiles.Manifest
	data      []byte
	candidate release.Candidate
}

func Authenticate(v release.VerifiedRelease, c release.Candidate, data []byte) (Inventory, error) {
	m, err := v.VerifyFiles(c, data)
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{manifest: m, data: append([]byte(nil), data...), candidate: c}, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func canonical(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	if !plainPlatformPath(path) {
		return false
	}
	actual, err := filepath.EvalSymlinks(path)
	return err == nil && actual == path
}
func checkParents(root *os.Root, name string) error {
	base, err := root.Stat(".")
	if err != nil {
		return err
	}
	for p := filepath.Dir(filepath.FromSlash(name)); p != "."; p = filepath.Dir(p) {
		info, err := root.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !sameDevice(base, info) || !plainPlatformPath(filepath.Join(root.Name(), p)) {
			return ErrChanged
		}
	}
	return nil
}
func openRegular(root *os.Root, name string) (*os.File, error) {
	if err := checkParents(root, name); err != nil {
		return nil, err
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ErrChanged
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		f.Close()
		return nil, ErrChanged
	}
	if err := singleFile(f, info); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func verifyFile(ctx context.Context, root *os.Root, f packagefiles.File) error {
	file, err := openRegular(root, f.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	base, err := root.Stat(".")
	if err != nil {
		return err
	}
	if info.Size() != f.Size || !matchesMode(info, f.Mode) || !sameDevice(base, info) {
		return ErrChanged
	}
	hash := sha256.New()
	n, err := io.Copy(hash, &contextReader{ctx: ctx, r: io.LimitReader(file, f.Size+1)})
	if err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return ErrChanged
	}
	return nil
}
func verifyInventory(ctx context.Context, root *os.Root, i Inventory) error {
	if len(i.data) == 0 {
		return ErrInvalid
	}
	f, err := openRegular(root, packagefiles.Name)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, packagefiles.SizeLimit+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if string(data) != string(i.data) {
		return ErrChanged
	}
	for _, file := range i.manifest.Files {
		if err := verifyFile(ctx, root, file); err != nil {
			return err
		}
	}
	return nil
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
func index(i Inventory) map[string]packagefiles.File {
	m := make(map[string]packagefiles.File, len(i.manifest.Files))
	for _, f := range i.manifest.Files {
		m[f.Path] = f
	}
	return m
}
func modeFor(i Inventory, mode uint32) os.FileMode {
	if i.manifest.OS == "windows" {
		return 0644
	}
	return os.FileMode(mode)
}
func pathKey(path string) string { return strings.ToLower(path) }
