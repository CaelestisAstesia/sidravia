//go:build linux

package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var errLinuxNewlineFailure = errors.New("injected newline failure")

type linuxFailWriter struct {
	failOn int
	calls  int
}

func (w *linuxFailWriter) Write(value []byte) (int, error) {
	w.calls++
	if w.calls == w.failOn {
		return 0, errLinuxNewlineFailure
	}
	return len(value), nil
}

func TestReadInteractivePasswordLinuxNonFileReturnsInstruction(t *testing.T) {
	var output bytes.Buffer
	getCalls := 0
	_, err := readInteractivePasswordWith(strings.NewReader("secret-marker"), &output,
		func(int, uint) (*unix.Termios, error) { getCalls++; return nil, nil },
		func(int, uint, *unix.Termios) error { t.Error("set called for non-file"); return nil },
	)
	if err == nil || err.Error() != linuxNonTerminalMessage {
		t.Errorf("err = %v, want non-terminal instruction", err)
	}
	if getCalls != 0 {
		t.Errorf("get calls = %d, want 0 for non-file", getCalls)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty for non-file", output.String())
	}
}

func TestReadInteractivePasswordLinuxNonTerminalReturnsInstruction(t *testing.T) {
	r, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var output bytes.Buffer
	setCalls := 0
	done := make(chan error, 1)
	go func() {
		_, e := readInteractivePasswordWith(r, &output,
			func(int, uint) (*unix.Termios, error) { return nil, errors.New("ENOTTY") },
			func(int, uint, *unix.Termios) error { setCalls++; return nil },
		)
		done <- e
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != linuxNonTerminalMessage {
			t.Errorf("err = %v, want non-terminal instruction", err)
		}
		if setCalls != 0 {
			t.Errorf("set calls = %d, want 0 for non-terminal", setCalls)
		}
		if output.Len() != 0 {
			t.Errorf("output = %q, want empty for non-terminal", output.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("non-terminal input blocked; readPasswordLine likely called")
	}
}

func TestReadInteractivePasswordLinuxSuccessOrderAndRestore(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var output bytes.Buffer
	var setValues []*unix.Termios
	getCalls := 0
	original := &unix.Termios{Lflag: unix.ECHO | 0x10}

	go func() { _, _ = w.Write([]byte("secret-marker\n")) }()
	defer w.Close()

	password, err := readInteractivePasswordWith(r, &output,
		func(int, uint) (*unix.Termios, error) { getCalls++; return original, nil },
		func(fd int, req uint, value *unix.Termios) error {
			setValues = append(setValues, value)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if password != "secret-marker" {
		t.Errorf("password = %q, want secret-marker", password)
	}
	if output.String() != "密码： \n" {
		t.Errorf("output = %q, want 密码： \\n", output.String())
	}
	if getCalls != 1 {
		t.Errorf("get calls = %d, want 1", getCalls)
	}
	if len(setValues) != 2 {
		t.Fatalf("set calls = %d, want 2 (clear and restore)", len(setValues))
	}
	if setValues[0].Lflag != 0x10 {
		t.Errorf("clear Lflag = %x, want ECHO cleared (0x10)", setValues[0].Lflag)
	}
	if setValues[1] != original {
		t.Error("restore did not use the exact original termios")
	}
}

func TestReadInteractivePasswordLinuxReadFailurePreservesCauseAndRestores(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	w.Close()
	var output bytes.Buffer
	original := &unix.Termios{Lflag: unix.ECHO}
	var setCalls int
	_, err = readInteractivePasswordWith(r, &output,
		func(int, uint) (*unix.Termios, error) { return original, nil },
		func(int, uint, *unix.Termios) error { setCalls++; return nil },
	)
	if err == nil {
		t.Fatal("want read error")
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("err = %v, want closed-read cause preserved", err)
	}
	if setCalls != 2 {
		t.Errorf("set calls = %d, want 2 (clear and restore)", setCalls)
	}
}

func TestReadInteractivePasswordLinuxDisableFailureDoesNotRead(t *testing.T) {
	cause := errors.New("injected disable failure")
	r, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, e := readInteractivePasswordWith(r, &output,
			func(int, uint) (*unix.Termios, error) { return &unix.Termios{Lflag: unix.ECHO}, nil },
			func(int, uint, *unix.Termios) error { return cause },
		)
		done <- e
	}()
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Errorf("err = %v, want disable cause", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disable failure blocked; readPasswordLine likely called")
	}
}

func TestReadInteractivePasswordLinuxRestoreFailurePreservesCause(t *testing.T) {
	cause := errors.New("injected restore failure")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	original := &unix.Termios{Lflag: unix.ECHO}
	sets := 0
	go func() { _, _ = w.Write([]byte("secret-marker\n")) }()
	defer w.Close()
	_, err = readInteractivePasswordWith(r, io.Discard,
		func(int, uint) (*unix.Termios, error) { return original, nil },
		func(fd int, req uint, value *unix.Termios) error {
			sets++
			if value == original {
				return cause
			}
			return nil
		},
	)
	if err == nil {
		t.Fatal("want restore error")
	}
	if !errors.Is(err, cause) {
		t.Errorf("err = %v, want restore cause", err)
	}
	if sets != 2 {
		t.Errorf("set calls = %d, want 2", sets)
	}
	if strings.Contains(err.Error(), "secret-marker") {
		t.Error("restore error contains secret marker")
	}
}

func TestReadInteractivePasswordLinuxNewlineFailurePreservesCause(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := &linuxFailWriter{failOn: 2}
	go func() { _, _ = w.Write([]byte("secret-marker\n")) }()
	defer w.Close()
	_, err = readInteractivePasswordWith(r, out,
		func(int, uint) (*unix.Termios, error) { return &unix.Termios{Lflag: unix.ECHO}, nil },
		func(int, uint, *unix.Termios) error { return nil },
	)
	if err == nil {
		t.Fatal("want newline error")
	}
	if !errors.Is(err, errLinuxNewlineFailure) {
		t.Errorf("err = %v, want newline cause", err)
	}
	if !strings.Contains(err.Error(), "写入密码换行") {
		t.Errorf("err = %v, want newline label", err)
	}
}
