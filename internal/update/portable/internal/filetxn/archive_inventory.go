package filetxn

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
)

// ExtractRelease authenticates the ownership index before the existing full
// extractor validates every archive byte and leaf. No index is trusted merely
// because it was found in a downloaded archive.
func ExtractRelease(ctx context.Context, target, archive string, v release.VerifiedRelease, c release.Candidate) (*Payload, error) {
	asset, err := v.Asset(c)
	if err != nil {
		return nil, err
	}
	if !Canonical(target) {
		return nil, ErrInvalid
	}
	if (asset.OS != "linux" && asset.OS != "windows") || (asset.Kind != "core-portable" && asset.Kind != "gui-portable") {
		return nil, ErrUnsupported
	}
	f, size, err := openArchive(ctx, filepath.Dir(target), archive, c.Size)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return extractRelease(ctx, target, f, size, v, c, asset)
}

func extractRelease(ctx context.Context, target string, f *os.File, size int64, v release.VerifiedRelease, c release.Candidate, asset release.ReleaseAsset) (*Payload, error) {
	if err := verifyArchive(ctx, f, c); err != nil {
		return nil, err
	}
	if asset.OS == "windows" {
		var footer [22]byte
		if size < 22 {
			return nil, ErrInvalid
		}
		if _, err := f.ReadAt(footer[:], size-22); err != nil {
			return nil, err
		}
		count := int(binary.LittleEndian.Uint16(footer[10:]))
		if err := validateZIPDirectory(f, size, count); err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(f, size)
		if err != nil {
			return nil, err
		}
		var data []byte
		for _, entry := range zr.File {
			if entry.Name != packagefiles.Name {
				continue
			}
			if data != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > packagefiles.SizeLimit {
				return nil, ErrInvalid
			}
			reader, e := entry.Open()
			if e != nil {
				return nil, e
			}
			data, e = io.ReadAll(io.LimitReader(reader, packagefiles.SizeLimit+1))
			closeErr := reader.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
		inventory, err := Authenticate(v, c, data)
		if err != nil {
			return nil, err
		}
		return extractPayload(ctx, target, f, size, inventory)
	}
	gz, err := openTarStream(ctx, f, c.Name)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	// This discovery pass never extracts or runs a file; expansion and header
	// count are bounded even if a corrupt archive hides the index until the end.
	limit := &io.LimitedReader{R: &contextReader{ctx: ctx, r: gz}, N: (4 << 30) + packagefiles.SizeLimit + packagefiles.MaximumFiles*2048 + (1 << 20)}
	reader := tar.NewReader(limit)
	wanted := "sidravia-v" + c.Version + "-" + asset.OS + "-" + asset.Arch + "/" + packagefiles.Name
	for count := 0; count < packagefiles.MaximumFiles+2; count++ {
		header, err := reader.Next()
		if err != nil {
			return nil, err
		}
		if header.Name != wanted {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > packagefiles.SizeLimit {
			return nil, ErrInvalid
		}
		data, err := io.ReadAll(io.LimitReader(reader, packagefiles.SizeLimit+1))
		if err != nil {
			return nil, err
		}
		inventory, err := Authenticate(v, c, data)
		if err != nil {
			return nil, err
		}
		return extractPayload(ctx, target, f, size, inventory)
	}
	return nil, ErrInvalid
}

func verifyArchive(ctx context.Context, f *os.File, candidate release.Candidate) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, r: io.LimitReader(f, candidate.Size+1)})
	if err != nil {
		return err
	}
	if size != candidate.Size || hex.EncodeToString(hash.Sum(nil)) != candidate.SHA256 {
		return ErrChanged
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}

func openArchiveSource(path string, size int64) (*os.File, error) {
	if size <= 0 || size > 2<<30 {
		return nil, ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, errorsJoinClose(err, f)
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return nil, errorsJoinClose(ErrChanged, f)
	}
	return f, nil
}
