package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func createCloudWorkspace(t *testing.T, h http.Handler, machineID, name string) string {
	t.Helper()
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", machineID, "secret", map[string]any{"name": name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	return ws.ID
}

func decodeLeaseBody(t *testing.T, raw []byte) cloudworkspace.AcquireOutcome {
	t.Helper()
	var out cloudworkspace.AcquireOutcome
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode lease %q: %v", raw, err)
	}
	return out
}

func TestCloudWorkspaceConcurrentLeaseAcquireOnlyOne200(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	path := "/api/v1/cloud-workspaces/" + id + "/leases"
	type result struct {
		code int
		body string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, mid := range []string{"m1", "m1b"} {
		wg.Add(1)
		go func(mid string) {
			defer wg.Done()
			rec := doCloudWorkspaceRequest(t, h, http.MethodPost, path, mid, "secret", map[string]any{"force": false})
			results <- result{code: rec.Code, body: rec.Body.String()}
		}(mid)
	}
	wg.Wait()
	close(results)
	var ok, conflict int
	var bodies []string
	for rec := range results {
		bodies = append(bodies, rec.body)
		switch rec.code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status=%d body=%s", rec.code, rec.body)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d bodies=%v", ok, conflict, bodies)
	}
}

func TestCloudWorkspaceExpiredLeaseStolenByOtherMachine(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return clock }
	id := createCloudWorkspace(t, h, "m1", "A")
	path := "/api/v1/cloud-workspaces/" + id + "/leases"
	first := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{"force": false})
	if first.Code != http.StatusOK {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	got := decodeLeaseBody(t, first.Body.Bytes())
	if got.Acquired != cloudworkspace.AcquiredGranted {
		t.Fatalf("first=%+v", got)
	}
	blocked := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1b", "secret", map[string]any{"force": false})
	if blocked.Code != http.StatusConflict {
		t.Fatalf("unexpired=%d %s", blocked.Code, blocked.Body.String())
	}
	var inUse struct {
		Error             string `json:"error"`
		HolderMachineID   string `json:"holder_machine_id"`
		HolderMachineName string `json:"holder_machine_name"`
	}
	if err := json.Unmarshal(blocked.Body.Bytes(), &inUse); err != nil {
		t.Fatal(err)
	}
	if inUse.Error != "CLOUD_WORKSPACE_IN_USE" || inUse.HolderMachineID != "m1" || inUse.HolderMachineName != "DESKTOP-M1" {
		t.Fatalf("in use=%+v", inUse)
	}
	clock = clock.Add(cloudworkspace.LeaseTTL)
	stolen := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1b", "secret", map[string]any{"force": false})
	if stolen.Code != http.StatusOK {
		t.Fatalf("stolen=%d %s", stolen.Code, stolen.Body.String())
	}
	gotStolen := decodeLeaseBody(t, stolen.Body.Bytes())
	if gotStolen.Acquired != cloudworkspace.AcquiredGranted || gotStolen.LeaseID == got.LeaseID {
		t.Fatalf("stolen=%+v first=%+v", gotStolen, got)
	}
}

func TestCloudWorkspaceSameMachineLeaseRenews(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	path := "/api/v1/cloud-workspaces/" + id + "/leases"
	first := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{"force": false})
	if first.Code != http.StatusOK {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	got := decodeLeaseBody(t, first.Body.Bytes())
	renewed := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{"force": false})
	if renewed.Code != http.StatusOK {
		t.Fatalf("renew=%d %s", renewed.Code, renewed.Body.String())
	}
	gotRenew := decodeLeaseBody(t, renewed.Body.Bytes())
	if gotRenew.Acquired != cloudworkspace.AcquiredRenewed || gotRenew.LeaseID != got.LeaseID {
		t.Fatalf("renew=%+v first=%+v", gotRenew, got)
	}
}

func TestCloudWorkspaceHeartbeat409AfterSteal(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	path := "/api/v1/cloud-workspaces/" + id + "/leases"
	first := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{"force": false})
	if first.Code != http.StatusOK {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	got := decodeLeaseBody(t, first.Body.Bytes())
	forced := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1b", "secret", map[string]any{"force": true})
	if forced.Code != http.StatusOK {
		t.Fatalf("force=%d %s", forced.Code, forced.Body.String())
	}
	gotForced := decodeLeaseBody(t, forced.Body.Bytes())
	if gotForced.Acquired != cloudworkspace.AcquiredGranted {
		t.Fatalf("forced=%+v", gotForced)
	}
	hb := doCloudWorkspaceRequest(t, h, http.MethodPost, path+"/"+got.LeaseID+"/heartbeat", "m1", "secret", nil)
	if hb.Code != http.StatusConflict {
		t.Fatalf("old heartbeat=%d %s", hb.Code, hb.Body.String())
	}
	var inUse struct {
		Error           string `json:"error"`
		HolderMachineID string `json:"holder_machine_id"`
	}
	if err := json.Unmarshal(hb.Body.Bytes(), &inUse); err != nil {
		t.Fatal(err)
	}
	if inUse.Error != "CLOUD_WORKSPACE_IN_USE" || inUse.HolderMachineID != "m1b" {
		t.Fatalf("old heartbeat body=%s", hb.Body.String())
	}
	okHB := doCloudWorkspaceRequest(t, h, http.MethodPost, path+"/"+gotForced.LeaseID+"/heartbeat", "m1b", "secret", nil)
	if okHB.Code != http.StatusOK {
		t.Fatalf("new heartbeat=%d %s", okHB.Code, okHB.Body.String())
	}
}

func TestCloudWorkspaceDeleteBlockedWhenOtherMachineHoldsLease(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	path := "/api/v1/cloud-workspaces/" + id + "/leases"
	if rec := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1b", "secret", map[string]any{"force": false}); rec.Code != http.StatusOK {
		t.Fatalf("acquire=%d %s", rec.Code, rec.Body.String())
	}
	blocked := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+id, "m1", "secret", nil)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("delete=%d %s", blocked.Code, blocked.Body.String())
	}
	var inUse struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(blocked.Body.Bytes(), &inUse); err != nil {
		t.Fatal(err)
	}
	if inUse.Error != "CLOUD_WORKSPACE_IN_USE" {
		t.Fatalf("body=%s", blocked.Body.String())
	}
}

func TestCloudWorkspaceLeaseRejectsClientMachineName(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+id+"/leases", "m1", "secret", map[string]any{
		"force":        false,
		"machine_name": "spoofed",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudWorkspaceEntitlementLeaseIsSelf(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	if rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+id+"/leases", "m1", "secret", map[string]any{"force": false}); rec.Code != http.StatusOK {
		t.Fatalf("acquire=%d %s", rec.Code, rec.Body.String())
	}
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("entitlement=%d %s", rec.Code, rec.Body.String())
	}
	var ent cloudworkspace.Entitlement
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatal(err)
	}
	if len(ent.Workspaces) != 1 || ent.Workspaces[0].Lease == nil {
		t.Fatalf("ent=%+v", ent)
	}
	lease := ent.Workspaces[0].Lease
	if !lease.Held || !lease.IsSelf || lease.MachineID != "m1" || lease.MachineName != "DESKTOP-M1" {
		t.Fatalf("lease=%+v", lease)
	}
	if ent.Workspaces[0].LeaseInUse {
		t.Fatalf("self lease must not set lease_in_use: %+v", ent.Workspaces[0])
	}
	other := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1b", "secret", nil)
	if other.Code != http.StatusOK {
		t.Fatalf("other entitlement=%d %s", other.Code, other.Body.String())
	}
	var entOther cloudworkspace.Entitlement
	if err := json.Unmarshal(other.Body.Bytes(), &entOther); err != nil {
		t.Fatal(err)
	}
	if len(entOther.Workspaces) != 1 || entOther.Workspaces[0].Lease == nil || entOther.Workspaces[0].Lease.IsSelf {
		t.Fatalf("other ent=%+v", entOther)
	}
	if !entOther.Workspaces[0].LeaseInUse || entOther.Workspaces[0].LeaseHolder != "DESKTOP-M1" {
		t.Fatalf("other machine should see occupied: %+v", entOther.Workspaces[0])
	}
}
