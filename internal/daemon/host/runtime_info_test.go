package host

import (
	"context"
	"testing"

	"sidravia/internal/ipc/contract"
)

type runtimeInfoTestReader struct {
	data   []byte
	exists bool
	err    error
}

func (reader runtimeInfoTestReader) Read(context.Context, string, int64) ([]byte, bool, error) {
	return reader.data, reader.exists, reader.err
}

func TestCleanupRuntimeInfoRemovesOnlyMatchingPIDAndToken(t *testing.T) {
	want := contract.RuntimeInfo{SchemaVersion: 1, Endpoint: "ws://127.0.0.1:1/ipc", PID: 100, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProductVersion: "v", BuildID: "b"}
	cases := []struct {
		name    string
		stored  contract.RuntimeInfo
		removed bool
	}{
		{name: "matching pid and token", stored: want, removed: true},
		{name: "different pid", stored: contract.RuntimeInfo{SchemaVersion: 1, Endpoint: want.Endpoint, PID: 101, Token: want.Token, ProductVersion: "v", BuildID: "b"}},
		{name: "different token", stored: contract.RuntimeInfo{SchemaVersion: 1, Endpoint: want.Endpoint, PID: want.PID, Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ProductVersion: "v", BuildID: "b"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data, err := contract.EncodeRuntimeInfo(test.stored)
			if err != nil {
				t.Fatal(err)
			}
			removed := false
			cleanupRuntimeInfo("runtime.json", want, runtimeInfoTestReader{data: data, exists: true}, func(string) error {
				removed = true
				return nil
			})
			if removed != test.removed {
				t.Fatalf("removed = %v, want %v", removed, test.removed)
			}
		})
	}
}
