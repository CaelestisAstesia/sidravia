package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// RuntimeInfo describes the daemon connection bootstrap.
type RuntimeInfo struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Endpoint       string `json:"endpoint"`
	PID            int    `json:"pid"`
	Token          string `json:"token"`
	ProductVersion string `json:"productVersion"`
	BuildID        string `json:"buildId"`
}

// DecodeRuntimeInfo performs strict decoding and validation.
func DecodeRuntimeInfo(data []byte) (RuntimeInfo, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var info RuntimeInfo
	if err := decoder.Decode(&info); err != nil {
		return RuntimeInfo{}, fmt.Errorf("runtime info: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return RuntimeInfo{}, fmt.Errorf("runtime info: trailing data")
	} else if err != io.EOF {
		return RuntimeInfo{}, fmt.Errorf("runtime info: trailing garbage")
	}

	if info.SchemaVersion != 1 {
		return RuntimeInfo{}, fmt.Errorf("runtime info: unsupported schema version %d", info.SchemaVersion)
	}
	if err := validateEndpoint(info.Endpoint); err != nil {
		return RuntimeInfo{}, fmt.Errorf("runtime info: %w", err)
	}
	if info.PID <= 0 {
		return RuntimeInfo{}, fmt.Errorf("runtime info: pid must be positive")
	}
	if err := validateToken(info.Token); err != nil {
		return RuntimeInfo{}, fmt.Errorf("runtime info: %w", err)
	}
	if info.ProductVersion == "" {
		return RuntimeInfo{}, fmt.Errorf("runtime info: product version is empty")
	}
	if info.BuildID == "" {
		return RuntimeInfo{}, fmt.Errorf("runtime info: build id is empty")
	}
	return info, nil
}

// EncodeRuntimeInfo encodes RuntimeInfo as JSON.
func EncodeRuntimeInfo(info RuntimeInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("encode runtime info: %w", err)
	}
	return data, nil
}

// SafeDisplay returns a human-readable representation without secrets.
func (info RuntimeInfo) SafeDisplay() string {
	return fmt.Sprintf("sidraviad %s (%s) pid=%d status=running", info.ProductVersion, info.BuildID, info.PID)
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "ws" {
		return fmt.Errorf("endpoint scheme must be ws")
	}
	if u.Path != "/ipc" {
		return fmt.Errorf("endpoint path must be /ipc")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return fmt.Errorf("endpoint host:port: %w", err)
	}
	if !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("endpoint must be loopback")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return fmt.Errorf("endpoint port invalid")
	}
	return nil
}

func validateToken(token string) error {
	if len(token) != 64 {
		return fmt.Errorf("token must be 64 hex characters")
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("token must be lowercase hex")
		}
	}
	return nil
}
