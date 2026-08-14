package main

import (
	"io"
	"os"

	"sidravia/internal/cli"
)

var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	err := cli.RunWithIdentity(args, ProductVersion, BuildID)
	if err != nil {
		_ = cli.WriteError(stderr, err)
	}
	return cli.ExitCode(err)
}
