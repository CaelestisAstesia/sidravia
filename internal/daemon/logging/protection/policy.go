// Package protection applies the daemon log permission policy for each platform.
package protection

// Policy protects a log directory or file using the current process identity.
type Policy interface {
	Directory(path string) error
	File(path string) error
}
