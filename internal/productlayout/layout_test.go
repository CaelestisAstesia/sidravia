package productlayout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testResolver builds a resolver whose executable lives in exeDir, with the
// supplied OS user directories. lstat defaults to the real os.Lstat so the
// marker check exercises the filesystem.
func testResolver(t *testing.T, exeDir, configDir, cacheDir string) osResolver {
	t.Helper()
	return osResolver{
		executable:    func() (string, error) { return filepath.Join(exeDir, "sidravia"), nil },
		userConfigDir: func() (string, error) { return configDir, nil },
		userCacheDir:  func() (string, error) { return cacheDir, nil },
		lstat:         os.Lstat,
	}
}

func assertAbsoluteClean(t *testing.T, layout Layout) {
	t.Helper()
	paths := []struct {
		name string
		path string
	}{
		{"executable directory", layout.ExecutableDirectory},
		{"configurations path", layout.ConfigurationsPath},
		{"institution profiles directory", layout.InstitutionProfilesDirectory},
		{"runtime info path", layout.RuntimeInfoPath},
		{"daemon log path", layout.DaemonLogPath},
	}
	for _, p := range paths {
		if !filepath.IsAbs(p.path) {
			t.Fatalf("%s is not absolute: %q", p.name, p.path)
		}
		if filepath.Clean(p.path) != p.path {
			t.Fatalf("%s is not clean: %q", p.name, p.path)
		}
	}
}

// 1. Without a marker, installed mode maps the five product paths onto the two
// OS user directories exactly.
func TestResolveInstalledModeWithoutMarker(t *testing.T) {
	exeDir := t.TempDir()
	configDir := t.TempDir()
	cacheDir := t.TempDir()

	layout, err := resolve(testResolver(t, exeDir, configDir, cacheDir))
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if layout.Mode != ModeInstalled {
		t.Fatalf("Mode = %q, want installed", layout.Mode)
	}
	if layout.ExecutableDirectory != exeDir {
		t.Fatalf("ExecutableDirectory = %q, want %q", layout.ExecutableDirectory, exeDir)
	}
	configRoot := filepath.Join(configDir, "Sidravia")
	cacheRoot := filepath.Join(cacheDir, "Sidravia")
	if layout.ConfigurationsPath != filepath.Join(configRoot, "configurations.json") {
		t.Fatalf("ConfigurationsPath = %q", layout.ConfigurationsPath)
	}
	if layout.InstitutionProfilesDirectory != filepath.Join(exeDir, "institution-profiles") {
		t.Fatalf("InstitutionProfilesDirectory = %q", layout.InstitutionProfilesDirectory)
	}
	if layout.RuntimeInfoPath != filepath.Join(cacheRoot, "runtime.json") {
		t.Fatalf("RuntimeInfoPath = %q", layout.RuntimeInfoPath)
	}
	if layout.DaemonLogPath != filepath.Join(cacheRoot, "logs", "sidraviad.log") {
		t.Fatalf("DaemonLogPath = %q", layout.DaemonLogPath)
	}
	assertAbsoluteClean(t, layout)
}

// 2. A regular empty marker enables portable mode and places all five product
// paths under the executable directory; 3. portable mode never calls the OS
// user directory resolvers.
func TestResolvePortableModeWithRegularMarker(t *testing.T) {
	exeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(exeDir, portableMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	userDirCalls := 0
	r := osResolver{
		executable:    func() (string, error) { return filepath.Join(exeDir, "sidravia"), nil },
		userConfigDir: func() (string, error) { userDirCalls++; return "", errors.New("userConfigDir must not be called") },
		userCacheDir:  func() (string, error) { userDirCalls++; return "", errors.New("userCacheDir must not be called") },
		lstat:         os.Lstat,
	}

	layout, err := resolve(r)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if layout.Mode != ModePortable {
		t.Fatalf("Mode = %q, want portable", layout.Mode)
	}
	if userDirCalls != 0 {
		t.Fatalf("portable mode called user directory resolvers %d times", userDirCalls)
	}
	if layout.ConfigurationsPath != filepath.Join(exeDir, "config", "configurations.json") {
		t.Fatalf("ConfigurationsPath = %q", layout.ConfigurationsPath)
	}
	if layout.InstitutionProfilesDirectory != filepath.Join(exeDir, "institution-profiles") {
		t.Fatalf("InstitutionProfilesDirectory = %q", layout.InstitutionProfilesDirectory)
	}
	if layout.RuntimeInfoPath != filepath.Join(exeDir, "runtime", "runtime.json") {
		t.Fatalf("RuntimeInfoPath = %q", layout.RuntimeInfoPath)
	}
	if layout.DaemonLogPath != filepath.Join(exeDir, "logs", "sidraviad.log") {
		t.Fatalf("DaemonLogPath = %q", layout.DaemonLogPath)
	}
	assertAbsoluteClean(t, layout)
}

// 4. A directory, symlink or other non-regular marker fails without falling
// back to either mode.
func TestResolveFailsWhenMarkerIsNotRegular(t *testing.T) {
	t.Run("directory marker", func(t *testing.T) {
		exeDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(exeDir, portableMarker), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := resolve(testResolver(t, exeDir, t.TempDir(), t.TempDir())); err == nil {
			t.Fatal("directory marker: expected error, got nil")
		}
	})

	t.Run("symlink marker", func(t *testing.T) {
		exeDir := t.TempDir()
		target := filepath.Join(exeDir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(exeDir, portableMarker)); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		if _, err := resolve(testResolver(t, exeDir, t.TempDir(), t.TempDir())); err == nil {
			t.Fatal("symlink marker: expected error, got nil")
		}
	})
}

// 5. The executable failure, marker IO cause and user directory failures are
// observable through errors.Is/As.
func TestResolvePreservesCauses(t *testing.T) {
	t.Run("executable failure", func(t *testing.T) {
		exeErr := errors.New("executable sentinel")
		r := osResolver{
			executable:    func() (string, error) { return "", exeErr },
			userConfigDir: func() (string, error) { return t.TempDir(), nil },
			userCacheDir:  func() (string, error) { return t.TempDir(), nil },
			lstat:         os.Lstat,
		}
		_, err := resolve(r)
		if !errors.Is(err, exeErr) {
			t.Fatalf("executable failure: error = %v, want sentinel", err)
		}
	})

	t.Run("marker io cause", func(t *testing.T) {
		exeDir := t.TempDir()
		ioErr := &os.PathError{Op: "lstat", Path: filepath.Join(exeDir, portableMarker), Err: os.ErrPermission}
		r := osResolver{
			executable:    func() (string, error) { return filepath.Join(exeDir, "sidravia"), nil },
			userConfigDir: func() (string, error) { return t.TempDir(), nil },
			userCacheDir:  func() (string, error) { return t.TempDir(), nil },
			lstat:         func(string) (os.FileInfo, error) { return nil, ioErr },
		}
		_, err := resolve(r)
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("marker io cause: error = %v, want os.ErrPermission", err)
		}
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) {
			t.Fatalf("marker io cause: error does not wrap *os.PathError: %v", err)
		}
	})

	t.Run("user config directory failure", func(t *testing.T) {
		exeDir := t.TempDir()
		configErr := errors.New("user config sentinel")
		r := osResolver{
			executable:    func() (string, error) { return filepath.Join(exeDir, "sidravia"), nil },
			userConfigDir: func() (string, error) { return "", configErr },
			userCacheDir:  func() (string, error) { return t.TempDir(), nil },
			lstat:         os.Lstat,
		}
		_, err := resolve(r)
		if !errors.Is(err, configErr) {
			t.Fatalf("user config failure: error = %v, want sentinel", err)
		}
	})

	t.Run("user cache directory failure", func(t *testing.T) {
		exeDir := t.TempDir()
		cacheErr := errors.New("user cache sentinel")
		r := osResolver{
			executable:    func() (string, error) { return filepath.Join(exeDir, "sidravia"), nil },
			userConfigDir: func() (string, error) { return t.TempDir(), nil },
			userCacheDir:  func() (string, error) { return "", cacheErr },
			lstat:         os.Lstat,
		}
		_, err := resolve(r)
		if !errors.Is(err, cacheErr) {
			t.Fatalf("user cache failure: error = %v, want sentinel", err)
		}
	})
}

// 7. Resolution never creates config, runtime, logs or any product file.
func TestResolveDoesNotCreateFilesOrDirectories(t *testing.T) {
	t.Run("installed", func(t *testing.T) {
		exeDir := t.TempDir()
		configDir := t.TempDir()
		cacheDir := t.TempDir()
		if _, err := resolve(testResolver(t, exeDir, configDir, cacheDir)); err != nil {
			t.Fatalf("resolve() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(configDir, "Sidravia")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("resolve created config root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, "Sidravia")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("resolve created cache root: %v", err)
		}
	})

	t.Run("portable", func(t *testing.T) {
		exeDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(exeDir, portableMarker), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		layout, err := resolve(testResolver(t, exeDir, t.TempDir(), t.TempDir()))
		if err != nil {
			t.Fatalf("resolve() error = %v", err)
		}
		// Verify none of the resolved parent directories were created.
		for _, dir := range []string{
			filepath.Dir(layout.ConfigurationsPath),
			filepath.Dir(layout.RuntimeInfoPath),
			filepath.Dir(layout.DaemonLogPath),
		} {
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("resolve created directory %q", dir)
			}
		}
	})
}

// TestResolveDefaultReturnsInstalledLayout proves the public Resolve wires the
// real host calls and returns an absolute, clean installed layout for the test
// binary (which has no portable marker next to it).
func TestResolveDefaultReturnsInstalledLayout(t *testing.T) {
	layout, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if layout.Mode != ModeInstalled {
		t.Fatalf("Mode = %q, want installed (test binary has no marker)", layout.Mode)
	}
	assertAbsoluteClean(t, layout)
}

func TestResolveNamespaceProductionIsByteIdentical(t *testing.T) {
	exeDir := t.TempDir()
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	r := testResolver(t, exeDir, configDir, cacheDir)
	prod, err := resolve(r)
	if err != nil {
		t.Fatal(err)
	}
	withNs, err := resolveWithNamespace(r, Namespace{})
	if err != nil {
		t.Fatal(err)
	}
	if prod != withNs {
		t.Fatalf("production namespace layout differs: %#v != %#v", prod, withNs)
	}
}

func TestResolveNamespaceInstalledIsolatedRoot(t *testing.T) {
	exeDir := t.TempDir()
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	ns, err := NewNamespace("mock-test")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := resolveWithNamespace(testResolver(t, exeDir, configDir, cacheDir), ns)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Namespace != ns {
		t.Fatalf("Namespace = %q, want %q", layout.Namespace, ns)
	}
	root := filepath.Join(configDir, "Sidravia", "namespaces", "mock-test")
	if layout.ConfigurationsPath != filepath.Join(root, "configurations.json") {
		t.Fatalf("ConfigurationsPath = %q", layout.ConfigurationsPath)
	}
	if layout.InstitutionProfilesDirectory != filepath.Join(root, "institution-profiles") {
		t.Fatalf("InstitutionProfilesDirectory = %q", layout.InstitutionProfilesDirectory)
	}
	if layout.RuntimeInfoPath != filepath.Join(root, "runtime.json") {
		t.Fatalf("RuntimeInfoPath = %q", layout.RuntimeInfoPath)
	}
	if layout.DaemonLogPath != filepath.Join(root, "logs", "sidraviad.log") {
		t.Fatalf("DaemonLogPath = %q", layout.DaemonLogPath)
	}
	assertAbsoluteClean(t, layout)
}

func TestResolveNamespacePortableIsolatedRoot(t *testing.T) {
	exeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(exeDir, portableMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ns, err := NewNamespace("mock-test")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := resolveWithNamespace(testResolver(t, exeDir, t.TempDir(), t.TempDir()), ns)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(exeDir, "namespaces", "mock-test")
	if layout.ConfigurationsPath != filepath.Join(root, "configurations.json") {
		t.Fatalf("ConfigurationsPath = %q", layout.ConfigurationsPath)
	}
	if layout.InstitutionProfilesDirectory != filepath.Join(root, "institution-profiles") {
		t.Fatalf("InstitutionProfilesDirectory = %q", layout.InstitutionProfilesDirectory)
	}
	if layout.RuntimeInfoPath != filepath.Join(root, "runtime.json") {
		t.Fatalf("RuntimeInfoPath = %q", layout.RuntimeInfoPath)
	}
	if layout.DaemonLogPath != filepath.Join(root, "logs", "sidraviad.log") {
		t.Fatalf("DaemonLogPath = %q", layout.DaemonLogPath)
	}
	assertAbsoluteClean(t, layout)
}
