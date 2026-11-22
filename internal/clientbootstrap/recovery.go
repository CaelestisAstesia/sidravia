package clientbootstrap

import (
	"sidravia/internal/ipc/contract"
)

// StartHeadlessForRecovery returns only the exact new generation created by
// this launch. Singleton contention must never adopt a user's concurrent start.
func StartHeadlessForRecovery(identity Identity, logLevel string) (contract.RuntimeInfo, error) {
	if !desktopBootstrapSupported() {
		return contract.RuntimeInfo{}, ErrDesktopUnsupported
	}
	return startHeadlessForRecovery(identity, logLevel, defaultDependencies())
}
func StartHeadlessForUpdate(identity Identity, level, operation, token, configuration string) (contract.RuntimeInfo, error) {
	return startHeadlessRecoveryPolicy(identity, level, defaultDependencies(), operation, token, configuration)
}
func startHeadlessForRecovery(identity Identity, level string, deps dependencies) (contract.RuntimeInfo, error) {
	return startHeadlessRecoveryPolicy(identity, level, deps, "", "", "")
}
func startHeadlessRecoveryPolicy(identity Identity, level string, deps dependencies, operation, token, configuration string) (contract.RuntimeInfo, error) {
	ns, err := deps.namespace()
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	before, err := probeWithNamespace(identity, ns, deps)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	if before.State != ProbeStopped {
		return contract.RuntimeInfo{}, ErrModeConflict
	}
	options := headlessOptions(ns)
	options.SkipAutoLogin = true
	options.RecoveryOperation = operation
	options.RecoveryToken = token
	options.RecoveryConfiguration = configuration
	if err := options.Validate(); err != nil {
		return contract.RuntimeInfo{}, err
	}
	launched, err := deps.launch(options, level)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	if launched.release != nil {
		defer launched.release()
	}
	if launched.pid <= 0 || launched.alive == nil {
		return contract.RuntimeInfo{}, ErrModeConflict
	}
	var ready contract.RuntimeInfo
	err = waitForReadiness(deps.totalWait, deps.pollInterval, launched.exited, func() (bool, error) {
		if !launched.alive() {
			return false, ErrModeConflict
		}
		result, e := probeWithNamespace(identity, ns, deps)
		if e != nil {
			return false, e
		}
		if result.State == ProbeReachable {
			if result.Info.PID != launched.pid || !matchesOptions(result.Status, options) {
				return false, ErrModeConflict
			}
			if !launched.alive() {
				return false, ErrModeConflict
			}
			ready = result.Info
			return true, nil
		}
		if result.State == ProbeIncompatible {
			return false, ErrIncompatibleGeneration
		}
		return false, nil
	})
	return ready, err
}
