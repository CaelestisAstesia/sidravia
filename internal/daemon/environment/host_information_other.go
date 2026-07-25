//go:build !windows

package environment

// ReadSystemHostInformation is unsupported on non-Windows platforms. It
// returns ErrUnsupported instead of an apparently valid empty host.
func ReadSystemHostInformation() (SystemHostInformation, error) {
	return SystemHostInformation{}, ErrUnsupported
}
