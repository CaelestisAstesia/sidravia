package supported

import (
	"testing"

	"sidravia/internal/connection/auth/d520"
)

func TestCheckDriversRequiresExactlyTheReleasedProtocols(t *testing.T) {
	if err := CheckDrivers(d520.ProtocolID); err != nil {
		t.Fatal(err)
	}
	if err := CheckDrivers(); err == nil {
		t.Fatal("accepted a missing runtime driver")
	}
	if err := CheckDrivers(d520.ProtocolID, d520.ProtocolID); err == nil {
		t.Fatal("accepted duplicate runtime drivers")
	}
	if err := CheckDrivers(d520.ProtocolID, "unreleased"); err == nil {
		t.Fatal("accepted a driver with no shipped description")
	}
}
