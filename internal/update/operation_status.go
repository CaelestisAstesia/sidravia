package update

import (
	"context"
	"time"

	"sidravia/internal/update/internal/candidatecache"

	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
)

// Only a bounded status projection leaves the execution store. Signed proof,
// filesystem targets and private recovery inputs are not CLI presentation data.
type installerView struct {
	ExitCode        *uint32 `json:"exitCode,omitempty"`
	Integration     string  `json:"integration"`
	RestartRequired bool    `json:"restartRequired"`
}

type operationView struct {
	Installer            *installerView    `json:"installer,omitempty"`
	ID                   string            `json:"operationId"`
	Phase                transaction.Phase `json:"phase"`
	SourceVersion        string            `json:"sourceVersion"`
	TargetVersion        string            `json:"targetVersion"`
	RequiresStartupBlock bool              `json:"requiresStartupBlock"`
	UpdatedAt            time.Time         `json:"updatedAt"`
	FailureCode          string            `json:"failureCode,omitempty"`
}
type statusResult struct {
	candidatecache.State
	Operation *operationView `json:"operation,omitempty"`
}

func executionStatus(ctx context.Context, root, target string) (*operationView, error) {
	store, err := transaction.Open(root, target, release.PublisherTrust())
	if err != nil {
		return nil, err
	}
	return readExecutionStatus(ctx, store)
}
func readExecutionStatus(ctx context.Context, store *transaction.Store) (*operationView, error) {
	r, exists, err := store.Read(ctx)
	if err != nil || !exists {
		return nil, err
	}
	installer, e := installerStatus(ctx, r)
	if e != nil {
		return nil, e
	}
	return &operationView{Installer: installer, ID: r.ID, Phase: r.Phase, SourceVersion: r.Source.Version, TargetVersion: r.Target.Version, RequiresStartupBlock: r.RequiresStartupBlock(), UpdatedAt: r.UpdatedAt, FailureCode: r.FailureCode}, nil
}

func composeStatus(previous candidatecache.State, hasCheck bool, execution *operationView) statusResult {
	if !hasCheck {
		previous = candidatecache.State{SchemaVersion: 1, Check: release.CheckResult{State: "not_checked"}}
	}
	return statusResult{State: previous, Operation: execution}
}
