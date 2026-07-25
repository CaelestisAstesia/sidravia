package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

// profileLoaderOperation is the static label used by directory- and call-level
// loader failures. It never echoes an absolute path.
const profileLoaderOperation = "load institution profiles"

// profileDocument is the version-1 on-disk shape of one institution Profile.
// InstitutionProtocolConfiguration is decoded as raw JSON and preserved without
// interpretation; the configuration package does not decode or interpret
// protocol-specific fields. DecodeStrict scans the whole document, so duplicate
// keys inside the protocol configuration are rejected before the protocol
// factory sees it.
type profileDocument struct {
	SchemaVersion                    uint64          `json:"schemaVersion"`
	InstitutionProfileID             string          `json:"institutionProfileId"`
	DisplayName                      string          `json:"displayName"`
	AuthenticationProtocolID         string          `json:"authenticationProtocolId"`
	InstitutionProtocolConfiguration json.RawMessage `json:"institutionProtocolConfiguration"`
}

// DefaultInstitutionProfilesDirectory derives the local institution Profile
// directory:
//
//	<os.UserConfigDir()>/Sidravia/institution-profiles
//
// It returns an absolute cleaned path or a wrapped error that preserves the
// os.UserConfigDir cause. It does not create, inspect, harden or modify the
// directory.
func DefaultInstitutionProfilesDirectory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}
	return filepath.Clean(filepath.Join(base, "Sidravia", "institution-profiles")), nil
}

// LoadProfileCatalogFromDirectory performs a one-shot startup load of the
// institution Profile directory. It reads every <id>.json file, strictly
// decodes and validates each document, resolves its protocol through the
// supplied registry, invokes that factory's configuration validator exactly
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
	registry *protocol.AuthenticationProtocolRegistry,
) (*ProfileCatalog, error) {
	if err := validateProfileLoaderContext(ctx); err != nil {
		return nil, err
	}
	if directory == "" || !filepath.IsAbs(directory) || registry == nil {
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

	profiles := make([]InstitutionProfile, 0, len(entries))
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
		profile, err := loadInstitutionProfileFile(ctx, cleaned, name, registry)
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
	registry *protocol.AuthenticationProtocolRegistry,
) (InstitutionProfile, error) {
	if err := ctx.Err(); err != nil {
		return InstitutionProfile{}, persistence.NewFailure(persistence.FailureInvalidArgument, err)
	}

	path := filepath.Join(directory, name)
	file, err := os.Open(path)
	if err != nil {
		return InstitutionProfile{}, profileLoaderFileIOError(name, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return InstitutionProfile{}, profileLoaderFileIOError(name, err)
	}
	if !info.Mode().IsRegular() {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}

	data, err := jsonfile.ReadLimited(file, jsonfile.ProfileFileSizeLimit)
	if err != nil {
		return InstitutionProfile{}, profileLoaderFileError(name, err)
	}

	document, err := decodeInstitutionProfileDocument(data)
	if err != nil {
		return InstitutionProfile{}, profileLoaderFileError(name, err)
	}

	if !isCanonicalInstitutionProfileID(document.InstitutionProfileID) {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}
	if name != document.InstitutionProfileID+".json" {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}
	if document.DisplayName == "" || document.AuthenticationProtocolID == "" {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, nil))
	}

	factory, err := registry.GetFactory(protocol.AuthenticationProtocolID(document.AuthenticationProtocolID))
	if err != nil {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, err))
	}

	configuration := append(protocol.InstitutionProtocolConfiguration(nil), document.InstitutionProtocolConfiguration...)
	if err := factory.ValidateInstitutionProtocolConfiguration(configuration); err != nil {
		return InstitutionProfile{}, profileLoaderFileError(name,
			persistence.NewFailure(persistence.FailureInvalidDocument, err))
	}

	return InstitutionProfile{
		InstitutionProfileID:             InstitutionProfileID(document.InstitutionProfileID),
		DisplayName:                      document.DisplayName,
		AuthenticationProtocolID:         protocol.AuthenticationProtocolID(document.AuthenticationProtocolID),
		InstitutionProtocolConfiguration: configuration,
	}, nil
}

// decodeInstitutionProfileDocument runs the shared strict JSON helpers. All
// five root fields must be present, the schema version must be integer 1, no
// unknown root field is allowed, and duplicate keys at any depth (including
// inside the protocol configuration) are rejected before the factory sees it.
func decodeInstitutionProfileDocument(data []byte) (profileDocument, error) {
	if err := jsonfile.RequireObjectFields(data,
		"schemaVersion",
		"institutionProfileId",
		"displayName",
		"authenticationProtocolId",
		"institutionProtocolConfiguration",
	); err != nil {
		return profileDocument{}, err
	}
	if err := jsonfile.RequireSchemaVersion(data, jsonfile.SchemaVersion1); err != nil {
		return profileDocument{}, err
	}
	var document profileDocument
	if err := jsonfile.DecodeStrict(data, &document); err != nil {
		return profileDocument{}, err
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
