package guiapp

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// worklogRecordKind is a query-shape projection for one external time-block
// work record. It selects a capability label. It does not name a tool, a
// server, or a schema, and it does not read provider descriptions.
type worklogRecordKind int

const (
	worklogRecordNone worklogRecordKind = iota
	worklogRecordRead
	worklogRecordUpdate
)

var worklogCompletionPercent = regexp.MustCompile(`\d+(?:\.\d+)?\s*[%％]`)

// semanticManagedPlanningText rewrites a bare continue onto the prior
// markdown-file write, and only then onto a prior work-record utterance.
// Markdown wins when it rewrote the text, so a weather or markdown continue
// is not pulled into a work record.
func semanticManagedPlanningText(userText string, history []agent.ConversationEntry) string {
	planned := markdownFileWritePlanningText(userText, history)
	inner := acpInnerUserRequest(userText)
	if planned != inner {
		return planned
	}
	if classifyWorklogRecord(planned) != worklogRecordNone {
		return planned
	}
	if !semanticBareContinueQuery(planned) {
		return planned
	}
	for i := len(history) - 1; i >= 0; i-- {
		if strings.ToLower(strings.TrimSpace(history[i].Role)) != "user" {
			continue
		}
		text := acpInnerUserRequest(markdownHistoryUserText(history[i].Content))
		if text == "" || semanticBareContinueQuery(text) {
			continue
		}
		if classifyWorklogRecord(text) != worklogRecordNone {
			return text
		}
		return planned
	}
	return planned
}

func classifyWorklogRecord(query string) worklogRecordKind {
	q := strings.ToLower(strings.TrimSpace(acpInnerUserRequest(query)))
	if q == "" || worklogHasLocalTodoMarker(q) {
		return worklogRecordNone
	}
	stripped := worklogWithoutRecordNouns(q)
	update := worklogHasUpdateVerb(stripped)
	if worklogHasRecordNoun(q) {
		if update {
			return worklogRecordUpdate
		}
		if worklogHasQueryVerb(stripped) {
			return worklogRecordRead
		}
		return worklogRecordNone
	}
	if worklogHasTimeBlock(q) && update && worklogHasCompletionStamp(q) {
		return worklogRecordUpdate
	}
	return worklogRecordNone
}

// worklogHasCompletionStamp requires the percent to be its own trailing
// clause. "，100%" is a work-record stamp. A percent followed by more words,
// or attached to a measure ("宽度 100%", "覆盖率100%"), is not.
func worklogHasCompletionStamp(q string) bool {
	loc := worklogCompletionPercent.FindAllStringIndex(q, -1)
	if len(loc) == 0 {
		return false
	}
	last := loc[len(loc)-1]
	rest := strings.TrimSpace(q[last[1]:])
	rest = strings.Trim(rest, "，。；、,.!！?？;:：\"'）)」】》")
	if strings.TrimSpace(rest) != "" {
		return false
	}
	i := last[0]
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(q[:i])
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if unicode.IsSpace(r) {
			i -= size
			continue
		}
		return strings.ContainsRune("，。；、,.!！?？;:：", r)
	}
	return false
}

func worklogHasLocalTodoMarker(q string) bool {
	for _, marker := range []string{"待办", "任务列表", "任务清单"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return strings.Contains(q, "todo list")
}

func worklogHasRecordNoun(q string) bool {
	for _, noun := range []string{"工作日志", "工时日志", "工时记录", "工时"} {
		if strings.Contains(q, noun) {
			return true
		}
	}
	return worklogHasASCIIToken(q, "worklog") || strings.Contains(q, "work log") || strings.Contains(q, "work record")
}

func worklogWithoutRecordNouns(q string) string {
	for _, noun := range []string{"工作日志", "工时日志", "工时记录", "work record", "work log", "worklog", "工时"} {
		q = strings.ReplaceAll(q, noun, " ")
	}
	return q
}

func worklogHasTimeBlock(q string) bool {
	for _, block := range []string{"上午", "下午", "晚上", "中午", "凌晨"} {
		if strings.Contains(q, block) {
			return true
		}
	}
	for _, block := range []string{"morning", "afternoon", "evening", "noon"} {
		if worklogHasASCIIToken(q, block) {
			return true
		}
	}
	return false
}

func worklogHasUpdateVerb(q string) bool {
	for _, verb := range []string{"添加", "补记", "修改", "更新", "追加", "记录", "补上"} {
		if strings.Contains(q, verb) {
			return true
		}
	}
	return worklogHasASCIIToken(q, "append") || worklogHasASCIIToken(q, "update")
}

func worklogHasQueryVerb(q string) bool {
	for _, verb := range []string{"查", "看看", "列出", "查询", "是什么"} {
		if strings.Contains(q, verb) {
			return true
		}
	}
	for _, verb := range []string{"show", "query", "read", "list"} {
		if worklogHasASCIIToken(q, verb) {
			return true
		}
	}
	return false
}

func worklogHasASCIIToken(text, token string) bool {
	text = strings.ToLower(text)
	token = strings.ToLower(token)
	if token == "" || len(token) > len(text) {
		return false
	}
	for i := 0; i+len(token) <= len(text); {
		rel := strings.Index(text[i:], token)
		if rel < 0 {
			return false
		}
		start := i + rel
		end := start + len(token)
		beforeOK := start == 0 || !worklogASCIIWord(text[start-1])
		afterOK := end == len(text) || !worklogASCIIWord(text[end])
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
	return false
}

func worklogASCIIWord(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// semanticWorklogPlanningClassification projects a work-record utterance onto
// its sealed label. A stored task_track, knowledge, or continuation verdict
// does not keep the local todo tool or a knowledge search on this turn.
func semanticWorklogPlanningClassification(result intent.ClassificationResult, kind worklogRecordKind) intent.ClassificationResult {
	if kind == worklogRecordNone {
		return result
	}
	out := semanticLookupClassificationForPlanning(result)
	switch kind {
	case worklogRecordRead:
		out.Primary = intent.LabelWorklogRead
	case worklogRecordUpdate:
		out.Primary = intent.LabelWorklogUpdate
	default:
		return result
	}
	kept := make([]intent.IntentLabel, 0, len(out.Secondary))
	for _, label := range out.Secondary {
		if semanticWorklogSuppressedLabel(label) {
			continue
		}
		kept = append(kept, label)
	}
	out.Secondary = kept
	return out
}

func semanticWorklogSuppressedLabel(label intent.IntentLabel) bool {
	switch label {
	case intent.LabelTaskTrack, intent.LabelKnowledgeRead, intent.LabelKnowledgeWrite, intent.LabelKnowledgeAdmin,
		intent.LabelBusinessData, intent.LabelContinuation, intent.LabelMemoryManage,
		intent.LabelWorklogRead, intent.LabelWorklogUpdate:
		return true
	default:
		return false
	}
}
