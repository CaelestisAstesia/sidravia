package session

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"sidravia/internal/daemon/authentication/protocol"
)

// publicFailure projects a protocol failure using its recommendation when the
// Session applies that recommendation unchanged.
func (session *AuthenticationSession) publicFailure(failure *protocol.AuthenticationProtocolRunFailure) *AuthenticationFailure {
	return session.publicFailureWithRecommendation(failure, failure.HandlingRecommendation)
}

// publicFailureWithRecommendation projects the safe protocol failure code and
// description together with the effective recommendation selected by Session
// policy. The input protocol failure remains unchanged for Session decisions.
func (session *AuthenticationSession) publicFailureWithRecommendation(
	failure *protocol.AuthenticationProtocolRunFailure,
	recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation,
) *AuthenticationFailure {
	forbidden := session.publicFailureForbiddenMaterial(failure)
	return &AuthenticationFailure{
		Code:                   protocol.AuthenticationProtocolFailureCode(safePublicFailureCode(string(failure.Code), forbidden)),
		Description:            safePublicFailureDescription(failure.Description, forbidden),
		HandlingRecommendation: safePublicFailureRecommendation(recommendation),
	}
}

func (session *AuthenticationSession) publicFailureForbiddenMaterial(failure *protocol.AuthenticationProtocolRunFailure) []string {
	values := []string{
		session.definition.AuthenticationCredential.Username,
		session.definition.AuthenticationCredential.Password,
		"",
		string(session.definition.Configuration.ProtocolContextOverride),
		string(session.definition.InstitutionProfile.InstitutionProtocolConfiguration),
	}
	if failure.DiagnosticCause != nil {
		values = append(values, diagnosticCauseText(failure.DiagnosticCause))
	}
	forbidden := values[:0]
	for _, value := range values {
		if value != "" {
			forbidden = append(forbidden, value)
		}
	}
	return forbidden
}

func diagnosticCauseText(cause error) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	return cause.Error()
}

func safePublicFailureCode(value string, forbidden []string) string {
	if validPublicFailureCode(value) && (knownPublicFailureCode(value) || !containsForbiddenPublicFailureMaterial(value, forbidden)) {
		return value
	}
	return firstSafePublicFailureFallback(forbidden,
		"authentication_protocol_failure",
		"protocol_failure",
		"failure",
	)
}

// These producer-independent machine constants disclose no credential even
// when a credential happens to equal part or all of one of their names.
func knownPublicFailureCode(value string) bool {
	switch value {
	case "quota_exceeded", "account_suspended", "account_expired", "too_many_addresses", "server_session_reset", "server_session_ended", "negotiation_unsupported", "network_timeout", "network_io_failed", "server_busy", "session_in_use",
		"credential_invalid", "insufficient_funds", "account_frozen", "binding_ip_mismatch",
		"binding_mac_mismatch", "too_many_sessions", "incompatible_version", "binding_pair_mismatch",
		"dhcp_required", "authentication_rejected", "protocol_response_invalid", "protocol_contract_violated",
		"logout_cleanup_failed", "protocol_run_creation_failed", "authentication_protocol_failure", "protocol_failure", "failure":
		return true
	default:
		return false
	}
}

func validPublicFailureCode(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func safePublicFailureDescription(value string, forbidden []string) string {
	if validPublicFailureDescription(value) && !containsForbiddenPublicFailureMaterial(value, forbidden) {
		return value
	}
	return firstSafePublicFailureFallback(forbidden,
		"Authentication protocol failure.",
		"Protocol failure.",
		"Authentication failed.",
	)
}

func validPublicFailureDescription(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 256 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func firstSafePublicFailureFallback(forbidden []string, fallbacks ...string) string {
	for _, fallback := range fallbacks {
		if !containsForbiddenPublicFailureMaterial(fallback, forbidden) {
			return fallback
		}
	}
	// Fixed text must remain nonempty; substring collisions with short secrets
	// are not disclosures of secret-derived text.
	if len(fallbacks) > 0 {
		return fallbacks[0]
	}
	return "failure"
}

func containsForbiddenPublicFailureMaterial(value string, forbidden []string) bool {
	for _, material := range forbidden {
		if material != "" && strings.Contains(value, material) {
			return true
		}
	}
	return false
}

// Recommendations are a finite trusted enum selected by Session policy, not
// diagnostic strings; credential text must not change that policy projection.
func safePublicFailureRecommendation(value protocol.AuthenticationProtocolFailureHandlingRecommendation) protocol.AuthenticationProtocolFailureHandlingRecommendation {
	if validPublicFailureRecommendation(value) {
		return value
	}
	return protocol.BlockUntilExplicitRestartOrRelevantInputChange
}

func validPublicFailureRecommendation(value protocol.AuthenticationProtocolFailureHandlingRecommendation) bool {
	return value == protocol.RetryAfterStandardDelay ||
		value == protocol.RetryAfterExtendedDelay ||
		value == protocol.RetryAfterBoundedRecovery ||
		value == protocol.BlockUntilExplicitRestartOrRelevantInputChange
}
