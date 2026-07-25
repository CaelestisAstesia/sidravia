package environment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Observer publishes network Snapshots until the caller's context is canceled.
type Observer interface {
	Observe(context.Context, chan<- Snapshot) error
}

// NewSystemObserver returns the production network Observer for the current
// platform. On Windows it polls real adapter facts at a fixed interval. On
// other platforms Observe returns ErrUnsupported and publishes no Snapshot.
func NewSystemObserver() Observer {
	return newSystemObserver()
}

// networkCollector reads the current adapter facts and converts them into a
// normalized, deterministically ordered interface slice. The production
// implementation is platform-specific; deterministic package tests inject a
// replacement through this private seam rather than a fake exported API.
type networkCollector func() ([]NetworkInterface, error)

// nowClock returns the collection completion time. Tests inject a deterministic
// clock; production uses time.Now.
type nowClock func() time.Time

type systemObserver struct {
	collect  networkCollector
	now      nowClock
	interval time.Duration
}

// Observe runs a blocking polling loop on the caller's goroutine. It validates
// its inputs, collects immediately, publishes revision 1, then polls every
// interval and publishes a new revision only when normalized adapter facts
// change. Context cancellation unblocks a pending send or timer wait and
// returns nil. A collection or conversion failure stops the loop and returns an
// error that preserves the cause. Observe never starts a goroutine.
func (observer *systemObserver) Observe(ctx context.Context, output chan<- Snapshot) error {
	if ctx == nil {
		return errors.New("network observer: nil context")
	}
	if output == nil {
		return errors.New("network observer: nil output channel")
	}
	if err := ctx.Err(); err != nil {
		return nil
	}

	interfaces, err := observer.collect()
	if err != nil {
		return fmt.Errorf("network observer: collect network facts: %w", err)
	}
	lastFacts := interfaces
	revision := uint64(1)
	if canceled := observer.publish(ctx, output, revision, interfaces); canceled {
		return nil
	}

	ticker := time.NewTicker(observer.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		interfaces, err := observer.collect()
		if err != nil {
			return fmt.Errorf("network observer: collect network facts: %w", err)
		}
		if networkFactsEqual(lastFacts, interfaces) {
			continue
		}
		lastFacts = interfaces
		revision++
		if canceled := observer.publish(ctx, output, revision, interfaces); canceled {
			return nil
		}
	}
}

// publish constructs a Snapshot at the collection completion time and sends it.
// It returns true when the context was canceled while waiting for a blocked
// send, so the caller can return nil without inventing a partial result.
func (observer *systemObserver) publish(
	ctx context.Context,
	output chan<- Snapshot,
	revision uint64,
	interfaces []NetworkInterface,
) bool {
	snapshot := NewSnapshot(revision, observer.now(), interfaces)
	select {
	case output <- snapshot:
		return false
	case <-ctx.Done():
		return true
	}
}

func networkFactsEqual(a, b []NetworkInterface) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !interfaceFactsEqual(a[index], b[index]) {
			return false
		}
	}
	return true
}

// interfaceFactsEqual compares the normalized network facts of two interfaces
// without considering Snapshot-level Revision or ObservedAt, so unchanged
// adapter facts cannot create a new revision merely because linked-list
// enumeration order changed.
func interfaceFactsEqual(a, b NetworkInterface) bool {
	if a.InterfaceID != b.InterfaceID ||
		a.DisplayName != b.DisplayName ||
		a.OperationalState != b.OperationalState ||
		a.PhysicalMedium != b.PhysicalMedium ||
		a.AddressAssignmentMethod != b.AddressAssignmentMethod {
		return false
	}
	if !bytes.Equal(a.HardwareAddress(), b.HardwareAddress()) {
		return false
	}
	if !slices.Equal(a.IPv4AddressAssignments(), b.IPv4AddressAssignments()) {
		return false
	}
	if !slices.Equal(a.DefaultIPv4GatewayAddresses(), b.DefaultIPv4GatewayAddresses()) {
		return false
	}
	if !slices.Equal(a.DNSServerAddresses(), b.DNSServerAddresses()) {
		return false
	}
	leftDHCP, leftHas := a.DHCPServerIPv4Address()
	rightDHCP, rightHas := b.DHCPServerIPv4Address()
	if leftHas != rightHas {
		return false
	}
	return !leftHas || leftDHCP == rightDHCP
}
