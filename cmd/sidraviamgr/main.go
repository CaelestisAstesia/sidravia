// sidraviamgr performs one PATH or login-startup operation and exits.
package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/installationmanager"
)

var ProductVersion = "0.3.0-alpha.1"
var BuildID = "dev"

const usage = "sidraviamgr: status [--directory absolute-path] | path enable | path disable | autostart set gui | autostart set daemon | autostart disable\nResults: UTF-8 JSON version 1. Exit codes: 0 success, 1 operation failed, 2 invalid arguments.\nOnly current-user PATH and login startup are managed here. Installation, upgrade, uninstall and data cleanup belong to the installer.\n"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Executable, installationmanager.Run)) }

func run(args []string, out io.Writer, executable func() (string, error), execute func(string, installationmanager.Operation) (installationmanager.Result, error)) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		if _, err := io.WriteString(out, usage); err != nil {
			return 1
		}
		return 0
	}
	queryDirectory := ""
	if len(args) == 3 && args[0] == "status" && args[1] == "--directory" && filepath.IsAbs(args[2]) && !strings.ContainsAny(args[2], "\x00\r\n\"") {
		queryDirectory = filepath.Clean(args[2])
		args = args[:1]
	}
	var operation installationmanager.Operation
	if len(args) == 1 && args[0] == "status" {
		operation = installationmanager.Status
	} else if len(args) == 2 && args[0] == "path" && args[1] == "enable" {
		operation = installationmanager.EnablePath
	} else if len(args) == 2 && args[0] == "path" && args[1] == "disable" {
		operation = installationmanager.DisablePath
	} else if len(args) == 2 && args[0] == "autostart" && args[1] == "disable" {
		operation = installationmanager.DisableAutostart
	} else if len(args) == 3 && args[0] == "autostart" && args[1] == "set" && args[2] == "gui" {
		operation = installationmanager.StartGUI
	} else if len(args) == 3 && args[0] == "autostart" && args[1] == "set" && args[2] == "daemon" {
		operation = installationmanager.StartDaemon

	}
	write := func(r installationmanager.Result, code int) int {
		if err := json.NewEncoder(out).Encode(r); err != nil {
			return 1
		}
		return code
	}
	if operation == "" {
		return write(installationmanager.Result{Version: 1, Code: "invalid_arguments", Capabilities: []string{}}, 2)
	}
	path := filepath.Join(queryDirectory, "sidraviamgr.exe")
	var err error
	if queryDirectory == "" {
		path, err = executable()
	}
	if err != nil {
		return write(installationmanager.Result{Version: 1, Code: "executable_path_failed", Capabilities: []string{}}, 1)
	}
	if !filepath.IsAbs(path) {
		return write(installationmanager.Result{Version: 1, Code: "executable_path_failed", Capabilities: []string{}}, 1)
	}
	r, err := execute(filepath.Dir(path), operation)
	if err != nil {
		r.OK = false
		r.Code = installationmanager.Code(err)
		return write(r, 1)
	}
	if !r.OK {
		return write(r, 1)
	}
	return write(r, 0)
}
