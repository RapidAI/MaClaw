package main

import "testing"

func TestRichScriptReadIsNotAWrite(t *testing.T) {
	read := `const b=document.querySelector('.RichText'); return {title: document.title, len: b.innerText.length, txt: b.textContent.slice(0,80)}`
	if desktopScriptWritesRichBody(read) {
		t.Fatalf("read-only script refused as rich_text_dom_write")
	}
	for _, w := range []string{
		`document.querySelector('.public-DraftEditor-content').innerText = "x"`,
		`document.querySelector('[contenteditable]').innerHTML+='<p>x</p>'`,
		`el=document.querySelector('.ProseMirror'); el.textContent="y"`,
	} {
		if !desktopScriptWritesRichBody(w) {
			t.Fatalf("write not caught: %s", w)
		}
	}
}
