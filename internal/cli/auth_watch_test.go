package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestAuthWatchCommandHelpAndInvalidArgumentsNeverDispatch(t *testing.T) {
	called := false
	deps := commandDependencies{
		output: io.Discard,
		authWatch: func(context.Context) error {
			called = true
			return nil
		},
	}
	for _, args := range [][]string{{"auth", "watch", "secret-marker"}, {"auth", "watch", "--secret-marker"}} {
		err := runCommand(args, deps)
		if err == nil {
			t.Fatalf("%q unexpectedly succeeded", args)
		}
		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("%q leaked invalid argument: %v", args, err)
		}
		if called {
			t.Fatalf("%q dispatched watch", args)
		}
	}
	var watchHelp bytes.Buffer
	deps.output = &watchHelp
	if err := runCommand([]string{"help", "auth", "watch"}, deps); err != nil {
		t.Fatalf("watch help: %v", err)
	}
	if called {
		t.Fatal("watch help dispatched watch")
	}
	if !strings.Contains(watchHelp.String(), "Ctrl+C") || !strings.Contains(watchHelp.String(), "重新运行此命令") {
		t.Fatalf("watch help omitted stop or disconnect guidance: %q", watchHelp.String())
	}
	var output bytes.Buffer
	deps.output = &output
	if err := runCommand([]string{"help", "auth"}, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "watch  持续查看最新 Session 与网络状态") {
		t.Fatalf("auth help omitted watch command: %q", output.String())
	}
	if err := runCommand([]string{"auth", "watch"}, deps); err != nil {
		t.Fatalf("valid watch dispatch: %v", err)
	}
	if !called {
		t.Fatal("valid watch did not dispatch")
	}
}
