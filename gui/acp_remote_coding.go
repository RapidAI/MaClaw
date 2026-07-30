package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/acpagent"
)

// ACP (Mode B) extensions for VS Code remote coding attach.
// Methods are MaClaw-specific (`maclaw/*`) and stay thin: list/status/arm +
// read-only remote file preview. The agent brain remains sticky remote SubAgent.

// acpRemoteCodingTask is the compact task row returned to VS Code.
type acpRemoteCodingTask struct {
	Name        string `json:"name"`
	ProjectPath string `json:"project_path"`
	Host        string `json:"host"`
	User        string `json:"user"`
	Port        int    `json:"port"`
	WorkDir     string `json:"work_dir"`
	// Kind is always "remote" for this list; kept for forward compatibility.
	Kind           string `json:"kind"`
	Armed          bool   `json:"armed"`
	NeedsReconnect bool   `json:"needs_reconnect"`
	Message        string `json:"message,omitempty"`
	LastActivity   string `json:"last_activity,omitempty"`
}

// onMaclawCreateRemoteCodingTask creates the same persistent remote_coding_dev
// task record that the MaClaw GUI creates. SSH passwords are deliberately not
// part of this request: the VS Code client asks for one only when attaching.
func (s *acpHostSession) onMaclawCreateRemoteCodingTask(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		Name       string `json:"name"`
		SSHHost    string `json:"ssh_host"`
		SSHHostAlt string `json:"sshHost"`
		SSHUser    string `json:"ssh_user"`
		SSHUserAlt string `json:"sshUser"`
		WorkDir    string `json:"work_dir"`
		WorkDirAlt string `json:"workDir"`
		SSHPort    int    `json:"ssh_port"`
		SSHPortAlt int    `json:"sshPort"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
	}
	if params.SSHPort < 0 || params.SSHPortAlt < 0 {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_port must be between 1 and 65535")
	}
	name := strings.TrimSpace(params.Name)
	host := normalizeSSHHostInput(firstNonEmpty(params.SSHHost, params.SSHHostAlt))
	user := sanitizeTaskMetadataTagValue(firstNonEmpty(params.SSHUser, params.SSHUserAlt))
	workDir := sanitizeTaskMetadataTagValue(firstNonEmpty(params.WorkDir, params.WorkDirAlt))
	port := params.SSHPort
	if port <= 0 {
		port = params.SSHPortAlt
	}
	if name == "" || host == "" || user == "" || workDir == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "name, ssh_host, ssh_user, and work_dir are required")
	}
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_port must be between 1 and 65535")
	}
	// Atomically find or create the target record. A separate preflight lookup
	// would race another UI/client request and misreport a duplicate as new.
	remoteCodingTaskMu.Lock()
	task, reused := s.app.findOrCreateRemoteCodingTask(name, host, user, workDir, port)
	remoteCodingTaskMu.Unlock()
	if strings.TrimSpace(task.ProjectPath) == "" {
		return nil, acpErr(acpagent.CodeInternalError, "failed to create remote coding task")
	}
	return s.remoteCodingTaskResult(task.ProjectPath, task.Name, reused)
}

func (s *acpHostSession) remoteCodingTaskResult(projectPath, name string, reused bool) (any, *acpagent.RPCError) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, acpErr(acpagent.CodeInternalError, "remote coding task path is empty")
	}
	meta, err := s.app.GetRemoteCodingTaskMeta(projectPath)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, "failed to read remote coding task metadata: "+err.Error())
	}
	status := s.app.GetCodingWorkbenchStatus(projectPath)
	return map[string]any{
		"ok":     true,
		"reused": reused,
		"task": acpRemoteCodingTask{
			Name:           strings.TrimSpace(name),
			ProjectPath:    projectPath,
			Host:           strings.TrimSpace(meta.Host),
			User:           strings.TrimSpace(meta.User),
			Port:           meta.Port,
			WorkDir:        strings.TrimSpace(meta.WorkDir),
			Kind:           "remote",
			Armed:          status.Armed,
			NeedsReconnect: status.NeedsReconnect,
			Message:        strings.TrimSpace(status.Message),
		},
	}, nil
}

func (s *acpHostSession) onMaclawListRemoteCodingTasks(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		Limit int `json:"limit"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}
	// Tasks are ordered by recency, not by kind. A shallow over-fetch can hide a
	// valid remote task behind routine local tasks, which makes the VS Code picker
	// misleadingly appear empty. Scan a bounded broad window, then return only
	// the requested remote rows.
	tasks := s.app.ListTasks(1000)
	out := make([]acpRemoteCodingTask, 0, 16)
	for _, t := range tasks {
		if !projectRecordHasTagLike(t.Tags, taskRemoteCodingDevTag) {
			continue
		}
		meta, _ := s.app.GetRemoteCodingTaskMeta(t.ProjectPath)
		st := s.app.GetCodingWorkbenchStatus(t.ProjectPath)
		port := meta.Port
		if port <= 0 {
			port = 22
		}
		out = append(out, acpRemoteCodingTask{
			Name:           strings.TrimSpace(t.Name),
			ProjectPath:    strings.TrimSpace(t.ProjectPath),
			Host:           strings.TrimSpace(meta.Host),
			User:           strings.TrimSpace(meta.User),
			Port:           port,
			WorkDir:        strings.TrimSpace(meta.WorkDir),
			Kind:           "remote",
			Armed:          st.Armed,
			NeedsReconnect: st.NeedsReconnect,
			Message:        strings.TrimSpace(st.Message),
			LastActivity:   strings.TrimSpace(t.LastActivity),
		})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{"tasks": out}, nil
}

func (s *acpHostSession) onMaclawGetCodingWorkbenchStatus(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	path := strings.TrimSpace(params.ProjectPath)
	if path == "" {
		path = strings.TrimSpace(params.ProjectPathAlt)
	}
	if path == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path is required")
	}
	st := s.app.GetCodingWorkbenchStatus(path)
	meta, _ := s.app.GetRemoteCodingTaskMeta(path)
	return map[string]any{
		"status": st,
		"meta":   meta,
	}, nil
}

func (s *acpHostSession) onMaclawEnsureCodingWorkbenchArmed(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	path := strings.TrimSpace(params.ProjectPath)
	if path == "" {
		path = strings.TrimSpace(params.ProjectPathAlt)
	}
	if path == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path is required")
	}
	st, err := s.app.EnsureCodingWorkbenchArmed(path)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	meta, _ := s.app.GetRemoteCodingTaskMeta(path)
	return map[string]any{
		"status": st,
		"meta":   meta,
	}, nil
}

func (s *acpHostSession) onMaclawPrepareRemoteCoding(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
		SSHHost        string `json:"ssh_host"`
		SSHHostAlt     string `json:"sshHost"`
		SSHUser        string `json:"ssh_user"`
		SSHUserAlt     string `json:"sshUser"`
		SSHPassword    string `json:"ssh_password"`
		SSHPasswordAlt string `json:"sshPassword"`
		WorkDir        string `json:"work_dir"`
		WorkDirAlt     string `json:"workDir"`
		SSHPort        int    `json:"ssh_port"`
		SSHPortAlt     int    `json:"sshPort"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	if params.SSHPort < 0 || params.SSHPortAlt < 0 {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_port must be between 1 and 65535")
	}
	path := firstNonEmpty(params.ProjectPath, params.ProjectPathAlt)
	if path == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path is required")
	}
	// Prefer explicit params; fall back to stored task meta for host/user/workdir.
	meta, _ := s.app.GetRemoteCodingTaskMeta(path)
	host := normalizeSSHHostInput(firstNonEmpty(params.SSHHost, params.SSHHostAlt, meta.Host))
	user := sanitizeTaskMetadataTagValue(firstNonEmpty(params.SSHUser, params.SSHUserAlt, meta.User))
	workDir := sanitizeTaskMetadataTagValue(firstNonEmpty(params.WorkDir, params.WorkDirAlt, meta.WorkDir))
	password := firstNonEmpty(params.SSHPassword, params.SSHPasswordAlt)
	port := params.SSHPort
	if port <= 0 {
		port = params.SSHPortAlt
	}
	if port <= 0 {
		port = meta.Port
	}
	if port <= 0 {
		port = 22
	}
	if port > 65535 {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_port must be between 1 and 65535")
	}
	if password == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_password is required")
	}
	if host == "" || user == "" || workDir == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "ssh_host, ssh_user, and work_dir are required (or stored on the task)")
	}
	if err := s.app.PrepareRemoteCodingEnvironment(path, host, user, password, workDir, port); err != nil {
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	// Persist the coordinates only after a successful connection. This lets a
	// user correct an old host/work_dir from VS Code without saving unverified
	// connection details when SSH authentication fails.
	if err := s.app.UpdateRemoteCodingTaskMeta(path, host, user, workDir, port); err != nil {
		return nil, acpErr(acpagent.CodeInternalError, "remote coding connected but failed to save task metadata: "+err.Error())
	}
	st := s.app.GetCodingWorkbenchStatus(path)
	storedMeta, err := s.app.GetRemoteCodingTaskMeta(path)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, "remote coding connected but failed to read task metadata: "+err.Error())
	}
	// Never echo password.
	return map[string]any{
		"ok":      true,
		"status":  st,
		"meta":    storedMeta,
		"message": fmt.Sprintf("remote coding armed for %s@%s:%s", user, host, workDir),
	}, nil
}

// onMaclawReadRemoteFile reads a remote text file via the sticky SSH session
// for a remote_coding_dev task (VS Code virtual document preview).
func (s *acpHostSession) onMaclawReadRemoteFile(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
		Path           string `json:"path"`
		Offset         int    `json:"offset"`
		Limit          int    `json:"limit"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	projectPath := firstNonEmpty(params.ProjectPath, params.ProjectPathAlt)
	filePath := strings.TrimSpace(params.Path)
	if projectPath == "" || filePath == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path and path are required")
	}
	sessionID, workDir, err := s.app.acpRemoteSSHSession(projectPath)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	absPath := acpResolveRemotePath(filePath, workDir)
	if absPath == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "invalid path")
	}
	if !remotePathWithinDir(absPath, workDir) {
		return nil, acpErr(acpagent.CodeInvalidRequest, "path outside remote work_dir: "+workDir)
	}
	offset, limit := params.Offset, params.Limit
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	hub := s.app.ensureHubClient()
	if hub == nil {
		return nil, acpErr(acpagent.CodeInternalError, "AI assistant not initialized")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return nil, acpErr(acpagent.CodeInternalError, "message handler unavailable")
	}
	rawOut := handler.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      remoteReadFileRangePythonCommand(absPath, offset, limit),
		"wait_seconds": float64(20),
	})
	if remoteCodingToolOutcome(rawOut) != "success" {
		return nil, acpErr(acpagent.CodeInternalError, compactRemoteSSHError(rawOut))
	}
	content := extractRemoteReadPreviewContent(rawOut)
	truncated := strings.Contains(rawOut, "truncated") || strings.Contains(rawOut, "remote read_file truncated")
	// Cap payload size for ACP (~1.5MB runes is excessive; keep ~400k chars).
	if utf8.RuneCountInString(content) > 400000 {
		content = string([]rune(content)[:400000])
		truncated = true
	}
	return map[string]any{
		"ok":        true,
		"path":      absPath,
		"work_dir":  workDir,
		"content":   content,
		"offset":    offset,
		"limit":     limit,
		"truncated": truncated,
		"encoding":  "utf-8",
	}, nil
}

// onMaclawListRemoteDir lists a remote directory (ls -la style) for VS Code.
func (s *acpHostSession) onMaclawListRemoteDir(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
		Path           string `json:"path"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	projectPath := firstNonEmpty(params.ProjectPath, params.ProjectPathAlt)
	if projectPath == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path is required")
	}
	sessionID, workDir, err := s.app.acpRemoteSSHSession(projectPath)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	dirPath := strings.TrimSpace(params.Path)
	if dirPath == "" {
		dirPath = workDir
	}
	absPath := acpResolveRemotePath(dirPath, workDir)
	if !remotePathWithinDir(absPath, workDir) {
		return nil, acpErr(acpagent.CodeInvalidRequest, "path outside remote work_dir: "+workDir)
	}
	hub := s.app.ensureHubClient()
	if hub == nil {
		return nil, acpErr(acpagent.CodeInternalError, "AI assistant not initialized")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return nil, acpErr(acpagent.CodeInternalError, "message handler unavailable")
	}
	cmd := fmt.Sprintf("ls -la -- %s 2>&1; echo \"---EXIT_CODE:$?\"", remoteShellQuote(absPath))
	rawOut := handler.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      cmd,
		"wait_seconds": float64(15),
	})
	if remoteCodingToolOutcome(rawOut) != "success" {
		// Still return listing text when exit != 0 but has useful output.
		if strings.TrimSpace(rawOut) == "" {
			return nil, acpErr(acpagent.CodeInternalError, compactRemoteSSHError(rawOut))
		}
	}
	listing := acpStripSSHEnvelope(rawOut)
	return map[string]any{
		"ok":       true,
		"path":     absPath,
		"work_dir": workDir,
		"listing":  listing,
	}, nil
}

// acpRemoteSSHSession resolves sticky SSH session + workdir for a remote task.
func (a *App) acpRemoteSSHSession(projectPath string) (sessionID, workDir string, err error) {
	if a == nil {
		return "", "", fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return "", "", fmt.Errorf("project path is required")
	}
	a.ensureInteractionInfra()
	hub := a.ensureHubClient()
	if hub == nil {
		return "", "", fmt.Errorf("AI assistant not initialized")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return "", "", fmt.Errorf("message handler unavailable")
	}
	userID := projectSessionOwnerID(projectPath)
	mem := handler.getStickyCodingWorkbenchMemory(userID)
	meta, _ := a.GetRemoteCodingTaskMeta(projectPath)
	workDir = strings.TrimSpace(mem.RemoteWorkDir)
	if workDir == "" {
		workDir = strings.TrimSpace(mem.RemoteProjectDir)
	}
	if workDir == "" {
		workDir = strings.TrimSpace(meta.WorkDir)
	}
	if workDir == "" {
		return "", "", fmt.Errorf("remote work_dir unknown; attach/prepare remote coding first")
	}
	sessionID = strings.TrimSpace(mem.RemoteSessionID)
	if sessionID == "" || !handler.sshSessionAlive(sessionID) {
		return "", "", fmt.Errorf("SSH session not connected; re-attach remote coding with password")
	}
	return sessionID, workDir, nil
}

func acpResolveRemotePath(filePath, workDir string) string {
	filePath = strings.TrimSpace(filePath)
	workDir = strings.TrimSpace(workDir)
	if filePath == "" {
		return ""
	}
	if strings.HasPrefix(filePath, "/") {
		return remoteCleanPath(filePath)
	}
	if workDir == "" {
		return remoteCleanPath(filePath)
	}
	return remoteCleanPath(path.Join(workDir, filePath))
}

// onMaclawSearchRemote runs a bounded content search under remote work_dir.
// Prefers ripgrep when available, falls back to grep -R.
func (s *acpHostSession) onMaclawSearchRemote(raw json.RawMessage) (any, *acpagent.RPCError) {
	if s == nil || s.app == nil {
		return nil, acpErr(acpagent.CodeInternalError, "app unavailable")
	}
	var params struct {
		ProjectPath    string `json:"project_path"`
		ProjectPathAlt string `json:"projectPath"`
		Query          string `json:"query"`
		Path           string `json:"path"`
		MaxResults     int    `json:"max_results"`
		CaseSensitive  bool   `json:"case_sensitive"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	projectPath := firstNonEmpty(params.ProjectPath, params.ProjectPathAlt)
	query := strings.TrimSpace(params.Query)
	if projectPath == "" || query == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "project_path and query are required")
	}
	if utf8.RuneCountInString(query) > 200 {
		return nil, acpErr(acpagent.CodeInvalidParams, "query too long (max 200 runes)")
	}
	sessionID, workDir, err := s.app.acpRemoteSSHSession(projectPath)
	if err != nil {
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	scope := strings.TrimSpace(params.Path)
	if scope == "" {
		scope = workDir
	}
	absScope := acpResolveRemotePath(scope, workDir)
	if !remotePathWithinDir(absScope, workDir) {
		return nil, acpErr(acpagent.CodeInvalidRequest, "path outside remote work_dir: "+workDir)
	}
	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}
	hub := s.app.ensureHubClient()
	if hub == nil {
		return nil, acpErr(acpagent.CodeInternalError, "AI assistant not initialized")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return nil, acpErr(acpagent.CodeInternalError, "message handler unavailable")
	}

	// Shell-escape query for single-quoted remote command (no expansion).
	qEsc := strings.ReplaceAll(query, `'`, `'"'"'`)
	caseFlag := ""
	if !params.CaseSensitive {
		caseFlag = "i"
	}
	// Prefer rg; fall back to grep. Cap output lines server-side.
	cmd := fmt.Sprintf(
		`set +e; SCOPE=%s; Q='%s'; MAX=%d; `+
			`if command -v rg >/dev/null 2>&1; then `+
			`  rg -n%s --no-heading --color never -m "$MAX" -- "$Q" "$SCOPE" 2>/dev/null | head -n "$MAX"; `+
			`  EC=$?; `+
			`elif command -v grep >/dev/null 2>&1; then `+
			`  grep -R%sn --include='*.*' --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=build -- "$Q" "$SCOPE" 2>/dev/null | head -n "$MAX"; `+
			`  EC=$?; `+
			`else echo "neither rg nor grep found on remote"; EC=127; fi; `+
			`echo "---EXIT_CODE:$EC"`,
		remoteShellQuote(absScope),
		qEsc,
		maxResults,
		caseFlag,
		caseFlag,
	)
	rawOut := handler.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      cmd,
		"wait_seconds": float64(30),
	})
	// rg exits 1 when no matches — still OK.
	text := acpStripSearchOutput(rawOut)
	hits := acpParseSearchHits(text, workDir, maxResults)
	return map[string]any{
		"ok":        true,
		"query":     query,
		"path":      absScope,
		"work_dir":  workDir,
		"hits":      hits,
		"raw":       text,
		"hit_count": len(hits),
		"truncated": len(hits) >= maxResults,
	}, nil
}

type acpSearchHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Preview string `json:"preview"`
}

func acpStripSearchOutput(raw string) string {
	lines := strings.Split(raw, "\n")
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "---EXIT_CODE:") || strings.HasPrefix(line, "EXIT:") {
			break
		}
		// Skip SSH envelope noise.
		if strings.HasPrefix(line, "$ ") || strings.Contains(line, "base64 -d") ||
			strings.HasPrefix(line, "[ssh_") || strings.Contains(line, "状态:") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func acpParseSearchHits(text, workDir string, max int) []acpSearchHit {
	hits := make([]acpSearchHit, 0, 16)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// path:line:content  (rg / grep -n)
		// Windows-unfriendly but remote is posix.
		pathPart, rest, ok := strings.Cut(line, ":")
		if !ok || pathPart == "" {
			continue
		}
		lineStr, content, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		var ln int
		if _, err := fmt.Sscanf(lineStr, "%d", &ln); err != nil || ln <= 0 {
			continue
		}
		p := remoteCleanPath(pathPart)
		if workDir != "" && !remotePathWithinDir(p, workDir) {
			// Allow if path is relative under workdir.
			if strings.HasPrefix(p, "/") {
				continue
			}
			p = acpResolveRemotePath(p, workDir)
		}
		preview := strings.TrimSpace(content)
		if utf8.RuneCountInString(preview) > 200 {
			preview = string([]rune(preview)[:200]) + "…"
		}
		hits = append(hits, acpSearchHit{
			Path:    p,
			Line:    ln,
			Text:    content,
			Preview: preview,
		})
		if len(hits) >= max {
			break
		}
	}
	return hits
}

func compactRemoteSSHError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "remote SSH command failed"
	}
	// Keep last non-empty lines for diagnostics.
	lines := strings.Split(raw, "\n")
	var useful []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "$ ") || strings.Contains(line, "base64 -d") {
			continue
		}
		useful = append(useful, line)
	}
	if len(useful) == 0 {
		return truncateRunesV2(raw, 400)
	}
	if len(useful) > 8 {
		useful = useful[len(useful)-8:]
	}
	return strings.Join(useful, "\n")
}

func acpStripSSHEnvelope(raw string) string {
	// Prefer content after the last shell marker; fall back to full trim.
	lines := strings.Split(raw, "\n")
	var out []string
	started := false
	for _, line := range lines {
		if strings.HasPrefix(line, "total ") || strings.HasPrefix(line, "d") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "l") {
			started = true
		}
		if !started {
			// Also keep bare "ls: " error lines.
			if strings.Contains(line, "No such file") || strings.Contains(line, "cannot access") {
				started = true
			}
		}
		if started {
			if strings.HasPrefix(line, "---EXIT_CODE:") || strings.HasPrefix(line, "EXIT:") {
				break
			}
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return strings.TrimSpace(raw)
	}
	return strings.Join(out, "\n")
}
