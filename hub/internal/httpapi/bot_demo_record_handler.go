package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// Demonstrate-and-narrate recording (MACLAW_DEMO_SKILL, stage 3):
//
//	POST /api/v1/bots/{id}/demo/start   {"title":"..."}
//	POST /api/v1/bots/{id}/demo/narrate {"text":"..."} or {"audio_base64":"...","mime":"audio/wav"}
//	POST /api/v1/bots/{id}/demo/stop    -> {"timeline":{...},"shot_dir":"..."}
//
// Spoken narration is transcribed with the Hub's ASR when it is available;
// otherwise the client is told to send typed text (asr_available=false).
const demoNarrateBodyLimit = 12 << 20

func demoRecordBody(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	if r.Body == nil {
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil || (len(strings.TrimSpace(string(raw))) > 0 && json.Unmarshal(raw, dst) != nil) {
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid demo recording request")
		return false
	}
	return true
}

// PostBotDemoStartHandler POST /api/v1/bots/{id}/demo/start
func PostBotDemoStartHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		in := struct {
			Title string `json:"title"`
		}{}
		if !demoRecordBody(w, r, botMessageBodyLimit, &in) {
			return
		}
		out, err := svc.DemoRecordStart(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Title)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		out["asr_available"] = mobileSpeechTranscribeAvailable()
		writeJSON(w, http.StatusOK, out)
	}
}

// PostBotDemoNarrateHandler POST /api/v1/bots/{id}/demo/narrate
func PostBotDemoNarrateHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		in := struct {
			Text        string `json:"text"`
			AudioBase64 string `json:"audio_base64"`
			Mime        string `json:"mime"`
		}{}
		if !demoRecordBody(w, r, demoNarrateBodyLimit, &in) {
			return
		}
		text := strings.TrimSpace(in.Text)
		source := "typed"
		if text == "" && strings.TrimSpace(in.AudioBase64) != "" {
			if !mobileSpeechTranscribeAvailable() {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "ASR_UNAVAILABLE", "message": "语音转文字现在不可用，请直接输入文字旁白。", "asr_available": false})
				return
			}
			audio, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.AudioBase64))
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "audio_base64 is not base64")
				return
			}
			text, err = mobileReviewedHostSpeechTranscriber{}.TranscribeSpeech(r.Context(), in.Mime, audio)
			if err != nil || strings.TrimSpace(text) == "" {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "ASR_FAILED", "message": "这段语音没有转出文字，请重说或直接输入文字旁白。", "asr_available": true})
				return
			}
			source = "speech"
		}
		out, err := svc.DemoRecordNarrate(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), text)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		out["text"] = strings.TrimSpace(text)
		out["source"] = source
		writeJSON(w, http.StatusOK, out)
	}
}

// PostBotDemoStopHandler POST /api/v1/bots/{id}/demo/stop
func PostBotDemoStopHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		out, err := svc.DemoRecordStop(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// PostBotDemoDistillHandler POST /api/v1/bots/{id}/demo/distill
// Body: {"timeline":{...},"notes":"..."} — usually the stop response's timeline.
func PostBotDemoDistillHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		in := struct {
			Timeline json.RawMessage `json:"timeline"`
			Notes    string          `json:"notes"`
		}{}
		if !demoRecordBody(w, r, demoNarrateBodyLimit, &in) {
			return
		}
		var tl demoskill.Timeline
		if len(in.Timeline) == 0 || json.Unmarshal(in.Timeline, &tl) != nil || tl.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "timeline is required")
			return
		}
		out, err := svc.DemoRecordDistill(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), tl, in.Notes)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

// GetBotDemoShotHandler GET /api/v1/bots/{id}/demo/shots/{demoID}/{ref}
func GetBotDemoShotHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		mime, data, err := svc.DemoRecordShot(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("demoID"), r.PathValue("ref"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mime": mime, "data": data})
	}
}

// PostBotDemoRedistillHandler POST /api/v1/bots/{id}/demo/redistill
// Body: {"skill_name":"...","notes":"..."}. bot id is only for auth scoping.
func PostBotDemoRedistillHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		in := struct {
			SkillName string `json:"skill_name"`
			Notes     string `json:"notes"`
		}{}
		if !demoRecordBody(w, r, botMessageBodyLimit, &in) {
			return
		}
		out, err := svc.DemoSkillRedistill(r.Context(), principal.TenantID, principal.UserID, in.SkillName, in.Notes)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// PutBotDemoSkillHandler PUT /api/v1/bots/{id}/demo/skills/{skillName}
func PutBotDemoSkillHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		in := struct {
			Content     string   `json:"content"`
			Description string   `json:"description"`
			Triggers    []string `json:"triggers"`
		}{}
		if !demoRecordBody(w, r, demoNarrateBodyLimit, &in) {
			return
		}
		out, err := svc.DemoSkillUpdate(r.Context(), principal.TenantID, principal.UserID, r.PathValue("skillName"), in.Content, in.Description, in.Triggers)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}
