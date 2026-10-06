package embedding

import "testing"

func TestParseQwen3TwoPass(t *testing.T) {
	all := qwen3TwoPass{true, true, true, true}
	none := qwen3TwoPass{}
	cases := []struct {
		in   string
		want qwen3TwoPass
	}{
		{"", all},          // default: two-pass everywhere
		{"   ", all},
		{"all", all},
		{"none", none},
		{"0", none},
		{"qkv", qwen3TwoPass{qkv: true}},
		{"o,gu", qwen3TwoPass{o: true, gu: true}},
		{"qkv,o,gu,down", all},
		{"none,qkv", qwen3TwoPass{qkv: true}}, // last non-none wins within list; none then qkv → qkv
	}
	// "none,qkv": our parser processes left-to-right; none clears, then qkv sets.
	// Actually looking at the implementation: case "none","0" sets t = empty, then "qkv" sets qkv.
	// So want is {qkv: true}. Good.

	for _, c := range cases {
		got := parseQwen3TwoPass(c.in)
		if got != c.want {
			t.Errorf("parseQwen3TwoPass(%q)=%+v want %+v", c.in, got, c.want)
		}
	}
}
