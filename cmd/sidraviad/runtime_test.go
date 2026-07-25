package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
	"sidravia/internal/ipc/contract"
)

type inMemoryStore struct {
	mu     sync.Mutex
	data   map[string][]byte
	opened map[string]bool
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		data:   make(map[string][]byte),
		opened: make(map[string]bool),
	}
}

func (s *inMemoryStore) Read(_ context.Context, path string, _ int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened[path] = true
	data, ok := s.data[path]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, true, nil
}

func (s *inMemoryStore) Replace(_ context.Context, path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	s.data[path] = cp
	return nil
}

func (s *inMemoryStore) wasOpened(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened[path]
}

type fakeObserver struct {
	mu        sync.Mutex
	snapshots []environment.Snapshot
	done      chan struct{}
	started   chan struct{}
	err       error
	block     bool
}

func newFakeObserver() *fakeObserver {
	return &fakeObserver{
		done:    make(chan struct{}),
		started: make(chan struct{}),
		block:   true,
	}
}

func (o *fakeObserver) Observe(ctx context.Context, output chan<- environment.Snapshot) error {
	close(o.started)
	if o.err != nil {

		return o.err
	}
	o.mu.Lock()
	snapshots := make([]environment.Snapshot, len(o.snapshots))
	copy(snapshots, o.snapshots)
	o.mu.Unlock()
	for _, s := range snapshots {
		select {
		case output <- s:
		case <-ctx.Done():
			return nil
		}
	}
	if o.block {
		<-ctx.Done()
	}
	return nil
}

func (o *fakeObserver) addSnapshot(s environment.Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.snapshots = append(o.snapshots, s)
}

type fakeHostRunner struct {
	mu      sync.Mutex
	called  bool
	cfg     *host.Config
	done    chan error
	started chan struct{}
}

func newFakeHostRunner() *fakeHostRunner {
	return &fakeHostRunner{
		done:    make(chan error, 1),
		started: make(chan struct{}),
	}
}

func (r *fakeHostRunner) run(ctx context.Context, cfg host.Config) error {
	r.mu.Lock()
	r.called = true
	r.cfg = &cfg
	r.mu.Unlock()
	close(r.started)
	select {
	case err := <-r.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *fakeHostRunner) wasCalled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.called
}

func (r *fakeHostRunner) signalDone(err error) {
	r.done <- err
}

func testPaths(t *testing.T) defaultPaths {
	t.Helper()
	dir := t.TempDir()
	return defaultPaths{
		profiles:       filepath.Join(dir, "institution-profiles"),
		configurations: filepath.Join(dir, "configurations.json"),
		credentials:    filepath.Join(dir, "credentials.json"),
		runtimeInfo:    filepath.Join(dir, "runtime.json"),
	}
}

func writeTestProfile(t *testing.T, dir string, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write profile %s: %v", name, err)
	}
}

func validJLUProfile(t *testing.T) []byte {
	t.Helper()
	return []byte(`{
  "schemaVersion": 1,
  "institutionProfileId": "jlu",
  "displayName": "Jilin University",
  "authenticationProtocolId": "drcom-5.2.0-d",
  "institutionProtocolConfiguration": {
    "serverAddress": "127.0.0.1",
    "serverPort": 61440,
    "authVersionHex": "0a00",
    "keepAliveVersionHex": "0b00",
    "controlCheckStatusHex": "00",
    "ipdogHex": "00",
    "adapterNumberHex": "00",
    "osInfoHex": "0000000000000000000000000000000000000000",
    "challengePaddingHex": "000000000000000000000000000000",
    "challengeTimeout": "5s",
    "loginTimeout": "5s",
    "keepaliveTimeout": "5s",
    "logoutTimeout": "5s",
    "heartbeatInterval": "30s",
    "busyMaxAttempts": 5,
    "busyBackoffMin": "100ms",
    "busyBackoffMax": "5s"
  }
}`)
}

func testHostInfo() environment.SystemHostInformation {
	return environment.SystemHostInformation{
		HostName:               "test-pc",
		OperatingSystemFamily:  "Windows",
		OperatingSystemRelease: "10.0.19045",
		MachineArchitecture:    "amd64",
	}
}

func TestComposeObjectGraphWithValidProfile(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}
	defer func() {
		rt.sup.Close()
		rt.sup.Wait()
	}()

	ctx := context.Background()
	result, rpcErr := rt.handler(ctx, contract.MethodDaemonStatus, nil)
	if rpcErr != nil {
		t.Fatalf("daemon.status error: %v", rpcErr)
	}
	var status contract.StatusResult
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.ProductVersion != "1.0.0-test" {
		t.Fatalf("status.ProductVersion = %q, want %q", status.ProductVersion, "1.0.0-test")
	}
	if status.BuildID != "abc1234" {
		t.Fatalf("status.BuildID = %q, want %q", status.BuildID, "abc1234")
	}
	if status.Status != "running" {
		t.Fatalf("status.Status = %q, want running", status.Status)
	}

	if !store.wasOpened(paths.configurations) {
		t.Fatal("store was not opened at configurations path")
	}
	if !store.wasOpened(paths.credentials) {
		t.Fatal("store was not opened at credentials path")
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called during construction")
	}
}

func TestSessionStartOneShotThroughComposedHandler(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}
	defer func() {
		rt.sup.Close()
		rt.sup.Wait()
	}()

	startPayload, _ := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:              "Test Session",
		InstitutionProfileID:     "jlu",
		Username:                 "testuser",
		Password:                 "testpass",
		NetworkBindingPolicyMode: "automatically_select_latest_available",
		ProtocolContextOverride:  json.RawMessage(`{}`),
	})

	ctx := context.Background()
	result, rpcErr := rt.handler(ctx, contract.MethodSessionStartOneShot, startPayload)
	if rpcErr != nil {
		t.Fatalf("session.startOneShot error: %v", rpcErr)
	}

	var sessionResult contract.SessionResult
	if err := json.Unmarshal(result, &sessionResult); err != nil {
		t.Fatalf("unmarshal session result: %v", err)
	}

	if sessionResult.State != "waiting_for_network" {
		t.Fatalf("session.State = %q, want waiting_for_network", sessionResult.State)
	}

	if sessionResult.AuthenticationProtocolID != "drcom-5.2.0-d" {
		t.Fatalf("session.AuthenticationProtocolID = %q, want drcom-5.2.0-d", sessionResult.AuthenticationProtocolID)
	}

	if sessionResult.AccountLabel == "" {
		t.Fatal("session.AccountLabel is empty")
	}
	if sessionResult.AccountLabel == "testuser" {
		t.Fatal("session.AccountLabel contains raw username")
	}

	stopPayload, _ := json.Marshal(contract.SessionStopPayload{
		SessionID: sessionResult.AuthenticationSessionID,
	})
	_, stopErr := rt.handler(ctx, contract.MethodSessionStop, stopPayload)
	if stopErr != nil {
		t.Fatalf("session.stop error: %v", stopErr)
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called during IPC session test")
	}
}

func TestCompositionFailsOnInvalidProfile(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "bad.json", []byte(`{"schemaVersion":1}`))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	_, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err == nil {
		t.Fatal("composeObjectGraph() expected error for invalid profile")
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called despite invalid profile")
	}
}

func TestRuntimeLifecycleHostReturnsNilOnNormalShutdown(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	hostRunner.signalDone(nil)

	ctx := context.Background()
	if err := rt.run(ctx); err != nil {
		t.Fatalf("run() error = %v, want nil on normal shutdown", err)
	}
}

func TestRuntimeLifecycleHostFailureCancelsOthers(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	hostErr := errors.New("host failure")
	hostRunner.signalDone(hostErr)

	ctx := context.Background()
	err = rt.run(ctx)
	if err == nil {
		t.Fatal("run() expected error for host failure")
	}
}

func TestRuntimeLifecycleObserverFailureCancelsHost(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	observer.err = errors.New("observer failure")
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	ctx := context.Background()
	err = rt.run(ctx)
	if err == nil {
		t.Fatal("run() expected error for observer failure")
	}
	if !hostRunner.wasCalled() {
		t.Fatal("host runner was not called")
	}
}

func TestRuntimeLifecycleSnapshotDeliveryFailureCancelsOthers(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	observer.block = false

	ctx := context.Background()
	err = rt.run(ctx)
	if err != nil {
		t.Fatalf("run() unexpected error: %v", err)
	}
}

func TestRuntimeLifecycleAlreadyCanceledContextStartsNothing(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := rt.run(ctx); err != nil {
		t.Fatalf("run() with cancelled context error = %v, want nil", err)
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called with cancelled context")
	}
}

func TestRuntimeLifecycleNilContextStillShutsDown(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	err = rt.run(nil)
	if err == nil {
		t.Fatal("run(nil) expected error")
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called with nil context")
	}
}

func TestRuntimeLifecycleSnapshotReachesSinkBeforeHostReturn(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))

	store := newInMemoryStore()
	hostInfo := testHostInfo()

	observer := newFakeObserver()
	observer.block = false
	observer.addSnapshot(environment.NewSnapshot(1, time.Now(), nil))

	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		hostInfo,
		observer,
		hostRunner.run,
		"test-token",
		"1.0.0-test",
		"abc1234",
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}

	hostRunner.signalDone(nil)

	ctx := context.Background()
	if err := rt.run(ctx); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}
