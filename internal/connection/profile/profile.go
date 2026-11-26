package profile

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"
)

type ID string

// Valid reports whether id is in the canonical form used by institution profiles.
func (id ID) Valid() bool {
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

func (announcement *AnnouncementMetadata) Validate() error {
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

type ProtocolID string
type ProtocolConfiguration json.RawMessage
type ProtocolOverride json.RawMessage

type Profile struct {
	InstitutionProfileID             ID
	DisplayName                      string
	AuthenticationProtocolID         ProtocolID
	Announcement                     *AnnouncementMetadata
	InstitutionProtocolConfiguration ProtocolConfiguration
}

type Summary struct {
	OverrideSchemaVersion    int
	InstitutionProfileID     ID
	DisplayName              string
	AuthenticationProtocolID ProtocolID
	Announcement             *AnnouncementMetadata
}

func (profile Profile) Clone() Profile {
	profile.Announcement = profile.Announcement.Clone()
	profile.InstitutionProtocolConfiguration = append(
		ProtocolConfiguration(nil),
		profile.InstitutionProtocolConfiguration...,
	)
	return profile
}
