package credential

// AuthenticationCredential carries the username and password into authentication.
// It is not a public snapshot or an IPC result.
type AuthenticationCredential struct {
	Username string
	Password string
}
