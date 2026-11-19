package petpack

import "testing"

func TestNormalizePackVersion(t *testing.T) {
	if got := NormalizePackVersion("  v1.2.3 "); got != "1.2.3" {
		t.Fatalf("normalize=%q", got)
	}
	if !ValidPackVersion("V1.0.0") || !ValidPackVersion("1") || !ValidPackVersion("1.2.3.4") {
		t.Fatal("numeric versions must be accepted")
	}
	for _, bad := range []string{"", "  ", "beta", "1.0.0-beta", "v", "1.2.3.4.5"} {
		if ValidPackVersion(bad) {
			t.Fatalf("version %q must be rejected", bad)
		}
	}
}

func TestComparePackVersions(t *testing.T) {
	if ComparePackVersions("v1.2.0", "1.2") != 0 || ComparePackVersions("1.0.0", "1") != 0 {
		t.Fatal("equivalent numeric versions must compare equal")
	}
	if ComparePackVersions("1.10.0", "1.9.0") <= 0 || ComparePackVersions("9.0.0", "10.0.0") >= 0 {
		t.Fatal("segments must compare as numbers")
	}
	if ComparePackVersions("1.0.0", "2.0.0") >= 0 {
		t.Fatal("an older version must compare lower")
	}
	if ComparePackVersions("beta", "1.0.0") <= 0 || ComparePackVersions("", "") != 0 {
		t.Fatal("non-numeric inequality is not a downgrade, and empty equals empty")
	}
}
