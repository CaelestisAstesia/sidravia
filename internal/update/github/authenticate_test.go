package github

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"sidravia/internal/update/releases"
)

func TestCandidateAuthenticationUsesPinnedRootsAndExactRelease(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "tampered", "wrong-asset", "wrong-version", "unknown-root", "unconfigured-root", "too-large"} {
		t.Run(mode, func(t *testing.T) {
			key, m, c, _ := signedReleaseFixture(t)
			if mode == "wrong-asset" {
				m.Assets[0].SHA256 = strings.Repeat("c", 64)
			}
			if mode == "wrong-version" {
				m.Version = "2.0.0"
			}
			data, _ := json.Marshal(m)
			signature := ed25519.Sign(key, data)
			if mode == "tampered" {
				data = append(data, ' ')
			}
			if mode == "too-large" {
				data = bytes.Repeat([]byte("x"), releases.ReleaseManifestLimit+1)
			}
			requests := 0
			source := NewGitHub(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				requests++
				prefix := "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/"
				if !strings.HasPrefix(r.URL.String(), prefix) {
					t.Fatal("candidate redirected to another release", r.URL)
				}
				body := data
				if strings.HasSuffix(r.URL.Path, releases.ReleaseSignatureName) {
					body = signature
				}
				status := http.StatusOK
				if mode == "missing" {
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})})
			source.publisherKeys, _ = releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
			if mode == "unknown-root" {
				source.publisherKeys = releases.PublisherTrust()
			}
			if mode == "unconfigured-root" {
				source.publisherKeys, _ = releases.NewPublisherKeys(nil)
			}
			c.DownloadURL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + c.Name
			verified, err := source.AuthenticateCandidate(context.Background(), c)
			if mode == "valid" {
				if err != nil || verified.BuildID() != "build" {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unauthenticated candidate accepted", mode)
			}
			if mode == "missing" && !errors.Is(err, ErrSignatureMissing) {
				t.Fatal(err)
			}
			if mode == "unconfigured-root" && requests != 0 {
				t.Fatal("unconfigured root performed a metadata request")
			}
		})
	}
}

func TestCheckOffersOnlyNativeSignedApply(t *testing.T) {
	for _, item := range []struct{ platform, kind string }{{"linux", "core-portable"}, {"windows", "setup"}, {"windows", "core-portable"}, {"windows", "gui-portable"}} {
		platform, kind := item.platform, item.kind
		t.Run(platform+"-"+kind, func(t *testing.T) {
			key, m, c, _ := signedReleaseFixture(t)
			m.Assets[0].OS = platform
			m.Assets[0].Kind = kind
			payload := "core"
			if kind == "gui-portable" {
				payload = "gui"
			}
			m.Assets[0].PayloadKind = payload
			suffix := kind + ".zip"
			if kind == "setup" {
				suffix = "setup.exe"
			}
			if platform == "linux" {
				suffix = kind + ".tar.gz"
			}
			c.Name = "sidravia-v1.0.0-" + platform + "-amd64-" + suffix
			m.Assets[0].Name = c.Name
			data, _ := json.Marshal(m)
			source := NewGitHub(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				body := data
				if r.URL.Host == "api.github.com" {
					item := fixtureRelease("1.0.0", 1)
					item.Assets[0].Name = c.Name
					item.Assets[0].Size = c.Size
					item.Assets[0].Digest = "sha256:" + c.SHA256
					item.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + c.Name
					body, _ = json.Marshal([]release{item})
				} else if strings.HasSuffix(r.URL.Path, releases.ReleaseSignatureName) {
					body = ed25519.Sign(key, data)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})})
			source.publisherKeys, _ = releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
			result, err := source.Check(context.Background(), releases.Selection{OS: platform, Arch: "amd64", Kind: kind, Channel: "stable", CurrentVersion: "0.9.0"})
			native := runtime.GOOS == platform && runtime.GOARCH == "amd64"
			if err != nil || result.Candidate == nil || result.Candidate.CanApply != native || (native && result.Candidate.ApplyReason != "source_verification_and_confirmation_required") {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}

func TestCheckDoesNotTreatCanceledMetadata404AsUnsigned(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key, _, c, _ := signedReleaseFixture(t)
	source := NewGitHub(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			item := fixtureRelease("1.0.0", 1)
			item.Assets[0].Name = c.Name
			item.Assets[0].Size = c.Size
			item.Assets[0].Digest = "sha256:" + c.SHA256
			item.Assets[0].URL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + c.Name
			body, _ := json.Marshal([]release{item})
			return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		cancel()
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	source.publisherKeys, _ = releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	result, err := source.Check(ctx, releases.Selection{OS: "linux", Arch: "amd64", Kind: "core-portable", Channel: "stable", CurrentVersion: "0.9.0"})
	if !errors.Is(err, context.Canceled) || result.Candidate != nil {
		t.Fatalf("canceled metadata became unsigned success: %+v %v", result, err)
	}
}

func TestSignedMetadataRetainsSourceBackoff(t *testing.T) {
	key, _, c, _ := signedReleaseFixture(t)
	for _, status := range []int{403, 429} {
		source := NewGitHub(&http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"600"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})})
		source.publisherKeys, _ = releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
		c.DownloadURL = "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + c.Name
		_, err := source.AuthenticateCandidate(context.Background(), c)
		var limited *RateLimitError
		if !errors.As(err, &limited) || time.Until(limited.RetryAt) < 9*time.Minute || time.Until(limited.RetryAt) > 11*time.Minute {
			t.Fatal("metadata lost bounded backoff", err)
		}
	}
}

func TestCheckRejectsCanceledBufferedAPIPage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := NewGitHub(&http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		data, _ := json.Marshal([]release{fixtureRelease("1.0.0", 1)})
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(cancelAtEOF{Reader: bytes.NewReader(data), cancel: cancel})}, nil
	})})
	result, err := source.Check(ctx, releases.Selection{OS: "windows", Arch: "amd64", Kind: "setup", Channel: "stable", CurrentVersion: "0.9.0"})
	if !errors.Is(err, context.Canceled) || result.Candidate != nil {
		t.Fatal("canceled API page returned a candidate", err)
	}
}
