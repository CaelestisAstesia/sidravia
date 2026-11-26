//go:build windows

package environment

import (
	"fmt"
	"runtime"
	"slices"
	"time"

	"golang.org/x/sys/windows"
)

const systemObserverPollInterval = 2 * time.Second

func newSystemObserver() Observer {
	return &systemObserver{
		collect:  readWindowsNetworkInterfaces,
		now:      time.Now,
		interval: systemObserverPollInterval,
	}
}

// readWindowsNetworkInterfaces collects real Windows adapter facts through
// GetAdaptersAddresses. It requests unicast, DNS and gateway information for
// the IPv4 family, copies every retained value out of the temporary Windows
// buffer, retains loopback and omits tunnel adapters, and returns a deterministically
// ordered slice. No shell-out, registry access, watcher, goroutine or retry
// beyond the bounded buffer resize is used.
func readWindowsNetworkInterfaces() ([]NetworkInterface, error) {
	buffer, adapters, err := readWindowsAdapters()
	if err != nil {
		return nil, err
	}
	defer runtime.KeepAlive(buffer)
	if adapters == nil {
		return nil, nil
	}

	interfaces := make([]NetworkInterface, 0)
	for aa := adapters; aa != nil; aa = aa.Next {
		interfaceID := windows.BytePtrToString(aa.AdapterName)
		if interfaceID == "" {
			return nil, fmt.Errorf("read windows network interfaces: adapter with empty name")
		}
		if aa.IfType == windows.IF_TYPE_TUNNEL {
			continue
		}

		row := windows.MibIfRow2{InterfaceLuid: aa.Luid}
		if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormalWithoutStatistics, &row); err != nil {
			if err == windows.ERROR_FILE_NOT_FOUND {
				continue
			}
			return nil, fmt.Errorf(
				"read windows network interfaces: read classification for adapter %q: %w",
				interfaceID,
				err,
			)
		}
		classification := decodeWindowsInterfaceClassification(row.InterfaceAndOperStatusFlags)

		if uint32(len(aa.PhysicalAddress)) < aa.PhysicalAddressLength {
			return nil, fmt.Errorf(
				"read windows network interfaces: adapter %q hardware address length %d exceeds fixed array size %d",
				interfaceID,
				aa.PhysicalAddressLength,
				len(aa.PhysicalAddress),
			)
		}
		hardwareAddress := make([]byte, aa.PhysicalAddressLength)
		copy(hardwareAddress, aa.PhysicalAddress[:aa.PhysicalAddressLength])

		unicasts, hasDHCP, hasNonManual := collectUnicastAddresses(aa.FirstUnicastAddress)
		gateways := collectGatewayIPv4Addresses(aa.FirstGatewayAddress)
		dnsServers := collectDNSIPv4Addresses(aa.FirstDnsServerAddress)
		dhcpServer := collectDhcpv4Server(aa.Dhcpv4Server)

		unicasts = dedupUnicastAssignments(unicasts)
		gateways = dedupAddrs(gateways)
		dnsServers = dedupAddrs(dnsServers)
		sortUnicastAssignments(unicasts)
		sortAddrs(gateways)
		// DNS preserves Windows-reported order after dedup, because D520
		// consumes the first IPv4 entries.

		networkInterface, err := NewNetworkInterface(NetworkInterfaceFacts{
			InterfaceID:                 InterfaceID(interfaceID),
			DisplayName:                 windows.UTF16PtrToString(aa.FriendlyName),
			Description:                 windows.UTF16PtrToString(aa.Description),
			OperationalState:            mapOperationalState(aa.OperStatus),
			PhysicalMedium:              mapPhysicalMedium(aa.IfType),
			HardwareBacked:              classification.hardwareBacked,
			PhysicalConnectorPresent:    classification.physicalConnectorPresent,
			FilterInterface:             classification.filterInterface,
			EndpointInterface:           classification.endpointInterface,
			HardwareAddress:             hardwareAddress,
			AddressAssignmentMethod:     classifyAssignmentMethod(unicasts, hasDHCP, hasNonManual),
			IPv4AddressAssignments:      unicasts,
			DefaultIPv4GatewayAddresses: gateways,
			DNSServerAddresses:          dnsServers,
			DHCPServerIPv4Address:       dhcpServer,
		})
		if err != nil {
			return nil, fmt.Errorf("read windows network interfaces: %w", err)
		}
		interfaces = append(interfaces, networkInterface)
	}

	slices.SortFunc(interfaces, func(a, b NetworkInterface) int {
		switch {
		case a.InterfaceID < b.InterfaceID:
			return -1
		case a.InterfaceID > b.InterfaceID:
			return 1
		default:
			return 0
		}
	})
	return interfaces, nil
}
