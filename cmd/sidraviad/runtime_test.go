package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/desktopowner"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func bufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

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

func (s *inMemoryStore) ReplaceSensitive(ctx context.Context, path string, data []byte, _ bool) error {
	return s.Replace(ctx, path, data)
}

func (s *inMemoryStore) ProtectionStatus() jsonfile.ProtectionStatus {
	return jsonfile.ProtectionProtected
}

func (s *inMemoryStore) wasOpened(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened[path]
}

type fakeObserver struct {
	mu                 sync.Mutex
	snapshots          []environment.Snapshot
	done               chan struct{}
	started            chan struct{}
	afterPublish       <-chan struct{}
	err                error
	block              bool
	returnContextError bool
}

func newFakeObserver() *fakeObserver {
	return &fakeObserver{
		done:    make(chan struct{}),
		started: make(chan struct{}),
		block:   true,
	}
}

func (o *fakeObserver) Observe(ctx context.Context, output chan<- environment.Snapshot) error {
	defer close(o.done)
	close(o.started)
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
	if o.afterPublish != nil {
		select {
		case <-o.afterPublish:
		case <-ctx.Done():
			if o.returnContextError {
				return ctx.Err()
			}
			return nil
		}
	}
	if o.block {
		<-ctx.Done()
		if o.returnContextError {
			return ctx.Err()
		}
	}
	return o.err
}

func (o *fakeObserver) addSnapshot(s environment.Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.snapshots = append(o.snapshots, s)
}

type fakeHostRunner struct {
	mu       sync.Mutex
	called   bool
	cfg      *host.Config
	release  chan error
	started  chan struct{}
	finished chan struct{}
}

type fakeIPCShutdown struct {
	err     error
	called  chan struct{}
	started chan struct{}
	release <-chan struct{}
}

func (s *fakeIPCShutdown) Shutdown(ctx context.Context) error {
	close(s.started)
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(s.called)
	return s.err
}

func newFakeHostRunner() *fakeHostRunner {
	return &fakeHostRunner{
		release:  make(chan error, 1),
		started:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (r *fakeHostRunner) run(ctx context.Context, cfg host.Config) error {
	defer close(r.finished)
	r.mu.Lock()
	r.called = true
	r.cfg = &cfg
	r.mu.Unlock()
	close(r.started)
	select {
	case err := <-r.release:
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
	r.release <- err
}

func testPaths(t *testing.T) defaultPaths {
	t.Helper()
	dir := t.TempDir()
	return defaultPaths{
		profiles:       filepath.Join(dir, "institution-profiles"),
		configurations: filepath.Join(dir, "configurations.json"),
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
    "localPort": {"mode": "fixed", "value": 61440},
    "authVersionHex": "0a00",
    "keepAliveVersionHex": "0b00",
    "controlCheckStatusHex": "00",
    "ipdogHex": "00",
    "adapterNumberHex": "00",
    "osInfoHex": "0000000000000000000000000000000000000000",
    "challengePaddingHex": "000000000000000000000000000000",
    "loginIPDogPaddingHex": "00000000",
    "loginDHCPPaddingHex": "0000000000000000",
    "loginAuthExtensionPaddingHex": "0000",
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
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, "test-token", "abc1234")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	statusResponse, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("daemon.status call: %v", err)
	}
	if !statusResponse.OK {
		t.Fatalf("daemon.status error: %+v", statusResponse.Error)
	}
	var status contract.StatusResult
	if err := json.Unmarshal(statusResponse.Result, &status); err != nil {
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

	profileResponse, err := conn.Call(ctx, contract.MethodProfileList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("profile.list call: %v", err)
	}
	if !profileResponse.OK {
		t.Fatalf("profile.list error: %+v", profileResponse.Error)
	}
	var profiles contract.ProfileListResult
	if err := json.Unmarshal(profileResponse.Result, &profiles); err != nil {
		t.Fatalf("unmarshal profile.list: %v", err)
	}
	if len(profiles.Profiles) != 1 {
		t.Fatalf("profile.list count = %d, want 1", len(profiles.Profiles))
	}
	if profiles.Profiles[0].InstitutionProfileID != "jlu" ||
		profiles.Profiles[0].DisplayName != "Jilin University" ||
		profiles.Profiles[0].AuthenticationProtocolID != "drcom-5.2.0-d" {
		t.Fatalf("profile.list result = %#v", profiles.Profiles[0])
	}

	if !store.wasOpened(paths.configurations) {
		t.Fatal("store was not opened at configurations path")
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
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, "test-token", "abc1234")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	usernameMarker := "fictional-user-7B2F0D91"
	passwordMarker := "fictional-password-4A8C6E13"
	startPayload, _ := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:              "Test Session",
		InstitutionProfileID:     "jlu",
		Username:                 usernameMarker,
		Password:                 passwordMarker,
		NetworkBindingPolicyMode: "automatically_select_latest_available",
		ProtocolContextOverride:  json.RawMessage(`{}`),
	})

	startResponse, err := conn.Call(ctx, contract.MethodSessionStartOneShot, startPayload)
	if err != nil {
		t.Fatalf("session.startOneShot call: %v", err)
	}
	if !startResponse.OK {
		t.Fatalf("session.startOneShot error: %+v", startResponse.Error)
	}
	result := startResponse.Result
	if !bytes.Contains(result, []byte(usernameMarker)) {
		t.Fatal("session.startOneShot response omitted the full account name")
	}
	if bytes.Contains(result, []byte(passwordMarker)) {
		t.Fatal("raw session.startOneShot response contains password marker")
	}

	var startResult contract.SessionStartResult
	if err := json.Unmarshal(result, &startResult); err != nil {
		t.Fatalf("unmarshal session start result: %v", err)
	}
	if startResult.Outcome != "created" {
		t.Fatalf("session start outcome = %q, want created", startResult.Outcome)
	}
	sessionResult := startResult.Session

	if sessionResult.State != "waiting_for_network" {
		t.Fatalf("session.State = %q, want waiting_for_network", sessionResult.State)
	}

	if sessionResult.AuthenticationProtocolID != "drcom-5.2.0-d" {
		t.Fatalf("session.AuthenticationProtocolID = %q, want drcom-5.2.0-d", sessionResult.AuthenticationProtocolID)
	}

	if sessionResult.AccountName != usernameMarker {
		t.Fatalf("session.AccountName = %q, want complete username %q", sessionResult.AccountName, usernameMarker)
	}

	ensureResponse, err := conn.Call(ctx, contract.MethodSessionEnsureRunning, []byte(`{"sessionId":"`+sessionResult.AuthenticationSessionID+`"}`))
	if err != nil {
		t.Fatalf("session.ensureRunning call: %v", err)
	}
	if !ensureResponse.OK {
		t.Fatalf("session.ensureRunning error: %+v", ensureResponse.Error)
	}
	if bytes.Contains(ensureResponse.Result, []byte(passwordMarker)) {
		t.Fatal("raw session.ensureRunning response contains password marker")
	}
	var ensured contract.SessionStartResult
	if err := json.Unmarshal(ensureResponse.Result, &ensured); err != nil {
		t.Fatalf("unmarshal ensure result: %v", err)
	}
	if ensured.Outcome != "already_running" || ensured.Session.AuthenticationSessionID != sessionResult.AuthenticationSessionID || ensured.Session.AccountName != usernameMarker {
		t.Fatalf("ensure result = %#v", ensured)
	}

	listResponse, err := conn.Call(ctx, contract.MethodSessionList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("session.list call: %v", err)
	}
	if !listResponse.OK {
		t.Fatalf("session.list error: %+v", listResponse.Error)
	}
	listedData := listResponse.Result
	var listed contract.SessionListResult
	if err := json.Unmarshal(listedData, &listed); err != nil {
		t.Fatalf("unmarshal session.list: %v", err)
	}
	if len(listed.Sessions) != 1 ||
		listed.Sessions[0].AuthenticationSessionID != sessionResult.AuthenticationSessionID {
		t.Fatalf("session.list result = %#v", listed.Sessions)
	}

	stopPayload, _ := json.Marshal(contract.SessionStopPayload{
		SessionID: sessionResult.AuthenticationSessionID,
	})
	stopResponse, err := conn.Call(ctx, contract.MethodSessionStop, stopPayload)
	if err != nil {
		t.Fatalf("session.stop call: %v", err)
	}
	if !stopResponse.OK {
		t.Fatalf("session.stop error: %+v", stopResponse.Error)
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
		discardLogger(),
	)
	if err == nil {
		t.Fatal("composeObjectGraph() expected error for invalid profile")
	}

	if hostRunner.wasCalled() {
		t.Fatal("host runner was called despite invalid profile")
	}
	select {
	case <-observer.started:
		t.Fatal("observer Observe method started despite invalid profile")
	default:
	}
}

// orderTrackingStore wraps an in-memory store and lets a test observe or fail
// the secure store Read used by OpenCatalog. It implements the same
// configuration.SensitiveStore contract without widening any production seam.
type orderTrackingStore struct {
	*inMemoryStore
	readErr    error
	readCalled bool
}

func (s *orderTrackingStore) Read(ctx context.Context, path string, max int64) ([]byte, bool, error) {
	s.readCalled = true
	if s.readErr != nil {
		return nil, false, s.readErr
	}
	return s.inMemoryStore.Read(ctx, path, max)
}

// TestCompositionOpensCatalogBeforeProfileTraversal proves composeObjectGraph
// opens the Authentication Configuration catalog (secure store Read, which
// prepares and protects the shared configuration root) before the Profile loader
// traverses the program-root institution-profiles directory. It uses only
// test-owned filesystem and store behavior: an invalid Profile on disk plus a store whose
// Read fails with a sentinel. Because the Profile is invalid, Profile traversal
// would surface a "load profiles" error if it ran first; observing the catalog
// sentinel instead proves the catalog Read runs and stops composition before
// Profile traversal.
func TestCompositionOpensCatalogBeforeProfileTraversal(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "bad.json", []byte(`{"schemaVersion":1}`))

	catalogReadErr := errors.New("catalog-read-order-sentinel")
	store := &orderTrackingStore{
		inMemoryStore: newInMemoryStore(),
		readErr:       catalogReadErr,
	}

	_, err := composeObjectGraph(
		context.Background(),
		store,
		paths,
		testHostInfo(),
		newFakeObserver(),
		newFakeHostRunner().run,
		"test-token",
		"1.0.0-test",
		"abc1234",
		discardLogger(),
	)
	if !errors.Is(err, catalogReadErr) {
		t.Fatalf("composeObjectGraph() error = %v, want catalog read sentinel", err)
	}
	if !store.readCalled {
		t.Fatal("catalog Read was not invoked before Profile traversal")
	}
	if strings.Contains(err.Error(), "load profiles") {
		t.Fatalf("Profile traversal ran before catalog Read: %v", err)
	}
}

type fakeSnapshotSink struct {
	mu                  sync.Mutex
	snapshots           []environment.Snapshot
	started             chan struct{}
	done                chan struct{}
	startOnce           sync.Once
	doneOnce            sync.Once
	err                 error
	waitForCancellation bool
}

func newFakeSnapshotSink() *fakeSnapshotSink {
	return &fakeSnapshotSink{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (sink *fakeSnapshotSink) ApplySystemNetworkSnapshot(ctx context.Context, snapshot environment.Snapshot) error {
	sink.mu.Lock()
	sink.snapshots = append(sink.snapshots, snapshot)
	sink.mu.Unlock()
	sink.startOnce.Do(func() { close(sink.started) })
	defer sink.doneOnce.Do(func() { close(sink.done) })
	if sink.waitForCancellation {
		<-ctx.Done()
		return ctx.Err()
	}
	return sink.err
}

type fakeShutdown struct {
	mu                 sync.Mutex
	closeErr           error
	closeCount         int
	waitCount          int
	order              []string
	activityDone       []<-chan struct{}
	allDoneBeforeClose bool
}

func (shutdown *fakeShutdown) Close() error {
	shutdown.mu.Lock()
	defer shutdown.mu.Unlock()
	shutdown.closeCount++
	shutdown.order = append(shutdown.order, "close")
	shutdown.allDoneBeforeClose = true
	for _, done := range shutdown.activityDone {
		select {
		case <-done:
		default:
			shutdown.allDoneBeforeClose = false
		}
	}
	return shutdown.closeErr
}

func (shutdown *fakeShutdown) Wait() {
	shutdown.mu.Lock()
	defer shutdown.mu.Unlock()
	shutdown.waitCount++
	shutdown.order = append(shutdown.order, "wait")
}

// fakeAutomaticLoginPerformer is a no-op automaticLoginPerformer for tests
// that do not exercise automatic login behavior.
type fakeAutomaticLoginPerformer struct{}

func (*fakeAutomaticLoginPerformer) PerformAutomaticLogin(context.Context) error { return nil }

type fakeDesktopOwner struct {
	started    chan struct{}
	done       chan struct{}
	release    chan error
	closeCount int
	mu         sync.Mutex
}

func newFakeDesktopOwner() *fakeDesktopOwner {
	return &fakeDesktopOwner{started: make(chan struct{}), done: make(chan struct{}), release: make(chan error, 1)}
}
func (o *fakeDesktopOwner) Wait(ctx context.Context) error {
	close(o.started)
	defer close(o.done)
	select {
	case err := <-o.release:
		return err
	case <-ctx.Done():
		return nil
	}
}
func (o *fakeDesktopOwner) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeCount++
	return nil
}

func newLifecycleRuntime(
	observer *fakeObserver,
	hostRunner *fakeHostRunner,
	sink *fakeSnapshotSink,
	shutdown *fakeShutdown,
	logger *slog.Logger,
) *composedRuntime {
	return &composedRuntime{
		observer:       observer,
		snapshotSink:   sink,
		automaticLogin: &fakeAutomaticLoginPerformer{},
		shutdown:       shutdown,
		hostRunner:     hostRunner.run,
		logger:         logger,
		productVersion: "test-version",
		buildID:        "test-build",
	}
}

func runRuntime(rt *composedRuntime, ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- rt.run(ctx)
	}()
	return result
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForRuntimeResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime")
		return nil
	}
}

func assertSignalOpen(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatalf("%s unexpectedly started", name)
	default:
	}
}

func assertShutdown(t *testing.T, shutdown *fakeShutdown) {
	t.Helper()
	shutdown.mu.Lock()
	defer shutdown.mu.Unlock()
	if shutdown.closeCount != 1 || shutdown.waitCount != 1 {
		t.Fatalf("shutdown counts = Close %d, Wait %d; want one each", shutdown.closeCount, shutdown.waitCount)
	}
	if len(shutdown.order) != 2 || shutdown.order[0] != "close" || shutdown.order[1] != "wait" {
		t.Fatalf("shutdown order = %v, want [close wait]", shutdown.order)
	}
	if !shutdown.allDoneBeforeClose {
		t.Fatal("shutdown Close ran before every started activity returned")
	}
}

func newCoordinatedLifecycle(t *testing.T, logger *slog.Logger) (*composedRuntime, *fakeObserver, *fakeHostRunner, *fakeSnapshotSink, *fakeShutdown) {
	t.Helper()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	sink.waitForCancellation = true
	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	shutdown := &fakeShutdown{
		activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done},
	}
	return newLifecycleRuntime(observer, hostRunner, sink, shutdown, logger), observer, hostRunner, sink, shutdown
}

func TestRuntimeLifecycleHostReturnsNilAfterOrderedSnapshotDelivery(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(nil)

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil on normal host shutdown", err)
	}
	waitForSignal(t, hostRunner.finished, "host completion")
	waitForSignal(t, observer.done, "observer completion")
	waitForSignal(t, sink.done, "delivery completion")
	assertShutdown(t, shutdown)
}

func TestDesktopOwnerExitUsesNormalBoundedShutdown(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	owner := newFakeDesktopOwner()
	rt.launchOptions, _ = launchcontract.Desktop(42)
	rt.desktopOwner = owner
	shutdown.activityDone = append(shutdown.activityDone, owner.done)
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "delivery start")
	waitForSignal(t, owner.started, "owner start")
	owner.release <- nil
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("owner exit error=%v", err)
	}
	assertShutdown(t, shutdown)
	owner.mu.Lock()
	closes := owner.closeCount
	owner.mu.Unlock()
	if closes != 1 {
		t.Fatalf("owner closes=%d", closes)
	}
}

func TestDesktopOwnerFailureIsRuntimeFailure(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	owner := newFakeDesktopOwner()
	rt.launchOptions, _ = launchcontract.Desktop(42)
	rt.desktopOwner = owner
	shutdown.activityDone = append(shutdown.activityDone, owner.done)
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "delivery start")
	cause := errors.New("owner failed")
	owner.release <- cause
	if err := waitForRuntimeResult(t, result); !errors.Is(err, cause) {
		t.Fatalf("err=%v", err)
	}
	assertShutdown(t, shutdown)
}

func TestDesktopOwnerJoinsExternalCancellationAndCommittedStop(t *testing.T) {
	for _, trigger := range []struct {
		name string
		fire func(context.CancelFunc, *composedRuntime)
	}{
		{"caller cancellation", func(cancel context.CancelFunc, _ *composedRuntime) { cancel() }},
		{"committed stop", func(_ context.CancelFunc, rt *composedRuntime) { rt.stopCh <- struct{}{} }},
	} {
		t.Run(trigger.name, func(t *testing.T) {
			rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
			owner := newFakeDesktopOwner()
			rt.launchOptions, _ = launchcontract.Desktop(42)
			rt.desktopOwner = owner
			rt.stopCh = make(chan struct{}, 1)
			shutdown.activityDone = append(shutdown.activityDone, owner.done)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := runRuntime(rt, ctx)
			waitForSignal(t, hostRunner.started, "host start")
			waitForSignal(t, observer.started, "observer start")
			waitForSignal(t, sink.started, "delivery start")
			waitForSignal(t, owner.started, "owner start")
			trigger.fire(cancel, rt)
			if err := waitForRuntimeResult(t, result); err != nil {
				t.Fatalf("run error=%v", err)
			}
			assertShutdown(t, shutdown)
			owner.mu.Lock()
			closes := owner.closeCount
			owner.mu.Unlock()
			if closes != 1 {
				t.Fatalf("owner closes=%d", closes)
			}
		})
	}
}

func TestCompositionLaunchOptionWatcherRequirements(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))
	options, _ := launchcontract.Desktop(42)
	if _, err := composeObjectGraphWithLaunchOptions(context.Background(), newInMemoryStore(), paths, testHostInfo(), newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", discardLogger(), options, nil); err == nil {
		t.Fatal("desktop accepted no owner")
	}
	if _, err := composeObjectGraphWithLaunchOptions(context.Background(), newInMemoryStore(), paths, testHostInfo(), newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", discardLogger(), launchcontract.Headless(), newFakeDesktopOwner()); err == nil {
		t.Fatal("headless accepted owner")
	}
}

var _ desktopowner.Watcher = (*fakeDesktopOwner)(nil)

func TestRuntimeLifecycleHostFailureCancelsOthers(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	hostErr := errors.New("host failure sentinel")
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(hostErr)

	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, hostErr) {
		t.Fatalf("run() error = %v, want host sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleObserverFailurePreservesCause(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observerErr := errors.New("observer failure sentinel")
	observer.block = false
	observer.err = observerErr
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Fatalf("run() error = %v, want observer sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleObserverNilWhileActiveFails(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observer.block = false
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if err == nil {
		t.Fatal("run() expected error when observer stopped while context remained active")
	}
	if !strings.Contains(err.Error(), "observer stopped unexpectedly") {
		t.Fatalf("run() error = %q, want unexpected observer stop", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleInvalidCallerContextsStartNothingAndShutDown(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, test := range []struct {
		name      string
		ctx       context.Context
		wantError bool
	}{
		{name: "nil", ctx: nil, wantError: true},
		{name: "already canceled", ctx: canceledCtx},
	} {
		t.Run(test.name, func(t *testing.T) {
			observer := newFakeObserver()
			hostRunner := newFakeHostRunner()
			sink := newFakeSnapshotSink()
			shutdown := &fakeShutdown{}
			rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, discardLogger())

			err := rt.run(test.ctx)
			if test.wantError && err == nil {
				t.Fatal("run() expected a nil-context error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("run() error = %v, want nil", err)
			}
			assertSignalOpen(t, hostRunner.started, "host")
			assertSignalOpen(t, observer.started, "observer")
			assertSignalOpen(t, sink.started, "snapshot delivery")
			assertShutdown(t, shutdown)
		})
	}
}

func TestRuntimeLifecycleExternalCancellationIsAlwaysNormal(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observer.returnContextError = true
	ctx, cancel := context.WithCancel(context.Background())

	result := runRuntime(rt, ctx)
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	cancel()

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil after external cancellation", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleExternalCancellationPreservesNonCancellationFailure(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observerErr := errors.New("observer failure concurrent with caller cancellation")
	observer.err = observerErr
	ctx, cancel := context.WithCancel(context.Background())

	result := runRuntime(rt, ctx)
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	cancel()

	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Errorf("run() error = %v, want concurrent observer sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleSnapshotDeliveryFailurePreservesCause(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	deliveryErr := errors.New("snapshot delivery failure sentinel")
	sink.err = deliveryErr
	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	shutdown := &fakeShutdown{
		activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done},
	}
	rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, discardLogger())

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("run() error = %v, want delivery sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleShutdownCloseErrorIsReturnedAndJoined(t *testing.T) {
	closeErr := errors.New("shutdown close failure sentinel")

	t.Run("normal trigger", func(t *testing.T) {
		rt, _, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
		shutdown.closeErr = closeErr
		result := runRuntime(rt, context.Background())
		waitForSignal(t, hostRunner.started, "host start")
		waitForSignal(t, sink.started, "snapshot delivery")
		hostRunner.signalDone(nil)

		err := waitForRuntimeResult(t, result)
		if !errors.Is(err, closeErr) {
			t.Fatalf("run() error = %v, want close sentinel", err)
		}
		assertShutdown(t, shutdown)
	})

	t.Run("fatal trigger", func(t *testing.T) {
		rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
		observerErr := errors.New("observer failure joined with close")
		observer.block = false
		observer.err = observerErr
		observer.afterPublish = sink.started
		shutdown.closeErr = closeErr
		result := runRuntime(rt, context.Background())
		waitForSignal(t, hostRunner.started, "host start")

		err := waitForRuntimeResult(t, result)
		if !errors.Is(err, observerErr) || !errors.Is(err, closeErr) {
			t.Fatalf("run() error = %v, want observer and close sentinels", err)
		}
		assertShutdown(t, shutdown)
	})
}

func TestRuntimeLifecycleJoinsIPCShutdownFailure(t *testing.T) {
	rt, _, hostRunner, _, _ := newCoordinatedLifecycle(t, discardLogger())
	hostErr := errors.New("host failure sentinel")
	ipcErr := errors.New("ipc shutdown failure sentinel")
	ipc := &fakeIPCShutdown{err: ipcErr, called: make(chan struct{}), started: make(chan struct{})}
	rt.ipcShutdown = ipc

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	hostRunner.signalDone(hostErr)
	waitForSignal(t, ipc.started, "IPC shutdown start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, hostErr) || !errors.Is(err, ipcErr) {
		t.Fatalf("run() error = %v, want host and IPC shutdown sentinels", err)
	}
	waitForSignal(t, ipc.called, "IPC shutdown completion")
}

// TestRuntimeLogsStartSnapshotStop proves the runtime emits daemon_runtime_started
// with product_version/build_id/pid, network_snapshot_applied with revision and
// interface_count, and daemon_runtime_stopped on normal completion.
func TestRuntimeLogsStartSnapshotStop(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	observer := newFakeObserver()
	observer.block = true
	observer.addSnapshot(environment.NewSnapshot(7, time.Unix(100, 0), nil))
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	shutdown := &fakeShutdown{
		activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done},
	}
	rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, logger)

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(nil)

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil on normal completion", err)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_runtime_started") != 1 {
		t.Fatalf("expected one daemon_runtime_started, got:\n%s", output)
	}
	if !strings.Contains(output, "product_version=test-version") {
		t.Fatalf("expected product_version=test-version, got:\n%s", output)
	}
	if !strings.Contains(output, "build_id=test-build") {
		t.Fatalf("expected build_id=test-build, got:\n%s", output)
	}
	if !strings.Contains(output, "pid=") {
		t.Fatalf("expected pid attribute, got:\n%s", output)
	}
	if strings.Count(output, "event=network_snapshot_applied") != 1 {
		t.Fatalf("expected one network_snapshot_applied, got:\n%s", output)
	}
	if !strings.Contains(output, "revision=7") {
		t.Fatalf("expected revision=7, got:\n%s", output)
	}
	if !strings.Contains(output, "interface_count=0") {
		t.Fatalf("expected interface_count=0, got:\n%s", output)
	}
	if strings.Count(output, "event=daemon_runtime_stopped") != 1 {
		t.Fatalf("expected one daemon_runtime_stopped, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_failed") {
		t.Fatalf("normal runtime must not log daemon_runtime_failed, got:\n%s", output)
	}
}

// TestRuntimeFailureLogsStartedWithoutStopOrCause proves a failed runtime logs
// daemon_runtime_started, never logs daemon_runtime_stopped, and never leaks the
// original cause. The single daemon_runtime_failed record is owned by the process
// boundary, not by run.
func TestRuntimeFailureLogsStartedWithoutStopOrCause(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	rt, observer, hostRunner, sink, _ := newCoordinatedLifecycle(t, logger)
	observerErr := errors.New("observer failure sentinel-secret-9Z4E1B")
	observer.block = false
	observer.err = observerErr
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Fatalf("run() error = %v, want observer sentinel in error chain", err)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_runtime_started") != 1 {
		t.Fatalf("expected one daemon_runtime_started, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_stopped") {
		t.Fatalf("failed runtime must not log daemon_runtime_stopped, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_failed") {
		t.Fatalf("run must not own the daemon_runtime_failed record, got:\n%s", output)
	}
	if strings.Contains(output, "sentinel-secret-9Z4E1B") {
		t.Fatalf("original cause leaked into runtime log, got:\n%s", output)
	}
}

// TestCompositionInjectsSessionDiagnostics proves the composition root wires the
// production session diagnostics adapter into created Sessions, so the Info
// session_snapshot event appears and the password never does.
func TestCompositionInjectsSessionDiagnostics(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))
	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	rt, err := composeObjectGraph(
		context.Background(), store, paths, hostInfo, observer, hostRunner.run,
		"test-token", "1.0.0-test", "abc1234", logger,
	)
	if err != nil {
		t.Fatalf("composeObjectGraph: %v", err)
	}
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()
	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, "test-token", "abc1234")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	startPayload, _ := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:              "Test Session",
		InstitutionProfileID:     "jlu",
		Username:                 "fictional-user-9Z1",
		Password:                 "fictional-password-9Z2",
		NetworkBindingPolicyMode: "automatically_select_latest_available",
		ProtocolContextOverride:  json.RawMessage(`{}`),
	})
	response, err := conn.Call(ctx, contract.MethodSessionStartOneShot, startPayload)
	if err != nil {
		t.Fatalf("session.startOneShot call: %v", err)
	}
	if !response.OK {
		t.Fatalf("session.startOneShot error: %+v", response.Error)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "event=session_snapshot") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(buf.String(), "event=session_snapshot") {
		t.Fatalf("expected session_snapshot event in log, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "fictional-password-9Z2") {
		t.Fatalf("password leaked into diagnostics log:\n%s", buf.String())
	}
}

// TestCompositionDaemonStopTriggersRuntimeShutdown proves a committed
// daemon.stop response (written via the IPC server) signals the runtime-owned
// stop channel, which cancels the child context and runs the existing graceful
// shutdown path. The response reaches the client before shutdown begins.
func TestCompositionDaemonStopTriggersRuntimeShutdown(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))
	store := newInMemoryStore()
	hostInfo := testHostInfo()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()

	rt, err := composeObjectGraph(
		context.Background(), store, paths, hostInfo, observer, hostRunner.run,
		"test-token", "1.0.0-test", "abc1234", discardLogger(),
	)
	if err != nil {
		t.Fatalf("composeObjectGraph: %v", err)
	}

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")

	conn, err := client.Connect(ctx, wsURL, "test-token", "abc1234")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	idle, err := client.Connect(ctx, wsURL, "test-token", "abc1234")
	if err != nil {
		t.Fatalf("connect idle: %v", err)
	}
	defer idle.Close()

	resp, err := conn.Call(ctx, contract.MethodDaemonStop, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("daemon.stop call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("daemon.stop error: %+v", resp.Error)
	}
	var stopResult contract.DaemonStopResult
	if err := json.Unmarshal(resp.Result, &stopResult); err != nil {
		t.Fatalf("unmarshal stop result: %v", err)
	}
	if stopResult.Status != "stopping" {
		t.Errorf("status = %q, want stopping", stopResult.Status)
	}
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime did not shut down cleanly after daemon.stop: %v", err)
	}
	if _, err := idle.Call(ctx, contract.MethodDaemonStatus, json.RawMessage(`{}`)); err == nil {
		t.Fatal("idle IPC connection remained open after runtime shutdown")
	}
}

type countingAutomaticLoginPerformer struct {
	mu    sync.Mutex
	count int
	err   error
}

func (p *countingAutomaticLoginPerformer) PerformAutomaticLogin(context.Context) error {
	p.mu.Lock()
	p.count++
	err := p.err
	p.mu.Unlock()
	return err
}

func (p *countingAutomaticLoginPerformer) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

func newLifecycleRuntimeWithAutomaticLogin(
	observer *fakeObserver,
	hostRunner *fakeHostRunner,
	sink *fakeSnapshotSink,
	shutdown *fakeShutdown,
	logger *slog.Logger,
	performer automaticLoginPerformer,
) *composedRuntime {
	return &composedRuntime{
		observer:       observer,
		snapshotSink:   sink,
		automaticLogin: performer,
		shutdown:       shutdown,
		hostRunner:     hostRunner.run,
		logger:         logger,
		productVersion: "test-version",
		buildID:        "test-build",
	}
}

func waitForSinkSnapshots(t *testing.T, sink *fakeSnapshotSink, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		sink.mu.Lock()
		got := len(sink.snapshots)
		sink.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %d snapshots, got %d", want, want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestDeliverSnapshotsInvokesAutomaticLoginOnceAfterFirstSnapshot(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	performer := &countingAutomaticLoginPerformer{}
	shutdown := &fakeShutdown{activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done}}
	rt := newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, discardLogger(), performer)

	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	observer.addSnapshot(environment.NewSnapshot(2, time.Unix(101, 0), nil))

	ctx, cancel := context.WithCancel(context.Background())
	result := runRuntime(rt, ctx)

	waitForSinkSnapshots(t, sink, 2)
	if got := performer.Count(); got != 1 {
		t.Fatalf("automatic login count = %d, want 1", got)
	}
	cancel()
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime error = %v", err)
	}
}

func TestDeliverSnapshotsAutomaticLoginFailureDoesNotTerminateDaemon(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	performer := &countingAutomaticLoginPerformer{err: errors.New("simulated failure")}
	shutdown := &fakeShutdown{activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done}}
	rt := newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, discardLogger(), performer)

	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	observer.addSnapshot(environment.NewSnapshot(2, time.Unix(101, 0), nil))

	ctx, cancel := context.WithCancel(context.Background())
	result := runRuntime(rt, ctx)

	waitForSinkSnapshots(t, sink, 2)

	// Verify the runtime is still running (not terminated by the failure).
	select {
	case err := <-result:
		t.Fatalf("runtime terminated after automatic login failure: %v", err)
	default:
	}

	if got := performer.Count(); got != 1 {
		t.Fatalf("automatic login count = %d, want 1", got)
	}
	cancel()
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime error = %v", err)
	}
}
