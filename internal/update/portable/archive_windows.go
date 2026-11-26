//go:build windows

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// The original archive might already have writable mappings, so a read pin
// on that archive cannot establish immutable contents. Start with a new file
// with no sharing and automatic deletion. No other process can obtain a write
// handle or mapping before or during the copy. After the copy, retain only a
// read-access duplicate of the same file object and never reopen its path.
func openArchive(ctx context.Context, directory, path string, size int64) (_ *os.File, _ int64, resultErr error) {
	source, err := openArchiveSource(path, size)
	if err != nil {
		return nil, 0, err
	}
	defer source.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, 0, err
	}
	snapshot := filepath.Join(directory, ".sidravia-archive-"+hex.EncodeToString(nonce[:]))
	name, err := windows.UTF16PtrFromString(snapshot)
	if err != nil {
		return nil, 0, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, 0, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_TEMPORARY|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
	if err != nil {
		return nil, 0, err
	}
	writer := os.NewFile(uintptr(handle), snapshot)
	defer func() {
		if writer != nil {
			resultErr = errors.Join(resultErr, writer.Close())
		}
	}()
	copied, err := io.Copy(writer, &contextReader{ctx: ctx, r: io.LimitReader(source, size+1)})
	if err != nil {
		return nil, 0, err
	}
	if copied != size {
		return nil, 0, ErrChanged
	}
	var readHandle windows.Handle
	process := windows.CurrentProcess()
	if err = windows.DuplicateHandle(process, handle, process, &readHandle, windows.FILE_GENERIC_READ, false, 0); err != nil {
		return nil, 0, err
	}
	reader := os.NewFile(uintptr(readHandle), snapshot)
	err = writer.Close()
	writer = nil
	if err != nil {
		return nil, 0, errorsJoinClose(err, reader)
	}
	return reader, size, nil
}
