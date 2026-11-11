package main

import "testing"

func TestRememberDesktopOwnerKeepsTheServicePrincipal(t *testing.T) {
	defer desktopPrincipalByInstance.Delete("inst_pr")
	defer desktopUserByInstance.Delete("inst_pr")
	rememberDesktopOwner("inst_pr", map[string]string{"hub_user_id": "u_1", "hub_tenant_id": "tenant_default"}, "user_srv", "tenant_srv")
	a, ok := desktopInstancePrincipal("inst_pr")
	if !ok || a.UserID != "user_srv" || a.TenantID != "tenant_srv" {
		t.Fatalf("principal = %+v %v", a, ok)
	}
	j := &desktopLongJob{InstanceID: "inst_pr"}
	_ = desktopLongJobResume(t.Context(), j)
	if j.PrincipalUser != "user_srv" {
		t.Fatalf("resume principal = %q", j.PrincipalUser)
	}
}
