//go:build !linux

package portable

import (
	"context"
	"os"
)

// Windows shares the inventory/archive/application algorithms. Its publication
// adapter remains disabled until native file-lock and durability acceptance.
func singleFile(os.FileInfo) error         { return ErrUnsupported }
func matchesMode(os.FileInfo, uint32) bool { return false }
func applicationPlatform(Inventory) error  { return ErrUnsupported }
func syncDirectory(*os.Root, string) error { return ErrUnsupported }
func availableSpace(*os.Root, int64) error { return ErrUnsupported }

func ensureQuiescent(string) error { return ErrUnsupported }

func sameDevice(os.FileInfo, os.FileInfo) bool { return false }

func publish(*os.Root, string, string, bool) error { return ErrUnsupported }

func verifyGUI(*os.Root, Inventory) error                          { return ErrUnsupported }
func Preflight(context.Context, string, Inventory, *Payload) error { return ErrUnsupported }
