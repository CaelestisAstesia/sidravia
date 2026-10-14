package d520

import (
	"strings"
	"testing"
)

func TestUsernameValidationMatchesRunAndWireBounds(t *testing.T) {
	for _, tc := range []struct {
		name, username string
		bytes          int
		valid          bool
	}{
		{"ascii boundary", strings.Repeat("a", 36), 36, true},
		{"ascii overflow", strings.Repeat("a", 37), 37, false},
		{"gbk boundary", strings.Repeat("中", 18), 36, true},
		{"gbk overflow", strings.Repeat("中", 19), 38, false},
		{"empty", "", 0, false},
		{"invalid utf8", string([]byte{0xff}), 0, false},
		{"nul", "private\x00marker", 0, false},
		{"unencodable", "private😀marker", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewFactory()
			err := factory.ValidateUsername(tc.username)
			if (err == nil) != tc.valid {
				t.Fatalf("username validation: %v", err)
			}
			cred := testCredential()
			cred.Username = tc.username
			_, runErr := factory.CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), cred, testBinding(t)))
			if (runErr == nil) != tc.valid {
				t.Fatalf("run validation disagrees: %v", runErr)
			}
			if tc.bytes > 0 {
				encoded, err := encodeProtocolText(tc.username)
				if err != nil || len(encoded) != tc.bytes {
					t.Fatalf("encoded length = %d, error=%v", len(encoded), err)
				}
			}
			// Existing packet fixtures protect complete bytes; both packet paths must
			// retain the same rejection boundary as saving a non-empty username.
			if tc.username != "" {
				_, _, _, _, loginErr := encodeCredentialFields(loginInput{username: tc.username})
				_, _, logoutErr := encodeLogoutCredentialFields(loginInput{username: tc.username})
				if (loginErr == nil) != tc.valid || (logoutErr == nil) != tc.valid {
					t.Fatal("packet username boundary disagrees")
				}
			}
			if err != nil && tc.username != "" && strings.Contains(err.Error(), tc.username) {
				t.Fatal("username echoed in validation error")
			}
		})
	}
}
