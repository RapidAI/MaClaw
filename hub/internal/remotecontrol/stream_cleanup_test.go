package remotecontrol

import (
	"context"
	"testing"
)

func TestRevokeReleasesStreamRateLimitState(t *testing.T) {
	svc, _, _, _ := testService(t)
	ctx := context.Background()
	grant, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.AllowStream(grant.ID) {
		t.Fatal("new active grant did not receive stream capacity")
	}
	svc.streamMu.Lock()
	_, present := svc.streamBuckets[grant.ID]
	svc.streamMu.Unlock()
	if !present {
		t.Fatal("stream bucket was not allocated")
	}

	if err := svc.Revoke(ctx, "tenant", "user-a", "mac-a", grant.ID); err != nil {
		t.Fatal(err)
	}
	svc.streamMu.Lock()
	_, present = svc.streamBuckets[grant.ID]
	svc.streamMu.Unlock()
	if present {
		t.Fatal("revoked grant retained its stream rate-limit bucket")
	}
}

func TestInvalidStreamLookupDropsRateLimitState(t *testing.T) {
	svc, _, _, _ := testService(t)
	ctx := context.Background()
	grant, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		grantID   string
		machineID string
	}{
		{name: "unknown grant", grantID: "rc_unknown", machineID: "mac-b"},
		{name: "wrong machine", grantID: grant.ID, machineID: "mac-attacker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !svc.AllowStream(test.grantID) {
				t.Fatal("new bucket did not receive initial capacity")
			}
			if _, err := svc.StreamGrant(ctx, test.grantID, test.machineID); err == nil {
				t.Fatal("invalid stream passed validation")
			}
			svc.streamMu.Lock()
			_, present := svc.streamBuckets[test.grantID]
			svc.streamMu.Unlock()
			if present {
				t.Fatal("invalid stream retained its rate-limit bucket")
			}
		})
	}
}

func TestInactiveStreamLookupDropsLegacyRateLimitState(t *testing.T) {
	svc, _, _, _ := testService(t)
	ctx := context.Background()
	grant, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.AllowStream(grant.ID) {
		t.Fatal("new active grant did not receive stream capacity")
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE remote_control_grants SET status = ? WHERE id = ?`, StatusRevoked, grant.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.StreamGrant(ctx, grant.ID, "mac-b"); err == nil {
		t.Fatal("revoked grant passed stream validation")
	}
	svc.streamMu.Lock()
	_, present := svc.streamBuckets[grant.ID]
	svc.streamMu.Unlock()
	if present {
		t.Fatal("inactive stream lookup did not collect legacy bucket")
	}
}
