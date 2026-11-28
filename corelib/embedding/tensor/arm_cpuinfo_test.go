package tensor

import "testing"

func TestParseARMCPUInfoFeatures(t *testing.T) {
	text := "processor\t: 0\nFeatures\t: fp asimd aes asimddp sha512 i8mm bf16\nCPU implementer\t: 0x51\n"
	dot, i8 := parseARMCPUInfoFeatures(text)
	if !dot || !i8 {
		t.Fatalf("snapdragon-like features: dot=%v i8mm=%v", dot, i8)
	}
	dot, i8 = parseARMCPUInfoFeatures("Features : fp asimd asimddp\n")
	if !dot || i8 {
		t.Fatalf("dotprod-only: dot=%v i8mm=%v", dot, i8)
	}
	dot, i8 = parseARMCPUInfoFeatures("flags : asimddp i8mm\n")
	if dot || i8 {
		t.Fatalf("x86 flags line must not count: dot=%v i8mm=%v", dot, i8)
	}
}
