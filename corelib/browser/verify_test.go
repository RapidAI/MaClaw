package browser

import (
	"strings"
	"testing"
)

func TestReadbackVerdict(t *testing.T) {
	cases := []struct {
		exp  string
		got  []string
		want string
	}{
		{"Hello  world, 你好", []string{"hello world,\u00a0你好 and more"}, ReadbackApplied},
		{"# Title\n**bold** text", []string{"Title\nbold text"}, ReadbackApplied},
		{"long text that the editor dropped", []string{"", "请输入正文"}, ReadbackNotApplied},
		{"x", []string{""}, ReadbackUnknown},
		{"<p>段落<b>加粗</b></p>", []string{"段落 加粗"}, ReadbackApplied},
	}
	for _, c := range cases {
		if got := ReadbackVerdict(c.exp, c.got); got != c.want {
			t.Errorf("%q vs %q: got %s want %s", c.exp, c.got, got, c.want)
		}
	}
}

func TestReadbackJSKeepsTheCaretOfALongField(t *testing.T) {
	js := readbackJS(true, true)
	for _, needle := range []string{
		"s.length<=100000",
		"s.slice(-8000)",
		"selectionStart",
		"getSelection",
		`toLowerCase()==="password"`,
		"fromCharCode(0x241e)",
		"shadowRoot",
		"after.toString()",
		"!document.hasFocus()",
	} {
		if !strings.Contains(js, needle) {
			t.Errorf("script missing %q", needle)
		}
	}
}

func TestScriptWrittenText(t *testing.T) {
	cases := map[string]string{
		`document.querySelector('.DraftEditor-root [contenteditable]').innerText = "这是一段正文内容";`: "这是一段正文内容",
		`el.focus(); document.execCommand('insertText', false, 'pasted body text');`:            "pasted body text",
		"const dt=new DataTransfer();dt.setData('text/html', `<p>富文本</p>`);":                    "<p>富文本</p>",
		`input.value = text;`:                       "",
		`document.title = "not a write"; x.click()`: "",
	}
	for script, want := range cases {
		if got := ScriptWrittenText(script); got != want {
			t.Errorf("%s: got %q want %q", script, got, want)
		}
	}
}
