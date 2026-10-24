package productlayout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageHostResolvesOriginalPortableTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sidravia.portable"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTarget(root, Namespace{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModePortable || got.ExecutableDirectory != root || got.ConfigurationsPath != filepath.Join(root, "config", "configurations.json") {
		t.Fatalf("layout=%+v", got)
	}
	if _, err = ResolveTarget("relative", Namespace{}); err == nil {
		t.Fatal("relative target accepted")
	}
}
