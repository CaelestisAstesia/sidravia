package productlayout

import (
	"errors"
	"strings"
	"unicode"
)

// ErrInvalidNamespace is returned when a runtime namespace violates the strict
// grammar. The descriptive reason stays with the caller as a normal error and
// is never exposed to the user as a raw value.
var ErrInvalidNamespace = errors.New("productlayout: invalid runtime namespace")

// Namespace is the immutable opt-in runtime isolation identifier. The zero
// value is the exact production namespace. Its value is private so callers
// cannot forge a namespace without passing NewNamespace validation.
type Namespace struct {
	value string
}

const maxNamespaceLen = 64

var reservedNamespaces = map[string]struct{}{
	"production": {},
	"default":    {},
	"prod":       {},
}

// NewNamespace validates raw and returns the immutable Namespace. The empty
// string is the exact production namespace. A non-empty value must be a strict
// short ASCII identifier: alphanumerics, '-', '_' and '.', no path separators,
// no drive syntax, no whitespace, no control characters, no traversal, no
// reserved production alias, at most maxNamespaceLen bytes.
func NewNamespace(raw string) (Namespace, error) {
	if raw == "" {
		return Namespace{}, nil
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return Namespace{}, ErrInvalidNamespace
	}
	if value != raw {
		// Surrounding whitespace is not silently accepted by a strict grammar.
		return Namespace{}, ErrInvalidNamespace
	}
	if len(raw) > maxNamespaceLen {
		return Namespace{}, ErrInvalidNamespace
	}
	if strings.ContainsAny(raw, `/\`) || strings.Contains(raw, ":") {
		return Namespace{}, ErrInvalidNamespace
	}
	if _, reserved := reservedNamespaces[strings.ToLower(raw)]; reserved {
		return Namespace{}, ErrInvalidNamespace
	}
	if strings.HasPrefix(raw, ".") || strings.HasSuffix(raw, ".") || strings.Contains(raw, "..") {
		return Namespace{}, ErrInvalidNamespace
	}
	for _, r := range raw {
		if r > unicode.MaxASCII || !namespaceASCII(r) {
			return Namespace{}, ErrInvalidNamespace
		}
	}
	return Namespace{value: raw}, nil
}

// IsProduction reports whether the namespace is the exact production namespace.
func (n Namespace) IsProduction() bool { return n.value == "" }

// String returns the exact verified value. It is empty for production.
func (n Namespace) String() string { return n.value }

func namespaceASCII(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
}
