//go:build linux || windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestUpdateCheckProcessCancellation(t *testing.T) {
	root := t.TempDir()
	product := filepath.Join(root, "product")
	home := filepath.Join(root, "home")
	for _, dir := range []string{product, home} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := "sidraviactl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(product, name)
	if output, err := exec.Command("go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CTL: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(product, "sidravia.portable"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("APPDATA", home)
	t.Setenv("SIDRAVIA_NAMESPACE", "")
	t.Setenv("NO_PROXY", "")
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256([]byte(product + "\x00core-portable\x00preview"))
	legacy := filepath.Join(cacheRoot, "Sidravia", "updates", hex.EncodeToString(identity[:16]))
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	legacyState := filepath.Join(legacy, "state.json")
	if err := os.WriteFile(legacyState, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	proxy, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", "http://"+proxy.Addr().String())

	for _, args := range [][]string{
		{"download", "--candidate", "1:2"},
		{"apply", "--candidate", "1:2", "--confirm"},
		{"status"},
	} {
		output, err := exec.Command(executable, append([]string{"update"}, args...)...).CombinedOutput()
		if err == nil || !bytes.Contains(output, []byte(`"code":"portable_download_only"`)) {
			t.Fatalf("portable update %v: %s (%v)", args, output, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := func() (*exec.Cmd, *bytes.Buffer) {
		cmd := exec.CommandContext(ctx, executable, "update", "check")
		output := new(bytes.Buffer)
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, output
	}
	accept := func() (*net.TCPConn, *bufio.Reader, error) {
		if err := proxy.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return nil, nil, err
		}
		conn, err := proxy.AcceptTCP()
		if err != nil {
			return nil, nil, err
		}
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			conn.Close()
			return nil, nil, err
		}
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				conn.Close()
				return nil, nil, err
			}
			if line == "\r\n" {
				return conn, reader, nil
			}
		}
	}

	// The check reaches the proxy with only a CTL binary installed, then the GUI's
	// process termination model must close the connection and release its locks.
	first, output := start()
	connection, reader, err := accept()
	if err != nil {
		_ = first.Process.Kill()
		_ = first.Wait()
		t.Fatalf("first check never reached the proxy: %v (%s)", err, output)
	}
	defer connection.Close()
	killErr := first.Process.Kill()
	_ = first.Wait()
	if killErr != nil {
		t.Fatal(killErr)
	}
	if _, err := reader.ReadByte(); err != io.EOF && !errors.Is(err, syscall.ECONNRESET) &&
		!(runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(10054))) { // WSAECONNRESET
		t.Fatalf("expected EOF or reset after CTL termination: %v", err)
	}

	second, output := start()
	connection, _, err = accept()
	if err != nil {
		_ = second.Process.Kill()
		_ = second.Wait()
		t.Fatalf("second check never reached the proxy: %v (%s)", err, output)
	}
	_, _ = io.WriteString(connection, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
	connection.Close()
	if err := second.Wait(); err == nil {
		t.Fatal("proxy error produced a successful update check")
	}
	var response struct {
		SchemaVersion int    `json:"schemaVersion"`
		Code          string `json:"code"`
	}
	// CLI human feedback is on stderr; it follows the single JSON line.
	line, _, _ := bytes.Cut(output.Bytes(), []byte{'\n'})
	if err := json.Unmarshal(line, &response); err != nil || response.SchemaVersion != 1 || response.Code != "source_unavailable" {
		t.Fatalf("check failure response = %s (%v)", output, err)
	}
	if data, err := os.ReadFile(legacyState); err != nil || string(data) != "{" {
		t.Fatalf("version hint modified retired update state: %q %v", data, err)
	}

}
