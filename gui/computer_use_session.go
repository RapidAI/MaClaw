package main

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

const (
	computerUseDefaultOwner     = "default"
	computerUseMaxSessions      = 24
	computerUseSessionIdleEvict = 2 * time.Hour
)

func setComputerUseOwner(owner string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	globalComputerUse.activeOwner = owner
	globalComputerUse.mu.Unlock()
}

// computerUseOwnerFromLoop matches ExecuteToolCall: SessionKey, else UserID,
// else fallback. Do not use promptRuntimeUserID / PolicyOwnerID here.
func computerUseOwnerFromLoop(loopCtx *LoopContext, fallback string) string {
	owner := strings.TrimSpace(fallback)
	if loopCtx != nil {
		if sk := strings.TrimSpace(loopCtx.Runtime.Conversation.SessionKey); sk != "" {
			owner = sk
		} else if uid := strings.TrimSpace(loopCtx.UserID); uid != "" {
			owner = uid
		}
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return computerUseDefaultOwner
	}
	return owner
}

func computerUseOwnerKey() string {
	owner := strings.TrimSpace(globalComputerUse.activeOwner)
	if owner == "" {
		return computerUseDefaultOwner
	}
	return owner
}

func cuSession() *computeruse.Session {
	sess, _ := cuSessionAndOwner()
	return sess
}

func cuSessionForOwner(owner string) *computeruse.Session {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	if owner == computerUseDefaultOwner && globalComputerUse.session != nil {
		if globalComputerUse.sessions != nil {
			if sess := globalComputerUse.sessions[owner]; sess != nil {
				return sess
			}
		}
		return globalComputerUse.session
	}
	if globalComputerUse.sessions == nil {
		return nil
	}
	return globalComputerUse.sessions[owner]
}

func clearComputerUseTaskStateForOwner(owner string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	if globalComputerUse.taskStates != nil {
		delete(globalComputerUse.taskStates, owner)
	}
}

func cuSessionAndOwner() (*computeruse.Session, string) {
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	owner := computerUseOwnerKey()
	return cuSessionLocked(), owner
}

func cuSessionLocked() *computeruse.Session {
	owner := computerUseOwnerKey()
	if globalComputerUse.sessions == nil {
		globalComputerUse.sessions = make(map[string]*computeruse.Session)
	}
	if globalComputerUse.sessionUsed == nil {
		globalComputerUse.sessionUsed = make(map[string]time.Time)
	}
	// Tests and registerComputerUseTools may assign .session directly.
	if owner == computerUseDefaultOwner && globalComputerUse.session != nil {
		if globalComputerUse.sessions[owner] != globalComputerUse.session {
			globalComputerUse.sessions[owner] = globalComputerUse.session
		}
	}
	sess := globalComputerUse.sessions[owner]
	if sess == nil {
		sess = computeruse.NewSession(computeruse.DefaultConfig())
		globalComputerUse.sessions[owner] = sess
	}
	globalComputerUse.session = sess
	globalComputerUse.sessionUsed[owner] = time.Now()
	evictComputerUseSessionsLocked(owner)
	sess.SetWindowResolver(accessibility.WindowTitleAtPoint)
	if computerUseTargetAppsFn != nil {
		if apps, ok := computerUseTargetAppsFn(); ok {
			sess.SetTargetApps(apps)
		}
	}
	return sess
}

func computerUseOwnerClaimOnlyLocked(owner string) bool {
	return globalComputerUse.horizonClaimOnly[owner]
}

func evictComputerUseSessionsLocked(keep string) {
	if len(globalComputerUse.sessions) <= computerUseMaxSessions {
		now := time.Now()
		for owner, used := range globalComputerUse.sessionUsed {
			if owner == keep || computerUseOwnerClaimOnlyLocked(owner) {
				continue
			}
			if !used.IsZero() && now.Sub(used) > computerUseSessionIdleEvict {
				forgetComputerUseOwnerLocked(owner)
			}
		}
		return
	}
	var oldest string
	var oldestAt time.Time
	first := true
	for owner, used := range globalComputerUse.sessionUsed {
		if owner == keep || computerUseOwnerClaimOnlyLocked(owner) {
			continue
		}
		if first || used.Before(oldestAt) {
			oldest, oldestAt, first = owner, used, false
		}
	}
	if oldest != "" {
		forgetComputerUseOwnerLocked(oldest)
	}
}

func forgetComputerUseOwnerLocked(owner string) {
	delete(globalComputerUse.sessions, owner)
	delete(globalComputerUse.sessionUsed, owner)
	delete(globalComputerUse.taskStates, owner)
	// Keep horizonClaimOnly. Evicting a session mid GUI episode must not
	// let computer_done take today's outer-task completion path.
}

func resetComputerUseSessionForOwner(owner string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	claim := false
	if globalComputerUse.horizonClaimOnly != nil {
		claim = globalComputerUse.horizonClaimOnly[owner]
	}
	forgetComputerUseOwnerLocked(owner)
	if claim {
		if globalComputerUse.horizonClaimOnly == nil {
			globalComputerUse.horizonClaimOnly = make(map[string]bool)
		}
		globalComputerUse.horizonClaimOnly[owner] = true
	}
}

func setHorizonComputerUseClaimOnly(owner string, enabled bool) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	if globalComputerUse.horizonClaimOnly == nil {
		globalComputerUse.horizonClaimOnly = make(map[string]bool)
	}
	if enabled {
		globalComputerUse.horizonClaimOnly[owner] = true
		return
	}
	delete(globalComputerUse.horizonClaimOnly, owner)
}

func horizonComputerUseClaimOnly(owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	return globalComputerUse.horizonClaimOnly[owner]
}

func forEachComputerUseSession(fn func(*computeruse.Session)) {
	if fn == nil {
		return
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	seen := map[*computeruse.Session]struct{}{}
	for _, sess := range globalComputerUse.sessions {
		if sess == nil {
			continue
		}
		if _, ok := seen[sess]; ok {
			continue
		}
		seen[sess] = struct{}{}
		fn(sess)
	}
	if globalComputerUse.session != nil {
		if _, ok := seen[globalComputerUse.session]; !ok {
			fn(globalComputerUse.session)
		}
	}
}

func resetComputerUseSessionsLocked() {
	if globalComputerUse.sessions == nil {
		globalComputerUse.sessions = make(map[string]*computeruse.Session)
	}
	for _, sess := range globalComputerUse.sessions {
		if sess != nil {
			sess.ResetControl()
		}
	}
	if globalComputerUse.session != nil {
		globalComputerUse.session.ResetControl()
	} else {
		sess := computeruse.NewSession(computeruse.DefaultConfig())
		globalComputerUse.session = sess
		globalComputerUse.sessions[computerUseDefaultOwner] = sess
	}
}
