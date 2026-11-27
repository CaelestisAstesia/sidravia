package request

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
)

func TestQtProfileComesFromInstalledComponents(t *testing.T) {
	const core = "<Package><Name>org.sidravia.core</Name></Package>"
	const gui = "<Package><Name>org.sidravia.gui</Name></Package>"
	for _, scenario := range []struct {
		name, packages, kind string
		valid                bool
	}{
		{"core", core, release.QtCoreRepository, true},
		{"gui even with missing GUI executable", core + gui, release.QtGUIRepository, true},
		{"missing mandatory Core", gui, "", false},
		{"duplicate components", core + core, "", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			layout := productlayout.Layout{Mode: productlayout.ModeInstalled, ExecutableDirectory: directory}
			if kind, err := qtInstalledKind(layout); kind != "" || err != nil {
				t.Fatalf("directory without maintenance tool classified as Qt: %q, %v", kind, err)
			}
			if err := os.WriteFile(filepath.Join(directory, "sidravia-maintenance.exe"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			inventory := "<Packages><ApplicationName>Sidravia</ApplicationName>" + scenario.packages + "</Packages>"
			if err := os.WriteFile(filepath.Join(directory, "components.xml"), []byte(inventory), 0600); err != nil {
				t.Fatal(err)
			}
			kind, err := qtInstalledKind(layout)
			if kind != scenario.kind || scenario.valid && err != nil || !scenario.valid && !errors.Is(err, release.ErrMetadata) {
				t.Fatalf("got kind=%q, err=%v for %v", kind, err, scenario.name)
			}
		})
	}
}
