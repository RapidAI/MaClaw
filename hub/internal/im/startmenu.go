package im

import (
	"context"
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
)

const maxStartMenuDocumentBytes = 2 << 20

var startMenuFieldPattern = regexp.MustCompile(`\[([^\[\]]+)\]`)

// StartMenuTemplateStore reads the same cloud-synced custom templates that the
// desktop welcome page saves. IM deliberately uses this shared source instead
// of maintaining a second, divergent shortcut list.
type StartMenuTemplateStore struct {
	root      string
	userEmail func(context.Context, string, string) (string, error)
}

type startMenuTemplate struct {
	Title     string              `json:"title"`
	Body      string              `json:"body"`
	AgentMode string              `json:"agentMode,omitempty"`
	CodingEnv *startMenuCodingEnv `json:"codingEnv,omitempty"`
}

type startMenuCodingEnv struct {
	WorkingDir string              `json:"workingDir,omitempty"`
	Remote     *startMenuRemoteEnv `json:"remote,omitempty"`
}

type startMenuRemoteEnv struct {
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	WorkDir string `json:"workDir,omitempty"`
}

type startMenuDocument struct {
	Templates []startMenuTemplate `json:"templates"`
}

func NewStartMenuTemplateStore(root string, userEmail func(context.Context, string, string) (string, error)) *StartMenuTemplateStore {
	return &StartMenuTemplateStore{root: strings.TrimSpace(root), userEmail: userEmail}
}

func (s *StartMenuTemplateStore) List(ctx context.Context, tenantID, userID string) ([]startMenuTemplate, error) {
	if s == nil || s.root == "" || s.userEmail == nil {
		return nil, fmt.Errorf("快捷方式服务未配置")
	}
	email, err := s.userEmail(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("未找到已绑定账号的邮箱")
	}
	key := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "..", "_").Replace(email)
	path := filepath.Join(s.root, "_email", key, "document.json")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取快捷方式失败: %w", err)
	}
	if info.IsDir() || info.Size() > maxStartMenuDocumentBytes {
		return nil, fmt.Errorf("快捷方式数据无效或过大")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取快捷方式失败: %w", err)
	}
	var doc startMenuDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("快捷方式数据无效: %w", err)
	}
	out := make([]startMenuTemplate, 0, len(doc.Templates))
	for _, item := range doc.Templates {
		item.Title = strings.TrimSpace(item.Title)
		item.Body = strings.TrimSpace(item.Body)
		item.AgentMode = strings.TrimSpace(item.AgentMode)
		if item.Title != "" && item.Body != "" {
			out = append(out, item)
		}
	}
	return out, nil
}

type startMenuField struct {
	Name     string
	Hint     string
	Start    int
	End      int
	Required bool
}

type startMenuState struct {
	Templates []startMenuTemplate
	Selected  int
	Fields    []startMenuField
	Values    []string
	Confirm   bool
	UpdatedAt time.Time
}

type startMenuResult struct {
	Handled         bool
	Response        *GenericResponse
	Launch          string
	LaunchConfirmed bool
	TaskText        string
	TaskTitle       string
	AgentMode       string
	CodingEnv       *startMenuCodingEnv
}

const startMenuStateTTL = 15 * time.Minute

type startMenuService struct {
	store  *StartMenuTemplateStore
	mu     sync.Mutex
	states map[string]*startMenuState
}

func newStartMenuService(store *StartMenuTemplateStore) *startMenuService {
	return &startMenuService{store: store, states: make(map[string]*startMenuState)}
}

// awaitingConfirmation reports whether this user's active shortcut wizard is
// ready for its final /confirm. It lets the adapter apply routing safeguards
// without treating unrelated IM /confirm commands as shortcut launches.
func (s *startMenuService) awaitingConfirmation(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[tenantUserRuntimeKey(tenantID, userID)]
	return state != nil && time.Since(state.UpdatedAt) <= startMenuStateTTL && state.Selected >= 0 && state.Confirm
}

func (s *startMenuService) handle(ctx context.Context, tenantID, userID, text string) startMenuResult {
	if s == nil {
		return startMenuResult{}
	}
	text = strings.TrimSpace(text)
	key := tenantUserRuntimeKey(tenantID, userID)
	if strings.EqualFold(text, "/startmenu") {
		s.mu.Lock()
		for stateKey, state := range s.states {
			if state == nil || time.Since(state.UpdatedAt) > startMenuStateTTL {
				delete(s.states, stateKey)
			}
		}
		s.mu.Unlock()
		if s.store == nil {
			return startMenuResult{Handled: true, Response: menuResponse(500, "快捷方式不可用", "任务快捷方式服务尚未配置。")}
		}
		templates, err := s.store.List(ctx, tenantID, userID)
		if err != nil {
			return startMenuResult{Handled: true, Response: menuResponse(500, "快捷方式不可用", err.Error())}
		}
		s.mu.Lock()
		if len(templates) == 0 {
			delete(s.states, key)
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(200, "任务快捷方式", "引导页尚未保存快捷方式。请先在桌面端将任务保存到「我的模板」，并上传到云端。")}
		}
		s.states[key] = &startMenuState{Templates: templates, Selected: -1, UpdatedAt: time.Now()}
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(200, "任务快捷方式", formatStartMenuList(templates))}
	}

	s.mu.Lock()
	state := s.states[key]
	if state == nil {
		s.mu.Unlock()
		return startMenuResult{}
	}
	if time.Since(state.UpdatedAt) > startMenuStateTTL {
		delete(s.states, key)
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(400, "快捷方式已超时", "本次快捷方式填写已超时，请重新输入 /startmenu 开始。")}
	}
	state.UpdatedAt = time.Now()
	if text == "/cancel" || text == "/取消" {
		delete(s.states, key)
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(200, "已取消", "已退出任务快捷方式。")}
	}
	if text == "/back" {
		state.Selected, state.Fields, state.Values, state.Confirm = -1, nil, nil, false
		templates := append([]startMenuTemplate(nil), state.Templates...)
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(200, "任务快捷方式", formatStartMenuList(templates))}
	}
	if state.Selected < 0 {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > len(state.Templates) {
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(400, "请选择任务", fmt.Sprintf("请输入 1 到 %d 的序号，或输入 /cancel 取消。", len(state.Templates)))}
		}
		state.Selected = n - 1
		state.Fields = parseStartMenuFields(state.Templates[state.Selected].Body)
		state.Values = make([]string, len(state.Fields))
		state.Confirm = false
		body := formatStartMenuFields(state)
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(200, "已选择："+state.Templates[n-1].Title, body)}
	}
	if text == "/run" {
		if missing := missingStartMenuFields(state); len(missing) > 0 {
			body := formatStartMenuFields(state) + "\n\n仍缺少必填项：" + strings.Join(missing, "、")
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(400, "请补全必填项", body)}
		}
		state.Confirm = true
		body := formatStartMenuConfirmation(state)
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(200, "确认启动任务", body)}
	}
	if text == "/confirm" {
		if !state.Confirm {
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(400, "请先确认", "请先输入 /run 查看最终参数，再输入 /confirm 启动。")}
		}
		if missing := missingStartMenuFields(state); len(missing) > 0 {
			state.Confirm = false
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(400, "请补全必填项", "仍缺少必填项："+strings.Join(missing, "、"))}
		}
		launch := fillStartMenuTemplate(state.Templates[state.Selected].Body, state.Fields, state.Values)
		if launch == "" {
			state.Confirm = false
			s.mu.Unlock()
			return startMenuResult{Handled: true, Response: menuResponse(400, "任务内容为空", "该快捷方式在填入参数后没有可执行内容。请返回修改模板，或输入 /back 选择其他快捷方式。")}
		}
		if env := startMenuEnvironmentInstruction(state.Templates[state.Selected]); env != "" {
			launch += "\n\n" + env
		}
		// The result will re-enter Adapter.HandleMessage. Prefix it so a template
		// beginning with `/` is still routed as a task, rather than accidentally
		// being consumed as an IM control command such as /cancel or /workflow.
		taskText := launch
		launch = "任务内容：\n" + taskText
		delete(s.states, key)
		s.mu.Unlock()
		return startMenuResult{
			Handled:         true,
			Launch:          launch,
			LaunchConfirmed: true,
			TaskText:        taskText,
			TaskTitle:       state.Templates[state.Selected].Title,
			AgentMode:       state.Templates[state.Selected].AgentMode,
			CodingEnv:       state.Templates[state.Selected].CodingEnv,
		}
	}

	idx, value, ok := parseStartMenuAssignment(text, state.Fields)
	if !ok {
		body := formatStartMenuFields(state) + "\n\n输入“参数序号 值”或“参数名 值”修改；/run 确认启动。"
		s.mu.Unlock()
		return startMenuResult{Handled: true, Response: menuResponse(400, "参数格式不正确", body)}
	}
	state.Values[idx] = value
	state.Confirm = false
	body := "已更新：" + state.Fields[idx].Name + " = " + displayStartMenuValue(value) + "\n\n" + formatStartMenuFields(state)
	s.mu.Unlock()
	return startMenuResult{Handled: true, Response: menuResponse(200, "参数已更新", body)}
}

func menuResponse(code int, title, body string) *GenericResponse {
	return &GenericResponse{StatusCode: code, StatusIcon: "info", Title: title, Body: body}
}

func formatStartMenuList(templates []startMenuTemplate) string {
	var b strings.Builder
	b.WriteString("已保存的任务快捷方式\n\n")
	for i, item := range templates {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item.Title)
		if first := strings.TrimSpace(strings.Split(item.Body, "\n")[0]); first != "" {
			fmt.Fprintf(&b, "   %s\n", first)
		}
	}
	b.WriteString("\n请输入序号选择，或输入 /cancel 取消。")
	return b.String()
}

func formatStartMenuFields(state *startMenuState) string {
	if len(state.Fields) == 0 {
		return "此快捷方式没有可填写参数。\n\n输入 /run 查看启动确认，/back 返回列表，/cancel 取消。"
	}
	var b strings.Builder
	b.WriteString("当前参数\n")
	for i, field := range state.Fields {
		mark := ""
		if field.Required {
			mark = "（必填）"
		}
		fmt.Fprintf(&b, "%d. %s：%s%s\n", i+1, field.Name, displayStartMenuValue(state.Values[i]), mark)
	}
	b.WriteString("\n输入“参数序号 值”或“参数名 值”修改。\n输入 /run 开始确认，/back 返回快捷方式列表，/cancel 取消。")
	return b.String()
}

func formatStartMenuConfirmation(state *startMenuState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "即将启动任务：%s\n\n", state.Templates[state.Selected].Title)
	if len(state.Fields) == 0 {
		b.WriteString("- 无需额外参数\n")
	}
	for i, field := range state.Fields {
		fmt.Fprintf(&b, "- %s：%s\n", field.Name, displayStartMenuValue(state.Values[i]))
	}
	b.WriteString("\n输入 /confirm 启动，或继续修改参数。")
	return b.String()
}

func displayStartMenuValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未填写"
	}
	return value
}

func missingStartMenuFields(state *startMenuState) []string {
	var out []string
	for i, field := range state.Fields {
		if field.Required && strings.TrimSpace(state.Values[i]) == "" {
			out = append(out, field.Name)
		}
	}
	return out
}

func parseStartMenuFields(template string) []startMenuField {
	var fields []startMenuField
	for _, match := range startMenuFieldPattern.FindAllStringSubmatchIndex(template, -1) {
		start, end := match[0], match[1]
		hint := strings.TrimSpace(template[match[2]:match[3]])
		if hint != "" {
			lineStart := strings.LastIndex(template[:start], "\n") + 1
			before := strings.TrimSpace(template[lineStart:start])
			name := ""
			if strings.HasSuffix(before, "：") {
				name = strings.TrimSpace(strings.TrimSuffix(before, "："))
			} else if strings.HasSuffix(before, ":") {
				name = strings.TrimSpace(strings.TrimSuffix(before, ":"))
			}
			if name == "" {
				name = hint
			}
			fields = append(fields, startMenuField{Name: name, Hint: hint, Start: start, End: end, Required: !strings.Contains(hint, "可空") && !strings.Contains(strings.ToLower(hint), "optional")})
		}
	}
	return fields
}

func parseStartMenuAssignment(text string, fields []startMenuField) (int, string, bool) {
	text = strings.TrimSpace(text)
	// Prefer the full field name before tokenizing. This supports labels that
	// contain spaces and the common Chinese `参数名：值` form.
	for i, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || len(text) <= len(name) || !strings.EqualFold(text[:len(name)], name) {
			continue
		}
		remainder := text[len(name):]
		if !startMenuAssignmentSeparator(remainder) {
			continue
		}
		value := strings.TrimLeft(remainder, " \t：:")
		if value != "" {
			return i, value, true
		}
	}
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return 0, "", false
	}
	key := parts[0]
	value := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, key), "：:"))
	if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(fields) {
		return n - 1, value, true
	}
	return 0, "", false
}

func startMenuAssignmentSeparator(remainder string) bool {
	if remainder == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(remainder)
	return first == ' ' || first == '\t' || first == ':' || first == '：'
}

func fillStartMenuTemplate(template string, fields []startMenuField, values []string) string {
	for i := len(fields) - 1; i >= 0; i-- {
		template = template[:fields[i].Start] + strings.TrimSpace(values[i]) + template[fields[i].End:]
	}
	return strings.TrimSpace(template)
}

func startMenuEnvironmentInstruction(template startMenuTemplate) string {
	if template.AgentMode != "coding_dev" && template.AgentMode != "remote_coding_dev" || template.CodingEnv == nil {
		return ""
	}
	if template.AgentMode == "coding_dev" && strings.TrimSpace(template.CodingEnv.WorkingDir) != "" {
		return "运行环境：本地开发目录=" + strings.TrimSpace(template.CodingEnv.WorkingDir)
	}
	remote := template.CodingEnv.Remote
	if template.AgentMode != "remote_coding_dev" || remote == nil || strings.TrimSpace(remote.Host) == "" || strings.TrimSpace(remote.User) == "" || strings.TrimSpace(remote.WorkDir) == "" {
		return ""
	}
	port := remote.Port
	if port <= 0 || port > 65535 {
		port = 22
	}
	return fmt.Sprintf("运行环境：远程开发主机=%s 端口=%d 用户=%s 工作目录=%s。请通过既有的安全凭据或交互式认证连接，绝不要求用户在 IM 中发送密码。", strings.TrimSpace(remote.Host), port, strings.TrimSpace(remote.User), strings.TrimSpace(remote.WorkDir))
}
