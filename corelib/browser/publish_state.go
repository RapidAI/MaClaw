package browser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Editor readiness and the post-submit outcome are states, computed from
// signals the page and the network actually expose. They are not a label
// match and not a timer.

// editorReadyFacts is one observation of the editor.
type editorReadyFacts struct {
	Expected      int  // pictures the body must contain
	Remote        int  // pictures whose src is an http(s) URL
	Pending       int  // pictures present but not a remote URL (data:, blob:, empty)
	UploadsOpen   int  // upload requests still in flight
	UploadsFailed int  // upload requests that already failed
	UploadsSeen   int  // upload requests observed at all (open, ok, or failed)
	ProgressBusy  bool // a visible aria-busy node or an unfinished progressbar
}

// editorReady reports whether those facts are the state "ready to submit".
// The reason names the fact that is not there yet.
func editorReady(f editorReadyFacts) (bool, string) {
	switch {
	case f.UploadsFailed > 0:
		return false, fmt.Sprintf("%d upload request(s) failed", f.UploadsFailed)
	case f.UploadsOpen > 0:
		return false, fmt.Sprintf("%d upload request(s) still in flight", f.UploadsOpen)
	case f.ProgressBusy:
		return false, "an upload progress indicator is still active"
	case f.Expected > 0 && f.Remote+f.Pending < f.Expected:
		return false, fmt.Sprintf("%d of %d picture(s) are in the editor", f.Remote+f.Pending, f.Expected)
	case f.UploadsSeen > 0 && f.Expected > 0 && f.Remote < f.Expected:
		// An upload ran and the element still has no remote URL: the
		// site has not stored the picture. A local preview with no
		// upload at all is the picture the submit will send.
		return false, fmt.Sprintf("%d of %d picture(s) have a remote url", f.Remote, f.Expected)
	case f.UploadsSeen > 0 && f.Pending > 0:
		return false, fmt.Sprintf("%d picture(s) are still a local preview", f.Pending)
	default:
		return true, ""
	}
}

// A submit has one of three outcomes. A second click is allowed only for
// still-in-editor, and only after the platform's own list has been read
// and does not contain the post.
const (
	submitPublished = "published"
	submitInEditor  = "still-in-editor"
	submitUnknown   = "unknown"
	submitPending   = "pending" // writes still in flight; the outcome is not decided
)

// submitFacts is what can be observed after one click without guessing.
type submitFacts struct {
	WritesOpen   int  // requests sent by the click that have not finished
	NetPublished bool // the site's own response carried a post id or link
	SuccessPage  bool // the page URL is the site's success page
	Editor       bool // the editor element is still on the page
	EditorKnown  bool
	// Listed is meaningful only when ListKnown: the platform's post or
	// draft list was read.
	ListKnown bool
	Listed    bool
}

// submitOutcome classifies the facts. pending means look again; the other
// three are final for this click.
func submitOutcome(f submitFacts) string {
	if f.WritesOpen > 0 {
		return submitPending
	}
	if f.NetPublished || f.SuccessPage || (f.ListKnown && f.Listed) {
		return submitPublished
	}
	if f.EditorKnown && f.Editor && f.ListKnown && !f.Listed {
		return submitInEditor
	}
	return submitUnknown
}

// imageSrcRemote reports whether src is a stored remote picture. A data
// or blob URL is the browser's local preview, not the site's copy.
func imageSrcRemote(src string) bool {
	src = trimProto(src)
	return len(src) > 8 && (hasPrefixFold(src, "https://") || hasPrefixFold(src, "http://"))
}

func trimProto(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// waitEditorReady returns when the editor is ready to submit, or when the
// step budget ends with the editor still not ready. It does not click.
func (p *publishRun) waitEditorReady() error {
	deadline := time.Now().Add(p.spec.StepBudget)
	var why string
	for {
		facts := p.editorFacts()
		ready, reason := editorReady(facts)
		if ready {
			return nil
		}
		why = reason
		// A failed upload will not become ready by waiting.
		if facts.UploadsFailed > 0 || time.Now().After(deadline) {
			return fmt.Errorf("editor is not ready to submit: %s; nothing was submitted", why)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (p *publishRun) editorFacts() editorReadyFacts {
	f := editorReadyFacts{Expected: p.wantImages()}
	if p.net != nil {
		started, _, failed, open := p.net.uploadStats(time.Now().Add(-publishUploadLookback))
		f.UploadsOpen, f.UploadsFailed, f.UploadsSeen = open, failed, started
	}
	f.ProgressBusy = p.progressBusy()
	// Markdown stores a picture as a link in the text, which read-back
	// already checked. There is no image element to resolve.
	if p.bodyKind() == "markdown" {
		f.Expected = 0
		return f
	}
	if f.Expected == 0 {
		return f
	}
	obj, err := p.element(p.bodyExpr())
	if err != nil || obj == "" {
		return f
	}
	r, err := p.readStructure(obj)
	if err != nil {
		return f
	}
	f.Pending = r.Pending
	f.Remote = r.Imgs - r.Pending
	if f.Remote < 0 {
		f.Remote = 0
	}
	// A picture can be in the editor before it has a size or a URL (the
	// structure walk only classifies one that is remote or large). The
	// element count is the inserted count.
	if n := p.imgCount(obj); n > r.Imgs {
		f.Pending += n - r.Imgs
	}
	return f
}

func (p *publishRun) readStructure(obj string) (structureResult, error) {
	raw, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishStructureFn, "arguments": []map[string]interface{}{{"value": p.structureSpec()}}, "returnByValue": true})
	if err != nil {
		return structureResult{}, err
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &out)
	var r structureResult
	_ = json.Unmarshal([]byte(out.Result.Value), &r)
	return r, nil
}

// observeSubmit reads the outcome of one click. The list is read only when
// no request from the click is still in flight and the site's own response
// did not already name the post.
func (p *publishRun) observeSubmit(since time.Time, readList bool) submitFacts {
	f := submitFacts{}
	if p.net != nil {
		n, _ := p.net.state()
		f.WritesOpen = n
		id, link := p.net.evidenceSince(since)
		f.NetPublished = id != "" || link != ""
	}
	if u := p.pageURL(); u != "" && p.spec.SuccessURL != "" && !p.seen[u] {
		if re, err := regexp.Compile(p.spec.SuccessURL); err == nil && re.MatchString(u) {
			f.SuccessPage = true
		}
	}
	if ed, err := p.hasEditor(2 * time.Second); err == nil {
		f.EditorKnown, f.Editor = true, ed
	}
	if readList && f.WritesOpen == 0 && !f.NetPublished && !f.SuccessPage {
		found, known := p.listingHas(clipRunes(firstNonEmpty(p.spec.Title, p.spec.Body), 12))
		f.ListKnown, f.Listed = known, found
	}
	return f
}

// listingHas reads the platform's own post or draft list once, in the page
// session, without leaving the editor. known is false when this site has
// no list or the read failed.
func (p *publishRun) listingHas(key string) (found, known bool) {
	key = normText(key)
	if key == "" {
		return false, false
	}
	if p.spec.VerifyAPI != "" {
		out, err := p.evalWithin(publishAPIFindJS+"("+jsq(p.spec.VerifyAPI)+", "+jsq(key)+")", 8*time.Second)
		var o struct {
			Found bool   `json:"found"`
			Err   string `json:"err"`
		}
		if err != nil || json.Unmarshal([]byte(out), &o) != nil || o.Err != "" {
			return false, false
		}
		return o.Found, true
	}
	if p.spec.VerifyURL == "" {
		return false, false
	}
	out, err := p.evalWithin(publishListTextJS+"("+jsq(p.spec.VerifyURL)+", "+jsq(key)+")", 8*time.Second)
	var o struct {
		Found bool   `json:"found"`
		Err   string `json:"err"`
	}
	if err != nil || json.Unmarshal([]byte(out), &o) != nil || o.Err != "" {
		return false, false
	}
	return o.Found, true
}

const publishListTextJS = `(async (url, key) => {
  try {
    const r = await fetch(url, {credentials: 'include'});
    const t = await r.text();
    const n = s => String(s || '').replace(/\s+/g, '');
    return JSON.stringify({found: n(t).includes(n(key))});
  } catch (e) { return JSON.stringify({err: String(e).slice(0, 120)}); }
})`

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}
