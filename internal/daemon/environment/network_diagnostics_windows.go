//go:build windows

package environment

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	diagnosticIPHelper   = windows.NewLazySystemDLL("iphlpapi.dll")
	diagnosticBestRoute  = diagnosticIPHelper.NewProc("GetBestRoute2")
	diagnosticICMPCreate = diagnosticIPHelper.NewProc("IcmpCreateFile")
	diagnosticICMPSend   = diagnosticIPHelper.NewProc("IcmpSendEcho2Ex")
	diagnosticICMPClose  = diagnosticIPHelper.NewProc("IcmpCloseHandle")
)

func diagnoseNetwork(ctx context.Context, query NetworkDiagnosticQuery) (NetworkDiagnosticResult, error) {
	constrained := query.InterfaceID != ""
	var index uint32
	var source *windows.RawSockaddrInet
	if constrained {
		adapter, err := diagnosticAdapter(query.InterfaceID, 0, query.SourceIPv4)
		if err != nil {
			return NetworkDiagnosticResult{}, err
		}
		index = adapter.index
		address := diagnosticSockaddr(query.SourceIPv4)
		source = &address
	} else if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: query.DestinationIPv4.As4()}, &index); err != nil {
		return NetworkDiagnosticResult{}, diagnosticRouteError("find best interface", err)
	}
	if index == 0 {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: OS returned zero interface index")
	}
	if err := ctx.Err(); err != nil {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: context: %w", err)
	}
	if err := diagnosticBestRoute.Find(); err != nil {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: load route API: %w", err)
	}
	destination := diagnosticSockaddr(query.DestinationIPv4)
	var row windows.MibIpForwardRow2
	var bestSource windows.RawSockaddrInet
	code, _, _ := diagnosticBestRoute.Call(0, uintptr(index), uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(&destination)), 0, uintptr(unsafe.Pointer(&row)), uintptr(unsafe.Pointer(&bestSource)))
	runtime.KeepAlive(source)
	runtime.KeepAlive(destination)
	if code != 0 {
		cause := syscall.Errno(code)
		if constrained {
			if _, err := diagnosticAdapter(query.InterfaceID, index, query.SourceIPv4); err != nil {
				return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: constrained route: %w: %w", err, cause)
			}
		}
		return NetworkDiagnosticResult{}, diagnosticRouteError("read best route", cause)
	}
	sourceIPv4, ok := diagnosticRawIPv4(&bestSource)
	if !ok || !diagnosticUnicastIPv4(sourceIPv4) || row.InterfaceIndex == 0 || row.InterfaceIndex != index {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid OS route interface or source")
	}
	if constrained && sourceIPv4 != query.SourceIPv4 {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: route source changed: %w", ErrNetworkBindingUnavailable)
	}
	// Recollect after route lookup, so a removed/down adapter or changed source
	// fails closed instead of turning an earlier fact into an apparent binding.
	adapter, err := diagnosticAdapter(query.InterfaceID, index, sourceIPv4)
	if err != nil {
		return NetworkDiagnosticResult{}, err
	}
	if row.InterfaceLuid != adapter.luid {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: route adapter changed: %w", ErrNetworkBindingUnavailable)
	}
	prefixAddress, ok := diagnosticRawIPv4(&row.DestinationPrefix.Prefix)
	if !ok || row.DestinationPrefix.PrefixLength > 32 {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid OS route prefix")
	}
	prefix := netip.PrefixFrom(prefixAddress, int(row.DestinationPrefix.PrefixLength)).Masked()
	if !prefix.Contains(query.DestinationIPv4) {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: OS route prefix does not contain destination")
	}
	nextHop, ok := diagnosticRawIPv4(&row.NextHop)
	if !ok || (!nextHop.IsUnspecified() && !diagnosticUnicastIPv4(nextHop)) {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid OS route next hop")
	}
	iface := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceLuid: adapter.luid, InterfaceIndex: index}
	if err := windows.GetIpInterfaceEntry(&iface); err != nil {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: read interface metric: %w", err)
	}
	if iface.Family != windows.AF_INET || iface.InterfaceIndex != index || iface.InterfaceLuid != adapter.luid {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid OS interface metric identity")
	}
	result := NetworkDiagnosticResult{
		Route: NetworkRoute{InterfaceID: adapter.id, InterfaceIndex: index, SourceIPv4: sourceIPv4, DestinationPrefix: prefix, NextHopIPv4: nextHop, RouteMetric: row.Metric, InterfaceMetric: iface.Metric, EffectiveMetric: effectiveNetworkMetric(row.Metric, iface.Metric)},
		Probe: NetworkProbeResult{Status: NetworkProbeNotRequested},
	}
	if query.Probe {
		result.Probe, err = diagnosticProbe(ctx, sourceIPv4, query.DestinationIPv4)
		if err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: context: %w", err)
	}
	result.ObservedAt = time.Now()
	return result, nil
}

type diagnosticAdapterFact struct {
	id    InterfaceID
	index uint32
	luid  uint64
}

func diagnosticAdapter(id InterfaceID, index uint32, source netip.Addr) (diagnosticAdapterFact, error) {
	buffer, first, err := readWindowsAdapters()
	if err != nil {
		return diagnosticAdapterFact{}, fmt.Errorf("diagnose network: collect adapters: %w", err)
	}
	defer runtime.KeepAlive(buffer)
	for adapter := first; adapter != nil; adapter = adapter.Next {
		actualID := InterfaceID(windows.BytePtrToString(adapter.AdapterName))
		if id != "" && actualID != id || index != 0 && adapter.IfIndex != index {
			continue
		}
		if actualID == "" || adapter.IfIndex == 0 || adapter.Luid == 0 {
			return diagnosticAdapterFact{}, errors.New("diagnose network: invalid OS adapter identity")
		}
		if adapter.OperStatus != windows.IfOperStatusUp {
			break
		}
		for address := adapter.FirstUnicastAddress; address != nil; address = address.Next {
			candidate, ok := socketAddressToIPv4(address.Address)
			if ok && candidate == source {
				return diagnosticAdapterFact{actualID, adapter.IfIndex, adapter.Luid}, nil
			}
		}
		break
	}
	return diagnosticAdapterFact{}, fmt.Errorf("diagnose network: adapter or source unavailable: %w", ErrNetworkBindingUnavailable)
}

func diagnosticRouteError(operation string, cause error) error {
	switch {
	case errors.Is(cause, windows.ERROR_NOT_FOUND), errors.Is(cause, windows.ERROR_FILE_NOT_FOUND), errors.Is(cause, windows.ERROR_NETWORK_UNREACHABLE), errors.Is(cause, windows.ERROR_HOST_UNREACHABLE):
		return fmt.Errorf("diagnose network: %s: %w: %w", operation, ErrNetworkRouteUnavailable, cause)
	default:
		return fmt.Errorf("diagnose network: %s: %w", operation, cause)
	}
}

func diagnosticSockaddr(address netip.Addr) windows.RawSockaddrInet {
	var raw windows.RawSockaddrInet
	ipv4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
	ipv4.Family = windows.AF_INET
	ipv4.Addr = address.As4()
	return raw
}

func diagnosticRawIPv4(raw *windows.RawSockaddrInet) (netip.Addr, bool) {
	if raw.Family != windows.AF_INET {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(raw)).Addr), true
}

func diagnosticProbe(ctx context.Context, source, destination netip.Addr) (probe NetworkProbeResult, err error) {
	probe.Status = NetworkProbeFailed
	if err := ctx.Err(); err != nil {
		return probe, fmt.Errorf("diagnose network: context: %w", err)
	}
	for _, proc := range []*windows.LazyProc{diagnosticICMPCreate, diagnosticICMPSend, diagnosticICMPClose} {
		if err := proc.Find(); err != nil {
			probe.Cause = err
			return probe, fmt.Errorf("diagnose network: load ICMP API: %w", err)
		}
	}
	handle, _, cause := diagnosticICMPCreate.Call()
	if handle == uintptr(windows.InvalidHandle) {
		probe.Cause = cause
		return probe, fmt.Errorf("diagnose network: create ICMP handle: %w", cause)
	}
	defer func() {
		closed, _, cause := diagnosticICMPClose.Call(handle)
		if closed == 0 {
			closeErr := fmt.Errorf("diagnose network: close ICMP handle: %w", cause)
			err = errors.Join(err, closeErr)
			probe.Status = NetworkProbeFailed
			probe.RoundTripTime = nil
			probe.Cause = errors.Join(probe.Cause, closeErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return probe, fmt.Errorf("diagnose network: context: %w", err)
	}
	timeout := uint32(1500)
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < time.Millisecond {
			return probe, fmt.Errorf("diagnose network: context deadline: %w", context.DeadlineExceeded)
		}
		if remaining < 1500*time.Millisecond {
			timeout = uint32(remaining / time.Millisecond)
		}
	}
	// IPAddr has the in-memory network-order bytes of inet_addr, not a
	// big-endian integer value. Windows is little-endian on supported targets.
	sourceBytes, destinationBytes := source.As4(), destination.As4()
	payload := []byte("Sidravia diagnostic")
	var reply [256]byte
	replies, _, cause := diagnosticICMPSend.Call(handle, 0, 0, 0, uintptr(binary.LittleEndian.Uint32(sourceBytes[:])), uintptr(binary.LittleEndian.Uint32(destinationBytes[:])), uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout))
	runtime.KeepAlive(payload)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return probe, fmt.Errorf("diagnose network: context: %w", ctxErr)
	}
	if replies == 0 {
		// Proc.Call captures GetLastError on the calling thread. Do not parse a
		// buffer for which the native API reported no replies.
		var errno syscall.Errno
		if !errors.As(cause, &errno) || errno == 0 {
			probe.Cause = cause
			return probe, fmt.Errorf("diagnose network: ICMP returned no replies without OS cause: %w", cause)
		}
		return classifyNetworkProbe(uint32(errno), cause)
	}
	status := binary.LittleEndian.Uint32(reply[4:8])
	if status != 0 {
		return classifyNetworkProbe(status, syscall.Errno(status))
	}
	if [4]byte(reply[:4]) != destinationBytes {
		return probe, errors.New("diagnose network: ICMP echo address differs from destination")
	}
	rtt := time.Duration(binary.LittleEndian.Uint32(reply[8:12])) * time.Millisecond
	return NetworkProbeResult{Status: NetworkProbeReachable, RoundTripTime: &rtt}, nil
}
