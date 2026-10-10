package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// DesktopOutcomeDecision is the evidence gate for one execute-phase finish.
// Continue keeps the same user turn open. Message is shown to the model only.
// Replace, when the turn is not continuing, is the chat text in place of the
// unchecked draft.
type DesktopOutcomeDecision struct {
	Continue bool
	Message  string
	Replace  string
}

// DesktopOutcomeReviewer reviews a desktop bot's finished draft against the
// turn's tool results and the latest screenshot. The draft itself is not
// evidence: the worker that wrote it is the one that can claim a result the
// screen does not show.
type DesktopOutcomeReviewer interface {
	ReviewDesktopOutcome(userText, draft string, history []ConversationEntry, image *ToolModelImage) DesktopOutcomeDecision
}

const desktopOutcomeUnconfirmed = "这一步的结果还没有对照桌面确认。"

const desktopOutcomeJudgeSystem = "你只判断这一步的结果是否已经满足用户要求。只回复一个 JSON 对象 {\"ok\":true或false,\"gap\":\"...\"}。\n" +
	"ok 为 true 只在给出的工具结果或附上的桌面截图本身显示要求已经达成。\n" +
	"工具报错或画面与要求不符时 ok 为 false。桌面截图与桌面工具文字不一致时，以截图为准。ssh 的输出单独判断远程命令：输出写明已经达成时，没有桌面截图也可以 ok 为 true，桌面截图也不能把它判成未达成。要求出现在这台桌面的画面上时，没有当前截图则 ok 为 false。\n" +
	"gap 用一句中文说明还缺什么。不要写步骤，不要声称已经完成。"

// gateDesktopReply is the last rewrite before a reply leaves the loop.
// room is false when this exit cannot take another model round.
func gateDesktopReply(cb LoopCallbacks, userText, text string, history []ConversationEntry, shot *ToolModelImage, room bool) (next, nudge string, cont bool) {
	reviewer, ok := cb.(DesktopOutcomeReviewer)
	if !ok {
		return text, "", false
	}
	return ApplyDesktopOutcome(text, reviewer.ReviewDesktopOutcome(userText, text, history, shot), room)
}

// ApplyDesktopOutcome applies one review. room is false when the loop cannot
// take another model round. An unchecked draft does not become the reply.
func ApplyDesktopOutcome(content string, decision DesktopOutcomeDecision, room bool) (next, message string, cont bool) {
	if decision.Continue && strings.TrimSpace(decision.Message) != "" && room {
		return content, strings.TrimSpace(decision.Message), true
	}
	if gap := strings.TrimSpace(decision.Replace); gap != "" && (!decision.Continue || !room) {
		return gap, "", false
	}
	if decision.Continue && !room {
		return desktopOutcomeUnconfirmed, "", false
	}
	return content, "", false
}

// JudgeDesktopOutcome asks the same model, in a separate call, whether the
// evidence meets the request. The worker's draft is not part of that call.
func JudgeDesktopOutcome(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, userText, evidence string, image *ToolModelImage) (bool, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	user := desktopOutcomeUserContent(cfg, userText, evidence, image)
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": desktopOutcomeJudgeSystem},
		map[string]interface{}{"role": "user", "content": user},
	}
	resp, err := desktopOutcomeComplete(ctx, cfg, httpClient, messages)
	if err != nil {
		return false, "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return false, "", fmt.Errorf("desktop outcome check returned nothing")
	}
	return parseDesktopOutcomeVerdict(resp.Choices[0].Message.Content)
}

func desktopOutcomeComplete(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, messages []interface{}) (*llm.Response, error) {
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return doLLMRequestWithTools(ctx, cfg, messages, nil, ToolSurfaceInvocationPolicy{}, httpClient)
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return llm.DoAnthropicRequest(ctx, cfg, messages, nil, httpClient)
	}
	return llm.DoOpenAIRequestWithOptions(ctx, cfg, messages, nil, httpClient, llm.OpenAIChatRequestOptions{})
}

func desktopOutcomeUserContent(cfg corelib.MaclawLLMConfig, userText, evidence string, image *ToolModelImage) interface{} {
	text := "用户要求：\n" + clipDesktopOutcome(userText, 800) + "\n\n工具结果：\n"
	if strings.TrimSpace(evidence) == "" {
		text += "（没有桌面工具结果）"
	} else {
		text += evidence
	}
	if desktopOutcomeEvidenceHasSSH(evidence) {
		text += "\n\nssh 的输出判断远程命令。桌面截图不是那台机器的画面，不能用来否定 ssh 的输出。"
	}
	if image == nil || strings.TrimSpace(image.Base64) == "" {
		text += "\n\n没有当前桌面截图。更早的截图不能当作现在的画面。"
		return text
	}
	if !cfg.SupportsVision {
		text += "\n\n桌面截图已采集，但这次判断看不到图片，只能根据工具结果判断。工具结果没有写明的画面不能算已经达成。"
		return text
	}
	mime := strings.TrimSpace(image.MIME)
	if mime == "" {
		mime = "image/png"
	}
	atts := []MessageAttachment{{MimeType: mime, Data: image.Base64}}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return BuildAnthropicVisionContent(text, atts)
	}
	return BuildOpenAIVisionContent(text, atts)
}

// desktopOutcomeEvidenceHasSSH reports a tool record. A continuation line is
// indented, so a command that prints "ssh:" is not a second record.
func desktopOutcomeEvidenceHasSSH(evidence string) bool {
	for _, line := range strings.Split(evidence, "\n") {
		if strings.HasPrefix(line, "ssh: ") {
			return true
		}
	}
	return false
}

func parseDesktopOutcomeVerdict(raw string) (bool, string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return false, "", fmt.Errorf("desktop outcome check was not json")
	}
	var parsed struct {
		OK  bool   `json:"ok"`
		Gap string `json:"gap"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return false, "", err
	}
	return parsed.OK, cleanDesktopOutcomeGap(parsed.Gap), nil
}

func cleanDesktopOutcomeGap(gap string) string {
	gap = strings.TrimSpace(gap)
	gap = strings.ReplaceAll(gap, "\r", " ")
	gap = strings.ReplaceAll(gap, "\n", " ")
	return clipDesktopOutcome(gap, 120)
}

func clipDesktopOutcome(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}
