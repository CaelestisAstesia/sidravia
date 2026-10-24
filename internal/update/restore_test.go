package update

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRestorePreservesIntent(t *testing.T) {
	now := time.Now().UTC()
	target := Target{ID: "fixture", Version: "1.0.0", BuildID: "fixture", Layout: "portable"}
	for _, i := range []RestoreIntent{{Mode: "none"}, {Mode: "desktop", Visible: true}, {Mode: "desktop", Maintain: true, ConfigurationID: "saved"}, {Mode: "headless"}, {Mode: "headless", Maintain: true, ConfigurationID: "saved"}} {
		ticket := ResumeTicket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: i}
		p, err := PlanRestore(ticket, target, true, now)
		if err != nil || p.Mode != i.Mode || p.Visible != i.Visible || p.SkipAutoLogin != (i.Mode != "none") || p.ConfigurationID != i.ConfigurationID {
			t.Fatalf("intent=%+v plan=%+v err=%v", i, p, err)
		}
		if _, err = PlanRestore(ticket, target, false, now); !errors.Is(err, ErrNotApplied) {
			t.Fatal(err)
		}
		wrong := target
		wrong.BuildID = "other"
		if _, err = PlanRestore(ticket, wrong, true, now); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if _, err = PlanRestore(ticket, target, true, ticket.ExpiresAt); !errors.Is(err, ErrExpired) {
			t.Fatal(err)
		}
	}
}

func TestRecoveryClaimIsExclusiveAndDurable(t *testing.T) {
	now := time.Now().UTC()
	target := Target{ID: "fixture", Version: "1.0.0", BuildID: "fixture", Layout: "installed"}
	ticket := ResumeTicket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: RestoreIntent{Mode: "headless", Maintain: true, ConfigurationID: "saved"}}
	dir := t.TempDir()
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _, err := ClaimRestore(dir, ticket, target, true, now); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConsumed) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("claims=%d", successes)
	}
	if _, err := ClaimRestore(dir, ticket, target, true, now); !errors.Is(err, ErrConsumed) {
		t.Fatal(err)
	}
}

func TestInvalidIntentCannotCreateClaim(t *testing.T) {
	now := time.Now().UTC()
	target := Target{ID: "fixture", Version: "1", BuildID: "fixture", Layout: "portable"}
	for _, i := range []RestoreIntent{{Mode: "none", Maintain: true, ConfigurationID: "secret"}, {Mode: "headless", Visible: true}, {Mode: "desktop", ConfigurationID: "saved"}, {Mode: "desktop", Maintain: true}, {Mode: "unknown"}} {
		ticket := ResumeTicket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: i}
		if _, err := ClaimRestore(t.TempDir(), ticket, target, true, now); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
