package desktoppool

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// botGUIUse is the last time a bot run drove this user's desktop screen
// (a browser session, a screenshot, an app or open call) through the
// worker's desktop tool route, and the noVNC page that session returned.
type botGUIUse struct {
	at    time.Time
	novnc string
}

func botUseKey(tenantID, userID string) string {
	return store.NormalizeTenantID(tenantID) + "\x00" + strings.TrimSpace(userID)
}

// NoteBotGUIUse records one bot desktop-screen step. An empty novnc keeps
// the page an earlier session of the same desktop returned. It never starts
// or pins the desktop; it only remembers that a bot step just used it.
func (p *Pool) NoteBotGUIUse(tenantID, userID, novnc string) {
	if p == nil || strings.TrimSpace(userID) == "" {
		return
	}
	key := botUseKey(tenantID, userID)
	p.useMu.Lock()
	defer p.useMu.Unlock()
	if p.botUse == nil {
		p.botUse = map[string]botGUIUse{}
	}
	use := p.botUse[key]
	use.at = p.now()
	if novnc = strings.TrimSpace(novnc); novnc != "" {
		use.novnc = novnc
	}
	p.botUse[key] = use
}

// ForgetBotGUIUse drops the record when the desktop is stopped.
func (p *Pool) ForgetBotGUIUse(tenantID, userID string) {
	if p == nil {
		return
	}
	p.useMu.Lock()
	delete(p.botUse, botUseKey(tenantID, userID))
	p.useMu.Unlock()
}

// BotGUIUse reports the last bot desktop-screen step and its noVNC page.
// A zero time means no bot step has used this desktop since Hub started.
// Reading it is free: it does not reach desktopd.
func (p *Pool) BotGUIUse(tenantID, userID string) (time.Time, string) {
	if p == nil {
		return time.Time{}, ""
	}
	p.useMu.Lock()
	defer p.useMu.Unlock()
	use := p.botUse[botUseKey(tenantID, userID)]
	return use.at, use.novnc
}
