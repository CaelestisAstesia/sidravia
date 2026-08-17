package productlayout

import (
	"errors"
	"testing"
)

func TestNewNamespaceValidatesGrammar(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		ok   bool
		want Namespace
	}{
		{"", true, Namespace("")},
		{"isolated-1", true, "isolated-1"},
		{"mock-test", true, "mock-test"},
		{"a.b_c-9", true, "a.b_c-9"},
		{"   ", false, ""},
		{" spaced ", false, ""},
		{"a/b", false, ""},
		{`a\b`, false, ""},
		{"c:drive", false, ""},
		{"..", false, ""},
		{".hidden", false, ""},
		{"hidden.", false, ""},
		{"production", false, ""},
		{"default", false, ""},
		{"PROD", false, ""},
		{"control\nx", false, ""},
		{"中文", false, ""},
		{"x12345678901234567890123456789012345678901234567890123456789012345", false, ""},
	} {
		got, err := NewNamespace(tc.raw)
		if tc.ok && (err != nil || got != tc.want) {
			t.Fatalf("NewNamespace(%q)=%q,%v want %q", tc.raw, got, err, tc.want)
		}
		if !tc.ok && !errors.Is(err, ErrInvalidNamespace) {
			t.Fatalf("NewNamespace(%q) error = %v, want ErrInvalidNamespace", tc.raw, err)
		}
	}
}

func TestNamespaceProductionSemantics(t *testing.T) {
	ns, err := NewNamespace("")
	if err != nil {
		t.Fatal(err)
	}
	if !ns.IsProduction() || ns.String() != "" {
		t.Fatalf("production namespace = %q", ns.String())
	}
	ns2, err := NewNamespace("isolated-1")
	if err != nil {
		t.Fatal(err)
	}
	if ns2.IsProduction() || ns2.String() != "isolated-1" {
		t.Fatalf("isolated namespace = %q", ns2.String())
	}
}
