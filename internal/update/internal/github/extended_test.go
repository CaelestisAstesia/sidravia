package github

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
			profile := release.QtRepositoryProfile(target.kind)
			payload := []byte("repository bytes for " + target.kind)
			if profile != "" {
				c.Size = int64(len(payload))
				c.SHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
				m.Assets[0].Size, m.Assets[0].SHA256 = c.Size, c.SHA256
			}
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
			m.Assets[0].OS, m.Assets[0].Arch, m.Assets[0].Kind = target.os, "arm64", target.kind
			m.Assets[0].Name, m.Assets[0].PayloadKind = name, "gui"
			manifestName, signatureName := release.ExtendedReleaseManifestName, release.ExtendedReleaseSignatureName
			if profile != "" {
				m.Assets[0].PayloadKind = profile
				manifestName, signatureName = release.QtReleaseManifestName, release.QtReleaseSignatureName
				for _, competitor := range []struct{ kind, suffix string }{
					{release.QtCoreRepository, "qt-core-repository.zip"},
					{release.QtGUIRepository, "qt-gui-repository.zip"},
					{release.QtOfflineInstaller, "qt-setup.exe"},
				} {
					if competitor.kind == target.kind {
						continue
					}
					competing := a
					competing.ID = int64(len(item.Assets) + 10)
					competing.Name = strings.TrimSuffix(name, target.suffix) + competitor.suffix
					competing.URL = strings.TrimSuffix(a.URL, target.suffix) + competitor.suffix
					item.Assets = append([]asset{competing}, item.Assets...)
					entry := m.Assets[0]
					entry.Kind, entry.Name = competitor.kind, competing.Name
					entry.PayloadKind = "gui"
					if competitor.kind == release.QtCoreRepository {
						entry.PayloadKind = "core"
					}
					m.Assets = append(m.Assets, entry)
				}
			}
			page, _ := json.Marshal([]githubRelease{item})
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
						if profile != "" {
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
						case profile != "" && mode == "valid" && req.URL.String() == a.URL:
							body = payload
						default:
							t.Fatalf("unexpected metadata fallback: %s", req.URL)
						}
						return &http.Response{StatusCode: status, ContentLength: int64(len(body)), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
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
					} else if profile != "" {
						cache := t.TempDir()
						path, err := Download(context.Background(), source.client, *got.Candidate, cache)
						if err != nil {
							t.Fatal(err)
						}
						gotBytes, err := os.ReadFile(path)
						if err != nil || filepath.Base(path) != name || !bytes.Equal(gotBytes, payload) {
							t.Fatalf("wrong repository downloaded: %s, %v", path, err)
						}
					}
				})
			}
		})
	}
}
