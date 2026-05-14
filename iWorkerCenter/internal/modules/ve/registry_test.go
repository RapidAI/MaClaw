package ve

import (
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T, quota int) *Registry {
	t.Helper()
	dir := t.TempDir()
	key := testKeyMat()
	hubID := "hub-reg-test"

	quotaFP := filepath.Join(dir, "quota.enc")
	qs := NewQuotaStore(key, hubID, quotaFP)
	if err := qs.SaveQuota(quota); err != nil {
		t.Fatal(err)
	}

	regFP := filepath.Join(dir, "ve_registry.json")
	return NewRegistry(qs, regFP)
}

func TestRegistry_Register_Success(t *testing.T) {
	reg := newTestRegistry(t, 5)

	ve, err := reg.Register(VERegistrationRequest{
		OwnerMachineID: "machine-001",
		OwnerAgentID:   "agent-001",
		Name:           "测试员工",
		SkillDesc:      "擅长代码审查",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if ve.Status != VEStatusPending {
		t.Errorf("Status = %q, want pending", ve.Status)
	}
	if ve.ID == "" {
		t.Error("ID should not be empty")
	}
}

func TestRegistry_Register_QuotaExceeded(t *testing.T) {
	reg := newTestRegistry(t, 1)

	// Register and approve first VE
	ve1, err := reg.Register(VERegistrationRequest{
		OwnerMachineID: "machine-001",
		Name:           "VE1",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Approve(ve1.ID); err != nil {
		t.Fatal(err)
	}

	// Second registration should fail (quota=1, active=1)
	_, err = reg.Register(VERegistrationRequest{
		OwnerMachineID: "machine-002",
		Name:           "VE2",
		AccessPolicy:   PolicyPublic,
	})
	if err == nil {
		t.Fatal("Register() should fail when quota exceeded")
	}
	var qe *QuotaExceededError
	if !isQuotaExceeded(err, &qe) {
		t.Errorf("expected QuotaExceededError, got: %v", err)
	}
}

func TestRegistry_Approve_Reject_Disable(t *testing.T) {
	reg := newTestRegistry(t, 10)

	// Register
	ve, _ := reg.Register(VERegistrationRequest{
		OwnerMachineID: "m1",
		Name:           "Worker",
		AccessPolicy:   PolicyPublic,
	})

	// Approve
	if err := reg.Approve(ve.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	got, _ := reg.GetByID(ve.ID)
	if got.Status != VEStatusActive {
		t.Errorf("after Approve: Status = %q, want active", got.Status)
	}

	// Cannot approve again
	if err := reg.Approve(ve.ID); err == nil {
		t.Error("Approve() on active VE should fail")
	}

	// Disable
	if err := reg.Disable(ve.ID); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	got, _ = reg.GetByID(ve.ID)
	if got.Status != VEStatusDisabled {
		t.Errorf("after Disable: Status = %q, want disabled", got.Status)
	}
}

func TestRegistry_Reject(t *testing.T) {
	reg := newTestRegistry(t, 10)

	ve, _ := reg.Register(VERegistrationRequest{
		OwnerMachineID: "m2",
		Name:           "Worker2",
		AccessPolicy:   PolicyPublic,
	})

	if err := reg.Reject(ve.ID, "不符合要求"); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	got, _ := reg.GetByID(ve.ID)
	if got.Status != VEStatusRejected {
		t.Errorf("after Reject: Status = %q, want rejected", got.Status)
	}
	if got.RejectReason != "不符合要求" {
		t.Errorf("RejectReason = %q, want '不符合要求'", got.RejectReason)
	}
}

func TestRegistry_ListDiscoverable_AccessPolicy(t *testing.T) {
	reg := newTestRegistry(t, 10)

	// Create and approve VEs with different policies
	policies := []struct {
		machineID string
		name      string
		policy    AccessPolicy
		whitelist []string
		blacklist []string
	}{
		{"m-pub", "Public VE", PolicyPublic, nil, nil},
		{"m-wl", "Whitelist VE", PolicyWhitelist, []string{"requester-A"}, nil},
		{"m-bl", "Blacklist VE", PolicyBlacklist, nil, []string{"requester-B"}},
		{"m-pr", "PerRequest VE", PolicyPerRequest, nil, nil},
	}

	for _, p := range policies {
		ve, err := reg.Register(VERegistrationRequest{
			OwnerMachineID: p.machineID,
			Name:           p.name,
			AccessPolicy:   p.policy,
			Whitelist:      p.whitelist,
			Blacklist:      p.blacklist,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Approve(ve.ID); err != nil {
			t.Fatal(err)
		}
	}

	// requester-A: should see public + whitelist(in list) + blacklist(not in blacklist) + per_request = 4
	list := reg.ListDiscoverable("requester-A")
	if len(list) != 4 {
		t.Errorf("requester-A sees %d VEs, want 4", len(list))
	}

	// requester-B: public=visible, whitelist=not in list so invisible, blacklist=IN blacklist so invisible, per_request=visible = 2
	list = reg.ListDiscoverable("requester-B")
	if len(list) != 2 {
		t.Errorf("requester-B sees %d VEs, want 2", len(list))
	}

	// requester-C: public=visible, whitelist=not in list so invisible, blacklist=not in blacklist so visible, per_request=visible = 3
	list = reg.ListDiscoverable("requester-C")
	if len(list) != 3 {
		t.Errorf("requester-C sees %d VEs, want 3", len(list))
	}
}

func TestRegistry_CanAccess(t *testing.T) {
	reg := newTestRegistry(t, 10)

	ve, _ := reg.Register(VERegistrationRequest{
		OwnerMachineID: "m-owner",
		Name:           "PerReq VE",
		AccessPolicy:   PolicyPerRequest,
	})
	reg.Approve(ve.ID)

	allowed, needsAuth, err := reg.CanAccess(ve.ID, "some-requester")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("per_request VE should not directly allow access")
	}
	if !needsAuth {
		t.Error("per_request VE should require auth")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	key := testKeyMat()
	hubID := "hub-persist"
	quotaFP := filepath.Join(dir, "quota.enc")
	qs := NewQuotaStore(key, hubID, quotaFP)
	qs.SaveQuota(10)

	regFP := filepath.Join(dir, "ve_registry.json")

	// Create and register
	reg1 := NewRegistry(qs, regFP)
	ve, _ := reg1.Register(VERegistrationRequest{
		OwnerMachineID: "m-persist",
		Name:           "Persistent VE",
		AccessPolicy:   PolicyPublic,
	})
	reg1.Approve(ve.ID)

	// Load from disk with new instance
	reg2 := NewRegistry(qs, regFP)
	got, ok := reg2.GetByID(ve.ID)
	if !ok {
		t.Fatal("VE not found after reload")
	}
	if got.Name != "Persistent VE" {
		t.Errorf("Name = %q, want 'Persistent VE'", got.Name)
	}
	if got.Status != VEStatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := newTestRegistry(t, 10)

	_, err := reg.Register(VERegistrationRequest{
		OwnerMachineID: "m-dup",
		Name:           "First",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Same machine trying to register again
	_, err = reg.Register(VERegistrationRequest{
		OwnerMachineID: "m-dup",
		Name:           "Second",
		AccessPolicy:   PolicyPublic,
	})
	if err == nil {
		t.Error("duplicate registration from same machine should fail")
	}
}

func TestRegistry_InputValidation(t *testing.T) {
	reg := newTestRegistry(t, 10)

	tests := []struct {
		name string
		req  VERegistrationRequest
	}{
		{"empty name", VERegistrationRequest{OwnerMachineID: "m1", Name: "", AccessPolicy: PolicyPublic}},
		{"name too long", VERegistrationRequest{OwnerMachineID: "m1", Name: string(make([]rune, 51)), AccessPolicy: PolicyPublic}},
		{"invalid policy", VERegistrationRequest{OwnerMachineID: "m1", Name: "X", AccessPolicy: "invalid"}},
		{"empty machine_id", VERegistrationRequest{OwnerMachineID: "", Name: "X", AccessPolicy: PolicyPublic}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.Register(tt.req)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

// helper to check QuotaExceededError type
func isQuotaExceeded(err error, target **QuotaExceededError) bool {
	var qe *QuotaExceededError
	if ok := errorAs(err, &qe); ok {
		*target = qe
		return true
	}
	return false
}

func errorAs(err error, target interface{}) bool {
	// Simple type assertion since errors.As requires Go 1.13+
	switch t := target.(type) {
	case **QuotaExceededError:
		if qe, ok := err.(*QuotaExceededError); ok {
			*t = qe
			return true
		}
	}
	return false
}
