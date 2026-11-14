package auth

import (
	"context"
	"errors"
	"testing"
)

func TestAuthenticateMachineRejectsDisabledUserAndMachine(t *testing.T) {
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
		"http://hub.local",
	)
	ctx := context.Background()
	enrolled, err := svc.StartEnrollment(ctx, "machine-security@example.com", "office-pc", "windows", "machine-security-client", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateMachine(ctx, enrolled.MachineID, enrolled.MachineToken); err != nil {
		t.Fatalf("active machine authentication failed: %v", err)
	}

	if _, err := deps.provider.Write.ExecContext(ctx, `UPDATE users SET status = 'disabled' WHERE id = ?`, enrolled.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateMachine(ctx, enrolled.MachineID, enrolled.MachineToken); !errors.Is(err, ErrMachineUnauthorized) {
		t.Fatalf("disabled user machine authentication error = %v, want ErrMachineUnauthorized", err)
	}
	if token, err := svc.IssueViewerTokenForUser(ctx, enrolled.UserID); err == nil || token != "" {
		t.Fatalf("disabled user received viewer token=%q, err=%v", token, err)
	}

	if _, err := deps.provider.Write.ExecContext(ctx, `UPDATE users SET status = 'active' WHERE id = ?`, enrolled.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.provider.Write.ExecContext(ctx, `UPDATE machines SET status = 'disabled' WHERE id = ?`, enrolled.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateMachine(ctx, enrolled.MachineID, enrolled.MachineToken); !errors.Is(err, ErrMachineUnauthorized) {
		t.Fatalf("disabled machine authentication error = %v, want ErrMachineUnauthorized", err)
	}
}
