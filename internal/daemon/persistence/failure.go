package persistence

type FailureCode string

const (
	FailureInvalidArgument          FailureCode = "invalid_argument"
	FailureInvalidDocument          FailureCode = "invalid_document"
	FailureUnsupportedSchemaVersion FailureCode = "unsupported_schema_version"
	FailureSizeLimitExceeded        FailureCode = "size_limit_exceeded"
	FailurePermissionDenied         FailureCode = "permission_denied"
	FailureAtomicWrite              FailureCode = "atomic_write_failure"
	FailureAmbiguousIdentity        FailureCode = "ambiguous_identity"
	FailureConflict                 FailureCode = "conflict"
	FailureNotFound                 FailureCode = "not_found"
)

type Failure struct {
	code            FailureCode
	diagnosticCause error
}

func NewFailure(code FailureCode, diagnosticCause error) *Failure {
	return &Failure{code: code, diagnosticCause: diagnosticCause}
}

func (failure *Failure) Code() FailureCode {
	return failure.code
}

func (failure *Failure) Error() string {
	return "persistence failure: " + string(failure.Code())
}

func (failure *Failure) DiagnosticCause() error {
	return failure.diagnosticCause
}
