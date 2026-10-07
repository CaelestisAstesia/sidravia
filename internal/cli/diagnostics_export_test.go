package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

func TestDiagnosticsExportCommandValidatesArgumentsBeforeDispatch(t *testing.T) {
	calls := 0
	var gotPath string
	var output bytes.Buffer
	deps := commandDependencies{
		diagnosticsExport: func(path string) error {
			calls++
			gotPath = path
			return nil
		},
		output: &output,
	}
	for _, args := range [][]string{
		{"diagnostics"},
		{"help", "diagnostics"},
		{"diagnostics", "--help"},
		{"help", "diagnostics", "export"},
		{"diagnostics", "export", "--help"},
	} {
		output.Reset()
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("help %q: %v", args, err)
		}
		if !strings.Contains(output.String(), "用法：") || calls != 0 {
			t.Fatalf("help %q dispatched or omitted usage: calls=%d output=%q", args, calls, output.String())
		}
	}
	for _, args := range [][]string{
		{"diagnostics", "export"},
		{"diagnostics", "export", "--output"},
		{"diagnostics", "export", "--output="},
		{"diagnostics", "export", "--output", ""},
		{"diagnostics", "export", "--output", "path", "extra"},
		{"diagnostics", "export", "--unknown", "value"},
		{"diagnostics", "export", "--output", "bad\x00path"},
	} {
		if err := runCommand(args, deps); err == nil {
			t.Errorf("invalid arguments %q accepted", args)
		}
		if calls != 0 {
			t.Fatalf("invalid arguments %q dispatched", args)
		}
	}
	if err := runCommand([]string{"diagnostics", "export", "--output", "new.json"}, deps); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotPath != "new.json" {
		t.Fatalf("dispatch count/path = %d/%q", calls, gotPath)
	}
}

func TestDiagnosticsExportDefaultDependencyIsWired(t *testing.T) {
	identity, err := clientbootstrap.NewIdentity("0.1.0-dev", "cli-build")
	if err != nil {
		t.Fatal(err)
	}
	if defaultCommandDependencies(identity).diagnosticsExport == nil {
		t.Fatal("diagnostics export dependency is nil")
	}
}

func TestRunDiagnosticsExportWritesCanonicalArtifactExclusively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics-\u0085-export.json")
	raw, expected := diagnosticsExportWire(t)
	connection := diagnosticsExportClient(t, append(append([]byte("  "), raw...), ' ', '\n'), nil)
	var output bytes.Buffer
	if err := runDiagnosticsExport(diagnosticsExportDependencies(connection, &output), path); err != nil {
		t.Fatalf("runDiagnosticsExport: %v", err)
	}
	want := append(append([]byte(nil), expected...), '\n')
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("artifact = %q, want canonical %q", got, want)
	}
	if connection.callCount != 1 || connection.closeCount != 1 {
		t.Fatalf("calls/close = %d/%d", connection.callCount, connection.closeCount)
	}
	if !strings.Contains(output.String(), "诊断信息已写入：") || !strings.Contains(output.String(), "diagnostics-�-export.json") || strings.ContainsRune(output.String(), '\u0085') {
		t.Fatalf("success presentation did not sanitize path: %q", output.String())
	}
	decoded, err := contract.DecodeDiagnosticsExportResult(got[:len(got)-1])
	if err != nil || decoded.SchemaVersion != 1 {
		t.Fatalf("written artifact did not strictly decode: result=%+v err=%v", decoded, err)
	}
}

func TestRunDiagnosticsExportRPCAndInvalidArtifactCreateNoFile(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "diagnostics.json")
		cause := errors.New("private transport marker")
		connection := diagnosticsExportClient(t, nil, cause)
		var output bytes.Buffer
		err := runDiagnosticsExport(diagnosticsExportDependencies(connection, &output), path)
		if !errors.Is(err, cause) || strings.Contains(err.Error(), "private transport marker") {
			t.Fatalf("unsafe or lost transport error: %v", err)
		}
		assertExportPathMissing(t, path)
	})
	t.Run("invalid artifact", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "diagnostics.json")
		bad := []byte(`{"schemaVersion":1,"generatedAt":"2026-10-07T00:00:00Z","productVersion":"1","buildId":"b","operatingSystem":"windows","architecture":"amd64","network":{"available":false},"catalog":{"storageProtection":"protected","totalConfigurations":0,"autoLoginConfigurations":0,"autoReconnectConfigurations":0,"automaticBindingConfigurations":0,"explicitBindingConfigurations":0},"sessions":{"totalCount":0,"truncated":false,"items":[]},"secret":"private artifact marker"}`)
		connection := diagnosticsExportClient(t, bad, nil)
		var output bytes.Buffer
		err := runDiagnosticsExport(diagnosticsExportDependencies(connection, &output), path)
		if err == nil || strings.Contains(err.Error(), "private artifact marker") {
			t.Fatalf("invalid artifact error = %v", err)
		}
		assertExportPathMissing(t, path)
	})
}

func TestRunDiagnosticsExportNeverReplacesExistingPaths(t *testing.T) {
	raw, _ := diagnosticsExportWire(t)
	valid := func(t *testing.T, path string) error {
		t.Helper()
		connection := diagnosticsExportClient(t, raw, nil)
		return runDiagnosticsExport(diagnosticsExportDependencies(connection, io.Discard), path)
	}

	t.Run("existing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "existing.json")
		before := []byte("owned existing bytes")
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := valid(t, path); err == nil {
			t.Fatal("existing file path accepted")
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, before) {
			t.Fatalf("existing bytes = %q, err=%v", got, err)
		}
	})
	t.Run("existing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "existing-directory")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := valid(t, path); err == nil {
			t.Fatal("directory path accepted")
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("directory changed: info=%v err=%v", info, err)
		}
	})
	t.Run("missing parent", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "missing-parent")
		if err := valid(t, filepath.Join(parent, "diagnostics.json")); err == nil {
			t.Fatal("missing parent unexpectedly created an artifact")
		}
		if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing parent state = %v", err)
		}
	})
	t.Run("dangling symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "link.json")
		target := filepath.Join(dir, "not-created.json")
		if err := os.Symlink(target, path); err != nil {
			if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skipf("Windows denied symlink creation without required privilege: %v", err)
			}
			t.Fatalf("create dangling symlink: %v", err)
		}
		if err := valid(t, path); err == nil {
			t.Fatal("dangling symlink path accepted")
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink changed: info=%v err=%v", info, err)
		}
		assertExportPathMissing(t, target)
	})
}

func TestWriteOwnedArtifactChecksWriteAndClosesExactlyOnce(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		file := &testArtifactWriter{writeN: -1}
		if err := writeOwnedArtifact(file, []byte("artifact")); err != nil {
			t.Fatal(err)
		}
		if file.writeCount != 1 || file.closeCount != 1 || string(file.data) != "artifact" {
			t.Fatalf("writer state = %+v", file)
		}
	})
	t.Run("short write", func(t *testing.T) {
		file := &testArtifactWriter{writeN: 2}
		err := writeOwnedArtifact(file, []byte("artifact"))
		if !errors.Is(err, io.ErrShortWrite) || file.closeCount != 1 {
			t.Fatalf("short write error/close = %v/%d", err, file.closeCount)
		}
	})
	t.Run("write and close errors", func(t *testing.T) {
		writeErr := errors.New("private write cause")
		closeErr := errors.New("private close cause")
		file := &testArtifactWriter{writeN: 0, writeErr: writeErr, closeErr: closeErr}
		err := writeOwnedArtifact(file, []byte("artifact"))
		if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) || file.closeCount != 1 {
			t.Fatalf("joined error/close = %v/%d", err, file.closeCount)
		}
	})
}

func TestRunDiagnosticsExportPresentationFailureLeavesCompleteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.json")
	raw, expected := diagnosticsExportWire(t)
	connection := diagnosticsExportClient(t, raw, nil)
	cause := errors.New("private presentation marker")
	deps := diagnosticsExportDependencies(connection, io.Discard)
	deps.stdout = exportFailWriter{err: cause}
	err := runDiagnosticsExport(deps, path)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "文件已写入，但提示输出失败") || strings.Contains(err.Error(), "private presentation marker") {
		t.Fatalf("presentation error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	want := append(append([]byte(nil), expected...), '\n')
	if readErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("file after presentation failure = %q, read err=%v", got, readErr)
	}
}

func diagnosticsExportDependencies(connection daemonClient, stdout io.Writer) listDependencies {
	return listDependencies{
		connection: daemonConnectionDependencies{
			acquire:     func(context.Context) (daemonClient, error) { return connection, nil },
			callTimeout: 1,
		},
		stdout: stdout,
	}
}

func diagnosticsExportClient(t *testing.T, result []byte, callErr error) *fakeDaemonClient {
	t.Helper()
	return &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodDiagnosticsExport {
			t.Errorf("method = %q, want %q", method, contract.MethodDiagnosticsExport)
		}
		if string(payload) != `{}` {
			t.Errorf("payload = %s, want {}", payload)
		}
		if callErr != nil {
			return contract.Response{}, callErr
		}
		return contract.NewSuccessResponse("export-1", json.RawMessage(result)), nil
	}}
}

func diagnosticsExportWire(t *testing.T) ([]byte, []byte) {
	t.Helper()
	result := contract.DiagnosticsExportResult{
		SchemaVersion:   1,
		GeneratedAt:     "2026-10-07T01:02:03Z",
		ProductVersion:  "0.1.0-dev",
		BuildID:         "build-1",
		OperatingSystem: "windows",
		Architecture:    "amd64",
		Network:         contract.DiagnosticExportNetwork{Available: false},
		Catalog: contract.DiagnosticExportCatalog{
			StorageProtection: "protected",
		},
		Sessions: contract.DiagnosticExportSessions{Items: []contract.DiagnosticExportSession{}},
	}
	canonical, err := contract.MarshalDiagnosticsExportResult(result)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), canonical...), append([]byte(nil), canonical...)
}

func assertExportPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path exists or cannot be checked: %q err=%v", path, err)
	}
}

type testArtifactWriter struct {
	data       []byte
	writeN     int
	writeErr   error
	closeErr   error
	writeCount int
	closeCount int
}

func (writer *testArtifactWriter) Write(data []byte) (int, error) {
	writer.writeCount++
	n := len(data)
	if writer.writeN >= 0 {
		n = writer.writeN
		if n > len(data) {
			n = len(data)
		}
	}
	writer.data = append(writer.data, data[:n]...)
	return n, writer.writeErr
}

func (writer *testArtifactWriter) Close() error {
	writer.closeCount++
	return writer.closeErr
}

type exportFailWriter struct {
	err error
}

func (writer exportFailWriter) Write([]byte) (int, error) { return 0, writer.err }
