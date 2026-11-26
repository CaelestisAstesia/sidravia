//go:build linux

package update

import (
	"context"
	"errors"
	"os"
	"sidravia/internal/update/internal/candidatecache"

	"sidravia/internal/installation/change"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/portable"
	"sidravia/internal/update/transaction"
)

func platformApplyErrorCode(err error) string {
	switch {
	case errors.Is(err, portable.ErrRolledBack):
		return "update_rolled_back"
	case errors.Is(err, portable.ErrRestore):
		return "resume_failed"
	case errors.Is(err, portable.ErrPreparation):
		return "preparation_rejected"
	case errors.Is(err, portable.ErrChanged):
		return "target_changed"
	case errors.Is(err, portable.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, portable.ErrInvalid), errors.Is(err, transaction.ErrInvalid):
		return "invalid_metadata"
	}
	return "apply_failed"
}
func applyDownloaded(ctx context.Context, root string, layout productlayout.Layout, version, build string, source *github.GitHub, candidate release.Candidate, path string, releaseRuntime func()) (outcome *applyResult, err error) {
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return nil, err
	}
	exists, err := candidatecache.Inspect(root, path, &candidate)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, github.ErrIntegrity
	}
	opRoot, err := productlayout.OperationRoot()
	if err != nil {
		return nil, err
	}
	store, err := transaction.Open(opRoot, layout.ExecutableDirectory, release.PublisherTrust())
	if err != nil {
		return nil, err
	}
	if r, exists, err := store.Read(ctx); err != nil {
		return nil, err
	} else if exists {
		if r.RequiresStartupBlock() {
			return operationResult(ctx, store), transaction.ErrConflict
		}
		if r.Recovery != nil && r.Recovery.ManagedService {
			if pending, err := portable.ManagedRollbackPending(layout.ExecutableDirectory, r.ID); pending || err != nil {
				return operationResult(ctx, store), errors.Join(transaction.ErrConflict, err)
			}
		}
	}
	oldProof, oldAsset, oldIndex, err := installedProof(ctx, source, layout, version, build)
	if err != nil {
		return nil, err
	}
	proof, err := source.AuthenticateCandidate(ctx, candidate)
	if err != nil {
		return nil, err
	}
	installed := portable.ReleaseInput{Proof: oldProof, Candidate: oldAsset, Index: oldIndex}
	target := change.Target{ID: layout.ExecutableDirectory, Version: version, BuildID: build, Layout: "portable"}
	inspect, err := portable.ApplyRelease(ctx, store, target, installed, proof, candidate, path, ns.String(), releaseRuntime)
	if inspect {
		return operationResult(context.WithoutCancel(ctx), store), err
	}
	return nil, err
}

func recoverOperation(ctx context.Context, layout productlayout.Layout, version, build, id string, source *github.GitHub) (*applyResult, error) {
	if layout.Mode != productlayout.ModePortable || !change.ValidID(id) {
		return nil, transaction.ErrInvalid
	}
	root, err := productlayout.OperationRoot()
	if err != nil {
		return nil, err
	}
	store, err := transaction.Open(root, layout.ExecutableDirectory, release.PublisherTrust())
	if err != nil {
		return nil, err
	}
	record, exists, err := store.Read(ctx)
	if err != nil {
		return nil, err
	}
	if !exists || record.ID != id {
		return nil, errOperation
	}
	result := func(err error) (*applyResult, error) { return operationResult(context.WithoutCancel(ctx), store), err }
	if record.Recovery != nil && record.Recovery.ManagedService {
		switch record.Phase {
		case transaction.Applying, transaction.RepairRequired, transaction.Applied, transaction.ServiceStarting, transaction.RollingBack, transaction.RolledBack, transaction.Completed, transaction.Aborted:
			return result(portable.RecoverManagedService(ctx, store, id, layout.ExecutableDirectory))
		}
	}
	switch record.Phase {
	case transaction.Applying, transaction.RepairRequired:
		return result(errManualRepair)
	case transaction.Completed, transaction.Aborted, transaction.ResumeFailed:
		return result(nil)
	}
	expected := record.Source
	checksTarget := record.Phase == transaction.Resuming || record.Phase == transaction.Applied || record.Phase == transaction.ServiceStarting
	if checksTarget {
		expected = record.Target
	}
	if version != expected.Version || build != expected.BuildID {
		return result(errSourceProof)
	}
	var proof release.VerifiedRelease
	var c release.Candidate
	var index []byte
	if checksTarget {
		// Recovery must authenticate the encoding recorded by this operation.
		// The installed-source gzip counterpart has identical files but a
		// different archive hash/name and cannot replace the original proof.
		c = release.Candidate{ID: record.Package.CandidateID, Version: record.Target.Version, Name: record.Package.Name, Size: record.Package.Size, SHA256: record.Package.SHA256}
		c.DownloadURL = "https://github.com/" + release.Repository + "/releases/download/v" + c.Version + "/" + c.Name
		proof, err = source.AuthenticateCandidate(ctx, c)
		if err != nil {
			if errors.Is(err, github.ErrSignatureMissing) || errors.Is(err, release.ErrUntrusted) || errors.Is(err, release.ErrMetadata) {
				err = errors.Join(errSourceProof, err)
			}
			return result(err)
		}
		if proof.BuildID() != build {
			return result(errSourceProof)
		}
		index, err = readIndex(layout.ExecutableDirectory)
		if err != nil {
			return result(errors.Join(errSourceProof, err))
		}
		if _, err = proof.VerifyFiles(c, index); err != nil {
			return result(errors.Join(errSourceProof, err))
		}
	} else {
		proof, c, index, err = installedProof(ctx, source, layout, version, build)
		if err != nil {
			return result(err)
		}
	}
	return result(portable.ReconcileRelease(ctx, store, id, layout.ExecutableDirectory, portable.ReleaseInput{Proof: proof, Candidate: c, Index: index}))
}
