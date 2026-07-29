package configuration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const profileLoaderTestProtocolID protocol.AuthenticationProtocolID = "test-protocol"

// profileLoaderTestFactory is a private AuthenticationProtocolFactory stub. It
// does not interpret protocol configuration; it counts validation calls and
// optionally rejects, so tests can prove the loader invokes the factory exactly
// once per file and preserves factory rejection causes. It never creates a run.
type profileLoaderTestFactory struct {
	protocolID    protocol.AuthenticationProtocolID
	validateCount *int
	rejectErr     error
}

func (f profileLoaderTestFactory) ProtocolID() protocol.AuthenticationProtocolID {
	return f.protocolID
}

func (f profileLoaderTestFactory) ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration) error {
	if f.validateCount != nil {
		*f.validateCount++
	}
	return f.rejectErr
}

func (f profileLoaderTestFactory) ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride) error {
	return nil
}

func (f profileLoaderTestFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return nil, errors.New("profileLoaderTestFactory does not create runs")
}

func newProfileLoaderTestRegistry(t *testing.T, factory profileLoaderTestFactory) *protocol.AuthenticationProtocolRegistry {
	t.Helper()
	if factory.protocolID == "" {
		factory.protocolID = profileLoaderTestProtocolID
	}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatalf("NewAuthenticationProtocolRegistry() error = %v", err)
	}
	return registry
}

func newProfileLoaderAcceptingRegistry(t *testing.T) (*protocol.AuthenticationProtocolRegistry, *int) {
	t.Helper()
	count := 0
	registry := newProfileLoaderTestRegistry(t, profileLoaderTestFactory{validateCount: &count})
	return registry, &count
}

func writeProfileFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
}

// profileLoaderDocument builds a version-1 Profile document with the given ID,
// display name and raw protocol configuration JSON object body (without the
// enclosing braces).
func profileLoaderDocument(id, displayName, configBody string) string {
	return `{"schemaVersion":1,"institutionProfileId":"` + id +
		`","displayName":"` + displayName +
		`","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) +
		`","institutionProtocolConfiguration":{` + configBody + `}}`
}

const profileLoaderDefaultConfigBody = `"endpoint":"203.0.113.10"`

func profileLoaderFailureCode(t *testing.T, err error) persistence.FailureCode {
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

func profileLoaderDiagnosticCause(t *testing.T, err error) error {
	t.Helper()
	var failure *persistence.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error is not a persistence failure: %v", err)
	}
	return failure.DiagnosticCause()
}

// 1. a missing directory returns an empty usable catalog.
func TestLoadProfileCatalogFromDirectoryMissingDirectory(t *testing.T) {
	ctx := context.Background()
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory(missing) error = %v", err)
	}
	summaries, err := catalog.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected empty catalog, got %#v", summaries)
	}
	if _, err := catalog.Get(ctx, "jlu"); profileLoaderFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get on empty catalog error = %v", err)
	}
}

// 3. two valid files load into sorted summaries, preserve their IDs/display
// names/protocol IDs and raw protocol configurations, and invoke factory
// validation once per file.
func TestLoadProfileCatalogFromDirectoryLoadsValidProfiles(t *testing.T) {
	ctx := context.Background()
	registry, validateCount := newProfileLoaderAcceptingRegistry(t)
	dir := t.TempDir()

	writeProfileFile(t, dir, "profile-b.json", profileLoaderDocument("profile-b", "B 大学", `"endpoint":"203.0.113.11"`))
	writeProfileFile(t, dir, "profile-a.json", profileLoaderDocument("profile-a", "A 大学", `"endpoint":"203.0.113.10"`))

	catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
	}
	if *validateCount != 2 {
		t.Fatalf("factory validation called %d times, want 2", *validateCount)
	}

	summaries, err := catalog.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 2 ||
		summaries[0].InstitutionProfileID != "profile-a" ||
		summaries[1].InstitutionProfileID != "profile-b" {
		t.Fatalf("summaries not sorted or wrong: %#v", summaries)
	}
	if summaries[0].DisplayName != "A 大学" || summaries[0].AuthenticationProtocolID != profileLoaderTestProtocolID {
		t.Fatalf("summary[0] = %#v", summaries[0])
	}

	a, err := catalog.Get(ctx, "profile-a")
	if err != nil {
		t.Fatalf("Get(profile-a) error = %v", err)
	}
	if a.DisplayName != "A 大学" || a.AuthenticationProtocolID != profileLoaderTestProtocolID {
		t.Fatalf("Get(profile-a) = %#v", a)
	}
	if string(a.InstitutionProtocolConfiguration) != `{"endpoint":"203.0.113.10"}` {
		t.Fatalf("raw protocol configuration not preserved: %q", a.InstitutionProtocolConfiguration)
	}
}

// 4. mutating source test buffers or a value returned by Get cannot mutate the
// catalog.
func TestLoadProfileCatalogFromDirectoryCatalogIsImmutable(t *testing.T) {
	ctx := context.Background()
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	dir := t.TempDir()
	writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))

	catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
	}

	got, err := catalog.Get(ctx, "jlu")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got.DisplayName = "mutated"
	got.InstitutionProtocolConfiguration[0] = 'X'

	again, err := catalog.Get(ctx, "jlu")
	if err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	if again.DisplayName != "JLU" || again.InstitutionProtocolConfiguration[0] != '{' {
		t.Fatalf("catalog was mutated through Get: %#v", again)
	}
}

// 5. exact filename/ID matching and the canonical ID examples.
func TestLoadProfileCatalogFromDirectoryFilenameAndIDRules(t *testing.T) {
	ctx := context.Background()

	t.Run("valid ids load", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		writeProfileFile(t, dir, "profile-1.json", profileLoaderDocument("profile-1", "Profile 1", profileLoaderDefaultConfigBody))
		catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if err != nil {
			t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
		}
		summaries, _ := catalog.ListSummaries(ctx)
		if len(summaries) != 2 {
			t.Fatalf("expected 2 profiles, got %#v", summaries)
		}
	})

	t.Run("non-canonical id fails", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		for _, id := range []string{"JLU", "jlu_campus", "jlu.json", "-jlu", "jlu-", "jlu--main"} {
			dir := t.TempDir()
			writeProfileFile(t, dir, "jlu.json", profileLoaderDocument(id, "JLU", profileLoaderDefaultConfigBody))
			_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
			if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
				t.Fatalf("id %q: code = %v, want invalid_document", id, code)
			}
		}
	})

	t.Run("filename must equal id plus json", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		// The ID inside is canonical "jlu" but the file is named "jlu-campus.json".
		writeProfileFile(t, dir, "jlu-campus.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
			t.Fatalf("filename mismatch: code = %v, want invalid_document", code)
		}
	})

	t.Run("non-json and uppercase suffix files are ignored", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		writeProfileFile(t, dir, "jlu.json.txt", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		writeProfileFile(t, dir, "jlu.JSON", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		writeProfileFile(t, dir, "readme.txt", "ignore me")
		catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if err != nil {
			t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
		}
		summaries, _ := catalog.ListSummaries(ctx)
		if len(summaries) != 1 || summaries[0].InstitutionProfileID != "jlu" {
			t.Fatalf("expected only jlu to load, got %#v", summaries)
		}
	})
}

// 6. a valid but absent ID remains not_found, while an invalid lookup ID is
// invalid_argument.
func TestLoadProfileCatalogFromDirectoryLookupCodes(t *testing.T) {
	ctx := context.Background()
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	dir := t.TempDir()
	writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
	catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
	}
	if _, err := catalog.Get(ctx, "profile-1"); profileLoaderFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get(valid absent) error = %v", err)
	}
	if _, err := catalog.Get(ctx, "JLU"); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(invalid lookup) error = %v", err)
	}
}

// 7. representative strict failures.
func TestLoadProfileCatalogFromDirectoryStrictFailures(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		body string
		code persistence.FailureCode
	}{
		{
			"missing field",
			`{"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) + `"}`,
			persistence.FailureInvalidDocument,
		},
		{
			"unknown root field",
			`{"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) + `","institutionProtocolConfiguration":{},"extra":1}`,
			persistence.FailureInvalidDocument,
		},
		{
			"duplicate root key",
			`{"schemaVersion":1,"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) + `","institutionProtocolConfiguration":{}}`,
			persistence.FailureInvalidDocument,
		},
		{
			"duplicate nested protocol key",
			profileLoaderDocument("jlu", "JLU", `"endpoint":"1.2.3.4","endpoint":"5.6.7.8"`),
			persistence.FailureInvalidDocument,
		},
		{
			"trailing json",
			profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody) + `{"extra":1}`,
			persistence.FailureInvalidDocument,
		},
		{
			"null document",
			`null`,
			persistence.FailureInvalidDocument,
		},
		{
			"non-object document",
			`[1,2,3]`,
			persistence.FailureInvalidDocument,
		},
		{
			"unsupported schema",
			`{"schemaVersion":2,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) + `","institutionProtocolConfiguration":{}}`,
			persistence.FailureUnsupportedSchemaVersion,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry, _ := newProfileLoaderAcceptingRegistry(t)
			dir := t.TempDir()
			writeProfileFile(t, dir, "jlu.json", tc.body)
			_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
			if code := profileLoaderFailureCode(t, err); code != tc.code {
				t.Fatalf("code = %v, want %v (err=%v)", code, tc.code, err)
			}
		})
	}
}

// 8. unknown protocol and factory rejection fail the entire load without
// returning a catalog.
func TestLoadProfileCatalogFromDirectoryUnknownProtocolAndFactoryRejection(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown protocol", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", `{"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"unknown-protocol","institutionProtocolConfiguration":{}}`)
		catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if err == nil {
			t.Fatalf("expected error, got catalog %#v", catalog)
		}
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
			t.Fatalf("code = %v, want invalid_document", code)
		}
	})

	t.Run("factory rejection fails entire load", func(t *testing.T) {
		count := 0
		registry := newProfileLoaderTestRegistry(t, profileLoaderTestFactory{
			validateCount: &count,
			rejectErr:     errors.New("factory rejected configuration"),
		})
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		writeProfileFile(t, dir, "profile-1.json", profileLoaderDocument("profile-1", "Profile 1", profileLoaderDefaultConfigBody))
		catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if err == nil {
			t.Fatalf("expected error, got catalog %#v", catalog)
		}
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
			t.Fatalf("code = %v, want invalid_document", code)
		}
		// The first rejected file aborts the load; only one factory call happened.
		if count != 1 {
			t.Fatalf("factory validation called %d times, want 1", count)
		}
	})
}

// 9. the exact 1 MiB limit is accepted when the document itself is valid and one
// byte over is size_limit_exceeded; constructed deterministically without fixture
// files.
func TestLoadProfileCatalogFromDirectorySizeLimit(t *testing.T) {
	ctx := context.Background()

	buildSizedDocument := func(size int) []byte {
		prefix := `{"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"` + string(profileLoaderTestProtocolID) + `","institutionProtocolConfiguration":{"pad":"`
		suffix := `"}}`
		padding := size - len(prefix) - len(suffix)
		if padding < 0 {
			panic("requested size smaller than document frame")
		}
		data := append([]byte(prefix), bytes.Repeat([]byte("x"), padding)...)
		return append(data, suffix...)
	}

	t.Run("exact limit accepted", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		data := buildSizedDocument(int(jsonfile.ProfileFileSizeLimit))
		if len(data) != int(jsonfile.ProfileFileSizeLimit) {
			t.Fatalf("constructed document is %d bytes, want %d", len(data), jsonfile.ProfileFileSizeLimit)
		}
		if err := os.WriteFile(filepath.Join(dir, "jlu.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		catalog, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if err != nil {
			t.Fatalf("LoadProfileCatalogFromDirectory() error = %v", err)
		}
		summaries, _ := catalog.ListSummaries(ctx)
		if len(summaries) != 1 {
			t.Fatalf("expected 1 profile, got %#v", summaries)
		}
	})

	t.Run("one byte over rejected", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		data := buildSizedDocument(int(jsonfile.ProfileFileSizeLimit) + 1)
		if err := os.WriteFile(filepath.Join(dir, "jlu.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureSizeLimitExceeded {
			t.Fatalf("code = %v, want size_limit_exceeded", code)
		}
	})
}

// 10. canceled/nil context, relative/empty directory and nil registry are
// invalid_argument.
func TestLoadProfileCatalogFromDirectoryInvalidCalls(t *testing.T) {
	ctx := context.Background()
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	dir := t.TempDir()

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := LoadProfileCatalogFromDirectory(canceled, dir, registry); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("canceled ctx error = %v", err)
	}
	if _, err := LoadProfileCatalogFromDirectory(nil, dir, registry); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("nil ctx error = %v", err)
	}
	if _, err := LoadProfileCatalogFromDirectory(ctx, "relative/path", registry); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("relative dir error = %v", err)
	}
	if _, err := LoadProfileCatalogFromDirectory(ctx, "", registry); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("empty dir error = %v", err)
	}
	if _, err := LoadProfileCatalogFromDirectory(ctx, dir, nil); profileLoaderFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("nil registry error = %v", err)
	}
}

// 11. one representative irregular .json entry is rejected without following it,
// on platforms where the test can create it without privileges.
func TestLoadProfileCatalogFromDirectoryRejectsIrregularFile(t *testing.T) {
	ctx := context.Background()
	registry, _ := newProfileLoaderAcceptingRegistry(t)
	dir := t.TempDir()

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "bad.json")
	if err := os.Symlink(target, link); err != nil {
		// Platforms that cannot create a symlink without privileges skip this
		// representative case rather than failing.
		t.Skipf("cannot create symlink: %v", err)
	}

	_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
	if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
		t.Fatalf("irregular .json entry: code = %v, want invalid_document (err=%v)", code, err)
	}
	if strings.Contains(err.Error(), "target") {
		t.Fatalf("public error followed or named the symlink target: %v", err)
	}
}

// 12. public errors do not contain an injected secret JSON marker, full temporary
// directory, injected protocol ID, or injected factory error marker, while the
// returned persistence failure retains the diagnostic cause.
func TestLoadProfileCatalogFromDirectoryPublicErrorSecrecy(t *testing.T) {
	ctx := context.Background()
	const (
		secretMarker     = "SECRET-MARKER-VALUE"
		factoryMarker    = "FACTORY-REJECTION-MARKER"
		leakedProtocolID = "leaked-protocol-id"
	)

	t.Run("unknown protocol hides protocol id and secret", func(t *testing.T) {
		registry, _ := newProfileLoaderAcceptingRegistry(t)
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", `{"schemaVersion":1,"institutionProfileId":"jlu","displayName":"JLU","authenticationProtocolId":"`+leakedProtocolID+`","institutionProtocolConfiguration":{"secret":"`+secretMarker+`"}}`)
		_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
			t.Fatalf("code = %v, want invalid_document", code)
		}
		publicErr := err.Error()
		if strings.Contains(publicErr, leakedProtocolID) {
			t.Fatalf("public error leaked protocol id: %v", publicErr)
		}
		if strings.Contains(publicErr, secretMarker) {
			t.Fatalf("public error leaked secret marker: %v", publicErr)
		}
		if strings.Contains(publicErr, dir) {
			t.Fatalf("public error leaked full temp directory: %v", publicErr)
		}
		cause := profileLoaderDiagnosticCause(t, err)
		if !strings.Contains(cause.Error(), leakedProtocolID) {
			t.Fatalf("diagnostic cause lost the protocol id: %v", cause)
		}
	})

	t.Run("factory rejection hides factory error marker", func(t *testing.T) {
		registry := newProfileLoaderTestRegistry(t, profileLoaderTestFactory{
			rejectErr: errors.New(factoryMarker),
		})
		dir := t.TempDir()
		writeProfileFile(t, dir, "jlu.json", profileLoaderDocument("jlu", "JLU", profileLoaderDefaultConfigBody))
		_, err := LoadProfileCatalogFromDirectory(ctx, dir, registry)
		if code := profileLoaderFailureCode(t, err); code != persistence.FailureInvalidDocument {
			t.Fatalf("code = %v, want invalid_document", code)
		}
		publicErr := err.Error()
		if strings.Contains(publicErr, factoryMarker) {
			t.Fatalf("public error leaked factory error marker: %v", publicErr)
		}
		if strings.Contains(publicErr, dir) {
			t.Fatalf("public error leaked full temp directory: %v", publicErr)
		}
		cause := profileLoaderDiagnosticCause(t, err)
		if !strings.Contains(cause.Error(), factoryMarker) {
			t.Fatalf("diagnostic cause lost the factory error marker: %v", cause)
		}
	})
}
