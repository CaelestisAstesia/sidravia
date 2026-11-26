//go:build windows

package inno

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
)

func TestClearRetiredOperationsPreservesUnownedFiles(t *testing.T) {
	root := t.TempDir()
	namespace, err := productlayout.NewNamespace("uninstall-fixture")
	if err != nil {
		t.Fatal(err)
	}
	layout := productlayout.Layout{Mode: productlayout.ModeInstalled, ExecutableDirectory: filepath.Join(root, "installed"), Namespace: namespace}
	family := filepath.Join(root, operationFamily(layout.ExecutableDirectory, layout.Namespace))
	ownedID := strings.Repeat("1", 32)
	foreignID := strings.Repeat("2", 32)
	unknownID := strings.Repeat("3", 32)
	retiredID := strings.Repeat("4", 32)
	ownedPath := filepath.Join(family, ownedID, "state.json")
	foreignPath := filepath.Join(family, foreignID, "state.json")
	unknownPath := filepath.Join(family, unknownID, "state.json")
	retiredPath := filepath.Join(family, retiredID, "state.json")
	sibling := filepath.Join(family, ownedID, "notes.txt")
	skippedFamily := filepath.Join(root, operationFamily(layout.ExecutableDirectory, productlayout.Namespace{}), ownedID, "state.json")

	state := record{
		SchemaVersion: 1, OperationID: ownedID,
		Source:      change.Target{ID: layout.ExecutableDirectory, Version: "0.6.0-dev.d2.1", BuildID: strings.Repeat("a", 40), Layout: string(layout.Mode)},
		Namespace:   namespace.String(),
		Coordinator: processID{PID: uint32(os.Getpid()), Created: 1},
		Hashes:      map[string]string{"sidraviactl.exe": strings.Repeat("0", 64), "sidraviad.exe": strings.Repeat("1", 64)},
		Stage:       "stopped",
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeRecord := func(path string, state record) {
		t.Helper()
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		write(path, data)
	}
	writeRecord(ownedPath, state)
	write(sibling, []byte("user document"))
	write(unknownPath, []byte("unknown data"))
	production := state
	production.Namespace = ""
	writeRecord(skippedFamily, production)
	retired := state
	retired.OperationID = retiredID
	retired.Stage = "aborted"
	writeRecord(retiredPath, retired)
	foreign := state
	foreign.OperationID = foreignID
	foreign.Source.ID = filepath.Join(root, "independent-portable")
	foreign.Source.Layout = string(productlayout.ModePortable)
	writeRecord(foreignPath, foreign)

	if err := clearRetiredRecords(context.Background(), root, layout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(ownedPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("owned stopped record remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(retiredPath)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("retired operation directory remains: %v", err)
	}
	for _, path := range []string{sibling, foreignPath, unknownPath, skippedFamily} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("unowned file changed: %s, %v", path, err)
		}
	}

	state.Stage = "prepared"
	writeRecord(ownedPath, state)
	if err := clearRetiredRecords(context.Background(), root, layout); !errors.Is(err, change.ErrInvalid) {
		t.Fatalf("active record cleanup was accepted: %v", err)
	}
	if _, err := os.Lstat(ownedPath); err != nil {
		t.Fatalf("active record changed: %v", err)
	}
}
