package update

import (
	"testing"
	"time"
)

func TestMaintenanceCancellationAndCommitBoundary(t *testing.T) {
	now := time.Now()
	id := "0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"desktop", "headless"} {
		var m Maintenance
		if err := m.Prepare(id, false, RestoreIntent{Mode: mode}, now); err != nil {
			t.Fatal(err)
		}
		if mode == "desktop" {
			if m.Commit(id) == nil {
				t.Fatal("commit before GUI approval")
			}
			if m.Acknowledge(id, true, false) != nil {
				t.Fatal("ack")
			}
		}
		if m.Abort("ffffffffffffffffffffffffffffffff") == nil {
			t.Fatal("foreign cancellation")
		}
		if m.Abort(id) != nil || m.Status().Phase != "idle" {
			t.Fatal("abort")
		}
		if m.Prepare(id, false, RestoreIntent{Mode: mode}, now) != nil {
			t.Fatal("prepare")
		}
		if !m.Expire(now, false) || m.Status().Phase != "idle" {
			t.Fatal("coordinator death did not thaw")
		}
		m.Prepare(id, false, RestoreIntent{Mode: mode}, now)
		if !m.Expire(now.Add(91*time.Second), true) {
			t.Fatal("hung preparation")
		}
		m.Prepare(id, false, RestoreIntent{Mode: mode}, now)
		if mode == "desktop" {
			m.Acknowledge(id, true, true)
		}
		if m.Commit(id) != nil {
			t.Fatal("commit")
		}
		if m.Expire(now.Add(time.Hour), false) || m.Abort(id) == nil {
			t.Fatal("committed execution thawed by timeout/abort")
		}
	}
}
