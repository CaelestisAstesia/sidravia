package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	profilevalue "sidravia/internal/connection/profile"
	persistence "sidravia/internal/document"
	"sidravia/internal/document/jsondoc"
)

// profileLoaderOperation is the static label used by directory- and call-level
// loader failures. It never echoes an absolute path.
const (
	profileLoaderOperation        = "load institution profiles"
	profileSchemaVersion   uint64 = 1
	FileSizeLimit          int64  = 1 * 1024 * 1024
)

// ValidateInstitution validates protocol data without resolving a runtime driver.
type ValidateInstitution func(profilevalue.ProtocolID, profilevalue.ProtocolConfiguration) error

// Source is an already captured profile file. Name is its basename.
type Source struct {
	Name string
	Data []byte
}

type announcementDocument struct {
	URL          string `json:"url"`
	ClientWidth  int    `json:"clientWidth"`
	ClientHeight int    `json:"clientHeight"`
}

func (document *announcementDocument) metadata() *profilevalue.AnnouncementMetadata {
	if document == nil {
		return nil
	}
	return &profilevalue.AnnouncementMetadata{URL: document.URL, ClientWidth: document.ClientWidth, ClientHeight: document.ClientHeight}
}

// profileDocument is the version-1 on-disk shape of one institution Profile.
// InstitutionProtocolConfiguration is decoded as raw JSON and preserved without
// interpretation; the catalog does not decode or interpret
// protocol-specific fields. DecodeStrict scans the whole document, so duplicate
// keys inside the protocol configuration are rejected before the protocol
// validator sees it.
type profileDocument struct {
	Announcement                     *announcementDocument `json:"announcement"`
	SchemaVersion                    uint64                `json:"schemaVersion"`
	InstitutionProfileID             string                `json:"institutionProfileId"`
	DisplayName                      string                `json:"displayName"`
	AuthenticationProtocolID         string                `json:"authenticationProtocolId"`
	InstitutionProtocolConfiguration json.RawMessage       `json:"institutionProtocolConfiguration"`
}

// LoadProfileCatalogFromDirectory performs a one-shot startup load of the
// institution Profile directory. It reads every <id>.json file, strictly
// decodes and validates each document, checks its protocol through the
// supplied static description exactly
// once per file, and constructs a single ProfileCatalog from the complete set.
//
// A missing directory is a valid empty catalog. Any unreadable or invalid file
// fails the whole load: the loader never returns a partial catalog, skips,
// quarantines, rewrites or continues collecting multiple issues. It starts no
// goroutine, watcher, timer or reload loop; file edits take effect only after
// the caller restarts and invokes the loader again.
func LoadProfileCatalogFromDirectory(
	ctx context.Context,
	directory string,
	validate ValidateInstitution,
) (*ProfileCatalog, error) {
	if err := validateProfileLoaderContext(ctx); err != nil {
		return nil, err
	}
	if directory == "" || !filepath.IsAbs(directory) || validate == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	cleaned := filepath.Clean(directory)

	entries, err := os.ReadDir(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return NewProfileCatalog(nil)
		}
		return nil, profileLoaderDirectoryError(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	profiles := make([]profilevalue.Profile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, profileLoaderFileError(name,
				persistence.NewFailure(persistence.FailureInvalidDocument, nil))
		}
		profile, err := loadInstitutionProfileFile(ctx, cleaned, name, validate)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}

	if err := ctx.Err(); err != nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}
	return NewProfileCatalog(profiles)
}

// loadInstitutionProfileFile opens, reads and validates one accepted <id>.json
// entry. The basename is the only file identity exposed in any error; the full
// path and raw JSON remain reachable only through DiagnosticCause.
func loadInstitutionProfileFile(
	ctx context.Context,
	directory string,
	name string,
	validate ValidateInstitution,
) (profilevalue.Profile, error) {
	if err := ctx.Err(); err != nil {
		return profilevalue.Profile{}, persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}

	path := filepath.Join(directory, name)
	file, err := os.Open(path)
	if err != nil {
		return profilevalue.Profile{}, profileLoaderFileIOError(name, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return profilevalue.Profile{}, profileLoaderFileIOError(name, err)
	}
	if !info.Mode().IsRegular() {
		return profilevalue.Profile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}

	data, err := jsondoc.ReadLimited(file, FileSizeLimit)
	if err != nil {
		return profilevalue.Profile{}, profileLoaderFileError(name, err)
	}

	return decodeProfile(name, data, validate)
}

// LoadCaptured validates the same bytes the release producer will publish.
func LoadCaptured(ctx context.Context, sources []Source, validate ValidateInstitution) (*ProfileCatalog, error) {
	if err := validateProfileLoaderContext(ctx); err != nil {
		return nil, err
	}
	if validate == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	profiles := make([]profilevalue.Profile, 0, len(sources))
	for _, source := range sources {
		if err := validateProfileLoaderContext(ctx); err != nil {
			return nil, err
		}
		if source.Name == "" || filepath.Base(source.Name) != source.Name || !strings.HasSuffix(source.Name, ".json") {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
		}
		if int64(len(source.Data)) > FileSizeLimit {
			return nil, profileLoaderFileError(source.Name, persistence.NewFailure(persistence.FailureSizeLimitExceeded, nil))
		}
		value, err := decodeProfile(source.Name, source.Data, validate)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, value)
	}
	if err := validateProfileLoaderContext(ctx); err != nil {
		return nil, err
	}
	return NewProfileCatalog(profiles)
}

func decodeProfile(name string, data []byte, validate ValidateInstitution) (profilevalue.Profile, error) {
	document, err := decodeInstitutionProfileDocument(data)
	if err != nil {
		return profilevalue.Profile{}, profileLoaderFileError(name, err)
	}

	if !profilevalue.ID(document.InstitutionProfileID).Valid() {
		return profilevalue.Profile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}
	if name != document.InstitutionProfileID+".json" {
		return profilevalue.Profile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}
	if document.DisplayName == "" || document.AuthenticationProtocolID == "" {
		return profilevalue.Profile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}

	protocolID := profilevalue.ProtocolID(document.AuthenticationProtocolID)
	configuration := profilevalue.ProtocolConfiguration(document.InstitutionProtocolConfiguration)
	if err := validate(protocolID, append(profilevalue.ProtocolConfiguration(nil), configuration...)); err != nil {
		return profilevalue.Profile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, err))
	}

	return profilevalue.Profile{
		InstitutionProfileID:             profilevalue.ID(document.InstitutionProfileID),
		DisplayName:                      document.DisplayName,
		AuthenticationProtocolID:         protocolID,
		InstitutionProtocolConfiguration: configuration,
		Announcement:                     document.Announcement.metadata(),
	}, nil
}

// decodeInstitutionProfileDocument runs the shared strict JSON helpers. All
// five required root fields must be present and the schema version must be 1.
// Only announcement is optional. Unknown root and announcement fields are rejected;
// protocol-specific fields remain opaque here. Duplicate keys at any depth,
// including inside protocol configuration, are rejected before the validator sees it.
func decodeInstitutionProfileDocument(data []byte) (profileDocument, error) {
	if err := jsondoc.RequireObjectFields(data,
		"schemaVersion",
		"institutionProfileId",
		"displayName",
		"authenticationProtocolId",
		"institutionProtocolConfiguration",
	); err != nil {
		return profileDocument{}, err
	}
	if err := jsondoc.RequireSchemaVersion(data, profileSchemaVersion); err != nil {
		return profileDocument{}, err
	}
	var document profileDocument
	if err := jsondoc.DecodeStrict(data, &document); err != nil {
		return profileDocument{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return profileDocument{}, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	if len(fields) > 5 && (len(fields) != 6 || fields["announcement"] == nil) {
		return profileDocument{}, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
	}
	if raw := fields["announcement"]; len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := jsondoc.RequireObjectFields(raw, "url", "clientWidth", "clientHeight"); err != nil {
			return profileDocument{}, err
		}
		var announcementFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &announcementFields); err != nil || len(announcementFields) != 3 {
			return profileDocument{}, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
	}
	if err := document.Announcement.metadata().Validate(); err != nil {
		return profileDocument{}, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	return document, nil
}

func validateProfileLoaderContext(ctx context.Context) error {
	if ctx == nil {
		return persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	if err := ctx.Err(); err != nil {
		return persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}
	return nil
}

// profileLoaderFileError wraps a file-specific failure with the entry's
// basename. The public text exposes only the basename and the safe
// persistence.Failure.Error() text; the original cause remains reachable only
// via persistence.Failure.DiagnosticCause().
func profileLoaderFileError(basename string, err error) error {
	return fmt.Errorf("load profile %s: %w", basename, err)
}

// profileLoaderFileIOError classifies an OS-level file open, stat or read error
// as permission_denied or invalid_document and wraps it with the basename.
func profileLoaderFileIOError(basename string, err error) error {
	code := persistence.FailureInvalidDocument
	if os.IsPermission(err) {
		code = persistence.FailurePermissionDenied
	}
	return profileLoaderFileError(basename, persistence.NewFailure(code, err))
}

// profileLoaderDirectoryError classifies a directory enumeration error (other
// than not-exist, which yields an empty catalog) as permission_denied or
// invalid_document behind the static operation label.
func profileLoaderDirectoryError(err error) error {
	code := persistence.FailureInvalidDocument
	if os.IsPermission(err) {
		code = persistence.FailurePermissionDenied
	}
	return fmt.Errorf("%s: %w", profileLoaderOperation, persistence.NewFailure(code, err))
}
