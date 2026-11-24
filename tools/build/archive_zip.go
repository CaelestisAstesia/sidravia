package main

import (
	"archive/zip"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sort"
	"time"
)

// archiveEntry is one file in a package payload.
type archiveEntry struct {
	name string
	data []byte
	mode os.FileMode
}

var archiveModTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// writeZip writes entries to path in lexicographic name order with fixed
// timestamps, modes and Deflate compression.
func writeZip(path string, entries []archiveEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		fh.Modified = archiveModTime
		fh.SetMode(e.mode)
		w, err := zw.CreateHeader(fh)
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := w.Write(e.data); err != nil {
			zw.Close()
			return err
		}
	}
	return zw.Close()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
