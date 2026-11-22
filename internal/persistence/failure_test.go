package persistence

import (
	"errors"
	"strings"
	"testing"
)

func TestFailureTextContainsOnlyStableCode(t *testing.T) {
	cause := errors.New(`path=C:\Users\alice\credentials.json username=alice password=hunter2 credentialID=cred-123 json={"token":"secret"}`)
	failure := NewFailure(FailureInvalidDocument, cause)
	if got, want := failure.Error(), "persistence failure: invalid_document"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	for _, secret := range []string{"C:\\Users", "alice", "hunter2", "cred-123", `{"token":"secret"}`} {
		if strings.Contains(failure.Error(), secret) {
			t.Fatalf("Error leaked diagnostic detail %q", secret)
		}
	}
	if failure.Code() != FailureInvalidDocument || failure.DiagnosticCause() != cause {
		t.Fatal("Failure accessors did not retain their stable code and diagnostic cause")
	}
}

func TestFailureDoesNotUnwrapDiagnosticCause(t *testing.T) {
	cause := errors.New("private diagnostic")
	failure := NewFailure(FailureInvalidDocument, cause)
	if errors.Is(failure, cause) {
		t.Fatal("Failure must not expose the diagnostic cause through errors.Is")
	}
	if _, ok := any(failure).(interface{ Unwrap() error }); ok {
		t.Fatal("Failure must not implement Unwrap")
	}
}
