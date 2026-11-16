package command

import (
	"context"
	"time"

	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
)

// Only a bounded status projection leaves the execution store. Signed proof,
// filesystem targets and private recovery inputs are not CLI presentation data.
type operationView struct {
	ID                   string           `json:"operationId"`
	Phase                operations.Phase `json:"phase"`
	SourceVersion        string           `json:"sourceVersion"`
	TargetVersion        string           `json:"targetVersion"`
	RequiresStartupBlock bool             `json:"requiresStartupBlock"`
	UpdatedAt            time.Time        `json:"updatedAt"`
	FailureCode          string           `json:"failureCode,omitempty"`
}
type statusResult struct {
	cached
	Operation *operationView `json:"operation,omitempty"`
}

func executionStatus(ctx context.Context, root, target string) (*operationView, error) {
	store, err := operations.Open(root, target, releases.PublisherTrust())
	if err != nil {
		return nil, err
	}
	return readExecutionStatus(ctx, store)
}
func readExecutionStatus(ctx context.Context, store *operations.Store) (*operationView, error) {
	r, exists, err := store.Read(ctx)
	if err != nil || !exists {
		return nil, err
	}
	return &operationView{ID: r.ID, Phase: r.Phase, SourceVersion: r.Source.Version, TargetVersion: r.Target.Version, RequiresStartupBlock: r.RequiresStartupBlock(), UpdatedAt: r.UpdatedAt, FailureCode: r.FailureCode}, nil
}

func composeStatus(previous cached, hasCheck bool, execution *operationView) statusResult {
	if !hasCheck {
		previous = cached{SchemaVersion: 1, Check: releases.CheckResult{State: "not_checked"}}
	}
	return statusResult{cached: previous, Operation: execution}
}
