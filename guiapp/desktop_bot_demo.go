package guiapp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Desktop demo skill recording (MACLAW_DEMO_SKILL, stage 4). The Hub owns
// the session; these bindings only proxy start/narrate/stop/distill/edit so
// the GUI panel can drive a demonstration on an isolated bot desktop.

// StartDesktopDemoRecording POST /api/v1/bots/{id}/demo/start
func (a *App) StartDesktopDemoRecording(botID, title string) map[string]interface{} {
	var out map[string]interface{}
	err := a.desktopBotCall(context.Background(), http.MethodPost,
		"/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/demo/start",
		map[string]string{"title": strings.TrimSpace(title)}, &out, 30*time.Second)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// NarrateDesktopDemoRecording POST .../demo/narrate — typed text, or audio_base64 when ASR is available.
func (a *App) NarrateDesktopDemoRecording(botID, text, audioBase64, mime string) map[string]interface{} {
	body := map[string]string{}
	if t := strings.TrimSpace(text); t != "" {
		body["text"] = t
	}
	if a64 := strings.TrimSpace(audioBase64); a64 != "" {
		body["audio_base64"] = a64
		if m := strings.TrimSpace(mime); m != "" {
			body["mime"] = m
		}
	}
	var out map[string]interface{}
	err := a.desktopBotCall(context.Background(), http.MethodPost,
		"/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/demo/narrate",
		body, &out, 120*time.Second)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// StopDesktopDemoRecording POST .../demo/stop
func (a *App) StopDesktopDemoRecording(botID string) map[string]interface{} {
	var out map[string]interface{}
	err := a.desktopBotCall(context.Background(), http.MethodPost,
		"/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/demo/stop",
		nil, &out, 60*time.Second)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// DistillDesktopDemoRecording POST .../demo/distill with the stop timeline.
func (a *App) DistillDesktopDemoRecording(botID string, timeline map[string]interface{}, notes string) map[string]interface{} {
	body := map[string]interface{}{"timeline": timeline, "notes": strings.TrimSpace(notes)}
	var out map[string]interface{}
	err := a.desktopBotCall(context.Background(), http.MethodPost,
		"/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/demo/distill",
		body, &out, 10*time.Minute)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// GetDesktopDemoShot GET .../demo/shots/{demoID}/{ref}
func (a *App) GetDesktopDemoShot(botID, demoID, ref string) map[string]interface{} {
	var out map[string]interface{}
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/demo/shots/" +
		url.PathEscape(strings.TrimSpace(demoID)) + "/" + url.PathEscape(strings.TrimSpace(ref))
	err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, 30*time.Second)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// RedistillDesktopDemoSkill POST .../demo/redistill
func (a *App) RedistillDesktopDemoSkill(botID, skillName, notes string) map[string]interface{} {
	var out map[string]interface{}
	err := a.desktopBotCall(context.Background(), http.MethodPost,
		"/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/demo/redistill",
		map[string]string{"skill_name": strings.TrimSpace(skillName), "notes": strings.TrimSpace(notes)},
		&out, 10*time.Minute)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}

// UpdateDesktopDemoSkill PUT .../demo/skills/{skillName}
func (a *App) UpdateDesktopDemoSkill(botID, skillName, content, description string, triggers []string) map[string]interface{} {
	body := map[string]interface{}{
		"content": content, "description": description, "triggers": triggers,
	}
	var out map[string]interface{}
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/demo/skills/" + url.PathEscape(strings.TrimSpace(skillName))
	err := a.desktopBotCall(context.Background(), http.MethodPut, path, body, &out, 60*time.Second)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return out
}
