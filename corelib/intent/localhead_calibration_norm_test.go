package intent

import "testing"

func TestNormalizeLegacyCalibrationCompute(t *testing.T) {
	if got := normalizeCalibrationCompute("box amd64 TestZZVecDump vectors"); got != "q8p:avx512:2pass" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeCalibrationCompute(" q8p:avx2:1pass "); got != "q8p:avx2:1pass" {
		t.Fatalf("got %q", got)
	}
}
