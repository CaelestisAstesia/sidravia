//go:build linux

package portable

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func staticELF() string {
	data := make([]byte, 64)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	binary.LittleEndian.PutUint16(data[54:], 56)
	return string(data)
}
func guiFixture(t *testing.T, version, build string) testImage {
	return imageKind(t, version, build, map[string]string{"sidravia_gui": staticELF(), "gui-runtime/sidravia_gui": "signed runtime " + build, "gui-runtime/lib/libapp.so": "app " + build, "gui-runtime/lib/libflutter_linux_gtk.so": "flutter " + build, "gui-runtime/data/icudtl.dat": "data " + build}, nil, "gui")
}
func TestGUIApplicationRequiresStaticEntryAndExactRuntimeInventory(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("GUI producer supports AMD64")
	}
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "unknown-library"}[unknown], func(t *testing.T) {
			target := t.TempDir()
			old := guiFixture(t, "1.0.0", "old")
			next := guiFixture(t, "1.1.0", "new")
			install(t, target, old)
			if unknown {
				if err := os.WriteFile(filepath.Join(target, "gui-runtime/lib/unowned.so"), []byte("user"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			p := extract(t, target, next)
			s, id := quiescent(t, target, old, next)
			err := Apply(context.Background(), s, id, target, old.inventory, p)
			if (err != nil) != unknown {
				t.Fatal("unexpected GUI application result", err)
			}
			if unknown {
				data, _ := os.ReadFile(filepath.Join(target, "gui-runtime/lib/unowned.so"))
				if string(data) != "user" {
					t.Fatal("removed unowned library")
				}
			}
		})
	}
}
