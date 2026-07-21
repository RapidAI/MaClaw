package main

// Local /startmenu support for LANXIN standalone mode.  The browser owns the
// editable welcome-page list; it sends a scrubbed snapshot here so IM never
// needs Hub (or browser localStorage) to run a saved shortcut.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

const localStartMenuStateTTL = 15 * time.Minute
const localStartMenuMaxDocumentBytes = 2 << 20
const localStartMenuMenuPreviewRunes = 160
const localStartMenuMaxValueRunes = 2_000
const localStartMenuMaxTaskRunes = 12_000

var localStartMenuFieldPattern = regexp.MustCompile(`\[([^\[\]]+)\]`)

type LocalStartMenuTemplate struct {
	Title     string                   `json:"title"`
	Body      string                   `json:"body"`
	AgentMode string                   `json:"agentMode,omitempty"`
	CodingEnv *localStartMenuCodingEnv `json:"codingEnv,omitempty"`
}

type localStartMenuCodingEnv struct {
	WorkingDir string                   `json:"workingDir,omitempty"`
	Remote     *localStartMenuRemoteEnv `json:"remote,omitempty"`
}

// Password is intentionally absent: it must never leave browser localStorage.
type localStartMenuRemoteEnv struct {
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	WorkDir string `json:"workDir,omitempty"`
}

type localStartMenuDocument struct {
	Templates []LocalStartMenuTemplate `json:"templates"`
}
type localStartMenuField struct {
	Name       string
	Hint       string
	Start, End int
	Required   bool
}
type localStartMenuState struct {
	Templates []LocalStartMenuTemplate
	Selected  int
	Fields    []localStartMenuField
	Values    []string
	Confirm   bool
	UpdatedAt time.Time
}
type localStartMenuResult struct {
	Handled                        bool
	Reply                          string
	Confirmed                      bool
	TaskText, TaskTitle, AgentMode string
	Env                            *localStartMenuCodingEnv
}

type localStartMenuService struct {
	app       *App
	mu        sync.Mutex
	templates []LocalStartMenuTemplate
	states    map[string]*localStartMenuState
	loaded    bool
}

func (a *App) localStartMenuService() *localStartMenuService {
	a.localStartMenuMu.Lock()
	defer a.localStartMenuMu.Unlock()
	if a.localStartMenu == nil {
		a.localStartMenu = &localStartMenuService{app: a, states: make(map[string]*localStartMenuState)}
	}
	return a.localStartMenu
}

func (s *localStartMenuService) documentPath() string {
	return filepath.Join(s.app.GetDataDir(), "local-startmenu-templates.json")
}
func (s *localStartMenuService) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	info, err := os.Stat(s.documentPath())
	if err != nil || info.IsDir() || info.Size() > localStartMenuMaxDocumentBytes {
		return
	}
	raw, err := os.ReadFile(s.documentPath())
	if err != nil {
		return
	}
	var doc localStartMenuDocument
	if json.Unmarshal(raw, &doc) == nil {
		s.templates = sanitizeLocalStartMenuTemplates(doc.Templates)
	}
}
func sanitizeLocalStartMenuTemplates(in []LocalStartMenuTemplate) []LocalStartMenuTemplate {
	out := make([]LocalStartMenuTemplate, 0, len(in))
	for _, input := range in {
		// The Wails decoder normally gives us fresh values, but callers and tests
		// can still reuse their input slice. Clone nested pointers before trimming
		// so the persisted IM snapshot is isolated from all caller-owned memory.
		t := cloneLocalStartMenuTemplate(input)
		t.Title = localStartMenuSingleLine(t.Title)
		if len([]rune(t.Title)) > 80 {
			t.Title = string([]rune(t.Title)[:80])
		}
		t.Body = strings.TrimSpace(t.Body)
		if len([]rune(t.Body)) > 8000 {
			t.Body = string([]rune(t.Body)[:8000])
		}
		if t.AgentMode != "coding_dev" && t.AgentMode != "remote_coding_dev" {
			t.AgentMode = ""
		}
		if t.CodingEnv != nil {
			t.CodingEnv.WorkingDir = strings.TrimSpace(t.CodingEnv.WorkingDir)
			if r := t.CodingEnv.Remote; r != nil {
				r.Host = strings.TrimSpace(r.Host)
				r.User = strings.TrimSpace(r.User)
				r.WorkDir = strings.TrimSpace(r.WorkDir)
				if r.Port <= 0 || r.Port > 65535 {
					r.Port = 22
				}
			}
		}
		if t.Title != "" && t.Body != "" {
			out = append(out, t)
		}
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func cloneLocalStartMenuTemplate(input LocalStartMenuTemplate) LocalStartMenuTemplate {
	output := input
	if input.CodingEnv == nil {
		return output
	}
	env := *input.CodingEnv
	output.CodingEnv = &env
	if input.CodingEnv.Remote != nil {
		remote := *input.CodingEnv.Remote
		env.Remote = &remote
	}
	return output
}

// UpdateLocalStartMenuTemplates is a Wails binding. It persists only non-secret
// fields, making the local IM menu survive a desktop restart.
func (a *App) UpdateLocalStartMenuTemplates(templates []LocalStartMenuTemplate) error {
	s := a.localStartMenuService()
	clean := sanitizeLocalStartMenuTemplates(templates)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep the disk update inside the same critical section as the in-memory
	// replacement. Without this, two quick frontend CRUD calls can write their
	// snapshots out of order and resurrect an already deleted template after a
	// restart. AtomicWriteJSON also prevents a partial document on interruption.
	if err := configfile.AtomicWriteJSON(s.documentPath(), localStartMenuDocument{Templates: clean}); err != nil {
		return err
	}
	s.templates = clean
	s.loaded = true
	return nil
}

func (s *localStartMenuService) handle(key, text string) localStartMenuResult {
	text = strings.TrimSpace(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	if strings.EqualFold(text, "/startmenu") {
		for k, v := range s.states {
			if v == nil || time.Since(v.UpdatedAt) > localStartMenuStateTTL {
				delete(s.states, k)
			}
		}
		if len(s.templates) == 0 {
			return localStartMenuResult{Handled: true, Reply: "常用任务\n\n尚未保存常用任务。请先在桌面端 AI 助手引导页的「快捷」区域，将任务保存为常用模板。"}
		}
		// A wizard must keep executing the exact saved version selected by the
		// user, even if the desktop page edits/reorders templates meanwhile.
		list := cloneLocalStartMenuTemplates(s.templates)
		s.states[key] = &localStartMenuState{Templates: list, Selected: -1, UpdatedAt: time.Now()}
		return localStartMenuResult{Handled: true, Reply: localStartMenuList(list)}
	}
	st := s.states[key]
	if st == nil {
		return localStartMenuResult{}
	}
	if time.Since(st.UpdatedAt) > localStartMenuStateTTL {
		delete(s.states, key)
		return localStartMenuResult{Handled: true, Reply: "快捷方式已超时，请重新输入 /startmenu 开始。"}
	}
	st.UpdatedAt = time.Now()
	if strings.EqualFold(text, "/cancel") || text == "/取消" {
		delete(s.states, key)
		return localStartMenuResult{Handled: true, Reply: "已退出任务快捷方式。"}
	}
	if strings.EqualFold(text, "/back") {
		st.Selected = -1
		st.Fields = nil
		st.Values = nil
		st.Confirm = false
		return localStartMenuResult{Handled: true, Reply: localStartMenuList(st.Templates)}
	}
	if st.Selected < 0 {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > len(st.Templates) {
			return localStartMenuResult{Handled: true, Reply: fmt.Sprintf("请输入 1 到 %d 的序号，或输入 /cancel 取消。", len(st.Templates))}
		}
		st.Selected = n - 1
		st.Fields = parseLocalStartMenuFields(st.Templates[st.Selected].Body)
		st.Values = make([]string, len(st.Fields))
		st.Confirm = false
		return localStartMenuResult{Handled: true, Reply: "已选择：" + st.Templates[st.Selected].Title + "\n\n" + localStartMenuFields(st)}
	}
	if strings.EqualFold(text, "/run") {
		if missing := localStartMenuMissing(st); len(missing) > 0 {
			return localStartMenuResult{Handled: true, Reply: localStartMenuFields(st) + "\n\n仍缺少必填项：" + strings.Join(missing, "、")}
		}
		st.Confirm = true
		return localStartMenuResult{Handled: true, Reply: localStartMenuConfirm(st)}
	}
	if strings.EqualFold(text, "/confirm") {
		if !st.Confirm {
			return localStartMenuResult{Handled: true, Reply: "请先输入 /run 查看最终参数，再输入 /confirm 启动。"}
		}
		if missing := localStartMenuMissing(st); len(missing) > 0 {
			st.Confirm = false
			return localStartMenuResult{Handled: true, Reply: "仍缺少必填项：" + strings.Join(missing, "、")}
		}
		task := fillLocalStartMenuTemplate(st.Templates[st.Selected].Body, st.Fields, st.Values)
		if task == "" {
			st.Confirm = false
			return localStartMenuResult{Handled: true, Reply: "任务内容为空，请输入 /back 选择其他快捷方式。"}
		}
		if len([]rune(task)) > localStartMenuMaxTaskRunes {
			st.Confirm = false
			return localStartMenuResult{Handled: true, Reply: fmt.Sprintf("任务内容过长（最多 %d 个字符），请缩短参数后重新输入 /run。", localStartMenuMaxTaskRunes)}
		}
		tpl := st.Templates[st.Selected]
		delete(s.states, key)
		return localStartMenuResult{Handled: true, Confirmed: true, TaskText: task, TaskTitle: tpl.Title, AgentMode: tpl.AgentMode, Env: tpl.CodingEnv}
	}
	// Do not silently reinterpret another slash command as a parameter value.
	// In particular, `/run <name>` and `/exec ...` are passthrough commands;
	// allowing them to leak through while a group wizard is active would make
	// the current confirmation context unclear. The user can explicitly leave
	// this short-lived wizard before issuing a separate system command.
	if strings.HasPrefix(text, "/") {
		return localStartMenuResult{Handled: true, Reply: "当前正在填写任务快捷方式。请输入 /cancel 退出后，再执行其他直通命令。"}
	}
	idx, val, ok := parseLocalStartMenuAssignment(text, st.Fields)
	if !ok {
		return localStartMenuResult{Handled: true, Reply: localStartMenuFields(st) + "\n\n输入“参数序号 值”或“参数名 值”修改；/run 确认启动。"}
	}
	if len([]rune(val)) > localStartMenuMaxValueRunes {
		return localStartMenuResult{Handled: true, Reply: fmt.Sprintf("参数值过长（最多 %d 个字符），请缩短后重试。", localStartMenuMaxValueRunes)}
	}
	// A prompt may reference the same named parameter more than once, e.g.
	// `项目：[项目名]` in both title and body. Treat it as one logical field so
	// filling by name or number never leaves a duplicate placeholder required.
	fieldName := strings.TrimSpace(st.Fields[idx].Name)
	fieldHint := strings.TrimSpace(st.Fields[idx].Hint)
	for i, field := range st.Fields {
		if strings.EqualFold(strings.TrimSpace(field.Name), fieldName) ||
			(fieldHint != "" && strings.EqualFold(strings.TrimSpace(field.Hint), fieldHint)) {
			st.Values[i] = val
		}
	}
	st.Confirm = false
	return localStartMenuResult{Handled: true, Reply: "已更新：" + st.Fields[idx].Name + " = " + val + "\n\n" + localStartMenuFields(st)}
}

func cloneLocalStartMenuTemplates(in []LocalStartMenuTemplate) []LocalStartMenuTemplate {
	out := make([]LocalStartMenuTemplate, len(in))
	for i, t := range in {
		out[i] = cloneLocalStartMenuTemplate(t)
	}
	return out
}

// active reports whether this exact conversation/user is currently in the
// local wizard. It is used only to relax the group @mention gate for the next
// wizard reply; ignore/allowlist/disabled policies remain enforced first.
func (s *localStartMenuService) active(key string) bool {
	if s == nil || strings.TrimSpace(key) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[key]
	if state == nil {
		return false
	}
	if time.Since(state.UpdatedAt) > localStartMenuStateTTL {
		delete(s.states, key)
		return false
	}
	return true
}
func localStartMenuList(ts []LocalStartMenuTemplate) string {
	var b strings.Builder
	b.WriteString("已保存的常用任务\n\n")
	for i, t := range ts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, t.Title)
		if first := strings.TrimSpace(strings.Split(t.Body, "\n")[0]); first != "" {
			fmt.Fprintf(&b, "   %s\n", localStartMenuPreview(first, localStartMenuMenuPreviewRunes))
		}
	}
	b.WriteString("\n请输入序号选择，或输入 /cancel 取消。\n仅展示已保存为常用模板的任务，不包含最近任务或场景推荐。")
	return b.String()
}

// localStartMenuSingleLine keeps an IM list item to one physical line. Titles
// originate in the desktop UI but are still persisted and later rendered into
// a chat message, so whitespace normalization prevents visual list injection.
func localStartMenuSingleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func localStartMenuPreview(value string, maxRunes int) string {
	value = localStartMenuSingleLine(value)
	if maxRunes <= 0 || len([]rune(value)) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes]) + "…"
}
func localStartMenuFields(st *localStartMenuState) string {
	if len(st.Fields) == 0 {
		return "此快捷方式没有可填写参数。\n\n输入 /run 开始确认，/back 返回列表，/cancel 取消。"
	}
	var b strings.Builder
	b.WriteString("当前参数\n")
	for i, f := range st.Fields {
		v := st.Values[i]
		if v == "" {
			v = "未填写"
		}
		req := ""
		if f.Required {
			req = "（必填）"
		}
		fmt.Fprintf(&b, "%d. %s：%s%s\n", i+1, f.Name, v, req)
	}
	b.WriteString("\n输入“参数序号 值”或“参数名 值”修改。\n输入 /run 开始确认，/back 返回列表，/cancel 取消。")
	return b.String()
}
func localStartMenuConfirm(st *localStartMenuState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "即将启动任务：%s\n\n", st.Templates[st.Selected].Title)
	if len(st.Fields) == 0 {
		b.WriteString("- 无需额外参数\n")
	}
	for i, f := range st.Fields {
		v := st.Values[i]
		if v == "" {
			v = "未填写"
		}
		fmt.Fprintf(&b, "- %s：%s\n", f.Name, v)
	}
	b.WriteString("\n输入 /confirm 启动，或继续修改参数。")
	return b.String()
}
func localStartMenuMissing(st *localStartMenuState) []string {
	var out []string
	for i, f := range st.Fields {
		if f.Required && strings.TrimSpace(st.Values[i]) == "" {
			out = append(out, f.Name)
		}
	}
	return out
}
func parseLocalStartMenuFields(t string) []localStartMenuField {
	var out []localStartMenuField
	for _, m := range localStartMenuFieldPattern.FindAllStringSubmatchIndex(t, -1) {
		hint := strings.TrimSpace(t[m[2]:m[3]])
		if hint == "" {
			continue
		}
		before := strings.TrimSpace(t[strings.LastIndex(t[:m[0]], "\n")+1 : m[0]])
		name := ""
		// Only explicit label separators define a parameter name. Plain prose
		// such as `为 [项目名] 生成…` must keep `项目名` as its display name.
		if strings.HasSuffix(before, "：") {
			name = strings.TrimSpace(strings.TrimSuffix(before, "："))
		} else if strings.HasSuffix(before, ":") {
			name = strings.TrimSpace(strings.TrimSuffix(before, ":"))
		}
		if name == "" {
			name = hint
		}
		out = append(out, localStartMenuField{Name: name, Hint: hint, Start: m[0], End: m[1], Required: !strings.Contains(hint, "可空") && !strings.Contains(strings.ToLower(hint), "optional")})
	}
	return out
}
func parseLocalStartMenuAssignment(text string, fs []localStartMenuField) (int, string, bool) {
	text = strings.TrimSpace(text)
	for i, f := range fs {
		n := strings.TrimSpace(f.Name)
		if n != "" && len(text) > len(n) && strings.EqualFold(text[:len(n)], n) {
			r := text[len(n):]
			if localStartMenuSeparator(r) {
				if v := strings.TrimSpace(strings.TrimLeft(r, " \t:：")); v != "" {
					return i, v, true
				}
			}
		}
		// The placeholder hint itself is a useful alias when a template uses
		// prose before it (e.g. `为 [项目名] 生成...`).
		hint := strings.TrimSpace(f.Hint)
		if hint != "" && hint != n && len(text) > len(hint) && strings.EqualFold(text[:len(hint)], hint) {
			r := text[len(hint):]
			if localStartMenuSeparator(r) {
				if v := strings.TrimSpace(strings.TrimLeft(r, " \t:：")); v != "" {
					return i, v, true
				}
			}
		}
	}
	p := strings.Fields(text)
	if len(p) < 2 {
		return 0, "", false
	}
	n, e := strconv.Atoi(p[0])
	if e != nil || n < 1 || n > len(fs) {
		return 0, "", false
	}
	return n - 1, strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, p[0]), " \t:：")), true
}
func localStartMenuSeparator(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r == ' ' || r == '\t' || r == ':' || r == '：'
}
func fillLocalStartMenuTemplate(t string, fs []localStartMenuField, vs []string) string {
	for i := len(fs) - 1; i >= 0; i-- {
		t = t[:fs[i].Start] + strings.TrimSpace(vs[i]) + t[fs[i].End:]
	}
	return strings.TrimSpace(t)
}

// startMenuTaskCreatedReply is shared by local gateways and Hub delivery so
// users receive the same accurate launch status regardless of IM route.
func startMenuTaskCreatedReply(remote bool) string {
	if remote {
		return "远程任务已创建，并已在 AI 助手中打开新的任务标签页。正在自动连接 SSH 并执行任务，无需手动操作；如自动连接失败，请在该标签页补充或修正连接信息。"
	}
	return "任务已创建，并已在 AI 助手中打开新的任务标签页。"
}

func (a *App) openLocalStartMenuTask(result localStartMenuResult, platform, target string, isGroup bool) error {
	title := strings.TrimSpace(result.TaskTitle)
	if title == "" {
		title = "任务"
	}
	mode := result.AgentMode
	var task ProjectSearchResult
	remote := mode == "remote_coding_dev"
	if remote {
		if result.Env == nil || result.Env.Remote == nil {
			return fmt.Errorf("远程开发环境信息不完整")
		}
		r := result.Env.Remote
		task = a.CreateRemoteCodingTask(title, r.Host, r.User, r.WorkDir, r.Port)
	} else if mode == "coding_dev" {
		wd := ""
		if result.Env != nil {
			wd = result.Env.WorkingDir
		}
		task = a.CreateTaskWithMode(title, wd, mode)
		if task.ProjectPath != "" {
			if err := a.PrepareLocalCodingEnvironment(task.ProjectPath, wd); err != nil {
				return err
			}
		}
	} else {
		task = a.CreateTask(title, "")
	}
	if task.ProjectPath == "" {
		return fmt.Errorf("创建任务失败")
	}
	host := ""
	if result.Env != nil && result.Env.Remote != nil {
		host = result.Env.Remote.Host
	}
	// Remote prompts stay in frontend tab state until SSH reconnect succeeds;
	// local tasks still dispatch immediately.
	a.emitEvent("im-startmenu-task-created", map[string]interface{}{"project_path": task.ProjectPath, "task_title": task.Name, "initial_message": result.TaskText, "auto_send": true, "prepare_mode": "new-agent", "agent_mode": mode, "remote_host": host, "remote_needs_reconnect": remote, "im_platform": platform, "im_target_uid": target, "im_is_group": isGroup})
	return nil
}
