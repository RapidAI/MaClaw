package gopdf

import (
	"strings"
	"testing"
)

// TestDecodeGlyphName covers /Differences glyph-name resolution.
func TestDecodeGlyphName(t *testing.T) {
	cases := []struct {
		name string
		want rune
		ok   bool
	}{
		{"A", 'A', true},
		{"a", 'a', true},
		{"fi", 0xFB01, true},
		{"Euro", 0x20AC, true},
		{"alpha", 0x03B1, true},
		{"quoteright", 0x2019, true},
		{"uni0041", 'A', true},
		{"u0041", 'A', true},
		{"g12", 0, false},
		{"cid45", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := decodeGlyphName(c.name)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("decodeGlyphName(%q) = %v(%v), want %v(%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestParseDifferences covers the /Differences array parser.
func TestParseDifferences(t *testing.T) {
	dict := `<< /Type /Font /Differences [24 /breve 26 /caron 65 /A /B /C] >>`
	d := parseDifferences(dict)
	if d[24] != "breve" || d[26] != "caron" {
		t.Errorf("explicit codes wrong: %v", d)
	}
	if d[65] != "A" || d[66] != "B" || d[67] != "C" {
		t.Errorf("sequential codes wrong: %v", d)
	}
	if len(parseDifferences("<< /Type /Font >>")) != 0 {
		t.Errorf("expected empty map for dict without Differences")
	}
}

// TestDecodeBaseEncodingBytes covers WinAnsi/MacRoman/PDFDoc byte tables.
func TestDecodeBaseEncodingBytes(t *testing.T) {
	if r, _ := decodeBaseEncodingByte(0x80, "WinAnsiEncoding"); r != 0x20AC {
		t.Errorf("WinAnsi 0x80 = %U, want Euro", r)
	}
	if r, _ := decodeBaseEncodingByte(0x93, "WinAnsiEncoding"); r != 0x201C {
		t.Errorf("WinAnsi 0x93 = %U, want left double quote", r)
	}
	// PDF spec MacRomanEncoding (not Mac OS Roman): 0x8C is a-ring.
	if r, _ := decodeBaseEncodingByte(0x8C, "MacRomanEncoding"); r != 0x00E5 {
		t.Errorf("MacRoman 0x8C = %U, want a-ring", r)
	}
	if r, _ := decodeBaseEncodingByte(0xD0, "MacRomanEncoding"); r != 0x2013 {
		t.Errorf("MacRoman 0xD0 = %U, want en dash", r)
	}
	if r, _ := decodeBaseEncodingByte(0xE0, "MacRomanEncoding"); r != 0x2021 {
		t.Errorf("MacRoman 0xE0 = %U, want double dagger", r)
	}
	if r, _ := decodeBaseEncodingByte(0x41, "StandardEncoding"); r != 'A' {
		t.Errorf("Standard 0x41 = %U, want A", r)
	}
}

// TestFastParseFloat covers the allocation-free number parser.
func TestFastParseFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"12", 12, true},
		{"-3.5", -3.5, true},
		{"+2.25", 2.25, true},
		{"0", 0, true},
		{"007", 7, true},
		{".5", 0.5, true},
		{"5.", 5, true},
		{"1e3", 1000, true}, // exponent falls back to strconv
		{"", 0, false},
		{"-", 0, false},
		{"abc", 0, false},
		{"--3", 0, false},
	}
	for _, c := range cases {
		got, ok := fastParseFloat([]byte(c.in))
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("fastParseFloat(%q) = %v(%v), want %v(%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestScanObjectHeaders covers the byte-level "N 0 obj" scanner, including
// the multi-digit-generation false-match guard.
func TestScanObjectHeaders(t *testing.T) {
	data := []byte("1 0 obj\nfirst\nendobj\n12 0 obj\nsecond\nendobj\nx 5 10 obj binary\n>>\n3 0 obj")
	hs := scanObjectHeaders(data)
	if len(hs) != 3 {
		t.Fatalf("expected 3 headers (1, 12, 3) — '5 10 obj' must not match — got %d: %v", len(hs), hs)
	}
	wantNums := map[int]bool{1: true, 12: true, 3: true}
	for _, h := range hs {
		if !wantNums[h.num] {
			t.Errorf("unexpected object number %d", h.num)
		}
	}
}

// TestStreamFilterNames covers /Filter single-name and array forms.
func TestStreamFilterNames(t *testing.T) {
	cases := []struct {
		dict  string
		want  int
		first string
	}{
		{`<< /Filter /FlateDecode /Length 5 >>`, 1, "FlateDecode"},
		{`<< /Filter/FlateDecode/Length 5 >>`, 1, "FlateDecode"},
		{`<< /Filter [ /ASCII85Decode /FlateDecode ] >>`, 2, "ASCII85Decode"},
		{`<< /Length 5 >>`, 0, ""},
		{`<< /Filter [ /FlateDecode ] >>`, 1, "FlateDecode"},
	}
	for _, c := range cases {
		got := streamFilterNames(c.dict)
		if len(got) != c.want {
			t.Errorf("streamFilterNames(%q) = %v, want %d names", c.dict, got, c.want)
			continue
		}
		if c.want > 0 && got[0] != c.first {
			t.Errorf("streamFilterNames(%q)[0] = %q, want %q", c.dict, got[0], c.first)
		}
	}
}

// TestTokenizeBytes covers tokenizer edge cases including the stray-delimiter
// fix (must not hang or drop progress).
func TestTokenizeBytes(t *testing.T) {
	toks := tokenizeBytes([]byte(`BT (hi) Tj ET ) > [ (x) ] << >>`))
	joined := ""
	for _, b := range toks {
		joined += string(b) + "|"
	}
	// stray ')' and '>' are skipped, everything else tokenized
	for _, want := range []string{"BT|", "(hi)|", "Tj|", "ET|", "[|", "(x)|", "]|", "<<|", ">>|"} {
		if !contains(joined, want) {
			t.Errorf("token stream missing %q: %s", want, joined)
		}
	}
	if contains(joined, ")|") && !contains(joined, "(x)|") {
		t.Errorf("stray delimiter handling broke later tokens: %s", joined)
	}
}

// TestGroupIntoTextLinesEmpty covers empty-input safety.
func TestGroupIntoTextLinesEmpty(t *testing.T) {
	if lines := GroupIntoTextLines(nil); len(lines) != 0 {
		t.Errorf("expected no lines for nil input, got %d", len(lines))
	}
	if words := MergeTextRuns(nil); len(words) != 0 {
		t.Errorf("expected no words for nil input, got %d", len(words))
	}
}

// TestParseCMapSurrogates covers ligature and surrogate CMap destinations.
func TestParseCMapSurrogates(t *testing.T) {
	cmap := ` /CIDInit /ProcSet findresource begin
	1 beginbfchar
	<0001> <00660069>
	<0002> <D83DDE00>
	endbfchar
	1 beginbfrange
	<0010> <0012> <0041>
	endbfrange
	end `
	m := parseCMap([]byte(cmap))
	if m[0x0001] != "fi" {
		t.Errorf("ligature destination = %q, want fi", m[0x0001])
	}
	if m[0x0002] != "😀" {
		t.Errorf("surrogate destination = %q, want emoji", m[0x0002])
	}
	if m[0x0010] != "A" || m[0x0011] != "B" || m[0x0012] != "C" {
		t.Errorf("range destinations wrong: %v %v %v", m[0x0010], m[0x0011], m[0x0012])
	}
}

// TestParseCSSColorRGBA covers rgb/rgba parsing.
func TestParseCSSColorRGBA(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b uint8
		ok      bool
	}{
		{"rgb(1, 2, 3)", 1, 2, 3, true},
		{"rgba(10, 20, 30, 0.5)", 10, 20, 30, true},
		{"#FF8000", 255, 128, 0, true},
		{"#F80", 255, 136, 0, true},
		{"red", 255, 0, 0, true},
		{"rgb(1, 2)", 0, 0, 0, false},
		{"rgb(a, b, c)", 0, 0, 0, false},
	}
	for _, c := range cases {
		r, g, b, ok := parseCSSColor(c.in)
		if ok != c.ok || (ok && (r != c.r || g != c.g || b != c.b)) {
			t.Errorf("parseCSSColor(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)",
				c.in, r, g, b, ok, c.r, c.g, c.b, c.ok)
		}
	}
}

// TestParseHTMLRawScriptStyle verifies <script>/<style> content is treated
// as raw text (markup inside must not create elements).
func TestParseHTMLRawScriptStyle(t *testing.T) {
	nodes := parseHTML(`<p>a</p><script>if (a<b && c>d) { run("<div>"); }</script><p>after</p>`)
	var tags []string
	var walk func([]*htmlNode)
	walk = func(ns []*htmlNode) {
		for _, n := range ns {
			if n.Type == htmlNodeElement {
				tags = append(tags, n.Tag)
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	for _, tag := range tags {
		if tag == "b" || tag == "div" {
			t.Errorf("markup inside <script> leaked as element: %v", tags)
		}
	}
	// the style rule with a '<' survives as raw CSS text
	nodes = parseHTML(`<style>p:before { content: "<"; color: red; }</style><p>x</p>`)
	if len(nodes) < 2 {
		t.Fatalf("style + p expected")
	}
	if nodes[0].Tag != "style" || len(nodes[0].Children) != 1 || nodes[0].Children[0].Type != htmlNodeText {
		t.Errorf("style content should be a single raw text node")
	}
	if !strings.Contains(nodes[0].Children[0].Text, `content: "<"`) {
		t.Errorf("style raw text truncated: %q", nodes[0].Children[0].Text)
	}
	// unterminated script consumes the rest without hanging
	nodes = parseHTML(`<p>x</p><script>var s = 1;`)
	if len(nodes) != 2 {
		t.Errorf("unterminated script: %d nodes", len(nodes))
	}
}

// TestParseHTMLEntitiesInAttrs verifies entity decoding in attributes.
func TestParseHTMLEntitiesInAttrs(t *testing.T) {
	nodes := parseHTML(`<a href="x?a=1&amp;b=2" title="&#39;q&#39;">l</a>`)
	if nodes[0].Attrs["href"] != "x?a=1&b=2" {
		t.Errorf("href entity decode: %q", nodes[0].Attrs["href"])
	}
	if nodes[0].Attrs["title"] != "'q'" {
		t.Errorf("title entity decode: %q", nodes[0].Attrs["title"])
	}
}

// TestParseSimpleSelectorStar verifies *.class combines with classes.
func TestParseSimpleSelectorStar(t *testing.T) {
	cs, ok := parseSimpleSelector("*.hl")
	if !ok || cs.tag != "" || cs.class != "hl" {
		t.Errorf("parseSimpleSelector(*.hl) = %+v (%v), want class=hl no tag", cs, ok)
	}
	// and it matches elements via class only
	node := parseHTML(`<span class="hl">x</span>`)[0]
	rules := parseStylesheet(`*.hl { color: red; } p { color: blue; }`)
	res := &htmlStyleResolver{tagRules: nil}
	for _, r := range rules {
		switch r.selector.specificity() {
		case 2:
			res.classRules = append(res.classRules, r)
		default:
			res.tagRules = append(res.tagRules, r)
		}
	}
	props := res.resolve(node)
	if props["color"] != "red" {
		t.Errorf("*.hl did not apply: %v", props)
	}
}
