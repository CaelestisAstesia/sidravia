package persistence

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestIssueCollectorDeduplicatesAndPreservesFirstSeenOrder(t *testing.T) {
	collector := NewIssueCollector(3)
	first := Issue{FileKind: FileKindInstitutionProfiles, Location: "first.json", Code: IssueCodeProfileDocumentInvalid}
	second := Issue{FileKind: FileKindApplicationSettings, Location: "application", Code: IssueCodeApplicationDocumentInvalid}

	if !collector.Record(first) || collector.Record(first) || !collector.Record(second) {
		t.Fatal("Record acceptance did not distinguish first occurrence from duplicate")
	}
	got := collector.Snapshot()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("Snapshot() = %#v, want first-seen unique issues", got)
	}
}

func TestIssueCollectorClampsLimitToOneThroughTwoHundredFiftySix(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int
		want  int
	}{
		{name: "below minimum", limit: -1, want: 1},
		{name: "zero", limit: 0, want: 1},
		{name: "within range", limit: 2, want: 2},
		{name: "above maximum", limit: 257, want: 256},
	} {
		t.Run(test.name, func(t *testing.T) {
			collector := NewIssueCollector(test.limit)
			for index := 0; index < test.want+1; index++ {
				issue := Issue{FileKind: FileKindInstitutionProfiles, Location: fmt.Sprintf("%d", index), Code: IssueCodeProfileDocumentInvalid}
				if got, want := collector.Record(issue), index < test.want; got != want {
					t.Fatalf("Record(%d) = %v, want %v", index, got, want)
				}
			}
		})
	}
}

func TestIssueCollectorRejectsUnsafeOrStructurallyInvalidLocations(t *testing.T) {
	valid := []string{"0", "1", "application", "notificationSettings", "loggingSettings", "institution-profiles", "Example University.json", strings.Repeat("a", 250) + ".json"}
	invalid := []string{"", "+1", "-1", "01", " 1", "1 ", "a/b.json", `a\b.json`, `C:\secret.json`, `C:secret.json`, `Z:profile.json`, "control\n.json", "control\u0085.json", strings.Repeat("a", 251) + ".json"}
	for _, location := range valid {
		t.Run("accept "+location, func(t *testing.T) {
			collector := NewIssueCollector(1)
			if !collector.Record(Issue{FileKind: FileKindInstitutionProfiles, Location: location, Code: IssueCodeProfileDocumentInvalid}) {
				t.Fatalf("Record rejected valid location %q", location)
			}
		})
	}
	for _, location := range invalid {
		t.Run("reject "+fmt.Sprintf("%q", location), func(t *testing.T) {
			collector := NewIssueCollector(1)
			if collector.Record(Issue{FileKind: FileKindInstitutionProfiles, Location: location, Code: IssueCodeProfileDocumentInvalid}) {
				t.Fatalf("Record accepted unsafe or invalid location %q", location)
			}
		})
	}

	collector := NewIssueCollector(1)
	if collector.Record(Issue{FileKind: "unknown", Location: "0", Code: IssueCodeProfileDocumentInvalid}) || collector.Record(Issue{FileKind: FileKindInstitutionProfiles, Location: "0", Code: "unknown"}) {
		t.Fatal("Record accepted an unknown file kind or issue code")
	}
}

func TestIssueCollectorSnapshotIsImmutable(t *testing.T) {
	collector := NewIssueCollector(1)
	issue := Issue{FileKind: FileKindApplicationSettings, Location: "application", Code: IssueCodeApplicationDocumentInvalid}
	collector.Record(issue)
	copy := collector.Snapshot()
	copy[0].Location = "mutated.json"
	if got := collector.Snapshot(); len(got) != 1 || got[0] != issue {
		t.Fatalf("Snapshot mutation changed collector state: %#v", got)
	}
}

func TestIssueCollectorIsSafeForConcurrentRecordAndSnapshot(t *testing.T) {
	collector := NewIssueCollector(256)
	var group sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := 0; index < 64; index++ {
				collector.Record(Issue{FileKind: FileKindInstitutionProfiles, Location: fmt.Sprintf("%d", worker*64+index), Code: IssueCodeProfileDocumentInvalid})
				_ = collector.Snapshot()
			}
		}(worker)
	}
	group.Wait()
	if got := len(collector.Snapshot()); got != 256 {
		t.Fatalf("stored %d unique issues, want capped 256", got)
	}
}
