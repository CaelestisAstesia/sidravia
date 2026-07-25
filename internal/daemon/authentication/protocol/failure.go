package protocol

type AuthenticationProtocolFailureHandlingRecommendation string

const (
	RetryAfterStandardDelay                        AuthenticationProtocolFailureHandlingRecommendation = "retry_after_standard_delay"
	RetryAfterExtendedDelay                        AuthenticationProtocolFailureHandlingRecommendation = "retry_after_extended_delay"
	BlockUntilExplicitRestartOrRelevantInputChange AuthenticationProtocolFailureHandlingRecommendation = "block_until_explicit_restart_or_relevant_input_change"
)

type AuthenticationProtocolRunFailure struct {
	Code                   AuthenticationProtocolFailureCode
	Description            string
	HandlingRecommendation AuthenticationProtocolFailureHandlingRecommendation
	DiagnosticCause        error
}
