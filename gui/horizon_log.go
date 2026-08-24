package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func horizonLog(sess *horizonSession, event, extra string) {
	task, owner, status := "", "", ""
	round := 0
	if sess != nil {
		sess.mu.Lock()
		task, owner, status, round = sess.horizonLogIDsLocked()
		sess.mu.Unlock()
	}
	horizonLogIDs(task, owner, status, round, event, extra)
}

func horizonLogOwner(owner, event, extra string) {
	horizonLogIDs("", strings.TrimSpace(owner), "", 0, event, extra)
}

func (s *horizonSession) horizonLogIDsLocked() (task, owner, status string, round int) {
	if s == nil {
		return "", "", "", 0
	}
	owner = strings.TrimSpace(s.ownerID)
	if s.state != nil {
		task = strings.TrimSpace(s.state.TaskID)
		status = strings.TrimSpace(s.state.Status)
		round = s.state.RoundIndex
	}
	return task, owner, status, round
}

func horizonLogIDs(task, owner, status string, round int, event, extra string) {
	event = strings.TrimSpace(event)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		log.Printf("[horizon] event=%s task=%s owner=%s status=%s round=%d", event, task, owner, status, round)
		return
	}
	log.Printf("[horizon] event=%s task=%s owner=%s status=%s round=%d %s", event, task, owner, status, round, extra)
}

func horizonLogKV(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " ")
}

func horizonLogField(key, value string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return key + "=" + horizonLogQuote(value)
}

func horizonLogClip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return longhorizon.Clip(s, max)
}

func horizonLogQuote(s string) string {
	return fmt.Sprintf("%q", horizonLogClip(s, 80))
}
