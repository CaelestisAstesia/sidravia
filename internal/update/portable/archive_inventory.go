package portable

import (
	"archive/tar"
	"archive/zip"
	"context"
	"encoding/binary"
	"io"
	"os"

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
	if (asset.OS != "linux" && asset.OS != "windows") || (asset.Kind != "core-portable" && asset.Kind != "gui-portable") {
		return nil, ErrUnsupported
	}
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != c.Size {
		return nil, ErrChanged
	}
	if asset.OS == "windows" {
		var footer [22]byte
		if info.Size() < 22 {
			return nil, ErrInvalid
		}
		if _, err = f.ReadAt(footer[:], info.Size()-22); err != nil {
			return nil, err
		}
		count := int(binary.LittleEndian.Uint16(footer[10:]))
		if err = validateZIPDirectory(f, info.Size(), count); err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(f, info.Size())
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
		return Extract(ctx, target, archive, inventory)
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
		return Extract(ctx, target, archive, inventory)
	}
	return nil, ErrInvalid
}
