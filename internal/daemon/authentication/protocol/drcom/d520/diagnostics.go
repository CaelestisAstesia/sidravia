package d520

// Stable diagnostic phase identifiers for D520 UDP exchanges. They are passed
// to the diagnostic sink and never derived from packet contents. The set
// matches the operator contract: challenge, login, bootstrap_ka1,
// bootstrap_ka2, keepalive_ka1, keepalive_ka2 and logout.
const (
	phaseChallenge    = "challenge"
	phaseLogin        = "login"
	phaseBootstrapKA1 = "bootstrap_ka1"
	phaseBootstrapKA2 = "bootstrap_ka2"
	phaseKeepaliveKA1 = "keepalive_ka1"
	phaseKeepaliveKA2 = "keepalive_ka2"
	phaseLogout       = "logout"
)

// Stable phase boundary markers recorded by the diagnostic sink at Debug level.
const (
	phaseBoundaryBegin = "begin"
	phaseBoundaryEnd   = "end"
)
