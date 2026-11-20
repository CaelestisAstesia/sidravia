package github

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

	"sidravia/internal/update/releases"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDownloadVerifiesBytesAndNeverLeavesBadPackage(t *testing.T) {
	data := "synthetic update bytes"
	hash := sha256.Sum256([]byte(data))
	name := "sidravia-v1.0.0-windows-amd64-setup.exe"
	candidate := releases.Candidate{Version: "1.0.0", Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), DownloadURL: "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + name}
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
	for _, candidate := range []releases.Candidate{{Version: "1.0.0", Name: "../secret"}, {Version: "1.0.0", Name: "sidravia-v1.0.0-windows-amd64-setup.exe", Size: 1, SHA256: strings.Repeat("a", 64), DownloadURL: "https://foreign.invalid/setup.exe"}} {
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("network called"); return nil, nil })}
		if _, err := Download(context.Background(), client, candidate, t.TempDir()); !errors.Is(err, releases.ErrMetadata) {
			t.Fatal(err)
		}
	}
}

// Simulate cancellation delivered with the last response bytes. A transport may
// already have buffered all bytes even though the caller has canceled the work.
type cancelAtEOF struct {
	io.Reader
	cancel context.CancelFunc
}

func (r cancelAtEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}

func TestDownloadCanceledBeforePublicationLeavesNoPackage(t *testing.T) {
	payload := "synthetic update bytes"
	hash := sha256.Sum256([]byte(payload))
	name := "sidravia-v1.0.0-windows-amd64-setup.exe"
	candidate := releases.Candidate{Version: "1.0.0", Name: name, Size: int64(len(payload)), SHA256: hex.EncodeToString(hash[:]), DownloadURL: "https://github.com/" + releases.Repository + "/releases/download/v1.0.0/" + name}
	for _, before := range []bool{false, true} {
		when := "buffered-body"
		if before {
			when = "before-request"
		}
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := t.TempDir()
			requests := 0
			client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				requests++
				body := cancelAtEOF{Reader: strings.NewReader(payload), cancel: cancel}
				return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(body), Header: make(http.Header)}, nil
			})}
			if before {
				cancel()
			}
			path, err := Download(ctx, client, candidate, cache)
			if !errors.Is(err, context.Canceled) || path != "" {
				t.Errorf("canceled download published a package: path=%q err=%v", path, err)
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 0 {
				t.Errorf("canceled package retained: %v, %v", entries, err)
			}
			if before && requests != 0 {
				t.Errorf("canceled download performed %d requests", requests)
			}
		})
	}
}
