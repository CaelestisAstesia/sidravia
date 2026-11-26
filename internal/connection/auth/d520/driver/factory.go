package driver

import (
	"errors"
	"fmt"

	"sidravia/internal/connection/auth/d520"
	"sidravia/internal/connection/auth/d520/internal/settings"
	protocol "sidravia/internal/connection/auth/protocol"
	environment "sidravia/internal/connection/environment"
	profilevalue "sidravia/internal/connection/profile"
)

// factory is the concrete D520 AuthenticationProtocolFactory. It is stateless;
// every Run is built fresh from the supplied inputs.
type factory struct{}

// NewFactory returns the D520 AuthenticationProtocolFactory. It is the only
// public constructor in this package for the runtime driver.
func NewFactory() protocol.AuthenticationProtocolFactory {
	return factory{}
}

func (factory) ProtocolID() profilevalue.ProtocolID { return d520.ProtocolID }

func (factory) ValidateInstitutionProtocolConfiguration(raw profilevalue.ProtocolConfiguration) error {
	return d520.ValidateInstitution(raw)
}

func (factory) OverrideSchemaVersion() int { return 1 }

func (factory) ValidateProtocolContextOverride(raw profilevalue.ProtocolOverride) error {
	return settings.ValidateOverride(raw)
}

func (factory) ValidateUsername(username string) error {
	if username == "" {
		return errors.New("username is required")
	}
	_, err := encodeUsername(username)
	return err
}

func (factory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	cfg, err := settings.DecodeInstitution(inputs.InstitutionProtocolConfiguration)
	if err != nil {
		return nil, err
	}
	override, err := settings.DecodeOverride(inputs.ProtocolContextOverride)
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
	if override.ReportedMAC != nil {
		mac = *override.ReportedMAC
	} else {
		mac, err = selectMAC(binding)
		if err != nil {
			return nil, err
		}
	}
	clientIPv4 := socketSourceIPv4
	if override.ReportedIPv4 != nil {
		clientIPv4 = *override.ReportedIPv4
	}
	override.ApplyCompatibility(&cfg)
	primaryDNS, secondaryDNS := selectDNS(binding)
	dhcpIPv4 := selectDHCP(binding)

	if override.ReportedDNSIPv4 != nil {
		primaryDNS, secondaryDNS = override.ReportedDNSIPv4[0], override.ReportedDNSIPv4[1]
	}
	if override.ReportedDHCPIPv4 != nil {
		dhcpIPv4 = *override.ReportedDHCPIPv4
	}
	host := inputs.SystemHostInformation
	if override.HostName != nil {
		host.HostName = *override.HostName
	}
	if override.OsFamily != nil {
		host.OperatingSystemFamily = *override.OsFamily
	}
	if override.OsRelease != nil {
		host.OperatingSystemRelease = *override.OsRelease
	}
	login := loginInput{
		jluExperimental:           cfg.JluExperimental,
		username:                  credentialValue.Username,
		password:                  credentialValue.Password,
		mac:                       mac,
		clientIPv4:                clientIPv4,
		hostName:                  host.HostName,
		hostOS:                    deriveHostOS(host),
		primaryDNS:                primaryDNS,
		secondaryDNS:              secondaryDNS,
		dhcpIPv4:                  dhcpIPv4,
		osInfo:                    cfg.OsInfo,
		controlCheckStatus:        cfg.ControlCheckStatus,
		adapterNum:                cfg.AdapterNumber,
		ipdog:                     cfg.Ipdog,
		authVersion:               cfg.AuthVersion,
		loginIPDogPadding:         cfg.LoginIPDogPadding,
		loginDHCPPadding:          cfg.LoginDHCPPadding,
		loginAuthExtensionPadding: cfg.LoginAuthExtensionPadding,
	}
	if _, err := buildLoginRequest(login, [4]byte{}); err != nil {
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
	cfg              settings.Institution
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

func deriveHostOS(host environment.SystemHostInformation) string {
	return settings.HostOS(host.OperatingSystemFamily, host.OperatingSystemRelease)
}
