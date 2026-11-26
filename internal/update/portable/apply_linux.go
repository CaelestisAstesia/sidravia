//go:build linux

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"sidravia/internal/installation/change"
	"sidravia/internal/release"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
)

// ApplyRelease authenticates both file inventories, owns staging cleanup, and
// coordinates publication and recovery after the durable operation is created.
func ApplyRelease(ctx context.Context, store *transaction.Store, source change.Target, installed ReleaseInput, proof release.VerifiedRelease, candidate release.Candidate, archive, namespace string, handoff func()) (created bool, resultErr error) {
	old, err := installed.authenticate()
	if err != nil {
		return false, err
	}
	payload, err := filetxn.ExtractRelease(ctx, source.ID, archive, proof, candidate)
	if err != nil {
		return false, err
	}
	defer func() {
		if cleanup := payload.Close(); cleanup != nil {
			resultErr = errors.Join(resultErr, ErrStagingCleanup, cleanup)
		}
	}()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return false, err
	}
	record, err := transaction.New(hex.EncodeToString(id[:]), source, candidate, proof, time.Now())
	if err != nil {
		return false, err
	}
	if _, err = store.Create(ctx, record); err != nil {
		return true, err
	}
	if _, _, err := store.Advance(ctx, record.ID, transaction.Created, transaction.Verified, "", time.Now()); err != nil {
		return true, err
	}
	return true, execute(ctx, execution{Store: store, OperationID: record.ID, Target: source.ID, Old: old, Payload: payload, Namespace: namespace, ReleaseRuntime: handoff})
}
