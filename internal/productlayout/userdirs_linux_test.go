//go:build linux

package productlayout

import (
	"path/filepath"
	"testing"
)

func TestLinuxXDGRootsMatchGUIPreferences(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, value := range []string{"", "relative"} {
		t.Setenv("XDG_CONFIG_HOME", value)
		t.Setenv("XDG_CACHE_HOME", value)
		config, err := platformUserConfigDir()
		if err != nil || config != filepath.Join(home, ".config") {
			t.Fatalf("config=%q err=%v", config, err)
		}
		cache, err := platformUserCacheDir()
		if err != nil || cache != filepath.Join(home, ".cache") {
			t.Fatalf("cache=%q err=%v", cache, err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "custom-config"))
	config, err := platformUserConfigDir()
	if err != nil || config != filepath.Join(home, "custom-config") {
		t.Fatalf("config=%q err=%v", config, err)
	}
}
