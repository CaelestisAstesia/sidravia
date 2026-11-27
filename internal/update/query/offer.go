package query

import (
	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"
)

func PortableDownloadOnly() (code, message string) {
	failure := request.PortableDownloadOnly()
	return failure.Code, failure.Message
}

// ProjectApplyOffer sets the public hint. Apply independently checks admission,
// confirmation, and proofs for both the installed source and the candidate.
func ProjectApplyOffer(result release.CheckResult, layoutEligible, platformEligible bool) {
	candidate := result.Candidate
	if candidate == nil {
		return
	}
	candidate.CanApply = false
	switch {
	case !layoutEligible:
		candidate.ApplyReason = "installed_setup_required"
	case !result.PublisherVerified:
		candidate.ApplyReason = "publisher_signature_required"
	case result.State != "available":
		candidate.ApplyReason = "not_newer"
	case !platformEligible:
		candidate.ApplyReason = "platform_not_supported"
	default:
		candidate.CanApply = true
		candidate.ApplyReason = "source_verification_and_confirmation_required"
	}
}
