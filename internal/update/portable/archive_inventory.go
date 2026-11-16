package portable

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"

	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
)

// ExtractRelease authenticates the ownership index before the existing full
// extractor validates every archive byte and leaf. No index is trusted merely
// because it was found in a downloaded archive.
func ExtractRelease(ctx context.Context, target, archive string, v releases.VerifiedRelease, c releases.Candidate) (*Payload, error) {
	asset, err := v.Asset(c)
	if err != nil {
		return nil, err
	}
	if asset.OS != "linux" || (asset.Kind != "core-portable" && asset.Kind != "gui-portable") {
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
	gz, err := gzip.NewReader(f)
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
