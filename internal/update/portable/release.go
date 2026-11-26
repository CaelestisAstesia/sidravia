package portable

import (
	"errors"

	"sidravia/internal/release"
	"sidravia/internal/update/portable/internal/filetxn"
)

var (
	ErrInvalid        = filetxn.ErrInvalid
	ErrChanged        = filetxn.ErrChanged
	ErrUnsupported    = filetxn.ErrUnsupported
	ErrStagingCleanup = errors.New("update staging cleanup failed")
)

type ReleaseInput struct {
	Proof     release.VerifiedRelease
	Candidate release.Candidate
	Index     []byte
}

func (i ReleaseInput) authenticate() (filetxn.Inventory, error) {
	return filetxn.Authenticate(i.Proof, i.Candidate, i.Index)
}
