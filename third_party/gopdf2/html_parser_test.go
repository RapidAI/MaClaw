package gopdf

import (
	"testing"
)

func TestParseHTML_PlainText(t *testing.T) {
	nodes := parseHTML("hello world")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Type != htmlNodeText {
		t.Fatal("expected text node")
	}
	if nodes[0].Text != "hello world" {
		t.Fatalf("expected 'hello world', got %q", nodes[0].Text)
	}
}

func TestParseHTML_BoldTag(t *testing.T) {
	nodes := parseHTML("<b>bold text</b>")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Type != htmlNodeElement || n.Tag != "b" {
		t.Fatalf("expected <b> element, got %v", n)
	}
	if len(n.Children) != 1 || n.Children[0].Text != "bold text" {
		t.Fatal("expected child text 'bold text'")
	}
}

func TestParseHTML_NestedTags(t *testing.T) {
	nodes := parseHTML("<p>Hello <b>bold <i>italic</i></b> world</p>")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	p := nodes[0]
	if p.Tag != "p" {
		t.Fatalf("expected <p>, got <%s>", p.Tag)
	}
	if len(p.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(p.Children))
	}
}

func TestParseHTML_SelfClosingBr(t *testing.T) {
	nodes := parseHTML("line1<br/>line2")
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if nodes[1].Tag != "br" {
		t.Fatalf("expected <br>, got <%s>", nodes[1].Tag)
	}
}

func TestParseHTML_Attributes(t *testing.T) {
	nodes := parseHTML(`<font color="#ff0000" size="5">red</font>`)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Attrs["color"] != "#ff0000" {
		t.Fatalf("expected color=#ff0000, got %q", n.Attrs["color"])
	}
	if n.Attrs["size"] != "5" {
		t.Fatalf("expected size=5, got %q", n.Attrs["size"])
	}
}

func TestParseHTML_ImgTag(t *testing.T) {
	nodes := parseHTML(`<img src="test.png" width="100" height="50"/>`)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Tag != "img" {
		t.Fatalf("expected <img>, got <%s>", n.Tag)
	}
	if n.Attrs["src"] != "test.png" {
		t.Fatalf("expected src=test.png, got %q", n.Attrs["src"])
	}
}

func TestParseHTML_HTMLEntities(t *testing.T) {
	nodes := parseHTML("&lt;hello&gt; &amp; &quot;world&quot;")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	expected := `<hello> & "world"`
	if nodes[0].Text != expected {
		t.Fatalf("expected %q, got %q", expected, nodes[0].Text)
	}
}

func TestParseHTML_Comment(t *testing.T) {
	nodes := parseHTML("before<!-- comment -->after")
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Text != "before" || nodes[1].Text != "after" {
		t.Fatalf("expected 'before' and 'after', got %q and %q", nodes[0].Text, nodes[1].Text)
	}
}

func TestParseCSSColor_Hex(t *testing.T) {
	r, g, b, ok := parseCSSColor("#ff8000")
	if !ok || r != 255 || g != 128 || b != 0 {
		t.Fatalf("expected (255,128,0), got (%d,%d,%d) ok=%v", r, g, b, ok)
	}
}

func TestParseCSSColor_ShortHex(t *testing.T) {
	r, g, b, ok := parseCSSColor("#f00")
	if !ok || r != 255 || g != 0 || b != 0 {
		t.Fatalf("expected (255,0,0), got (%d,%d,%d) ok=%v", r, g, b, ok)
	}
}

func TestParseCSSColor_Named(t *testing.T) {
	r, g, b, ok := parseCSSColor("blue")
	if !ok || r != 0 || g != 0 || b != 255 {
		t.Fatalf("expected (0,0,255), got (%d,%d,%d) ok=%v", r, g, b, ok)
	}
}

func TestParseCSSColor_RGB(t *testing.T) {
	r, g, b, ok := parseCSSColor("rgb(10, 20, 30)")
	if !ok || r != 10 || g != 20 || b != 30 {
		t.Fatalf("expected (10,20,30), got (%d,%d,%d) ok=%v", r, g, b, ok)
	}
}

func TestParseFontSize(t *testing.T) {
	tests := []struct {
		input    string
		current  float64
		expected float64
		ok       bool
	}{
		{"12pt", 10, 12, true},
		{"16px", 10, 12, true},
		{"2em", 10, 20, true},
		{"150%", 10, 15, true},
		{"large", 10, 14, true},
		{"invalid", 10, 0, false},
	}
	for _, tt := range tests {
		got, ok := parseFontSize(tt.input, tt.current)
		if ok != tt.ok || (ok && got != tt.expected) {
			t.Errorf("parseFontSize(%q, %v) = (%v, %v), want (%v, %v)", tt.input, tt.current, got, ok, tt.expected, tt.ok)
		}
	}
}

func TestCollapseWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello   world  ", "hello world"},
		{"no\nnewlines\there", "no newlines here"},
		{"single", "single"},
		{"   ", ""},
	}
	for _, tt := range tests {
		got := collapseWhitespace(tt.input)
		if got != tt.expected {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseHTML_List(t *testing.T) {
	html := `<ul><li>item 1</li><li>item 2</li></ul>`
	nodes := parseHTML(html)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	ul := nodes[0]
	if ul.Tag != "ul" {
		t.Fatalf("expected <ul>, got <%s>", ul.Tag)
	}
	if len(ul.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(ul.Children))
	}
}

func TestParseHTML_InlineStyle(t *testing.T) {
	nodes := parseHTML(`<span style="color: red; font-size: 18pt">styled</span>`)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Attrs["style"] != "color: red; font-size: 18pt" {
		t.Fatalf("unexpected style attr: %q", n.Attrs["style"])
	}
}

func TestParseHTML_Table(t *testing.T) {
	nodes := parseHTML(`<table><thead><tr><th>Name</th><th>Value</th></tr></thead><tbody><tr><td>Foo</td><td>Bar</td></tr></tbody></table>`)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	table := nodes[0]
	if table.Tag != "table" {
		t.Fatalf("expected <table>, got <%s>", table.Tag)
	}
	if len(table.Children) != 2 {
		t.Fatalf("expected 2 table sections, got %d", len(table.Children))
	}
	if table.Children[0].Tag != "thead" || table.Children[1].Tag != "tbody" {
		t.Fatalf("expected thead/tbody children, got <%s> <%s>", table.Children[0].Tag, table.Children[1].Tag)
	}
	row := table.Children[0].Children[0]
	if row.Tag != "tr" || len(row.Children) != 2 {
		t.Fatalf("expected header row with 2 cells, got <%s> with %d children", row.Tag, len(row.Children))
	}
	if row.Children[0].Tag != "th" || row.Children[1].Tag != "th" {
		t.Fatalf("expected th cells, got <%s> <%s>", row.Children[0].Tag, row.Children[1].Tag)
	}
}

func TestIsBlockElement(t *testing.T) {
	blocks := []string{"p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "hr", "center", "table", "thead", "tbody", "tfoot", "tr"}
	for _, tag := range blocks {
		if !isBlockElement(tag) {
			t.Errorf("expected %q to be block element", tag)
		}
	}
	inlines := []string{"b", "i", "u", "span", "a", "font", "em", "strong", "td", "th"}
	for _, tag := range inlines {
		if isBlockElement(tag) {
			t.Errorf("expected %q to be inline element", tag)
		}
	}
}
