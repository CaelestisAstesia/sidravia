//go:build windows

// Package testfixture supports source overlays for native update tests.
package testfixture

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// WindowsOverlay builds fixture-only binaries in a caller-owned directory.
// The caller remains responsible for test process lifetime and cleanup.
type WindowsOverlay struct {
	t            testing.TB
	repo         string
	owned        string
	replacements map[string]string
}

// NewWindowsOverlay adds the synthetic publisher and fixed-URL file source.
// Production trust and runtime source selection are unaffected.
func NewWindowsOverlay(t testing.TB, repo, owned, source string, pub ed25519.PublicKey) *WindowsOverlay {
	t.Helper()
	overlay := &WindowsOverlay{t: t, repo: repo, owned: owned, replacements: map[string]string{}}
	overlay.Replace("internal/update/releases/trust.go", func(s string) string {
		parts := []string{}
		for _, b := range pub {
			parts = append(parts, fmt.Sprint(b))
		}
		return strings.Replace(s, "NewPublisherKeys([]ed25519.PublicKey{ed25519.PublicKey(publisherPublicKey)})", "NewPublisherKeys([]ed25519.PublicKey{{"+strings.Join(parts, ",")+"}})", 1)
	})
	overlay.Replace("internal/update/github/github.go", func(s string) string {
		s = strings.Replace(s, "\"context\"", "\"context\"\n\"os\"\n\"bytes\"\n\"path/filepath\"", 1)
		return s + fmt.Sprintf(`
func init(){http.DefaultTransport=fixtureTransport{fallback:http.DefaultTransport}}
type fixtureTransport struct{fallback http.RoundTripper}
func(f fixtureTransport)RoundTrip(r *http.Request)(*http.Response,error){
 var file string
 if r.URL.Scheme=="http" && r.URL.Hostname()=="127.0.0.1" && r.URL.Path=="/ipc" {return f.fallback.RoundTrip(r)}
 if r.URL.Scheme!="https"{return nil,releases.ErrMetadata}
 if r.URL.Host=="api.github.com" && r.URL.Path=="/repos/"+releases.Repository+"/releases"{file="api.json"}else if r.URL.Host=="github.com" && strings.HasPrefix(r.URL.Path,"/"+releases.Repository+"/releases/download/v"){
  tail:=strings.TrimPrefix(r.URL.Path,"/"+releases.Repository+"/releases/download/v");parts:=strings.Split(tail,"/");if len(parts)!=2 || strings.Contains(tail,"..") || strings.Contains(tail,"\\"){return nil,releases.ErrMetadata};file=filepath.Join(parts[0],parts[1])
 }else{return nil,releases.ErrMetadata}
 data,e:=os.ReadFile(filepath.Join(%s,file));status:=200;if os.IsNotExist(e){data=nil;status=404}else if e!=nil{return nil,e}
 return &http.Response{StatusCode:status,Header:make(http.Header),ContentLength:int64(len(data)),Body:io.NopCloser(bytes.NewReader(data)),Request:r},nil
}
`, strconv.Quote(source))
	})
	return overlay
}

// Replace records a fixture-specific patch without editing the repository.
func (o *WindowsOverlay) Replace(relative string, edit func(string) string) {
	o.t.Helper()
	original := filepath.Join(o.repo, filepath.FromSlash(relative))
	data, err := os.ReadFile(original)
	if err != nil {
		o.t.Fatal(err)
	}
	updated := edit(string(data))
	if updated == string(data) {
		o.t.Fatal("overlay source changed", relative)
	}
	destination := filepath.Join(o.owned, strings.ReplaceAll(relative, "/", "-"))
	if err := os.WriteFile(destination, []byte(updated), 0o600); err != nil {
		o.t.Fatal(err)
	}
	o.replacements[original] = destination
}

// Write saves the manifest used by go build.
func (o *WindowsOverlay) Write() string {
	o.t.Helper()
	path := filepath.Join(o.owned, "overlay.json")
	data, err := json.Marshal(map[string]map[string]string{"Replace": o.replacements})
	if err != nil {
		o.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		o.t.Fatal(err)
	}
	return path
}

// BuildProduct builds the same four entry points for each fixture image.
func (o *WindowsOverlay) BuildProduct(overlayPath, target, version, build string) {
	o.t.Helper()
	for _, role := range []string{"sidraviactl", "sidraviad", "sidraviaupdater", "sidraviamgr"} {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go.exe"),
			"build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-overlay", overlayPath,
			"-ldflags", "-X main.ProductVersion="+version+" -X main.BuildID="+build,
			"-o", filepath.Join(target, role+".exe"), "./cmd/"+role)
		cmd.Dir = o.repo
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			o.t.Fatalf("build %s: %s %v", role, output, err)
		}
	}
}
