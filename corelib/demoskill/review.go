package demoskill

import (
	"encoding/json"
	"strings"
)

// SpecMarker prefixes the one-line machine copy of the skill inside its
// guidance, so the result judge can find the steps and the deliverable in a
// run that loaded the skill.
const SpecMarker = "[demoskill-spec] "

// SpecLine is the machine copy appended to the guidance.
func (sk *Skill) SpecLine() string {
	data, err := json.Marshal(sk)
	if err != nil {
		return ""
	}
	return SpecMarker + string(data)
}

// FindSpec returns the last demo skill whose spec line appears in text.
func FindSpec(text string) *Skill {
	idx := strings.LastIndex(text, SpecMarker)
	if idx < 0 {
		return nil
	}
	rest := text[idx+len(SpecMarker):]
	if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
		rest = rest[:nl]
	}
	var sk Skill
	if json.Unmarshal([]byte(strings.TrimSpace(rest)), &sk) != nil || sk.Normalize() != nil {
		return nil
	}
	return &sk
}

// Judge checks one requirement against the run's evidence. err means the
// check could not run; it says nothing about the result.
type Judge func(requirement string) (ok bool, gap string, err error)

// Review runs the per-step checks in order, then the deliverable check, and
// returns whether the loop must continue and the directive for the model.
// A step whose check cannot run is left for the deliverable check: an
// unavailable judge must not stall the run.
func (p *Progress) Review(judge Judge) (cont bool, directive string) {
	if p == nil || p.Skill == nil || judge == nil {
		return false, ""
	}
	for i, st := range p.Skill.Steps {
		if p.Done[i] {
			continue
		}
		ok, gap, err := judge(st.Check)
		if err != nil {
			continue
		}
		if !ok {
			return true, p.Check(i, false, gap)
		}
		p.Check(i, true, "")
	}
	req := "交付物：" + p.Skill.Deliverable
	if p.Skill.Evidence != "" {
		req += "；证据：" + p.Skill.Evidence
	}
	ok, gap, err := judge(req)
	if err != nil {
		return false, ""
	}
	if !ok {
		return true, p.Deliver(false, "") + " " + strings.TrimSpace(gap)
	}
	p.Deliver(true, firstNonEmpty(gap, "结果判定通过"))
	return false, ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
