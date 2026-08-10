package d520

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

// listenUDP binds a udp4 socket to ip:0 and returns the connection and its
// local address. The caller owns closing the connection.
func listenUDP(t *testing.T, ip net.IP) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		t.Fatalf("listen udp %v: %v", ip, err)
	}
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// freeUDPPort returns a port number that is currently free on ip by briefly
// binding ip:0 and closing the probe. It is used only to pick a fixed local
// port for a test; the caller rebinds it immediately. It never assumes port
// 61440 is free.
func freeUDPPort(t *testing.T, ip net.IP) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		t.Fatalf("probe free port on %v: %v", ip, err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := conn.Close(); err != nil {
		t.Fatalf("close free-port probe: %v", err)
	}
	return port
}

// captureSourceAddr starts a read on serverConn, invokes write, and returns the
// source address of the first datagram the server observes. It proves the
// exchange's writes reach the server.
func captureSourceAddr(t *testing.T, serverConn *net.UDPConn, write func()) *net.UDPAddr {
	t.Helper()
	type result struct {
		addr *net.UDPAddr
		ok   bool
	}
	ch := make(chan result, 1)
	go func() {
		if err := serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			ch <- result{}
			return
		}
		buf := make([]byte, 1500)
		_, addr, err := serverConn.ReadFromUDP(buf)
		if err != nil {
			ch <- result{}
			return
		}
		ch <- result{addr: addr, ok: true}
	}()
	write()
	select {
	case r := <-ch:
		if !r.ok {
			t.Fatal("server did not receive a datagram")
		}
		return r.addr
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive a datagram")
		return nil
	}
}

// TestUDPFixedLocalPortIsObservedByServer proves a fixed local port is the
// source port observed by an independent server. The server stays bound to an
// OS-assigned port while a separate free local port is selected, so both
// endpoints can use the universally available 127.0.0.1 loopback address.
func TestUDPFixedLocalPortIsObservedByServer(t *testing.T) {
	serverConn, serverAddr := listenUDP(t, net.IPv4(127, 0, 0, 1))
	defer serverConn.Close()
	portNum := freeUDPPort(t, net.IPv4(127, 0, 0, 1))

	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortFixed, value: uint16(portNum)}, netip.MustParseAddr("127.0.0.1"), uint16(serverAddr.Port))
	if err != nil {
		t.Fatalf("openUDPExchange fixed: %v", err)
	}
	defer ex.close()

	src := captureSourceAddr(t, serverConn, func() {
		if _, err := ex.conn.Write([]byte("fixed")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	if src.Port != portNum {
		t.Fatalf("source port = %d, want fixed %d", src.Port, portNum)
	}
	if !src.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("source IP = %v, want 127.0.0.1", src.IP)
	}
}

// TestUDPSystemAssignedBindsNonzeroPort proves system_assigned binds a nonzero
// OS-selected source port.
func TestUDPSystemAssignedBindsNonzeroPort(t *testing.T) {
	serverConn, serverAddr := listenUDP(t, net.IPv4(127, 0, 0, 1))
	defer serverConn.Close()

	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, netip.MustParseAddr("127.0.0.1"), uint16(serverAddr.Port))
	if err != nil {
		t.Fatalf("openUDPExchange system_assigned: %v", err)
	}
	defer ex.close()

	src := captureSourceAddr(t, serverConn, func() {
		if _, err := ex.conn.Write([]byte("sys")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	if src.Port == 0 {
		t.Fatalf("source port = 0, want OS-assigned nonzero")
	}
	if !src.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("source IP = %v, want 127.0.0.1", src.IP)
	}
}

// TestUDPOccupiedFixedLocalPortFailsWithoutFallback proves an occupied fixed
// local address/port fails and does not silently fall back to a
// system-assigned port. A holder socket is bound to 127.0.0.1:port before the
// exchange is opened, so a fallback to port 0 would have succeeded; observing a
// non-nil error and a nil exchange proves no fallback occurred.
func TestUDPOccupiedFixedLocalPortFailsWithoutFallback(t *testing.T) {
	holder, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("bind holder: %v", err)
	}
	defer holder.Close()
	occupiedPort := holder.LocalAddr().(*net.UDPAddr).Port

	serverConn, serverAddr := listenUDP(t, net.IPv4(127, 0, 0, 1))
	defer serverConn.Close()

	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortFixed, value: uint16(occupiedPort)}, netip.MustParseAddr("127.0.0.1"), uint16(serverAddr.Port))
	if err == nil {
		if ex != nil {
			ex.close()
		}
		t.Fatal("openUDPExchange unexpectedly succeeded on an occupied fixed port (silent fallback to a system-assigned port)")
	}
	if ex != nil {
		t.Fatal("exchange returned alongside an error")
	}
}

// TestUDPLocalIPv4AndRemoteEndpointAreIndependent proves the local source IPv4
// and the remote server endpoint are independent dimensions. The exchange
// binds 127.0.0.1 and is connected to server A's OS-assigned port; server A
// observes the datagram with source IP 127.0.0.1, while server B on a different
// port receives nothing because connected UDP sends only to the configured
// remote endpoint.
func TestUDPLocalIPv4AndRemoteEndpointAreIndependent(t *testing.T) {
	serverA, addrA := listenUDP(t, net.IPv4(127, 0, 0, 1))
	defer serverA.Close()
	serverB, _ := listenUDP(t, net.IPv4(127, 0, 0, 1))
	defer serverB.Close()

	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, netip.MustParseAddr("127.0.0.1"), uint16(addrA.Port))
	if err != nil {
		t.Fatalf("openUDPExchange: %v", err)
	}
	defer ex.close()

	src := captureSourceAddr(t, serverA, func() {
		if _, err := ex.conn.Write([]byte("to-A")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	if !src.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("source IP = %v, want 127.0.0.1", src.IP)
	}
	if src.Port == 0 {
		t.Fatalf("source port = 0, want nonzero")
	}

	if err := serverB.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("set server B deadline: %v", err)
	}
	buf := make([]byte, 1500)
	if _, _, err := serverB.ReadFromUDP(buf); err == nil {
		t.Fatal("server B received a datagram destined for server A; remote endpoint is not independent of the local source IPv4")
	}
}
