//go:build windows

package main

import (
	"context"
	"flag"
	"io"
	"sidravia/internal/update/portable"
	"time"
)

func portableExecute(args []string) (bool, int) {
	if len(args) == 0 || args[0] != "portable-execute" {
		return false, 0
	}
	f := flag.NewFlagSet("portable-execute", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	target := f.String("target", "", "")
	id := f.String("operation", "", "")
	pid := f.Uint("updater", 0, "")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *pid > 1<<32-1 {
		return true, 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := portable.ExecuteWindows(ctx, *target, *id, uint32(*pid), ProductVersion, BuildID); err != nil {
		return true, 1
	}
	return true, 0
}
