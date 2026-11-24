//go:build windows

package testfixture

import (
	"crypto/ed25519"
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
	*sourceOverlay
}

// NewWindowsOverlay adds the synthetic publisher and fixed-URL file source.
// Production trust and runtime source selection are unaffected.
func NewWindowsOverlay(t testing.TB, repo, owned, source string, pub ed25519.PublicKey) *WindowsOverlay {
	t.Helper()
	overlay := &WindowsOverlay{newPublisherOverlay(t, repo, owned, pub)}
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
