package application

import (
	"net/netip"
	"time"

	"sidravia/internal/daemon/internal/session"
	"sidravia/internal/ipc/contract"
)

func networkEndpoint(endpoint netip.AddrPort) *contract.NetworkEndpoint {
	return &contract.NetworkEndpoint{Address: endpoint.Addr().String(), Port: endpoint.Port()}
}
func networkProtocolSocket(socket session.ProtocolSocketObservation) *contract.NetworkProtocolSocket {
	result := &contract.NetworkProtocolSocket{State: string(socket.State), RunGeneration: socket.RunGeneration}
	if !socket.UpdatedAt.IsZero() {
		text := socket.UpdatedAt.Format(time.RFC3339Nano)
		result.UpdatedAt = &text
	}
	if socket.State != session.ProtocolSocketNotObserved {
		result.LocalEndpoint, result.RemoteEndpoint = networkEndpoint(socket.LocalEndpoint), networkEndpoint(socket.RemoteEndpoint)
	}
	return result
}
