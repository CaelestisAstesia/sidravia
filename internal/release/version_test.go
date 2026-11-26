package release

import "testing"

func TestSemVerOrderingAndBuildMetadata(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.2", "1.0.0-alpha.10", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "2.0.0", "999999999999999999999999.0.0"}
	for i, text := range order {
		v, err := ParseVersion(text)
		if err != nil {
			t.Fatal(err)
		}
		for j, other := range order {
			w, _ := ParseVersion(other)
			c := v.Compare(w)
			if (i < j && c >= 0) || (i == j && c != 0) || (i > j && c <= 0) {
				t.Fatalf("%s vs %s: %d", text, other, c)
			}
		}
	}
	a, _ := ParseVersion("1.0.0+one")
	b, _ := ParseVersion("1.0.0+two")
	if a.Compare(b) != 0 {
		t.Fatal("build metadata changes precedence")
	}
}
func TestVersionRejectsAmbiguousOrInvalidInputs(t *testing.T) {
	for _, text := range []string{"", "v1.0.0", "1.0", "01.0.0", "1.0.0-", "1.0.0-alpha.01", "1.0.0+", "1.0.0+build+more", "1.0.0-测试", "1.0.0\n", "1.0.0-alpha..one"} {
		if _, err := ParseVersion(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
