package main

import (
	"fmt"
	"os"

	"sidravia/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sidravia: %v\n", err)
		os.Exit(1)
	}
}
