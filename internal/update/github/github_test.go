package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sidravia/internal/update/releases"
)

func fixtureRelease(version string, id int64) release {
	name := "sidravia-v" + version + "-windows-amd64-setup.exe"
	return release{ID: id, Tag: "v" + version, Prerelease: strings.Contains(version, "-"), Assets: []asset{{ID: id, Name: name, State: "uploaded", Size: 3, Digest: "sha256:" + strings.Repeat("a", 64), URL: "https://github.com/" + releases.Repository + "/releases/download/v" + version + "/" + name}}}
}
func TestCheckSelectsSemVerAcrossPagesAndDoesNotAuthorizeExecution(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			json.NewEncoder(w).Encode([]release{fixtureRelease("1.0.0-alpha.10", 10)})
			return
		}
		w.Header().Add("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="last"`, server.URL, releases.Repository))
		w.Header().Add("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="next"; type="application/json"; title="releases, next"`, server.URL, releases.Repository))
		json.NewEncoder(w).Encode([]release{fixtureRelease("1.0.0-alpha.2", 2), fixtureRelease("0.9.0", 9)})
	}))
	defer server.Close()
	source := NewGitHub(server.Client())
	source.apiBase = server.URL
	got, err := source.Check(context.Background(), releases.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "preview", CurrentVersion: "1.0.0-alpha.1"})
	if err != nil || got.State != "available" || got.Candidate.Version != "1.0.0-alpha.10" || got.Candidate.CanApply {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = source.Check(context.Background(), releases.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "stable", CurrentVersion: "1.0.0"})
	if err != nil || got.State != "ahead_of_channel" || got.Candidate.Version != "0.9.0" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCheckFailsClosedOnBrokenPagesMetadataAndRateLimits(t *testing.T) {
	for _, mode := range []string{"failed-page", "bad-hash", "duplicate", "foreign-url", "rate-limit", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "rate-limit" {
					w.WriteHeader(429)
					return
				}
				if mode == "failed-page" && r.URL.Query().Get("page") == "2" {
					w.WriteHeader(500)
					return
				}
				item := fixtureRelease("1.0.0", 1)
				switch mode {
				case "bad-hash":
					item.Assets[0].Digest = "sha256:bad"
				case "duplicate":
					item.Assets = append(item.Assets, item.Assets[0])
				case "foreign-url":
					item.Assets[0].URL = "https://other.invalid/setup.exe"
				}
				if mode == "failed-page" || mode == "cycle" {
					w.Header().Set("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="next"`, server.URL, releases.Repository))
				}
				json.NewEncoder(w).Encode([]release{item})
			}))
			defer server.Close()
			source := NewGitHub(server.Client())
			source.apiBase = server.URL
			got, err := source.Check(context.Background(), releases.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "stable", CurrentVersion: "0.9.0"})
			if err == nil || got.Candidate != nil {
				t.Fatalf("%s: %+v %v", mode, got, err)
			}
		})
	}
}

func TestNextPageRejectsIncompleteOrAmbiguousLinks(t *testing.T) {
	for _, header := range []string{
		`<https://api.github.com/repos/x/releases?page=2>; rel="next"; broken`,
		`<https://api.github.com/repos/x/releases?page=2; rel="next"`,
		`<https://api.github.com/repos/x/releases?page=2>; rel="next`,
		`<https://api.github.com/repos/x/releases?page=2>; rel="next", <https://api.github.com/repos/x/releases?page=3>; rel="next"`,
		`<https://api.github.com/repos/x/releases?page=2#fragment>; rel=next`,
		`<https://api.github.com/repos/x/releases>; rel=next`,
	} {
		if _, err := nextPage(header); err == nil {
			t.Fatalf("accepted malformed pagination: %s", header)
		}
	}
	if next, err := nextPage(`<https://api.github.com/repos/x/releases?page=2>; type="application/json"; rel="alternate next"`); err != nil || next != "https://api.github.com/repos/x/releases?page=2" {
		t.Fatalf("valid link: %s %v", next, err)
	}
}

func TestLinuxGUISelectionNeverFallsBackToCoreOrLegacyPortable(t *testing.T) {
	gui := fixtureRelease("1.0.0", 1)
	name := "sidravia-v1.0.0-linux-amd64-gui-portable.tar.gz"
	gui.Assets[0].Name = name
	gui.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + name
	core := fixtureRelease("2.0.0", 2)
	core.Assets[0].Name = "sidravia-v2.0.0-linux-amd64-core-portable.tar.gz"
	core.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v2.0.0/" + core.Assets[0].Name
	legacy := fixtureRelease("3.0.0", 3)
	legacy.Assets[0].Name = "sidravia-v3.0.0-linux-amd64-portable.tar.gz"
	legacy.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v3.0.0/" + legacy.Assets[0].Name
	items := []release{core, legacy, gui}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(items) }))
	defer server.Close()
	source := NewGitHub(server.Client())
	source.apiBase = server.URL
	selection := releases.Selection{OS: "linux", Arch: "amd64", Kind: "gui-portable", Channel: "stable", CurrentVersion: "0.9.0"}
	result, err := source.Check(context.Background(), selection)
	if err != nil || result.Candidate == nil || result.Candidate.Name != name || result.Candidate.CanApply {
		t.Fatalf("%+v %v", result, err)
	}
	items = items[:2]
	result, err = source.Check(context.Background(), selection)
	if err != nil || result.State != "no_compatible_release" || result.Candidate != nil {
		t.Fatalf("%+v %v", result, err)
	}
	selection.Arch = "arm64"
	if _, err = source.Check(context.Background(), selection); err != releases.ErrUnsupported {
		t.Fatalf("unsupported arch: %v", err)
	}
}
