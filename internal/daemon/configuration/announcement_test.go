package configuration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/daemon/persistence"
)

func announcementProfileDocument(raw string) string {
	source := profileLoaderDocument("campus", "Campus", profileLoaderDefaultConfigBody)
	if raw == "" {
		return source
	}
	return strings.TrimSuffix(source, "}") + `,"announcement":` + raw + `}`
}

func TestAnnouncementLoaderOptionalAndMetadata(t *testing.T) {
	for _, raw := range []string{"", "null", `{"url":"http://login.jlu.edu.cn/notice_win.php","clientWidth":600,"clientHeight":460}`, `{"url":"https://school.example/notice?q=1","clientWidth":1,"clientHeight":4096}`} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			source := announcementProfileDocument(raw)
			writeProfileFile(t, dir, "campus.json", source)
			registry, _ := newProfileLoaderAcceptingRegistry(t)
			catalog, err := LoadProfileCatalogFromDirectory(context.Background(), dir, registry)
			if err != nil {
				t.Fatal(err)
			}
			summaries, err := catalog.ListSummaries(context.Background())
			if err != nil || len(summaries) != 1 {
				t.Fatalf("summaries = %#v, %v", summaries, err)
			}
			if raw == "" || raw == "null" {
				if summaries[0].Announcement != nil {
					t.Fatal("unavailable announcement fabricated")
				}
			} else if summaries[0].Announcement == nil {
				t.Fatal("metadata lost")
			}
			bytes, err := os.ReadFile(filepath.Join(dir, "campus.json"))
			if err != nil || string(bytes) != source {
				t.Fatal("read rewrote document")
			}
		})
	}
}

func TestAnnouncementCatalogClonesEveryBoundary(t *testing.T) {
	ctx := context.Background()
	profile := profileCatalogTestProfile("campus", "protocol")
	profile.Announcement = &AnnouncementMetadata{URL: "http://school.example/notice", ClientWidth: 600, ClientHeight: 460}
	cloned := profile.Clone()
	cloned.Announcement.URL = "http://changed.example"
	if profile.Announcement.URL != "http://school.example/notice" {
		t.Fatal("Clone shared metadata")
	}
	catalog, err := NewProfileCatalog([]InstitutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	profile.Announcement.ClientWidth = 1
	got, err := catalog.Get(ctx, "campus")
	if err != nil || got.Announcement.ClientWidth != 600 {
		t.Fatal("constructor shared metadata")
	}
	got.Announcement.ClientHeight = 1
	summaries, err := catalog.ListSummaries(ctx)
	if err != nil || summaries[0].Announcement.ClientHeight != 460 {
		t.Fatal("Get shared metadata")
	}
	summaries[0].Announcement.ClientWidth = 2
	again, err := catalog.ListSummaries(ctx)
	if err != nil || again[0].Announcement.ClientWidth != 600 {
		t.Fatal("List shared metadata")
	}
	final, err := catalog.Get(ctx, "campus")
	if err != nil || final.Announcement.ClientWidth != 600 || final.Announcement.ClientHeight != 460 {
		t.Fatal("summary mutated catalog")
	}
}

func TestAnnouncementInvalidURLAndSizeClassifications(t *testing.T) {
	for _, url := range []string{"", "notice.php", "//school.example/notice", "ftp://school.example", "http://", "http://:80", "http://user:password@school.example", "http://@school.example", "http://school.example/#", "http://school.example/#notice", "http://school.example/a b", "http://school.example/\n", "http://school.example/\u0085", "http://school.example/\u2003", "http://school.example/\u007f", "http://school.example/%zz"} {
		metadata := AnnouncementMetadata{URL: url, ClientWidth: 600, ClientHeight: 460}
		t.Run(url, func(t *testing.T) { assertAnnouncementRejected(t, metadata) })
	}
	for _, dimensions := range [][2]int{{0, 460}, {600, 0}, {-1, 460}, {600, -1}, {4097, 460}, {600, 4097}} {
		assertAnnouncementRejected(t, AnnouncementMetadata{URL: "http://school.example", ClientWidth: dimensions[0], ClientHeight: dimensions[1]})
	}
}

func assertAnnouncementRejected(t *testing.T, metadata AnnouncementMetadata) {
	t.Helper()
	profile := profileCatalogTestProfile("campus", "protocol")
	profile.Announcement = &metadata
	_, err := NewProfileCatalog([]InstitutionProfile{profile})
	if profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("direct error = %v", err)
	}
	raw, err := json.Marshal(map[string]any{"url": metadata.URL, "clientWidth": metadata.ClientWidth, "clientHeight": metadata.ClientHeight})
	if err != nil {
		t.Fatal(err)
	}
	assertAnnouncementDocumentRejected(t, string(raw))
}

func assertAnnouncementDocumentRejected(t *testing.T, raw string) {
	t.Helper()
	dir := t.TempDir()
	source := announcementProfileDocument(raw)
	writeProfileFile(t, dir, "campus.json", source)
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	_, err := LoadProfileCatalogFromDirectory(context.Background(), dir, registry)
	if profileLoaderFailureCode(t, err) != persistence.FailureInvalidDocument {
		t.Fatalf("loader error = %v", err)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "campus.json"))
	if err != nil || string(bytes) != source {
		t.Fatal("invalid source altered")
	}
}

func TestAnnouncementStrictNestedDocument(t *testing.T) {
	for _, raw := range []string{
		`{}`, `[]`, `true`, `"notice"`,
		`{"URL":"http://school.example","clientWidth":600,"clientHeight":460}`,
		`{"url":"http://school.example","ClientWidth":600,"clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":600,"ClientHeight":460}`,
		`{"url":"http://school.example","URL":"http://other.example","clientWidth":600,"clientHeight":460}`,
		`{"clientWidth":600,"clientHeight":460}`,
		`{"url":"http://school.example","clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":600}`,
		`{"url":null,"clientWidth":600,"clientHeight":460}`,
		`{"url":true,"clientWidth":600,"clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":null,"clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":"600","clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":600.5,"clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":600,"clientHeight":false}`,
		`{"url":"http://school.example","clientWidth":600,"clientHeight":460,"extra":true}`,
		`{"url":"http://school.example","url":"http://other.example","clientWidth":600,"clientHeight":460}`,
		`{"url":"http://school.example","clientWidth":600,"client\u0057idth":1,"clientHeight":460}`,
	} {
		t.Run(raw, func(t *testing.T) { assertAnnouncementDocumentRejected(t, raw) })
	}
}
