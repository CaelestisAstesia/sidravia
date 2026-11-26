package protocol

type AuthenticationProtocolFailureHandlingRecommendation string

const (
	RetryAfterStandardDelay                        AuthenticationProtocolFailureHandlingRecommendation = "retry_after_standard_delay"
	RetryAfterExtendedDelay                        AuthenticationProtocolFailureHandlingRecommendation = "retry_after_extended_delay"
	RetryAfterBoundedRecovery                      AuthenticationProtocolFailureHandlingRecommendation = "retry_after_bounded_recovery"
	BlockUntilExplicitRestartOrRelevantInputChange AuthenticationProtocolFailureHandlingRecommendation = "block_until_explicit_restart_or_relevant_input_change"
)

type AuthenticationProtocolRunFailure struct {
	Code                   AuthenticationProtocolFailureCode
	Description            string
	HandlingRecommendation AuthenticationProtocolFailureHandlingRecommendation
	DiagnosticCause        error
	// RetainRecoveryBudget applies only to bounded recovery. Once requested,
	// successful establishment alone does not replenish Session's existing
	// budget; explicit restart or a relevant input change still resets it.
	// This is an internal policy hint, not peer text or an IPC field.
	RetainRecoveryBudget bool
}
