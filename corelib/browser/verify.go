package browser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Verify-after-act. A type or a script that writes text can report success
// while a rich editor (Draft.js, ProseMirror, Lexical) throws the text away:
// it keeps its own model and redraws from it. After such an action the page
// is read back once and compared with what was meant to land. Reading is
// best effort: any error means "unknown", never a failure of the action.

// Readback verdicts.
const (
	ReadbackApplied    = "applied"
	ReadbackNotApplied = "not_applied"
	ReadbackUnknown    = "unknown"
)

// readbackProbeRunes is how much of the expected text must show up. Editors
// reflow long text, so only a leading piece is compared.
const readbackProbeRunes = 24

// readbackNormalize folds text so formatting differences (whitespace, markdown
// marks, nbsp, smart quotes, case) do not count as "not applied".
func readbackNormalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r) || r == '\u00a0' || r == '\u200b':
			continue
		case strings.ContainsRune("#*_`>~|[]()", r):
			continue
		case r == '\u201c' || r == '\u201d':
			r = '"'
		case r == '\u2018' || r == '\u2019':
			r = '\''
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ReadbackProbe is the normalized leading piece of expected text that the
// read-back content must contain, or "" when the text is too short to tell.
func ReadbackProbe(expected string) string {
	n := []rune(readbackNormalize(stripTags(expected)))
	if len(n) < 2 {
		return ""
	}
	if len(n) > readbackProbeRunes {
		n = n[:readbackProbeRunes]
	}
	return string(n)
}

// ReadbackVerdict compares read-back content with the expected text.
func ReadbackVerdict(expected string, contents []string) string {
	probe := ReadbackProbe(expected)
	if probe == "" {
		return ReadbackUnknown
	}
	for _, c := range contents {
		if strings.Contains(readbackNormalize(c), probe) {
			return ReadbackApplied
		}
	}
	return ReadbackNotApplied
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagRe.ReplaceAllString(s, " ") }

// scriptWriteRe finds string literals a page script writes into the page:
// el.value = "...", innerText/textContent/innerHTML = "...",
// execCommand("insertText"/"insertHTML", false, "..."), and
// setData("text/plain"|"text/html", "...") for synthetic paste events.
var scriptWriteRe = []*regexp.Regexp{
	regexp.MustCompile("(?s)(?:\\.value|innerText|textContent|innerHTML)\\s*\\+?=\\s*(\"(?:[^\"\\\\]|\\\\.){2,}\"|'(?:[^'\\\\]|\\\\.){2,}'|`[^`$]{2,}`)"),
	regexp.MustCompile("(?s)execCommand\\(\\s*['\"]insert(?:Text|HTML)['\"]\\s*,\\s*\\w+\\s*,\\s*(\"(?:[^\"\\\\]|\\\\.){2,}\"|'(?:[^'\\\\]|\\\\.){2,}'|`[^`$]{2,}`)"),
	regexp.MustCompile("(?s)setData\\(\\s*['\"]text/(?:plain|html)['\"]\\s*,\\s*(\"(?:[^\"\\\\]|\\\\.){2,}\"|'(?:[^'\\\\]|\\\\.){2,}'|`[^`$]{2,}`)"),
}

// ScriptWrittenText returns the longest literal text a script writes into
// the page, or "" when it writes none it can see (computed values are not
// guessed).
func ScriptWrittenText(script string) string {
	best := ""
	for _, re := range scriptWriteRe {
		for _, m := range re.FindAllStringSubmatch(script, -1) {
			lit := unquoteJSLiteral(m[1])
			if len([]rune(lit)) > len([]rune(best)) {
				best = lit
			}
		}
	}
	if ReadbackProbe(best) == "" {
		return ""
	}
	return best
}

func unquoteJSLiteral(lit string) string {
	if len(lit) < 2 {
		return ""
	}
	body := lit[1 : len(lit)-1]
	if lit[0] == '"' {
		var out string
		if json.Unmarshal([]byte(lit), &out) == nil {
			return out
		}
	}
	r := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\'`, "'", `\"`, `"`, `\\`, `\`)
	return r.Replace(body)
}

// readbackJS collects the text of the focused control (its editor root for a
// contenteditable, descending into same-origin iframes) and, when wide, of
// every editable on the page. Capped so a big page stays cheap.
func readbackJS(wide, needFocus bool) string {
	return fmt.Sprintf(`(function(){
if(%t&&!document.hasFocus())return "[]";
const out=[];const cap=s=>(s||"").slice(0,20000);
let d=document,el=d.activeElement;
for(let i=0;i<4&&el&&el.tagName==="IFRAME";i++){try{d=el.contentDocument;el=d.activeElement}catch(e){break}}
const textOf=e=>{if(!e)return"";if(e.tagName==="INPUT"||e.tagName==="TEXTAREA")return e.value||"";const r=e.closest&&e.closest("[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]");return (r||e).innerText||(r||e).textContent||""};
if(el&&el!==d.body)out.push(cap(textOf(el)));
if(%t){const all=d.querySelectorAll("input,textarea,[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]");for(let i=0;i<all.length&&i<50;i++)out.push(cap(textOf(all[i])));}
return JSON.stringify(out)})()`, needFocus, wide)
}

// ReadBack reads the page once and returns the verdict for expected. wide
// also reads every editable on the page (for a script that may not focus).
func (s *BrowserAgentSession) ReadBack(expected string, wide bool) string {
	return s.readBack(expected, wide, false)
}

// ReadBackIfFocused reads back only when the page has the keyboard focus
// (desktop-level typing may have gone to another window).
func (s *BrowserAgentSession) ReadBackIfFocused(expected string) string {
	return s.readBack(expected, false, true)
}

func (s *BrowserAgentSession) readBack(expected string, wide, needFocus bool) string {
	if s == nil || s.session == nil || ReadbackProbe(expected) == "" {
		return ReadbackUnknown
	}
	raw, err := s.session.Eval(readbackJS(wide, needFocus))
	if err != nil {
		return ReadbackUnknown
	}
	var contents []string
	if json.Unmarshal([]byte(raw), &contents) != nil {
		var inner string
		if json.Unmarshal([]byte(raw), &inner) != nil || json.Unmarshal([]byte(inner), &contents) != nil {
			return ReadbackUnknown
		}
	}
	if len(contents) == 0 {
		return ReadbackUnknown
	}
	return ReadbackVerdict(expected, contents)
}

// NotAppliedAdvice is the model-facing explanation for a not_applied verdict.
const NotAppliedAdvice = "未生效 (not applied): the text was sent but reading the field back does not show it — this editor keeps its own state and ignored a scripted/DOM write. Do not repeat the same method. Use real input instead: click into the editor, then app_run with a real Ctrl+V after clipboard_set (clipboard + real paste), or real keystrokes via app_run type (xdotool). Then read back with probe or a screenshot."

// PageHost is the attached page's host name, or "".
func (s *BrowserAgentSession) PageHost() string {
	raw, err := s.pageURL()
	if err != nil || raw == "" {
		return ""
	}
	return HostOfURL(raw)
}

// HostOfURL returns the lower-case host of a URL without "www.", or "".
func HostOfURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	} else {
		return ""
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.LastIndex(raw, ":"); i >= 0 && !strings.Contains(raw[i:], "]") {
		raw = raw[:i]
	}
	return strings.TrimPrefix(strings.ToLower(raw), "www.")
}

// richBodyJS measures the main rich-text body on the page: the longest
// contenteditable root's text, and a "字数: N" / "N 字" counter when the
// editor shows one (Zhihu). Title inputs are not contenteditable here.
const richBodyJS = `(function(){
const roots=new Set();
document.querySelectorAll("[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]").forEach(e=>{let r=e;while(r.parentElement&&r.parentElement.closest("[contenteditable=''],[contenteditable=true]"))r=r.parentElement.closest("[contenteditable=''],[contenteditable=true]");roots.add(r)});
let max=0;roots.forEach(r=>{const n=(r.innerText||r.textContent||"").replace(/\s+/g,"").length;if(n>max)max=n});
let count=-1;const m=(document.body&&document.body.innerText||"").match(/字数[:：]\s*(\d+)/);if(m)count=parseInt(m[1],10);
return JSON.stringify({editors:roots.size,max:max,count:count})})()`

// RichBody is the measured rich-text body of the attached page.
type RichBody struct {
	Editors int `json:"editors"`
	Max     int `json:"max"`
	Count   int `json:"count"`
}

// Len is the best body length: the editor's own counter when shown, else
// the longest editable text.
func (b RichBody) Len() int {
	if b.Count >= 0 {
		return b.Count
	}
	return b.Max
}

// RichBodyLen reads the rich-text body length. ok is false when the page
// has no editable body or cannot be read.
func (s *BrowserAgentSession) RichBodyLen() (RichBody, bool) {
	var b RichBody
	if s == nil || s.session == nil {
		return b, false
	}
	raw, err := s.session.Eval(richBodyJS)
	if err != nil || json.Unmarshal([]byte(raw), &b) != nil {
		return b, false
	}
	return b, b.Editors > 0
}
