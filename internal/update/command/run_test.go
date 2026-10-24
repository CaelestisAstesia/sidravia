package command

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestNonMutatingCommandsAndDisabledApply(t *testing.T) {
	for _, item := range []struct {
		args []string
		code string
		exit int
	}{{[]string{"--help"}, "ok", 0}, {[]string{"version"}, "ok", 0}, {[]string{"invalid"}, "invalid_arguments", 1}, {[]string{"download"}, "invalid_arguments", 1}, {[]string{"apply", "--candidate", "1:2"}, "apply_unavailable", 1}} {
		var out bytes.Buffer
		exit := Run(context.Background(), item.args, "0.4.0-alpha.1", "fixture", &out)
		var result response
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != item.code || exit != item.exit {
			t.Fatalf("%v %s %d", item.args, result.Code, exit)
		}
	}
}
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
