package corelib

import "time"

// DigitalEmployeeAuthorization describes a digital employee seat authorization pushed by HubCenter.
type DigitalEmployeeAuthorization struct {
	Quota     int    `json:"quota"`
	Enabled   bool   `json:"enabled"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Active    bool   `json:"active"`
	Reason    string `json:"reason,omitempty"`
}

// NormalizeDigitalEmployeeAuthorization returns a sanitized authorization state.
func NormalizeDigitalEmployeeAuthorization(auth DigitalEmployeeAuthorization, now time.Time) DigitalEmployeeAuthorization {
	if auth.Quota < 0 {
		auth.Quota = 0
	}
	if !auth.Enabled {
		auth.Active = false
		if auth.Reason == "" {
			auth.Reason = "disabled"
		}
		return auth
	}
	if auth.Quota <= 0 {
		auth.Active = false
		if auth.Reason == "" {
			auth.Reason = "quota_zero"
		}
		return auth
	}
	if auth.ExpiresAt == "" {
		auth.Active = false
		if auth.Reason == "" {
			auth.Reason = "not_subscribed"
		}
		return auth
	}
	expiresAt, err := time.Parse(time.RFC3339, auth.ExpiresAt)
	if err != nil || !expiresAt.After(now) {
		auth.Active = false
		if auth.Reason == "" {
			auth.Reason = "expired"
		}
		return auth
	}
	auth.Active = true
	auth.Reason = ""
	return auth
}
