package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/update"
	"sidravia/internal/update/releases"
)

var (
	ErrConflict   = errors.New("update execution state conflict")
	ErrCapacity   = errors.New("update execution history limit reached")
	ErrDurability = errors.New("update execution record durability unconfirmed")
)

const recordLimit = 512 << 10

// Store serializes one target's records across goroutines and processes. Its
// caller supplies a private cache root outside the target; one fixed current
// record survives coordinator exit and cannot be overwritten while unfinished.
type Store struct {
	mu                sync.Mutex
	directory, target string
	keys              *releases.PublisherKeys
	secure            *jsonfile.SecureStore
	postCommit        error
}

func Open(root, target string, keys *releases.PublisherKeys) (*Store, error) {
	if keys == nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return nil, ErrInvalid
	}
	physicalTarget, err := resolveExistingPrefix(target)
	if err != nil {
		return nil, err
	}
	physicalRoot, err := resolveExistingPrefix(root)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(physicalTarget, physicalRoot)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, ErrInvalid
	}
	identity := physicalTarget
	if filepath.Separator == '\\' {
		identity = strings.ToLower(identity)
	}
	sum := sha256.Sum256([]byte(identity))
	directory := filepath.Join(physicalRoot, hex.EncodeToString(sum[:]))
	actualDirectory, err := resolveExistingPrefix(directory)
	if err != nil {
		return nil, err
	}
	if actualDirectory != directory {
		return nil, ErrInvalid
	}
	store := &Store{directory: directory, target: target, keys: keys}
	store.secure, err = jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{OnPostCommitDiagnostic: func(err error) { store.postCommit = err }})
	if err != nil {
		return nil, err
	}
	return store, nil
}
func (s *Store) read(ctx context.Context) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	actual, err := resolveExistingPrefix(s.directory)
	if err != nil {
		return Record{}, false, err
	}
	if actual != s.directory {
		return Record{}, false, ErrInvalid
	}
	data, exists, err := s.secure.Read(ctx, filepath.Join(s.directory, "current.json"), recordLimit)
	if err != nil || !exists {
		return Record{}, exists, err
	}
	var r Record
	if err := jsonfile.DecodeStrict(data, &r); err != nil {
		return Record{}, true, errors.Join(ErrInvalid, err)
	}
	if !recordShape(data) || r.Source.ID != s.target {
		return Record{}, true, ErrInvalid
	}
	if err := r.Validate(s.keys); err != nil {
		return Record{}, true, err
	}
	claim, exists, err := s.secure.Read(ctx, s.claimPath(r.ID), 1024)
	if err != nil {
		return Record{}, true, err
	}
	var c claimRecord
	if !exists || jsonfile.DecodeStrict(claim, &c) != nil || c.SchemaVersion != 1 || c.ID != r.ID {
		return Record{}, true, ErrInvalid
	}
	digest, err := authorizationDigest(r)
	if err != nil || digest != c.Authorization {
		return Record{}, true, ErrInvalid
	}
	return r, true, nil
}
func (s *Store) Read(ctx context.Context) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(ctx)
}
func (s *Store) write(ctx context.Context, r Record) error {
	data, err := jsonfile.MarshalDeterministic(r)
	if err != nil {
		return err
	}
	s.postCommit = nil
	if err := s.secure.ReplaceSensitive(ctx, filepath.Join(s.directory, "current.json"), data, recordLimit, false); err != nil {
		return err
	}
	// A known rename with failed directory flush is insufficient permission to
	// begin replacing executable files; stop even though the record may exist.
	if s.postCommit != nil {
		return errors.Join(ErrDurability, s.postCommit)
	}
	return nil
}
func (s *Store) Create(ctx context.Context, r Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Source.ID != s.target || r.Phase != Created {
		return Record{}, ErrInvalid
	}
	if err := r.Validate(s.keys); err != nil {
		return Record{}, err
	}
	if _, _, err := s.read(ctx); err != nil {
		return Record{}, err
	} // prepares the secured directory
	unlock, err := lock(filepath.Join(s.directory, "operation.lock"))
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	current, exists, err := s.read(ctx)
	if err != nil {
		return Record{}, err
	}
	if exists && current.ID == r.ID {
		// Only byte-identical authorization may replay the same operation identity.
		if current.Source != r.Source || current.Target != r.Target || current.Package != r.Package || string(current.Manifest) != string(r.Manifest) || string(current.Signature) != string(r.Signature) {
			return Record{}, ErrConflict
		}
		return current, nil
	}
	if exists && !current.terminal() {
		return Record{}, ErrConflict
	}
	// Retain a bounded, permanent ID claim even after current.json advances to
	// later operations. Failure after claiming but before current publication
	// rejects that ID rather than accidentally granting a second execution.
	if _, exists, err := s.secure.Read(ctx, s.claimPath(r.ID), 1024); err != nil {
		return Record{}, err
	} else if exists {
		return Record{}, ErrConflict
	}
	if err := s.checkCapacity(); err != nil {
		return Record{}, err
	}
	digest, err := authorizationDigest(r)
	if err != nil {
		return Record{}, err
	}
	claim, err := jsonfile.MarshalDeterministic(claimRecord{SchemaVersion: 1, ID: r.ID, Authorization: digest})
	if err != nil {
		return Record{}, err
	}
	s.postCommit = nil
	if err := s.secure.PreserveSensitive(ctx, s.claimPath(r.ID), claim, 1024, false); err != nil {
		return Record{}, err
	}
	if s.postCommit != nil {
		return Record{}, errors.Join(ErrDurability, s.postCommit)
	}
	if err := s.write(ctx, r); err != nil {
		return Record{}, err
	}
	return r, nil
}

// Advance reports whether a durable transition was newly committed. A replay
// returns changed=false and must not be treated as fresh execution admission.
func (s *Store) Advance(ctx context.Context, id string, expected, next Phase, failureCode string, now time.Time) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.read(ctx); err != nil {
		return Record{}, false, err
	}
	unlock, err := lock(filepath.Join(s.directory, "operation.lock"))
	if err != nil {
		return Record{}, false, err
	}
	defer unlock()
	r, exists, err := s.read(ctx)
	if err != nil {
		return Record{}, false, err
	}
	if !exists || r.ID != id {
		return Record{}, false, ErrConflict
	}
	if r.Phase == next && r.FailureCode == failureCode {
		return r, false, nil
	}
	if r.Phase != expected || !allowed(expected, next) {
		return Record{}, false, ErrConflict
	}
	r.Phase = next
	r.FailureCode = failureCode
	if now.After(r.UpdatedAt) {
		r.UpdatedAt = now.UTC()
	}
	if err := r.Validate(s.keys); err != nil {
		return Record{}, false, err
	}
	if err := s.write(ctx, r); err != nil {
		return Record{}, false, err
	}
	return r, true, nil
}

// Resolve existing ancestors without creating either location. A cache alias
// into the program tree must not place crash protection among replaced files.
func resolveExistingPrefix(value string) (string, error) {
	var suffix []string
	current := value
	for {
		_, err := os.Lstat(current)
		if err == nil {
			actual, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(actual)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", ErrInvalid
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				actual = filepath.Join(actual, suffix[i])
			}
			return actual, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrInvalid
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func recordShape(data []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || !exactRecordFields(root, []string{"schemaVersion", "operationId", "source", "target", "package", "manifest", "signature", "phase", "createdAt", "updatedAt"}, "failureCode") {
		return false
	}
	for _, name := range []string{"source", "target", "package"} {
		var object map[string]json.RawMessage
		if json.Unmarshal(root[name], &object) != nil {
			return false
		}
		fields := []string{"id", "version", "buildId", "layout"}
		if name == "package" {
			fields = []string{"candidateId", "name", "size", "sha256"}
		}
		if !exactRecordFields(object, fields) {
			return false
		}
	}
	return true
}
func exactRecordFields(object map[string]json.RawMessage, required []string, optional ...string) bool {
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		if len(object[key]) == 0 {
			return false
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, value := range object {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

type claimRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"operationId"`
	Authorization string `json:"authorizationSha256"`
}

func (s *Store) claimPath(id string) string { return filepath.Join(s.directory, ".used-"+id+".json") }
func authorizationDigest(r Record) (string, error) {
	immutable := struct {
		ID                  string
		Source, Target      update.Target
		Package             Package
		Manifest, Signature []byte
		CreatedAt           time.Time
	}{r.ID, r.Source, r.Target, r.Package, r.Manifest, r.Signature, r.CreatedAt}
	data, err := json.Marshal(immutable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (s *Store) checkCapacity() error {
	// At most 4096 permanent operation claims, current.json and two locks. Unknown
	// leaves are retained and consume capacity; this code never prunes history.
	directory, err := os.Open(s.directory)
	if err != nil {
		return err
	}
	names, readErr := directory.Readdirnames(4100)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(names) >= 4099 {
		return ErrCapacity
	}
	return nil
}
