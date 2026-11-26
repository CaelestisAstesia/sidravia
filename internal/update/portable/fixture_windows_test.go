//go:build windows

package portable

import (
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/portable/internal/fixture"
	"testing"

	"sidravia/internal/release"
)

type winImage struct {
	fixture.Windows
	inventory filetxn.Inventory
	verified  release.VerifiedRelease
	archive   []byte
	keys      *release.PublisherKeys
}

func windowsImage(t *testing.T, version, build string, files map[string][]byte) winImage {
	t.Helper()
	i := fixture.WindowsImage(t, version, build, files)
	inventory, err := filetxn.Authenticate(i.Verified, i.Candidate, i.Index)
	if err != nil {
		t.Fatal(err)
	}
	return winImage{i, inventory, i.Verified, i.Archive, i.Keys}
}
func winFiles(value string) map[string][]byte { return fixture.WindowsFiles(value) }
func winInstall(t *testing.T, target string, i winImage) {
	fixture.InstallWindows(t, target, i.Windows)
}
