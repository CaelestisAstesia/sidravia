package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sidravia/internal/cli/internal/clitest"
	"strings"
	"testing"

	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/ipc/contract"
)

func hotListDependencies(t *testing.T, connection daemonconn.Client, output *bytes.Buffer) readOnlyCommandDependencies {
	t.Helper()
	return readOnlyCommandDependencies{
		connection: daemonconn.Connection{
			Acquire: func(_ context.Context) (daemonconn.Client, error) {
				return connection, nil
			},
			Timeout: 1,
		},
		stdout: output,
	}
}

func TestAuthListCallsExactMethodAndRendersAllSessions(t *testing.T) {
	result := contract.SessionListResult{CleanupRequiredSessionIDs: []string{}, Sessions: []contract.SessionResult{
		{
			ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}, Revision: 1, Intent: "maintain_authentication", AuthenticationSessionID: "session-1",
			InstitutionProfileID:     "jlu",
			InstitutionDisplayName:   "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
			AccountName:              "alice2024",
			State:                    "authenticated",
			UpdatedAt:                "2026-07-27T02:25:38+08:00",
		},
		{
			ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}, Revision: 1, Intent: "maintain_authentication", AuthenticationSessionID: "session-2",
			InstitutionDisplayName: "other", InstitutionProfileID: "other",
			AuthenticationProtocolID: "test-protocol",
			AccountName:              "bob",
			State:                    "suspended",
			UpdatedAt:                "2026-07-27T02:27:50+08:00",
		},
	}}
	data, err := contract.MarshalSessionListResult(result)
	if err != nil {
		t.Fatal(err)
	}
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionList {
			t.Errorf("method = %q, want %q", method, contract.MethodSessionList)
		}
		if string(payload) != `{}` {
			t.Errorf("payload = %s, want {}", payload)
		}
		return contract.NewSuccessResponse("1", data), nil
	}}
	var output bytes.Buffer
	if err := runAuthList(hotListDependencies(t, connection, &output)); err != nil {
		t.Fatalf("runAuthList = %v", err)
	}
	want := "会话（2）：\n" +
		"- session-1 | 状态：已认证（authenticated） | 机构：吉林大学（JLU） | 账号：alice2024 | 更新时间：2026-07-27T02:25:38+08:00\n" +
		"- session-2 | 状态：已暂停（suspended） | 机构：other（other） | 账号：bob | 更新时间：2026-07-27T02:27:50+08:00\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
	if connection.CallCount != 1 || connection.CloseCount != 1 {
		t.Errorf("calls/close = %d/%d, want 1/1", connection.CallCount, connection.CloseCount)
	}
}

func TestAuthListGuidesBlockedAndRetryingSessionsToStatus(t *testing.T) {
	result := contract.SessionListResult{CleanupRequiredSessionIDs: []string{}, Sessions: []contract.SessionResult{
		{
			ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}, Revision: 1, Intent: "maintain_authentication", AuthenticationSessionID: "session-blocked",
			InstitutionProfileID:     "jlu",
			InstitutionDisplayName:   "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
			AccountName:              "alice",
			State:                    "blocked_by_error",
			UpdatedAt:                "2026-08-03T10:00:00+08:00",
		},
		{
			ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}, Revision: 1, Intent: "maintain_authentication", AuthenticationSessionID: "session-retrying",
			InstitutionProfileID:     "jlu",
			InstitutionDisplayName:   "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
			AccountName:              "bob",
			State:                    "waiting_before_retry",
			UpdatedAt:                "2026-08-03T10:01:00+08:00",
		},
		{
			ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}, Revision: 1, Intent: "maintain_authentication", AuthenticationSessionID: "session-normal",
			InstitutionProfileID:     "jlu",
			InstitutionDisplayName:   "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
			AccountName:              "carol",
			State:                    "authenticated",
			UpdatedAt:                "2026-08-03T10:02:00+08:00",
		},
	}}
	data, err := contract.MarshalSessionListResult(result)
	if err != nil {
		t.Fatal(err)
	}
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionList {
			t.Errorf("method = %q, want %q", method, contract.MethodSessionList)
		}
		if string(payload) != `{}` {
			t.Errorf("payload = %s, want {}", payload)
		}
		return contract.NewSuccessResponse("1", data), nil
	}}
	var output bytes.Buffer
	if err := runAuthList(hotListDependencies(t, connection, &output)); err != nil {
		t.Fatalf("runAuthList = %v", err)
	}
	got := output.String()
	for _, command := range []string{
		"  查看详情：sidraviactl auth status session-blocked\n",
		"  查看详情：sidraviactl auth status session-retrying\n",
	} {
		if strings.Count(got, command) != 1 {
			t.Errorf("status command count for %q = %d, output = %q", command, strings.Count(got, command), got)
		}
	}
	if strings.Contains(got, "sidraviactl auth status session-normal") {
		t.Errorf("normal Session received a status command: %q", got)
	}
	if connection.CallCount != 1 || connection.CloseCount != 1 {
		t.Errorf("calls/close = %d/%d, want 1/1", connection.CallCount, connection.CloseCount)
	}
}

func TestProfileListRendersProfilesAndEmptyLists(t *testing.T) {
	t.Run("profiles", func(t *testing.T) {
		data, _ := contract.MarshalProfileListResult(contract.ProfileListResult{Profiles: []contract.ProfileSummaryResult{{
			InstitutionProfileID:     "jlu",
			DisplayName:              "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
		}}})
		connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodProfileList || string(payload) != `{}` {
				t.Errorf("call = %q %s", method, payload)
			}
			return contract.NewSuccessResponse("1", data), nil
		}}
		var output bytes.Buffer
		if err := runProfileList(hotListDependencies(t, connection, &output)); err != nil {
			t.Fatalf("runProfileList = %v", err)
		}
		want := "机构 Profile（1）：\n- jlu | 名称：吉林大学（JLU） | 协议：drcom-5.2.0-d\n"
		if output.String() != want {
			t.Errorf("output = %q, want %q", output.String(), want)
		}
	})

	for _, test := range []struct {
		name   string
		method string
		data   json.RawMessage
		run    func(readOnlyCommandDependencies) error
		want   string
	}{
		{"sessions", contract.MethodSessionList, json.RawMessage(`{"cleanupRequiredSessionIds":[],"sessions":[]}`), runAuthList, "没有 Session。\n下一步：运行 sidraviactl help auth start 开始认证。\n"},
		{"profiles", contract.MethodProfileList, json.RawMessage(`{"profiles":[]}`), runProfileList, "没有可用的机构 Profile。请检查完整 portable 包中的 institution-profiles，并重启 daemon。\n"},
	} {
		t.Run("empty "+test.name, func(t *testing.T) {
			connection := &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
				return contract.NewSuccessResponse("1", test.data), nil
			}}
			var output bytes.Buffer
			if err := test.run(hotListDependencies(t, connection, &output)); err != nil {
				t.Fatalf("run list = %v", err)
			}
			if output.String() != test.want {
				t.Errorf("output = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestListRejectsMalformedResponsesWithoutWriting(t *testing.T) {
	cases := []struct {
		name string
		run  func(readOnlyCommandDependencies) error
		data json.RawMessage
	}{
		{"session null", runAuthList, json.RawMessage(`{"sessions":null}`)},
		{"session unknown", runAuthList, json.RawMessage(`{"sessions":[],"secret":"marker"}`)},
		{"session incomplete", runAuthList, json.RawMessage(`{"sessions":[{"sessionId":"session-1"}]}`)},
		{"profile missing", runProfileList, json.RawMessage(`{}`)},
		{"profile incomplete", runProfileList, json.RawMessage(`{"profiles":[{"institutionProfileId":"jlu"}]}`)},
		{"profile trailing", runProfileList, json.RawMessage(`{"profiles":[]} {}`)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			connection := &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
				return contract.NewSuccessResponse("1", test.data), nil
			}}
			var output bytes.Buffer
			err := test.run(hotListDependencies(t, connection, &output))
			if err == nil {
				t.Fatal("malformed response returned nil")
			}
			if output.Len() != 0 {
				t.Errorf("partial output = %q", output.String())
			}
			if strings.Contains(err.Error(), "marker") {
				t.Errorf("error leaked response marker: %v", err)
			}
		})
	}
}

func TestListErrorsAreSafeAndPreserveWriterCause(t *testing.T) {
	t.Run("daemon error omits message", func(t *testing.T) {
		connection := &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
			return contract.NewErrorResponse(
				"1",
				contract.ErrorCodeProfileOperationFailed,
				"secret-diagnostic-marker",
			), nil
		}}
		var output bytes.Buffer
		err := runProfileList(hotListDependencies(t, connection, &output))
		if err == nil || strings.Contains(err.Error(), "secret-diagnostic-marker") {
			t.Fatalf("unsafe daemon error = %v", err)
		}
	})

	t.Run("writer cause", func(t *testing.T) {
		data := json.RawMessage(`{"cleanupRequiredSessionIds":[],"sessions":[]}`)
		connection := &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
			return contract.NewSuccessResponse("1", data), nil
		}}
		cause := errors.New("injected list writer failure")
		deps := hotListDependencies(t, connection, &bytes.Buffer{})
		deps.stdout = listErrorWriter{err: cause}
		err := runAuthList(deps)
		if !errors.Is(err, cause) {
			t.Fatalf("writer error = %v, want cause", err)
		}
	})
}

type listErrorWriter struct {
	err error
}

func (writer listErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

// TestListCommandsDoNotCallDaemonStop proves list commands never invoke the
// daemon.stop lifecycle method.
func TestListCommandsDoNotCallDaemonStop(t *testing.T) {
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, _ json.RawMessage) (contract.Response, error) {
		if method == contract.MethodDaemonStop {
			t.Errorf("list command must not call daemon.stop")
		}
		data, _ := contract.MarshalSessionListResult(contract.SessionListResult{Sessions: []contract.SessionResult{}, CleanupRequiredSessionIDs: []string{}})
		return contract.NewSuccessResponse("1", data), nil
	}}
	var output bytes.Buffer
	if err := runAuthList(hotListDependencies(t, connection, &output)); err != nil {
		t.Fatalf("runAuthList: %v", err)
	}
}

func TestReadOnlyListAcquisitionFailureWritesNothing(t *testing.T) {
	var output bytes.Buffer
	deps := hotListDependencies(t, &clitest.FakeDaemonClient{}, &output)
	deps.connection.Acquire = func(context.Context) (daemonconn.Client, error) { return nil, errors.New("stopped") }
	if err := runAuthList(deps); err == nil {
		t.Fatal("stopped list returned nil")
	}
	if output.Len() != 0 {
		t.Fatalf("output=%q", output.String())
	}
}
