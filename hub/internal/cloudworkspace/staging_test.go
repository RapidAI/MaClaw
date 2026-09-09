package cloudworkspace

import (
	"context"
	"testing"
	"time"
)

func TestStagingReservationReplacesChunkWithoutDoubleCharging(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "host")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "staging", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := st.ReserveStagingChunk(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 0, 10, 100, 1000, now); err != nil {
		t.Fatal(err)
	}
	if err := st.ReserveStagingChunk(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 0, 20, 100, 1000, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := st.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM cloud_workspace_staging_chunks WHERE workspace_id = ?`, ws.ID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 20 {
		t.Fatalf("staging total=%d want 20", total)
	}
}
