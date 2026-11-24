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

// zipEntry is a single file to add to a zip archive.
type zipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

var zipModTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// writeZip writes entries to path in lexicographic name order with fixed
// timestamps, modes and Deflate compression.
func writeZip(path string, entries []zipEntry) error {
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
		fh.Modified = zipModTime
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
