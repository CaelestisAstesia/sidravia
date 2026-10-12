// sidraviamgr performs one installation-maintenance operation and exits.
package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"sidravia/internal/installationmanager"
)

var ProductVersion = "0.3.0-alpha.1"
var BuildID = "dev"

const usage = "sidraviamgr: status | path enable | path disable | autostart set gui | autostart set daemon | autostart disable | uninstall-prepare | data delete\nResults: UTF-8 JSON version 1. Exit codes: 0 success, 1 operation failed, 2 invalid arguments.\nWindows user integration cleanup and explicit production data removal are supported. Binary uninstall and repair are performed by the installer; not yet available as manager commands.\n"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Executable, installationmanager.Run)) }

func run(args []string, out io.Writer, executable func() (string, error), execute func(string, installationmanager.Operation) (installationmanager.Result, error)) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		if _, err := io.WriteString(out, usage); err != nil {
			return 1
		}
		return 0
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
	} else if len(args) == 1 && args[0] == "uninstall-check" {
		operation = installationmanager.CheckUninstall
	} else if len(args) == 1 && args[0] == "uninstall-prepare" {
		operation = installationmanager.PrepareUninstall
	} else if len(args) == 2 && args[0] == "data" && args[1] == "delete" {
		operation = installationmanager.DeleteData
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
	path, err := executable()
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
