package httpapi

import (
	"fmt"
	"testing"
)

func TestRegistrationEmailLogIdentity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "***"},
		{"bad", "***"},
		{"ab@example.com", "ab***@example.com"},
		{"alice@example.com", "al***@example.com"},
		{"18701278637@139.com", "18***@139.com"},
		{"  Alice@Example.COM ", "al***@example.com"},
	}
	for _, tc := range cases {
		if got := registrationEmailLogIdentity(tc.in); got != tc.want {
			t.Fatalf("registrationEmailLogIdentity(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestRegistrationEmailDomainLog(t *testing.T) {
	t.Parallel()
	if got := registrationEmailDomainLog("18701278637@139.com"); got != "139.com" {
		t.Fatalf("domain=%q", got)
	}
	if got := registrationEmailDomainLog("bad"); got != "" {
		t.Fatalf("domain=%q", got)
	}
	if got := registrationEmailDomainLog("a@"); got != "" {
		t.Fatalf("domain=%q", got)
	}
}

func TestRegistrationPhoneLogIdentity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "***"},
		{"12", "***12"},
		{"123456", "12***56"},
		{"18701278637", "187***8637"},
		{"+86 187-0127-8637", "187***8637"},
	}
	for _, tc := range cases {
		if got := registrationPhoneLogIdentity(tc.in); got != tc.want {
			t.Fatalf("registrationPhoneLogIdentity(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestRegistrationAccountLogIdentity(t *testing.T) {
	t.Parallel()
	if got := registrationAccountLogIdentity("phone:18701278637"); got != "phone:187***8637" {
		t.Fatalf("phone account=%q", got)
	}
	if got := registrationAccountLogIdentity("PHONE:18701278637"); got != "phone:187***8637" {
		t.Fatalf("upper phone account=%q", got)
	}
	if got := registrationAccountLogIdentity("phone:"); got != "phone:***" {
		t.Fatalf("prefix-only account=%q", got)
	}
	if got := registrationAccountLogIdentity("alice@example.com"); got != "al***@example.com" {
		t.Fatalf("email account=%q", got)
	}
	if got := registrationAccountLogIdentity(""); got != "***" {
		t.Fatalf("empty account=%q", got)
	}
}

func TestRegistrationPhoneRejectCode(t *testing.T) {
	t.Parallel()
	if got := registrationPhoneRejectCode(nil); got != "" {
		t.Fatalf("nil code=%q", got)
	}
	if got := registrationPhoneRejectCode(errPhoneAlreadyRegistered{}); got != "PHONE_ALREADY_REGISTERED" {
		t.Fatalf("code=%q", got)
	}
	if got := registrationPhoneRejectCode(errPhoneRegistrationRouteCheck{err: fmt.Errorf("route")}); got != "PHONE_REGISTRATION_ROUTE_CHECK_FAILED" {
		t.Fatalf("code=%q", got)
	}
	// Prefer concrete typed errors even when wrapped.
	if got := registrationPhoneRejectCode(fmt.Errorf("wrap: %w", errPhoneAlreadyRegistered{})); got != "PHONE_ALREADY_REGISTERED" {
		t.Fatalf("wrapped already code=%q", got)
	}
	if got := registrationPhoneRejectCode(fmt.Errorf("wrap: %w", errPhoneRegistrationLookup{err: fmt.Errorf("db")})); got != "PHONE_REGISTRATION_LOOKUP_FAILED" {
		t.Fatalf("wrapped lookup code=%q", got)
	}
}
