package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

func TestComposeObjectGraphWithValidProfile(t *testing.T) {
	fixture := composeRuntimeWithJLUProfile(t, discardLogger())
	rt := fixture.runtime
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, runtimeFixtureToken, runtimeFixtureBuildID)
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

	if !fixture.store.wasOpened(fixture.paths.configurations) {
		t.Fatal("store was not opened at configurations path")
	}

	if fixture.hostRunner.wasCalled() {
		t.Fatal("host runner was called during construction")
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

func TestDeriveDefaultPathsIsolatesNamespace(t *testing.T) {
	prod, err := deriveDefaultPaths("")
	if err != nil {
		t.Fatal(err)
	}
	ns, err := deriveDefaultPaths("mock-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{prod.profiles, prod.configurations, prod.runtimeInfo} {
		if strings.Contains(p, "mock-test") {
			t.Fatalf("production path %q contains namespace", p)
		}
	}
	for _, p := range []string{ns.profiles, ns.configurations, ns.runtimeInfo} {
		if !strings.Contains(p, "mock-test") {
			t.Fatalf("namespace path %q missing isolation", p)
		}
	}
}

func TestCompositionPassesExactLaunchNamespaceToHostConfig(t *testing.T) {
	options, err := launchcontract.New(launchcontract.ModeHeadless, 0, "Ns02-Test")
	if err != nil {
		t.Fatal(err)
	}
	want, err := productlayout.NewNamespace(options.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := composeObjectGraphWithLaunchOptions(
		context.Background(), newInMemoryStore(), testPaths(t), testHostInfo(),
		newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", discardLogger(), options, nil,
	)
	if err != nil {
		t.Fatalf("composeObjectGraphWithLaunchOptions() error = %v", err)
	}
	if rt.hostCfg.Namespace != want {
		t.Fatalf("host namespace = %q, want %q", rt.hostCfg.Namespace.String(), want.String())
	}
}

func TestComposeObjectGraphRejectsInvalidPathsInOrder(t *testing.T) {
	for _, invalid := range []string{"", "relative", "/unclean/../path"} {
		for first := 0; first < 3; first++ {
			names := []string{"profiles", "configurations", "runtime info"}
			t.Run(names[first]+"/"+invalid, func(t *testing.T) {
				paths := testPaths(t)
				fields := []*string{&paths.profiles, &paths.configurations, &paths.runtimeInfo}
				for _, field := range fields[first:] {
					*field = invalid
				}
				store := newInMemoryStore()
				rt, err := composeObjectGraph(context.Background(), store, paths,
					testHostInfo(), newFakeObserver(), newFakeHostRunner().run,
					"test-token", "1.0.0-test", "abc1234", discardLogger())
				if rt != nil {
					rt.shutdown.Close()
					rt.shutdown.Wait()
					t.Fatal("invalid paths produced a runtime")
				}
				want := "sidraviad: " + names[first] + " path must be absolute and clean"
				if err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
				if len(store.opened) != 0 {
					t.Fatal("store was accessed before path rejection")
				}
			})
		}
	}
}
