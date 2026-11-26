package candidatecache

import (
	"testing"
)

func TestCacheLockIsExclusive(t *testing.T) {
	path := t.TempDir() + "/lock"
	release, err := lockCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockCache(path); err == nil {
		second()
		release()
		t.Fatal("parallel owner")
	}
	release()
	again, err := lockCache(path)
	if err != nil {
		t.Fatal(err)
	}
	again()
}
