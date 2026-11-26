package settings

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// EncodeText encodes s as the protocol text byte sequence. The current
// implementation uses strict GBK. An embedded NUL
// (0x00) is rejected because these fields are NUL-padded on the wire and an
// embedded NUL would be indistinguishable from padding. The returned slice is
// freshly allocated and never aliases s.
func EncodeText(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("not valid UTF-8, cannot encode as protocol text")
	}
	for _, r := range s {
		if r == 0 {
			return nil, fmt.Errorf("contains an embedded NUL, cannot encode as protocol text")
		}
	}
	encoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(s))
	if err != nil {
		return nil, fmt.Errorf("contains a rune that cannot be encoded as strict GBK")
	}
	return append([]byte(nil), encoded...), nil
}

// HostOS forms the wire host OS by trimming the OS family and release
// and joining non-empty parts with one ASCII space. Machine architecture is
// not sent.
func HostOS(family, release string) string {
	family = strings.TrimSpace(family)
	release = strings.TrimSpace(release)
	switch {
	case family != "" && release != "":
		return family + " " + release
	case family != "":
		return family
	case release != "":
		return release
	default:
		return ""
	}
}
