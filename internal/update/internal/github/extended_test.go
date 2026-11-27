package github

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"sidravia/internal/release"
)

func TestSeparatedReleaseChannelsUseOnlyTheirSignedMetadata(t *testing.T) {
	for _, target := range []struct{ os, kind, suffix string }{
		{"linux", "gui-portable", "gui-portable.tar.gz"},
		{"linux", "gui-portable", "gui-portable.tar.xz"},
		{"windows", "gui-portable", "gui-portable.zip"},
		{"windows", "setup", "setup.exe"},
		{"windows", release.QtCoreRepository, "qt-core-repository.zip"},
		{"windows", release.QtGUIRepository, "qt-gui-repository.zip"},
	} {
		t.Run(target.os+"/"+target.suffix, func(t *testing.T) {
			key, m, c, _ := signedReleaseFixture(t)
			item := fixtureRelease(c.Version, 1)
			name := "sidravia-v" + c.Version + "-" + target.os + "-arm64-" + target.suffix
			a := item.Assets[0]
			a.Name, a.Size, a.Digest = name, c.Size, "sha256:"+c.SHA256
			a.URL = "https://github.com/" + release.Repository + "/releases/download/" + item.Tag + "/" + name
			other := a
			other.ID++
			other.Name = strings.Replace(other.Name, "-arm64-", "-amd64-", 1)
			other.URL = strings.Replace(other.URL, "-arm64-", "-amd64-", 1)
			item.Assets = []asset{other, a}
			page, _ := json.Marshal([]githubRelease{item})
			m.Assets[0].OS, m.Assets[0].Arch, m.Assets[0].Kind = target.os, "arm64", target.kind
			m.Assets[0].Name, m.Assets[0].PayloadKind = name, "gui"
			manifestName, signatureName := release.ExtendedReleaseManifestName, release.ExtendedReleaseSignatureName
			if profile := release.QtRepositoryProfile(target.kind); profile != "" {
				m.Assets[0].PayloadKind = profile
				manifestName, signatureName = release.QtReleaseManifestName, release.QtReleaseSignatureName
			}
			data, _ := json.Marshal(m)
			signed := ed25519.Sign(key, data)
			for _, mode := range []string{"valid", "missing", "wrong-signature", "wrong-arch"} {
				t.Run(mode, func(t *testing.T) {
					manifest, signature := data, signed
					wantErr := error(nil)
					switch mode {
					case "missing":
						wantErr = ErrSignatureMissing
					case "wrong-signature":
						signature = make([]byte, ed25519.SignatureSize)
						wantErr = release.ErrUntrusted
					case "wrong-arch":
						changed := m
						changed.Assets = append([]release.ReleaseAsset(nil), m.Assets...)
						changed.Assets[0].Arch, changed.Assets[0].Name = "amd64", other.Name
						manifest, _ = json.Marshal(changed)
						signature = ed25519.Sign(key, manifest)
						wantErr = release.ErrMetadata
						if release.QtRepositoryProfile(target.kind) != "" {
							wantErr = release.ErrUntrusted
						}
					}
					source := NewGitHub(&http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
						var body []byte
						status := 200
						switch {
						case req.URL.Host == "api.github.com":
							body = page
						case strings.HasSuffix(req.URL.Path, "/"+manifestName):
							body = manifest
						case strings.HasSuffix(req.URL.Path, "/"+signatureName):
							if mode == "missing" {
								status = 404
							} else {
								body = signature
							}
						default:
							t.Fatalf("unexpected metadata fallback: %s", req.URL)
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
					})})
					source.publisherKeys, _ = release.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
					got, err := source.Check(context.Background(), release.Selection{OS: target.os, Arch: "arm64", Kind: target.kind, Channel: "stable", CurrentVersion: "0.9.0"})
					if !errors.Is(err, wantErr) {
						t.Fatalf("got %v, want %v", err, wantErr)
					}
					if wantErr != nil {
						if got.Candidate != nil {
							t.Fatalf("unverified candidate offered: %+v", got)
						}
					} else if got.Candidate == nil || got.Candidate.Name != name || !got.PublisherVerified {
						t.Fatalf("unexpected candidate: %+v", got)
					}
				})
			}
		})
	}
}
