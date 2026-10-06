package jsonfile

import (
	"context"
	"errors"
	"testing"

	"sidravia/internal/daemon/persistence"
)

func TestUnsupportedProtectionFallbackIsPermanentAndWarnsOnce(t *testing.T) {
	operations := &fakeSecureFileOperations{
		ensureErr:    errors.Join(ProtectionUnsupported, errors.New("unsupported")),
		commitResult: true,
	}
	warnings := 0
	store, err := newSecureStoreWithOperations(SecureStoreOptions{
		AllowUnsupportedProtectionFallback: true,
		OnUnprotected:                      func() { warnings++ },
	}, operations)
	if err != nil {
		t.Fatal(err)
	}
	operations.calls = nil
	if err := store.Replace(context.Background(), testDestination(t), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(context.Background(), testDestination(t), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if store.ProtectionStatus() != ProtectionUnprotected || warnings != 1 {
		t.Fatalf("status/warnings = %q/%d", store.ProtectionStatus(), warnings)
	}
}

func TestStrictStoreAndGenericFailuresNeverDowngrade(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback bool
		err      error
	}{
		{"installed unsupported", false, errors.Join(ProtectionUnsupported, errors.New("unsupported"))},
		{"portable generic", true, errors.New("denied")},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := &fakeSecureFileOperations{ensureErr: test.err}
			store, err := newSecureStoreWithOperations(SecureStoreOptions{AllowUnsupportedProtectionFallback: test.fallback}, operations)
			if err != nil {
				t.Fatal(err)
			}
			err = store.Replace(context.Background(), testDestination(t), []byte(`{}`))
			if err == nil || store.ProtectionStatus() != ProtectionProtected {
				t.Fatalf("Replace/status = %v/%q", err, store.ProtectionStatus())
			}
		})
	}
}

func TestSensitiveFallbackRequiresAuthorizationBeforeTemp(t *testing.T) {
	operations := &fakeSecureFileOperations{
		ensureErr: errors.Join(ProtectionUnsupported, errors.New("unsupported")),
	}
	store, err := newSecureStoreWithOperations(SecureStoreOptions{AllowUnsupportedProtectionFallback: true}, operations)
	if err != nil {
		t.Fatal(err)
	}
	err = store.ReplaceSensitive(context.Background(), testDestination(t), []byte(`{"password":""}`), 0, false)
	var failure *persistence.Failure
	if !errors.As(err, &failure) || !errors.Is(failure.DiagnosticCause(), ErrInsecureStorageConfirmationRequired) {
		t.Fatalf("ReplaceSensitive() = %v", err)
	}
	if store.ProtectionStatus() != ProtectionProtected || operations.temp != nil {
		t.Fatalf("status/temp = %q/%v", store.ProtectionStatus(), operations.temp)
	}
}

func TestSensitiveRewriteDowngradeRequiresConsentAndKeepsOldDestination(t *testing.T) {
	operations := &fakeSecureFileOperations{commitResult: true}
	store, err := newSecureStoreWithOperations(SecureStoreOptions{AllowUnsupportedProtectionFallback: true}, operations)
	if err != nil {
		t.Fatal(err)
	}
	path := testDestination(t)
	if err = store.ReplaceSensitive(context.Background(), path, []byte(`{"password":"old"}`), 1024, false); err != nil {
		t.Fatal(err)
	}
	before := string(operations.destination)
	operations.ensureErr = ProtectionUnsupported
	var failure *persistence.Failure
	if err = store.ReplaceSensitive(context.Background(), path, []byte(`{"password":"new"}`), 1024, false); !errors.As(err, &failure) || !errors.Is(failure.DiagnosticCause(), ErrInsecureStorageConfirmationRequired) {
		t.Fatal("downgrade allowed")
	}
	if string(operations.destination) != before || store.ProtectionStatus() != ProtectionProtected {
		t.Fatal("rejected downgrade changed state")
	}
	if err = store.ReplaceSensitive(context.Background(), path, []byte(`{"password":"new"}`), 1024, true); err != nil {
		t.Fatal(err)
	}
	if store.ProtectionStatus() != ProtectionUnprotected {
		t.Fatal("consented fallback not recorded")
	}
}
