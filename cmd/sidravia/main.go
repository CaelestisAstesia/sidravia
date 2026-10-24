package main

import (
	"fmt"
	"io"
	"os"
	"sidravia/internal/installationgate"

	"sidravia/internal/cli"
)

var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "update-internal" {
		release, err := installationgate.AcquireStart()
		if err != nil {
			fmt.Fprintln(os.Stderr, "安装或更新正在进行，请完成或取消后再运行命令。")
			os.Exit(1)
		}
		release()
	}
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	err := cli.RunWithIdentity(args, ProductVersion, BuildID)
	if err != nil {
		_ = cli.WriteError(stderr, err)
	}
	return cli.ExitCode(err)
}
