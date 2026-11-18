package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// boundChatCardPage is one page, screen, or tool result already read for a
// task. It is not the card's question or the person's "I've completed" click.
type boundChatCardPage struct {
	TenantID  string
	UserID    string
	RunID     string
	SessionID string
	LeaseID   string
	Frame     string
	Origin    string
	URL       string
	Source    string
	Text      string
	At        time.Time
}

var chatCardPages struct {
	mu    sync.Mutex
	items []boundChatCardPage
}

func noteChatCardBoundPage(page boundChatCardPage) {
	page.Text = strings.TrimSpace(page.Text)
	page.UserID = strings.TrimSpace(page.UserID)
	page.RunID = strings.TrimSpace(page.RunID)
	if page.Text == "" || page.UserID == "" || page.RunID == "" {
		return
	}
	if page.Source == "" {
		page.Source = "page"
	}
	if page.At.IsZero() {
		page.At = time.Now().UTC()
	}
	chatCardPages.mu.Lock()
	defer chatCardPages.mu.Unlock()
	chatCardPages.items = append(chatCardPages.items, page)
	if len(chatCardPages.items) > 400 {
		chatCardPages.items = append([]boundChatCardPage(nil), chatCardPages.items[len(chatCardPages.items)-400:]...)
	}
}

func observeChatCardBoundPage(p agentservice.Principal, card chatcard.Card) (agentservice.ChatCardEvidence, error) {
	if strings.TrimSpace(p.UserID) == "" || p.UserID != card.UserID || p.TenantID != card.TenantID {
		return agentservice.ChatCardEvidence{}, agentservice.ErrChatCardNoEvidence
	}
	wantFrame := ""
	wantOrigin := ""
	if card.Target != nil {
		wantFrame = strings.TrimSpace(card.Target.Frame)
		wantOrigin = strings.TrimSpace(card.Target.Origin)
	}
	wantLease := strings.TrimSpace(card.LeaseID)
	chatCardPages.mu.Lock()
	defer chatCardPages.mu.Unlock()
	var found boundChatCardPage
	var ok bool
	for _, page := range chatCardPages.items {
		if page.UserID != card.UserID || page.RunID != card.RunID {
			continue
		}
		if page.TenantID != "" && page.TenantID != card.TenantID {
			continue
		}
		if wantLease != "" && strings.TrimSpace(page.LeaseID) != wantLease {
			continue
		}
		if wantFrame != "" && strings.TrimSpace(page.Frame) != wantFrame {
			continue
		}
		if wantOrigin != "" && strings.TrimSpace(page.Origin) != wantOrigin {
			continue
		}
		text := strings.TrimSpace(page.Text)
		if text == "" || text == strings.TrimSpace(card.Question) {
			continue
		}
		repeatsLine := false
		for _, line := range card.Lines {
			if text == strings.TrimSpace(line.Text) {
				repeatsLine = true
				break
			}
		}
		if repeatsLine {
			continue
		}
		if !ok || page.At.After(found.At) {
			found, ok = page, true
		}
	}
	if !ok {
		return agentservice.ChatCardEvidence{}, agentservice.ErrChatCardNoEvidence
	}
	return agentservice.ChatCardEvidence{Source: found.Source, Text: found.Text, ObservedAt: found.At}, nil
}

func installChatCardObserver(svc *agentservice.Service, cards *chatcard.Store) {
	if svc == nil || cards == nil {
		return
	}
	svc.SetChatCardObserver(agentservice.ChatCardObserverFunc(func(_ context.Context, p agentservice.Principal, _, runID string) (agentservice.ChatCardEvidence, error) {
		card, err := cards.FindByRun(p.TenantID, p.UserID, runID)
		if err != nil {
			return agentservice.ChatCardEvidence{}, agentservice.ErrChatCardNoEvidence
		}
		return observeChatCardBoundPage(p, card)
	}))
}

func noteChatCardPageFromScope(scope agentruntime.Scope, url, title, body, frame string) {
	owner, _ := desktop.SplitDesktopUser(scope.UserID)
	text := strings.TrimSpace(body)
	if text == "" {
		text = strings.TrimSpace(title)
	}
	if text == "" {
		text = strings.TrimSpace(url)
	}
	noteChatCardBoundPage(boundChatCardPage{
		TenantID: scope.TenantID, UserID: owner, RunID: scope.RunID, SessionID: scope.SessionID,
		LeaseID: scope.UserID, Frame: frame, Origin: originOfURL(url), URL: url,
		Source: "page", Text: text,
	})
}

func originOfURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}
