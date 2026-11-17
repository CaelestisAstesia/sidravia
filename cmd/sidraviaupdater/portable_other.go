//go:build !windows

package main

func portableExecute([]string) (bool, int) { return false, 0 }
