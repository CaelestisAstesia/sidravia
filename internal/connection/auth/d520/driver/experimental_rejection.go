package driver

import "bytes"

// Only structural, confirmed categories leave the parser. Peer text, URLs and
// binding addresses are deliberately not retained, even in diagnostic causes.
type rejectionReason uint8

const (
	rejectionUnrecognized rejectionReason = iota
	rejectionKnownCode
	rejectionQuota
	rejectionFunds
	rejectionSuspended
	rejectionExpired
	rejectionAddressLimit
	rejectionVersion
	rejectionIPDetail
	rejectionMACDetail
	rejectionSessionDetail
)

func experimentalRejectionError(p []byte) *runError {
	failure := &runError{operation: "login", kind: kindExperimentalRejection, reason: rejectionUnrecognized}
	if len(p) < 5 || len(p) > maxResponseDatagramLength || p[0] != 5 {
		return failure
	}
	failure.wireCode = p[4]
	failure.hasWireCode = true
	switch p[4] {
	case 1:
		if len(p) == 5 {
			failure.reason = rejectionKnownCode
		} else if len(p) >= 15 {
			failure.reason = rejectionSessionDetail
		}
	case 2, 3, 0x16, 0x17:
		failure.reason = rejectionKnownCode
	case 4:
		failure.reason = rejectionQuota
		if len(p) >= 10 {
			failure.reason = rejectionFunds
		}
	case 5:
		failure.reason = rejectionSuspended
		if len(p) >= 6 {
			failure.reason = rejectionExpired
		}
	case 7:
		if len(p) == 5 {
			failure.reason = rejectionKnownCode
		} else if len(p) >= 9 {
			failure.reason = rejectionIPDetail
		}
	case 0x0b:
		if len(p) == 5 {
			failure.reason = rejectionKnownCode
		} else if len(p) >= 11 {
			failure.reason = rejectionMACDetail
		}
	case 0x14:
		failure.reason = rejectionAddressLimit
	case 0x15:
		if len(p) <= 20 {
			failure.reason = rejectionVersion
			break
		}
		// A terminated reason begins at offset 20. A nonempty reason is arbitrary
		// server text, not evidence of a password, balance or account-policy cause.
		text := p[20:]
		end := bytes.IndexByte(text, 0)
		if end < 0 || end > 511 {
			break
		}
		rest := text[end+1:]
		if len(rest) > 0 {
			urlEnd := bytes.IndexByte(rest, 0)
			if urlEnd < 0 || urlEnd >= 180 {
				break
			}
		}
		if end == 0 {
			failure.reason = rejectionVersion
		}
	}
	return failure
}

func (e *runError) experimentalRejectionDescription() (string, string) {
	switch e.reason {
	case rejectionSessionDetail:
		return "session_in_use", "The server reports an existing account session at another endpoint."
	case rejectionKnownCode:
		entry := knownRejectionTable[e.wireCode]
		return entry.code, entry.description
	case rejectionQuota:
		return "quota_exceeded", "The server reports that the account time or traffic quota is exhausted."
	case rejectionFunds:
		return "insufficient_funds", "The server reports insufficient funds for paid network access."
	case rejectionSuspended:
		return "account_suspended", "The server reports that account access is suspended."
	case rejectionExpired:
		return "account_expired", "The server reports that the account validity period has ended."
	case rejectionAddressLimit:
		return "too_many_addresses", "The server rejected the number of reported IPv4 addresses."
	case rejectionVersion:
		return "incompatible_version", "The server requires a different client protocol version."
	case rejectionIPDetail:
		return "binding_ip_mismatch", "The server requires an institution-bound IPv4 address. Check the account binding."
	case rejectionMACDetail:
		return "binding_mac_mismatch", "The server requires an institution-bound MAC address. Check the account binding."
	default:
		return "authentication_rejected", "Authentication was rejected; the server detail has no confirmed local classification."
	}
}
