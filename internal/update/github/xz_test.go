package github

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"sidravia/internal/update/releases"
)

func TestXZDiscoveryPrefersXZAndKeepsGzipFallback(t *testing.T) {
	for _, withXZ := range []bool{false, true} {
		r := fixtureRelease("1.0.0", 1)
		r.Assets[0].Name = "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz"
		r.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + r.Assets[0].Name
		if withXZ {
			a := r.Assets[0]
			a.ID = 2
			a.Name = strings.TrimSuffix(a.Name, ".gz") + ".xz"
			a.URL = strings.TrimSuffix(a.URL, ".gz") + ".xz"
			r.Assets = append(r.Assets, a)
		}
		raw, _ := json.Marshal([]release{r})
		source := NewGitHub(&http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "api.github.com" {
				t.Fatal("unexpected public request", req.URL)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
		})})
		source.publisherKeys = nil
		got, err := source.Check(context.Background(), releases.Selection{OS: "linux", Arch: "amd64", Kind: "core-portable", Channel: "stable", CurrentVersion: "0.9.0"})
		if err != nil || got.Candidate == nil || strings.HasSuffix(got.Candidate.Name, ".tar.xz") != withXZ {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
func TestXZCandidateUsesSeparatePublisherMetadata(t *testing.T) {
	key, m, c, _ := signedReleaseFixture(t)
	m.Assets[0].Name = strings.TrimSuffix(m.Assets[0].Name, ".gz") + ".xz"
	c.Name = m.Assets[0].Name
	c.DownloadURL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + c.Name
	raw, _ := json.Marshal(m)
	sig := ed25519.Sign(key, raw)
	source := NewGitHub(&http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch {
		case strings.HasSuffix(req.URL.Path, "/"+releases.XZReleaseManifestName):
			body = raw
		case strings.HasSuffix(req.URL.Path, "/"+releases.XZReleaseSignatureName):
			body = sig
		default:
			t.Fatal("wrong metadata endpoint", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	source.publisherKeys, _ = releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	if _, err := source.AuthenticateCandidate(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}
