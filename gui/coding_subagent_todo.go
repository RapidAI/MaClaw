package main

// coding_subagent_todo.go — Claude Code / Codex-style in-agent todo checklist.
//
// This is an *agent-internal* capability (not the multi-task workbench workflow):
// the coding / remote coding SubAgent decomposes a complex request into steps,
// executes them one-by-one, and checks items off via the todo_write tool.
// A lightweight UI checklist is driven by the same state.

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	codingAgentTodoToolName = "todo_write"

	codingAgentTodoPending    = "pending"
	codingAgentTodoInProgress = "in_progress"
	codingAgentTodoCompleted  = "completed"
	codingAgentTodoCancelled  = "cancelled"

	// Soft cap keeps UI + prompt compact (prompt asks for 2-8).
	codingAgentTodoMaxItems   = 12
	codingAgentTodoMaxContent = 120
)

// codingAgentTodoItem is one step in the agent-internal plan.
type codingAgentTodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed | cancelled
}

// codingAgentTodoState holds the live checklist for one SubAgent turn.
type codingAgentTodoState struct {
	mu       sync.Mutex
	items    []codingAgentTodoItem
	revision uint64
	version  uint64
}

type codingAgentTodoSnapshot struct {
	Revision uint64
	Version  uint64
	Items    []codingAgentTodoItem
}

// bindControlPlaneRevision advances the callback-local generation while
// preserving the checklist. Initializing revision one is not a replacement;
// a later different name-set invalidates the prior version so an old model
// payload cannot mutate a freshly rendered control-plane surface. Re-rendering
// the same names keeps the current tokens. It is a local fence, not a durable
// task revision: callers must not persist or derive it from request/runtime IDs.
func (s *codingAgentTodoState) bindControlPlaneRevision(revision uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.revision != 0 && s.revision != revision {
		s.version++
	}
	s.revision = revision
	s.mu.Unlock()
}

func (s *codingAgentTodoState) revisionVersion() (revision, version uint64) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision, s.version
}

func (s *codingAgentTodoState) controlPlaneSnapshot() codingAgentTodoSnapshot {
	if s == nil {
		return codingAgentTodoSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return codingAgentTodoSnapshot{Revision: s.revision, Version: s.version, Items: cloneCodingAgentTodoItems(s.items)}
}

func normalizeCodingAgentTodoStatus(raw string) string {
	s := strings.TrimSpace(raw)
	lower := strings.ToLower(s)
	switch lower {
	case codingAgentTodoInProgress, "running", "active", "doing", "wip", "working":
		return codingAgentTodoInProgress
	case codingAgentTodoCompleted, "done", "passed", "complete", "finished", "ok", "success":
		return codingAgentTodoCompleted
	case codingAgentTodoCancelled, "skipped", "cancel", "drop", "aborted":
		return codingAgentTodoCancelled
	case codingAgentTodoPending, "todo", "open", "new":
		return codingAgentTodoPending
	}
	// Chinese aliases (common with CN models / prompts).
	switch s {
	case "进行中", "处理中", "执行中", "正在做":
		return codingAgentTodoInProgress
	case "完成", "已完成", "做完", "勾选":
		return codingAgentTodoCompleted
	case "取消", "跳过", "放弃":
		return codingAgentTodoCancelled
	case "待办", "未开始", "待处理":
		return codingAgentTodoPending
	}
	return codingAgentTodoPending
}

func codingAgentTodoIDString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		// JSON numbers decode as float64.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case json.Number:
		return strings.TrimSpace(t.String())
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func truncateCodingAgentTodoContent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Collapse whitespace so checklist rows stay single-line in UI.
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= codingAgentTodoMaxContent {
		return s
	}
	runes := []rune(s)
	return string(runes[:codingAgentTodoMaxContent-1]) + "…"
}

func (s *codingAgentTodoState) snapshot() []codingAgentTodoItem {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneCodingAgentTodoItems(s.items)
}

func cloneCodingAgentTodoItems(in []codingAgentTodoItem) []codingAgentTodoItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]codingAgentTodoItem, len(in))
	copy(out, in)
	return out
}

// normalizeCodingAgentTodoItems cleans, assigns ids, dedupes (last wins), and caps length.
func normalizeCodingAgentTodoItems(items []codingAgentTodoItem) []codingAgentTodoItem {
	cleaned := make([]codingAgentTodoItem, 0, len(items))
	for _, it := range items {
		content := truncateCodingAgentTodoContent(it.Content)
		id := strings.TrimSpace(it.ID)
		if content == "" && id == "" {
			continue
		}
		if id == "" {
			// Sequential among kept items (not raw input index) so holes/skips
			// do not collide with later explicit numeric ids unexpectedly.
			id = strconv.Itoa(len(cleaned) + 1)
		}
		cleaned = append(cleaned, codingAgentTodoItem{
			ID:      id,
			Content: content,
			Status:  normalizeCodingAgentTodoStatus(it.Status),
		})
	}
	if len(cleaned) == 0 {
		return nil
	}
	// Dedupe by id (last wins) while preserving first-seen order of unique ids.
	order := make([]string, 0, len(cleaned))
	byID := make(map[string]codingAgentTodoItem, len(cleaned))
	for _, it := range cleaned {
		if _, seen := byID[it.ID]; !seen {
			order = append(order, it.ID)
		}
		byID[it.ID] = it
	}
	out := make([]codingAgentTodoItem, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	if len(out) > codingAgentTodoMaxItems {
		out = out[:codingAgentTodoMaxItems]
	}
	return out
}

func enforceSingleInProgress(items []codingAgentTodoItem) {
	lastIP := -1
	for i, it := range items {
		if it.Status == codingAgentTodoInProgress {
			lastIP = i
		}
	}
	if lastIP < 0 {
		return
	}
	for i := range items {
		if items[i].Status == codingAgentTodoInProgress && i != lastIP {
			items[i].Status = codingAgentTodoPending
		}
	}
}

// applyTodoWrite merges or replaces the checklist. Claude Code semantics:
// merge=true updates by id; merge=false replaces the whole list.
func (s *codingAgentTodoState) applyTodoWrite(items []codingAgentTodoItem, merge bool) []codingAgentTodoItem {
	applied, _ := s.applyTodoWriteCAS(items, merge, 0, 0, false)
	return applied
}

// applyTodoWriteCAS is the only mutation path used by model-request control
// plane calls. expected revision/version are checked while holding the state
// lock so a delayed merge/replace/clear cannot overwrite a newer checklist.
func (s *codingAgentTodoState) applyTodoWriteCAS(items []codingAgentTodoItem, merge bool, expectedRevision, expectedVersion uint64, requireExpected bool) ([]codingAgentTodoItem, bool) {
	if s == nil {
		return nil, false
	}
	cleaned := normalizeCodingAgentTodoItems(items)
	s.mu.Lock()
	defer s.mu.Unlock()
	if requireExpected && (s.revision != expectedRevision || s.version != expectedVersion) {
		return cloneCodingAgentTodoItems(s.items), false
	}
	if !merge || len(s.items) == 0 {
		for i := range cleaned {
			if cleaned[i].Content == "" {
				cleaned[i].Content = cleaned[i].ID
			}
		}
		s.items = cleaned
	} else {
		byID := make(map[string]int, len(s.items))
		for i, it := range s.items {
			byID[it.ID] = i
		}
		for _, it := range cleaned {
			if idx, ok := byID[it.ID]; ok {
				// Preserve title when caller only flips status.
				if it.Content == "" {
					it.Content = s.items[idx].Content
				}
				s.items[idx] = it
			} else {
				if it.Content == "" {
					it.Content = it.ID
				}
				if len(s.items) >= codingAgentTodoMaxItems {
					continue
				}
				byID[it.ID] = len(s.items)
				s.items = append(s.items, it)
			}
		}
	}
	enforceSingleInProgress(s.items)
	s.version++
	return cloneCodingAgentTodoItems(s.items), true
}

func codingAgentTodoProgress(items []codingAgentTodoItem) (done, total int, current string) {
	total = len(items)
	for _, it := range items {
		switch it.Status {
		case codingAgentTodoCompleted, codingAgentTodoCancelled:
			done++
		case codingAgentTodoInProgress:
			if current == "" {
				current = it.Content
			}
		}
	}
	return done, total, current
}

func formatCodingAgentTodoChecklist(items []codingAgentTodoItem) string {
	if len(items) == 0 {
		return "（清单为空）"
	}
	var b strings.Builder
	b.WriteString("执行步骤：\n")
	for i, it := range items {
		mark := "☐"
		switch it.Status {
		case codingAgentTodoCompleted:
			mark = "☑"
		case codingAgentTodoInProgress:
			mark = "…"
		case codingAgentTodoCancelled:
			mark = "–"
		}
		b.WriteString(fmt.Sprintf("%s %d. %s\n", mark, i+1, it.Content))
	}
	done, total, _ := codingAgentTodoProgress(items)
	b.WriteString(fmt.Sprintf("进度：%d/%d", done, total))
	return b.String()
}

// formatCodingAgentTodoProgressLine is a one-line status for onProgress (avoids
// flooding the activity stream with a multi-line checklist on every update).
func formatCodingAgentTodoProgressLine(items []codingAgentTodoItem) string {
	if len(items) == 0 {
		return "步骤清单已清空"
	}
	done, total, current := codingAgentTodoProgress(items)
	if current != "" {
		// "完成 d/t" avoids implying the current step number equals done.
		return fmt.Sprintf("完成 %d/%d · 进行中: %s", done, total, current)
	}
	if done >= total {
		return fmt.Sprintf("完成 %d/%d · 全部完成", done, total)
	}
	return fmt.Sprintf("完成 %d/%d", done, total)
}

// codingAgentTodoUnresolvedSummary lists incomplete steps for end-of-turn soft notes.
func codingAgentTodoUnresolvedSummary(items []codingAgentTodoItem) string {
	if len(items) == 0 {
		return ""
	}
	var open []string
	for i, it := range items {
		switch it.Status {
		case codingAgentTodoCompleted, codingAgentTodoCancelled:
			continue
		default:
			open = append(open, fmt.Sprintf("%d. %s (%s)", i+1, it.Content, it.Status))
		}
	}
	if len(open) == 0 {
		return ""
	}
	return "未勾选步骤：\n" + strings.Join(open, "\n")
}

// appendCodingAgentTodoTurnNote soft-appends checklist progress to the final
// turn summary (does not fail the task — agent-internal honesty signal only).
func appendCodingAgentTodoTurnNote(summary string, items []codingAgentTodoItem) string {
	if len(items) == 0 {
		return summary
	}
	done, total, _ := codingAgentTodoProgress(items)
	note := fmt.Sprintf("## 步骤清单\n进度：%d/%d", done, total)
	if unresolved := codingAgentTodoUnresolvedSummary(items); unresolved != "" {
		note = note + "\n" + unresolved
	} else {
		note = note + "\n全部完成"
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return note
	}
	if strings.Contains(summary, "## 步骤清单") {
		return summary
	}
	return summary + "\n\n" + note
}

func codingAgentTodosToStepStatuses(items []codingAgentTodoItem) []codingWorkbenchStepStatus {
	out := make([]codingWorkbenchStepStatus, 0, len(items))
	for i, it := range items {
		st := codingStepPending
		switch it.Status {
		case codingAgentTodoCompleted:
			st = codingStepPassed
		case codingAgentTodoInProgress:
			st = codingStepRunning
		case codingAgentTodoCancelled:
			st = codingStepSkipped
		}
		out = append(out, codingWorkbenchStepStatus{
			Index:  i + 1,
			Title:  it.Content,
			Status: st,
		})
	}
	return out
}

func buildCodingAgentTodoToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": codingAgentTodoToolName,
			"description": "维护本任务的执行步骤清单（Claude Code / Codex 风格）。" +
				"复杂需求应先拆成 2-8 个有序步骤再动手改代码；每完成一步立即勾选。" +
				"同一时间最多一个 in_progress。" +
				"merge=true 时按 id 合并更新（可只传 id+status）；merge=false 时整表替换。" +
				"模型请求必须回传本轮 definition 标明的 expected_revision 与 expected_version；版本不匹配会被拒绝。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todos": map[string]interface{}{
						"type": "array",
						"description": "步骤列表（也可用 tasks）。每项：id、content（或 title）、" +
							"status=pending|in_progress|completed|cancelled。状态更新可只传 id+status。",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								// type stays string for broad provider schema support;
								// runtime parse still accepts numeric ids.
								"id":      map[string]interface{}{"type": "string", "description": "稳定步骤 id（如 \"1\"；数字也会被接受）"},
								"content": map[string]interface{}{"type": "string", "description": "步骤描述（短句）；也可用 title"},
								"title":   map[string]interface{}{"type": "string", "description": "content 别名"},
								"status":  map[string]interface{}{"type": "string", "description": "pending | in_progress | completed | cancelled"},
							},
						},
					},
					"tasks": map[string]interface{}{
						"type":        "array",
						"description": "todos 的别名",
					},
					"merge": map[string]interface{}{
						"type":        "boolean",
						"description": "true=按 id 合并；false=整表替换。省略时：已有清单则 merge=true。",
					},
					"expected_revision": map[string]interface{}{
						"type":        "integer",
						"description": "本次请求的 callback-local control-plane revision（由 definition 给出）",
					},
					"expected_version": map[string]interface{}{
						"type":        "integer",
						"description": "本次请求的 checklist version（由 definition 给出；成功后递增）",
					},
				},
			},
		},
	}
}

// annotateCodingTodoDefinitionForControlPlane exposes the callback-local CAS
// values only in the rendered definition for this request. They are not
// aliases, grants, durable revisions, or values derived from model input.
func annotateCodingTodoDefinitionForControlPlane(tools []map[string]interface{}, revision, version uint64) []map[string]interface{} {
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if fn == nil || fn["name"] != codingAgentTodoToolName {
			continue
		}
		params, _ := fn["parameters"].(map[string]interface{})
		properties, _ := params["properties"].(map[string]interface{})
		if properties == nil {
			continue
		}
		if field, ok := properties["expected_revision"].(map[string]interface{}); ok {
			field["description"] = fmt.Sprintf("必须传回本次 request 的 control-plane revision：%d", revision)
		}
		if field, ok := properties["expected_version"].(map[string]interface{}); ok {
			field["description"] = fmt.Sprintf("必须传回本次 request 的 checklist version：%d", version)
		}
		break
	}
	return tools
}

// codingAgentTodoPromptSection is injected into local/remote coding system prompts.
const codingAgentTodoPromptSection = `
## 需求拆解与按步推进（todo_write）
对非琐碎任务，必须先用 todo_write 拆成 2-8 个有序步骤，再动手改代码：
1. 先规划：写入完整步骤，首项可标 in_progress，其余 pending。
2. 一项一项做：同一时间最多一个 in_progress。
3. 做完立刻勾：将该步改为 completed，并把下一步改为 in_progress（可 merge=true 只更新 id+status）。
4. 失败或取消：标 cancelled，并在摘要里说明原因。
5. 禁止空口报完成：未勾选的步骤不得在最终回复里声称已完成。
6. 若用户消息带 [Plan step Tn/N] 或写明只执行当前计划步：todo 只能拆解**当前这一步**的内部子工作，禁止把整份多步计划（T1…Tn）重新列成清单并在本回合全部做完；后续计划步由编排器另开任务执行。
琐碎单点修改（改一个 typo、跑一条命令）可不建清单。
`

const codingAgentTodoControlPlanePromptSection = `
## todo_write 并发令牌
在一次真实模型请求中，todo_write 的 definition 会提供 expected_revision 与 expected_version。
每次调用必须原样带回这两个值；成功结果会返回新的 version，下一次调用使用它。
不要从任务文本、路径、request/loop/runtime ID 猜测 token；版本不匹配时读取返回的 current 值后重新规划。
`

func parseCodingAgentTodoWriteArgs(argsJSON string) (items []codingAgentTodoItem, merge bool, mergeSet bool, expectedRevision, expectedVersion uint64, expectedSet bool, err error) {
	raw := strings.TrimSpace(argsJSON)
	if raw == "" || raw == "{}" || raw == "null" {
		return nil, false, false, 0, 0, false, fmt.Errorf("todo_write requires todos array")
	}

	// Prefer flexible map parse so id can be number and fields can use aliases.
	var generic map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &generic); err == nil && generic != nil {
		var revisionSet, versionSet bool
		if expectedRevision, revisionSet = codingAgentTodoUint64(generic["expected_revision"]); revisionSet {
			expectedVersion, versionSet = codingAgentTodoUint64(generic["expected_version"])
			expectedSet = versionSet
		}
		if v, ok := generic["merge"]; ok {
			mergeSet = true
			switch t := v.(type) {
			case bool:
				merge = t
			case string:
				merge = strings.EqualFold(strings.TrimSpace(t), "true") || strings.TrimSpace(t) == "1"
			}
		}
		items = codingAgentTodoItemsFromAny(generic["todos"])
		if len(items) == 0 {
			items = codingAgentTodoItemsFromAny(generic["tasks"])
		}
		if len(items) == 0 && !mergeSet {
			// Object without todos — try bare array path below.
		} else {
			return items, merge, mergeSet, expectedRevision, expectedVersion, expectedSet, nil
		}
	}

	// Bare array: [{"content":"..."}]
	if items = codingAgentTodoItemsFromJSONArray(raw); items != nil || strings.HasPrefix(raw, "[") {
		if items == nil && strings.HasPrefix(raw, "[") {
			return nil, false, false, 0, 0, false, fmt.Errorf("invalid todo_write array")
		}
		return items, false, false, 0, 0, false, nil
	}
	return nil, false, false, 0, 0, false, fmt.Errorf("invalid todo_write arguments: expected object with todos/tasks")
}

func codingAgentTodoUint64(value interface{}) (uint64, bool) {
	switch value := value.(type) {
	case float64:
		if value >= 0 && value == math.Trunc(value) && value <= float64(^uint64(0)) {
			return uint64(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(strings.TrimSpace(value.String()), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	case int:
		if value >= 0 {
			return uint64(value), true
		}
	case int64:
		if value >= 0 {
			return uint64(value), true
		}
	}
	return 0, false
}

func codingAgentTodoItemsFromJSONArray(raw string) []codingAgentTodoItem {
	var arr []interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return codingAgentTodoItemsFromAny(arr)
}

func codingAgentTodoItemsFromAny(v interface{}) []codingAgentTodoItem {
	arr, ok := v.([]interface{})
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]codingAgentTodoItem, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		id := codingAgentTodoIDString(m["id"])
		content := firstNonEmptyCodingAgentString(
			codingAgentTodoFieldString(m, "content"),
			codingAgentTodoFieldString(m, "title"),
			codingAgentTodoFieldString(m, "description"),
			codingAgentTodoFieldString(m, "text"),
			codingAgentTodoFieldString(m, "name"),
		)
		// Drop empty objects {} / null-ish rows so they don't count as steps
		// and cannot accidentally clear via replace of "valid-looking" empties.
		if id == "" && content == "" {
			continue
		}
		status := firstNonEmptyCodingAgentString(
			codingAgentTodoFieldString(m, "status"),
			codingAgentTodoFieldString(m, "state"),
		)
		out = append(out, codingAgentTodoItem{
			ID:      id,
			Content: content,
			Status:  status,
		})
	}
	return out
}

func codingAgentTodoFieldString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return codingAgentTodoIDString(v)
}

func firstNonEmptyCodingAgentString(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

// executeCodingAgentTodoWrite applies args to state and returns the checklist text.
// onProgress receives a one-line progress status; the full checklist is the tool result.
// onEmit pushes UI state (optional).
func executeCodingAgentTodoWrite(
	state *codingAgentTodoState,
	argsJSON string,
	onProgress func(string),
	onEmit func([]codingAgentTodoItem),
) (resultText string, outcome codingToolOutcome) {
	return executeCodingAgentTodoWriteForControlPlane(state, argsJSON, onProgress, onEmit, 0, false)
}

// executeCodingAgentTodoWriteForControlPlane applies a direct-host write when
// requireCurrentRevision is false, and a revision/version CAS model call when
// it is true. The latter is the only path used after a static request surface
// has been rendered.
func executeCodingAgentTodoWriteForControlPlane(
	state *codingAgentTodoState,
	argsJSON string,
	onProgress func(string),
	onEmit func([]codingAgentTodoItem),
	currentRevision uint64,
	requireCurrentRevision bool,
) (resultText string, outcome codingToolOutcome) {
	if state == nil {
		return "todo_write unavailable: no state", codingToolOutcomeFailed
	}
	items, merge, mergeSet, expectedRevision, expectedVersion, expectedSet, err := parseCodingAgentTodoWriteArgs(argsJSON)
	if err != nil {
		return err.Error(), codingToolOutcomeFailed
	}
	if requireCurrentRevision && !expectedSet {
		revision, version := state.revisionVersion()
		return fmt.Sprintf("control_plane_stale: todo_write requires expected_revision=%d and expected_version=%d; current revision=%d version=%d", currentRevision, version, revision, version), codingToolOutcomeFailed
	}

	// Default merge / clear rules.
	if !mergeSet {
		// Empty list without explicit merge=false is treated as error (avoid wipe).
		// Also covers [{"todos":[{}]}] after empty-object filtering.
		if len(items) == 0 {
			return "todo_write: empty todos — pass merge=false to clear, or provide steps with content/id", codingToolOutcomeFailed
		}
		// Prefer merge when a checklist already exists (status-only updates).
		merge = len(state.snapshot()) > 0
	} else if len(items) == 0 {
		if merge {
			return "todo_write: empty todos with merge=true is a no-op; pass merge=false to clear", codingToolOutcomeFailed
		}
		// explicit clear
	}

	applied, appliedOK := state.applyTodoWriteCAS(items, merge, expectedRevision, expectedVersion, requireCurrentRevision)
	if !appliedOK {
		revision, version := state.revisionVersion()
		return fmt.Sprintf("control_plane_stale: todo_write expected revision=%d version=%d; current revision=%d version=%d", expectedRevision, expectedVersion, revision, version), codingToolOutcomeFailed
	}
	revision, version := state.revisionVersion()
	checklist := formatCodingAgentTodoChecklist(applied)
	checklist += fmt.Sprintf("\ncontrol_plane_revision=%d control_plane_version=%d", revision, version)
	if onProgress != nil {
		onProgress(formatCodingAgentTodoProgressLine(applied))
	}
	if onEmit != nil {
		onEmit(applied)
	}
	return checklist, codingToolOutcomeSuccess
}

const codingAgentTodoPlanOwnedNote = "注：外层多步计划进度由编排器维护；上述 todo 仅表示本步内部子任务。" +
	"完成本计划步后请停止，勿继续后续 Tn。"

// annotateTodoChecklistForOrchestratedPlan appends a reminder when outer multi-step
// plan progress is orchestrator-owned, so models don't treat agent todo 5/5 as
// "whole plan done". Idempotent if the note is already present.
func annotateTodoChecklistForOrchestratedPlan(handler *IMMessageHandler, userID, checklist string) string {
	checklist = strings.TrimRight(checklist, "\n")
	if handler == nil || strings.TrimSpace(userID) == "" || checklist == "" {
		return checklist
	}
	if strings.Contains(checklist, "外层多步计划进度由编排器维护") {
		return checklist
	}
	if !stickyHasOrchestratedPlanSteps(handler.getStickyCodingWorkbenchMemory(userID)) {
		return checklist
	}
	return checklist + "\n\n" + codingAgentTodoPlanOwnedNote
}

// wrapTodoProgressForOrchestratedPlan softens onProgress lines so
// "完成 5/5 · 全部完成" is not read as whole multi-step plan completion.
func wrapTodoProgressForOrchestratedPlan(handler *IMMessageHandler, userID string, onProgress func(string)) func(string) {
	if onProgress == nil {
		return nil
	}
	userID = strings.TrimSpace(userID)
	return func(line string) {
		line = strings.TrimSpace(line)
		if line != "" && handler != nil && userID != "" &&
			stickyHasOrchestratedPlanSteps(handler.getStickyCodingWorkbenchMemory(userID)) {
			line = strings.Replace(line, " · 全部完成", " · 本步内部清单已勾完", 1)
			if !strings.Contains(line, "外层计划另计") {
				line = line + " · 外层计划另计"
			}
		}
		onProgress(line)
	}
}

// stickyPlanHasOpenSteps reports whether any plan step is still pending/running
// (not yet terminal: passed / failed / verify_failed / skipped).
func stickyPlanHasOpenSteps(steps []codingWorkbenchStepStatus) bool {
	for _, st := range steps {
		switch st.Status {
		case codingStepPending, codingStepRunning, "":
			return true
		}
	}
	return false
}

// stickyHasOrchestratedPlanSteps reports whether sticky memory holds an
// in-progress multi-step workbench plan owned by the pure-coding orchestrator.
// When true, agent-internal todo_write must not replace StepStatuses (that would
// let a RemoteCodingSubAgent mark T2–Tn "passed" during T1 and corrupt UI/state).
// Fully terminal plans (all passed/failed/skipped) no longer freeze the UI so
// free-form follow-up turns can use agent todos again.
func stickyHasOrchestratedPlanSteps(mem stickyCodingWorkbenchMemory) bool {
	if strings.TrimSpace(mem.ExecutionPlan) == "" {
		return false
	}
	if len(mem.StepStatuses) < codingWorkbenchPlanMinTasks {
		return false
	}
	return stickyPlanHasOpenSteps(mem.StepStatuses)
}

// publishCodingAgentTodosToUI mirrors agent todos into sticky step statuses + event
// so the pure-coding banner can show ☑/☐ live (local and remote).
// Uses debounced disk persist: mid-turn todo updates are frequent.
// No-op emit when the visible checklist is unchanged (reduces UI churn).
// When an in-progress orchestrated multi-step plan owns StepStatuses, todos stay
// agent-local (tool result / progress line only) and do not overwrite plan progress.
func publishCodingAgentTodosToUI(handler *IMMessageHandler, userID string, items []codingAgentTodoItem) {
	if handler == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	steps := codingAgentTodosToStepStatuses(items)
	// Single sticky snapshot for guard + equality (avoid double map lookup).
	cur := handler.getStickyCodingWorkbenchMemory(userID)
	if stickyHasOrchestratedPlanSteps(cur) {
		return
	}
	// Fast path: skip RMW + debounced disk schedule when nothing visible changed.
	// Agent loop is single-threaded per session; a tiny race is acceptable.
	if codingWorkbenchStepStatusesEqual(cur.StepStatuses, steps) {
		return
	}
	changed := false
	// Memory + debounce only (not full disk write each step).
	handler.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		// Re-check under lock: plan may have been armed between the outer read and here.
		if mem != nil && stickyHasOrchestratedPlanSteps(*mem) {
			return
		}
		// Re-check under lock in case another writer raced (defensive).
		if codingWorkbenchStepStatusesEqual(mem.StepStatuses, steps) {
			return
		}
		changed = true
		if len(steps) == 0 {
			mem.StepStatuses = nil
			return
		}
		mem.StepStatuses = append([]codingWorkbenchStepStatus(nil), steps...)
	})
	// Only emit when we actually mutated sticky state (avoid no-op UI churn).
	if changed {
		handler.emitCodingWorkbenchStepsUpdate(userID)
	}
}

func codingWorkbenchStepStatusesEqual(a, b []codingWorkbenchStepStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Index != b[i].Index || a[i].Status != b[i].Status || a[i].Title != b[i].Title {
			return false
		}
	}
	return true
}
