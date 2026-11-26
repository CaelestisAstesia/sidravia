package main

import (
	"os"

	"sidravia/internal/daemon"
)

// Set via ldflags.
var (
	ProductVersion = "0.5.0-alpha.1"
	BuildID        = "dev"
)

func main() { os.Exit(daemon.Run(ProductVersion, BuildID)) }
