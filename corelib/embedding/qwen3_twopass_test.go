package embedding

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestParseQwen3TwoPass(t *testing.T) {
	all := qwen3TwoPass{true, true, true, true}
	none := qwen3TwoPass{}
	// The empty value defaults are ISA-aware: single-pass (off) on AVX2,
	// two-pass everywhere on the faster ISAs. Compute the expected default
	// from the same source the parser consults so the test is portable
	// across CI runners.
	wantDefault := all
	if tensor.Q8PISAName() == "avx2" {
		wantDefault = none
	}
	cases := []struct {
		in   string
		want qwen3TwoPass
	}{
		{"", wantDefault}, // default: ISA-aware two-pass choice
		{"   ", wantDefault},
		{"all", all},
		{"none", none},
		{"0", none},
		{"qkv", qwen3TwoPass{qkv: true}},
		{"o,gu", qwen3TwoPass{o: true, gu: true}},
		{"qkv,o,gu,down", all},
		{"none,qkv", qwen3TwoPass{qkv: true}}, // last non-none wins within list; none then qkv → qkv
	}

	for _, c := range cases {
		got := parseQwen3TwoPass(c.in)
		if got != c.want {
			t.Errorf("parseQwen3TwoPass(%q)=%+v want %+v", c.in, got, c.want)
		}
	}
}
