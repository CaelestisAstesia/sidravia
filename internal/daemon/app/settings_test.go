package app

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSettingsEmptyWhenNoDocument(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	settings, err := OpenSettings(ctx, store, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	_, exists, err := settings.LoadAutoConnect(ctx)
	if err != nil {
		t.Fatalf("LoadAutoConnect() error = %v", err)
	}
	if exists {
		t.Fatal("LoadAutoConnect() exists = true, want false")
	}
}

func TestSettingsAutoConnectRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	settings, err := OpenSettings(ctx, store, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	if err := settings.SaveAutoConnect(ctx, AutoConnectSettings{ConfigurationID: "config-1"}); err != nil {
		t.Fatalf("SaveAutoConnect() error = %v", err)
	}
	loaded, exists, err := settings.LoadAutoConnect(ctx)
	if err != nil {
		t.Fatalf("LoadAutoConnect() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadAutoConnect() exists = false, want true")
	}
	if loaded.ConfigurationID != "config-1" {
		t.Fatalf("ConfigurationID = %q, want %q", loaded.ConfigurationID, "config-1")
	}
}

func TestSettingsAutoConnectSurvivesReload(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := OpenSettings(ctx, store, path)
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	if err := settings.SaveAutoConnect(ctx, AutoConnectSettings{ConfigurationID: "config-1"}); err != nil {
		t.Fatalf("SaveAutoConnect() error = %v", err)
	}
	reloaded, err := OpenSettings(ctx, store, path)
	if err != nil {
		t.Fatalf("OpenSettings() reload error = %v", err)
	}
	loaded, exists, err := reloaded.LoadAutoConnect(ctx)
	if err != nil {
		t.Fatalf("LoadAutoConnect() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadAutoConnect() exists = false after reload, want true")
	}
	if loaded.ConfigurationID != "config-1" {
		t.Fatalf("ConfigurationID = %q, want %q", loaded.ConfigurationID, "config-1")
	}
}

func TestSettingsClearAutoConnect(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	settings, err := OpenSettings(ctx, store, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	if err := settings.SaveAutoConnect(ctx, AutoConnectSettings{ConfigurationID: "config-1"}); err != nil {
		t.Fatalf("SaveAutoConnect() error = %v", err)
	}
	if err := settings.ClearAutoConnect(ctx); err != nil {
		t.Fatalf("ClearAutoConnect() error = %v", err)
	}
	_, exists, err := settings.LoadAutoConnect(ctx)
	if err != nil {
		t.Fatalf("LoadAutoConnect() error = %v", err)
	}
	if exists {
		t.Fatal("LoadAutoConnect() exists = true after clear, want false")
	}
}

func TestSettingsRejectsNilContext(t *testing.T) {
	store := newAppMemoryStore()
	settings, err := OpenSettings(context.Background(), store, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	if err := settings.SaveAutoConnect(nil, AutoConnectSettings{ConfigurationID: "config-1"}); err == nil {
		t.Fatal("SaveAutoConnect(nil ctx) error = nil, want error")
	}
}

func TestSettingsRejectsInvalidPath(t *testing.T) {
	store := newAppMemoryStore()
	if _, err := OpenSettings(context.Background(), store, "relative/path.json"); err == nil {
		t.Fatal("OpenSettings(relative path) error = nil, want error")
	}
}

func TestSettingsReloadWithCorruptDocument(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := store.Replace(ctx, path, []byte(`{not valid json`)); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if _, err := OpenSettings(ctx, store, path); err == nil {
		t.Fatal("OpenSettings(corrupt) error = nil, want error")
	}
}

func TestAutoConnectRestartResolvesWithNewSessionID(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	store := newAppMemoryStore()
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := OpenSettings(ctx, store, path)
	if err != nil {
		t.Fatalf("OpenSettings() error = %v", err)
	}
	if err := settings.SaveAutoConnect(ctx, AutoConnectSettings{ConfigurationID: setup.configuration.ConfigurationID}); err != nil {
		t.Fatalf("SaveAutoConnect() error = %v", err)
	}
	reloaded, err := OpenSettings(ctx, store, path)
	if err != nil {
		t.Fatalf("OpenSettings() reload error = %v", err)
	}
	loaded, exists, err := reloaded.LoadAutoConnect(ctx)
	if err != nil {
		t.Fatalf("LoadAutoConnect() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadAutoConnect() exists = false after reload")
	}
	if loaded.ConfigurationID != setup.configuration.ConfigurationID {
		t.Fatalf("ConfigurationID changed across reload: got %q, want %q", loaded.ConfigurationID, setup.configuration.ConfigurationID)
	}
	definition1, err := setup.authenticationResolver.Resolve(ctx, loaded.ConfigurationID, "session-1")
	if err != nil {
		t.Fatalf("Resolve(session-1) error = %v", err)
	}
	if definition1.Configuration.AuthenticationSessionID != "session-1" {
		t.Fatalf("first SessionID = %q", definition1.Configuration.AuthenticationSessionID)
	}
	definition2, err := setup.authenticationResolver.Resolve(ctx, loaded.ConfigurationID, "session-2")
	if err != nil {
		t.Fatalf("Resolve(session-2) error = %v", err)
	}
	if definition2.Configuration.AuthenticationSessionID != "session-2" {
		t.Fatalf("second SessionID = %q", definition2.Configuration.AuthenticationSessionID)
	}
	if definition1.Configuration.AuthenticationSessionID == definition2.Configuration.AuthenticationSessionID {
		t.Fatal("SessionID did not change across restart")
	}
	if definition1.Configuration.DisplayName != definition2.Configuration.DisplayName {
		t.Fatalf("DisplayName changed: %q vs %q", definition1.Configuration.DisplayName, definition2.Configuration.DisplayName)
	}
}
