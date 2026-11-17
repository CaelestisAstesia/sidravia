//go:build windows

package portable

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sidravia/internal/update/packagefiles"
	"strings"
)

// Windows keeps GUI/DLL names and layout. Unknown loadable code, and unknown
// GUI data, cannot safely remain active after a signed product replacement.
func verifyGUI(root *os.Root, i Inventory) error {
	files := index(i)
	required := []string{"sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe", "sidraviaupdater.exe"}
	if i.manifest.PayloadKind == "gui" {
		required = append(required, "Sidravia.exe", "flutter_windows.dll", "data/icudtl.dat", "data/flutter_assets/AssetManifest.bin")
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			return ErrUnsupported
		}
	}
	pending := []string{"."}
	visited := 0
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		handle, err := root.Open(dir)
		if err != nil {
			return err
		}
		for {
			entries, readErr := handle.ReadDir(128)
			for _, entry := range entries {
				visited++
				if visited > packagefiles.MaximumFiles*4 {
					handle.Close()
					return ErrChanged
				}
				name := filepath.ToSlash(filepath.Join(dir, entry.Name()))
				info, err := root.Lstat(name)
				if err != nil {
					handle.Close()
					return err
				}
				code := strings.EqualFold(filepath.Ext(name), ".exe") || strings.EqualFold(filepath.Ext(name), ".dll")
				data := strings.EqualFold(name, "data") || strings.HasPrefix(strings.ToLower(name), "data/")
				if !plainPlatformPath(filepath.Join(root.Name(), filepath.FromSlash(name))) {
					handle.Close()
					return ErrChanged
				}
				if info.IsDir() {
					pending = append(pending, name)
					continue
				}
				if _, known := files[name]; !known && (code || data) {
					handle.Close()
					return ErrChanged
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				handle.Close()
				return readErr
			}
		}
		if err := handle.Close(); err != nil {
			return err
		}
	}
	return nil
}
