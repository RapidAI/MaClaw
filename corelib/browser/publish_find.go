package browser

import (
	"fmt"
	"strings"
	"time"
)

// FindPosted looks for a post with the spec's title (or body opening) in
// the account's own post list (verify_api, then verify_url). It writes
// nothing and clicks nothing: it is the check a run makes before it could
// publish a second time, for example after the person had the desktop while
// a filled editor waited for its publish click. Found: State is PublishLive
// with the link or id the list shows. Not found: State is PublishNotSubmitted.
func FindPosted(d PublishDriver, spec PublishSpec) (*PublishReport, error) {
	if spec.StepBudget <= 0 {
		spec.StepBudget = 20 * time.Second
	}
	p := &publishRun{d: d, spec: spec, rep: &PublishReport{State: PublishNotSubmitted}}
	key := clipRunes(strings.TrimSpace(spec.Title), 12)
	if key == "" {
		key = clipRunes(strings.TrimSpace(spec.Body), 12)
	}
	if key == "" {
		return p.rep, fmt.Errorf("find posted needs a title or body")
	}
	if spec.VerifyAPI != "" {
		if _, err := p.checkAPI(key); err == nil {
			p.rep.State, p.rep.Evidence = PublishLive, "list_api_precheck"
			return p.rep, nil
		}
	}
	if spec.VerifyURL == "" {
		return p.rep, fmt.Errorf("no post list (verify_url / verify_api) is known for this site")
	}
	if err := p.navigate(spec.VerifyURL); err != nil {
		return p.rep, err
	}
	if _, err := p.checkResult(key, minDur(spec.StepBudget, 15*time.Second), true); err != nil {
		p.rep.ResultURL = ""
		return p.rep, nil
	}
	p.rep.State, p.rep.Evidence = PublishLive, "list_precheck"
	return p.rep, nil
}

// listTitleHit reports that a list row's visible text is this title. The
// list is located by the title's first 12 characters, so a different post
// that only shares that opening is not this one. The row counts when it
// shows the whole title, the title followed by a status, or a long enough
// leading truncation (the list cut it, with or without …).
func listTitleHit(want, got string) bool {
	w := normText(want)
	if w == "" {
		return false
	}
	head, truncated := cutListEllipsis(got)
	g := normText(head)
	if g == "" {
		return false
	}
	if truncated {
		return strings.HasPrefix(w, g) && len([]rune(g)) >= listPrefixMin(w)
	}
	if g == w || strings.HasPrefix(g, w) {
		return true
	}
	return strings.HasPrefix(w, g) && len([]rune(g)) >= listPrefixMin(w)
}

func listPrefixMin(w string) int {
	n := len([]rune(w))
	if n < 12 {
		return n
	}
	return 12
}

func cutListEllipsis(s string) (string, bool) {
	best := -1
	for _, mark := range []string{"…", "...", "⋯"} {
		if i := strings.Index(s, mark); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return s, false
	}
	return s[:best], true
}

// listWant is the title the list has to show. A body is only a fallback,
// and only its opening is visible in a list, so that case stays the search key.
func (p *publishRun) listWant() string {
	if p == nil {
		return ""
	}
	if t := strings.TrimSpace(p.spec.Title); t != "" {
		return t
	}
	if b := strings.TrimSpace(p.spec.Body); b != "" {
		return clipRunes(b, 12)
	}
	return ""
}
