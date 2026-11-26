package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
)

// Probe is read-only and never starts, stops, removes or rewrites runtime state.
func Probe(identity Identity) (ProbeResult, error) { return probeDefault(identity) }

func probe(identity Identity, deps dependencies) (ProbeResult, error) {
	ns, err := deps.namespace()
	if err != nil {
		return ProbeResult{}, wrapSafeOperation("运行命名空间", err)
	}
	return probeWithNamespace(identity, ns, deps)
}

func probeWithNamespace(identity Identity, ns productlayout.Namespace, deps dependencies) (ProbeResult, error) {
	return probeInContext(context.Background(), identity, ns, deps)
}

func probeInContext(ctx context.Context, identity Identity, ns productlayout.Namespace, deps dependencies) (ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	if err := identity.validate(); err != nil {
		return ProbeResult{}, err
	}
	path, err := deps.runtimeInfoPath(ns)
	if err != nil {
		return ProbeResult{}, wrapSafeOperation("运行信息路径", err)
	}
	info, err := deps.readRuntimeInfo(path)
	if canceled := ctx.Err(); canceled != nil {
		return ProbeResult{}, canceled
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProbeResult{State: ProbeStopped}, nil
		}
		return ProbeResult{State: ProbeMalformed}, nil
	}
	if !matchesIdentity(identity, info) {
		return ProbeResult{Info: info, State: ProbeIncompatible}, nil
	}
	status, ok := statusFor(ctx, identity, deps, info)
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	if !ok {
		return ProbeResult{Info: info, State: ProbeUnreachable, Status: status}, nil
	}
	return ProbeResult{Info: info, State: ProbeReachable, Status: status}, nil
}

// Connect opens a direct connection to exactly the supplied runtime generation.
func Connect(ctx context.Context, identity Identity, info contract.RuntimeInfo) (Client, error) {
	return connectWithDependencies(ctx, identity, info, defaultDependencies())
}

// ConnectExistingHeadless opens a client only when the currently described
// runtime generation is reachable, identity-matching and headless. It never
// launches a process or changes runtime state.
func ConnectExistingHeadless(ctx context.Context, identity Identity) (Client, error) {
	ns, err := defaultDependencies().namespace()
	if err != nil {
		return nil, err
	}
	if layout, registered, err := serviceLayout(ns); registered || err != nil {
		if err != nil {
			return nil, err
		}
		return connectService(ctx, identity, layout)
	}
	return connectExistingHeadless(ctx, identity, defaultDependencies())
}

func connectExistingHeadless(ctx context.Context, identity Identity, deps dependencies) (Client, error) {
	ns, err := deps.namespace()
	if err != nil {
		return nil, wrapSafeOperation("运行命名空间", err)
	}
	result, err := probeInContext(ctx, identity, ns, deps)
	if err != nil {
		return nil, err
	}
	switch result.State {
	case ProbeStopped:
		return nil, ErrDaemonStopped
	case ProbeIncompatible:
		return nil, ErrIncompatibleGeneration
	case ProbeReachable:
		if result.Status == nil || result.Status.Mode != string(launchcontract.ModeHeadless) || result.Status.DesktopOwnerPID != nil {
			return nil, ErrModeConflict
		}
	case ProbeUnreachable:
		if result.Status != nil && (result.Status.ProductVersion != identity.ProductVersion || result.Status.BuildID != identity.BuildID) {
			return nil, ErrIncompatibleGeneration
		}
		return nil, ErrConnectionUnavailable
	default:
		return nil, ErrConnectionUnavailable
	}
	connectCtx, cancel := context.WithTimeout(ctx, deps.callTimeout)
	defer cancel()
	connection, err := deps.connect(connectCtx, result.Info, identity)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, ErrConnectionUnavailable
	}
	return connection, nil
}

func connectWithDependencies(ctx context.Context, identity Identity, info contract.RuntimeInfo, deps dependencies) (Client, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}
	if !matchesIdentity(identity, info) {
		return nil, ErrIncompatibleGeneration
	}
	return deps.connect(ctx, info, identity)
}

func matchesIdentity(identity Identity, info contract.RuntimeInfo) bool {
	return identity.ProductVersion == info.ProductVersion && identity.BuildID == info.BuildID
}

func statusFor(parent context.Context, identity Identity, deps dependencies, info contract.RuntimeInfo) (*contract.StatusResult, bool) {
	ctx, cancel := context.WithTimeout(parent, deps.callTimeout)
	defer cancel()
	conn, err := deps.connect(ctx, info, identity)
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	response, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !response.OK {
		return nil, false
	}
	var status contract.StatusResult
	if err := json.Unmarshal(response.Result, &status); err != nil {
		return nil, false
	}
	return &status, validStatusFor(identity, info, &status)
}

func validStatusFor(identity Identity, info contract.RuntimeInfo, status *contract.StatusResult) bool {
	if status == nil || status.ProductVersion != identity.ProductVersion || status.BuildID != identity.BuildID ||
		status.PID <= 0 || status.PID != info.PID {
		return false
	}
	switch status.Mode {
	case string(launchcontract.ModeHeadless), string(launchcontract.ModeSystemd):
		return status.DesktopOwnerPID == nil
	case string(launchcontract.ModeDesktop):
		return status.DesktopOwnerPID != nil && *status.DesktopOwnerPID > 0
	default:
		return false
	}
}

// ProbeApplication observes the ordinary application-managed layout, including
// during a switch while a distinct systemd registration still exists.
func ProbeApplication(ctx context.Context, identity Identity, ns productlayout.Namespace) (ProbeResult, error) {
	return probeInContext(ctx, identity, ns, defaultDependencies())
}

// ReadRuntimeInfo decodes the generation published at a known registration path.
func ReadRuntimeInfo(path string) (contract.RuntimeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	return contract.DecodeRuntimeInfo(data)
}
