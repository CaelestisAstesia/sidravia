package main

import (
	"fmt"
	"io"
	"os"
	"sidravia/internal/installationgate"
	"sidravia/internal/update/command"

	"sidravia/internal/cli"
)

var (
	ProductVersion = "0.5.0-alpha.1"
	BuildID        = "dev"
)

func main() {
	releaseRun := func() {}
	if (len(os.Args) < 2 || os.Args[1] != "update-internal") && !(len(os.Args) > 2 && os.Args[1] == "update" && command.ControlEntry(os.Args[2:])) {
		release, err := installationgate.AcquireRunIdentity(ProductVersion, BuildID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "安装或更新正在进行，请完成或取消后再运行命令。")
			os.Exit(1)
		}
		// An updater child takes over coordination; retaining this caller's
		// lease would prevent that child from ever obtaining execution admission.
		if len(os.Args) > 1 && os.Args[1] == "update" {
			release()
		} else {
			releaseRun = release
		}
	}
	code := run(os.Args[1:], os.Stderr)
	releaseRun()
	os.Exit(code)
}

func run(args []string, stderr io.Writer) int {
	err := cli.RunWithIdentity(args, ProductVersion, BuildID)
	if err != nil {
		_ = cli.WriteError(stderr, err)
	}
	return cli.ExitCode(err)
}
