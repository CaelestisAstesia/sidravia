//go:build !windows && !linux

package host

import (
	"context"
	"testing"
)

func TestRunUnsupportedOnOtherPlatforms(t *testing.T) {
	err := Run(context.Background(), Config{})
	if err == nil {
		t.Fatal("Run = nil, want Unsupported error")
	}
	if err.Error() != "sidraviad: unsupported platform; only Windows and Linux are supported" {
		t.Fatalf("Run error = %q", err)
	}
}
