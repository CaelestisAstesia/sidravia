package configuration

import (
	"errors"
	"net/url"
	"sidravia/internal/daemon/authentication/protocol"
	"strings"
	"unicode"
)

type InstitutionProfileID string

// validateInstitutionProfileID is the single canonical identifier validator
// shared by catalog construction and lookup. An identifier is canonical when it
// is 1 through 64 ASCII bytes, its first character is a lowercase ASCII letter,
// every remaining character is a lowercase letter, decimal digit or hyphen, and
// every hyphen is surrounded by letters or digits. An invalid lookup identifier
// is FailureInvalidArgument; a valid but absent identifier remains
// FailureNotFound at the call site.
func validateInstitutionProfileID(id InstitutionProfileID) error {
	if !isCanonicalInstitutionProfileID(string(id)) {
		return profileCatalogInvalidArgument(nil)
	}
	return nil
}

func isCanonicalInstitutionProfileID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for index := 0; index < len(id); index++ {
		switch c := id[index]; {
		case c >= 'a' && c <= 'z':
		case index > 0 && c >= '0' && c <= '9':
		case c == '-' && index > 0 && index+1 < len(id) &&
			isProfileIDBorder(id[index-1]) && isProfileIDBorder(id[index+1]):
		default:
			return false
		}
	}
	return true
}

func isProfileIDBorder(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// AnnouncementMetadata describes a school-owned webpage without fetching it.
type AnnouncementMetadata struct {
	URL          string
	ClientWidth  int
	ClientHeight int
}

func (announcement *AnnouncementMetadata) Clone() *AnnouncementMetadata {
	if announcement == nil {
		return nil
	}
	copy := *announcement
	return &copy
}

func validateAnnouncement(announcement *AnnouncementMetadata) error {
	if announcement == nil {
		return nil
	}
	invalid := func() error { return errors.New("invalid announcement metadata") }
	if announcement.ClientWidth <= 0 || announcement.ClientWidth > 4096 || announcement.ClientHeight <= 0 || announcement.ClientHeight > 4096 ||
		announcement.URL == "" || strings.Contains(announcement.URL, "#") || strings.ContainsFunc(announcement.URL, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return invalid()
	}
	parsed, err := url.Parse(announcement.URL)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.User != nil ||
		!(strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
		return invalid()
	}
	return nil
}

type InstitutionProfile struct {
	InstitutionProfileID             InstitutionProfileID
	DisplayName                      string
	AuthenticationProtocolID         protocol.AuthenticationProtocolID
	Announcement                     *AnnouncementMetadata
	InstitutionProtocolConfiguration protocol.InstitutionProtocolConfiguration
}

type InstitutionProfileSummary struct {
	InstitutionProfileID     InstitutionProfileID
	DisplayName              string
	AuthenticationProtocolID protocol.AuthenticationProtocolID
	Announcement             *AnnouncementMetadata
}

func (profile InstitutionProfile) Clone() InstitutionProfile {
	profile.Announcement = profile.Announcement.Clone()
	profile.InstitutionProtocolConfiguration = append(
		protocol.InstitutionProtocolConfiguration(nil),
		profile.InstitutionProtocolConfiguration...,
	)
	return profile
}
