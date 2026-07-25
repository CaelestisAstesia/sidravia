package persistence

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

type FileKind string

const (
	FileKindAuthenticationSessions    FileKind = "authentication_sessions"
	FileKindAuthenticationCredentials FileKind = "authentication_credentials"
	FileKindInstitutionProfiles       FileKind = "institution_profiles"
	FileKindApplicationSettings       FileKind = "application_settings"
)

type IssueCode string

const (
	IssueCodeRecordSchemaInvalid         IssueCode = "record_schema_invalid"
	IssueCodeProfileDocumentInvalid      IssueCode = "profile_document_invalid"
	IssueCodeProfileFilenameMismatch     IssueCode = "profile_filename_mismatch"
	IssueCodeProfileDirectoryMissing     IssueCode = "profile_directory_missing"
	IssueCodeApplicationDocumentInvalid  IssueCode = "application_document_invalid"
	IssueCodeNotificationSettingsInvalid IssueCode = "notification_settings_invalid"
	IssueCodeLoggingSettingsInvalid      IssueCode = "logging_settings_invalid"
)

type Issue struct {
	FileKind FileKind
	Location string
	Code     IssueCode
}

type IssueCollector struct {
	mutex  sync.Mutex
	limit  int
	issues []Issue
	seen   map[Issue]struct{}
}

func NewIssueCollector(limit int) *IssueCollector {
	if limit < 1 {
		limit = 1
	}
	if limit > 256 {
		limit = 256
	}
	return &IssueCollector{limit: limit, seen: make(map[Issue]struct{})}
}

func (collector *IssueCollector) Record(issue Issue) bool {
	if !validFileKind(issue.FileKind) || !validIssueCode(issue.Code) || !validLocation(issue.Location) {
		return false
	}
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	if _, exists := collector.seen[issue]; exists || len(collector.issues) >= collector.limit {
		return false
	}
	collector.seen[issue] = struct{}{}
	collector.issues = append(collector.issues, issue)
	return true
}

func (collector *IssueCollector) Snapshot() []Issue {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return append([]Issue(nil), collector.issues...)
}

func validFileKind(kind FileKind) bool {
	switch kind {
	case FileKindAuthenticationSessions, FileKindAuthenticationCredentials, FileKindInstitutionProfiles, FileKindApplicationSettings:
		return true
	default:
		return false
	}
}

func validIssueCode(code IssueCode) bool {
	switch code {
	case IssueCodeRecordSchemaInvalid, IssueCodeProfileDocumentInvalid, IssueCodeProfileFilenameMismatch, IssueCodeProfileDirectoryMissing, IssueCodeApplicationDocumentInvalid, IssueCodeNotificationSettingsInvalid, IssueCodeLoggingSettingsInvalid:
		return true
	default:
		return false
	}
}

func validLocation(location string) bool {
	switch location {
	case "application", "notificationSettings", "loggingSettings", "institution-profiles":
		return true
	}
	if index, err := strconv.Atoi(location); err == nil && index >= 0 && strconv.Itoa(index) == location {
		return true
	}
	return filepath.Base(location) == location &&
		!isDriveQualifiedName(location) &&
		!strings.ContainsAny(location, "/\\") &&
		strings.HasSuffix(location, ".json") &&
		len(location) <= 255 &&
		!containsControlCharacter(location)
}

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func isDriveQualifiedName(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	return (value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')
}
