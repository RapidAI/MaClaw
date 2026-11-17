package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ChatCardDecision is MaClawSrv's record of one click. Hub does not decide
// whether the task resumes.
type ChatCardDecision struct {
	Status    string   `json:"status"`
	Resume    bool     `json:"resume"`
	Already   bool     `json:"already,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	TaskID    string   `json:"task_id"`
	RunID     string   `json:"run_id"`
	RequestID string   `json:"request_id"`
	BotID     string   `json:"bot_id"`
	Done      bool     `json:"done"`
	Granted   []string `json:"granted,omitempty"`
}

// ChatCardDesktop is the computer a screenshot click may attach to.
// Wake stays false. Hub does not start a different desktop.
type ChatCardDesktop struct {
	BotID       string `json:"bot_id"`
	TaskID      string `json:"task_id"`
	RunID       string `json:"run_id"`
	LeaseID     string `json:"lease_id"`
	DesktopMode string `json:"desktop_mode"`
	Attach      string `json:"attach"`
	Wake        bool   `json:"wake"`
}

// DecideChatCard forwards one authenticated decision to the bot's own instance.
// The body is the card fields the GUI showed. The caller's user id is not
// copied into it.
func (s *Service) DecideChatCard(ctx context.Context, tenantID, userID, botID, runID, requestID string, body map[string]any) (ChatCardDecision, error) {
	path := "/interactions/" + url.PathEscape(strings.TrimSpace(requestID)) + "/decision"
	var out ChatCardDecision
	if err := s.forwardChatCard(ctx, tenantID, userID, botID, runID, http.MethodPost, path, body, &out); err != nil {
		return ChatCardDecision{}, err
	}
	// A click is permission to continue, never a completed purchase.
	out.Done = false
	if out.Mode == "verify" {
		out.Granted = nil
	}
	return out, nil
}

// ChatCardShot is the captured image for one card. Hub does not decode it
// into a log.
type ChatCardShot struct {
	MIME string `json:"mime"`
	Data string `json:"data"`
}

// GetChatCardShot reads the screenshot MaClawSrv stored for this card.
func (s *Service) GetChatCardShot(ctx context.Context, tenantID, userID, botID, runID, requestID string) (ChatCardShot, error) {
	path := "/interactions/" + url.PathEscape(strings.TrimSpace(requestID)) + "/shot"
	var out ChatCardShot
	if err := s.forwardChatCard(ctx, tenantID, userID, botID, runID, http.MethodGet, path, nil, &out); err != nil {
		return ChatCardShot{}, err
	}
	if strings.TrimSpace(out.Data) == "" {
		return ChatCardShot{}, ErrNotFound
	}
	return out, nil
}

// OpenChatCardDesktop asks MaClawSrv which computer this card's screenshot
// belongs to. A mismatched bot or lease is the server's refusal.
func (s *Service) OpenChatCardDesktop(ctx context.Context, tenantID, userID, botID, runID, requestID string, body map[string]any) (ChatCardDesktop, error) {
	path := "/interactions/" + url.PathEscape(strings.TrimSpace(requestID)) + "/desktop"
	var out ChatCardDesktop
	if err := s.forwardChatCard(ctx, tenantID, userID, botID, runID, http.MethodPost, path, body, &out); err != nil {
		return ChatCardDesktop{}, err
	}
	if out.BotID != strings.TrimSpace(botID) {
		return ChatCardDesktop{}, ErrNotFound
	}
	out.Wake = false
	if out.Attach == "" {
		out.Attach = "watch"
	}
	return out, nil
}

func (s *Service) forwardChatCard(ctx context.Context, tenantID, userID, botID, runID, method, suffix string, body, dest any) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" || strings.TrimSpace(botID) == "" {
		return ErrNotFound
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerTokenOn(ctx, tenantID, userID, rec.Bots[index].NodeID)
	if err != nil {
		return err
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID) + suffix
	return s.authAsOwner(ctx, tenantID, userID, &fresh, &token, method, path, body, dest)
}

// ChatCardBody strips anything the GUI must not use to name the caller.
func ChatCardBody(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "user_id", "tenant_id", "authenticated", "actor", "done", "granted", "resume":
			continue
		default:
			out[key] = value
		}
	}
	return out
}
