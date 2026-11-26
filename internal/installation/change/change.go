package change

import (
	"encoding/hex"
	"errors"
)

var ErrInvalid = errors.New("invalid update operation")

type Target struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	BuildID string `json:"buildId"`
	Layout  string `json:"layout"`
}

func ValidID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == id
}

func (t Target) Validate() error {
	if t.ID == "" || t.Version == "" || t.BuildID == "" || (t.Layout != "installed" && t.Layout != "portable") {
		return ErrInvalid
	}
	return nil
}
