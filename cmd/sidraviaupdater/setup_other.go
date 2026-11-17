//go:build !windows

package main

func setupFinish([]string) (bool, int) { return false, 0 }
