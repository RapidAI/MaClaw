package main

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopSecretFocus and desktopSecretSend are the live insertion path.
// Tests replace them. Production reads the focused control and sends one
// CDP Input.insertText. The value is never written to a log.
var desktopSecretFocus = desktopLiveSecretFocus
var desktopSecretSend = desktopLiveSecretSend

func desktopLiveSession(scope agentruntime.Scope) *browser.BrowserAgentSession {
	scope = desktopOwner(scope)
	userKey, err := desktopUserKey(scope)
	if err != nil {
		return nil
	}
	binding := desktopBindingFor(userKey)
	binding.mu.Lock()
	session := binding.session
	binding.mu.Unlock()
	if session == nil || !session.DesktopConnected() {
		return nil
	}
	return session
}

func desktopLiveSecretFocus(scope agentruntime.Scope) (bool, string) {
	session := desktopLiveSession(scope)
	if session == nil {
		return false, ""
	}
	return session.FocusedInputKind()
}

func desktopLiveSecretSend(scope agentruntime.Scope, method string, params map[string]any) error {
	if method != "Input.insertText" {
		return fillErrorCode("unavailable")
	}
	text, _ := params["text"].(string)
	session := desktopLiveSession(scope)
	if session == nil {
		return fillErrorCode("unavailable")
	}
	if err := session.InsertPasswordText(text); err != nil {
		return fillErrorCode("unavailable")
	}
	return nil
}

type fillErrorCode string

func (e fillErrorCode) Error() string { return string(e) }

func applyDesktopSecretFill(scope agentruntime.Scope, value string) error {
	focused, inputType := desktopSecretFocus(scope)
	err := browser.FillPasswordField(focused, inputType, value, func(method string, params map[string]any) error {
		return desktopSecretSend(scope, method, params)
	})
	if err == nil {
		return nil
	}
	code := err.Error()
	if code != "not_password_field" && code != "no_focus" && code != "unavailable" {
		code = "unavailable"
	}
	if value != "" && strings.Contains(code, value) {
		code = "unavailable"
	}
	return fillErrorCode(code)
}

func (s *HTTPServer) handleDesktopSecretFill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	scope := agentruntime.Scope{TenantID: p.TenantID, UserID: p.UserID, InstanceID: r.PathValue("instanceId")}
	if err := applyDesktopSecretFill(scope, in.Value); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"filled": strings.TrimSpace(in.Name)})
}
