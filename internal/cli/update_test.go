package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestUpdateDelegatesOnlyExplicitArguments(t *testing.T) {
	var got []string
	var out bytes.Buffer
	cmd := newUpdateCommand(&out, func(_ context.Context, args []string) ([]byte, error) {
		got = args
		return []byte(`{"schemaVersion":1,"ok":true,"code":"ok","result":{"state":"available"}}`), nil
	})
	cmd.SetArgs([]string{"download", "--channel", "preview", "--candidate", "1:2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"download", "--channel", "preview", "--candidate", "1:2"}) {
		t.Fatal(got)
	}
}
func TestUpdaterFailureDoesNotBecomeSuccessfulCLIResult(t *testing.T) {
	for _, data := range []string{"", `{"schemaVersion":99,"ok":true,"code":"ok"}`, `{"schemaVersion":1,"ok":false,"code":"apply_unavailable"}`} {
		var out bytes.Buffer
		cmd := newUpdateCommand(&out, func(context.Context, []string) ([]byte, error) {
			return []byte(data), errors.New("private process cause")
		})
		cmd.SetArgs([]string{"apply"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("updater failure accepted")
		}
	}
}
