//go:build linux

package registration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/productlayout"
)

func TestRenderUserUnitUsesResolvedPackagePaths(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "data %n")
	cache := filepath.Join(root, "cache")
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", cache)

	installed := filepath.Join(root, "installed")
	layout, err := productlayout.ResolveTarget(installed, productlayout.Namespace{})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := renderUserUnit(layout)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"ExecStart=\":" + installed + "/sidraviad\"\n",
		"Environment=\"XDG_CONFIG_HOME=" + strings.ReplaceAll(config, "%", "%%") + "\"\n",
		"Environment=\"XDG_CACHE_HOME=" + cache + "\"\n",
	} {
		if !strings.Contains(string(unit), line) {
			t.Errorf("installed unit lacks %q", line)
		}
	}

	portable := filepath.Join(root, "portable folder")
	if err := os.Mkdir(portable, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(portable, "sidravia.portable"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	layout, err = productlayout.ResolveTarget(portable, productlayout.Namespace{})
	if err != nil {
		t.Fatal(err)
	}
	unit, err = renderUserUnit(layout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "ExecStart=\":"+portable+"/sidraviad\"\n") ||
		strings.Contains(string(unit), "XDG_CONFIG_HOME=") {
		t.Errorf("portable unit should keep data beside the executable")
	}
}

func TestUnitPathCannotInjectDirectives(t *testing.T) {
	if _, err := quoteUnitPath("/tmp/rejected\nExecStart=/other"); err == nil {
		t.Fatal("accepted a path containing a newline")
	}
}
