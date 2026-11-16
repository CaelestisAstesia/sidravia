package packagefiles

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixture() Manifest {
	return Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "build", OS: "linux", Arch: "amd64", PayloadKind: "core", Files: []File{{Path: "sidraviad", Size: 3, SHA256: strings.Repeat("a", 64), Mode: 0755}}}
}
func TestOwnershipRoundTripAndPrivateSort(t *testing.T) {
	m := fixture()
	m.Files = append(m.Files, File{Path: "LICENSE", Size: 0, SHA256: strings.Repeat("b", 64), Mode: 0644})
	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Path != "sidraviad" {
		t.Fatal("mutated input order")
	}
	got, err := Decode(data)
	if err != nil || got.Files[0].Path != "LICENSE" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestOwnershipRejectsPathsAliasesAndData(t *testing.T) {
	if !SafePath("data/legal name.txt") {
		t.Fatal("legal internal space rejected")
	}
	for _, name := range []string{"../escape", "/absolute", "C:/path", `lib\x.dll`, "lib//x", "lib/../x", "lib/x.", "CON.txt", "CON .txt", "lib/COM1 .dll", "CONIN$", "CONOUT$.txt", "lib/lpt1.dll", "x:y", "config/settings.json", "LOGS/log", "namespaces/x/a", Name, "sidravia.portable", "sidravia.update", "a "} {
		m := fixture()
		m.Files[0].Path = name
		if m.Validate() == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, names := range [][2]string{{"lib/a", "LIB/a"}, {"lib", "lib/a"}, {"LIB/a", "lib"}} {
		m := fixture()
		m.Files[0].Path = names[0]
		f := m.Files[0]
		f.Path = names[1]
		m.Files = append(m.Files, f)
		if m.Validate() == nil {
			t.Errorf("accepted conflicting paths %v", names)
		}
	}
}
func TestOwnershipRejectsMalformedWireAndResourceBounds(t *testing.T) {
	data, _ := Encode(fixture())
	for _, bad := range []string{strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1), strings.Replace(string(data), `"size":3`, `"size":null`, 1), strings.Replace(string(data), `"size":3`, `"Size":3`, 1), string(data) + `{}`, strings.Replace(string(data), `"mode":493`, `"mode":511`, 1)} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Errorf("accepted invalid ownership document %s", bad)
		}
	}
	for _, change := range []func(*Manifest){func(m *Manifest) { m.SchemaVersion = 2 }, func(m *Manifest) { m.OS = "darwin" }, func(m *Manifest) { m.Arch = "386" }, func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("A", 64) }, func(m *Manifest) { m.Files[0].Size = -1 }, func(m *Manifest) { m.Files = make([]File, MaximumFiles+1) }} {
		m := fixture()
		change(&m)
		raw, _ := json.Marshal(m)
		if _, err := Decode(raw); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}
