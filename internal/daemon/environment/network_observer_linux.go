//go:build linux

package environment

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"
)

// linuxInterfaceEntry carries the kernel-reported facts for one interface
// without invoking net.Interface.Addrs at classification time. The production
// source reads them from net.Interfaces and each interface's Addrs; tests inject
// a private replacement through linuxSourceFunc.
type linuxInterfaceEntry struct {
	Name         string
	Flags        net.Flags
	HardwareAddr []byte
	Addresses    []net.Addr
}

// linuxSourceFunc lists interfaces with their addresses. Tests inject a private
// replacement; the production path reads net.Interfaces and Addrs. It must not
// shell out or start a goroutine.
type linuxSourceFunc func() ([]linuxInterfaceEntry, error)

// statFunc reports sysfs path metadata. Tests inject a private replacement; the
// production path uses os.Stat.
type statFunc func(string) (os.FileInfo, error)

var (
	defaultLinuxSource linuxSourceFunc = readLinuxInterfaces
	defaultStat        statFunc        = os.Stat
)

func readLinuxInterfaces() ([]linuxInterfaceEntry, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	entries := make([]linuxInterfaceEntry, len(ifaces))
	for index, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		entries[index] = linuxInterfaceEntry{
			Name:         iface.Name,
			Flags:        iface.Flags,
			HardwareAddr: iface.HardwareAddr,
			Addresses:    addrs,
		}
	}
	return entries, nil
}

func newSystemObserver() Observer {
	return &systemObserver{
		collect:  func() ([]NetworkInterface, error) { return collectLinuxInterfaces(defaultLinuxSource, defaultStat) },
		now:      time.Now,
		interval: 2 * time.Second,
	}
}

// collectLinuxInterfaces enumerates interfaces, retains loopback, classifies each
// observed interface from sysfs facts, and returns a stable, sorted slice.
// Any source or unexpected sysfs stat error stops collection with a wrapped
// cause; an absent sysfs path is an ordinary false fact.
func collectLinuxInterfaces(source linuxSourceFunc, stat statFunc) ([]NetworkInterface, error) {
	entries, err := source()
	if err != nil {
		return nil, fmt.Errorf("枚举网络接口: %w", err)
	}
	results := make([]NetworkInterface, 0, len(entries))
	for _, entry := range entries {
		networkIface, err := buildLinuxInterface(entry, stat)
		if err != nil {
			return nil, err
		}
		results = append(results, networkIface)
	}
	slices.SortFunc(results, compareInterfacesByID)
	return results, nil
}

func buildLinuxInterface(entry linuxInterfaceEntry, stat statFunc) (NetworkInterface, error) {
	name := entry.Name
	ipv4Assignments := collectLinuxIPv4Assignments(entry.Addresses)

	hardwareBacked, err := linuxSysfsExists(stat, name, "device")
	if err != nil {
		return NetworkInterface{}, fmt.Errorf("检查接口 %q 设备: %w", name, err)
	}
	wireless := false
	if hardwareBacked {
		wireless, err = linuxSysfsExists(stat, name, "wireless")
		if err != nil {
			return NetworkInterface{}, fmt.Errorf("检查接口 %q 无线: %w", name, err)
		}
	}

	// The kernel interface name is the only stable identifier this baseline has,
	// so it serves as both InterfaceID and DisplayName.
	medium := PhysicalMediumUnknown
	switch {
	case wireless:
		medium = PhysicalMediumWireless
	case hardwareBacked && len(entry.HardwareAddr) == 6:
		medium = PhysicalMediumWired
	}

	state := OperationalStateDown
	if entry.Flags&net.FlagUp != 0 {
		state = OperationalStateUp
	}

	facts := NetworkInterfaceFacts{
		InterfaceID:              InterfaceID(name),
		DisplayName:              name,
		OperationalState:         state,
		PhysicalMedium:           medium,
		HardwareBacked:           hardwareBacked,
		PhysicalConnectorPresent: hardwareBacked,
		FilterInterface:          false,
		EndpointInterface:        !hardwareBacked,
		HardwareAddress:          slices.Clone(entry.HardwareAddr),
		AddressAssignmentMethod:  AddressAssignmentUnknown,
		IPv4AddressAssignments:   ipv4Assignments,
	}
	networkIface, err := NewNetworkInterface(facts)
	if err != nil {
		return NetworkInterface{}, fmt.Errorf("构造接口 %q: %w", name, err)
	}
	return networkIface, nil
}

// collectLinuxIPv4Assignments retains only valid, non-unspecified,
// non-multicast IPv4 unicast assignments with their real prefix lengths, then
// sorts and deduplicates them so enumeration order alone cannot advance a
// revision.
func collectLinuxIPv4Assignments(addrs []net.Addr) []IPv4AddressAssignment {
	seen := make(map[IPv4AddressAssignment]struct{})
	assignments := make([]IPv4AddressAssignment, 0, len(addrs))
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil {
			continue
		}
		ones, bits := ipNet.Mask.Size()
		if bits != 32 {
			continue
		}
		addr4, ok := netip.AddrFromSlice(ip4)
		if !ok {
			continue
		}
		if addr4.IsUnspecified() || addr4.IsMulticast() {
			continue
		}
		assignment := IPv4AddressAssignment{Address: addr4, PrefixLength: uint8(ones)}
		if _, dup := seen[assignment]; dup {
			continue
		}
		seen[assignment] = struct{}{}
		assignments = append(assignments, assignment)
	}
	slices.SortFunc(assignments, compareIPv4Assignments)
	return assignments
}

// linuxSysfsExists reports whether a sysfs path exists. An absent path is an
// ordinary false fact; any other stat error is returned so it can stop
// collection with a wrapped cause.
func linuxSysfsExists(stat statFunc, name, suffix string) (bool, error) {
	_, err := stat("/sys/class/net/" + name + "/" + suffix)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func compareInterfacesByID(a, b NetworkInterface) int {
	return strings.Compare(string(a.InterfaceID), string(b.InterfaceID))
}

func compareIPv4Assignments(a, b IPv4AddressAssignment) int {
	if c := a.Address.Compare(b.Address); c != 0 {
		return c
	}
	return int(a.PrefixLength) - int(b.PrefixLength)
}
