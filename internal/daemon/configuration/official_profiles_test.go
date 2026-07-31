package configuration

import (
	"context"
	"path/filepath"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
)

// TestOfficialProfilesLoad proves the shipped official Profile directory loads
// through the strict loader and the real d520 factory. The build tool embeds
// this directory's jlu.json byte-for-byte into both packages, so the loader
// passing here is the automatic guarantee that a packaged Profile is valid.
// Go test sets the working directory to the package source directory, so
// filepath.Abs("profiles") resolves to internal/daemon/configuration/profiles.
func TestOfficialProfilesLoad(t *testing.T) {
	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		t.Fatalf("NewAuthenticationProtocolRegistry() error = %v", err)
	}
	dir, err := filepath.Abs("profiles")
	if err != nil {
		t.Fatalf("Abs(profiles) error = %v", err)
	}
	catalog, err := LoadProfileCatalogFromDirectory(context.Background(), dir, registry)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
	}
	summaries, err := catalog.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("official profiles = %d, want exactly 1", len(summaries))
	}
	if summaries[0].InstitutionProfileID != "jlu" {
		t.Fatalf("official profile ID = %q, want jlu", summaries[0].InstitutionProfileID)
	}
}
