//go:build !windows

package clientbootstrap

type unprotectedDaemonLogProtection struct{}

func (unprotectedDaemonLogProtection) protectDirectory(string) error { return nil }

func (unprotectedDaemonLogProtection) protectFile(string) error { return nil }

func newDaemonLogProtection() (daemonLogProtection, error) {
	return unprotectedDaemonLogProtection{}, nil
}
