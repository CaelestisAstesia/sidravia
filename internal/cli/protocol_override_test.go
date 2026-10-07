package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestLoadProtocolContextOverrideIsBoundedAndStrict(t *testing.T) {
	directory := t.TempDir()
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "valid unicode object", data: []byte("{\"校园\":\"网\"}"), want: "{\"校园\":\"网\"}"},
		{name: "exact cap", data: append([]byte(`{"x":1}`), bytes.Repeat([]byte{' '}, contract.MaximumProtocolContextOverrideBytes-len(`{"x":1}`))...), want: `{"x":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(directory, "override.json")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := loadProtocolContextOverride(path, true)
			if err != nil || !bytes.Equal(bytes.TrimSpace(got), []byte(test.want)) {
				t.Fatalf("load result %q, error %v", got, err)
			}
		})
	}
	path := filepath.Join(directory, "too-large.json")
	data := append([]byte(`{"x":1}`), bytes.Repeat([]byte{' '}, contract.MaximumProtocolContextOverrideBytes+1-len(`{"x":1}`))...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProtocolContextOverride(path, true); err == nil {
		t.Fatal("oversized override accepted")
	}
}

func TestLoadProtocolContextOverrideRejectsNonObjectsAndHidesPrivateData(t *testing.T) {
	directory := t.TempDir()
	for _, content := range []string{`[]`, `null`, `{"x":1,"x":2}`, `{"x":1} {}`, `{"x":"\ud800"}`} {
		path := filepath.Join(directory, "secret-path-marker.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadProtocolContextOverride(path, true)
		if err == nil || !strings.Contains(err.Error(), protocolOverrideFileError) {
			t.Fatalf("invalid content %q error = %v", content, err)
		}
		if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), content) {
			t.Fatalf("public error leaked private input: %v", err)
		}
	}
	if _, err := loadProtocolContextOverride("", true); err == nil {
		t.Fatal("empty supplied path accepted")
	}
	if _, err := loadProtocolContextOverride("a\x00b", true); err == nil {
		t.Fatal("NUL path accepted")
	}
	if value, err := loadProtocolContextOverride("", false); err != nil || value != nil {
		t.Fatalf("absent file result = %q, %v", value, err)
	}
}

type trackedOverrideReadCloser struct {
	data       []byte
	readErr    error
	closeErr   error
	readBytes  int
	closeCount int
}

func (reader *trackedOverrideReadCloser) Read(buffer []byte) (int, error) {
	if len(reader.data) > 0 {
		count := copy(buffer, reader.data)
		reader.data = reader.data[count:]
		reader.readBytes += count
		return count, nil
	}
	if reader.readErr != nil {
		return 0, reader.readErr
	}
	return 0, io.EOF
}

func (reader *trackedOverrideReadCloser) Close() error {
	reader.closeCount++
	return reader.closeErr
}

func TestReadProtocolContextOverrideOwnsBoundedReadAndClose(t *testing.T) {
	t.Run("one bounded read and close", func(t *testing.T) {
		reader := &trackedOverrideReadCloser{data: []byte(`{"factory-private-field":{"opaque":true}}`)}
		got, err := readProtocolContextOverride(reader)
		if err != nil || string(got) != `{"factory-private-field":{"opaque":true}}` || reader.closeCount != 1 {
			t.Fatalf("result=%s error=%v closes=%d", got, err, reader.closeCount)
		}
		if reader.readBytes > contract.MaximumProtocolContextOverrideBytes+1 {
			t.Fatalf("read %d bytes beyond the bounded limit", reader.readBytes)
		}
	})

	t.Run("read and close causes are both retained", func(t *testing.T) {
		readCause := errors.New("private read marker")
		closeCause := errors.New("private close marker")
		reader := &trackedOverrideReadCloser{readErr: readCause, closeErr: closeCause}
		_, err := readProtocolContextOverride(reader)
		if !errors.Is(err, readCause) || !errors.Is(err, closeCause) || reader.closeCount != 1 {
			t.Fatalf("error=%v closes=%d", err, reader.closeCount)
		}
		if err.Error() != protocolOverrideFileError || strings.Contains(err.Error(), "private") {
			t.Fatalf("public error leaked a private cause: %v", err)
		}
	})

	t.Run("close failure after successful read", func(t *testing.T) {
		closeCause := errors.New("private close marker")
		reader := &trackedOverrideReadCloser{data: []byte(`{"valid":true}`), closeErr: closeCause}
		_, err := readProtocolContextOverride(reader)
		if !errors.Is(err, closeCause) || reader.closeCount != 1 || err.Error() != protocolOverrideFileError {
			t.Fatalf("error=%v closes=%d", err, reader.closeCount)
		}
	})

	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "invalid object", data: []byte(`[]`)},
		{name: "oversized object", data: append([]byte(`{"x":1}`), bytes.Repeat([]byte{' '}, contract.MaximumProtocolContextOverrideBytes+1-len(`{"x":1}`))...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &trackedOverrideReadCloser{data: test.data}
			_, err := readProtocolContextOverride(reader)
			if err == nil || reader.closeCount != 1 || reader.readBytes > contract.MaximumProtocolContextOverrideBytes+1 {
				t.Fatalf("error=%v closes=%d readBytes=%d", err, reader.closeCount, reader.readBytes)
			}
		})
	}
}
