//go:build linux

package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/launchcontract"
	"sidravia/internal/update"
	"sidravia/internal/update/github"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/portable"
	"sidravia/internal/update/releases"
)

func applyPlatformSupported() bool { return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" }
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
	case errors.Is(err, portable.ErrInvalid), errors.Is(err, operations.ErrInvalid):
		return "invalid_metadata"
	}
	return "apply_failed"
}
func applyDownloaded(ctx context.Context, root string, layout productlayout.Layout, version, build string, source *github.GitHub, candidate releases.Candidate, path string, releaseRuntime func()) (outcome *applyResult, err error) {
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return nil, err
	}
	exists, err := inspectCached(root, path, &candidate)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, github.ErrIntegrity
	}
	opRoot, err := productlayout.UpdateOperationRoot()
	if err != nil {
		return nil, err
	}
	store, err := operations.Open(opRoot, layout.ExecutableDirectory, releases.PublisherTrust())
	if err != nil {
		return nil, err
	}
	if r, exists, err := store.Read(ctx); err != nil {
		return nil, err
	} else if exists {
		if r.RequiresStartupBlock() {
			return operationResult(ctx, store), operations.ErrConflict
		}
		if r.Recovery != nil && r.Recovery.ManagedService {
			if pending, err := portable.ManagedRollbackPending(layout.ExecutableDirectory, r.ID); pending || err != nil {
				return operationResult(ctx, store), errors.Join(operations.ErrConflict, err)
			}
		}
	}
	oldProof, oldAsset, oldIndex, err := installedProof(ctx, source, layout, version, build)
	if err != nil {
		return nil, err
	}
	old, err := portable.Authenticate(oldProof, oldAsset, oldIndex)
	if err != nil {
		return nil, errors.Join(errSourceProof, err)
	}
	proof, err := source.AuthenticateCandidate(ctx, candidate)
	if err != nil {
		return nil, err
	}
	payload, err := portable.ExtractRelease(ctx, layout.ExecutableDirectory, path, proof, candidate)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanup := payload.Close(); cleanup != nil {
			err = errors.Join(err, errStagingCleanup, cleanup)
		}
	}()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	record, err := operations.New(hex.EncodeToString(id), update.Target{ID: layout.ExecutableDirectory, Version: version, BuildID: build, Layout: "portable"}, candidate, proof, time.Now())
	if err != nil {
		return nil, err
	}
	if _, err = store.Create(ctx, record); err != nil {
		return operationResult(context.WithoutCancel(ctx), store), err
	}
	defer func() { outcome = operationResult(context.WithoutCancel(ctx), store) }()
	if _, _, err = store.Advance(ctx, record.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
		return nil, err
	}
	err = portable.Execute(ctx, portable.Execution{Store: store, OperationID: record.ID, Target: layout.ExecutableDirectory, Old: old, Payload: payload, Namespace: ns.String(), ReleaseRuntime: releaseRuntime})
	return nil, err
}
func recoverOperation(ctx context.Context, layout productlayout.Layout, version, build, id string, source *github.GitHub) (*applyResult, error) {
	if layout.Mode != productlayout.ModePortable || !update.ValidOperationID(id) {
		return nil, operations.ErrInvalid
	}
	root, err := productlayout.UpdateOperationRoot()
	if err != nil {
		return nil, err
	}
	store, err := operations.Open(root, layout.ExecutableDirectory, releases.PublisherTrust())
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
		case operations.Applying, operations.RepairRequired, operations.Applied, operations.ServiceStarting, operations.RollingBack, operations.RolledBack, operations.Completed, operations.Aborted:
			return result(portable.RecoverManagedService(ctx, store, id, layout.ExecutableDirectory))
		}
	}
	switch record.Phase {
	case operations.Applying, operations.RepairRequired:
		return result(errManualRepair)
	case operations.Completed, operations.Aborted, operations.ResumeFailed:
		return result(nil)
	}
	expected := record.Source
	checksTarget := record.Phase == operations.Resuming || record.Phase == operations.Applied || record.Phase == operations.ServiceStarting
	if checksTarget {
		expected = record.Target
	}
	if version != expected.Version || build != expected.BuildID {
		return result(errSourceProof)
	}
	var proof releases.VerifiedRelease
	var c releases.Candidate
	var index []byte
	if checksTarget {
		// Recovery must authenticate the encoding recorded by this operation.
		// The installed-source gzip counterpart has identical files but a
		// different archive hash/name and cannot replace the original proof.
		c = releases.Candidate{ID: record.Package.CandidateID, Version: record.Target.Version, Name: record.Package.Name, Size: record.Package.Size, SHA256: record.Package.SHA256}
		c.DownloadURL = "https://github.com/" + releases.Repository + "/releases/download/v" + c.Version + "/" + c.Name
		proof, err = source.AuthenticateCandidate(ctx, c)
		if err != nil {
			if errors.Is(err, github.ErrSignatureMissing) || errors.Is(err, releases.ErrUntrusted) || errors.Is(err, releases.ErrMetadata) {
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
	inventory, err := portable.Authenticate(proof, c, index)
	if err != nil {
		return result(err)
	}
	return result(portable.ReconcileInterrupted(ctx, store, id, layout.ExecutableDirectory, inventory))
}

func validApplyTarget(l productlayout.Layout, kind string) bool {
	return l.Mode == productlayout.ModePortable && kind == defaultPackageKind(runtime.GOOS, runtime.GOARCH, l)
}
