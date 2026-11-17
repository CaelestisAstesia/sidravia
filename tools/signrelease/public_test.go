package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicInspectionAndVerificationNeverReadPrivateInput(t *testing.T) {
	manifest, signature, pub, original, seed := fixture(t)
	key := filepath.Join(filepath.Dir(manifest), "public.txt")
	if e := os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(pub)), 0600); e != nil {
		t.Fatal(e)
	}
	var out, stderr bytes.Buffer
	inspect := []string{"inspect", "--manifest", manifest, "--public-key", key, "--json"}
	if code := run(inspect, forbiddenInput{}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var report map[string]any
	if e := json.Unmarshal(out.Bytes(), &report); e != nil || report["signature"] != "not_checked" || report["artifactBytes"] != "not_checked" {
		t.Fatal(out.String(), e)
	}
	if _, e := os.Stat(signature); !os.IsNotExist(e) {
		t.Fatal("inspection signed a release")
	}
	if code := run([]string{"--manifest", manifest, "--output", signature}, bytes.NewReader(seed), &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	out.Reset()
	verify := []string{"verify", "--manifest", manifest, "--public-key", key, "--signature", signature, "--json"}
	if code := run(verify, forbiddenInput{}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if e := json.Unmarshal(out.Bytes(), &report); e != nil || report["signature"] != "passed" {
		t.Fatal(out.String(), e)
	}
	current, _ := os.ReadFile(manifest)
	if !bytes.Equal(current, original) {
		t.Fatal("public verifier rewrote manifest")
	}
	// JSON-equivalent whitespace still changes the exact signed byte sequence.
	os.WriteFile(manifest, append(original, '\n'), 0600)
	if code := run(verify, forbiddenInput{}, &out, &stderr); code == 0 {
		t.Fatal("changed signed bytes accepted")
	}
}
func TestPublicVerifierRejectsMalformedOrMismatchedInputs(t *testing.T) {
	for _, scenario := range []string{"wrong-key", "duplicate-fields", "extra-signature-byte", "manifest-link"} {
		t.Run(scenario, func(t *testing.T) {
			manifest, signature, pub, original, seed := fixture(t)
			key := filepath.Join(filepath.Dir(manifest), "public.txt")
			var out, stderr bytes.Buffer
			if run([]string{"--manifest", manifest, "--output", signature}, bytes.NewReader(seed), &out, &stderr) != 0 {
				t.Fatal(stderr.String())
			}
			switch scenario {
			case "wrong-key":
				pub = bytes.Repeat([]byte{1}, 32)
			case "duplicate-fields":
				os.WriteFile(manifest, bytes.Replace(original, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":2,"schemaVersion":2`), 1), 0600)
			case "extra-signature-byte":
				b, _ := os.ReadFile(signature)
				os.WriteFile(signature, append(b, 0), 0600)
			case "manifest-link":
				link := manifest + ".link"
				if e := os.Symlink(manifest, link); e != nil {
					t.Skip("symlink unavailable", e)
				}
				manifest = link
			}
			os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(pub)), 0600)
			if code := run([]string{"verify", "--manifest", manifest, "--public-key", key, "--signature", signature, "--json"}, forbiddenInput{}, &out, &stderr); code == 0 {
				t.Fatal("bad public request accepted")
			}
		})
	}
}
