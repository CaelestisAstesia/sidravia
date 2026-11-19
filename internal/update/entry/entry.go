// Package entry classifies updater process admission without loading updater implementations.
package entry

// ReadOnly is shared with process entry points so status remains queryable
// during maintenance. Invalid arguments still reach the ordinary parser.
func ReadOnly(args []string) bool {
	return len(args) == 0 || args[0] == "status" || args[0] == "version" || args[0] == "help" || args[0] == "--help"
}
func ControlEntry(args []string) bool { return ReadOnly(args) || len(args) > 0 && args[0] == "recover" }
