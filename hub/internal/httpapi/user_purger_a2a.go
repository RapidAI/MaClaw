package httpapi

import (
	"context"
	"encoding/json"
	"strings"
)

// deleteUserA2AData removes collaboration state addressed by the user's
// machine IDs. Sessions hold participant references inside JSON, therefore a
// simple SQL foreign-key cleanup cannot discover all dependent records.
func (p *UserDataPurger) deleteUserA2AData(ctx context.Context, tenantID, userID string, logErr func(string, error)) {
	if p.DB == nil || strings.TrimSpace(userID) == "" {
		return
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT id FROM machines WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_machines_list", err)
		}
		return
	}
	var machineIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			logErr("a2a_machines_scan", err)
			continue
		}
		if id = strings.TrimSpace(id); id != "" {
			machineIDs = append(machineIDs, id)
		}
	}
	if err := rows.Close(); err != nil {
		logErr("a2a_machines_close", err)
	}
	if len(machineIDs) == 0 {
		return
	}
	machineSet := make(map[string]struct{}, len(machineIDs))
	for _, id := range machineIDs {
		machineSet[id] = struct{}{}
	}

	// Remove direct machine-addressed rows first. This handles a subset of old
	// schemas even when session_json has not yet been introduced.
	for _, id := range machineIDs {
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM a2a_group_profiles WHERE tenant_id = ? AND agent_id = ?`, tenantID, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_profiles", err)
		}
	}

	rows, err = p.DB.QueryContext(ctx, `SELECT session_id, session_json FROM a2a_group_sessions WHERE tenant_id = ?`, tenantID)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_sessions_list", err)
		}
		return
	}
	var sessionIDs []string
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			logErr("a2a_sessions_scan", err)
			continue
		}
		if userA2ASessionReferencesAny(raw, machineSet) {
			sessionIDs = append(sessionIDs, id)
		}
	}
	if err := rows.Close(); err != nil {
		logErr("a2a_sessions_close", err)
	}
	for _, sessionID := range sessionIDs {
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM a2a_group_invites WHERE tenant_id = ? AND session_id = ?`, tenantID, sessionID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_session_invites", err)
		}
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM a2a_group_sessions WHERE tenant_id = ? AND session_id = ?`, tenantID, sessionID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_sessions", err)
		}
	}
	for _, id := range machineIDs {
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM a2a_group_invites WHERE tenant_id = ? AND (to_id = ? OR from_id = ?)`, tenantID, id, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("a2a_invites", err)
		}
	}
	// The service also keeps a warm in-memory mirror. Remove it after durable
	// state so a later mutation cannot write a deleted session back to SQLite.
	if p.GroupDiscussionSvc != nil {
		if _, err := p.GroupDiscussionSvc.DeleteSessionsByParticipants(tenantID, machineIDs); err != nil {
			logErr("a2a_runtime", err)
		}
	}
}

func userA2ASessionReferencesAny(raw string, machineSet map[string]struct{}) bool {
	var session struct {
		Participants []struct {
			ID string `json:"id"`
		} `json:"participants"`
		Messages []struct {
			FromID string   `json:"from_id"`
			ToIDs  []string `json:"to_ids"`
		} `json:"messages"`
		Proposals []struct {
			AuthorID string `json:"author_id"`
		} `json:"proposals"`
		Reviews []struct {
			ReviewerID string `json:"reviewer_id"`
		} `json:"reviews"`
		Decision *struct {
			DecidedBy []string `json:"decided_by"`
		} `json:"decision"`
		Escalation *struct {
			RaisedBy string `json:"raised_by"`
		} `json:"escalation"`
	}
	if json.Unmarshal([]byte(raw), &session) != nil {
		return false
	}
	has := func(id string) bool { _, ok := machineSet[strings.TrimSpace(id)]; return ok }
	for _, participant := range session.Participants {
		if has(participant.ID) {
			return true
		}
	}
	for _, message := range session.Messages {
		if has(message.FromID) {
			return true
		}
		for _, id := range message.ToIDs {
			if has(id) {
				return true
			}
		}
	}
	for _, proposal := range session.Proposals {
		if has(proposal.AuthorID) {
			return true
		}
	}
	for _, review := range session.Reviews {
		if has(review.ReviewerID) {
			return true
		}
	}
	if session.Decision != nil {
		for _, id := range session.Decision.DecidedBy {
			if has(id) {
				return true
			}
		}
	}
	return session.Escalation != nil && has(session.Escalation.RaisedBy)
}
