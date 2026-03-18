package auth

import (
	"context"
	"net/url"
	"testing"
)

func TestIdentityServiceEnrollmentAndEmailLogin(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	enroll, err := svc.StartEnrollment(ctx, "user@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if enroll == nil || enroll.Status != "approved" {
		t.Fatalf("unexpected enrollment result: %+v", enroll)
	}
	if enroll.Email != "user@example.com" || enroll.SN == "" || enroll.MachineID == "" || enroll.MachineToken == "" {
		t.Fatalf("enrollment missing identity fields: %+v", enroll)
	}

	principal, err := svc.AuthenticateMachine(ctx, enroll.MachineID, enroll.MachineToken)
	if err != nil {
		t.Fatalf("AuthenticateMachine: %v", err)
	}
	if principal == nil || principal.UserID == "" || principal.MachineID != enroll.MachineID {
		t.Fatalf("unexpected machine principal: %+v", principal)
	}

	req, err := svc.RequestEmailLogin(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if req == nil || req.Status != "pending_email_confirmation" {
		t.Fatalf("unexpected login request result: %+v", req)
	}
	if req.Message == "" {
		t.Fatal("expected dev confirm URL message when mailer is nil")
	}

	prefix := "Use this confirm URL for development: "
	if len(req.Message) <= len(prefix) || req.Message[:len(prefix)] != prefix {
		t.Fatalf("unexpected confirm message: %q", req.Message)
	}

	confirmURL := req.Message[len(prefix):]
	if parsedBase, err := url.Parse(confirmURL); err == nil {
		if parsedBase.Scheme != "http" || parsedBase.Host != "127.0.0.1:9399" {
			t.Fatalf("unexpected confirm URL host: %s", confirmURL)
		}
	}
	parsedURL, err := url.Parse(confirmURL)
	if err != nil {
		t.Fatalf("parse confirm URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in confirm URL: %s", confirmURL)
	}
	viewerToken, user, err := svc.ConfirmEmailLogin(ctx, rawToken)
	if err != nil {
		t.Fatalf("ConfirmEmailLogin: %v", err)
	}
	if viewerToken == "" {
		t.Fatal("expected viewer token")
	}
	if user == nil || user.Email != "user@example.com" || user.SN != enroll.SN {
		t.Fatalf("unexpected user after confirm: %+v", user)
	}
}

func TestIdentityServiceApprovalModeCreatesPendingEnrollment(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"approval",
		true,
		nil,
		"http://127.0.0.1:9399",
	)

	result, err := svc.StartEnrollment(context.Background(), "pending@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if result == nil || result.Status != "pending_approval" {
		t.Fatalf("unexpected result: %+v", result)
	}

	pending, err := deps.store.Enrollments.GetPendingByEmail(context.Background(), "pending@example.com")
	if err != nil {
		t.Fatalf("GetPendingByEmail: %v", err)
	}
	if pending == nil {
		t.Fatal("expected pending enrollment record")
	}
}

func TestIdentityServiceManualModeRequiresExistingBinding(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"manual",
		true,
		nil,
		"http://127.0.0.1:9399",
	)

	result, err := svc.StartEnrollment(context.Background(), "manual-only@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if result == nil || result.Status != "manual_binding_required" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
