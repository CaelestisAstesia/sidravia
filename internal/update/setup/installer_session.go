package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/document/jsondoc"
	"sidravia/internal/document/securefile"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
)

type ProcessID struct {
	PID     uint32 `json:"pid"`
	Created uint64 `json:"created"`
}
type Session struct {
	SchemaVersion   int           `json:"schemaVersion"`
	OperationID     string        `json:"operationId"`
	Source          change.Target `json:"source"`
	Updater         ProcessID     `json:"updater"`
	Coordinator     ProcessID     `json:"coordinator"`
	Finalizer       ProcessID     `json:"finalizer"`
	Namespace       string        `json:"namespace"`
	HasGUI          bool          `json:"hasGui"`
	Expires         time.Time     `json:"expires"`
	SourceManifest  []byte        `json:"sourceManifest"`
	SourceSignature []byte        `json:"sourceSignature"`
	SourceIndex     []byte        `json:"sourceIndex"`
	InstallerExit   *uint32       `json:"installerExit,omitempty"`
	Integration     string        `json:"integration"`
	RestartRequired bool          `json:"restartRequired"`
}

func sessionPath(target, id string) (string, error) {
	if !change.ValidID(id) || !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return "", ErrInvalid
	}
	root, e := productlayout.OperationRoot()
	if e != nil {
		return "", e
	}
	if filepath.Separator == '\\' {
		target = strings.ToLower(target)
	}
	return filepath.Join(root, "setup", sum([]byte(target)), id+".json"), nil
}
func SaveSession(ctx context.Context, s Session) error {
	path, e := sessionPath(s.Source.ID, s.OperationID)
	if e != nil {
		return e
	}
	if s.SchemaVersion != 1 || s.Source.Layout != "installed" || s.Source.Validate() != nil || s.Updater.PID == 0 || s.Updater.Created == 0 {
		return ErrInvalid
	}
	store, e := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if e != nil {
		return e
	}
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return store.Replace(ctx, path, b)
}
func ReadSession(ctx context.Context, target, id string) (Session, error) {
	path, e := sessionPath(target, id)
	if e != nil {
		return Session{}, e
	}
	store, e := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if e != nil {
		return Session{}, e
	}
	b, exists, e := store.Read(ctx, path, 4<<20)
	if e != nil {
		return Session{}, e
	}
	if !exists {
		return Session{}, os.ErrNotExist
	}
	var s Session
	if e = jsondoc.DecodeStrict(b, &s); e != nil {
		return s, e
	}
	if s.SchemaVersion != 1 || s.OperationID != id || s.Source.ID != target || s.Source.Layout != "installed" || s.Source.Validate() != nil || s.Updater.PID == 0 || s.Updater.Created == 0 {
		return s, ErrInvalid
	}
	return s, nil
}
func (s Session) SourceProof(keys *release.PublisherKeys) (release.VerifiedRelease, release.Candidate, error) {
	proof, e := keys.Verify(s.SourceManifest, s.SourceSignature, s.Source.Version)
	if e != nil {
		return proof, release.Candidate{}, e
	}
	if proof.BuildID() != s.Source.BuildID {
		return proof, release.Candidate{}, ErrInvalid
	}
	c, e := proof.InstalledAsset("windows", "amd64", "setup")
	return proof, c, e
}
