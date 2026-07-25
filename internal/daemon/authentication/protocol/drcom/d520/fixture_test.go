package d520

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fixturePath locates the tracked client-vector fixture deterministically from
// the package test directory. It does not copy or generate a second source.
func fixturePath(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "..", "..",
		"tools", "drcom520d_mock_server", "tests", "fixtures", "d520_client_vectors_v1.json")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture not found at %s: %v", abs, err)
	}
	return abs
}

type fixtureCryptoItem struct {
	InputHex    string `json:"input_hex"`
	MaterialHex string `json:"material_hex"`
	OutputHex   string `json:"output_hex"`
}

type fixtureExchange struct {
	Name                   string `json:"name"`
	Operation              string `json:"operation"`
	RequestHex             string `json:"request_hex"`
	RequestLength          int    `json:"request_length"`
	DeclaredResponseHex    string `json:"declared_response_hex"`
	DeclaredResponseLength int    `json:"declared_response_length"`
}

type fixtureDoc struct {
	SchemaVersion int    `json:"schema_version"`
	ProtocolID    string `json:"protocol_id"`
	Determinism   struct {
		ServerSecretHex string   `json:"server_secret_hex"`
		SaltSequenceHex []string `json:"salt_sequence_hex"`
		ClientEndpoint  struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"client_endpoint"`
		ClockStart float64 `json:"clock_start"`
	} `json:"determinism"`
	FictionalInputs struct {
		Username              string `json:"username"`
		Password              string `json:"password"`
		ClientIPv4            string `json:"client_ipv4"`
		MacHex                string `json:"mac_hex"`
		PrimaryDNS            string `json:"primary_dns"`
		SecondaryDNS          string `json:"secondary_dns"`
		DhcpIPv4              string `json:"dhcp_ipv4"`
		HostName              string `json:"host_name"`
		HostOS                string `json:"host_os"`
		AuthVersionHex        string `json:"auth_version_hex"`
		KeepAliveVersionHex   string `json:"keep_alive_version_hex"`
		ControlCheckStatusHex string `json:"control_check_status_hex"`
		IPDogHex              string `json:"ipdog_hex"`
		AdapterNumHex         string `json:"adapter_num_hex"`
		OSInfoHex             string `json:"os_info_hex"`
	} `json:"fictional_inputs"`
	CryptoIntermediates struct {
		SaltHex      string            `json:"salt_hex"`
		MD5A         fixtureCryptoItem `json:"md5_a"`
		MD5B         fixtureCryptoItem `json:"md5_b"`
		MD5C         fixtureCryptoItem `json:"md5_c"`
		MACXOR       fixtureCryptoItem `json:"mac_xor"`
		CRC1968      fixtureCryptoItem `json:"crc_1968"`
		AuthInfo     fixtureCryptoItem `json:"auth_info"`
		KA2TailType1 fixtureCryptoItem `json:"ka2_tail_type1"`
		KA2TailType3 fixtureCryptoItem `json:"ka2_tail_type3"`
	} `json:"crypto_intermediates"`
	Exchanges []fixtureExchange `json:"exchanges"`
	Rejection struct {
		Login struct {
			RequestHex             string `json:"request_hex"`
			RequestLength          int    `json:"request_length"`
			DeclaredResponseHex    string `json:"declared_response_hex"`
			DeclaredResponseLength int    `json:"declared_response_length"`
			WireErrorCode          int    `json:"wire_error_code"`
		} `json:"login"`
	} `json:"rejection"`
}

func loadFixture(t *testing.T) fixtureDoc {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc fixtureDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if doc.ProtocolID != string(ProtocolID) {
		t.Fatalf("fixture protocol id %q does not match %q", doc.ProtocolID, ProtocolID)
	}
	return doc
}

func exchange(t *testing.T, doc fixtureDoc, name string) fixtureExchange {
	t.Helper()
	for _, ex := range doc.Exchanges {
		if ex.Name == name {
			return ex
		}
	}
	t.Fatalf("exchange %q not found in fixture", name)
	return fixtureExchange{}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex %q: %v", s, err)
	}
	return b
}

func mustHex6(t *testing.T, s string) [6]byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 6 {
		t.Fatalf("hex %q is %d bytes, expected 6", s, len(b))
	}
	var arr [6]byte
	copy(arr[:], b)
	return arr
}

func mustHex4(t *testing.T, s string) [4]byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 4 {
		t.Fatalf("hex %q is %d bytes, expected 4", s, len(b))
	}
	var arr [4]byte
	copy(arr[:], b)
	return arr
}

func mustHex2(t *testing.T, s string) [2]byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 2 {
		t.Fatalf("hex %q is %d bytes, expected 2", s, len(b))
	}
	var arr [2]byte
	copy(arr[:], b)
	return arr
}

func mustHex16(t *testing.T, s string) [16]byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 16 {
		t.Fatalf("hex %q is %d bytes, expected 16", s, len(b))
	}
	var arr [16]byte
	copy(arr[:], b)
	return arr
}

func mustHex20(t *testing.T, s string) [20]byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 20 {
		t.Fatalf("hex %q is %d bytes, expected 20", s, len(b))
	}
	var arr [20]byte
	copy(arr[:], b)
	return arr
}

func mustIPv4(t *testing.T, s string) [4]byte {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("parse ipv4 %q: nil", s)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		t.Fatalf("parse ipv4 %q: not v4", s)
	}
	var arr [4]byte
	copy(arr[:], ip4)
	return arr
}

func mustHexByte(t *testing.T, s string) byte {
	t.Helper()
	b := mustHex(t, s)
	if len(b) != 1 {
		t.Fatalf("hex %q is %d bytes, expected 1", s, len(b))
	}
	return b[0]
}

// buildLoginInput constructs the immutable loginInput from the fixture's
// fictional inputs. authExtTail is supplied by the caller because the fixture
// stores it only inside the Login request bytes.
func buildLoginInput(t *testing.T, doc fixtureDoc, authExtTail [2]byte) loginInput {
	t.Helper()
	fi := doc.FictionalInputs
	return loginInput{
		username:           fi.Username,
		password:           fi.Password,
		mac:                mustHex6(t, fi.MacHex),
		clientIPv4:         mustIPv4(t, fi.ClientIPv4),
		hostName:           fi.HostName,
		hostOS:             fi.HostOS,
		primaryDNS:         mustIPv4(t, fi.PrimaryDNS),
		secondaryDNS:       mustIPv4(t, fi.SecondaryDNS),
		dhcpIPv4:           mustIPv4(t, fi.DhcpIPv4),
		osInfo:             mustHex20(t, fi.OSInfoHex),
		controlCheckStatus: mustHexByte(t, fi.ControlCheckStatusHex),
		adapterNum:         mustHexByte(t, fi.AdapterNumHex),
		ipdog:              mustHexByte(t, fi.IPDogHex),
		authVersion:        mustHex2(t, fi.AuthVersionHex),
		authExtTail:        authExtTail,
	}
}
