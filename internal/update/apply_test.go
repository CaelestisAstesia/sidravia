package update

import (
	"context"
	"errors"
	"testing"

	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/query"

	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/transaction"
)

func TestApplyErrorsPreserveSafeDomainAndRateLimitPriority(t *testing.T) {
	for _, item := range []struct {
		err  error
		code string
	}{
		{errors.Join(errSourceProof, github.ErrRateLimited), "rate_limited"},
		{errors.Join(errSourceProof, release.ErrUntrusted), "source_proof_unavailable"},
		{github.ErrIntegrity, "integrity_failed"},
		{errors.Join(errSourceProof, context.Canceled), "cancelled"},
		{errManualRepair, "manual_repair_required"},
		{transaction.ErrConflict, "update_busy"},
		{errStagingCleanup, "staging_cleanup_failed"},
	} {
		if got := applyErrorCode(item.err); got != item.code {
			t.Fatalf("%v: %s want %s", item.err, got, item.code)
		}
	}
}

func TestInstalledStatusDoesNotMutateCachedCandidateOrOfferReinstall(t *testing.T) {
	original := candidatecache.State{Check: release.CheckResult{State: "available", Candidate: &release.Candidate{Version: "1.1.0", CanApply: true}}}
	current := projectInstalledVersion(original, "1.1.0")
	if current.Check.State != "up_to_date" || current.Check.Candidate.CanApply || current.Check.Candidate.ApplyReason != "not_newer" {
		t.Fatal(current)
	}
	if original.Check.State != "available" || !original.Check.Candidate.CanApply {
		t.Fatal("projection rewrote candidatecache.State source evidence")
	}
	if ahead := projectInstalledVersion(original, "1.2.0"); ahead.Check.State != "ahead_of_channel" || ahead.Check.Candidate.CanApply {
		t.Fatal(ahead)
	}
}

func TestApplyOfferRetainsReasonPriority(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason                        string
		verified, layoutEligible, platformEligible bool
		canApply                                   bool
	}{
		{"wrong layout", "available", "portable_layout_required", false, false, true, false},
		{"unsigned", "up_to_date", "publisher_signature_required", false, true, true, false},
		{"already current", "up_to_date", "not_newer", true, true, false, false},
		{"ahead of channel", "ahead_of_channel", "not_newer", true, true, true, false},
		{"unsupported platform", "available", "platform_not_supported", true, true, false, false},
		{"eligible", "available", "source_verification_and_confirmation_required", true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := &release.Candidate{CanApply: true}
			result := release.CheckResult{State: tc.state, Candidate: candidate, PublisherVerified: tc.verified}
			query.ProjectApplyOffer(result, tc.layoutEligible, tc.platformEligible)
			if candidate.CanApply != tc.canApply || candidate.ApplyReason != tc.reason {
				t.Fatalf("canApply=%v, reason=%q", candidate.CanApply, candidate.ApplyReason)
			}
		})
	}
}
