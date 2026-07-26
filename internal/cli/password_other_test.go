//go:build !windows

package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestNonWindowsInteractivePasswordIsUnsupportedWithoutReading(t *testing.T) {
	reads := 0
	input := actionReader{
		onRead: func() { reads++ },
		input:  strings.NewReader("secret-marker\n"),
	}
	var output bytes.Buffer

	_, err := readInteractivePassword(input, &output)
	if err == nil {
		t.Fatal("readInteractivePassword = nil, want Unsupported-style error")
	}
	if got := err.Error(); got != "interactive password input is unsupported on this platform; use --password-stdin" {
		t.Errorf("readInteractivePassword error = %q, want static Unsupported-style error", got)
	}
	if reads != 0 {
		t.Errorf("input reads = %d, want 0", reads)
	}
	if output.Len() != 0 {
		t.Errorf("stderr = %q, want empty", output.String())
	}

	var _ io.Reader = input
}
