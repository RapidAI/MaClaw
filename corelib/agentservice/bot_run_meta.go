package agentservice

import "strings"

// botTaskMetaKey is the short user-facing line shown on a bot's task list.
// The run used to keep only correlation ids, so a live turn had no title.
const botTaskMetaKey = "bot_task"

const botTaskRunes = 160

// stampBotRunIdentity copies the desktop bot's id and a short task line onto
// the run. The user message already stores the full text; the run keeps the
// line the task list can show without loading that message.
func stampBotRunIdentity(run *Run, meta map[string]string, content string) {
	if run == nil {
		return
	}
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	for _, key := range []string{"bot_id", "bot_phase", "bot_origin", "bot_desktop", "schedule_task"} {
		if strings.TrimSpace(run.Metadata[key]) != "" {
			continue
		}
		if v := strings.TrimSpace(meta[key]); v != "" {
			run.Metadata[key] = v
		}
	}
	if strings.TrimSpace(run.Metadata[botTaskMetaKey]) != "" {
		return
	}
	task := ""
	if meta != nil {
		task = clipBotTask(meta[botTaskMetaKey])
	}
	if task == "" {
		task = clipBotTask(content)
	}
	if task != "" {
		run.Metadata[botTaskMetaKey] = task
	}
}

func clipBotTask(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || content == restartNoticeContinue {
		return ""
	}
	// A timer body is the quiet preface, then the user's action on the next line.
	if strings.HasPrefix(content, "这一轮是例行检查") {
		if i := strings.Index(content, "\n"); i >= 0 {
			content = strings.TrimSpace(content[i+1:])
		}
	}
	content = strings.Join(strings.Fields(content), " ")
	if content == "" || content == restartNoticeContinue {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= botTaskRunes {
		return content
	}
	return string(runes[:botTaskRunes]) + "…"
}
