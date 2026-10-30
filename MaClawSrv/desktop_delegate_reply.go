package main

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// A Hub bot turn that handed work to a teammate ends with one sentence that
// says who has it. The prompt alone did not hold: on papers 2026-10-30 the
// main bot answered a delegation with the task's requirements and
// constraints as bullets plus "代码还没到，我不替它先写". The teammate's
// result reaches the person through the feed, so anything more in this turn
// is a restatement or a guess. The reply is therefore set here, from the
// delegations that actually succeeded, and not left to the model.

type desktopDelegation struct {
	targets []string
	chinese bool
	at      time.Time
}

var (
	desktopDelegatedMu sync.Mutex
	desktopDelegated   = map[string]*desktopDelegation{}
)

const desktopDelegatedKeep = time.Hour

func desktopNoteDelegated(runKey, target, personText string) {
	runKey, target = strings.TrimSpace(runKey), strings.TrimSpace(target)
	if runKey == "" || target == "" {
		return
	}
	now := time.Now()
	desktopDelegatedMu.Lock()
	defer desktopDelegatedMu.Unlock()
	for k, d := range desktopDelegated {
		if now.Sub(d.at) > desktopDelegatedKeep {
			delete(desktopDelegated, k)
		}
	}
	d := desktopDelegated[runKey]
	if d == nil {
		d = &desktopDelegation{chinese: desktopTextIsChinese(personText)}
		desktopDelegated[runKey] = d
	}
	d.at = now
	for _, t := range d.targets {
		if t == target {
			return
		}
	}
	d.targets = append(d.targets, target)
}

// desktopTakeDelegated returns and forgets the delegations of one run.
func desktopTakeDelegated(keys ...string) *desktopDelegation {
	desktopDelegatedMu.Lock()
	defer desktopDelegatedMu.Unlock()
	for _, k := range keys {
		if d := desktopDelegated[k]; d != nil && len(d.targets) > 0 {
			for _, kk := range keys {
				delete(desktopDelegated, kk)
			}
			return d
		}
	}
	return nil
}

// desktopDelegationReply is the whole reply of a turn that delegated.
func desktopDelegationReply(d *desktopDelegation) string {
	if d == nil || len(d.targets) == 0 {
		return ""
	}
	if d.chinese {
		return "已交给 " + strings.Join(d.targets, "、") + "，结果出来会直接发到这里。"
	}
	if len(d.targets) > 1 {
		return strings.Join(d.targets, " and ") + " have it; the results will show up here."
	}
	return d.targets[0] + " has it; the result will show up here."
}

// desktopReplyAfterDelegation replaces the model's text when this run
// delegated. instanceID and the run/session ids name the run the same way
// desktopDelegate keyed it.
func desktopReplyAfterDelegation(instanceID, runID, sessionID, content string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return content
	}
	keys := []string{}
	if v := strings.TrimSpace(runID); v != "" {
		keys = append(keys, instanceID+"|"+v)
	}
	if v := strings.TrimSpace(sessionID); v != "" {
		keys = append(keys, instanceID+"|"+v)
	}
	if reply := desktopDelegationReply(desktopTakeDelegated(keys...)); reply != "" {
		return reply
	}
	return content
}

func desktopTextIsChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
