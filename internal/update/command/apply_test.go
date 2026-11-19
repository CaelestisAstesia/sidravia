package command

import (
	"context"
	"errors"
	"testing"

	"sidravia/internal/update/github"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
)

func TestApplyErrorsPreserveSafeDomainAndRateLimitPriority(t *testing.T) {
	for _, item := range []struct {
		err  error
		code string
	}{
		{errors.Join(errSourceProof, github.ErrRateLimited), "rate_limited"},
		{errors.Join(errSourceProof, releases.ErrUntrusted), "source_proof_unavailable"},
		{github.ErrIntegrity, "integrity_failed"},
		{errors.Join(errSourceProof, context.Canceled), "cancelled"},
		{errManualRepair, "manual_repair_required"},
		{operations.ErrConflict, "update_busy"},
		{errStagingCleanup, "staging_cleanup_failed"},
	} {
		if got := applyErrorCode(item.err); got != item.code {
			t.Fatalf("%v: %s want %s", item.err, got, item.code)
		}
	}
}

func TestInstalledStatusDoesNotMutateCachedCandidateOrOfferReinstall(t *testing.T) {
	original := cached{Check: releases.CheckResult{State: "available", Candidate: &releases.Candidate{Version: "1.1.0", CanApply: true}}}
	current := projectInstalledVersion(original, "1.1.0")
	if current.Check.State != "up_to_date" || current.Check.Candidate.CanApply || current.Check.Candidate.ApplyReason != "not_newer" {
		t.Fatal(current)
	}
	if original.Check.State != "available" || !original.Check.Candidate.CanApply {
		t.Fatal("projection rewrote cached source evidence")
	}
	if ahead := projectInstalledVersion(original, "1.2.0"); ahead.Check.State != "ahead_of_channel" || ahead.Check.Candidate.CanApply {
		t.Fatal(ahead)
	}
}
