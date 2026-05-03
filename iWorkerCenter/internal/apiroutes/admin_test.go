package apiroutes

import "testing"

func TestIsAdminAPIPathIncludesSystemTenantMode(t *testing.T) {
	if !IsAdminAPIPath("/admin/system/tenant-mode") {
		t.Fatal("/admin/system/tenant-mode should be routed to admin API mux")
	}
}

func TestIsAdminAPIPathLeavesSPAAssetsAlone(t *testing.T) {
	if IsAdminAPIPath("/admin/assets/index.js") {
		t.Fatal("SPA assets should not be routed to admin API mux")
	}
}
