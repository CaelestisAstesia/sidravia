package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sidravia/internal/release"
)

type cacheTransport func(*http.Request) (*http.Response, error)

func (f cacheTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the public commands and real secured persistence together. Only the
// network transport is replaced; version selection and byte checks stay real.
func TestCheckDownloadStatusRetainsAndReusesVerifiedPackage(t *testing.T) {
	checkDownloadStatusRetainsAndReusesVerifiedPackage(t, testPackageKind(), testPackageSuffix())
}
func TestLinuxGUICheckDownloadStatusRetainsAndReusesVerifiedPackage(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("Linux AMD64 GUI target")
	}
	checkDownloadStatusRetainsAndReusesVerifiedPackage(t, "gui-portable", "linux-amd64-gui-portable.tar.gz")
}
func checkDownloadStatusRetainsAndReusesVerifiedPackage(t *testing.T, kind, suffix string) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("GitHub core packages are Linux/Windows only")
	}
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("LOCALAPPDATA", root)
	payload := "synthetic package"
	sum := sha256.Sum256([]byte(payload))
	version, releaseID, assetID := "1.0.0", 1, 2
	downloads := 0
	original := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = original })
	http.DefaultClient = &http.Client{Transport: cacheTransport(func(r *http.Request) (*http.Response, error) {
		body := payload
		if r.URL.Host == "api.github.com" {
			name := "sidravia-v" + version + "-" + suffix
			body = fmt.Sprintf(`[{"id":%d,"tag_name":"v%s","assets":[{"id":%d,"name":"%s","state":"uploaded","size":%d,"digest":"sha256:%s","browser_download_url":"https://github.com/CaelestisAstesia/sidravia/releases/download/v%s/%s"}]}]`, releaseID, version, assetID, name, len(payload), hex.EncodeToString(sum[:]), version, name)
		} else if r.URL.Host == "github.com" {
			if strings.HasSuffix(r.URL.Path, release.ReleaseManifestName) || strings.HasSuffix(r.URL.Path, release.ReleaseSignatureName) {
				return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			downloads++
		} else {
			t.Fatalf("unexpected host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	run := func(operation string, more ...string) cached {
		t.Helper()
		args := append([]string{operation, "--package", kind, "--channel", "stable"}, more...)
		var out bytes.Buffer
		if exit := Run(context.Background(), args, "0.9.0", "fixture", &out); exit != 0 {
			t.Fatalf("%v: %s", args, out.String())
		}
		var result struct {
			Code   string
			Result cached
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Result
	}
	first := run("check")
	if first.Check.Candidate.CanApply {
		t.Fatal("unsigned candidate offered automatic application")
	}
	downloaded := run("download", "--candidate", first.Check.Candidate.ID)
	if downloaded.Downloaded == "" || downloads != 1 {
		t.Fatalf("%+v downloads %d", downloaded, downloads)
	}
	checked := run("check")
	status := run("status")
	reused := run("download", "--candidate", first.Check.Candidate.ID)
	if checked.Downloaded != downloaded.Downloaded || status.Downloaded != downloaded.Downloaded || reused.Downloaded != downloaded.Downloaded || downloads != 1 {
		t.Fatal("download state was lost or package downloaded again")
	}
	version, releaseID, assetID = "1.1.0", 3, 4
	next := run("check")
	if next.Downloaded != "" {
		t.Fatal("old package attributed to new candidate")
	}
	if _, err := os.Stat(filepath.Dir(downloaded.Downloaded)); !os.IsNotExist(err) {
		t.Fatalf("old cache retained: %v", err)
	}
	replacement := run("download", "--candidate", next.Check.Candidate.ID)
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(replacement.Downloaded)))
	if err != nil {
		t.Fatal(err)
	}
	packages := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "package-") {
			packages++
		}
	}
	if downloads != 2 || packages != 1 {
		t.Fatalf("downloads %d packages %d", downloads, packages)
	}
	if err := os.WriteFile(replacement.Downloaded, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	exit := Run(context.Background(), []string{"download", "--package", kind, "--channel", "stable", "--candidate", next.Check.Candidate.ID}, "0.9.0", "fixture", &out)
	if exit == 0 || !strings.Contains(out.String(), `"cache_invalid"`) || downloads != 2 {
		t.Fatalf("changed cache accepted: %s", out.String())
	}
}

func TestCacheNeverDeletesAnUnrecognizedLeaf(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "unrecognized")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCached(root, outside, &release.Candidate{Name: "unrecognized"}); err == nil {
		t.Fatal("unexpected path accepted")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "preserve" {
		t.Fatal("unknown file changed")
	}
}

func testPackageKind() string {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return "setup"
	}
	return "core-portable"
}
func testPackageSuffix() string {
	suffix := "core-portable.tar.gz"
	if runtime.GOOS == "windows" {
		suffix = "core-portable.zip"
	}
	if testPackageKind() == "setup" {
		suffix = "setup.exe"
	}
	return runtime.GOOS + "-" + runtime.GOARCH + "-" + suffix
}
