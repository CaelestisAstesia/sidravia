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
	if got := err.Error(); got != "当前平台不支持交互式密码输入；请使用 --password-stdin" {
		t.Errorf("readInteractivePassword error = %q, want static Chinese Unsupported-style error", got)
	}
	if reads != 0 {
		t.Errorf("input reads = %d, want 0", reads)
	}
	if output.Len() != 0 {
		t.Errorf("stderr = %q, want empty", output.String())
	}

	var _ io.Reader = input
}
