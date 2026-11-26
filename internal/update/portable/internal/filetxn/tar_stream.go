package filetxn

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"strings"

	"github.com/xi2/xz"
)

// XZ archives produced by the fixed builder use an 8 MiB dictionary. The
// decoder enforces that bound before allocation, including subsequent blocks.
func openTarStream(ctx context.Context, file *os.File, name string) (io.ReadCloser, error) {
	source := &contextReader{ctx: ctx, r: file}
	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		reader, err := gzip.NewReader(source)
		if err != nil {
			return nil, err
		}
		return reader, nil
	case strings.HasSuffix(name, ".tar.xz"):
		reader, err := xz.NewReader(source, 8<<20)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(reader), nil
	default:
		return nil, ErrUnsupported
	}
}
