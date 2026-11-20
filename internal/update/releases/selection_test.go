package releases

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
)

func TestLinuxArchiveEncodingsPreserveExactTarget(t *testing.T) {
	selection := Selection{OS: "linux", Arch: "amd64", Kind: "core-portable", Channel: "preview"}
	for _, ext := range []string{"gz", "xz"} {
		if !AssetNameValid("1.0.0", selection, "sidravia-v1.0.0-linux-amd64-core-portable.tar."+ext) {
			t.Fatal(ext)
		}
	}
	if AssetNameValid("1.0.0", selection, "sidravia-v1.0.0-linux-arm64-core-portable.tar.xz") {
		t.Fatal("wrong target accepted")
	}
	if legacy, _ := PackageSuffix(selection); legacy != "core-portable.tar.gz" {
		t.Fatal("legacy suffix changed")
	}
}
func TestXZSignedOwnershipUsesSameVerification(t *testing.T) {
	key, m, c, files := signedReleaseFixture(t)
	m.Assets[0].Name = strings.TrimSuffix(m.Assets[0].Name, ".gz") + ".xz"
	c.Name = m.Assets[0].Name
	roots, _ := NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	raw, _ := json.Marshal(m)
	verified, err := roots.Verify(raw, ed25519.Sign(key, raw), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verified.VerifyFiles(c, files); err != nil {
		t.Fatal(err)
	}
	if _, err = roots.Verify(raw, make([]byte, ed25519.SignatureSize), m.Version); err == nil {
		t.Fatal("unsigned XZ accepted")
	}
}
