package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Server-side routing of a desktop bot message (no client decides):
//  1. an explicit continue_session (the person picked a task, or pressed
//     改为新的事) wins, as before;
//  2. otherwise the sticky window Hub keeps (a run that asked: 6h; a thread
//     that just finished: 30 min);
//  3. otherwise MaClawSrv's content routing over the bot's persisted recent
//     threads (POST …/bot-threads/route), within desktopRouteBudget;
//  4. otherwise main (or a new thread when main is busy).
// The decision travels back to the client as session, routed_title and
// routed_via, which the client only displays.

// desktopRouteBudget bounds the whole srv routing call; srv itself keeps
// its model call inside 1.5s. On timeout or error the message goes on as if
// there were no match.
var desktopRouteBudget = 2 * time.Second

type desktopRoute struct {
	Session    string  `json:"session"`
	Title      string  `json:"title"`
	Via        string  `json:"via"`
	Confidence float64 `json:"confidence"`
}

// peekDesktopRouteState reports the sticky conversation (without claiming
// it) and whether this key is a replay of a post Hub already holds.
func (s *Service) peekDesktopRouteState(tenantID, userID, botID, clientMessageID string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	botPrefix := desktopAdmitKey(tenantID, userID, botID, "")
	clientMessageID = strings.TrimSpace(clientMessageID)
	if clientMessageID != "" {
		for key, state := range s.desktopAdmits {
			if state == nil || !strings.HasPrefix(key, botPrefix) {
				continue
			}
			state.mu.Lock()
			same := !state.done && strings.TrimSpace(state.clientMessageID) == clientMessageID
			state.mu.Unlock()
			if same {
				return "", true
			}
		}
	}
	owner, ok := s.desktopAskSession[botPrefix]
	if !ok || strings.TrimSpace(owner.session) == "" {
		return "", false
	}
	window := desktopRecentThreadFor
	if owner.asks {
		window = desktopAskOwnerFor
	}
	if s.now().Sub(owner.at) > window {
		return "", false
	}
	return owner.session, false
}

// routeDesktopMessage asks MaClawSrv where a message with no explicit hint
// goes. An empty route means "no decision": the claim falls back as before.
func (s *Service) routeDesktopMessage(ctx context.Context, tenantID, userID string, rec *record, token *string, instanceID, botID, content, sticky string) desktopRoute {
	if s == nil || rec == nil || token == nil || strings.TrimSpace(instanceID) == "" {
		return desktopRoute{}
	}
	budget := desktopRouteBudget
	// Routing never takes more than a quarter of a shortened command wait.
	if s.messageTimeout > 0 && s.messageTimeout/4 < budget {
		budget = s.messageTimeout / 4
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var out desktopRoute
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/bot-threads/route"
	err := s.authAsOwner(rctx, tenantID, userID, rec, token, http.MethodPost, path, map[string]any{
		"bot_id":         botID,
		"content":        content,
		"sticky_session": sticky,
	}, &out)
	if err != nil {
		traceBot(botID, "route", err, "timeout", traceYes(rctx.Err() != nil))
		if sticky != "" {
			return desktopRoute{Session: sticky, Via: "sticky"}
		}
		return desktopRoute{}
	}
	out.Session = strings.TrimSpace(out.Session)
	if out.Session != "" && !desktopSessionBelongsToBot(botID, out.Session) {
		return desktopRoute{}
	}
	traceBot(botID, "route", nil, "via", out.Via, "thread", traceYes(desktopSessionIsThread(botID, out.Session)))
	return out
}

// routedFor is what the client is told about the choice: only when the
// post really landed in the thread the route named.
func routedFor(route desktopRoute, botID, session string) (string, string) {
	if route.Via == "" || route.Via == "main" || route.Session != session || !desktopSessionIsThread(botID, session) {
		return "", ""
	}
	return route.Via, clipDesktopRunes(strings.TrimSpace(route.Title), 80)
}
