package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RecordAudioRequest is the structured payload for the record_audio tool.
// The agent loop detects the special marker and pauses until the user stops
// the interactive recording UI (waveform + pause/stop).
type RecordAudioRequest struct {
	Title   string `json:"title,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

const recordAudioMarkerPrefix = "__RECORD_AUDIO__"

// ToolRecordAudio opens an interactive long-form recording session and waits
// for the user to stop. Desktop host UI shows a waveform with pause/stop controls.
// IM hosts should reject this tool and use short native voice messages instead.
// After the user stops, the next user message carries the saved audio path and
// summary metadata so the agent can continue (transcription, minutes, or just
// deliver the file).
func ToolRecordAudio(args map[string]interface{}) string {
	title, _ := args["title"].(string)
	purpose, _ := args["purpose"].(string)
	hint, _ := args["hint"].(string)
	return FormatRecordAudioMarker(&RecordAudioRequest{
		Title:   strings.TrimSpace(title),
		Purpose: strings.TrimSpace(purpose),
		Hint:    strings.TrimSpace(hint),
	})
}

// FormatRecordAudioMarker encodes a RecordAudioRequest as the interactive tool
// result marker (used by hosts that re-enter handle paths after RunLoop pause).
func FormatRecordAudioMarker(req *RecordAudioRequest) string {
	if req == nil {
		req = &RecordAudioRequest{}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "录音"
	}
	payload := RecordAudioRequest{
		Title:   title,
		Purpose: strings.TrimSpace(req.Purpose),
		Hint:    strings.TrimSpace(req.Hint),
	}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("%s%s", recordAudioMarkerPrefix, string(data))
}

// IsRecordAudioResult checks if a tool result opens an interactive recording session.
func IsRecordAudioResult(result string) bool {
	return strings.HasPrefix(result, recordAudioMarkerPrefix)
}

// ParseRecordAudioResult extracts the RecordAudioRequest from a tool result.
func ParseRecordAudioResult(result string) (*RecordAudioRequest, bool) {
	if !IsRecordAudioResult(result) {
		return nil, false
	}
	jsonStr := strings.TrimPrefix(result, recordAudioMarkerPrefix)
	var req RecordAudioRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		return nil, false
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "录音"
	}
	return &req, true
}

// FormatRecordAudioForDisplay formats a recording request for channels that
// show a textual status while the desktop waveform card is open.
func FormatRecordAudioForDisplay(req *RecordAudioRequest) string {
	if req == nil {
		return "录音中…请使用录音卡片的暂停/停止结束录音。"
	}
	var b strings.Builder
	b.WriteString("🎙️ 录音中")
	if req.Title != "" {
		b.WriteString("：")
		b.WriteString(req.Title)
	}
	if req.Purpose != "" {
		b.WriteString("\n")
		b.WriteString(req.Purpose)
	}
	if req.Hint != "" {
		b.WriteString("\n")
		b.WriteString(req.Hint)
	}
	b.WriteString("\n\n请使用录音卡片的暂停/停止结束录音；录音期间输入区已锁定。")
	return b.String()
}
