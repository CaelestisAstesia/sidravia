package session

import (
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

type recoveryFixture struct {
	session   *AuthenticationSession
	factory   *controlledFactory
	scheduler *manualRetryScheduler
}

func newRecoveryFixture(t *testing.T, reconnect, hold, explicit bool) *recoveryFixture {
	t.Helper()
	factory := &controlledFactory{holdCancellation: hold}
	scheduler := &manualRetryScheduler{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.AutoReconnect = reconnect
	if explicit {
		definition.Configuration.NetworkBindingPolicy = NetworkBindingPolicy{Mode: ExplicitInterfaceAndLocalIPv4, InterfaceID: "a", LocalIPv4Address: networkInterface(t, "a", environment.PhysicalMediumWired, "192.0.2.1", 24).IPv4AddressAssignments()[0].Address}
	}
	s, err := NewAuthenticationSession(definition, MaintainAuthentication, Dependencies{
		Now: scheduler.bindClock(func() time.Time { return time.Unix(1000, 0) }), RetryPolicy: NewDefaultRetryPolicy(), RetryScheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	t.Cleanup(func() {
		factory.mu.Lock()
		runs := append([]*controlledRun(nil), factory.runs...)
		factory.mu.Unlock()
		for _, run := range runs {
			run.unblock(nil)
		}
		select {
		case <-s.done:
			return
		default:
		}
		shutdownTestSession(t, s)
	})
	return &recoveryFixture{s, factory, scheduler}
}

func recoveryInterface(t *testing.T, id string) environment.NetworkInterface {
	t.Helper()
	return networkInterface(t, environment.InterfaceID(id), environment.PhysicalMediumWired, "192.0.2.1", 24)
}

func (f *recoveryFixture) apply(t *testing.T, revision uint64, interfaces ...environment.NetworkInterface) Snapshot {
	t.Helper()
	got, err := f.session.ApplySystemNetworkSnapshot(testContext(t), snapshot(t, revision, interfaces...))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f *recoveryFixture) advance(d time.Duration) {
	at := f.scheduler.clock().Add(d)
	f.scheduler.mu.Lock()
	f.scheduler.floor = at
	f.scheduler.mu.Unlock()
}

func (f *recoveryFixture) fail(t *testing.T, index int, recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation) Snapshot {
	t.Helper()
	waitForFactoryRun(t, testContext(t), f.factory, index).unblock(&protocol.AuthenticationProtocolRunFailure{Code: "test_transient", Description: "Transient failure.", HandlingRecommendation: recommendation})
	want := WaitingBeforeRetry
	if recommendation == protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		want = BlockedByError
	}
	return waitForState(t, testContext(t), f.session, want)
}

func (f *recoveryFixture) run(t *testing.T, index int, id string) *controlledRun {
	t.Helper()
	run := waitForFactoryRun(t, testContext(t), f.factory, index)
	if got := f.factory.creationInputs()[index].SelectedSystemNetworkBinding.NetworkInterface().InterfaceID; got != environment.InterfaceID(id) {
		t.Fatalf("run %d binding=%s want=%s", index, got, id)
	}
	return run
}

func (f *recoveryFixture) fire(t *testing.T) { t.Helper(); fireAutomaticWake(t, f.session) }

func TestAutomaticRecoveryKeepsCurrentWhileNewCandidateAppears(t *testing.T) {
	for _, establish := range []bool{false, true} {
		t.Run(map[bool]string{false: "authenticating", true: "authenticated"}[establish], func(t *testing.T) {
			f := newRecoveryFixture(t, true, false, false)
			f.apply(t, 1, recoveryInterface(t, "a"))
			if establish {
				if err := f.run(t, 0, "a").establish(testContext(t)); err != nil {
					t.Fatal(err)
				}
				waitForState(t, testContext(t), f.session, Authenticated)
			}
			f.apply(t, 2, recoveryInterface(t, "a"), recoveryInterface(t, "b"))
			f.advance(10 * time.Second)
			got := f.apply(t, 3, recoveryInterface(t, "a"), recoveryInterface(t, "b"))
			if len(f.factory.creationInputs()) != 1 || got.SelectedNetworkBinding.InterfaceID != "a" || f.scheduler.count() != 0 {
				t.Fatalf("new interface preempted: %#v", got)
			}
		})
	}
}

func TestAutomaticRecoveryStabilizesWithoutNewRevisionAndRejectsEarlyTimer(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	f.apply(t, 1)
	waiting := f.apply(t, 2, recoveryInterface(t, "a"))
	if waiting.State != WaitingForNetwork || waiting.NextRetryAt != nil || len(f.factory.creationInputs()) != 0 {
		t.Fatalf("stability leaked into retry contract: %#v", waiting)
	}
	// Invoke the actual callback without advancing the clock: an early event must
	// be rechecked rather than treated as evidence that its deadline has elapsed.
	f.scheduler.mu.Lock()
	early := f.scheduler.callbacks[0]
	f.scheduler.mu.Unlock()
	early()
	_ = sessionSnapshot(t, testContext(t), f.session)
	if len(f.factory.creationInputs()) != 0 {
		t.Fatal("early wake started authentication")
	}
	f.fire(t)
	f.run(t, 0, "a")
	early()
	_ = sessionSnapshot(t, testContext(t), f.session)
	if len(f.factory.creationInputs()) != 1 {
		t.Fatal("duplicate wake started a run")
	}
}

func TestAutomaticRecoveryRoundBudgetAndFairness(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	candidates := []environment.NetworkInterface{recoveryInterface(t, "a"), recoveryInterface(t, "b"), recoveryInterface(t, "c")}
	f.apply(t, 1, candidates...)
	for index, id := range []string{"a", "a", "b", "b"} {
		f.run(t, index, id)
		waiting := f.fail(t, index, protocol.RetryAfterStandardDelay)
		expected := 5 * time.Second
		if index == 3 {
			expected = bindingRoundDelay
		}
		if waiting.NextRetryAt == nil || waiting.NextRetryAt.Sub(f.scheduler.clock()) != expected {
			t.Fatalf("run %d deadline=%v", index, waiting.NextRetryAt)
		}
		if index == 3 {
			after := f.apply(t, 2, candidates...)
			if after.NextRetryAt == nil || !after.NextRetryAt.Equal(*waiting.NextRetryAt) {
				t.Fatal("snapshot refreshed round deadline")
			}
		}
		f.fire(t)
	}
	f.run(t, 4, "c")
}

func TestAutomaticRecoveryCandidateDisappearanceStartsRoundBackoff(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	a, b := recoveryInterface(t, "a"), recoveryInterface(t, "b")
	f.apply(t, 1, a, b)
	f.fail(t, 0, protocol.RetryAfterStandardDelay)
	f.fire(t)
	f.fail(t, 1, protocol.RetryAfterStandardDelay)
	got := f.apply(t, 2, a) // B vanished, no Run completion accompanies this event.
	if got.NextRetryAt == nil || got.NextRetryAt.Sub(f.scheduler.clock()) != bindingRoundDelay {
		t.Fatalf("no round deadline after candidate loss: %#v", got)
	}
	deadline := *got.NextRetryAt
	f.advance(time.Second)
	got = f.apply(t, 3, a)
	if got.NextRetryAt == nil || !got.NextRetryAt.Equal(deadline) {
		t.Fatal("repeat facts extended deadline")
	}
	f.fire(t) // No additional network revision is required.
	f.run(t, 2, "a")
}

func changedMAC(t *testing.T, iface environment.NetworkInterface, value byte) environment.NetworkInterface {
	t.Helper()
	facts := environment.NetworkInterfaceFacts{
		InterfaceID: iface.InterfaceID, OperationalState: environment.OperationalStateUp, PhysicalMedium: iface.PhysicalMedium,
		HardwareBacked: true, PhysicalConnectorPresent: true, HardwareAddress: []byte{0, 1, 2, 3, 4, value},
		IPv4AddressAssignments: iface.IPv4AddressAssignments(),
	}
	return newNetworkInterface(t, facts)
}

func fillRoundWithInputChanges(t *testing.T, f *recoveryFixture) environment.NetworkInterface {
	t.Helper()
	iface := recoveryInterface(t, "a")
	f.apply(t, 1, iface)
	for revision := uint64(2); revision <= 4; revision++ {
		old := f.run(t, int(revision)-2, "a")
		iface = changedMAC(t, iface, byte(revision))
		f.apply(t, revision, iface)
		if old.waitForCancellation(testContext(t)) == nil {
			t.Fatal("missing cancellation")
		}
		old.unblock(nil)
		f.run(t, int(revision)-1, "a")
	}
	return iface
}

func TestAutomaticRecoveryRetainsRelevantInputPermissionAcrossBackoff(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_relevant_change", true: "changed_input"}[changed], func(t *testing.T) {
			f := newRecoveryFixture(t, true, true, false)
			iface := fillRoundWithInputChanges(t, f)
			f.fail(t, 3, protocol.BlockUntilExplicitRestartOrRelevantInputChange)
			if changed {
				iface = changedMAC(t, iface, 9)
			}
			got := f.apply(t, 5, iface)
			if !changed {
				if got.State != BlockedByError || got.NextRetryAt != nil {
					t.Fatalf("timer unlocked failure: %#v", got)
				}
				f.advance(time.Minute)
				f.apply(t, 6, iface)
				if len(f.factory.creationInputs()) != 4 {
					t.Fatal("unchanged input restarted blocking failure")
				}
				return
			}
			if got.State != WaitingBeforeRetry || got.NextRetryAt == nil {
				t.Fatalf("qualification lost: %#v", got)
			}
			if len(f.factory.creationInputs()) != 4 {
				t.Fatal("input change bypassed launch cap")
			}
			f.fire(t)
			f.run(t, 4, "a")
		})
	}
}

func TestAutomaticRecoverySuccessDurationFreezesBeforeCleanup(t *testing.T) {
	for _, online := range []time.Duration{29 * time.Second, 31 * time.Second} {
		t.Run(online.String(), func(t *testing.T) {
			f := newRecoveryFixture(t, true, true, false)
			iface := fillRoundWithInputChanges(t, f)
			run := f.run(t, 3, "a")
			if err := run.establish(testContext(t)); err != nil {
				t.Fatal(err)
			}
			waitForState(t, testContext(t), f.session, Authenticated)
			f.advance(online)
			iface = changedMAC(t, iface, 8)
			f.apply(t, 5, iface)
			if run.waitForCancellation(testContext(t)) == nil {
				t.Fatal("not canceled")
			}
			f.advance(2 * time.Second) // cleanup must not turn 29 seconds into stable success.
			run.unblock(nil)
			if online < bindingStableSuccess {
				got := waitForState(t, testContext(t), f.session, WaitingBeforeRetry)
				if len(f.factory.creationInputs()) != 4 || got.NextRetryAt == nil {
					t.Fatal("cleanup replenished the budget")
				}
				f.fire(t)
			}
			f.run(t, 4, "a")
		})
	}
}

func TestAutomaticRecoveryNoCandidateDoesNotSpinOrRefreshBackoff(t *testing.T) {
	f := newRecoveryFixture(t, true, true, false)
	iface := fillRoundWithInputChanges(t, f)
	f.fail(t, 3, protocol.RetryAfterStandardDelay)
	f.apply(t, 5)
	count := f.scheduler.count()
	f.advance(time.Minute)
	got := f.apply(t, 6)
	if f.scheduler.count() != count || got.NextRetryAt != nil {
		t.Fatal("no-candidate state spun a timer")
	}
	f.apply(t, 7, iface)
	if len(f.factory.creationInputs()) != 4 {
		t.Fatal("returning candidate skipped stability")
	}
	f.fire(t)
	f.run(t, 4, "a")
}

func TestAutomaticRecoveryPendingQualificationIsCanceledByUserIntent(t *testing.T) {
	for _, operation := range []string{"suspend", "shutdown"} {
		t.Run(operation, func(t *testing.T) {
			f := newRecoveryFixture(t, true, true, false)
			iface := fillRoundWithInputChanges(t, f)
			f.fail(t, 3, protocol.BlockUntilExplicitRestartOrRelevantInputChange)
			f.apply(t, 5, changedMAC(t, iface, 9))
			callback := f.scheduler.callback(f.scheduler.count() - 1)
			if operation == "suspend" {
				if _, err := f.session.Suspend(testContext(t)); err != nil {
					t.Fatal(err)
				}
				waitForState(t, testContext(t), f.session, Suspended)
			} else {
				if err := f.session.Shutdown(testContext(t)); err != nil {
					t.Fatal(err)
				}
			}
			callback()
			if operation == "suspend" {
				f.apply(t, 6, changedMAC(t, iface, 10))
			}
			if len(f.factory.creationInputs()) != 4 {
				t.Fatal("old qualification ignored user intent")
			}
		})
	}
}

func TestAutomaticRecoveryExplicitBindingKeepsExistingRetryPolicy(t *testing.T) {
	f := newRecoveryFixture(t, true, false, true)
	f.apply(t, 1, recoveryInterface(t, "a"), recoveryInterface(t, "b"))
	for i := 0; i < 6; i++ {
		got := f.fail(t, i, protocol.RetryAfterStandardDelay)
		if got.NextRetryAt == nil || got.NextRetryAt.Sub(f.scheduler.clock()) != 5*time.Second {
			t.Fatal("automatic round cap affected explicit binding")
		}
		f.fire(t)
		f.run(t, i+1, "a")
	}
}

func TestAutomaticRecoveryDisabledDoesNotRebuildAfterNetworkCancellation(t *testing.T) {
	f := newRecoveryFixture(t, false, true, false)
	a, b := recoveryInterface(t, "a"), recoveryInterface(t, "b")
	f.apply(t, 1, a, b)
	run := f.run(t, 0, "a")
	f.apply(t, 2, b)
	if run.waitForCancellation(testContext(t)) == nil {
		t.Fatal("not canceled")
	}
	run.unblock(nil)
	got := waitForState(t, testContext(t), f.session, BlockedByError)
	if got.StateReason.Code != StateReasonCodeAutomaticReconnectDisabled {
		t.Fatalf("wrong block: %#v", got)
	}
	f.apply(t, 3, b)
	if len(f.factory.creationInputs()) != 1 || f.scheduler.count() != 0 {
		t.Fatal("disabled reconnect still launched")
	}
	if _, err := f.session.Restart(testContext(t)); err != nil {
		t.Fatal(err)
	}
	f.run(t, 1, "b")
}

func TestAutomaticRecoveryBusyRoundKeepsCurrentBinding(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	f.apply(t, 1, recoveryInterface(t, "a"), recoveryInterface(t, "b"))
	for i := 0; i < 4; i++ {
		f.run(t, i, "a")
		f.fail(t, i, protocol.RetryAfterExtendedDelay)
		f.fire(t)
	}
	f.run(t, 4, "a")
}

func TestAutomaticRecoveryOneInterfaceDoesNotScanAlternateAddressesAcrossRounds(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	a := recoveryInterface(t, "a")
	b := networkInterface(t, "a", environment.PhysicalMediumWired, "192.0.2.2", 24)
	assignments := append(a.IPv4AddressAssignments(), b.IPv4AddressAssignments()...)
	multi := newNetworkInterface(t, environment.NetworkInterfaceFacts{InterfaceID: "a", OperationalState: environment.OperationalStateUp, PhysicalMedium: environment.PhysicalMediumWired, HardwareBacked: true, PhysicalConnectorPresent: true, IPv4AddressAssignments: assignments})
	f.apply(t, 1, multi)
	for i := 0; i < 2; i++ {
		f.run(t, i, "a")
		f.fail(t, i, protocol.RetryAfterStandardDelay)
		f.fire(t)
	}
	f.run(t, 2, "a")
	got := f.factory.creationInputs()[2].SelectedSystemNetworkBinding.LocalIPv4AddressAssignment().Address
	if got != assignments[0].Address {
		t.Fatalf("cross-round scanned alternate address: got %v want retained %v", got, assignments[0].Address)
	}
}

func TestAutomaticRecoveryMixedExtendedAndBoundedRoundKeepsBinding(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	f.apply(t, 1, recoveryInterface(t, "a"), recoveryInterface(t, "b"))
	for i, recommendation := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{
		protocol.RetryAfterExtendedDelay, protocol.RetryAfterBoundedRecovery, protocol.RetryAfterExtendedDelay, protocol.RetryAfterBoundedRecovery,
	} {
		f.run(t, i, "a")
		f.fail(t, i, recommendation)
		f.fire(t)
	}
	f.run(t, 4, "a")
}

func TestAutomaticRecoveryRotationRetainsAddressOnPresentInterfaces(t *testing.T) {
	f := newRecoveryFixture(t, true, false, false)
	a := networkInterface(t, "a", environment.PhysicalMediumWired, "192.0.2.2", 24)
	b, c := recoveryInterface(t, "b"), recoveryInterface(t, "c")
	f.apply(t, 1, a, b, c)
	multi := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID: "a", OperationalState: environment.OperationalStateUp, PhysicalMedium: environment.PhysicalMediumWired,
		HardwareBacked: true, PhysicalConnectorPresent: true, AddressAssignmentMethod: a.AddressAssignmentMethod,
		IPv4AddressAssignments: append(a.IPv4AddressAssignments(), recoveryInterface(t, "a").IPv4AddressAssignments()...),
	})
	f.apply(t, 2, multi, b, c)
	f.advance(bindingStabilityDelay)
	for i, id := range []string{"a", "a", "b", "b", "c", "c"} {
		f.run(t, i, id)
		f.fail(t, i, protocol.RetryAfterStandardDelay)
		f.fire(t)
	}
	f.run(t, 6, "a")
	if got := f.factory.creationInputs()[6].SelectedSystemNetworkBinding.LocalIPv4AddressAssignment().Address; got != a.IPv4AddressAssignments()[0].Address {
		t.Fatalf("rotation scanned another address: %v", got)
	}
}

func TestAutomaticRecoveryRestoresRetryReasonAfterNetworkStabilizes(t *testing.T) {
	for _, recommendation := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{
		protocol.RetryAfterStandardDelay, protocol.RetryAfterExtendedDelay, protocol.RetryAfterBoundedRecovery,
	} {
		t.Run(string(recommendation), func(t *testing.T) {
			f := newRecoveryFixture(t, true, false, false)
			a := recoveryInterface(t, "a")
			f.apply(t, 1, a)
			before := f.fail(t, 0, recommendation)
			if before.StateReason == nil || before.NextRetryAt == nil || before.LastAuthenticationFailure == nil {
				t.Fatalf("missing original retry facts: %#v", before)
			}
			absent := f.apply(t, 2)
			if absent.State != WaitingForNetwork || absent.StateReason == nil || absent.StateReason.Code != StateReasonCodeNetworkUnavailable || absent.NextRetryAt != nil {
				t.Fatalf("incorrect absent-network projection: %#v", absent)
			}
			stabilizing := f.apply(t, 3, a)
			if stabilizing.State != WaitingForNetwork || stabilizing.NextRetryAt != nil {
				t.Fatalf("stabilization published retry time: %#v", stabilizing)
			}
			f.advance(bindingStabilityDelay)
			retry := f.apply(t, 4, a)
			if retry.State != WaitingBeforeRetry || retry.StateReason == nil || *retry.StateReason != *before.StateReason {
				t.Fatalf("retry did not restore failure reason: before=%#v after=%#v", before.StateReason, retry.StateReason)
			}
			if retry.NextRetryAt == nil || !retry.NextRetryAt.Equal(*before.NextRetryAt) || retry.LastAuthenticationFailure == nil || *retry.LastAuthenticationFailure != *before.LastAuthenticationFailure {
				t.Fatalf("retry changed deadline or failure: before=%#v after=%#v", before, retry)
			}
			if retry.SelectedNetworkBinding == nil || retry.SelectedNetworkBinding.InterfaceID != "a" || len(f.factory.creationInputs()) != 1 {
				t.Fatal("restoring reason started an early run or lost binding")
			}
			repeated := f.apply(t, 5, a)
			if repeated.StateReason == nil || *repeated.StateReason != *before.StateReason || repeated.NextRetryAt == nil || !repeated.NextRetryAt.Equal(*before.NextRetryAt) {
				t.Fatal("repeat facts changed retry projection")
			}
			f.fire(t)
			f.run(t, 1, "a")
		})
	}
}

func TestAutomaticRecoveryNetworkChurnBackoffDoesNotInventFailureReason(t *testing.T) {
	f := newRecoveryFixture(t, true, true, false)
	iface := fillRoundWithInputChanges(t, f)
	run := f.run(t, 3, "a")
	f.apply(t, 5)
	if run.waitForCancellation(testContext(t)) == nil {
		t.Fatal("missing network cancellation")
	}
	run.unblock(nil)
	f.apply(t, 6, iface)
	f.advance(bindingStabilityDelay)
	f.apply(t, 7, iface)
	retry := waitForState(t, testContext(t), f.session, WaitingBeforeRetry)
	if retry.StateReason != nil || retry.LastAuthenticationFailure != nil || retry.NextRetryAt == nil {
		t.Fatalf("network churn invented failure: %#v", retry)
	}
	if len(f.factory.creationInputs()) != 4 {
		t.Fatal("network churn bypassed backoff")
	}
	f.fire(t)
	f.run(t, 4, "a")
}
