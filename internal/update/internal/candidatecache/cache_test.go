package candidatecache

import (
	"os"
	"path/filepath"
	"sidravia/internal/release"
	"testing"
)

func TestCacheNeverDeletesAnUnrecognizedLeaf(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "unrecognized")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCached(root, outside, &release.Candidate{Name: "unrecognized"}); err == nil {
		t.Fatal("unexpected path accepted")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "preserve" {
		t.Fatal("unknown file changed")
	}
}
