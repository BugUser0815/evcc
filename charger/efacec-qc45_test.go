package charger

import "testing"

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
