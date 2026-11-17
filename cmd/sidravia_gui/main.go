//go:build linux

// The public Linux GUI entry is static Go. Admission happens before exec loads
// any product GTK/Flutter code; no extra resident parent or service is created.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"sidravia/internal/installationgate"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
)

var ProductVersion = "0.5.0-alpha.1"
var BuildID = "dev"

const gateFD = "SIDRAVIA_GUI_GATE_FD"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--help" {
		fmt.Println("Sidravia GUI launcher")
		return
	}
	exe, err := os.Executable()
	if err == nil {
		err = launch(filepath.Dir(exe), os.Args[1:], os.Environ())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "安装或更新尚未完成，或 GUI 运行文件不可用")
		os.Exit(1)
	}
}
func launch(directory string, args, env []string) error {
	physical, err := filepath.EvalSymlinks(directory)
	if err != nil || physical != directory {
		return errors.Join(operations.ErrInvalid, err)
	}
	runtime := filepath.Join(directory, "gui-runtime", "sidravia_gui")
	actual, err := filepath.EvalSymlinks(runtime)
	if err != nil || actual != runtime {
		return errors.Join(operations.ErrInvalid, err)
	}
	info, err := os.Lstat(runtime)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return operations.ErrInvalid
	}
	cleanEnv := make([]string, 0, len(env)+1)
	for _, value := range env {
		if !strings.HasPrefix(value, gateFD+"=") {
			cleanEnv = append(cleanEnv, value)
		}
	}
	if directory == "/usr/lib64/sidravia" {
		release, err := installationgate.AcquireStart()
		if err != nil {
			return err
		}
		release()
	} else {
		root, err := operations.DefaultRoot()
		if err != nil {
			return err
		}
		store, err := operations.Open(root, directory, releases.PublisherTrust())
		if err != nil {
			return err
		}
		lease, err := store.AcquireInheritedStart(context.Background())
		if err != nil {
			lease, err = store.AcquireInheritedRecoveryStart(context.Background(), os.Getenv(operations.EnvRecoveryOperation), os.Getenv(operations.EnvRecoveryToken), ProductVersion, BuildID, os.Getenv("SIDRAVIA_NAMESPACE"), os.Getenv(operations.EnvRecoveryConfiguration))
		} else {
			// Later core restarts must not reuse a completed restoration capability.
			filtered := cleanEnv[:0]
			for _, entry := range cleanEnv {
				key, _, _ := strings.Cut(entry, "=")
				if key != operations.EnvRecoveryOperation && key != operations.EnvRecoveryToken && key != operations.EnvRecoveryConfiguration {
					filtered = append(filtered, entry)
				}
			}
			cleanEnv = filtered
		}
		if err != nil {
			return err
		}
		defer lease.Close()
		if _, err = unix.FcntlInt(lease.Fd(), unix.F_SETFD, 0); err != nil {
			return err
		}
		cleanEnv = append(cleanEnv, gateFD+"="+strconv.FormatUint(uint64(lease.Fd()), 10))
	}
	return syscall.Exec(runtime, append([]string{runtime}, args...), cleanEnv)
}
