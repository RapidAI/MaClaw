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

		// Hub content keys vs human semver (asymmetric coordinate policy).
		// plain required + hash installed → accept (PDF翻译工具 regression).
		{"semver_vs_enterprise_hash_key", "1.0.0", "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53", true},
		// hash required (content pin) + plain semver → reject (pin not proven).
		{"enterprise_hash_key_vs_semver", "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53", "1.0.0", false},
		// full content key ↔ bare digest of the same hash → pin proven.
		{"enterprise_hash_key_vs_bare_digest", "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53", "d774c84f9b53", true},
		{"bare_digest_vs_enterprise_hash_key", "d774c84f9b53", "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53", true},
		{"bare_digest_mismatch_vs_key", "aaaaaaaaaaaa", "enterprise_hub:skill:foo@bbbbbbbbbbbb", false},
		{"enterprise_keys_equal_case", "enterprise_hub:skill:foo@AbC", "enterprise_hub:skill:foo@abc", true},
		{"enterprise_hash_keys_differ", "enterprise_hub:skill:foo@aaaabbbbcccc", "enterprise_hub:skill:foo@dddddeeeeeff", false},

		// Same hub identity with comparable semver @suffix (skillmarket-style).
		{"skillmarket_keys_newer_suffix", "skillmarket:skill:foo@1.0.0", "skillmarket:skill:foo@1.2.0", true},
		{"skillmarket_keys_older_suffix", "skillmarket:skill:foo@2.0.0", "skillmarket:skill:foo@1.0.0", false},
		{"skillmarket_key_vs_plain_newer", "1.0.0", "skillmarket:skill:foo@1.2.0", true},
		{"skillmarket_key_vs_plain_older", "2.0.0", "skillmarket:skill:foo@1.0.0", false},
		{"plain_vs_skillmarket_key_older", "skillmarket:skill:foo@2.0.0", "1.0.0", false},
		{"plain_vs_skillmarket_key_newer", "skillmarket:skill:foo@1.0.0", "1.5.0", true},
		{"source_keys_different_targets", "skillmarket:skill:foo@1.0.0", "skillmarket:skill:bar@1.0.0", false},
		{"hubcenter_skillmarket_kind_alias", "hubcenter:skill:foo@1.0.0", "skillmarket:skill:foo@1.2.0", true},
		{"required_key_without_suffix", "enterprise_hub:skill:foo", "enterprise_hub:skill:foo@d774c84f9b53", true},
		// Dual-key mixed suffix must follow the same policy as cross-coordinate.
		{"dual_key_semver_vs_hash_suffix", "skillmarket:skill:foo@1.0.0", "skillmarket:skill:foo@d774c84f9b53", true},
		{"dual_key_hash_pin_vs_semver_suffix", "skillmarket:skill:foo@d774c84f9b53", "skillmarket:skill:foo@1.0.0", false},
		// Loose substring must NOT be treated as a source key.
		{"loose_enterprise_hub_text_not_key", "enterprise_hub:not-a-skill-key", "1.0.0", false},

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
		{
			name: "enterprise_version_key_matches_case_insensitively",
			dep: maclawAppInstallPlanDependency{
				Source:           "enterprise_hub",
				RequiredVersion:  "enterprise_hub:skill:paper_pdf_translator@D1CB0335A151",
				InstalledVersion: "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151",
			},
			want: "matched",
		},
		{
			// App packages commonly declare human semver (maclaw.app.json appSkill.version)
			// while enterprise-hub installs record content-addressed hub_version keys.
			// Cross-format must not false-block a locally identity-matched skill.
			name: "enterprise_version_key_satisfies_declared_semver",
			dep: maclawAppInstallPlanDependency{
				Source:           "enterprise_hub",
				RequiredVersion:  "1.0.0",
				InstalledVersion: "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151",
			},
			want: "matched",
		},
		{
			// Content pin cannot be proven by a plain installed version alone.
			name: "enterprise_hash_pin_not_satisfied_by_plain_installed",
			dep: maclawAppInstallPlanDependency{
				Source:           "hub",
				RequiredVersion:  "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151",
				InstalledVersion: "1.0.0",
			},
			want: "mismatch",
		},
		{
			name: "different_enterprise_version_keys_mismatch",
			dep: maclawAppInstallPlanDependency{
				Source:           "enterprise_hub",
				RequiredVersion:  "enterprise_hub:skill:paper_pdf_translator@aaaabbbbcccc",
				InstalledVersion: "enterprise_hub:skill:paper_pdf_translator@dddddeeeeeff",
			},
			want: "mismatch",
		},
		{
			name: "skillmarket_source_keys_semver_suffix_compatible",
			dep: maclawAppInstallPlanDependency{
				Source:           "skillmarket",
				RequiredVersion:  "skillmarket:skill:paper_pdf_translator@1.0.0",
				InstalledVersion: "skillmarket:skill:paper_pdf_translator@1.3.0",
			},
			want: "matched",
		},
		{
			name: "skillmarket_key_suffix_too_old_for_plain_required",
			dep: maclawAppInstallPlanDependency{
				Source:           "skillmarket",
				RequiredVersion:  "2.0.0",
				InstalledVersion: "skillmarket:skill:paper_pdf_translator@1.0.0",
			},
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

func TestMaclawAppVersionCoordinateHelpers(t *testing.T) {
	if !maclawAppVersionLooksLikeContentHash("d774c84f9b53") {
		t.Fatal("expected hub digest to look like content hash")
	}
	for _, v := range []string{"1.0.0", "10", "20240101", "v1.2.3"} {
		if maclawAppVersionLooksLikeContentHash(v) {
			t.Fatalf("%q must not look like content hash", v)
		}
	}
	if !maclawAppVersionIsSemverLike("1.2.0") || !maclawAppVersionIsSemverLike("v2") {
		t.Fatal("expected semver-like versions to classify as semver-like")
	}
	if maclawAppVersionIsSemverLike("d774c84f9b53") || maclawAppVersionIsSemverLike("stable") {
		t.Fatal("hash/codename must not be semver-like")
	}
	if !maclawAppLooksLikeSourceVersionKey("enterprise_hub:skill:foo@d774c84f9b53") {
		t.Fatal("parseable enterprise key should look like source version key")
	}
	if maclawAppLooksLikeSourceVersionKey("enterprise_hub:not-a-skill-key") || maclawAppLooksLikeSourceVersionKey("1.0.0") {
		t.Fatal("non-keys must not look like source version keys")
	}
	if maclawAppNormalizeSourceVersionKind("hubcenter") != "skillmarket" || maclawAppNormalizeSourceVersionKind("ENTERPRISE") != "enterprise_hub" {
		t.Fatal("source version kind aliases should normalize")
	}
	if !maclawAppSourceVersionKeysCompatible(
		"skillmarket:skill:foo@1.0.0",
		"skillmarket:skill:foo@1.4.0",
	) {
		t.Fatal("same target with newer semver suffix should be compatible")
	}
	if !maclawAppSourceVersionKeysCompatible(
		"hubcenter:skill:foo@1.0.0",
		"skillmarket:skill:foo@1.1.0",
	) {
		t.Fatal("hubcenter and skillmarket should be treated as the same kind")
	}
	if maclawAppSourceVersionKeysCompatible(
		"enterprise_hub:skill:foo@aaaabbbbcccc",
		"enterprise_hub:skill:foo@dddddeeeeeff",
	) {
		t.Fatal("different content hashes must not be compatible")
	}
	// Asymmetric cross-coordinate policy.
	if !maclawAppCrossCoordinateVersionSatisfied("1.0.0", "enterprise_hub:skill:foo@d774c84f9b53", false) {
		t.Fatal("plain required should accept installed content-hash key")
	}
	if maclawAppCrossCoordinateVersionSatisfied("enterprise_hub:skill:foo@d774c84f9b53", "1.0.0", true) {
		t.Fatal("content-hash required pin must not be satisfied by plain installed")
	}
	if !maclawAppCrossCoordinateVersionSatisfied("enterprise_hub:skill:foo@d774c84f9b53", "d774c84f9b53", true) {
		t.Fatal("content-hash pin should be proven by matching bare digest")
	}
	if maclawAppCrossCoordinateVersionSatisfiedParts("d774c84f9b53", "aaaaaaaaaaaa", false) {
		t.Fatal("mismatched bare digests must not satisfy")
	}
	if !maclawAppDependencyVersionSatisfied(
		"enterprise_hub:skill:foo@d774c84f9b53",
		"d774c84f9b53",
	) {
		t.Fatal("single-parse path should treat full key vs bare digest as matched")
	}
	// Shared revision-token policy (dual-key @suffix and cross-coordinate).
	if !maclawAppRevisionTokensCompatible("1.0.0", "d774c84f9b53") {
		t.Fatal("semver required should accept installed content hash token")
	}
	if maclawAppRevisionTokensCompatible("d774c84f9b53", "1.0.0") {
		t.Fatal("hash pin must not be satisfied by semver token")
	}
	if !maclawAppRevisionTokensCompatible("1.0.0", "1.2.0") || maclawAppRevisionTokensCompatible("2.0.0", "1.0.0") {
		t.Fatal("semver token minimum compare failed")
	}
	if !maclawAppSourceVersionKeysCompatible(
		"skillmarket:skill:foo@1.0.0",
		"skillmarket:skill:foo@d774c84f9b53",
	) {
		t.Fatal("dual-key semver vs hash suffix should be compatible")
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
