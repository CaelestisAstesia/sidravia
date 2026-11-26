package daemon

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sidravia/internal/connection/environment"
	host "sidravia/internal/daemon/internal/endpoint"
	"sidravia/internal/document/securefile"
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

func (s *inMemoryStore) ReplaceSensitive(ctx context.Context, path string, data []byte, _ int64, _ bool) error {
	return s.Replace(ctx, path, data)
}

func (s *inMemoryStore) PreserveSensitive(ctx context.Context, path string, data []byte, maximum int64, allow bool) error {
	return s.ReplaceSensitive(ctx, path, data, maximum, allow)
}

func (s *inMemoryStore) ProtectionStatus() securefile.ProtectionStatus {
	return securefile.ProtectionProtected
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
	if cfg.OnReady != nil {
		cfg.OnReady()
	}
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
	profile, err := os.ReadFile(filepath.Join("testdata", "runtime-jlu-profile.json"))
	if err != nil {
		t.Fatalf("read JLU profile fixture: %v", err)
	}
	return profile
}

const (
	runtimeFixtureToken   = "test-token"
	runtimeFixtureVersion = "1.0.0-test"
	runtimeFixtureBuildID = "abc1234"
)

type runtimeCompositionFixture struct {
	runtime    *composedRuntime
	paths      defaultPaths
	store      *inMemoryStore
	observer   *fakeObserver
	hostRunner *fakeHostRunner
}

func composeRuntimeWithJLUProfile(t *testing.T, logger *slog.Logger) runtimeCompositionFixture {
	t.Helper()
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))
	store := newInMemoryStore()
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	rt, err := composeObjectGraph(
		context.Background(), store, paths, testHostInfo(), observer, hostRunner.run,
		runtimeFixtureToken, runtimeFixtureVersion, runtimeFixtureBuildID, logger,
	)
	if err != nil {
		t.Fatalf("composeObjectGraph() error = %v", err)
	}
	return runtimeCompositionFixture{rt, paths, store, observer, hostRunner}
}

func testHostInfo() environment.SystemHostInformation {
	return environment.SystemHostInformation{
		HostName:               "test-pc",
		OperatingSystemFamily:  "Windows",
		OperatingSystemRelease: "10.0.19045",
		MachineArchitecture:    "amd64",
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

func newLifecycleRuntime(
	observer *fakeObserver,
	hostRunner *fakeHostRunner,
	sink *fakeSnapshotSink,
	shutdown *fakeShutdown,
	logger *slog.Logger,
) *composedRuntime {
	return newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, logger, &fakeAutomaticLoginPerformer{})
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
