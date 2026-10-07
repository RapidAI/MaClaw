package intent

import "testing"

// TestApplyProductTauSOOFRule: heads tagged calibration.source "oof_rule"
// keep their own τ_s; untagged heads still get DefaultLocalTauS; env wins.
func TestApplyProductTauSOOFRule(t *testing.T) {
	t.Setenv(LocalTauSEnv, "")
	h, err := ParseLocalHead([]byte(`{"labels":["a","b"],"w2":[[1],[0]],"b2":[0,0],"tau":0.8,"tau_s":0.98,"calibration":{"source":"oof_rule"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := ApplyProductTauS(h); got != 0.98 {
		t.Fatalf("oof_rule head: got %v want 0.98", got)
	}
	h = &LocalHead{Tau: 0.8, TauS: 0.99}
	if got := ApplyProductTauS(h); got != DefaultLocalTauS {
		t.Fatalf("untagged head: got %v want %v", got, DefaultLocalTauS)
	}
	t.Setenv(LocalTauSEnv, "0.96")
	h = &LocalHead{Tau: 0.8, TauS: 0.98, CalibrationSource: CalibrationSourceOOFRule}
	if got := ApplyProductTauS(h); got != 0.96 {
		t.Fatalf("env override: got %v want 0.96", got)
	}
}
