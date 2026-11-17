//go:build windows

package command

import (
	"context"
	"sidravia/internal/update/portable"
	"testing"
)

func TestCommittedPortableHandoffDoesNotClaimCancellation(t *testing.T) {
	err := &portable.HandoffCommitError{Cause: context.Canceled}
	if code := applyErrorCode(err); code != "handoff_unconfirmed" {
		t.Fatal(code)
	}
}
