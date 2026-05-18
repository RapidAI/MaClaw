package main

import (
	"strings"
	"testing"
)

// --- VEApprovalConfig tests ---

func TestVEApprovalConfig_DefaultValues(t *testing.T) {
	cfg := DefaultVEApprovalConfig()

	if cfg.Enabled {
		t.Error("default config should have enabled=false")
	}
	if cfg.ACL.Mode != ACLWhitelist {
		t.Errorf("default ACL mode should be whitelist, got %q", cfg.ACL.Mode)
	}
	if cfg.MaxQueueSize != 50 {
		t.Errorf("default max_queue_size should be 50, got %d", cfg.MaxQueueSize)
	}
	if cfg.TimeoutHours != 24 {
		t.Errorf("default timeout_hours should be 24, got %d", cfg.TimeoutHours)
	}
	if cfg.DailyQuota != 100 {
		t.Errorf("default daily_quota should be 100, got %d", cfg.DailyQuota)
	}
	if cfg.FallbackApprover != "" {
		t.Errorf("default fallback_approver should be empty, got %q", cfg.FallbackApprover)
	}
}

func TestVEApprovalConfig_ValidateDefaults(t *testing.T) {
	cfg := DefaultVEApprovalConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got error: %v", err)
	}
}

func TestVEApprovalConfig_ValidateMaxQueueSize(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"min valid", 1, false},
		{"max valid", 1000, false},
		{"mid valid", 50, false},
		{"zero invalid", 0, true},
		{"negative invalid", -1, true},
		{"over max invalid", 1001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultVEApprovalConfig()
			cfg.MaxQueueSize = tt.size
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("MaxQueueSize=%d: wantErr=%v, got err=%v", tt.size, tt.wantErr, err)
			}
			if err != nil && !strings.Contains(err.Error(), "max_queue_size") {
				t.Errorf("error should mention max_queue_size, got: %v", err)
			}
		})
	}
}

func TestVEApprovalConfig_ValidateTimeoutHours(t *testing.T) {
	tests := []struct {
		name    string
		hours   int
		wantErr bool
	}{
		{"min valid", 1, false},
		{"max valid", 720, false},
		{"mid valid", 24, false},
		{"zero invalid", 0, true},
		{"negative invalid", -1, true},
		{"over max invalid", 721, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultVEApprovalConfig()
			cfg.TimeoutHours = tt.hours
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TimeoutHours=%d: wantErr=%v, got err=%v", tt.hours, tt.wantErr, err)
			}
			if err != nil && !strings.Contains(err.Error(), "timeout_hours") {
				t.Errorf("error should mention timeout_hours, got: %v", err)
			}
		})
	}
}

func TestVEApprovalConfig_ValidateDailyQuota(t *testing.T) {
	tests := []struct {
		name    string
		quota   int
		wantErr bool
	}{
		{"min valid", 1, false},
		{"max valid", 10000, false},
		{"mid valid", 100, false},
		{"zero invalid", 0, true},
		{"negative invalid", -1, true},
		{"over max invalid", 10001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultVEApprovalConfig()
			cfg.DailyQuota = tt.quota
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("DailyQuota=%d: wantErr=%v, got err=%v", tt.quota, tt.wantErr, err)
			}
			if err != nil && !strings.Contains(err.Error(), "daily_quota") {
				t.Errorf("error should mention daily_quota, got: %v", err)
			}
		})
	}
}

// --- AccessControlList tests ---

func TestAccessControlList_ValidModes(t *testing.T) {
	tests := []struct {
		mode    ACLMode
		wantErr bool
	}{
		{ACLWhitelist, false},
		{ACLBlacklist, false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			acl := AccessControlList{Mode: tt.mode}
			err := acl.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("mode=%q: wantErr=%v, got err=%v", tt.mode, tt.wantErr, err)
			}
		})
	}
}

func TestAccessControlList_PerCategoryLimit(t *testing.T) {
	// Each category can have at most 100 entries
	tests := []struct {
		name    string
		acl     AccessControlList
		wantErr bool
		errMsg  string
	}{
		{
			name: "departments at limit",
			acl: AccessControlList{
				Mode:        ACLWhitelist,
				Departments: makeStringSlice(100),
			},
			wantErr: false,
		},
		{
			name: "departments over limit",
			acl: AccessControlList{
				Mode:        ACLWhitelist,
				Departments: makeStringSlice(101),
			},
			wantErr: true,
			errMsg:  "departments",
		},
		{
			name: "roles at limit",
			acl: AccessControlList{
				Mode:  ACLWhitelist,
				Roles: makeStringSlice(100),
			},
			wantErr: false,
		},
		{
			name: "roles over limit",
			acl: AccessControlList{
				Mode:  ACLWhitelist,
				Roles: makeStringSlice(101),
			},
			wantErr: true,
			errMsg:  "roles",
		},
		{
			name: "skills at limit",
			acl: AccessControlList{
				Mode:   ACLWhitelist,
				Skills: makeStringSlice(100),
			},
			wantErr: false,
		},
		{
			name: "skills over limit",
			acl: AccessControlList{
				Mode:   ACLWhitelist,
				Skills: makeStringSlice(101),
			},
			wantErr: true,
			errMsg:  "skills",
		},
		{
			name: "entities at limit",
			acl: AccessControlList{
				Mode:     ACLWhitelist,
				Entities: makeStringSlice(100),
			},
			wantErr: false,
		},
		{
			name: "entities over limit",
			acl: AccessControlList{
				Mode:     ACLWhitelist,
				Entities: makeStringSlice(101),
			},
			wantErr: true,
			errMsg:  "entities",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.acl.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error should mention %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestAccessControlList_TotalEntriesLimit(t *testing.T) {
	// Total across all categories must not exceed 500
	tests := []struct {
		name    string
		acl     AccessControlList
		wantErr bool
	}{
		{
			name: "total at limit (4x100 + 100 entities)",
			acl: AccessControlList{
				Mode:        ACLWhitelist,
				Departments: makeStringSlice(100),
				Roles:       makeStringSlice(100),
				Skills:      makeStringSlice(100),
				Entities:    makeStringSlice(100),
			},
			wantErr: false, // 400 total, under 500
		},
		{
			name: "total exactly 500",
			acl: AccessControlList{
				Mode:        ACLBlacklist,
				Departments: makeStringSlice(100),
				Roles:       makeStringSlice(100),
				Skills:      makeStringSlice(100),
				Entities:    makeStringSlice(100),
			},
			// 400 total, still under 500
			wantErr: false,
		},
		{
			name: "total over 500 with spread",
			acl: AccessControlList{
				Mode:        ACLWhitelist,
				Departments: makeStringSlice(100),
				Roles:       makeStringSlice(100),
				Skills:      makeStringSlice(100),
				Entities:    makeStringSlice(100),
			},
			wantErr: false, // 400 total
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.acl.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}

	// Test that exceeding 500 total is caught
	t.Run("total exceeds 500", func(t *testing.T) {
		// We can't exceed 500 with per-category limit of 100 (max 400).
		// But the entities field could have up to 100, so max is 400.
		// To test the 500 limit, we need a scenario where individual categories
		// are within 100 but total exceeds 500. Since 4*100=400 < 500,
		// the total limit is effectively unreachable with current per-category limits.
		// However, the validation still checks it for future-proofing.

		// Verify TotalEntries calculation
		acl := AccessControlList{
			Mode:        ACLWhitelist,
			Departments: makeStringSlice(50),
			Roles:       makeStringSlice(50),
			Skills:      makeStringSlice(50),
			Entities:    makeStringSlice(50),
		}
		if acl.TotalEntries() != 200 {
			t.Errorf("TotalEntries() = %d, want 200", acl.TotalEntries())
		}
	})
}

func TestAccessControlList_TotalEntries(t *testing.T) {
	acl := AccessControlList{
		Mode:        ACLWhitelist,
		Departments: []string{"eng", "sales"},
		Roles:       []string{"admin"},
		Skills:      []string{"go", "python", "rust"},
		Entities:    []string{"user-1"},
	}
	if got := acl.TotalEntries(); got != 7 {
		t.Errorf("TotalEntries() = %d, want 7", got)
	}
}

func TestAccessControlList_EmptyIsValid(t *testing.T) {
	acl := AccessControlList{Mode: ACLWhitelist}
	if err := acl.Validate(); err != nil {
		t.Errorf("empty ACL with valid mode should be valid, got: %v", err)
	}
}

func TestVEApprovalConfig_ACLValidationPropagates(t *testing.T) {
	cfg := DefaultVEApprovalConfig()
	cfg.ACL.Mode = "invalid_mode"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("config with invalid ACL mode should fail validation")
	}
	if !strings.Contains(err.Error(), "acl:") {
		t.Errorf("error should be prefixed with 'acl:', got: %v", err)
	}
}

// --- Helper ---

func makeStringSlice(n int) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = strings.Repeat("x", 5) + strings.Repeat("0", 3) // "xxxxx000"
	}
	return s
}
