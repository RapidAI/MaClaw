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

const desktopDeliverablesMax = 6

const desktopDeliverablesSystem = "从用户这条要求里列出用户明确要求交给他、发给他、发布或展示给他的交付物（例如：某个文件、截图、链接、发布后的帖子、一段答案或数字）。" +
	"只列用户明确要的，不要推测，不要列步骤或中间产物。每项用一句不超过 40 字的中文，写清是什么、交到哪里。" +
	"用户只是让做一件事、没有要求交付任何东西时返回空列表。只回复 JSON：{\"items\":[\"...\"]}"

// ExtractDesktopDeliverables asks the model, in a separate call, for the
// explicit deliverables of a request. An error or empty list leaves the
// existing outcome check unchanged.
func ExtractDesktopDeliverables(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, userText string) ([]string, error) {
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
	resp, err := desktopOutcomeComplete(ctx, cfg, httpClient, messages)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("deliverables extraction returned nothing")
	}
	return ParseDesktopDeliverables(resp.Choices[0].Message.Content)
}

// ParseDesktopDeliverables reads {"items":[...]} from a model reply.
func ParseDesktopDeliverables(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("deliverables extraction was not json")
	}
	var parsed struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, item := range parsed.Items {
		item = clipDesktopOutcome(strings.Join(strings.Fields(item), " "), 80)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
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
