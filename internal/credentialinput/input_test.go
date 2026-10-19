package credentialinput

import (
	"errors"
	"strings"
	"testing"
)

func TestExactlyPrintableASCIIButNotSpace(t *testing.T) {
	for i := 0; i < 256; i++ {
		value := string([]byte{byte(i)})
		err := Validate(value)
		if (err == nil) != (i >= 0x21 && i <= 0x7e) {
			t.Fatalf("byte %02x: %v", i, err)
		}
	}
	var all strings.Builder
	for i := 0x21; i <= 0x7e; i++ {
		all.WriteByte(byte(i))
	}
	if Validate(all.String()) != nil {
		t.Fatal("ASCII symbols rejected")
	}
	for _, value := range []string{" user", "user ", "p ass", "用户", "ａｂ１２！", "a\u00a0b", "a\u200bb", "🔒"} {
		original := value
		if err := Validate(value); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), value) {
			t.Fatal("invalid text accepted or echoed")
		}
		if value != original {
			t.Fatal("input altered")
		}
	}
}
