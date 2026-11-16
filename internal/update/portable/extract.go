package portable

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/update/packagefiles"
)

// Payload owns an isolated directory outside the installation. It is never
// executable until all archive bytes and product files have been verified.
type Payload struct {
	directory   string
	inventory   Inventory
	root        *os.Root
	owned       map[string]os.FileInfo
	directories map[string]os.FileInfo
}

// Extract accepts the producer's exact portable format, not arbitrary archives.
// Staging is beside the target for same-filesystem publication; no scripts run.
func Extract(ctx context.Context, target, archive string, i Inventory) (result *Payload, err error) {
	if len(i.data) == 0 || !canonical(target) {
		return nil, ErrInvalid
	}
	parent := filepath.Dir(target)
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != i.candidate.Size {
		return nil, ErrChanged
	}
	hash := sha256.New()
	n, err := io.Copy(hash, &contextReader{ctx: ctx, r: io.LimitReader(f, i.candidate.Size+1)})
	if err != nil {
		return nil, err
	}
	if n != i.candidate.Size || hex.EncodeToString(hash.Sum(nil)) != i.candidate.SHA256 {
		return nil, ErrChanged
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(parent, ".sidravia-stage-")
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		os.Remove(directory)
		return nil, err
	}
	p := &Payload{directory: directory, inventory: i, root: root, owned: map[string]os.FileInfo{}, directories: map[string]os.FileInfo{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Close())
		}
	}()
	var required int64
	for _, entry := range i.manifest.Files {
		required += entry.Size
	}
	if err = availableSpace(root, required+int64(len(i.data))+1<<20); err != nil {
		return nil, err
	}
	expected := index(i)
	seen := map[string]bool{}
	consume := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		if seen[pathKey(name)] {
			return ErrInvalid
		}
		seen[pathKey(name)] = true
		switch name {
		case packagefiles.Name:
			if size != int64(len(i.data)) || mode != 0644 {
				return ErrChanged
			}
			data, e := io.ReadAll(io.LimitReader(r, size+1))
			if e != nil {
				return e
			}
			if string(data) != string(i.data) {
				return ErrChanged
			}
			return p.writeStage(ctx, name, mode, size, strings.NewReader(string(data)), digest(data))
		case "sidravia.portable":
			if size != 0 || mode != 0644 {
				return ErrInvalid
			}
			return nil
		}
		entry, ok := expected[name]
		allowedMode := os.FileMode(entry.Mode) == mode
		if i.manifest.OS == "windows" {
			allowedMode = mode == 0644 || mode == 0755
		}
		if !ok || entry.Size != size || !allowedMode {
			return ErrChanged
		}
		return p.writeStage(ctx, name, modeFor(i, entry.Mode), size, r, entry.SHA256)
	}
	switch {
	case strings.HasSuffix(i.candidate.Name, ".tar.gz"):
		gz, e := gzip.NewReader(f)
		if e != nil {
			return nil, e
		}
		gz.Multistream(false)
		// Bound even ignored archive metadata and padding, not just copied files.
		limited := &io.LimitedReader{R: &contextReader{ctx: ctx, r: gz}, N: required + packagefiles.SizeLimit + int64(packagefiles.MaximumFiles)*2048 + 1<<20}
		tr := tar.NewReader(limited)
		prefix := "sidravia-v" + i.manifest.Version + "-" + i.manifest.OS + "-" + i.manifest.Arch + "/"
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, prefix) || len(h.PAXRecords) > 0 || len(h.Xattrs) > 0 || h.Linkname != "" {
				return nil, ErrInvalid
			}
			if e := consume(strings.TrimPrefix(h.Name, prefix), os.FileMode(h.Mode), h.Size, tr); e != nil {
				return nil, e
			}
		}
		// Drain the gzip trailer so CRC failures cannot hide behind tar EOF.
		if _, e = io.Copy(io.Discard, limited); e != nil {
			return nil, e
		}
		if limited.N <= 0 {
			return nil, ErrInvalid
		}
		if e = gz.Close(); e != nil {
			return nil, e
		}
	case strings.HasSuffix(i.candidate.Name, ".zip"):
		if e := validateZIPDirectory(f, info.Size(), len(expected)+2); e != nil {
			return nil, e
		}
		zr, e := zip.NewReader(f, info.Size())
		if e != nil {
			return nil, e
		}
		if len(zr.File) > len(expected)+2 {
			return nil, ErrInvalid
		}
		for _, entry := range zr.File {
			if !entry.Mode().IsRegular() || entry.UncompressedSize64 > 4<<30 {
				return nil, ErrInvalid
			}
			r, e := entry.Open()
			if e != nil {
				return nil, e
			}
			e = consume(entry.Name, entry.Mode().Perm(), int64(entry.UncompressedSize64), r)
			e = errors.Join(e, r.Close())
			if e != nil {
				return nil, e
			}
		}
	default:
		return nil, ErrInvalid
	}
	if len(seen) != len(expected)+2 {
		return nil, ErrInvalid
	}
	if err = verifyInventory(ctx, root, i); err != nil {
		return nil, err
	}
	return p, nil
}
func (p *Payload) writeStage(ctx context.Context, name string, mode os.FileMode, size int64, r io.Reader, want string) error {
	root := p.root
	if name != packagefiles.Name && !packagefiles.SafePath(name) {
		return ErrInvalid
	}
	if err := p.makeDirectories(name); err != nil {
		return err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()
	if statErr != nil {
		return errorsJoinClose(statErr, f)
	}
	p.owned[name] = info
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), &contextReader{ctx: ctx, r: io.LimitReader(r, size+1)})
	if copyErr == nil && (n != size || hex.EncodeToString(hash.Sum(nil)) != want) {
		copyErr = ErrChanged
	}
	if copyErr == nil {
		copyErr = f.Chmod(mode)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	return errorsJoinClose(copyErr, f)
}
func errorsJoinClose(err error, f *os.File) error { return errors.Join(err, f.Close()) }
