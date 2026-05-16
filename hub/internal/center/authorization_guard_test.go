package center

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldAcceptAuthorizationUpdate(t *testing.T) {
	active5 := &corelib.DigitalEmployeeAuthorization{Active: true, Quota: 5, Enabled: true, ExpiresAt: "2027-01-01T00:00:00Z"}
	disabled5 := &corelib.DigitalEmployeeAuthorization{Active: false, Quota: 5, Enabled: false, Reason: "disabled"}
	expired5 := &corelib.DigitalEmployeeAuthorization{Active: false, Quota: 5, Enabled: true, Reason: "expired", ExpiresAt: "2020-01-01T00:00:00Z"}
	zeroDefault := &corelib.DigitalEmployeeAuthorization{Active: false, Quota: 0, Reason: "quota_zero"}
	notSubscribed := &corelib.DigitalEmployeeAuthorization{Active: false, Quota: 0, Reason: "not_subscribed"}

	tests := []struct {
		name     string
		local    *corelib.DigitalEmployeeAuthorization
		incoming *corelib.DigitalEmployeeAuthorization
		want     bool
	}{
		// No local value — always accept
		{"nil local, active incoming", nil, active5, true},
		{"nil local, zero incoming", nil, zeroDefault, true},
		{"nil local, disabled incoming", nil, disabled5, true},
		{"nil local, nil incoming", nil, nil, false},

		// Local inactive — always accept (nothing to protect)
		{"inactive local, active incoming", zeroDefault, active5, true},
		{"inactive local, zero incoming", zeroDefault, zeroDefault, true},

		// Local active — accept upgrades and explicit admin actions
		{"active local, active incoming (same)", active5, active5, true},
		{"active local, active incoming (upgrade)", active5, &corelib.DigitalEmployeeAuthorization{Active: true, Quota: 10, Enabled: true}, true},
		{"active local, explicit disable", active5, disabled5, true},
		{"active local, expired with quota", active5, expired5, true},

		// Local active — reject suspicious zero-quota responses (HA lag)
		{"active local, zero default incoming", active5, zeroDefault, false},
		{"active local, not_subscribed incoming", active5, notSubscribed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAcceptAuthorizationUpdate(tt.local, tt.incoming)
			if got != tt.want {
				t.Errorf("shouldAcceptAuthorizationUpdate(%+v, %+v) = %v, want %v", tt.local, tt.incoming, got, tt.want)
			}
		})
	}
}
