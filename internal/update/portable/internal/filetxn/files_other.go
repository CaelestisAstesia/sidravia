//go:build !linux && !windows

package filetxn

import (
	"os"
)

// Platforms without a native publication adapter fail explicitly.
func SingleFile(*os.File, os.FileInfo) error { return ErrUnsupported }
func matchesMode(os.FileInfo, uint32) bool   { return false }
func applicationPlatform(Inventory) error    { return ErrUnsupported }
func syncDirectory(*os.Root, string) error   { return ErrUnsupported }
func availableSpace(*os.Root, int64) error   { return ErrUnsupported }

func ensureQuiescent(string) error { return ErrUnsupported }

func sameDevice(os.FileInfo, os.FileInfo) bool { return false }

func publish(*os.Root, string, string, bool) error { return ErrUnsupported }

func VerifyGUI(*os.Root, Inventory) error { return ErrUnsupported }

func holdStartupSeal() bool       { return false }
func applyGuard() (func(), error) { return nil, ErrUnsupported }

func PlainPlatformPath(string) bool { return true }
