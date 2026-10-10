package charger

import (
	"testing"
	"time"
)

func TestEfacecQC45MaxPower(t *testing.T) {
	tests := []struct {
		mode string
		want int
	}{
		{mode: "dc", want: 50},
		{mode: "type2", want: 43},
	}

	for _, tc := range tests {
		charger := &EfacecQC45{mode: tc.mode}
		if got := charger.maxPowerKW(); got != tc.want {
			t.Errorf("mode %s: maxPowerKW()=%d, want %d", tc.mode, got, tc.want)
		}
	}
}

func TestEfacecQC45DCPermitFailClosed(t *testing.T) {
	c := &EfacecQC45{mode: "dc"}
	c.mu.Lock()
	if c.dcBudgetLocked() != 0 {
		t.Fatal("DC must start disabled")
	}
	c.requestedKw = 35
	c.pendingAt = time.Now()
	if c.dcBudgetLocked() != 0 {
		t.Fatal("MaxCurrent must not release power while disabled")
	}
	if c.freshPendingBudgetLocked(time.Now()) != 35 {
		t.Fatal("evcc MaxCurrent immediately before Enable must be accepted")
	}
	c.pendingAt = time.Now().Add(-qc45PendingPermitMaxAge - time.Second)
	if c.freshPendingBudgetLocked(time.Now()) != 0 {
		t.Fatal("stale MaxCurrent must not authorize power")
	}
	c.pendingAt = time.Now()
	c.evccEnabled = true
	if c.dcBudgetLocked() != 35 {
		t.Fatal("explicit evcc enable plus fresh request should permit 35kW")
	}
	c.evccEnabled = false
	c.requestedKw = 0
	c.pendingAt = time.Time{}
	if c.dcBudgetLocked() != 0 {
		t.Fatal("evcc disabled must revoke DC power")
	}
	c.mu.Unlock()
}
