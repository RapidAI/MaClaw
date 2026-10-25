package guiapp

import (
	"encoding/json"
	"log"
	"strings"
)

// LogFrontendDiagnostic lets narrowly-scoped frontend diagnostics reach the
// main ~/.maclaw log (and registration.log when tag=onboarding). Callers must
// avoid sending full user/assistant content.
func (a *App) LogFrontendDiagnostic(payload map[string]interface{}) {
	if len(payload) == 0 {
		return
	}
	// Performance marks are handled before the generic path so they feed the
	// startup timeline instead of only landing as log text. The frontend
	// reports these because only the renderer knows when a frame was actually
	// painted.
	if stage, _ := payload["stage"].(string); stage != "" && payload["tag"] == "startup-trace" {
		noteFrontendPerfStage(stage, payload)
	}
	encoded, err := json.Marshal(sanitizeFrontendDiagnosticPayload(payload))
	if err != nil {
		log.Printf("[frontend-diagnostic] marshal_error=%v", err)
		return
	}
	// Lines with "tag":"onboarding" are always persisted (see isRegistrationLogLine).
	log.Printf("[frontend-diagnostic] %s", string(encoded))
}

// noteFrontendPerfStage maps a renderer-reported stage onto the backend
// startup timeline. Unknown stages are ignored so the frontend can add detail
// without changing the backend.
func noteFrontendPerfStage(stage string, payload map[string]interface{}) {
	switch stage {
	case "first-paint":
		perfMarkFirstPaint()
	case "interactive":
		perfMarkInteractive()
	}
	if perfProbeVerboseEnabled() {
		if metric, ok := payload["metric"].(string); ok && metric != "" {
			log.Printf("[perf] frontend_metric stage=%s metric=%s value=%v",
				stage, metric, payload["value"])
		}
	}
}

func sanitizeFrontendDiagnosticPayload(payload map[string]interface{}) map[string]interface{} {
	safe := make(map[string]interface{}, len(payload))
	for key, value := range payload {
		if isFrontendDiagnosticContentKey(key) {
			continue
		}
		switch typed := value.(type) {
		case string:
			safe[key] = truncateFrontendDiagnosticString(typed, 240)
		default:
			safe[key] = value
		}
	}
	return safe
}

func isFrontendDiagnosticContentKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return true
	}
	if strings.Contains(normalized, "snippet") || strings.Contains(normalized, "content") || strings.Contains(normalized, "prompt") {
		return true
	}
	if normalized == "raw" || strings.HasPrefix(normalized, "raw") {
		return true
	}
	return normalized == "text" || strings.HasSuffix(normalized, "text")
}

func truncateFrontendDiagnosticString(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "...[truncated]"
}
