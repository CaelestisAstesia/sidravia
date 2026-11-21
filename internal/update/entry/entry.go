// Package entry classifies updater process admission without loading updater implementations.
package entry

import "runtime"

// CheckInCTL keeps the GUI's check child as the sole process holding admission.
// Apply and recovery retain their separate execution and handoff paths.
func CheckInCTL(args []string) bool {
	return len(args) > 0 && args[0] == "check" && (runtime.GOOS == "linux" || runtime.GOOS == "windows")
}

// ReadOnly is shared with process entry points so status remains queryable
// during maintenance. Invalid arguments still reach the ordinary parser.
func ReadOnly(args []string) bool {
	return len(args) == 0 || args[0] == "status" || args[0] == "version" || args[0] == "help" || args[0] == "--help"
}
func ControlEntry(args []string) bool { return ReadOnly(args) || len(args) > 0 && args[0] == "recover" }
