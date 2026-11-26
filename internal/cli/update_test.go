package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sidravia/internal/background/daemonctl"
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
		} else {
			var operation *commandOperationError
			if !errors.As(err, &operation) {
				t.Fatal("updater failure would be misclassified as command usage", err)
			}
		}
	}
}

func TestUpdateForwardsConfirmationAndRecoveryIdentity(t *testing.T) {
	for _, args := range [][]string{{"apply", "--candidate", "2:22", "--confirm"}, {"recover", "--operation", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--confirm"}} {
		var got []string
		var out bytes.Buffer
		cmd := newUpdateCommand(&out, func(_ context.Context, a []string) ([]byte, error) {
			got = a
			return []byte(`{"schemaVersion":1,"ok":true,"code":"ok"}`), nil
		})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		want := args
		if args[0] == "recover" {
			want = []string{"recover", "--confirm", "--operation", args[2]}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
}

func TestRPMUpdatesUsePackageManagerAndCompiledIdentity(t *testing.T) {
	identity := daemonctl.Identity{ProductVersion: "1.2.3", BuildID: "rpm-build"}
	for _, operation := range []string{"download", "status", "apply", "recover", "version"} {
		t.Run(operation, func(t *testing.T) {
			var out bytes.Buffer
			command := newUpdateCommand(&out, func(_ context.Context, args []string) ([]byte, error) { return rpmUpdateOutput(args, identity) })
			command.SetArgs([]string{operation})
			err := command.Execute()
			var response struct {
				OK     bool              `json:"ok"`
				Code   string            `json:"code"`
				Result map[string]string `json:"result"`
			}
			if decodeErr := json.Unmarshal(out.Bytes(), &response); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if operation == "version" {
				if err != nil || !response.OK || response.Result["productVersion"] != identity.ProductVersion || response.Result["buildId"] != identity.BuildID {
					t.Fatalf("%+v %v", response, err)
				}
			} else if err == nil || response.OK || response.Code != "system_package_managed" {
				t.Fatalf("%+v %v", response, err)
			}
		})
	}
}
