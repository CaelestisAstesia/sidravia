package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunGUIBootstrapInvalidArgumentsUsesMachineContract(t *testing.T) {
	var stderr bytes.Buffer
	if got := run([]string{"gui", "bootstrap"}, &stderr); got != 2 {
		t.Fatalf("exit=%d, want 2", got)
	}
	want := "{\"schemaVersion\":1,\"error\":{\"code\":\"invalid_arguments\",\"message\":\"GUI bootstrap 参数无效\"}}\n"
	if stderr.String() != want {
		t.Fatalf("stderr=%q want=%q", stderr.String(), want)
	}
}

func TestRunPreservesOrdinaryCLIErrorAndExitCode(t *testing.T) {
	var stderr bytes.Buffer
	if got := run([]string{"unknown-command"}, &stderr); got != 1 {
		t.Fatalf("exit=%d, want 1", got)
	}
	if !strings.Contains(stderr.String(), "错误：用法错误，请运行 sidravia help 查看帮助") || strings.Contains(stderr.String(), "schemaVersion") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunStderrFailureDoesNotReplaceBusinessExitCode(t *testing.T) {
	if got := run([]string{"gui", "bootstrap"}, failingWriter{}); got != 2 {
		t.Fatalf("exit=%d, want 2", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
