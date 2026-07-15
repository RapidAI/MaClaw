package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	workflowFormPhaseField       = "_workflow_phase"
	workflowFormUserIDField      = "_workflow_user_id"
	workflowFormWorkflowIDField  = "_workflow_id"
	workflowFormEventScopeField  = "_workflow_event_scope_id"
	workflowFormProjectPathField = "project_path"
	workflowFormSSHProfileField  = "ssh_profile"
	workflowFormRemoteWorkDir    = "remote_workdir"
	workflowFormRemoteHostField  = "remote_host"
	workflowFormRemoteUserField  = "remote_user"
	workflowFormRemotePortField  = "remote_port"
	workflowFormSSHPasswordField = "ssh_password"
	workflowFormSSHKeyPathField  = "ssh_key_path"
	workflowFormRemoteTaskPath   = "remote_task_path"
	workflowFormExecLocal        = "local"
	workflowFormExecRemote       = "remote"
	workflowFormSSHProfileNew    = "__new__"
)

// codingWorkflowRemoteCreds is session-only auth for coding workflow remote runs.
type codingWorkflowRemoteCreds struct {
	Password string
	KeyPath  string
	Host     string
	User     string
	Port     int
	WorkDir  string
	Profile  string
}

// emitWorkflowPhaseForm builds an AgentView form from the phase's InputSchema
// and emits it to the frontend via the standard AG UI lifecycle protocol.
// The form appears in the right-side task panel (AgentTaskPanel).
func (h *IMMessageHandler) emitWorkflowPhaseForm(userID string, workflowID string, schema *v2.PhaseInputSchemaSpec, phaseID string) {
	if h == nil || h.app == nil || schema == nil {
		return
	}
	if len(schema.Fields) == 0 && len(schema.Variants) == 0 {
		return
	}
	schema = localizeWorkflowPhaseInputSchema(schema, h.getWorkflowLang())

	view := buildWorkflowPhaseFormAgentView(userID, workflowID, h.app.getEventScopeID(userID), phaseID, schema)
	injectSSHProfileOptionsIntoAgentView(view, h.app.sshHostEntries())
	h.app.emitAgentView(view)
	log.Printf("[workflow-form] emitted AG UI form: phase=%s fields=%d variants=%d", phaseID, len(schema.Fields), len(schema.Variants))
}

func buildWorkflowPhaseFormAgentView(userID, workflowID, eventScopeID, phaseID string, schema *v2.PhaseInputSchemaSpec) map[string]interface{} {
	if schema == nil {
		return nil
	}
	fields := make([]map[string]interface{}, 0, len(schema.Fields)+4)
	for _, f := range schema.Fields {
		fields = append(fields, phaseInputFieldSpecToAgentViewField(f))
	}

	// Hidden fields carrying stable workflow routing. The agent loop clears
	// lastUserID after it finishes, so form submit must not depend on that
	// transient field.
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormPhaseField,
		"type":  "hidden",
		"value": phaseID,
	})
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormUserIDField,
		"type":  "hidden",
		"value": userID,
	})
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormWorkflowIDField,
		"type":  "hidden",
		"value": workflowID,
	})
	if strings.TrimSpace(eventScopeID) != "" {
		fields = append(fields, map[string]interface{}{
			"name":  workflowFormEventScopeField,
			"type":  "hidden",
			"value": eventScopeID,
		})
	}

	viewID := "workflow:form:" + phaseID
	view := map[string]interface{}{
		"type":        "form",
		"id":          viewID,
		"title":       schema.Title,
		"description": schema.Description,
		"fields":      fields,
		"submitLabel": avTr("Submit", "提交"),
		"meta": map[string]interface{}{
			"source":   "workflow.phase_form",
			"phase_id": phaseID,
		},
	}
	if len(schema.Variants) > 0 {
		variants := make([]map[string]interface{}, 0, len(schema.Variants))
		for _, v := range schema.Variants {
			variantFields := make([]map[string]interface{}, 0, len(v.Fields))
			for _, f := range v.Fields {
				variantFields = append(variantFields, phaseInputFieldSpecToAgentViewField(f))
			}
			variants = append(variants, map[string]interface{}{
				"id":     v.ID,
				"label":  v.Label,
				"fields": variantFields,
			})
		}
		view["variants"] = variants
	}
	return view
}

func phaseInputFieldSpecToAgentViewField(f v2.PhaseInputFieldSpec) map[string]interface{} {
	field := map[string]interface{}{
		"name":  f.Name,
		"label": f.Label,
		"type":  f.Type,
	}
	if f.Required {
		field["required"] = true
	}
	if f.Sensitive {
		field["sensitive"] = true
	}
	if f.Description != "" {
		field["description"] = f.Description
	}
	if f.Placeholder != "" {
		field["placeholder"] = f.Placeholder
	}
	if len(f.Options) > 0 {
		opts := make([]map[string]string, len(f.Options))
		for i, o := range f.Options {
			opts[i] = map[string]string{"label": o.Label, "value": o.Value}
		}
		field["options"] = opts
	}
	if f.Default != nil {
		field["value"] = f.Default
	}
	if f.Min != nil {
		field["min"] = *f.Min
	}
	if f.Max != nil {
		field["max"] = *f.Max
	}
	if f.MinLength != nil {
		field["minLength"] = *f.MinLength
	}
	if f.MaxLength != nil {
		field["maxLength"] = *f.MaxLength
	}
	if f.Pattern != "" {
		field["pattern"] = f.Pattern
	}
	return field
}

// sshHostProfileOptions builds select options from configured SSH hosts.
// Value is the host Label (fallback: host) so submit can resolve the profile.
// Always appends "新建连接" so users can enter host/user/password inline.
func sshHostProfileOptions(hosts []corelib.SSHHostEntry) []map[string]string {
	opts := make([]map[string]string, 0, len(hosts)+1)
	for _, h := range hosts {
		value := strings.TrimSpace(h.Label)
		if value == "" {
			value = strings.TrimSpace(h.Host)
		}
		if value == "" {
			continue
		}
		user := strings.TrimSpace(h.User)
		host := strings.TrimSpace(h.Host)
		port := h.Port
		if port <= 0 {
			port = 22
		}
		label := value
		if host != "" {
			if user != "" {
				label = fmt.Sprintf("%s (%s@%s:%d)", value, user, host, port)
			} else {
				label = fmt.Sprintf("%s (%s:%d)", value, host, port)
			}
		}
		opts = append(opts, map[string]string{"label": label, "value": value})
	}
	opts = append(opts, map[string]string{
		"label": "新建连接…",
		"value": workflowFormSSHProfileNew,
	})
	return opts
}

// injectSSHProfileOptionsIntoAgentView fills ssh_profile select options from
// configured SSH hosts. Mutates only the agent-view payload, not the template.
// Attaches visibleWhen:
//   - host/user/port/key → only for 新建连接
//   - password → whenever a profile is selected (optional override / new auth)
func injectSSHProfileOptionsIntoAgentView(view map[string]interface{}, hosts []corelib.SSHHostEntry) {
	if view == nil {
		return
	}
	opts := sshHostProfileOptions(hosts)
	newOnlyFields := map[string]bool{
		workflowFormRemoteHostField: true,
		workflowFormRemoteUserField: true,
		workflowFormRemotePortField: true,
		workflowFormSSHKeyPathField: true,
	}
	inject := func(fields []map[string]interface{}) {
		for _, field := range fields {
			name := fmt.Sprint(field["name"])
			if name == workflowFormSSHProfileField {
				field["options"] = opts
				field["type"] = "select"
			}
			if newOnlyFields[name] {
				field["visibleWhen"] = map[string]interface{}{
					"field":  workflowFormSSHProfileField,
					"equals": workflowFormSSHProfileNew,
				}
			}
			if name == workflowFormSSHPasswordField {
				// Show for any selected profile (including __new__) so users can
				// optionally override stored credentials for this session only.
				field["visibleWhen"] = map[string]interface{}{
					"field":    workflowFormSSHProfileField,
					"notEmpty": true,
				}
				// Never block submit when using a saved profile without typing password.
				field["required"] = false
			}
		}
	}
	if raw, ok := view["fields"].([]map[string]interface{}); ok {
		inject(raw)
	}
	if rawVariants, ok := view["variants"].([]map[string]interface{}); ok {
		for _, variant := range rawVariants {
			if fields, ok := variant["fields"].([]map[string]interface{}); ok {
				inject(fields)
			}
		}
	}
}

// resolveCodingWorkflowRemoteFormData expands ssh_profile / new-connection fields
// into non-secret remote meta. Sensitive values stay in data only long enough for
// the submit path to copy them into session-only codingWorkflowRemoteCreds.
func resolveCodingWorkflowRemoteFormData(hosts []corelib.SSHHostEntry, data map[string]interface{}) error {
	if data == nil {
		return nil
	}
	variant := strings.TrimSpace(fmt.Sprint(data["_agent_view_variant"]))
	if variant != workflowFormExecRemote {
		return nil
	}
	workDir := formDataTrimString(data, workflowFormRemoteWorkDir)
	if workDir == "" {
		return fmt.Errorf("远程工作目录不能为空")
	}
	profile := formDataTrimString(data, workflowFormSSHProfileField)
	if profile == "" {
		return fmt.Errorf("请选择 SSH 主机或「新建连接」")
	}

	password := formDataTrimString(data, workflowFormSSHPasswordField)
	keyPath := formDataTrimString(data, workflowFormSSHKeyPathField)
	host := formDataTrimString(data, workflowFormRemoteHostField)
	user := formDataTrimString(data, workflowFormRemoteUserField)
	port := formDataPort(data, workflowFormRemotePortField, 22)

	if profile != workflowFormSSHProfileNew {
		entry, ok := findSSHHostEntryByProfile(hosts, profile)
		if !ok {
			return fmt.Errorf("未找到 SSH 主机「%s」，请在 SSH 面板检查配置，或选「新建连接」", profile)
		}
		if host == "" {
			host = strings.TrimSpace(entry.Host)
		}
		if user == "" {
			user = strings.TrimSpace(entry.User)
		}
		if port <= 0 || formDataTrimString(data, workflowFormRemotePortField) == "" {
			if entry.Port > 0 {
				port = entry.Port
			} else {
				port = 22
			}
		}
		if keyPath == "" {
			keyPath = strings.TrimSpace(entry.KeyPath)
		}
		// Prefer session password from form; fall back to in-memory host password
		// when present (Hub vault injects; disk config usually empty).
		if password == "" {
			password = strings.TrimSpace(entry.Password)
		}
		if strings.TrimSpace(entry.Label) != "" {
			data[workflowFormSSHProfileField] = strings.TrimSpace(entry.Label)
		}
	}

	host = normalizeSSHHostInput(host)
	user = sanitizeTaskMetadataTagValue(user)
	workDir = sanitizeTaskMetadataTagValue(workDir)
	if host == "" || user == "" {
		return fmt.Errorf("远程主机和用户名不能为空（选择已有主机或填写新建连接信息）")
	}
	if port <= 0 || port >= 65536 {
		port = 22
	}
	// New connection must supply password or key. Profile may rely on SSH agent
	// or a configured key_path without re-entering secrets here.
	if profile == workflowFormSSHProfileNew && password == "" && keyPath == "" {
		return fmt.Errorf("新建连接请填写密码或私钥路径")
	}

	data[workflowFormRemoteHostField] = host
	data[workflowFormRemoteUserField] = user
	data[workflowFormRemotePortField] = port
	data[workflowFormRemoteWorkDir] = workDir
	if keyPath != "" {
		data[workflowFormSSHKeyPathField] = keyPath
	}
	// Keep password in data temporarily for session vault capture; scrub removes it later.
	if password != "" {
		data[workflowFormSSHPasswordField] = password
	}
	return nil
}

func formDataTrimString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func formDataPort(data map[string]interface{}, key string, def int) int {
	if data == nil {
		return def
	}
	v, ok := data[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		if n > 0 && n < 65536 {
			return n
		}
	case int64:
		if n > 0 && n < 65536 {
			return int(n)
		}
	case float64:
		if n > 0 && n < 65536 {
			return int(n)
		}
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return def
		}
		p, err := strconv.Atoi(s)
		if err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return def
}

func findSSHHostEntryByProfile(hosts []corelib.SSHHostEntry, profile string) (corelib.SSHHostEntry, bool) {
	profile = strings.TrimSpace(profile)
	if profile == "" || profile == workflowFormSSHProfileNew {
		return corelib.SSHHostEntry{}, false
	}
	// Prefer exact label match, then host match.
	for _, h := range hosts {
		if strings.TrimSpace(h.Label) == profile {
			return h, true
		}
	}
	for _, h := range hosts {
		if strings.TrimSpace(h.Host) == profile {
			return h, true
		}
	}
	return corelib.SSHHostEntry{}, false
}

func captureCodingWorkflowRemoteCreds(data map[string]interface{}) codingWorkflowRemoteCreds {
	port := formDataPort(data, workflowFormRemotePortField, 22)
	return codingWorkflowRemoteCreds{
		Password: formDataTrimString(data, workflowFormSSHPasswordField),
		KeyPath:  formDataTrimString(data, workflowFormSSHKeyPathField),
		Host:     formDataTrimString(data, workflowFormRemoteHostField),
		User:     formDataTrimString(data, workflowFormRemoteUserField),
		Port:     port,
		WorkDir:  formDataTrimString(data, workflowFormRemoteWorkDir),
		Profile:  formDataTrimString(data, workflowFormSSHProfileField),
	}
}

func (h *IMMessageHandler) storeCodingWorkflowRemoteCreds(userID string, creds codingWorkflowRemoteCreds) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if strings.TrimSpace(creds.Host) == "" || strings.TrimSpace(creds.User) == "" {
		return
	}
	if creds.Port <= 0 {
		creds.Port = 22
	}
	h.codingWorkflowRemoteCreds.Store(userID, creds)
}

func (h *IMMessageHandler) loadCodingWorkflowRemoteCreds(userID string) (codingWorkflowRemoteCreds, bool) {
	if h == nil {
		return codingWorkflowRemoteCreds{}, false
	}
	raw, ok := h.codingWorkflowRemoteCreds.Load(strings.TrimSpace(userID))
	if !ok {
		return codingWorkflowRemoteCreds{}, false
	}
	creds, ok := raw.(codingWorkflowRemoteCreds)
	return creds, ok
}

func (h *IMMessageHandler) clearCodingWorkflowRemoteCreds(userID string) {
	if h == nil {
		return
	}
	h.codingWorkflowRemoteCreds.Delete(strings.TrimSpace(userID))
}

// codingWorkflowRemoteEnvFromState reads non-secret remote execution target from
// any phase FormData (typically requirements).
func codingWorkflowRemoteEnvFromState(state *v2.WorkflowState) (host, user, workDir string, port int, ok bool) {
	if state == nil {
		return "", "", "", 0, false
	}
	for i := range state.Phases {
		fd := state.Phases[i].FormData
		if fd == nil {
			continue
		}
		variant := formDataTrimString(fd, "_agent_view_variant")
		if variant == workflowFormExecLocal {
			continue
		}
		// Require explicit remote marker when present.
		if variant != "" && variant != workflowFormExecRemote {
			continue
		}
		host = formDataTrimString(fd, workflowFormRemoteHostField)
		user = formDataTrimString(fd, workflowFormRemoteUserField)
		workDir = formDataTrimString(fd, workflowFormRemoteWorkDir)
		if workDir == "" {
			workDir = formDataTrimString(fd, "work_dir")
		}
		port = formDataPort(fd, workflowFormRemotePortField, 22)
		if host != "" && user != "" && workDir != "" && (variant == workflowFormExecRemote || formDataTrimString(fd, workflowFormSSHProfileField) != "") {
			return host, user, workDir, port, true
		}
	}
	return "", "", "", 0, false
}

func isCodingWorkflowRemoteExecution(state *v2.WorkflowState) bool {
	_, _, _, _, ok := codingWorkflowRemoteEnvFromState(state)
	return ok
}

// bindCodingWorkflowRemoteSticky mirrors non-secret remote coords into sticky
// coding memory so reopen/reconnect can reuse the same host without passwords.
func (h *IMMessageHandler) bindCodingWorkflowRemoteSticky(userID string, creds codingWorkflowRemoteCreds) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	if strings.TrimSpace(creds.Host) == "" || strings.TrimSpace(creds.User) == "" {
		return
	}
	port := creds.Port
	if port <= 0 {
		port = 22
	}
	workDir := strings.TrimSpace(creds.WorkDir)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if mem.Kind != "" && mem.Kind != "remote" {
			// Do not clobber an active local pure-coding sticky session.
			return
		}
		mem.Kind = "remote"
		mem.RemoteHost = strings.TrimSpace(creds.Host)
		mem.RemoteUser = strings.TrimSpace(creds.User)
		mem.RemotePort = port
		if workDir != "" {
			mem.RemoteWorkDir = workDir
			mem.RemoteProjectDir = workDir
		}
	})
}

// syncCodingWorkflowRemoteTask creates or updates a left-sidebar remote_coding_dev
// task with non-secret SSH meta tags. Stores remote_task_path into form data.
// Password is never written to tags or task.md.
func (h *IMMessageHandler) syncCodingWorkflowRemoteTask(userID string, data map[string]interface{}, state *v2.WorkflowState) {
	if h == nil || h.app == nil || data == nil {
		return
	}
	if formDataTrimString(data, "_agent_view_variant") != workflowFormExecRemote {
		return
	}
	host := formDataTrimString(data, workflowFormRemoteHostField)
	user := formDataTrimString(data, workflowFormRemoteUserField)
	workDir := formDataTrimString(data, workflowFormRemoteWorkDir)
	port := formDataPort(data, workflowFormRemotePortField, 22)
	if host == "" || user == "" || workDir == "" {
		return
	}
	if port <= 0 || port >= 65536 {
		port = 22
	}

	taskName := formDataTrimString(data, "project_name")
	if taskName == "" && state != nil {
		taskName = strings.TrimSpace(state.Summary)
	}
	if taskName == "" {
		taskName = "远程编程任务"
	}
	// Prefix so sidebar can distinguish workflow-linked remote tasks.
	if !strings.Contains(taskName, "远程") && !strings.Contains(strings.ToLower(taskName), "remote") {
		taskName = "远程 · " + taskName
	}

	existing := formDataTrimString(data, workflowFormRemoteTaskPath)
	// Prefer explicit path, then state, then sticky project path, then meta match.
	if existing == "" && state != nil {
		existing = remoteTaskPathFromWorkflowState(state)
	}
	if existing == "" {
		if mem := h.getStickyCodingWorkbenchMemory(userID); mem.Kind == "remote" {
			if p := normalizeProjectSessionPath(mem.ProjectPath); p != "" {
				// Only reuse sticky path when coords still match.
				th, tu, tw, _ := h.app.remoteCodingMetaFromTaskTags(p)
				if strings.EqualFold(normalizeSSHHostInput(th), normalizeSSHHostInput(host)) &&
					strings.EqualFold(sanitizeTaskMetadataTagValue(tu), sanitizeTaskMetadataTagValue(user)) &&
					normalizeRemoteWorkDirKey(tw) == normalizeRemoteWorkDirKey(workDir) {
					existing = p
				}
			}
		}
	}
	if existing == "" {
		if matched := h.app.FindRemoteCodingTaskByMeta(host, user, workDir); strings.TrimSpace(matched.ProjectPath) != "" {
			existing = normalizeProjectSessionPath(matched.ProjectPath)
			log.Printf("[workflow-remote-task] reusing existing task by meta path=%s", existing)
		}
	}

	if existing != "" {
		if err := h.app.UpdateRemoteCodingTaskMeta(existing, host, user, workDir, port); err != nil {
			log.Printf("[workflow-remote-task] update meta failed path=%s err=%v — will try create", existing, err)
		} else {
			data[workflowFormRemoteTaskPath] = normalizeProjectSessionPath(existing)
			h.bindCodingWorkflowRemoteTaskSticky(userID, existing, host, user, workDir, port)
			log.Printf("[workflow-remote-task] updated task path=%s host=%s@%s:%d", existing, user, host, port)
			return
		}
	}

	created := h.app.CreateRemoteCodingTask(taskName, host, user, workDir, port, taskSourceCodingWorkflowTag)
	if strings.TrimSpace(created.ProjectPath) == "" {
		log.Printf("[workflow-remote-task] CreateRemoteCodingTask returned empty path name=%s", taskName)
		return
	}
	taskPath := normalizeProjectSessionPath(created.ProjectPath)
	data[workflowFormRemoteTaskPath] = taskPath
	h.bindCodingWorkflowRemoteTaskSticky(userID, taskPath, host, user, workDir, port)
	log.Printf("[workflow-remote-task] created task path=%s host=%s@%s:%d workdir=%s", taskPath, user, host, port, workDir)
}

// bindCodingWorkflowRemoteTaskSticky links workflow session sticky memory to the
// local task artifact path (for hooks/reopen) while keeping remote SSH coords.
func (h *IMMessageHandler) bindCodingWorkflowRemoteTaskSticky(userID, taskPath, host, user, workDir string, port int) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	taskPath = normalizeProjectSessionPath(taskPath)
	if userID == "" || taskPath == "" {
		return
	}
	if port <= 0 {
		port = 22
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if mem.Kind != "" && mem.Kind != "remote" {
			return
		}
		mem.Kind = "remote"
		mem.ProjectPath = taskPath
		mem.RemoteHost = strings.TrimSpace(host)
		mem.RemoteUser = strings.TrimSpace(user)
		mem.RemotePort = port
		if wd := strings.TrimSpace(workDir); wd != "" {
			mem.RemoteWorkDir = wd
			mem.RemoteProjectDir = wd
		}
	})
	// Also seed the task-tab owner session so opening the task continues remote.
	taskOwner := projectSessionOwnerID(taskPath)
	if taskOwner != "" && taskOwner != userID {
		h.updateStickyCodingWorkbenchMemory(taskOwner, func(mem *stickyCodingWorkbenchMemory) {
			mem.Kind = "remote"
			mem.ProjectPath = taskPath
			mem.RemoteHost = strings.TrimSpace(host)
			mem.RemoteUser = strings.TrimSpace(user)
			mem.RemotePort = port
			if wd := strings.TrimSpace(workDir); wd != "" {
				mem.RemoteWorkDir = wd
				mem.RemoteProjectDir = wd
			}
		})
	}
}

// remoteTaskPathFromWorkflowState returns remote_task_path from form data if any.
func remoteTaskPathFromWorkflowState(state *v2.WorkflowState) string {
	if state == nil {
		return ""
	}
	for i := range state.Phases {
		if p := formDataTrimString(state.Phases[i].FormData, workflowFormRemoteTaskPath); p != "" {
			return normalizeProjectSessionPath(p)
		}
	}
	return ""
}

// handleWorkflowFormAgentViewSubmit processes the user's form submission from
// the AG UI task panel. It stores the form data via the v2 workflow engine.
// The next user message will trigger the agent loop with form data in the prompt.
func (a *App) handleWorkflowFormAgentViewSubmit(phaseID string, data map[string]interface{}, requestID string) *IMAgentResponse {
	phaseID = strings.TrimSpace(phaseID)
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return &IMAgentResponse{
			Text:           avTr("AI assistant not initialized.", "AI 助手尚未初始化。"),
			Error:          "missing hub client",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	handler := hubClient.ensureIMHandler()

	// Resolve the user ID from form hidden fields
	userID := workflowFormStringField(data, workflowFormUserIDField)
	if userID == "" && handler != nil {
		userID = handler.lastUserID
	}
	if scopeID := workflowFormStringField(data, workflowFormEventScopeField); scopeID != "" && userID != "" {
		a.sessionEventScopeIDs.Store(userID, scopeID)
	}

	// Route to v2 workflow engine
	if handler != nil && handler.isWorkflowV2Active(userID) {
		resp := handler.handleWorkflowV2FormSubmit(userID, phaseID, data, requestID)
		if resp != nil {
			resp.ResponseSource = imResponseSourceAgentViewSubmit.String()
			return resp
		}
		return &IMAgentResponse{
			Text:           "表单已提交，正在生成文档...",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	// Fallback: WorkflowEngine adapter (only instantiated in tests).
	if a.workflowEngine != nil && a.workflowEngine.HasActiveWorkflow(userID) {
		return a.handleWorkflowFormV1EngineSubmit(userID, phaseID, data)
	}

	return &IMAgentResponse{
		Text:           "当前没有活跃的工作流。",
		Error:          "no active workflow",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

// handleWorkflowFormV1EngineSubmit handles form submission when V2 machine is
// not available but WorkflowEngine adapter has an active workflow. It validates project_path,
// stores form data via engine.SubmitPhaseForm, and returns success or error.
func (a *App) handleWorkflowFormV1EngineSubmit(userID, phaseID string, data map[string]interface{}) *IMAgentResponse {
	engine := a.workflowEngine
	if engine == nil {
		return &IMAgentResponse{
			Text:           "当前没有活跃的工作流。",
			Error:          "no active workflow",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return &IMAgentResponse{
			Text:           "当前没有活跃的工作流。",
			Error:          "no active workflow",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	// Phase mismatch guard: if the form has a _workflow_phase field and it
	// doesn't match the current phase, reject.
	if submittedPhase := workflowFormStringField(data, workflowFormPhaseField); submittedPhase != "" {
		if submittedPhase != ws.CurrentPhase {
			return &IMAgentResponse{
				Text:           fmt.Sprintf("表单阶段不匹配 (submitted=%s, current=%s)", submittedPhase, ws.CurrentPhase),
				Error:          "phase mismatch: submitted " + submittedPhase + " != current " + ws.CurrentPhase,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
	}

	// Validate project_path before mutating state (local execution).
	if pp := workflowFormStringField(data, workflowFormProjectPathField); pp != "" {
		_, _, err := normalizeWorkflowProjectPath(pp)
		if err != nil {
			return &IMAgentResponse{
				Text:           fmt.Sprintf("项目路径无效: %v", err),
				Error:          err.Error(),
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
	}

	// Strip hidden routing fields from form data before storing.
	// Preserve _agent_view_variant for local/remote execution target.
	cleanData := make(map[string]interface{}, len(data))
	for k, v := range data {
		if k == workflowFormUserIDField || k == workflowFormWorkflowIDField || k == workflowFormPhaseField || k == workflowFormEventScopeField {
			continue
		}
		if k != "" && k[0] == '_' && k != "_agent_view_variant" {
			continue
		}
		cleanData[k] = v
	}
	if err := resolveCodingWorkflowRemoteFormData(a.sshHostEntries(), cleanData); err != nil {
		return &IMAgentResponse{
			Text:           err.Error(),
			Error:          err.Error(),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	if formDataTrimString(cleanData, "_agent_view_variant") == workflowFormExecRemote {
		if hub := a.ensureHubClient(); hub != nil {
			if handler := hub.ensureIMHandler(); handler != nil {
				creds := captureCodingWorkflowRemoteCreds(cleanData)
				handler.storeCodingWorkflowRemoteCreds(userID, creds)
				handler.bindCodingWorkflowRemoteSticky(userID, creds)
				delete(cleanData, workflowFormSSHPasswordField)
				var st *v2.WorkflowState
				if handler.isWorkflowV2Active(userID) {
					if wf := handler.getWorkflowV2(); wf != nil {
						st = wf.machine.GetActive(userID)
					}
				}
				handler.syncCodingWorkflowRemoteTask(userID, cleanData, st)
			} else {
				delete(cleanData, workflowFormSSHPasswordField)
			}
		} else {
			delete(cleanData, workflowFormSSHPasswordField)
		}
	}

	// Submit form data through the WorkflowEngine adapter.
	_, err := engine.SubmitPhaseForm(userID, cleanData)
	if err != nil {
		return &IMAgentResponse{
			Text:           "表单提交失败: " + err.Error(),
			Error:          err.Error(),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	log.Printf("[workflow-v1-form] form submitted: user=%s phase=%s fields=%d", userID, phaseID, len(cleanData))

	return &IMAgentResponse{
		Text:           "信息已收到！发送「继续」开始生成文档。",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

func resolveWorkflowFormUserID(handler *IMMessageHandler, engine *v2.WorkflowEngine, phaseID string, data map[string]interface{}) string {
	if userID := workflowFormStringField(data, workflowFormUserIDField); userID != "" {
		return userID
	}
	if engine != nil {
		if userID, ok := engine.ActiveWorkflowUserIDForPhase(phaseID); ok {
			return userID
		}
	}
	return ""
}

func workflowFormStringField(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func workflowFormLifecyclePayload(data map[string]interface{}) map[string]interface{} {
	return workflowFormLifecyclePayloadFor("", "", "", data)
}

func workflowFormLifecyclePayloadFor(workflowID, phaseID, userID string, data map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{}
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		workflowID = workflowFormStringField(data, workflowFormWorkflowIDField)
	}
	phaseID = strings.TrimSpace(phaseID)
	if phaseID == "" {
		phaseID = workflowFormStringField(data, workflowFormPhaseField)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = workflowFormStringField(data, workflowFormUserIDField)
	}
	if workflowID != "" {
		payload["workflow_id"] = workflowID
	}
	if phaseID != "" {
		payload["workflow_phase"] = phaseID
	}
	if userID != "" {
		payload["workflow_user_id"] = userID
	}
	return payload
}

func workflowFormLifecyclePayloadWithFallback(workflowID, phaseID, userID string, data map[string]interface{}) map[string]interface{} {
	payload := workflowFormLifecyclePayload(data)
	if _, ok := payload["workflow_id"]; !ok {
		if workflowID = strings.TrimSpace(workflowID); workflowID != "" {
			payload["workflow_id"] = workflowID
		}
	}
	if _, ok := payload["workflow_phase"]; !ok {
		if phaseID = strings.TrimSpace(phaseID); phaseID != "" {
			payload["workflow_phase"] = phaseID
		}
	}
	if _, ok := payload["workflow_user_id"]; !ok {
		if userID = strings.TrimSpace(userID); userID != "" {
			payload["workflow_user_id"] = userID
		}
	}
	return payload
}

func workflowFormMatchesActiveWorkflow(engine *v2.WorkflowEngine, userID, phaseID string, data map[string]interface{}) bool {
	if engine == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(phaseID) == "" {
		return false
	}
	phaseID = strings.TrimSpace(phaseID)
	if submittedPhaseID := workflowFormStringField(data, workflowFormPhaseField); submittedPhaseID != "" && submittedPhaseID != phaseID {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != phaseID {
		return false
	}
	if submittedWorkflowID := workflowFormStringField(data, workflowFormWorkflowIDField); submittedWorkflowID != "" && ws.ID != submittedWorkflowID {
		return false
	}
	return true
}

func workflowFormMatchesActiveWorkflowV2(machine *v2.StateMachine, userID, phaseID string, data map[string]interface{}) bool {
	if machine == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(phaseID) == "" {
		return false
	}
	phaseID = strings.TrimSpace(phaseID)
	if submittedPhaseID := workflowFormStringField(data, workflowFormPhaseField); submittedPhaseID != "" && submittedPhaseID != phaseID {
		return false
	}
	state := machine.GetActive(userID)
	if state == nil || state.UserID != userID {
		return false
	}
	if current := state.ActivePhase(); current == nil || current.ID != phaseID {
		return false
	}
	if submittedWorkflowID := workflowFormStringField(data, workflowFormWorkflowIDField); submittedWorkflowID != "" && state.ID != submittedWorkflowID {
		return false
	}
	return true
}

// buildIMFormGuidanceText generates a structured text prompt for IM channels
// (WeChat/Feishu/QQ) that cannot render AG UI forms. The text guides the user
// to provide information in a numbered format.
func buildIMFormGuidanceText(schema *v2.PhaseInputSchemaSpec) string {
	if schema == nil || len(schema.Fields) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n", schema.Title))
	if schema.Description != "" {
		sb.WriteString(schema.Description + "\n\n")
	}
	sb.WriteString(avTr("Please provide the following information in order. Fields marked with * are required.\n\n", "请按顺序提供以下信息。带 * 的字段为必填。\n\n"))
	for i, f := range schema.Fields {
		prefix := " "
		if f.Required {
			prefix = "*"
		}
		sb.WriteString(fmt.Sprintf("%s%d. %s", prefix, i+1, f.Label))
		if len(f.Options) > 0 {
			labels := make([]string, 0, len(f.Options))
			for _, o := range f.Options {
				labels = append(labels, o.Label)
			}
			sb.WriteString(avTr(" (choose: ", "（可选：") + strings.Join(labels, " / ") + avTr(")", "）"))
		}
		if f.Placeholder != "" {
			sb.WriteString(avTr(" - ", " - ") + f.Placeholder)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(avTr("\nReply by number, for example:\n1. My project\n2. Go\n3. Windows\n...", "\n请按编号回复，例如：\n1. 我的项目\n2. Go\n3. Windows\n..."))
	return sb.String()
}

// buildFormSubmissionSummary creates a brief text summary of the form data
// to use as the synthetic user message that triggers the agent loop.
func buildFormSubmissionSummary(data map[string]interface{}) string {
	if len(data) == 0 {
		return avTr("The user submitted the workflow form. Generate the phase output from the persisted form data.", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u3002\u8bf7\u57fa\u4e8e\u5df2\u4fdd\u5b58\u7684\u8868\u5355\u6570\u636e\u751f\u6210\u9636\u6bb5\u8f93\u51fa\u3002")
	}
	var parts []string
	for k, v := range data {
		valStr := fmt.Sprintf("%v", v)
		if valStr != "" && valStr != "[]" && valStr != "<nil>" {
			parts = append(parts, fmt.Sprintf("%s: %s", k, valStr))
		}
	}
	if len(parts) == 0 {
		return avTr("The user submitted the workflow form. Generate the phase output from the persisted form data.", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u3002\u8bf7\u57fa\u4e8e\u5df2\u4fdd\u5b58\u7684\u8868\u5355\u6570\u636e\u751f\u6210\u9636\u6bb5\u8f93\u51fa\u3002")
	}
	summary := strings.Join(parts, "; ")
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:200]) + "..."
	}
	return avTr("The user submitted workflow form data: ", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u6570\u636e\uff1a") + summary
}

// buildFormSubmissionEcho creates a formatted echo of user-submitted form data
// for display in the chat area. Uses field labels from InputSchema when available,
// falls back to field names. Fields are rendered in the order defined by the schema.
// Hidden fields and internal routing fields are excluded.
func buildFormSubmissionEcho(state *v2.WorkflowState, cleanData map[string]interface{}) string {
	if len(cleanData) == 0 {
		return "信息已收到"
	}

	// Collect schema metadata from the active phase.
	var orderedFields []v2.PhaseInputField
	optionLabelMap := make(map[string]map[string]string) // field_name → {value → label}
	if state != nil {
		phase := state.ActivePhase()
		if phase != nil && phase.InputSchema != nil {
			orderedFields = append(orderedFields, phase.InputSchema.Fields...)
			collectOptionLabels(phase.InputSchema.Fields, optionLabelMap)
			// Also collect from the active variant if present.
			if variantID, ok := cleanData["_agent_view_variant"]; ok && variantID != nil {
				vid := fmt.Sprint(variantID)
				for _, v := range phase.InputSchema.Variants {
					if v.ID == vid {
						orderedFields = append(orderedFields, v.Fields...)
						collectOptionLabels(v.Fields, optionLabelMap)
						break
					}
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("信息已收到\n")

	// Render fields in schema-defined order.
	rendered := make(map[string]bool, len(orderedFields))
	for _, f := range orderedFields {
		if f.Name == "" || f.Type == "hidden" {
			continue
		}
		v, exists := cleanData[f.Name]
		if !exists || v == nil {
			continue
		}
		valStr := formatFieldValue(f, v, optionLabelMap)
		if valStr == "" {
			continue
		}
		label := f.Label
		if label == "" {
			label = f.Name
		}
		sb.WriteString(fmt.Sprintf("- **%s**：%s\n", label, valStr))
		rendered[f.Name] = true
	}

	// Render any remaining data fields not in schema (edge case: dynamic fields).
	for k, v := range cleanData {
		if k == "" || k == "_agent_view_variant" || rendered[k] {
			continue
		}
		valStr := fmt.Sprintf("%v", v)
		if valStr == "" || valStr == "[]" || valStr == "<nil>" {
			continue
		}
		if len([]rune(valStr)) > 100 {
			valStr = string([]rune(valStr)[:97]) + "..."
		}
		sb.WriteString(fmt.Sprintf("- **%s**：%s\n", k, valStr))
	}

	return strings.TrimRight(sb.String(), "\n")
}

// formatFieldValue formats a single field value for display, considering its type.
func formatFieldValue(f v2.PhaseInputField, value interface{}, optionLabelMap map[string]map[string]string) string {
	valStr := fmt.Sprintf("%v", value)
	if valStr == "" || valStr == "[]" || valStr == "<nil>" {
		return ""
	}

	switch f.Type {
	case "boolean":
		if valStr == "true" {
			return "是"
		}
		return "否"
	case "file", "directory":
		// Show a meaningful tail of the path (last 80 chars) so context is preserved.
		if len([]rune(valStr)) > 80 {
			valStr = "..." + string([]rune(valStr)[len([]rune(valStr))-77:])
		}
		return valStr
	case "select":
		if optLabels, ok := optionLabelMap[f.Name]; ok {
			if resolved, found := optLabels[valStr]; found {
				return resolved
			}
		}
	case "multiselect":
		// Multiselect values are typically []interface{} from JSON.
		if slice, ok := value.([]interface{}); ok && len(slice) > 0 {
			optLabels := optionLabelMap[f.Name]
			labels := make([]string, 0, len(slice))
			for _, item := range slice {
				s := fmt.Sprint(item)
				if optLabels != nil {
					if resolved, found := optLabels[s]; found {
						s = resolved
					}
				}
				labels = append(labels, s)
			}
			return strings.Join(labels, "、")
		}
		// Fallback: if it's a plain string, try to resolve as single value.
		if optLabels, ok := optionLabelMap[f.Name]; ok {
			if resolved, found := optLabels[valStr]; found {
				return resolved
			}
		}
	case "textarea":
		// Multi-line: show first line + indicator if more lines exist.
		lines := strings.SplitN(valStr, "\n", 2)
		firstLine := strings.TrimSpace(lines[0])
		if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
			if len([]rune(firstLine)) > 80 {
				firstLine = string([]rune(firstLine)[:77]) + "..."
			}
			return firstLine + " (...)"
		}
		// Single line, fall through to truncation below.
		valStr = firstLine
	}

	// General truncation for long values.
	if len([]rune(valStr)) > 100 {
		valStr = string([]rune(valStr)[:97]) + "..."
	}
	return valStr
}

// collectOptionLabels populates optionLabelMap from a slice of fields.
func collectOptionLabels(fields []v2.PhaseInputField, optionLabelMap map[string]map[string]string) {
	for _, f := range fields {
		if len(f.Options) > 0 {
			m := make(map[string]string, len(f.Options))
			for _, opt := range f.Options {
				m[opt.Value] = opt.Label
			}
			optionLabelMap[f.Name] = m
		}
	}
}
