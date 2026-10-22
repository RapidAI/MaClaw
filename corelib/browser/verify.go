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
// every editable on the page. A short field is read whole. Keystrokes land
// at the caret, so a long focused field also keeps its start, its end, and
// the caret. Password values are left unread. A separator the verdict
// comparison keeps stands between those slices so they do not fuse.
func readbackJS(wide, needFocus bool) string {
	return fmt.Sprintf(`(function(){
if(%t&&!document.hasFocus())return "[]";
const out=[];
const sep=String.fromCharCode(0x241e);
const secret=e=>!!(e&&e.tagName==="INPUT"&&String(e.type||"").toLowerCase()==="password");
const rootOf=e=>{if(!e||secret(e))return null;if(e.tagName==="INPUT"||e.tagName==="TEXTAREA")return e;const r=e.closest&&e.closest("[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]");return r||e;};
const textOf=e=>{const r=rootOf(e);if(!r)return"";if(r.tagName==="INPUT"||r.tagName==="TEXTAREA")return r.value||"";return r.innerText||r.textContent||"";};
const edges=s=>{s=s||"";if(s.length<=20000)return s;return s.slice(0,8000)+sep+s.slice(-8000);};
const clip=(s,el)=>{
  s=s||"";
  if(s.length<=100000)return s;
  const parts=[s.slice(0,8000),s.slice(-8000)];
  if(el&&el.selectionStart!=null){
    const at=el.selectionStart;
    parts.push(s.slice(Math.max(0,at-100000),Math.min(s.length,at+200)));
  }
  try{
    const view=el&&el.ownerDocument&&el.ownerDocument.defaultView;
    const sel=view&&view.getSelection&&view.getSelection();
    if(sel&&sel.rangeCount&&el&&(el===sel.anchorNode||(el.contains&&el.contains(sel.anchorNode)))){
      const range=sel.getRangeAt(0);
      const before=range.cloneRange();
      before.selectNodeContents(el);
      before.setEnd(sel.anchorNode,sel.anchorOffset);
      const pre=before.toString();
      parts.push(pre.slice(Math.max(0,pre.length-100000)));
      const after=range.cloneRange();
      after.selectNodeContents(el);
      after.setStart(sel.anchorNode,sel.anchorOffset);
      parts.push(after.toString().slice(0,8000));
    }
  }catch(err){}
  return parts.filter(Boolean).join(sep);
};
let d=document,el=d.activeElement;
for(let hop=0;hop<6&&el;hop++){
  if(el.tagName==="IFRAME"){try{d=el.contentDocument;el=d&&d.activeElement}catch(e){break}continue}
  if(el.shadowRoot&&el.shadowRoot.activeElement){el=el.shadowRoot.activeElement;continue}
  break;
}
if(el&&el!==d.body&&!secret(el))out.push(clip(textOf(el),rootOf(el)));
if(%t){const all=d.querySelectorAll("input,textarea,[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]");for(let i=0;i<all.length&&i<50;i++){if(!secret(all[i]))out.push(edges(textOf(all[i])));}}
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
let body="";roots.forEach(r=>{const t=(r.innerText||r.textContent||"");if(t.replace(/\s+/g,"").length===max)body=t});
let title="";for(const el of document.querySelectorAll("textarea,input")){const ph=(el.getAttribute("placeholder")||"")+(el.getAttribute("aria-label")||"");if(/标题|title/i.test(ph)){title=el.value||"";break}}
return JSON.stringify({editors:roots.size,max:max,count:count,body:body.slice(0,400),title:title.slice(0,200)})})()`

// RichBody is the measured rich-text body of the attached page.
type RichBody struct {
	Editors int `json:"editors"`
	Max     int `json:"max"`
	Count   int    `json:"count"`
	Body    string `json:"body"`
	Title   string `json:"title"`
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
