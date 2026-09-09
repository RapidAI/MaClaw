package structureddata

import "testing"

func TestSanitizeCSVCell(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"=cmd|' /C calc'!A0", "'=cmd|' /C calc'!A0"},
		{"+1+2", "'+1+2"},
		{"-2+3", "'-2+3"},
		{"@SUM(A1:A9)", "'@SUM(A1:A9)"},
		{"\tignored", "'\tignored"},
		{"\rignored", "'\rignored"},
		{"normal text", "normal text"},
		{"", ""},
		{"hello=world", "hello=world"},
	}
	for _, c := range cases {
		if got := sanitizeCSVCell(c.in); got != c.want {
			t.Errorf("sanitizeCSVCell(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCSVCellFormulaInjection(t *testing.T) {
	if got := csvCell("=HYPERLINK(\"http://evil\",\"x\")"); got != "'=HYPERLINK(\"http://evil\",\"x\")" {
		t.Fatalf("csvCell did not neutralize formula: %q", got)
	}
	if got := csvCell("safe"); got != "safe" {
		t.Fatalf("csvCell mangled safe value: %q", got)
	}
}
