//go:build windows

package installationmanager

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

//go:embed task.ps1
var taskScript string

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("manager output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func invokePowerShell(script string, input []byte) ([]byte, error) {
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return nil, err
	}
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(encoded[2*i:], u)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdin = bytes.NewReader(input)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func taskOperation(directory string, operation Operation) (Result, error) {
	r := Result{Version: 1, Directory: directory, Capabilities: []string{"user_path", "user_autostart"}}
	fail := func(code string, cause error) (Result, error) {
		r.Code = code
		return r, &Failure{Code: code, Cause: cause}
	}
	if operation != Status && operation != StartGUI && operation != StartDaemon && operation != DisableAutostart {
		return fail("invalid_arguments", errors.New("unknown task operation"))
	}
	input, err := json.Marshal(struct {
		Directory string    `json:"directory"`
		Operation Operation `json:"operation"`
	}{directory, operation})
	if err != nil {
		return fail("scheduled_task_operation_failed", err)
	}
	output, err := invokePowerShell(taskScript, input)
	if err != nil {
		return fail("scheduled_task_operation_failed", err)
	}
	var response struct {
		OK      bool   `json:"ok"`
		Code    string `json:"code"`
		Mode    string `json:"mode"`
		Changed bool   `json:"changed"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fail("scheduled_task_response_invalid", err)
	}
	r.Changed = response.Changed
	r.Autostart = response.Mode
	if !response.OK {
		switch response.Code {
		case "scheduled_task_query_failed", "scheduled_task_conflict", "scheduled_task_verify_failed", "unsupported_release_script_host", "autostart_target_missing", "invalid_arguments", "scheduled_task_operation_failed":
			return fail(response.Code, errors.New("platform task operation failed"))
		default:
			return fail("scheduled_task_response_invalid", errors.New("unknown platform error"))
		}
	}
	if response.Code != "ok" || (response.Mode != "disabled" && response.Mode != "gui" && response.Mode != "daemon" && response.Mode != "conflict") {
		return fail("scheduled_task_response_invalid", errors.New("unknown platform status"))
	}
	r.OK = true
	r.Code = "ok"
	return r, nil
}
