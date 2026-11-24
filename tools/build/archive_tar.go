package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"sort"
)

func writeTar(path, root string, entries []archiveEntry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	if err := writeTarContents(gz, root, entries); err != nil {
		gz.Close()
		return err
	}
	return gz.Close()
}

func writeTarContents(writer io.Writer, root string, entries []archiveEntry) error {
	tw := tar.NewWriter(writer)
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: root + "/" + entry.name, Mode: int64(entry.mode.Perm()), Size: int64(len(entry.data)), ModTime: archiveModTime}); err != nil {
			return err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return err
		}
	}
	return tw.Close()
}

// The installer owns its display icon; portable payloads omit it. Each format
// gets the exact ownership index for its own bytes rather than sharing a stale
// index whose files are no longer present in the archive.
