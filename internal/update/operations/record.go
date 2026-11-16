// Package operations owns update execution records, not installation or Session
// state. A record never authorizes skipping signature or on-disk byte checks.
package operations

import (
	"errors"
	"path/filepath"
	"time"

	"sidravia/internal/update"
	"sidravia/internal/update/releases"
)

var (
	ErrInvalid = errors.New("invalid update execution record")
)

type Phase string

const (
	Created        Phase = "created"
	Verified       Phase = "verified"
	Preparing      Phase = "preparing"
	Quiescent      Phase = "quiescent"
	Applying       Phase = "applying"
	Applied        Phase = "applied"
	Resuming       Phase = "resuming"
	Completed      Phase = "completed"
	Aborted        Phase = "aborted"
	RepairRequired Phase = "repair_required"
	ResumeFailed   Phase = "resume_failed"
)

type Package struct {
	CandidateID string `json:"candidateId"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type Record struct {
	SchemaVersion int           `json:"schemaVersion"`
	ID            string        `json:"operationId"`
	Source        update.Target `json:"source"`
	Target        update.Target `json:"target"`
	Package       Package       `json:"package"`
	// []byte preserves exact signed bytes; JSON RawMessage would reformat them.
	Manifest    []byte    `json:"manifest"`
	Signature   []byte    `json:"signature"`
	Phase       Phase     `json:"phase"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	FailureCode string    `json:"failureCode,omitempty"`
}

func New(id string, source update.Target, c releases.Candidate, verified releases.VerifiedRelease, now time.Time) (Record, error) {
	if _, err := verified.Asset(c); err != nil {
		return Record{}, err
	}
	manifest, signature := verified.Proof()
	record := Record{SchemaVersion: 1, ID: id, Source: source, Target: update.Target{ID: source.ID, Layout: source.Layout, Version: c.Version, BuildID: verified.BuildID()}, Package: Package{CandidateID: c.ID, Name: c.Name, Size: c.Size, SHA256: c.SHA256}, Manifest: manifest, Signature: signature, Phase: Created, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if err := record.validateIdentity(); err != nil {
		return Record{}, err
	}
	return record, nil
}
func (r Record) candidate() releases.Candidate {
	return releases.Candidate{ID: r.Package.CandidateID, Version: r.Target.Version, Name: r.Package.Name, Size: r.Package.Size, SHA256: r.Package.SHA256}
}
func (r Record) validateIdentity() error {
	if r.SchemaVersion != 1 || !update.ValidOperationID(r.ID) || r.Source.Validate() != nil || r.Target.Validate() != nil ||
		!filepath.IsAbs(r.Source.ID) || filepath.Clean(r.Source.ID) != r.Source.ID || r.Source.ID != r.Target.ID || r.Source.Layout != r.Target.Layout ||
		len(r.Package.CandidateID) == 0 || len(r.Package.CandidateID) > 128 || r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return ErrInvalid
	}
	for _, ch := range r.Package.CandidateID {
		if !(ch >= '0' && ch <= '9' || ch == ':') {
			return ErrInvalid
		}
	}
	before, err := releases.ParseVersion(r.Source.Version)
	if err != nil {
		return ErrInvalid
	}
	after, err := releases.ParseVersion(r.Target.Version)
	if err != nil || after.Compare(before) <= 0 {
		return ErrInvalid
	}
	switch r.Phase {
	case Created, Verified, Preparing, Quiescent, Applying, Applied, Resuming, Completed:
		if r.FailureCode != "" {
			return ErrInvalid
		}
	case Aborted:
		if r.FailureCode != "cancelled" && r.FailureCode != "preparation_failed" {
			return ErrInvalid
		}
	case RepairRequired:
		if r.FailureCode != "apply_failed" {
			return ErrInvalid
		}
	case ResumeFailed:
		if r.FailureCode != "resume_failed" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func (r Record) Validate(keys *releases.PublisherKeys) error {
	if err := r.validateIdentity(); err != nil {
		return err
	}
	verified, err := keys.Verify(r.Manifest, r.Signature, r.Target.Version)
	if err != nil {
		return err
	}
	if verified.BuildID() != r.Target.BuildID {
		return ErrInvalid
	}
	_, err = verified.Asset(r.candidate())
	return err
}

// RequiresStartupBlock is a policy requirement for the later platform gate,
// not evidence that a running process has already been stopped.
func (r Record) RequiresStartupBlock() bool {
	switch r.Phase {
	case Preparing, Quiescent, Applying, Applied, Resuming, RepairRequired:
		return true
	}
	return false
}
func (r Record) terminal() bool {
	return r.Phase == Completed || r.Phase == Aborted || r.Phase == ResumeFailed
}
func allowed(from, to Phase) bool {
	switch from {
	case Created:
		return to == Verified || to == Aborted
	case Verified:
		return to == Preparing || to == Aborted
	case Preparing:
		return to == Quiescent || to == Aborted
	case Quiescent:
		return to == Applying || to == Aborted
	case Applying:
		return to == Applied || to == RepairRequired
	case Applied:
		return to == Resuming || to == Completed
	case Resuming:
		return to == Completed || to == ResumeFailed
	}
	return false
}
