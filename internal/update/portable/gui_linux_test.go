//go:build linux

package portable

import (
	"sidravia/internal/update/portable/internal/fixture"
	"testing"
)

func guiFixture(t *testing.T, version, build string) testImage {
	return imageKind(t, version, build, map[string]string{"sidravia_gui": fixture.StaticELF(), "gui-runtime/sidravia_gui": "signed runtime " + build, "gui-runtime/lib/libapp.so": "app " + build, "gui-runtime/lib/libflutter_linux_gtk.so": "flutter " + build, "gui-runtime/data/icudtl.dat": "data " + build}, nil, "gui")
}
