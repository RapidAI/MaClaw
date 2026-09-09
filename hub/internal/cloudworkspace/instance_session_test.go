package cloudworkspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestInstanceSessionIsHubIssuedBoundAndNotStoredPlaintext(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", ClientInstanceID: "cwi_attacker_chosen"}

	issued, err := st.IssueInstanceSession(ctx, principal, CloudWorkspaceProtocol, now)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Token == "" || issued.ID == "" || issued.ClientInstanceID == "" {
		t.Fatalf("issued=%+v", issued)
	}
	if issued.ClientInstanceID == principal.ClientInstanceID {
		t.Fatal("Hub must not accept a caller-chosen client instance id")
	}
	var storedHash string
	if err := st.db.QueryRow(`SELECT token_hash FROM cloud_workspace_instance_sessions WHERE id = ?`, issued.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == issued.Token || storedHash != instanceSessionTokenHash(issued.Token) {
		t.Fatalf("stored token hash=%q", storedHash)
	}

	authenticated, err := st.AuthenticateInstanceSession(ctx, principal, issued.Token, CloudWorkspaceProtocol, now.Add(8*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.ClientInstanceID != issued.ClientInstanceID || authenticated.ExpiresAt <= issued.ExpiresAt {
		t.Fatalf("authenticated=%+v issued=%+v", authenticated, issued)
	}

	wrongMachine := principal
	wrongMachine.MachineID = "m2"
	if _, err := st.AuthenticateInstanceSession(ctx, wrongMachine, issued.Token, CloudWorkspaceProtocol, now.Add(2*time.Minute)); !errors.Is(err, ErrInstanceSessionInvalid) {
		t.Fatalf("wrong machine err=%v", err)
	}
	if _, err := st.AuthenticateInstanceSession(ctx, principal, issued.Token, "v2", now.Add(2*time.Minute)); !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("wrong protocol err=%v", err)
	}
}

func TestInstanceSessionExpiryAndSingleSessionRevocation(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}

	first, err := st.IssueInstanceSession(ctx, principal, CloudWorkspaceProtocol, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.IssueInstanceSession(ctx, principal, CloudWorkspaceProtocol, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ClientInstanceID == second.ClientInstanceID || first.Token == second.Token {
		t.Fatalf("sessions must be independent: first=%+v second=%+v", first, second)
	}
	if err := st.RevokeInstanceSession(ctx, principal, first.ID, first.Token, CloudWorkspaceProtocol, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateInstanceSession(ctx, principal, first.Token, CloudWorkspaceProtocol, now.Add(2*time.Second)); !errors.Is(err, ErrInstanceSessionInvalid) {
		t.Fatalf("revoked token err=%v", err)
	}
	if _, err := st.AuthenticateInstanceSession(ctx, principal, second.Token, CloudWorkspaceProtocol, now.Add(2*time.Second)); err != nil {
		t.Fatalf("revoking first invalidated second: %v", err)
	}
	if _, err := st.AuthenticateInstanceSession(ctx, principal, second.Token, CloudWorkspaceProtocol, now.Add(2*time.Second+InstanceSessionTTL+time.Second)); !errors.Is(err, ErrInstanceSessionInvalid) {
		t.Fatalf("expired token err=%v", err)
	}
}
