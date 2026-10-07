package d520

import (
	"errors"
	"fmt"
	"strings"

	protocol "sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

// factory is the concrete D520 AuthenticationProtocolFactory. It is stateless;
// every Run is built fresh from the supplied inputs.
type factory struct{}

// NewFactory returns the D520 AuthenticationProtocolFactory. It is the only
// public constructor in this package alongside ProtocolID.
func NewFactory() protocol.AuthenticationProtocolFactory {
	return factory{}
}

func (factory) ProtocolID() protocol.AuthenticationProtocolID { return ProtocolID }

func (factory) ValidateInstitutionProtocolConfiguration(raw protocol.InstitutionProtocolConfiguration) error {
	_, err := decodeInstitutionProtocolConfiguration(raw)
	return err
}

func (factory) ValidateProtocolContextOverride(raw protocol.AuthenticationProtocolContextOverride) error {
	return validateProtocolContextOverride(raw)
}

func (factory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	cfg, err := decodeInstitutionProtocolConfiguration(inputs.InstitutionProtocolConfiguration)
	if err != nil {
		return nil, err
	}
	override, err := decodeProtocolContextOverride(inputs.ProtocolContextOverride)
	if err != nil {
		return nil, err
	}

	credentialValue := inputs.AuthenticationCredential
	if credentialValue.Username == "" {
		return nil, errors.New("username is required")
	}

	binding := inputs.SelectedSystemNetworkBinding
	socketSourceIPv4, err := selectClientIPv4(binding)
	if err != nil {
		return nil, err
	}
	var mac [6]byte
	if override.reportedMAC != nil {
		mac = *override.reportedMAC
	} else {
		mac, err = selectMAC(binding)
		if err != nil {
			return nil, err
		}
	}
	clientIPv4 := socketSourceIPv4
	if override.reportedIPv4 != nil {
		clientIPv4 = *override.reportedIPv4
	}
	override.applyCompatibility(&cfg)
	primaryDNS, secondaryDNS := selectDNS(binding)
	dhcpIPv4 := selectDHCP(binding)

	if override.reportedDNSIPv4 != nil {
		primaryDNS, secondaryDNS = override.reportedDNSIPv4[0], override.reportedDNSIPv4[1]
	}
	if override.reportedDHCPIPv4 != nil {
		dhcpIPv4 = *override.reportedDHCPIPv4
	}
	host := inputs.SystemHostInformation
	if override.hostName != nil {
		host.HostName = *override.hostName
	}
	if override.osFamily != nil {
		host.OperatingSystemFamily = *override.osFamily
	}
	if override.osRelease != nil {
		host.OperatingSystemRelease = *override.osRelease
	}
	login := loginInput{
		username:                  credentialValue.Username,
		password:                  credentialValue.Password,
		mac:                       mac,
		clientIPv4:                clientIPv4,
		hostName:                  host.HostName,
		hostOS:                    deriveHostOS(host),
		primaryDNS:                primaryDNS,
		secondaryDNS:              secondaryDNS,
		dhcpIPv4:                  dhcpIPv4,
		osInfo:                    cfg.osInfo,
		controlCheckStatus:        cfg.controlCheckStatus,
		adapterNum:                cfg.adapterNumber,
		ipdog:                     cfg.ipdog,
		authVersion:               cfg.authVersion,
		loginIPDogPadding:         cfg.loginIPDogPadding,
		loginDHCPPadding:          cfg.loginDHCPPadding,
		loginAuthExtensionPadding: cfg.loginAuthExtensionPadding,
	}
	if _, _, _, _, err := encodeCredentialFields(login); err != nil {
		return nil, err
	}

	diagnostics := inputs.Diagnostics
	if diagnostics == nil {
		diagnostics = protocol.NoopAuthenticationProtocolDiagnostics{}
	}
	return &d520Run{
		definition: runDefinition{
			login:            login,
			cfg:              cfg,
			socketSourceIPv4: socketSourceIPv4,
		},
		diagnostics: diagnostics,
	}, nil
}

// runDefinition is the immutable private Run definition. login holds the
// per-packet inputs (authExtTail is generated fresh per execution and
// overwrites the zero placeholder before use); cfg holds the institution
// Profile values the Run needs at execution time. socketSourceIPv4 is the
// actual binding and is never replaced by the reported clientIPv4. No caller-owned mutable
// slice or JSON buffer is retained.
type runDefinition struct {
	login            loginInput
	cfg              institutionConfig
	socketSourceIPv4 [4]byte
}

// selectClientIPv4 takes the client IPv4 from the selected binding and rejects
// an unspecified address before copying it into a fixed value.
func selectClientIPv4(binding environment.SelectedSystemNetworkBinding) ([4]byte, error) {
	addr := binding.LocalIPv4AddressAssignment().Address
	if !addr.Is4() || addr.IsUnspecified() {
		return [4]byte{}, errors.New("client IPv4 address is unavailable or unspecified")
	}
	return addr.As4(), nil
}

// selectMAC takes the 6-byte MAC from the selected interface and rejects a
// wrong length or an all-zero MAC before copying it into a fixed value.
func selectMAC(binding environment.SelectedSystemNetworkBinding) ([6]byte, error) {
	macBytes := binding.NetworkInterface().HardwareAddress()
	if len(macBytes) != macLength {
		return [6]byte{}, fmt.Errorf("mac is %d bytes, expected %d", len(macBytes), macLength)
	}
	var mac [6]byte
	copy(mac[:], macBytes)
	if mac == ([6]byte{}) {
		return [6]byte{}, errors.New("mac is all zero")
	}
	return mac, nil
}

// selectDNS takes the first two IPv4 DNS addresses from the selected
// interface and fills missing positions with zero.
func selectDNS(binding environment.SelectedSystemNetworkBinding) (primary, secondary [4]byte) {
	dnsServers := binding.NetworkInterface().DNSServerAddresses()
	filled := 0
	for _, server := range dnsServers {
		if !server.Is4() {
			continue
		}
		if filled == 0 {
			primary = server.As4()
		} else if filled == 1 {
			secondary = server.As4()
		}
		filled++
		if filled == 2 {
			break
		}
	}
	return primary, secondary
}

// selectDHCP returns the selected interface's IPv4 DHCP server when present,
// otherwise zero.
func selectDHCP(binding environment.SelectedSystemNetworkBinding) [4]byte {
	dhcp, ok := binding.NetworkInterface().DHCPServerIPv4Address()
	if !ok || !dhcp.Is4() {
		return [4]byte{}
	}
	return dhcp.As4()
}

// deriveHostOS forms the wire host OS by trimming the OS family and release
// and joining non-empty parts with one ASCII space. Machine architecture is
// not sent.
func deriveHostOS(host environment.SystemHostInformation) string {
	family := strings.TrimSpace(host.OperatingSystemFamily)
	release := strings.TrimSpace(host.OperatingSystemRelease)
	switch {
	case family != "" && release != "":
		return family + " " + release
	case family != "":
		return family
	case release != "":
		return release
	default:
		return ""
	}
}
