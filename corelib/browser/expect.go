package browser

import (
	"fmt"
	"regexp"
	"strings"
)

func (s *BrowserAgentSession) verifyExpect(obs *BrowserObservation, expect ExpectSpec) error {
	if s == nil || obs == nil {
		return nil
	}
	expect.Type = strings.ToLower(strings.TrimSpace(expect.Type))
	if expect.Type == "" {
		return nil
	}
	pattern := strings.TrimSpace(expect.Pattern)
	switch expect.Type {
	case "url_contains":
		if pattern == "" {
			return fmt.Errorf("expect url_contains requires a pattern")
		}
		if !strings.Contains(strings.ToLower(obs.Snapshot.URL), strings.ToLower(pattern)) {
			return fmt.Errorf("expect failed: url %q does not contain %q", obs.Snapshot.URL, pattern)
		}
	case "text":
		if pattern == "" {
			return fmt.Errorf("expect text requires a pattern")
		}
		haystack := strings.ToLower(obs.Snapshot.PageTextExcerpt + " " + obs.Snapshot.Title)
		if !strings.Contains(haystack, strings.ToLower(pattern)) {
			return fmt.Errorf("expect failed: page text does not contain %q", pattern)
		}
	case "url_matches":
		if pattern == "" {
			return fmt.Errorf("expect url_matches requires a pattern")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("expect url_matches invalid regex %q", pattern)
		}
		if !re.MatchString(obs.Snapshot.URL) {
			return fmt.Errorf("expect failed: url %q does not match %q", obs.Snapshot.URL, pattern)
		}
	case "ref_appears":
		if pattern == "" {
			return fmt.Errorf("expect ref_appears requires a ref")
		}
		if findExpectRef(obs.Snapshot.Refs, pattern) == nil {
			return fmt.Errorf("expect failed: ref/name %q not in current observe", pattern)
		}
	case "ref_gone":
		if pattern == "" {
			return fmt.Errorf("expect ref_gone requires a ref")
		}
		if findExpectRef(obs.Snapshot.Refs, pattern) != nil {
			return fmt.Errorf("expect failed: ref/name %q still present", pattern)
		}
	case "checked":
		if pattern == "" {
			return fmt.Errorf("expect checked requires a ref")
		}
		ref := findExpectRef(obs.Snapshot.Refs, pattern)
		if ref == nil {
			return fmt.Errorf("expect failed: ref/name %q not in current observe", pattern)
		}
		if !ref.Checked {
			return fmt.Errorf("expect failed: %q is not checked", pattern)
		}
	case "no_flag":
		if pattern == "" {
			return fmt.Errorf("expect no_flag requires a flag name")
		}
		set, known := pageFlagSet(obs.Snapshot.PageFlags, pattern)
		if !known {
			return fmt.Errorf("unknown page flag %q", pattern)
		}
		if set {
			return fmt.Errorf("expect failed: page flag %q is set", pattern)
		}
	case "select_value":
		if pattern == "" {
			return fmt.Errorf("expect select_value requires a value")
		}
		refPat, val, ok := strings.Cut(pattern, "=")
		if ok {
			ref := findExpectRef(obs.Snapshot.Refs, strings.TrimSpace(refPat))
			if ref == nil {
				return fmt.Errorf("expect failed: ref/name %q not in current observe", strings.TrimSpace(refPat))
			}
			if !strings.EqualFold(strings.TrimSpace(ref.Value), strings.TrimSpace(val)) {
				return fmt.Errorf("expect failed: selected value %q is not %q", ref.Value, strings.TrimSpace(val))
			}
			return nil
		}
		want := strings.ToLower(pattern)
		for _, ref := range obs.Snapshot.Refs {
			if strings.ToLower(strings.TrimSpace(ref.Value)) == want {
				return nil
			}
		}
		return fmt.Errorf("expect failed: no control has value %q", pattern)
	case "dialog":
		if s.session == nil || !s.session.hasPendingDialog() {
			return fmt.Errorf("expect failed: no JavaScript dialog is open")
		}
	default:
		return fmt.Errorf("unknown expect type %q (use url_contains, url_matches, text, ref_appears, ref_gone, checked, select_value, no_flag, dialog)", expect.Type)
	}
	return nil
}

func validExpect(expect ExpectSpec) bool {
	typ := strings.ToLower(strings.TrimSpace(expect.Type))
	switch typ {
	case "dialog":
		return true
	case "url_contains", "url_matches", "text", "ref_appears", "ref_gone", "checked", "no_flag", "select_value":
	default:
		return false
	}
	pattern := strings.TrimSpace(expect.Pattern)
	if len(pattern) < 2 {
		return false
	}
	if typ == "url_contains" && (pattern == "/" || strings.EqualFold(pattern, "http")) {
		return false
	}
	if typ == "url_matches" {
		if trivialURLMatch(pattern) {
			return false
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return false
		}
	}
	return true
}

func trivialURLMatch(pattern string) bool {
	switch strings.TrimSpace(pattern) {
	case ".", ".*", ".+", "^", "$", "^$", "^.*$", ".*$", "^.*":
		return true
	default:
		return false
	}
}

func (s *BrowserAgentSession) applyGoalClassContract(result *BrowserActionResult, expect ExpectSpec) *BrowserActionResult {
	if result == nil {
		return result
	}
	if result.Status == "ask" || result.Status == "blocked" {
		return result
	}
	if !result.GoalClass {
		return result
	}
	if validExpect(expect) {
		if s != nil {
			s.mu.Lock()
			s.lastMissingKey = ""
			s.missingExpectN = 0
			s.mu.Unlock()
		}
		return result
	}
	key := strings.TrimSpace(result.Detail)
	n := 1
	if s != nil {
		s.mu.Lock()
		if key != "" && key == s.lastMissingKey {
			s.missingExpectN++
		} else {
			s.lastMissingKey = key
			s.missingExpectN = 1
		}
		n = s.missingExpectN
		s.mu.Unlock()
	}
	result.Status = "expect_failed"
	result.Display = "expect required for submit/publish click; example expect=url_contains:/success"
	if n > 1 {
		result.Display = "expect still required for this submit control; add expect=url_contains:… and observe. Do not use computer_*."
	}
	if result.Data == nil {
		result.Data = map[string]interface{}{}
	}
	result.Data["reason"] = "missing_expect"
	return result
}

func lastExpectExcerpt(expect ExpectSpec) map[string]string {
	typ := strings.ToLower(strings.TrimSpace(expect.Type))
	if typ == "" {
		return nil
	}
	return map[string]string{
		"type":    typ,
		"pattern": strings.TrimSpace(expect.Pattern),
	}
}

func attachLastExpectLedger(s *BrowserAgentSession, data map[string]interface{}) map[string]interface{} {
	if s == nil {
		return data
	}
	s.mu.RLock()
	excerpt := lastExpectExcerpt(s.lastExpect)
	s.mu.RUnlock()
	if excerpt == nil {
		return data
	}
	out := data
	if out == nil {
		out = map[string]interface{}{}
	} else {
		cloned := make(map[string]interface{}, len(out)+1)
		for k, v := range out {
			cloned[k] = v
		}
		out = cloned
	}
	out["last_expect"] = excerpt
	return out
}

func (s *BrowserAgentSession) applyExpect(result *BrowserActionResult, expect ExpectSpec) *BrowserActionResult {
	if result != nil && (result.Status == "ask" || result.Status == "blocked") {
		return result
	}
	if s == nil || result == nil || strings.TrimSpace(expect.Type) == "" {
		return result
	}
	fail := func(err error, obs *BrowserObservation, reason string) {
		result.Status = "expect_failed"
		result.Display = err.Error()
		extra := map[string]interface{}{}
		if result.Data != nil {
			if target, ok := result.Data["target"]; ok {
				extra["target"] = target
			}
		}
		if reason != "" {
			extra["reason"] = reason
		}
		extra["delta"] = expectDelta(obs, err)
		result.Data = compactFailureData(obs, extra)
	}
	if !validExpect(expect) {
		fail(fmt.Errorf("expect rejected as trivial"), nil, "missing_expect")
		return result
	}
	snap, ok := s.GetSnapshot(result.SnapshotID)
	if !ok || snap == nil {
		fail(fmt.Errorf("expect failed: snapshot missing"), nil, "mismatch")
		return result
	}
	s.mu.Lock()
	s.lastExpect = expect
	s.mu.Unlock()
	obs := &BrowserObservation{Snapshot: *snap}
	if err := s.verifyExpect(obs, expect); err != nil {
		fail(err, obs, "mismatch")
		return result
	}
	if result.Status == "unchanged" {
		if result.GoalClass {
			fail(fmt.Errorf("expect failed: page did not change"), obs, "unchanged")
			return result
		}
		result.Status = "ok"
		result.Display = strings.TrimSuffix(result.Display, unchangedDisplaySuffix)
		if result.Data != nil {
			delete(result.Data, "delta")
		}
	}
	return result
}

func expectDelta(obs *BrowserObservation, err error) map[string]interface{} {
	delta := map[string]interface{}{}
	if obs != nil {
		delta["url"] = obs.Snapshot.URL
		delta["title"] = obs.Snapshot.Title
		delta["ready_state"] = obs.Snapshot.ReadyState
		delta["page_flags"] = obs.Snapshot.PageFlags
	}
	if err != nil {
		delta["error"] = err.Error()
	}
	return delta
}

func findExpectRef(refs []BrowserElementRef, pattern string) *BrowserElementRef {
	want := strings.TrimSpace(pattern)
	if want == "" {
		return nil
	}
	for i := range refs {
		if strings.EqualFold(strings.TrimSpace(refs[i].Ref), want) {
			return &refs[i]
		}
	}
	if strings.HasPrefix(strings.ToLower(want), "@") {
		return nil
	}
	needle := strings.ToLower(want)
	for i := range refs {
		name := strings.ToLower(strings.TrimSpace(firstNonEmpty(refs[i].Name, refs[i].Text)))
		if name == needle {
			return &refs[i]
		}
	}
	return nil
}

func pageFlagSet(flags BrowserPageFlags, name string) (set bool, known bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "captcha_widget":
		return flags.CaptchaWidget, true
	case "captcha":
		return flags.Captcha, true
	case "login_wall":
		return flags.LoginWall, true
	case "mfa":
		return flags.MFA, true
	case "canvas":
		return flags.Canvas, true
	default:
		return false, false
	}
}
