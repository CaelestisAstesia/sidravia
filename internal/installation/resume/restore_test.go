package resume

import (
	"errors"
	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
	"sync"
	"testing"
	"time"
)

func TestRestorePreservesIntent(t *testing.T) {
	now := time.Now().UTC()
	target := change.Target{ID: "fixture", Version: "1.0.0", BuildID: "fixture", Layout: "portable"}
	for _, i := range []maintenance.Intent{{Mode: "none"}, {Mode: "desktop", Visible: true}, {Mode: "desktop", Maintain: true, ConfigurationID: "saved"}, {Mode: "headless"}, {Mode: "headless", Maintain: true, ConfigurationID: "saved"}} {
		ticket := Ticket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: i}
		p, err := Plan(ticket, target, true, now)
		if err != nil || p.Mode != i.Mode || p.Visible != i.Visible || p.SkipAutoLogin != (i.Mode != "none") || p.ConfigurationID != i.ConfigurationID {
			t.Fatalf("intent=%+v plan=%+v err=%v", i, p, err)
		}
		if _, err = Plan(ticket, target, false, now); !errors.Is(err, ErrNotApplied) {
			t.Fatal(err)
		}
		wrong := target
		wrong.BuildID = "other"
		if _, err = Plan(ticket, wrong, true, now); !errors.Is(err, change.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err = Plan(ticket, target, true, ticket.ExpiresAt); !errors.Is(err, ErrExpired) {
			t.Fatal(err)
		}
	}
}

func TestRecoveryClaimIsExclusiveAndDurable(t *testing.T) {
	now := time.Now().UTC()
	target := change.Target{ID: "fixture", Version: "1.0.0", BuildID: "fixture", Layout: "installed"}
	ticket := Ticket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: maintenance.Intent{Mode: "headless", Maintain: true, ConfigurationID: "saved"}}
	dir := t.TempDir()
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _, err := Claim(dir, ticket, target, true, now); results <- err })
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
	if _, err := Claim(dir, ticket, target, true, now); !errors.Is(err, ErrConsumed) {
		t.Fatal(err)
	}
}

func TestInvalidIntentCannotCreateClaim(t *testing.T) {
	now := time.Now().UTC()
	target := change.Target{ID: "fixture", Version: "1", BuildID: "fixture", Layout: "portable"}
	for _, i := range []maintenance.Intent{{Mode: "none", Maintain: true, ConfigurationID: "secret"}, {Mode: "headless", Visible: true}, {Mode: "desktop", ConfigurationID: "saved"}, {Mode: "desktop", Maintain: true}, {Mode: "unknown"}} {
		ticket := Ticket{OperationID: "0123456789abcdef0123456789abcdef", Target: target, ExpiresAt: now.Add(time.Minute), Intent: i}
		if _, err := Claim(t.TempDir(), ticket, target, true, now); !errors.Is(err, change.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
