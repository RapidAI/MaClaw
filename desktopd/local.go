package desktopd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

const localPrefix = "/v1/desktops/local/"

// serveLocal is the same-machine screenshot and file path. The ticket is
// the authorization Hub already checked; the desktopd API token is not on
// the request. Bytes stay on this machine.
func (s *Service) serveLocal(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.HubLink == nil || r.URL == nil || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") || strings.Contains(r.URL.Path, `\`) {
		http.NotFound(w, r)
		return
	}
	ticket := strings.TrimPrefix(r.URL.Path, localPrefix)
	if ticket == "" || strings.Contains(ticket, "/") {
		http.NotFound(w, r)
		return
	}
	claims, err := edgelink.VerifyLocal(s.HubLink.Key, ticket, time.Now())
	if err != nil || claims.NodeID != s.HubLink.NodeID() {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	switch claims.Action {
	case "screenshot":
		s.serveLocalScreenshot(w, r, claims, raw)
	case "file":
		s.serveLocalFile(w, r, claims, raw)
	default:
		http.NotFound(w, r)
	}
}

func localIdentityMatches(claims edgelink.LocalClaims, tenantID, userID string) bool {
	return strings.TrimSpace(tenantID) == strings.TrimSpace(claims.TenantID) && strings.TrimSpace(userID) == strings.TrimSpace(claims.UserID)
}

func (s *Service) serveLocalScreenshot(w http.ResponseWriter, r *http.Request, claims edgelink.LocalClaims, raw []byte) {
	var in struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Display  string `json:"display"`
		Name     string `json:"name"`
		desktop.ShotOptions
	}
	if json.Unmarshal(raw, &in) != nil || !localIdentityMatches(claims, in.TenantID, in.UserID) {
		http.NotFound(w, r)
		return
	}
	shot, saved, err := s.CaptureShot(r.Context(), in.TenantID, in.UserID, in.Display, in.Name, in.ShotOptions)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	body := map[string]any{
		"mime":         shot.MIME,
		"image_base64": base64.StdEncoding.EncodeToString(shot.Data),
		"bytes":        len(shot.Data),
	}
	if saved != "" {
		body["saved_path"] = saved
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Service) serveLocalFile(w http.ResponseWriter, r *http.Request, claims edgelink.LocalClaims, raw []byte) {
	var in struct {
		TenantID  string `json:"tenant_id"`
		UserID    string `json:"user_id"`
		Action    string `json:"action"`
		Path      string `json:"path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if json.Unmarshal(raw, &in) != nil || !localIdentityMatches(claims, in.TenantID, in.UserID) {
		http.NotFound(w, r)
		return
	}
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "read", "bytes", "write", "edit", "list":
	default:
		writeOp(w, "", fmt.Errorf("%w: file action is invalid", ErrInvalid))
		return
	}
	var text string
	var err error
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "read":
		text, err = s.ReadFile(r.Context(), in.TenantID, in.UserID, in.Path)
	case "bytes":
		text, err = s.ReadBytes(r.Context(), in.TenantID, in.UserID, in.Path)
	case "write":
		text, err = s.WriteFile(r.Context(), in.TenantID, in.UserID, in.Path, in.Content)
	case "edit":
		text, err = s.EditFile(r.Context(), in.TenantID, in.UserID, in.Path, in.OldString, in.NewString)
	case "list":
		text, err = s.ListFile(r.Context(), in.TenantID, in.UserID, in.Path)
	}
	writeOp(w, text, err)
}
