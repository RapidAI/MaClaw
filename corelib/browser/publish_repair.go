package browser

import (
	"fmt"
	"strings"
	"time"
)

var defaultUpdateTexts = []string{"更新", "发布", "Update", "Publish"}

// publishCaretAfterFn puts the caret at the end of the block that holds the
// given text (the text a picture belongs after); "" when not found.
const publishCaretAfterFn = `function(tail) { const n = s => (s || '').replace(/\s+/g, ''); const want = n(tail); let best = null; for (const e of this.querySelectorAll('p,h1,h2,h3,h4,h5,h6,li,blockquote,div')) { if (n(e.innerText).includes(want) && (!best || n(e.innerText).length < n(best.innerText).length)) best = e; } if (!best) return ''; this.focus(); const r = document.createRange(); r.selectNodeContents(best); r.collapse(false); const s = getSelection(); s.removeAllRanges(); s.addRange(r); return 'ok'; }`

// repair puts pictures missing from the live post back: it opens the post's
// edit page, pastes each missing picture after its anchor text, waits until
// the site stored them and saved, submits the update and re-reads the post.
func (p *publishRun) repair(missing []int) (string, error) {
	post := strings.SplitN(p.rep.ResultURL, "?", 2)[0]
	edit := strings.ReplaceAll(p.spec.EditURL, "{url}", strings.TrimRight(post, "/"))
	if err := p.navigate(edit); err != nil {
		return "", fmt.Errorf("edit page: %v", err)
	}
	p.editURL = edit
	var obj string
	end := time.Now().Add(minDur(p.spec.StepBudget, 20*time.Second))
	for time.Now().Before(end) {
		obj, _ = p.element(p.bodyExpr())
		if obj != "" {
			if got, _ := p.call(obj, readValueFn); len([]rune(normText(got))) > 10 {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if obj == "" {
		return "", fmt.Errorf("edit page has no body editor")
	}
	if _, err := p.loadPicker(); err != nil {
		return "", err
	}
	for _, i := range missing {
		if i >= len(p.doc.anchors) {
			continue
		}
		lines := strings.Split(strings.TrimSpace(p.doc.texts[i]), "\n")
		tail := []rune(strings.Join(strings.Fields(lines[len(lines)-1]), ""))
		if len(tail) > 12 {
			tail = tail[len(tail)-12:]
		}
		v, _ := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishCaretAfterFn, "arguments": []map[string]interface{}{{"value": string(tail)}}, "returnByValue": true})
		if !strings.Contains(string(v), "ok") {
			return "", fmt.Errorf("text before picture %d not found in the editor", i+1)
		}
		if err := p.pasteImageHere(obj, p.doc.anchors[i]); err != nil {
			return "", err
		}
	}
	p.lastEdit = time.Now()
	if _, err := p.checkStructure(obj, "editor"); err != nil {
		return "", err
	}
	if err := p.waitSaved(); err != nil {
		return "", err
	}
	texts := p.spec.UpdateTexts
	if len(texts) == 0 {
		texts = defaultUpdateTexts
	}
	saved := p.spec.SubmitTexts
	p.spec.SubmitTexts = texts
	_, err := p.submit()
	p.spec.SubmitTexts = saved
	if err != nil {
		return "", fmt.Errorf("update: %v", err)
	}
	if err := p.navigate(post); err != nil {
		return "", err
	}
	sum, err := p.checkPublished()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("repaired %d missing picture(s) through the edit page; %s", len(missing), sum), nil
}
