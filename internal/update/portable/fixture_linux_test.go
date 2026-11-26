//go:build linux

package portable

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/portable/internal/fixture"
	"testing"

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
)

type testImage struct {
	fixture.Linux
	inventory filetxn.Inventory
	verified  release.VerifiedRelease
	archive   []byte
	keys      *release.PublisherKeys
}

func signedImage(t *testing.T, i fixture.Linux) testImage {
	t.Helper()
	inventory, err := filetxn.Authenticate(i.Verified, i.Candidate, i.Index)
	if err != nil {
		t.Fatal(err)
	}
	return testImage{i, inventory, i.Verified, i.Archive, i.Keys}
}
func image(t *testing.T, version, build string, contents map[string]string, alter func(*tar.Header)) testImage {
	return imageKind(t, version, build, contents, alter, "core")
}
func imageKind(t *testing.T, version, build string, contents map[string]string, alter func(*tar.Header), kind string) testImage {
	return signedImage(t, fixture.LinuxImage(t, version, build, contents, alter, kind))
}
func signImage(t *testing.T, m packagefiles.Manifest, data, archive []byte, contents map[string]string) testImage {
	return signedImage(t, fixture.SignLinuxImage(t, m, data, archive, contents))
}
func install(t *testing.T, target string, i testImage) { fixture.InstallLinux(t, target, i.Linux) }
func extract(t *testing.T, target string, i testImage) *filetxn.Payload {
	t.Helper()
	archive := filepath.Join(t.TempDir(), i.Candidate.Name)
	if err := os.WriteFile(archive, i.archive, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := filetxn.Extract(context.Background(), target, archive, i.inventory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}
