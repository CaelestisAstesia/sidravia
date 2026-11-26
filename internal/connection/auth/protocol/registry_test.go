package protocol

import (
	profilevalue "sidravia/internal/connection/profile"
	"testing"
)

type registryTestFactory struct {
	protocolID profilevalue.ProtocolID
}

type pointerRegistryTestFactory struct{}

func (*pointerRegistryTestFactory) ProtocolID() profilevalue.ProtocolID {
	return "pointer"
}

func (*pointerRegistryTestFactory) ValidateInstitutionProtocolConfiguration(
	profilevalue.ProtocolConfiguration,
) error {
	return nil
}

func (*pointerRegistryTestFactory) ValidateProtocolContextOverride(
	profilevalue.ProtocolOverride,
) error {
	return nil
}

func (*pointerRegistryTestFactory) CreateAuthenticationProtocolRun(
	AuthenticationProtocolRunCreationInputs,
) (AuthenticationProtocolRun, error) {
	return nil, nil
}

func (factory registryTestFactory) ProtocolID() profilevalue.ProtocolID {
	return factory.protocolID
}

func (registryTestFactory) ValidateInstitutionProtocolConfiguration(
	profilevalue.ProtocolConfiguration,
) error {
	return nil
}

func (registryTestFactory) ValidateProtocolContextOverride(
	profilevalue.ProtocolOverride,
) error {
	return nil
}

func (registryTestFactory) CreateAuthenticationProtocolRun(
	AuthenticationProtocolRunCreationInputs,
) (AuthenticationProtocolRun, error) {
	return nil, nil
}

func TestNewAuthenticationProtocolRegistryRejectsEmptyProtocolID(t *testing.T) {
	_, err := NewAuthenticationProtocolRegistry(registryTestFactory{})
	if err == nil {
		t.Fatal("expected empty protocol ID error")
	}
}

func TestNewAuthenticationProtocolRegistryRejectsTypedNilFactory(t *testing.T) {
	var factory *pointerRegistryTestFactory

	_, err := NewAuthenticationProtocolRegistry(factory)
	if err == nil {
		t.Fatal("expected typed nil factory error")
	}
}

func TestNewAuthenticationProtocolRegistryRejectsDuplicateProtocolID(t *testing.T) {
	_, err := NewAuthenticationProtocolRegistry(
		registryTestFactory{protocolID: "test"},
		registryTestFactory{protocolID: "test"},
	)
	if err == nil {
		t.Fatal("expected duplicate protocol ID error")
	}
}

func TestAuthenticationProtocolRegistryReturnsRegisteredFactory(t *testing.T) {
	registry, err := NewAuthenticationProtocolRegistry(
		registryTestFactory{protocolID: "test"},
	)
	if err != nil {
		t.Fatal(err)
	}

	factory, err := registry.GetFactory("test")
	if err != nil {
		t.Fatal(err)
	}
	if factory.ProtocolID() != "test" {
		t.Fatalf("unexpected protocol ID %q", factory.ProtocolID())
	}
}

func TestAuthenticationProtocolRegistryRejectsUnknownProtocolID(t *testing.T) {
	registry, err := NewAuthenticationProtocolRegistry()
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.GetFactory("missing")
	if err == nil {
		t.Fatal("expected unknown protocol ID error")
	}
}

func (*pointerRegistryTestFactory) ValidateUsername(string) error { return nil }
func (registryTestFactory) ValidateUsername(string) error         { return nil }
