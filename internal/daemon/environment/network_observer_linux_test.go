//go:build linux

package environment

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func linuxAddr(ip string, prefix int) net.Addr {
	return &net.IPNet{
		IP:   net.ParseIP(ip).To4(),
		Mask: net.CIDRMask(prefix, 32),
	}
}

func mustParseIPv4(s string) netip.Addr {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return addr
}

func missingStat(string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

type dummyFileInfo struct{}

func (dummyFileInfo) Name() string       { return "" }
func (dummyFileInfo) Size() int64        { return 0 }
func (dummyFileInfo) Mode() os.FileMode  { return 0 }
func (dummyFileInfo) ModTime() time.Time { return time.Time{} }
func (dummyFileInfo) IsDir() bool        { return false }
func (dummyFileInfo) Sys() any           { return nil }

func findInterface(ifaces []NetworkInterface, id string) NetworkInterface {
	for _, iface := range ifaces {
		if string(iface.InterfaceID) == id {
			return iface
		}
	}
	return NetworkInterface{}
}

func staticSource(entries []linuxInterfaceEntry) linuxSourceFunc {
	return func() ([]linuxInterfaceEntry, error) { return entries, nil }
}

func TestCollectLinuxInterfacesOmitsLoopback(t *testing.T) {
	entries := []linuxInterfaceEntry{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback, Addresses: []net.Addr{linuxAddr("127.0.0.1", 8)}},
		{Name: "eth0", Flags: net.FlagUp, HardwareAddr: []byte{1, 2, 3, 4, 5, 6}, Addresses: []net.Addr{linuxAddr("192.168.1.10", 24)}},
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), missingStat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	if len(ifaces) != 1 || string(ifaces[0].InterfaceID) != "eth0" {
		t.Fatalf("interfaces = %+v, want only eth0", ifaces)
	}
}

func TestCollectLinuxInterfacesSortsAndDeduplicatesIPv4(t *testing.T) {
	entries := []linuxInterfaceEntry{
		{Name: "eth1", Flags: net.FlagUp, Addresses: []net.Addr{
			linuxAddr("192.168.1.20", 24),
			linuxAddr("192.168.1.10", 24),
			linuxAddr("192.168.1.10", 24),
			linuxAddr("10.0.0.5", 8),
		}},
		{Name: "eth0", Flags: net.FlagUp, Addresses: []net.Addr{linuxAddr("10.0.0.1", 8)}},
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), missingStat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	if len(ifaces) != 2 || string(ifaces[0].InterfaceID) != "eth0" || string(ifaces[1].InterfaceID) != "eth1" {
		t.Fatalf("interface order = %v, want eth0 then eth1", ifaces)
	}
	assignments := ifaces[1].IPv4AddressAssignments()
	want := []IPv4AddressAssignment{
		{Address: mustParseIPv4("10.0.0.5"), PrefixLength: 8},
		{Address: mustParseIPv4("192.168.1.10"), PrefixLength: 24},
		{Address: mustParseIPv4("192.168.1.20"), PrefixLength: 24},
	}
	if !slices.Equal(assignments, want) {
		t.Errorf("assignments = %+v, want %+v", assignments, want)
	}
}

func TestCollectLinuxInterfacesRetainsDistinctPrefixes(t *testing.T) {
	entries := []linuxInterfaceEntry{
		{Name: "eth0", Flags: net.FlagUp, Addresses: []net.Addr{
			linuxAddr("10.0.0.1", 24),
			linuxAddr("10.0.0.1", 8),
		}},
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), missingStat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	assignments := ifaces[0].IPv4AddressAssignments()
	if len(assignments) != 2 {
		t.Fatalf("assignments = %+v, want 2 distinct prefixes", assignments)
	}
	if assignments[0].PrefixLength != 8 || assignments[1].PrefixLength != 24 {
		t.Errorf("prefix order = %+v, want 8 then 24", assignments)
	}
}

func TestCollectLinuxInterfacesUpAndDownState(t *testing.T) {
	entries := []linuxInterfaceEntry{
		{Name: "down0", Flags: 0},
		{Name: "up0", Flags: net.FlagUp},
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), missingStat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	if findInterface(ifaces, "up0").OperationalState != OperationalStateUp {
		t.Error("up0 state = down, want up")
	}
	if findInterface(ifaces, "down0").OperationalState != OperationalStateDown {
		t.Error("down0 state = up, want down")
	}
}

func TestCollectLinuxInterfacesClassification(t *testing.T) {
	ethMAC := []byte{1, 2, 3, 4, 5, 6}
	entries := []linuxInterfaceEntry{
		{Name: "wlan0", Flags: net.FlagUp, HardwareAddr: ethMAC},
		{Name: "eth0", Flags: net.FlagUp, HardwareAddr: ethMAC},
		{Name: "veth0", Flags: net.FlagUp, HardwareAddr: ethMAC},
	}
	stat := func(path string) (os.FileInfo, error) {
		switch path {
		case "/sys/class/net/wlan0/device", "/sys/class/net/wlan0/wireless", "/sys/class/net/eth0/device":
			return dummyFileInfo{}, nil
		default:
			return nil, os.ErrNotExist
		}
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), stat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	wlan := findInterface(ifaces, "wlan0")
	if wlan.PhysicalMedium != PhysicalMediumWireless || !wlan.HardwareBacked || !wlan.PhysicalConnectorPresent {
		t.Errorf("wlan0 = %+v, want wireless hardware-backed", wlan)
	}
	if wlan.EndpointInterface {
		t.Error("wlan0 EndpointInterface = true, want false for hardware-backed")
	}
	eth := findInterface(ifaces, "eth0")
	if eth.PhysicalMedium != PhysicalMediumWired || !eth.HardwareBacked || !eth.PhysicalConnectorPresent {
		t.Errorf("eth0 = %+v, want wired hardware-backed", eth)
	}
	if eth.EndpointInterface {
		t.Error("eth0 EndpointInterface = true, want false for hardware-backed")
	}
	veth := findInterface(ifaces, "veth0")
	if veth.PhysicalMedium != PhysicalMediumUnknown || veth.HardwareBacked || veth.PhysicalConnectorPresent {
		t.Errorf("veth0 = %+v, want unknown virtual", veth)
	}
	if !veth.EndpointInterface {
		t.Error("veth0 EndpointInterface = false, want true for virtual")
	}
}

func TestCollectLinuxInterfacesVirtualNotPhysicalCandidate(t *testing.T) {
	entries := []linuxInterfaceEntry{
		{Name: "veth0", Flags: net.FlagUp, HardwareAddr: []byte{1, 2, 3, 4, 5, 6}, Addresses: []net.Addr{linuxAddr("10.0.0.5", 24)}},
	}
	ifaces, err := collectLinuxInterfaces(staticSource(entries), missingStat)
	if err != nil {
		t.Fatalf("collectLinuxInterfaces = %v, want nil", err)
	}
	veth := ifaces[0]
	if veth.HardwareBacked || veth.PhysicalConnectorPresent {
		t.Errorf("veth0 hardware-backed = %v, want false", veth.HardwareBacked)
	}
	if !veth.EndpointInterface {
		t.Error("veth0 EndpointInterface = false, want true")
	}
}

func TestLinuxSystemObserverEmptyRevisionNotUnsupported(t *testing.T) {
	observer := &systemObserver{
		collect:  func() ([]NetworkInterface, error) { return nil, nil },
		now:      func() time.Time { return time.Time{} },
		interval: 10 * time.Millisecond,
	}
	output := make(chan Snapshot, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := observer.Observe(ctx, output)
	if err != nil {
		t.Fatalf("Observe = %v, want nil", err)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatal("Observe returned ErrUnsupported for empty Linux facts")
	}
	select {
	case snapshot := <-output:
		if snapshot.Revision != 1 {
			t.Errorf("snapshot revision = %d, want 1", snapshot.Revision)
		}
		if len(snapshot.Interfaces()) != 0 {
			t.Errorf("snapshot interfaces = %v, want empty", snapshot.Interfaces())
		}
	default:
		t.Fatal("observer published no snapshot")
	}
}

func TestCollectLinuxInterfacesPreservesSourceCause(t *testing.T) {
	cause := errors.New("injected source failure")
	_, err := collectLinuxInterfaces(func() ([]linuxInterfaceEntry, error) { return nil, cause }, missingStat)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want source cause preserved", err)
	}
	if !strings.Contains(err.Error(), "枚举网络接口") {
		t.Errorf("error = %v, want enumeration label", err)
	}
}

func TestCollectLinuxInterfacesPreservesUnexpectedStatCause(t *testing.T) {
	cause := errors.New("injected stat failure")
	entries := []linuxInterfaceEntry{{Name: "eth0", Flags: net.FlagUp}}
	stat := func(string) (os.FileInfo, error) { return nil, cause }
	_, err := collectLinuxInterfaces(staticSource(entries), stat)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want stat cause preserved", err)
	}
}

func TestLinuxSystemObserverCancellation(t *testing.T) {
	observer := &systemObserver{
		collect:  func() ([]NetworkInterface, error) { return []NetworkInterface{}, nil },
		now:      func() time.Time { return time.Time{} },
		interval: time.Hour,
	}
	output := make(chan Snapshot, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := observer.Observe(ctx, output)
	if err != nil {
		t.Fatalf("Observe = %v, want nil on canceled context", err)
	}
}

func TestLinuxSystemObserverAdvancesRevisionOnChange(t *testing.T) {
	first, err := NewNetworkInterface(NetworkInterfaceFacts{InterfaceID: "eth0", DisplayName: "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewNetworkInterface(NetworkInterfaceFacts{InterfaceID: "eth0", DisplayName: "eth0", HardwareBacked: true})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := 0
	collect := func() ([]NetworkInterface, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return []NetworkInterface{first}, nil
		}
		return []NetworkInterface{second}, nil
	}
	observer := &systemObserver{
		collect:  collect,
		now:      func() time.Time { return time.Time{} },
		interval: 5 * time.Millisecond,
	}
	output := make(chan Snapshot, 8)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = observer.Observe(ctx, output) }()

	snap1 := <-output
	if snap1.Revision != 1 {
		t.Fatalf("first revision = %d, want 1", snap1.Revision)
	}
	snap2 := <-output
	if snap2.Revision != 2 {
		t.Fatalf("second revision = %d, want 2", snap2.Revision)
	}
	cancel()
}
