package configuration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
)

func profileCatalogTestProfile(id InstitutionProfileID, protoID protocol.AuthenticationProtocolID) InstitutionProfile {
	return InstitutionProfile{
		InstitutionProfileID:             id,
		DisplayName:                      string(id) + " display",
		AuthenticationProtocolID:         protoID,
		InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration(`{"realm":"` + string(id) + `"}`),
	}
}

func TestProfileCatalogGetListSummariesAndSorting(t *testing.T) {
	ctx := context.Background()
	profiles := []InstitutionProfile{
		profileCatalogTestProfile("profile-b", "drcom"),
		profileCatalogTestProfile("profile-a", "drcom"),
	}
	catalog, err := NewProfileCatalog(profiles)
	if err != nil {
		t.Fatalf("NewProfileCatalog() error = %v", err)
	}
	summaries, err := catalog.ListSummaries(ctx)
	if err != nil || len(summaries) != 2 {
		t.Fatalf("ListSummaries() = %#v, %v", summaries, err)
	}
	if summaries[0].InstitutionProfileID != "profile-a" || summaries[1].InstitutionProfileID != "profile-b" {
		t.Fatalf("ListSummaries() not sorted: %#v", summaries)
	}
	got, err := catalog.Get(ctx, "profile-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.AuthenticationProtocolID != "drcom" || got.DisplayName != "profile-a display" {
		t.Fatalf("Get() = %#v", got)
	}
	if string(got.InstitutionProtocolConfiguration) != `{"realm":"profile-a"}` {
		t.Fatalf("InstitutionProtocolConfiguration = %q", got.InstitutionProtocolConfiguration)
	}
}

func TestProfileCatalogGetReturnsDeepCopy(t *testing.T) {
	ctx := context.Background()
	catalog, err := NewProfileCatalog([]InstitutionProfile{
		profileCatalogTestProfile("profile-1", "drcom"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.Get(ctx, "profile-1")
	if err != nil {
		t.Fatal(err)
	}
	got.InstitutionProtocolConfiguration[0] = 'X'
	got.DisplayName = "mutated"
	again, err := catalog.Get(ctx, "profile-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.DisplayName != "profile-1 display" || again.InstitutionProtocolConfiguration[0] != '{' {
		t.Fatalf("internal state was mutated: %#v", again)
	}
}

func TestProfileCatalogRejectsInvalidCalls(t *testing.T) {
	ctx := context.Background()
	catalog, err := NewProfileCatalog([]InstitutionProfile{
		profileCatalogTestProfile("profile-1", "drcom"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Get(ctx, ""); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(empty) error = %v", err)
	}
	if _, err := catalog.Get(ctx, "missing"); profileCatalogFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get(missing) error = %v", err)
	}
	if _, err := catalog.Get(nil, "profile-1"); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(nil ctx) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := catalog.Get(canceled, "profile-1"); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(canceled) error = %v", err)
	}
	if _, err := catalog.ListSummaries(nil); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("ListSummaries(nil ctx) error = %v", err)
	}
}

func TestNewProfileCatalogRejectsInvalidProfiles(t *testing.T) {
	valid := profileCatalogTestProfile("profile-1", "drcom")
	cases := []struct {
		name    string
		profile InstitutionProfile
	}{
		{"empty id", InstitutionProfile{DisplayName: "x", AuthenticationProtocolID: "drcom"}},
		{"empty display name", InstitutionProfile{InstitutionProfileID: "profile-1", AuthenticationProtocolID: "drcom"}},
		{"empty protocol id", InstitutionProfile{InstitutionProfileID: "profile-1", DisplayName: "x"}},
	}
	for _, tc := range cases {
		_, err := NewProfileCatalog([]InstitutionProfile{tc.profile})
		if profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
			t.Fatalf("%s: NewProfileCatalog() error = %v", tc.name, err)
		}
	}
	duplicate := []InstitutionProfile{valid, valid}
	if _, err := NewProfileCatalog(duplicate); profileCatalogFailureCode(t, err) != persistence.FailureConflict {
		t.Fatalf("duplicate id error = %v", err)
	}
}

func TestProfileCatalogCanonicalProfileID(t *testing.T) {
	ctx := context.Background()
	valid := []InstitutionProfileID{"jlu", "profile-1", "a", "a1", "z-9"}
	for _, id := range valid {
		if err := validateInstitutionProfileID(id); err != nil {
			t.Fatalf("valid id %q rejected: %v", id, err)
		}
	}
	invalid := []InstitutionProfileID{
		"",
		"JLU",
		"jlu_campus",
		"jlu.json",
		"-jlu",
		"jlu-",
		"jlu--main",
		"1leading-digit",
	}
	for _, id := range invalid {
		if err := validateInstitutionProfileID(id); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
			t.Fatalf("invalid id %q should be invalid_argument, got %v", id, err)
		}
	}

	// Construction rejects a non-canonical ID even when the other fields are set.
	if _, err := NewProfileCatalog([]InstitutionProfile{
		profileCatalogTestProfile("JLU", "drcom"),
	}); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("NewProfileCatalog(JLU) error = %v", err)
	}

	catalog, err := NewProfileCatalog([]InstitutionProfile{
		profileCatalogTestProfile("jlu", "drcom"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A valid but absent identifier remains not_found.
	if _, err := catalog.Get(ctx, "profile-1"); profileCatalogFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get(valid absent) error = %v", err)
	}
	// An invalid lookup identifier is invalid_argument, not not_found.
	if _, err := catalog.Get(ctx, "JLU"); profileCatalogFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(JLU) error = %v", err)
	}
}

func TestProfileCatalogPublicErrorSecrecy(t *testing.T) {
	catalog, err := NewProfileCatalog([]InstitutionProfile{
		profileCatalogTestProfile("profile-secret", "drcom"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, getErr := catalog.Get(context.Background(), "missing")
	if strings.Contains(getErr.Error(), "realm") {
		t.Fatalf("public error leaked protocol config: %v", getErr)
	}
}

func profileCatalogFailureCode(t *testing.T, err error) persistence.FailureCode {
	t.Helper()
	var failure *persistence.Failure
	if errors.As(err, &failure) {
		return failure.Code()
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	t.Fatalf("error is not a persistence failure: %v", err)
	return ""
}
