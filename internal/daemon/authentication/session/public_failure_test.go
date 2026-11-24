package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestSessionRejectsCredentialMaterialFromPublicFailure(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	sentinel := definition.AuthenticationCredential.Password
	username := definition.AuthenticationCredential.Username
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{
		Code:        protocol.AuthenticationProtocolFailureCode("code-" + username + "-" + sentinel),
		Description: "description-" + username + "-" + sentinel,
	})
	got := waitForState(t, ctx, session, BlockedByError)
	if got.LastAuthenticationFailure == nil {
		t.Fatal("public failure was not retained")
	}
	if got.LastAuthenticationFailure.Code != "authentication_protocol_failure" || got.LastAuthenticationFailure.Description != "Authentication protocol failure." {
		t.Fatal("public failure did not use stable generic fallbacks")
	}
	if strings.Contains(string(got.LastAuthenticationFailure.Code), sentinel) || strings.Contains(string(got.LastAuthenticationFailure.Code), username) ||
		strings.Contains(got.LastAuthenticationFailure.Description, sentinel) ||
		strings.Contains(got.LastAuthenticationFailure.Description, username) ||
		strings.Contains(fmt.Sprint(got.LastAuthenticationFailure), sentinel) {
		t.Fatal("public failure contains credential material")
	}
}

func TestSessionUsesSafeFallbackForEmptyPublicFailureText(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{})
	got := waitForState(t, ctx, session, BlockedByError)
	if got.LastAuthenticationFailure == nil || got.LastAuthenticationFailure.Code != "authentication_protocol_failure" || got.LastAuthenticationFailure.Description != "Authentication protocol failure." {
		t.Fatal("empty public failure text did not receive safe fallbacks")
	}
}

func TestSessionNeverProjectsCredentialMaterialFromAnyFailureField(t *testing.T) {
	for _, field := range []string{"code", "description", "recommendation"} {
		t.Run(field, func(t *testing.T) {
			factory := &controlledFactory{}
			definition := validRuntimeDefinition(t)
			definition.AuthenticationProtocolFactory = factory
			session, _ := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(time.Now))
			session.Start()
			ctx := testContext(t)
			defer shutdownTestSession(t, session)
			failure := &protocol.AuthenticationProtocolRunFailure{Code: "safe-code", Description: "safe-description", HandlingRecommendation: protocol.RetryAfterStandardDelay}
			sentinel := definition.AuthenticationCredential.Password
			switch field {
			case "code":
				failure.Code = protocol.AuthenticationProtocolFailureCode(sentinel)
			case "description":
				failure.Description = sentinel
			case "recommendation":
				failure.HandlingRecommendation = protocol.AuthenticationProtocolFailureHandlingRecommendation(sentinel)
			}
			_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
			factory.run(0).unblock(failure)
			got := waitForState(t, ctx, session, BlockedByError).LastAuthenticationFailure
			if got == nil || strings.Contains(fmt.Sprint(got), sentinel) {
				t.Fatal("public failure contains credential material")
			}
		})
	}
}

func TestSessionDoesNotLeakRedactionOrFallbackCollisions(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.AuthenticationCredential.Username = "[redacted]"
	definition.AuthenticationCredential.Password = "Authentication protocol failure."
	session, _ := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(time.Now))
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{Code: "[redacted]", Description: "Authentication protocol failure.", HandlingRecommendation: protocol.RetryAfterStandardDelay})
	got := waitForState(t, ctx, session, BlockedByError).LastAuthenticationFailure
	if got == nil || strings.Contains(fmt.Sprint(got), definition.AuthenticationCredential.Username) || strings.Contains(fmt.Sprint(got), definition.AuthenticationCredential.Password) {
		t.Fatal("public projection contains collision secret")
	}
}

func TestShortCredentialFailureFields(t *testing.T) {
	for _, password := range []string{"a", "e", "retry", "network_timeout", "Authentication protocol failure."} {
		s := &AuthenticationSession{definition: validRuntimeDefinition(t)}
		s.definition.AuthenticationCredential.Password = password
		for _, rec := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{protocol.RetryAfterStandardDelay, protocol.RetryAfterExtendedDelay, protocol.RetryAfterBoundedRecovery, protocol.BlockUntilExplicitRestartOrRelevantInputChange} {
			input := &protocol.AuthenticationProtocolRunFailure{Code: "network_timeout", Description: "untrusted-content:" + password, DiagnosticCause: fmt.Errorf("private-cause:%s", password)}
			got := s.publicFailureWithRecommendation(input, rec)
			if got.Code != "network_timeout" || got.Description == "" || got.HandlingRecommendation != rec {
				t.Errorf("fixed public fields corrupted for credential %q: %#v", password, got)
			}
			if strings.Contains(got.Description, "untrusted-content:") || strings.Contains(got.Description, "private-cause:") {
				t.Fatal("untrusted failure text leaked")
			}
			input.Code = protocol.AuthenticationProtocolFailureCode("untrusted_" + password)
			if got := s.publicFailureWithRecommendation(input, rec); got.Code == input.Code || got.Code == "" {
				t.Fatal("arbitrary credential-bearing code was retained/emptied")
			}
		}
	}
}
