package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadPasswordStdinFirstLineBehavior(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "LF", input: "alpha\n", want: "alpha"},
		{name: "CRLF", input: "alpha\r\n", want: "alpha"},
		{name: "spaces", input: "  alpha  \n", want: "  alpha  "},
		{name: "empty input", input: "", want: ""},
		{name: "empty line", input: "\n", want: ""},
		{name: "EOF without newline", input: "alpha", want: "alpha"},
		{name: "later line ignored", input: "alpha\nsecond-line", want: "alpha"},
		{name: "lone CR preserved", input: "alpha\r", want: "alpha\r"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readPasswordStdin(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("readPasswordStdin = %v, want nil", err)
			}
			if got != test.want {
				t.Error("readPasswordStdin returned different bytes")
			}
		})
	}
}

func TestReadPasswordStdinBound(t *testing.T) {
	accepted := []string{
		strings.Repeat("a", maxPasswordBytes),
		strings.Repeat("a", maxPasswordBytes) + "\n",
		strings.Repeat("a", maxPasswordBytes) + "\r\n",
	}
	for _, input := range accepted {
		password, err := readPasswordStdin(strings.NewReader(input))
		if err != nil {
			t.Fatalf("boundary password rejected: %v", err)
		}
		if len(password) != maxPasswordBytes {
			t.Errorf("accepted password length = %d, want %d", len(password), maxPasswordBytes)
		}
	}

	rejected := []string{
		strings.Repeat("a", maxPasswordBytes+1),
		strings.Repeat("a", maxPasswordBytes+1) + "\n",
		strings.Repeat("a", maxPasswordBytes+1) + "\r\n",
	}
	for _, input := range rejected {
		_, err := readPasswordStdin(strings.NewReader(input))
		if !errors.Is(err, errPasswordTooLong) {
			t.Errorf("oversize input error = %v, want password bound error", err)
		}
	}
}

func TestReadPasswordStdinPreservesSafeReadCause(t *testing.T) {
	cause := errors.New("injected read failure")
	_, err := readPasswordStdin(errorReader{err: cause})
	if err == nil {
		t.Fatal("readPasswordStdin = nil, want error")
	}
	if !errors.Is(err, cause) {
		t.Error("readPasswordStdin did not preserve read cause")
	}
	if err.Error() != "read password from stdin" {
		t.Errorf("readPasswordStdin error = %q, want static operation label", err)
	}
}

func TestReadHiddenPasswordSuccessOrderAndRestore(t *testing.T) {
	const echoFlag uint32 = 0x0004
	const originalMode uint32 = 0x0017
	actions := make([]string, 0, 4)
	var output bytes.Buffer
	readObserved := false

	password, err := readHiddenPassword(
		actionReader{
			onRead: func() {
				if !readObserved {
					actions = append(actions, "read")
					readObserved = true
				}
			},
			input: strings.NewReader("secret-marker\r\n"),
		},
		actionWriter{
			onWrite: func(value string) { actions = append(actions, "write:"+value) },
			writer:  &output,
		},
		echoFlag,
		func() (uint32, error) {
			actions = append(actions, "get")
			return originalMode, nil
		},
		func(mode uint32) error {
			if mode == originalMode&^echoFlag {
				actions = append(actions, "disable")
			} else if mode == originalMode {
				actions = append(actions, "restore")
			} else {
				t.Error("setMode received unexpected mode")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("readHiddenPassword = %v, want nil", err)
	}
	if password != "secret-marker" {
		t.Error("readHiddenPassword returned different bytes")
	}
	wantActions := []string{"write:Password: ", "get", "disable", "read", "write:\n", "restore"}
	if strings.Join(actions, "|") != strings.Join(wantActions, "|") {
		t.Errorf("actions = %v, want %v", actions, wantActions)
	}
	if output.String() != "Password: \n" {
		t.Errorf("prompt output = %q, want exact prompt and line break", output.String())
	}
}

func TestReadHiddenPasswordRestoresAfterReadFailure(t *testing.T) {
	readCause := errors.New("injected line read failure")
	const originalMode uint32 = 0x0017
	var modes []uint32
	var output bytes.Buffer

	_, err := readHiddenPassword(
		errorReader{err: readCause},
		&output,
		0x0004,
		func() (uint32, error) { return originalMode, nil },
		func(mode uint32) error {
			modes = append(modes, mode)
			return nil
		},
	)
	if err == nil {
		t.Fatal("readHiddenPassword = nil, want error")
	}
	if !errors.Is(err, readCause) {
		t.Error("readHiddenPassword did not preserve read cause")
	}
	if len(modes) != 2 || modes[1] != originalMode {
		t.Error("readHiddenPassword did not restore original mode")
	}
	if strings.Contains(err.Error(), "secret-marker") {
		t.Error("read error contains supplied secret marker")
	}
}

func TestReadHiddenPasswordDisableFailureDoesNotReadOrRestore(t *testing.T) {
	disableCause := errors.New("injected disable failure")
	reads, sets := 0, 0
	_, err := readHiddenPassword(
		actionReader{
			onRead: func() { reads++ },
			input:  strings.NewReader("secret-marker\n"),
		},
		io.Discard,
		0x0004,
		func() (uint32, error) { return 0x0017, nil },
		func(uint32) error {
			sets++
			return disableCause
		},
	)
	if !errors.Is(err, disableCause) {
		t.Error("readHiddenPassword did not preserve disable cause")
	}
	if reads != 0 {
		t.Errorf("input reads = %d, want 0", reads)
	}
	if sets != 1 {
		t.Errorf("setMode calls = %d, want 1", sets)
	}
}

func TestReadHiddenPasswordReturnsRestoreFailure(t *testing.T) {
	restoreCause := errors.New("injected restore failure")
	const originalMode uint32 = 0x0017
	sets := 0
	_, err := readHiddenPassword(
		strings.NewReader("secret-marker\n"),
		io.Discard,
		0x0004,
		func() (uint32, error) { return originalMode, nil },
		func(mode uint32) error {
			sets++
			if mode == originalMode {
				return restoreCause
			}
			return nil
		},
	)
	if err == nil {
		t.Fatal("readHiddenPassword = nil, want restore error")
	}
	if !errors.Is(err, restoreCause) {
		t.Error("readHiddenPassword did not preserve restore cause")
	}
	if strings.Contains(err.Error(), "secret-marker") {
		t.Error("restore error contains supplied secret marker")
	}
	if sets != 2 {
		t.Errorf("setMode calls = %d, want 2", sets)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type actionReader struct {
	onRead func()
	input  io.Reader
}

func (reader actionReader) Read(buffer []byte) (int, error) {
	reader.onRead()
	return reader.input.Read(buffer)
}

type actionWriter struct {
	onWrite func(string)
	writer  io.Writer
}

func (writer actionWriter) Write(value []byte) (int, error) {
	writer.onWrite(string(value))
	return writer.writer.Write(value)
}
