package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	t.Run("valid required", func(t *testing.T) {
		c, err := parseFlags([]string{"--version", "1.2.3", "--build-id", "abc", "--output", "out"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if c.version != "1.2.3" || c.buildID != "abc" || c.output != "out" || c.goBin != "go" || c.artifact != "release" || c.help {
			t.Fatalf("bad config: %+v", c)
		}
	})
	t.Run("field validation artifact", func(t *testing.T) {
		c, err := parseFlags([]string{"--version=1.0.0", "--build-id=x", "--output=o", "--artifact=field-validation"})
		if err != nil || c.artifact != "field-validation" {
			t.Fatalf("artifact=%q err=%v", c.artifact, err)
		}
	})
	t.Run("equal form and custom go", func(t *testing.T) {
		c, err := parseFlags([]string{"--version=1.0.0", "--build-id=x", "--output=o", "--go=/p/go"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if c.goBin != "/p/go" || c.version != "1.0.0" {
			t.Fatalf("bad config: %+v", c)
		}
	})
	t.Run("help", func(t *testing.T) {
		for _, args := range [][]string{{"--help"}, {"-h"}} {
			c, err := parseFlags(args)
			if err != nil || !c.help {
				t.Fatalf("args=%v: help=%v err=%v", args, c.help, err)
			}
		}
	})
	t.Run("help must be sole argument", func(t *testing.T) {
		for _, args := range [][]string{
			{"--help", "unexpected"},
			{"unexpected", "--help"},
			{"--version", "1.0.0", "--help"},
			{"--help", "--version", "1.0.0"},
			{"-h", "--output", "out"},
			{"--help", "--help"},
		} {
			if _, err := parseFlags(args); err == nil {
				t.Fatalf("args=%v: expected error, got nil", args)
			}
		}
	})
	for name, args := range map[string][]string{
		"missing version":  {"--build-id", "x", "--output", "o"},
		"missing build-id": {"--version", "1.0.0", "--output", "o"},
		"missing output":   {"--version", "1.0.0", "--build-id", "x"},
		"empty value":      {"--version", "", "--build-id", "x", "--output", "o"},
		"repeated":         {"--version", "1.0.0", "--version", "2.0.0", "--build-id", "x", "--output", "o"},
		"unknown flag":     {"--foo", "1"},
		"positional":       {"--version", "1.0.0", "--build-id", "x", "--output", "o", "extra"},
		"missing value":    {"--version"},
		"invalid artifact": {"--version", "1.0.0", "--build-id", "x", "--output", "o", "--artifact", "installed"},
		"empty artifact":   {"--version", "1.0.0", "--build-id", "x", "--output", "o", "--artifact="},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(args); err == nil {
				t.Fatalf("expected error for %v", args)
			}
		})
	}
}

func TestValidateProductVersion(t *testing.T) {
	for _, v := range []string{
		"0.1.0", "0.1.0-alpha.2", "1.2.3", "0.0.0", "10.20.30",
		"0.1.0-alpha", "0.1.0-alpha-rc.1", "0.1.0-0", "0.1.0-1.2",
	} {
		if err := validateProductVersion(v); err != nil {
			t.Errorf("accept %q: %v", v, err)
		}
	}
	for _, v := range []string{
		"", "v0.1.0", "V0.1.0", "01.0.0", "0.01.0", "0.1.0+local", "0.1.0+build",
		"0.1.0-", "0.1.0-alpha.", "0.1", "0.1.0.0", "0.1.0-alpha.02",
		"0.1.0 alpha", "0.1.0/alpha", "0.1.0\\alpha", "0.1.0\talpha",
		"0.1.0-α",
	} {
		if err := validateProductVersion(v); err == nil {
			t.Errorf("reject %q: expected error", v)
		}
	}
}

func TestValidateBuildID(t *testing.T) {
	for _, v := range []string{
		"a", "abc", "0", "605fbcc2841525949f748fb90771887e9c0f74db",
		"A.B-C_D", strings.Repeat("a", 64),
	} {
		if err := validateBuildID(v); err != nil {
			t.Errorf("accept %q: %v", v, err)
		}
	}
	for _, v := range []string{
		"", ".abc", "-abc", "_abc", "/abc", "a b", "a$b", "a/b",
		"abc:def", strings.Repeat("a", 65), "a-α",
	} {
		if err := validateBuildID(v); err == nil {
			t.Errorf("reject %q: expected error", v)
		}
	}
}

func TestHelpPrintsAndExits(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"--help"}, &stdout, io.Discard)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "--version") || !strings.Contains(stdout.String(), "go1.26.4") {
		t.Errorf("help text missing content: %q", stdout.String())
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	code := run([]string{"--version", "v0.1.0", "--build-id", "x", "--output", "o"}, io.Discard, io.Discard)
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid version")
	}
}

func TestRunRejectsMixedHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"--help", "unexpected"}, &stdout, io.Discard)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}
