package app

import (
	"encoding/json"
	"testing"
	"time"
	"yuntools/internal/certificates"
	"yuntools/internal/tasks"
)

func TestRenewalScheduleUsesActualLifetimeAndAvoidsDuplicates(t *testing.T) {
	now := time.Now()
	cert := certificates.Certificate{ID: "cert", AccountID: "a", ExpiresAt: now.Add(20 * time.Hour), RenewAfter: now.Add(-time.Hour)}
	if !renewalDue(cert, true, nil, now) {
		t.Fatal("due cert skipped")
	}
	if renewalDue(cert, false, nil, now) {
		t.Fatal("opt-out ignored")
	}
	b, _ := json.Marshal("cert")
	for _, s := range []tasks.State{tasks.Queued, tasks.Running, tasks.Paused, tasks.Failed} {
		if renewalDue(cert, true, []tasks.Job{{AccountID: "a", Kind: "renew", Payload: b, State: s}}, now) {
			t.Fatalf("duplicate renewal for %s", s)
		}
	}
	cert.RenewAfter = now.Add(time.Hour)
	if renewalDue(cert, true, nil, now) {
		t.Fatal("renewal too early")
	}
}
