package main

import (
	"os"

	"sidravia/internal/cli"
)

var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	err := cli.RunWithIdentity(os.Args[1:], ProductVersion, BuildID)
	if err != nil {
		_ = cli.WriteError(os.Stderr, err)
		os.Exit(1)
	}
}
