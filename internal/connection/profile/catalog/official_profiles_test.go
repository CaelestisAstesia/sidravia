package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"sidravia/internal/connection/auth/supported"
)

// TestOfficialProfilesLoad proves the shipped official Profile directory loads
// through the strict loader and the shipped protocol descriptions. The build tool embeds
// this directory's jlu.json byte-for-byte into both packages, so the loader
// passing here is the automatic guarantee that a packaged Profile is valid.
// Go test sets the working directory to the package source directory, so
// filepath.Abs("../data") resolves to internal/connection/profile/data.
func TestOfficialProfilesLoad(t *testing.T) {
	dir, err := filepath.Abs("../data")
	if err != nil {
		t.Fatalf("Abs(profiles) error = %v", err)
	}
	catalog, err := LoadProfileCatalogFromDirectory(context.Background(), dir, supported.ValidateInstitution)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
	}
	summaries, err := catalog.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("official profiles = %d, want exactly 2", len(summaries))
	}
	if summaries[0].InstitutionProfileID != "jlu" {
		t.Fatalf("official profile ID = %q, want jlu", summaries[0].InstitutionProfileID)
	}
	if summaries[1].InstitutionProfileID != "jlu-experimental" || summaries[1].DisplayName != "吉林大学（实验性）" {
		t.Fatalf("experimental profile = %#v", summaries[1])
	}
	if a := summaries[0].Announcement; a == nil || a.URL != "http://login.jlu.edu.cn/notice_win.php" || a.ClientWidth != 600 || a.ClientHeight != 460 {
		t.Fatalf("official announcement = %#v", a)
	}
}
