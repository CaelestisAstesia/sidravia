package update

import (
	"context"
	"encoding/json"
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
		persisted, current, state string
	}{
		{"pending", "1.2.2", "pending"},
		{"failed", "1.2.2", "failed"},
		{"completed", "1.2.2", "version_unconfirmed"},
		{"completed", "1.2.3", "completed"},
	} {
		path := filepath.Join(parent, qtOutcomeFilename)
		if tc.persisted == "pending" {
			if err := writeQtPending(context.Background(), path, "1.2.3"); err != nil {
				t.Fatal(err)
			}
		} else {
			data, err := json.Marshal(qtOutcome{TargetVersion: "1.2.3", State: tc.persisted})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		result, exists, err := readQtOutcome(context.Background(), parent, tc.current)
		if err != nil || !exists || result.State != tc.state || result.TargetVersion != "1.2.3" {
			t.Fatalf("%q with %q: got %+v, %t, %v; want %q", tc.persisted, tc.current, result, exists, err, tc.state)
		}
	}
}
