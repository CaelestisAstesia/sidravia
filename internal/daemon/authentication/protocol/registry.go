package protocol

import (
	"fmt"
	"reflect"
)

type AuthenticationProtocolRegistry struct {
	factories map[AuthenticationProtocolID]AuthenticationProtocolFactory
}

func NewAuthenticationProtocolRegistry(
	factories ...AuthenticationProtocolFactory,
) (*AuthenticationProtocolRegistry, error) {
	factoriesByID := make(map[AuthenticationProtocolID]AuthenticationProtocolFactory, len(factories))
	for _, factory := range factories {
		if isNilAuthenticationProtocolFactory(factory) || factory.ProtocolID() == "" {
			return nil, fmt.Errorf("authentication protocol factory has empty ID")
		}
		if _, exists := factoriesByID[factory.ProtocolID()]; exists {
			return nil, fmt.Errorf("duplicate authentication protocol ID %q", factory.ProtocolID())
		}
		factoriesByID[factory.ProtocolID()] = factory
	}
	return &AuthenticationProtocolRegistry{factories: factoriesByID}, nil
}

func isNilAuthenticationProtocolFactory(factory AuthenticationProtocolFactory) bool {
	if factory == nil {
		return true
	}
	value := reflect.ValueOf(factory)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (registry *AuthenticationProtocolRegistry) GetFactory(
	protocolID AuthenticationProtocolID,
) (AuthenticationProtocolFactory, error) {
	factory, exists := registry.factories[protocolID]
	if !exists {
		return nil, fmt.Errorf("authentication protocol %q is not registered", protocolID)
	}
	return factory, nil
}
