package portable

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const xzSample = "/Td6WFoAAATm1rRGAgAhARYAAAB0L+WjAQAGcGF5bG9hZAAAix/Qg55JjzcAAR8HFi64cx+2830BAAAAAARZWg=="
const xzOversizeDictionary = "/Td6WFoAAATm1rRGAgAhASQAAABeH8f5AQAGcGF5bG9hZAAAix/Qg55JjzcAAR8HFi64cx+2830BAAAAAARZWg=="

func TestTarStreamsValidateAllMembersAndBoundDictionary(t *testing.T) {
	xzBytes, _ := base64.StdEncoding.DecodeString(xzSample)
	large, _ := base64.StdEncoding.DecodeString(xzOversizeDictionary)
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte("payload"))
	w.Close()
	for _, item := range []struct {
		name string
		data []byte
		bad  bool
	}{
		{"valid.tar.gz", gz.Bytes(), false}, {"valid.tar.xz", xzBytes, false},
		{"large.tar.xz", large, true}, {"truncated.tar.xz", xzBytes[:len(xzBytes)-5], true},
		{"trailing.tar.xz", append(append([]byte(nil), xzBytes...), 1), true},
	} {
		t.Run(item.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), item.name)
			os.WriteFile(path, item.data, 0600)
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := openTarStream(context.Background(), f, item.name)
			var data []byte
			if err == nil {
				defer r.Close()
				data, err = io.ReadAll(r)
			}
			if item.bad {
				if err == nil {
					t.Fatal("invalid stream accepted")
				}
			} else if err != nil || string(data) != "payload" {
				t.Fatalf("%q %v", data, err)
			}
		})
	}
	for _, ext := range []string{"gz", "xz"} {
		first := append([]byte(nil), gz.Bytes()...)
		if ext == "xz" {
			first = append([]byte(nil), xzBytes...)
		}
		second := append([]byte(nil), first...)
		second[len(second)-5] ^= 1
		path := filepath.Join(t.TempDir(), "members.tar."+ext)
		os.WriteFile(path, append(first, second...), 0600)
		f, _ := os.Open(path)
		r, err := openTarStream(context.Background(), f, path)
		if err == nil {
			_, err = io.ReadAll(r)
			r.Close()
		}
		f.Close()
		if err == nil {
			t.Fatal("bad trailing member ignored", ext)
		}
	}
	path := filepath.Join(t.TempDir(), "cancel.tar.xz")
	os.WriteFile(path, xzBytes, 0600)
	f, _ := os.Open(path)
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := openTarStream(ctx, f, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
