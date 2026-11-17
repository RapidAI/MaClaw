package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// A desktop run knows, from its start, what the person explicitly asked to
// receive, and may finish only when each item has reached the chat or the
// reply names what is missing and why. Bot 2 (papers 11-11 20:26-20:53) was
// asked for a running game's screenshot, produced it at step ~22, then spent
// ~30 steps re-verifying pixels before delivering; afterwards the outcome
// check still asked for a new screenshot of the delivered image. The items
// come from a model reading the request, never from keyword rules.

const desktopDeliverablesMax = 8

const desktopDeliverablesSystem = "从用户这条要求里列出用户明确要求交给他、发给他、发布或展示给他的交付物（例如：某个文件、截图、链接、发布后的帖子、一段答案或数字）。" +
	"用户打不开这台电脑上的文件：要求里要做出来给他看的文件或截图（即使只说保存、给出路径），也是交给他的交付物，写成发到聊天里。" +
	"用户写了成功标准、验收条件或数值目标时，每条可核对的标准也列一项，以「标准：」开头，结束前要逐条核对。" +
	"要求是训练、评估、基准测试、调参或其它实验时，另加一项「结果评估：对比基线或目标、检查结果是否可信（如验证集与测试集差距、类别不平衡、过拟合、抽样推理）、说明能否使用，并建议继续训练/调整数据或超参/停止及理由」，写进最后的报告。" +
	"若说明这一轮是定时器到点，周期和重复次数由定时器负责，不要列「每N分钟」「持续」这类标准。" +
	"只列用户明确要的，不要推测，不要列步骤或中间产物。每项用一句不超过 40 字的中文，写清是什么、交到哪里。" +
	"用户只是让做一件事、没有要求交付任何东西时返回空列表。只回复 JSON：" +
	"{\"items\":[{\"id\":\"d1\",\"kind\":\"file\",\"text\":\"要交给用户的文件\"},{\"id\":\"d2\",\"kind\":\"post\",\"text\":\"要发布的帖子\"},{\"id\":\"d3\",\"kind\":\"text\",\"text\":\"要写在回复里的答案\"}]}。" +
	"kind 只能是 file、post 或 text。id 用 d1、d2 这种稳定编号，同一份清单里不重复。"

// DesktopDeliverable is one required output read from the model. Kind is
// file, post, or text. ID is the model's stable id for this item. A legacy
// string item has an empty ID and Kind.
type DesktopDeliverable struct {
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind,omitempty"`
	Text string `json:"text,omitempty"`
}

// CompleteDesktopOutcome is the model call ExtractDesktopDeliverableContract
// uses. Tests replace it with a canned response.
var CompleteDesktopOutcome = desktopOutcomeComplete

// ExtractDesktopDeliverables asks the model, in a separate call, for the
// explicit deliverables of a request. An error or empty list leaves the
// existing outcome check unchanged. The returned strings are the item texts.
func ExtractDesktopDeliverables(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, userText string) ([]string, error) {
	items, err := ExtractDesktopDeliverableContract(ctx, cfg, httpClient, userText)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(item.Text); text != "" {
			out = append(out, text)
		}
	}
	return out, nil
}

// ExtractDesktopDeliverableContract is ExtractDesktopDeliverables with the
// parsed items. A parse failure is returned and must not be treated as an
// empty checklist.
func ExtractDesktopDeliverableContract(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, userText string) ([]DesktopDeliverable, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": desktopDeliverablesSystem},
		map[string]interface{}{"role": "user", "content": clipDesktopOutcome(userText, 1500)},
	}
	resp, err := CompleteDesktopOutcome(ctx, cfg, httpClient, messages)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("deliverables extraction returned nothing")
	}
	return ParseDesktopDeliverableContract(resp.Choices[0].Message.Content)
}

// ParseDesktopDeliverables reads {"items":[...]} from a model reply.
// A string element stays a legacy text. Object elements are read by
// ParseDesktopDeliverableContract; this helper returns only their text.
func ParseDesktopDeliverables(raw string) ([]string, error) {
	items, err := ParseDesktopDeliverableContract(raw)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(item.Text); text != "" {
			out = append(out, text)
		}
	}
	return out, nil
}

// ParseDesktopDeliverableContract reads the model checklist. A string array
// is the legacy contract: items have text and no id or kind. An object array
// requires kind file, post, or text. A missing id becomes d1, d2, … in order.
// Invalid JSON or an unknown kind is an error.
func ParseDesktopDeliverableContract(raw string) ([]DesktopDeliverable, error) {
	raw = strings.TrimSpace(raw)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("deliverables extraction was not json")
	}
	var parsed struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, err
	}
	if len(bytesTrim(parsed.Items)) == 0 || string(bytesTrim(parsed.Items)) == "null" {
		return nil, nil
	}
	var stringsOnly []string
	if err := json.Unmarshal(parsed.Items, &stringsOnly); err == nil {
		return legacyDeliverables(stringsOnly), nil
	}
	var objects []DesktopDeliverable
	if err := json.Unmarshal(parsed.Items, &objects); err != nil {
		return nil, fmt.Errorf("deliverables extraction was not a checklist")
	}
	return normalizeDeliverables(objects)
}

func bytesTrim(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

func legacyDeliverables(items []string) []DesktopDeliverable {
	var out []DesktopDeliverable
	seen := map[string]bool{}
	for _, item := range items {
		item = clipDesktopOutcome(strings.Join(strings.Fields(item), " "), 80)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, DesktopDeliverable{Text: item})
		if len(out) == desktopDeliverablesMax {
			break
		}
	}
	return out
}

func normalizeDeliverables(items []DesktopDeliverable) ([]DesktopDeliverable, error) {
	var out []DesktopDeliverable
	seen := map[string]bool{}
	for i, item := range items {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		switch kind {
		case "file", "post", "text":
		default:
			return nil, fmt.Errorf("deliverable kind %q is not file, post, or text", item.Kind)
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = "d" + strconv.Itoa(i+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("deliverable id %q is repeated", id)
		}
		seen[id] = true
		text := clipDesktopOutcome(strings.Join(strings.Fields(item.Text), " "), 80)
		out = append(out, DesktopDeliverable{ID: id, Kind: kind, Text: text})
		if len(out) == desktopDeliverablesMax {
			break
		}
	}
	return out, nil
}

func desktopDeliverablesList(items []string) string {
	var b strings.Builder
	for i, item := range items {
		b.WriteString(strconv.Itoa(i+1) + ". " + item + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// DesktopDeliverablesNote is the run-start checklist shown to the worker.
func DesktopDeliverablesNote(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "[系统] 用户这次明确要的交付物（结束前每一项都要交到用户聊天里：文件和图片用 deliver，链接和答案写进回复；做不到的项要在回复里点名说明原因）：\n" +
		desktopDeliverablesList(items) +
		"\n某项的证据已经有了（例如截图已经截好）就直接交付，不要反复核对同一件事。"
}

func desktopDeliverablesJudgeBlock(items []string) string {
	return "交付清单（从用户要求里提取的明确交付物）：\n" + desktopDeliverablesList(items) + "\n" +
		"逐项核对这份清单：每一项都要在工具结果里有交到用户聊天的证据——deliver 成功（Delivered …）、send_to_user，或发布后读回的链接和页面；答案类的项可以由回复正文给出。" +
		"要的是图片或文件时，工具结果显示它已经用 deliver 交付、且更早的工具结果说明了它是怎么得到的（例如在程序运行时截取），这一项就算已交付：不要再要求当前截图去核对这张图的内容。" +
		"清单每一项都有证据时 ok 为 true；否则 ok 为 false，gap 写出还缺哪几项。"
}

// JudgeDesktopOutcomeWithChecklist is JudgeDesktopOutcomeWithReply with the
// run's deliverables. With no items it is the same check.
func JudgeDesktopOutcomeWithChecklist(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, userText, reply, evidence string, image *ToolModelImage, items []string) (bool, string, error) {
	if len(items) == 0 {
		return JudgeDesktopOutcomeWithReply(ctx, cfg, httpClient, userText, reply, evidence, image)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": desktopOutcomeJudgeSystem},
		map[string]interface{}{"role": "user", "content": desktopOutcomeUserContentChecklist(cfg, userText, items, reply, evidence, image)},
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
