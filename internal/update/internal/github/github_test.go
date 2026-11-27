package github

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sidravia/internal/release"
)

func fixtureRelease(version string, id int64) githubRelease {
	name := "sidravia-v" + version + "-windows-amd64-setup.exe"
	return githubRelease{ID: id, Tag: "v" + version, Prerelease: strings.Contains(version, "-"), Assets: []asset{{ID: id, Name: name, State: "uploaded", Size: 3, Digest: "sha256:" + strings.Repeat("a", 64), URL: "https://github.com/" + release.Repository + "/releases/download/v" + version + "/" + name}}}
}

func TestCheckRPMSelectsMatchingPublishedPackageWithoutPortableSignature(t *testing.T) {
	stable := fixtureRelease("0.9.0", 1)
	preview := fixtureRelease("1.0.0-alpha.2", 2)
	newer := fixtureRelease("1.0.0-alpha.3", 3)
	add := func(r *githubRelease, name string) {
		a := r.Assets[0]
		a.ID = a.ID*100 + int64(len(r.Assets))
		a.Name = name
		a.URL = "https://github.com/" + release.Repository + "/releases/download/" + r.Tag + "/" + name
		r.Assets = append(r.Assets, a)
	}
	add(&stable, "sidravia-0.9.0-1.x86_64.rpm")
	add(&preview, "sidravia-1.0.0-alpha.2-1.x86_64.rpm")
	add(&preview, "sidravia-1.0.0~alpha.2-10.x86_64.rpm")
	add(&preview, "sidravia-core-1.0.0-alpha.2-2.x86_64.rpm")
	add(&newer, "sidravia-1.0.0-alpha.3-1.aarch64.rpm")
	add(&newer, "sidravia-core-1.0.0-alpha.3-1.x86_64.rpm")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+release.Repository+"/releases" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]githubRelease{stable, preview, newer})
	}))
	defer server.Close()
	source := NewGitHub(server.Client())
	source.apiBase = server.URL
	key, _, _, _ := signedReleaseFixture(t)
	source.publisherKeys, _ = release.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	for _, tc := range []struct{ kind, channel, current, state, name string }{
		{"gui-rpm", "preview", "1.0.0-alpha.1", "available", "sidravia-1.0.0~alpha.2-10.x86_64.rpm"},
		{"gui-rpm", "preview", "1.0.0-alpha.2", "up_to_date", "sidravia-1.0.0~alpha.2-10.x86_64.rpm"},
		{"gui-rpm", "stable", "1.0.0-alpha.1", "ahead_of_channel", "sidravia-0.9.0-1.x86_64.rpm"},
		{"core-rpm", "preview", "1.0.0-alpha.2", "available", "sidravia-core-1.0.0-alpha.3-1.x86_64.rpm"},
		{"core-rpm", "stable", "0.8.0", "no_compatible_release", ""},
	} {
		t.Run(tc.kind+"/"+tc.channel+"/"+tc.current, func(t *testing.T) {
			got, err := source.Check(context.Background(), release.Selection{OS: "linux", Arch: "amd64", Kind: tc.kind, Channel: tc.channel, CurrentVersion: tc.current})
			if err != nil || got.State != tc.state {
				t.Fatalf("%+v %v", got, err)
			}
			c := got.Candidate
			if tc.name == "" {
				if c != nil {
					t.Fatalf("unexpected candidate %+v", c)
				}
			} else if c == nil || c.Name != tc.name || got.PublisherVerified || c.CanApply || c.ApplyReason != "" {
				t.Fatalf("unexpected candidate %+v", c)
			}
		})
	}
}
func TestCheckSelectsSemVerAcrossPagesAndDoesNotAuthorizeExecution(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			json.NewEncoder(w).Encode([]githubRelease{fixtureRelease("1.0.0-alpha.10", 10)})
			return
		}
		w.Header().Add("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="last"`, server.URL, release.Repository))
		w.Header().Add("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="next"; type="application/json"; title="releases, next"`, server.URL, release.Repository))
		json.NewEncoder(w).Encode([]githubRelease{fixtureRelease("1.0.0-alpha.2", 2), fixtureRelease("0.9.0", 9)})
	}))
	defer server.Close()
	source := NewGitHub(server.Client())
	// Discovery-only fixtures never access public signature endpoints.
	source.publisherKeys = nil
	source.apiBase = server.URL
	got, err := source.Check(context.Background(), release.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "preview", CurrentVersion: "1.0.0-alpha.1"})
	if err != nil || got.State != "available" || got.Candidate.Version != "1.0.0-alpha.10" || got.Candidate.CanApply {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = source.Check(context.Background(), release.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "stable", CurrentVersion: "1.0.0"})
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
					w.Header().Set("Link", fmt.Sprintf(`<%s/repos/%s/releases?per_page=100&page=2>; rel="next"`, server.URL, release.Repository))
				}
				json.NewEncoder(w).Encode([]githubRelease{item})
			}))
			defer server.Close()
			source := NewGitHub(server.Client())
			// Discovery-only fixtures never access public signature endpoints.
			source.publisherKeys = nil
			source.apiBase = server.URL
			got, err := source.Check(context.Background(), release.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "stable", CurrentVersion: "0.9.0"})
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
	gui.Assets[0].URL = "https://github.com/" + release.Repository + "/releases/download/v1.0.0/" + name
	core := fixtureRelease("2.0.0", 2)
	core.Assets[0].Name = "sidravia-v2.0.0-linux-amd64-core-portable.tar.gz"
	core.Assets[0].URL = "https://github.com/" + release.Repository + "/releases/download/v2.0.0/" + core.Assets[0].Name
	legacy := fixtureRelease("3.0.0", 3)
	legacy.Assets[0].Name = "sidravia-v3.0.0-linux-amd64-portable.tar.gz"
	legacy.Assets[0].URL = "https://github.com/" + release.Repository + "/releases/download/v3.0.0/" + legacy.Assets[0].Name
	items := []githubRelease{core, legacy, gui}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(items) }))
	defer server.Close()
	source := NewGitHub(server.Client())
	// Discovery-only fixtures never access public signature endpoints.
	source.publisherKeys = nil
	source.apiBase = server.URL
	selection := release.Selection{OS: "linux", Arch: "amd64", Kind: "gui-portable", Channel: "stable", CurrentVersion: "0.9.0"}
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
	result, err = source.Check(context.Background(), selection)
	if err != nil || result.State != "no_compatible_release" || result.Candidate != nil {
		t.Fatalf("%+v %v", result, err)
	}
	selection.Arch = "386"
	if _, err = source.Check(context.Background(), selection); err != release.ErrUnsupported {
		t.Fatalf("unsupported arch: %v", err)
	}
}

func TestSystemPackageDiscoveryBindsArchitectureFlavorAndLocation(t *testing.T) {
	item := fixtureRelease("1.0.0-dev.audit.6", 1)
	names := []string{
		"sidravia_1.0.0~dev.audit.6-1_amd64.deb",
		"sidravia_1.0.0-dev.audit.6-10_amd64.deb",
		"sidravia_1.0.0~dev.audit.6-9_arm64.deb",
		"sidravia-core_1.0.0-dev.audit.6-2_arm64.deb",
		"sidravia-core_1.0.0~dev.audit.6-8_amd64.deb",
		"sidravia-core_1.0.0-dev.audit.6-23_amd64.deb.bak",
		"sidravia-core_1.0.0-dev.audit.60-30_amd64.deb",
		"sidravia-1.0.0~dev.audit.6-3.aarch64.rpm",
		"sidravia-1.0.0~dev.audit.6-30.x86_64.rpm",
	}
	for i, name := range names {
		a := item.Assets[0]
		a.ID = int64(i + 2)
		a.Name = name
		a.URL = "https://github.com/" + release.Repository + "/releases/download/" + item.Tag + "/" + name
		item.Assets = append(item.Assets, a)
	}
	for _, tt := range []struct {
		arch, kind string
		want       string
	}{
		{"amd64", "gui-deb", names[1]},
		{"arm64", "gui-deb", names[2]},
		{"amd64", "core-deb", names[4]},
		{"arm64", "core-deb", names[3]},
		{"arm64", "gui-rpm", names[7]},
	} {
		t.Run(tt.kind+"/"+tt.arch, func(t *testing.T) {
			for _, corruptURL := range []bool{false, true} {
				changed := item
				changed.Assets = append([]asset(nil), item.Assets...)
				if corruptURL {
					for i := range changed.Assets {
						if changed.Assets[i].Name == tt.want {
							changed.Assets[i].URL = strings.Replace(changed.Assets[i].URL, "github.com", "example.invalid", 1)
						}
					}
				}
				source := NewGitHub(&http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host != "api.github.com" {
						t.Fatal("system package triggered portable metadata lookup", req.URL)
					}
					raw, _ := json.Marshal([]githubRelease{changed})
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				})})
				got, err := source.Check(context.Background(), release.Selection{OS: "linux", Arch: tt.arch, Kind: tt.kind, Channel: "preview", CurrentVersion: "0.9.0"})
				if corruptURL {
					if !errors.Is(err, release.ErrMetadata) || got.Candidate != nil {
						t.Fatalf("invalid URL offered: %+v %v", got, err)
					}
				} else if err != nil || got.State != "available" || got.Candidate == nil || got.Candidate.Name != tt.want || got.PublisherVerified || got.Candidate.CanApply {
					t.Fatalf("%+v %v", got, err)
				}
			}
		})
	}
}
