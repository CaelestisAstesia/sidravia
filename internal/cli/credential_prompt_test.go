package cli

import "testing"

func assertClearedBytes(t *testing.T, buffer []byte) {
	t.Helper()
	if len(buffer) == 0 {
		t.Fatal("no owned buffer was observed")
	}
	for _, value := range buffer {
		if value != 0 {
			t.Fatal("owned buffer was not cleared")
		}
	}
}
