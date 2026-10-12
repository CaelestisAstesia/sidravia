package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestIntegrationCommandDispatch(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"path", "enable"}, {"path", "disable"}, {"autostart", "set", "gui"}, {"autostart", "set", "daemon"}, {"autostart", "disable"}} {
		var out bytes.Buffer
		called := false
		cmd := newIntegrationCommand(&out, func(_ context.Context, got []string) ([]byte, error) {
			called = true
			if !reflect.DeepEqual(got, args) {
				t.Fatalf("%v != %v", got, args)
			}
			return []byte(`{"version":1,"ok":true,"code":"ok","capabilities":["user_path","user_autostart"],"private":"do not forward"}`), nil
		})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil || !called || strings.Contains(out.String(), "private") {
			t.Fatalf("args=%v err=%v output=%s", args, err, out.String())
		}
	}
}
func TestIntegrationInvalidAndFailure(t *testing.T) {
	for _, args := range [][]string{{"status", "extra"}, {"autostart", "set", "other"}, {"path", "enable", "--other"}, {"--help"}} {
		var out bytes.Buffer
		cmd := newIntegrationCommand(&out, func(context.Context, []string) ([]byte, error) { t.Fatal("invalid/help dispatched"); return nil, nil })
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}
	var out bytes.Buffer
	var cause = errors.New("private native error")
	cmd := newIntegrationCommand(&out, func(context.Context, []string) ([]byte, error) {
		return []byte(`{"version":1,"ok":false,"code":"scheduled_task_conflict","capabilities":[]}`), cause
	})
	cmd.SetArgs([]string{"status"})
	err := cmd.Execute()
	if err == nil || strings.Contains(err.Error(), "private") || !strings.Contains(err.Error(), "scheduled_task_conflict") {
		t.Fatal(err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("lost underlying native cause")
	}
}
