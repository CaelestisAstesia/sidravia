package releases

import (
	"errors"
	"strings"
)

var ErrVersion = errors.New("invalid semantic version")

type Version struct {
	core [3]string
	pre  []string
	text string
}

func ParseVersion(text string) (Version, error) {
	if len(text) == 0 || len(text) > 128 {
		return Version{}, ErrVersion
	}
	main, build, hasBuild := strings.Cut(text, "+")
	if hasBuild && !identifiers(build, false) {
		return Version{}, ErrVersion
	}
	core, pre, hasPre := strings.Cut(main, "-")
	if hasPre && !identifiers(pre, true) {
		return Version{}, ErrVersion
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, ErrVersion
	}
	for _, p := range parts {
		if !numeric(p) || (len(p) > 1 && p[0] == '0') {
			return Version{}, ErrVersion
		}
	}
	v := Version{core: [3]string{parts[0], parts[1], parts[2]}, text: text}
	if hasPre {
		v.pre = strings.Split(pre, ".")
	}
	return v, nil
}
func identifiers(text string, prerelease bool) bool {
	for _, p := range strings.Split(text, ".") {
		if p == "" {
			return false
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
		if prerelease && numeric(p) && len(p) > 1 && p[0] == '0' {
			return false
		}
	}
	return true
}
func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func numberCompare(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func (v Version) String() string   { return v.text }
func (v Version) Prerelease() bool { return len(v.pre) > 0 }
func (v Version) Compare(other Version) int {
	for i := range 3 {
		if c := numberCompare(v.core[i], other.core[i]); c != 0 {
			return c
		}
	}
	if len(v.pre) == 0 && len(other.pre) == 0 {
		return 0
	}
	if len(v.pre) == 0 {
		return 1
	}
	if len(other.pre) == 0 {
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(other.pre); i++ {
		a, b := v.pre[i], other.pre[i]
		c := 0
		if numeric(a) && numeric(b) {
			c = numberCompare(a, b)
		} else if numeric(a) {
			c = -1
		} else if numeric(b) {
			c = 1
		} else {
			c = strings.Compare(a, b)
		}
		if c != 0 {
			return c
		}
	}
	if len(v.pre) < len(other.pre) {
		return -1
	}
	if len(v.pre) > len(other.pre) {
		return 1
	}
	return 0
}
