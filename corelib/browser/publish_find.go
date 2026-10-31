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
