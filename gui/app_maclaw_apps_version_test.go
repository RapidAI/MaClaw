package main

import "testing"

func TestMaclawAppDependencyVersionSatisfied(t *testing.T) {
	cases := []struct {
		name      string
		required  string
		installed string
		want      bool
	}{
		// The reported bug: newer installed version must satisfy a lower minimum.
		{"newer_major_satisfies_minimum", "1.0.0", "10", true},
		{"newer_patch_satisfies_minimum", "1.0.0", "1.0.3", true},
		{"newer_minor_satisfies_minimum", "1.0.0", "1.2.0", true},

		// Cosmetic differences must not cause a false mismatch.
		{"v_prefix_installed", "1.0.0", "v1.0.0", true},
		{"v_prefix_required", "v1.0.0", "1.0.0", true},
		{"uppercase_v_prefix", "1.0.0", "V1.0.0", true},
		{"whitespace", "1.0.0", " 1.0.0 ", true},
		{"segment_count_short_installed", "1.0.0", "1.0", true},
		{"segment_count_short_required", "1.0", "1.0.0", true},
		{"exact_match", "2.3.4", "2.3.4", true},

		// Older installed version does NOT satisfy the minimum.
		{"older_major", "2.0.0", "1.0.0", false},
		{"older_minor", "1.2.0", "1.1.0", false},
		{"older_patch", "1.0.5", "1.0.2", false},

		// Constraint expressions are accepted (no solver here).
		{"caret_constraint", "^1.0.0", "1.5.0", true},
		{"gte_constraint", ">=1.0.0", "0.9.0", true},
		{"tilde_constraint", "~1.2.0", "1.2.9", true},
		{"wildcard_constraint", "1.*", "9.9.9", true},

		// Non-numeric codenames fall back to normalized exact match.
		{"codename_equal", "stable", "stable", true},
		{"codename_v_prefix", "vstable", "stable", true},
		{"codename_differ", "stable", "beta", false},
		{"codename_vs_numeric", "1.0.0", "stable", false},

		// Empty handling.
		{"both_empty", "", "", true},
		{"required_empty", "", "1.0.0", false},
		{"installed_empty", "1.0.0", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maclawAppDependencyVersionSatisfied(tc.required, tc.installed)
			if got != tc.want {
				t.Fatalf("maclawAppDependencyVersionSatisfied(required=%q, installed=%q) = %v, want %v",
					tc.required, tc.installed, got, tc.want)
			}
		})
	}
}

func TestMaclawAppDependencyVersionStatus(t *testing.T) {
	cases := []struct {
		name string
		dep  maclawAppInstallPlanDependency
		want string
	}{
		{
			name: "reported_bug_installed_10_required_1_0_0",
			dep:  maclawAppInstallPlanDependency{RequiredVersion: "1.0.0", InstalledVersion: "10"},
			want: "matched",
		},
		{
			name: "falls_back_to_version_field_when_required_empty",
			dep:  maclawAppInstallPlanDependency{Version: "1.0.0", InstalledVersion: "1.0.0"},
			want: "matched",
		},
		{
			name: "no_required_version",
			dep:  maclawAppInstallPlanDependency{InstalledVersion: "1.0.0"},
			want: "",
		},
		{
			name: "installed_unknown",
			dep:  maclawAppInstallPlanDependency{RequiredVersion: "1.0.0"},
			want: "unknown",
		},
		{
			name: "older_installed_is_mismatch",
			dep:  maclawAppInstallPlanDependency{RequiredVersion: "2.0.0", InstalledVersion: "1.0.0"},
			want: "mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maclawAppDependencyVersionStatus(tc.dep)
			if got != tc.want {
				t.Fatalf("maclawAppDependencyVersionStatus(%+v) = %q, want %q", tc.dep, got, tc.want)
			}
		})
	}
}

func TestMaclawAppVersionIsNumeric(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"1.0.0", true},
		{"10", true},
		{"v2.0", true},
		{"2.0-beta", true},
		{"stable", false},
		{"latest", false},
		{"", false},
		{"v", false},
	}
	for _, tc := range cases {
		t.Run(tc.v, func(t *testing.T) {
			if got := maclawAppVersionIsNumeric(tc.v); got != tc.want {
				t.Fatalf("maclawAppVersionIsNumeric(%q) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestMaclawAppNeedsReviewDependencyDiagnosticsBlock(t *testing.T) {
	if !maclawAppDependencyDiagnosticStatusFailed("needs_review") {
		t.Fatal("needs_review diagnostic status should be treated as failed")
	}
	if !maclawAppDependencyDiagnosticCodeFailed("skill_needs_review_required") {
		t.Fatal("needs_review diagnostic code should be treated as failed")
	}
	cases := []map[string]any{
		{"installed": true, "health": "needs_review"},
		{"installed": true, "installed_status": "needs_review"},
		{"installed": true, "action": "needs_review"},
	}
	for _, tc := range cases {
		if !maclawAppVerifiedDependencyBlocked(tc) {
			t.Fatalf("needs_review dependency should block: %#v", tc)
		}
	}
}
