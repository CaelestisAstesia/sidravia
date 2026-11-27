package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestQtOutcomeConfirmsTheRunningVersion(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "update-cache")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		record, current, state string
	}{
		{"1.2.3 pending\n", "1.2.2", "pending"},
		{"1.2.3 failed\n", "1.2.2", "failed"},
		{"1.2.3 completed\n", "1.2.2", "version_unconfirmed"},
		{"1.2.3 completed\n", "1.2.3", "completed"},
	} {
		if err := os.WriteFile(filepath.Join(parent, qtOutcomeFilename), []byte(tc.record), 0600); err != nil {
			t.Fatal(err)
		}
		result, exists, err := readQtOutcome(context.Background(), parent, tc.current)
		if err != nil || !exists || result.State != tc.state || result.TargetVersion != "1.2.3" {
			t.Fatalf("%q with %q: got %+v, %t, %v; want %q", tc.record, tc.current, result, exists, err, tc.state)
		}
	}
}
