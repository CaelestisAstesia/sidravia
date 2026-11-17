//go:build windows

package main

import (
	"context"
	"flag"
	"io"
	"sidravia/internal/clientbootstrap"
	"sidravia/internal/update/setup"
	"time"
)

func setupFinish(args []string) (bool, int) {
	if len(args) == 0 || args[0] != "setup-finish" {
		return false, 0
	}
	f := flag.NewFlagSet("setup-finish", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	op := f.String("operation", "", "")
	pid := f.Uint("coordinator", 0, "")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *pid > 1<<32-1 {
		return true, 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 31*time.Minute)
	defer cancel()
	if err := setup.Finish(ctx, *op, uint32(*pid), clientbootstrap.Identity{ProductVersion: ProductVersion, BuildID: BuildID}); err != nil {
		return true, 1
	}
	return true, 0
}
