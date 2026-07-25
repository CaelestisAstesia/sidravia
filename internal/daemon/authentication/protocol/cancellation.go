package protocol

type AuthenticationProtocolRunCleanupRequirement string

const (
	TerminateWithoutLogout        AuthenticationProtocolRunCleanupRequirement = "terminate_without_logout"
	TerminateWithBestEffortLogout AuthenticationProtocolRunCleanupRequirement = "terminate_with_best_effort_logout"
)

type AuthenticationProtocolRunCancellationCause struct {
	CleanupRequirement AuthenticationProtocolRunCleanupRequirement
	Description        string
}

func (cause AuthenticationProtocolRunCancellationCause) Error() string {
	return cause.Description
}
