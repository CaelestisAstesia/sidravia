package main

import (
	"os"

	"sidravia/internal/cli"
)

func main() {
	err := cli.Run(os.Args[1:])
	if err != nil {
		_ = cli.WriteError(os.Stderr, err)
		os.Exit(1)
	}
}
