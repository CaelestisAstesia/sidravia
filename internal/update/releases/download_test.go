package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDownloadVerifiesBytesAndNeverLeavesBadPackage(t *testing.T) {
	data := "synthetic update bytes"
	hash := sha256.Sum256([]byte(data))
	name := "sidravia-v1.0.0-windows-amd64-setup.exe"
	candidate := Candidate{Version: "1.0.0", Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), DownloadURL: "https://github.com/" + repository + "/releases/download/v1.0.0/" + name}
	for _, body := range []string{data, "truncated", data + "extra", strings.Repeat("x", len(data))} {
		t.Run(body, func(t *testing.T) {
			cache := t.TempDir()
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			path, err := Download(context.Background(), client, candidate, cache)
			if body == data {
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != data {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, ErrIntegrity) {
					t.Fatal(err)
				}
				entries, _ := os.ReadDir(cache)
				if len(entries) != 0 {
					t.Fatal("partial candidate retained")
				}
			}
		})
	}
}
func TestDownloadRejectsArbitraryPathAndHostBeforeNetwork(t *testing.T) {
	for _, candidate := range []Candidate{{Version: "1.0.0", Name: "../secret"}, {Version: "1.0.0", Name: "sidravia-v1.0.0-windows-amd64-setup.exe", Size: 1, SHA256: strings.Repeat("a", 64), DownloadURL: "https://foreign.invalid/setup.exe"}} {
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("network called"); return nil, nil })}
		if _, err := Download(context.Background(), client, candidate, t.TempDir()); !errors.Is(err, ErrMetadata) {
			t.Fatal(err)
		}
	}
}
