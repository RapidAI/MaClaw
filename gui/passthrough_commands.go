package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type PassthroughParam struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Default  string `json:"default,omitempty"`
	Example  string `json:"example,omitempty"`
}

type PassthroughCommand struct {
	Name            string               `json:"name"`
	Title           string               `json:"title,omitempty"`
	Description     string               `json:"description,omitempty"`
	ScriptPath      string               `json:"script_path"`
	TemplateArgs    []string             `json:"template_args,omitempty"`
	Runtime         string               `json:"runtime"`
	Cwd             string               `json:"cwd,omitempty"`
	TimeoutSeconds  int                  `json:"timeout_seconds"`
	ConfirmRequired bool                 `json:"confirm_required"`
	Enabled         bool                 `json:"enabled"`
	Params          []PassthroughParam   `json:"params,omitempty"`
	CreatedAt       string               `json:"created_at,omitempty"`
	UpdatedAt       string               `json:"updated_at,omitempty"`
	LastRunAt       string               `json:"last_run_at,omitempty"`
	LastExitCode    int                  `json:"last_exit_code,omitempty"`
	LastStatus      passthroughRunStatus `json:"last_status,omitempty"`
}

type PassthroughRunResult struct {
	CommandName string               `json:"command_name"`
	Status      passthroughRunStatus `json:"status"`
	ExitCode    int                  `json:"exit_code"`
	DurationMs  int64                `json:"duration_ms"`
	Output      string               `json:"output"`
	Args        []string             `json:"args,omitempty"`
	StartedAt   string               `json:"started_at"`
	FinishedAt  string               `json:"finished_at"`
}

type PassthroughSettings struct {
	AllowExec bool `json:"allow_exec"`
}

type PassthroughAuditEntry struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	CommandName string               `json:"command_name"`
	Source      string               `json:"source,omitempty"`
	Args        []string             `json:"args,omitempty"`
	Status      passthroughRunStatus `json:"status"`
	ExitCode    int                  `json:"exit_code"`
	DurationMs  int64                `json:"duration_ms"`
	StartedAt   string               `json:"started_at"`
	FinishedAt  string               `json:"finished_at"`
	Error       string               `json:"error,omitempty"`
}

type passthroughRegistryFile struct {
	Version  int                     `json:"version"`
	Settings PassthroughSettings     `json:"settings"`
	Commands []PassthroughCommand    `json:"commands"`
	Audit    []PassthroughAuditEntry `json:"audit,omitempty"`
}

type PassthroughRegistry struct {
	path string
	mu   sync.Mutex
}

var (
	passthroughNameRe          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	passthroughParamRe         = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
	passthroughTemplateParamRe = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_-]{0,63}\}`)
	passthroughTemplateAnyRe   = regexp.MustCompile(`\$\{([^}]*)\}`)
	passthroughNumberRe        = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

func defaultPassthroughRegistryPath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "passthrough", "commands.json")
}

func newPassthroughRegistry(path string) *PassthroughRegistry {
	if strings.TrimSpace(path) == "" {
		path = defaultPassthroughRegistryPath()
	}
	return &PassthroughRegistry{path: path}
}

func (r *PassthroughRegistry) Path() string {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return defaultPassthroughRegistryPath()
	}
	return r.path
}

func (r *PassthroughRegistry) List() ([]PassthroughCommand, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return nil, err
	}
	out := append([]PassthroughCommand(nil), f.Commands...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *PassthroughRegistry) Get(name string) (PassthroughCommand, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return PassthroughCommand{}, false, err
	}
	for _, cmd := range f.Commands {
		if cmd.Name == name {
			return cmd, true, nil
		}
	}
	return PassthroughCommand{}, false, nil
}

func (r *PassthroughRegistry) GetSettings() (PassthroughSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return PassthroughSettings{}, err
	}
	return f.Settings, nil
}

func (r *PassthroughRegistry) SaveSettings(settings PassthroughSettings) (PassthroughSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return PassthroughSettings{}, err
	}
	f.Settings = settings
	if err := r.saveLocked(f); err != nil {
		return PassthroughSettings{}, err
	}
	return settings, nil
}

func (r *PassthroughRegistry) ListAudit(limit int) ([]PassthroughAuditEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return nil, err
	}
	out := append([]PassthroughAuditEntry(nil), f.Audit...)
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (r *PassthroughRegistry) Upsert(cmd PassthroughCommand) (PassthroughCommand, error) {
	if err := validatePassthroughCommand(&cmd); err != nil {
		return PassthroughCommand{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return PassthroughCommand{}, err
	}
	now := time.Now().Format(time.RFC3339)
	cmd.UpdatedAt = now
	replaced := false
	for i := range f.Commands {
		if f.Commands[i].Name == cmd.Name {
			cmd.CreatedAt = f.Commands[i].CreatedAt
			if cmd.CreatedAt == "" {
				cmd.CreatedAt = now
			}
			cmd.LastRunAt = f.Commands[i].LastRunAt
			cmd.LastExitCode = f.Commands[i].LastExitCode
			cmd.LastStatus = f.Commands[i].LastStatus
			f.Commands[i] = cmd
			replaced = true
			break
		}
	}
	if !replaced {
		cmd.CreatedAt = now
		f.Commands = append(f.Commands, cmd)
	}
	if err := r.saveLocked(f); err != nil {
		return PassthroughCommand{}, err
	}
	return cmd, nil
}

func (r *PassthroughRegistry) UpsertWithAudit(cmd PassthroughCommand, source string) (PassthroughCommand, error) {
	saved, err := r.Upsert(cmd)
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		name = "save"
	}
	if err != nil {
		_ = r.recordControlAudit("registry", "save "+name, source, passthroughRunStatusFailed, -1, err.Error())
		return PassthroughCommand{}, err
	}
	return saved, r.recordControlAudit("registry", "save "+saved.Name, source, passthroughRunStatusSuccess, 0, "")
}

func (r *PassthroughRegistry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return err
	}
	next := f.Commands[:0]
	deleted := false
	for _, cmd := range f.Commands {
		if cmd.Name == name {
			deleted = true
			continue
		}
		next = append(next, cmd)
	}
	if !deleted {
		return fmt.Errorf("passthrough command %q not found", name)
	}
	f.Commands = next
	return r.saveLocked(f)
}

func (r *PassthroughRegistry) DeleteWithAudit(name string, source string) error {
	name = strings.TrimSpace(name)
	if err := r.Delete(name); err != nil {
		_ = r.recordControlAudit("registry", "delete "+name, source, passthroughRunStatusFailed, -1, err.Error())
		return err
	}
	return r.recordControlAudit("registry", "delete "+name, source, passthroughRunStatusSuccess, 0, "")
}

func (r *PassthroughRegistry) SetEnabled(name string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return err
	}
	for i := range f.Commands {
		if f.Commands[i].Name == name {
			f.Commands[i].Enabled = enabled
			f.Commands[i].UpdatedAt = time.Now().Format(time.RFC3339)
			return r.saveLocked(f)
		}
	}
	return fmt.Errorf("passthrough command %q not found", name)
}

func (r *PassthroughRegistry) SetEnabledWithAudit(name string, enabled bool, source string) error {
	action := passthroughControlActionForEnabled(enabled)
	if err := r.SetEnabled(name, enabled); err != nil {
		_ = r.recordControlAudit("runctl", string(action)+" "+name, source, passthroughRunStatusFailed, -1, err.Error())
		return err
	}
	return r.recordControlAudit("runctl", string(action)+" "+name, source, passthroughRunStatusSuccess, 0, "")
}

func (r *PassthroughRegistry) Run(ctx context.Context, name string, values map[string]string, confirmed bool) (PassthroughRunResult, error) {
	return r.RunWithSource(ctx, name, values, confirmed, "")
}

func (r *PassthroughRegistry) RunWithSource(ctx context.Context, name string, values map[string]string, confirmed bool, source string) (PassthroughRunResult, error) {
	start := time.Now()
	cmd, ok, err := r.Get(name)
	if err != nil {
		return PassthroughRunResult{}, err
	}
	if !ok {
		err := fmt.Errorf("passthrough command %q not found", name)
		_ = r.recordControlAuditArgs("run", strings.TrimSpace(name), source, passthroughAuditArgsFromValues(values), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	if !cmd.Enabled {
		err := fmt.Errorf("passthrough command %q is disabled", name)
		_ = r.recordControlAuditArgs("run", name, source, passthroughAuditArgsFromValues(values), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	if cmd.ConfirmRequired && !confirmed {
		err := fmt.Errorf("passthrough command %q requires confirmation; resend with --confirm", name)
		_ = r.recordControlAuditArgs("run", name, source, passthroughAuditArgsFromValues(values), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	program, args, cwd, err := buildPassthroughProcess(cmd, values)
	if err != nil {
		_ = r.recordControlAuditArgs("run", name, source, passthroughAuditArgsFromValues(values), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	result, err := executePassthroughProcess(ctx, start, name, program, args, cwd, time.Duration(cmd.TimeoutSeconds)*time.Second)
	_ = r.recordRun(name, time.Now(), result.Status, result.ExitCode)
	auditResult := result
	auditResult.Args = redactPassthroughRunAuditArgs(result.Args, cmd.Params, values)
	_ = r.recordAudit("run", source, auditResult, err)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *PassthroughRegistry) RunExec(ctx context.Context, text string) (PassthroughRunResult, error) {
	return r.RunExecWithSource(ctx, text, "")
}

func (r *PassthroughRegistry) RunExecWithSource(ctx context.Context, text string, source string) (PassthroughRunResult, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return PassthroughRunResult{}, err
	}
	if !settings.AllowExec {
		err := fmt.Errorf("/exec is disabled; enable one-time system commands in Monitor > Passthrough Tasks")
		_ = r.recordControlAudit("exec", "disabled", source, passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	program, args, confirmed, err := parsePassthroughExecText(text)
	if err != nil {
		_ = r.recordControlAudit("exec", "parse", source, passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	if !confirmed {
		err := fmt.Errorf("/exec requires --confirm")
		_ = r.recordControlAuditArgs("exec", program, source, redactPassthroughCLIArgs(append([]string{program}, args...)), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	resolvedProgram, err := exec.LookPath(program)
	if err != nil {
		err := fmt.Errorf("executable not found: %s", program)
		_ = r.recordControlAuditArgs("exec", program, source, redactPassthroughCLIArgs(append([]string{program}, args...)), passthroughRunStatusFailed, -1, err.Error())
		return PassthroughRunResult{}, err
	}
	result, runErr := executePassthroughProcess(ctx, time.Now(), program, resolvedProgram, args, "", 120*time.Second)
	_ = r.recordAudit("exec", source, result, runErr)
	return result, runErr
}

func (r *PassthroughRegistry) recordRun(name string, at time.Time, status passthroughRunStatus, exitCode int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return err
	}
	for i := range f.Commands {
		if f.Commands[i].Name == name {
			f.Commands[i].LastRunAt = at.Format(time.RFC3339)
			f.Commands[i].LastStatus = status
			f.Commands[i].LastExitCode = exitCode
			return r.saveLocked(f)
		}
	}
	return nil
}

func (r *PassthroughRegistry) recordAudit(kind, source string, result PassthroughRunResult, runErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return err
	}
	entry := PassthroughAuditEntry{
		ID:          randomPassthroughSuffix() + randomPassthroughSuffix(),
		Kind:        kind,
		CommandName: result.CommandName,
		Source:      strings.TrimSpace(source),
		Args:        redactPassthroughCLIArgs(result.Args),
		Status:      result.Status,
		ExitCode:    result.ExitCode,
		DurationMs:  result.DurationMs,
		StartedAt:   result.StartedAt,
		FinishedAt:  result.FinishedAt,
	}
	if runErr != nil {
		entry.Error = runErr.Error()
	}
	f.Audit = append([]PassthroughAuditEntry{entry}, f.Audit...)
	if len(f.Audit) > 100 {
		f.Audit = f.Audit[:100]
	}
	return r.saveLocked(f)
}

func (r *PassthroughRegistry) recordControlAudit(kind, commandName, source string, status passthroughRunStatus, exitCode int, errText string) error {
	return r.recordControlAuditArgs(kind, commandName, source, nil, status, exitCode, errText)
}

func (r *PassthroughRegistry) recordControlAuditArgs(kind, commandName, source string, args []string, status passthroughRunStatus, exitCode int, errText string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.loadLocked()
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	entry := PassthroughAuditEntry{
		ID:          randomPassthroughSuffix() + randomPassthroughSuffix(),
		Kind:        strings.TrimSpace(kind),
		CommandName: strings.TrimSpace(commandName),
		Source:      strings.TrimSpace(source),
		Args:        append([]string(nil), args...),
		Status:      normalizePassthroughRunStatus(status),
		ExitCode:    exitCode,
		StartedAt:   now,
		FinishedAt:  now,
		Error:       strings.TrimSpace(errText),
	}
	f.Audit = append([]PassthroughAuditEntry{entry}, f.Audit...)
	if len(f.Audit) > 100 {
		f.Audit = f.Audit[:100]
	}
	return r.saveLocked(f)
}

func (r *PassthroughRegistry) loadLocked() (passthroughRegistryFile, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return passthroughRegistryFile{Version: 1}, nil
		}
		return passthroughRegistryFile{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return passthroughRegistryFile{Version: 1}, nil
	}
	var f passthroughRegistryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return passthroughRegistryFile{}, err
	}
	if f.Version == 0 {
		f.Version = 1
	}
	for i := range f.Commands {
		f.Commands[i].LastStatus = normalizePassthroughRunStatus(f.Commands[i].LastStatus)
	}
	for i := range f.Audit {
		f.Audit[i].Status = normalizePassthroughRunStatus(f.Audit[i].Status)
	}
	return f, nil
}

func (r *PassthroughRegistry) saveLocked(f passthroughRegistryFile) error {
	if f.Version == 0 {
		f.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp-" + randomPassthroughSuffix()
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func randomPassthroughSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func passthroughAuditArgsFromValues(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "confirm" || key == "yes" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		value := values[key]
		if strings.HasPrefix(key, "_") {
			args = append(args, key, truncatePassthroughAuditValue(value))
			continue
		}
		args = append(args, "--"+key, redactPassthroughAuditValue(key, value))
	}
	return args
}

func redactPassthroughRunAuditArgs(args []string, params []PassthroughParam, values map[string]string) []string {
	out := redactPassthroughCLIArgs(args)
	if len(out) == 0 || len(params) == 0 {
		return out
	}
	redactedValues := map[string]bool{}
	for _, p := range params {
		if !isSensitivePassthroughParamName(p.Name) {
			continue
		}
		value := ""
		ok := false
		if values != nil {
			value, ok = values[p.Name]
		}
		if !ok {
			value = p.Default
		}
		if value != "" {
			redactedValues[value] = true
		}
	}
	if len(redactedValues) == 0 {
		return out
	}
	for i, arg := range out {
		if arg == "<redacted>" {
			continue
		}
		next := arg
		for value := range redactedValues {
			if next == value {
				next = "<redacted>"
				break
			}
			next = strings.ReplaceAll(next, value, "<redacted>")
		}
		out[i] = next
	}
	return out
}

func redactPassthroughCLIArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := append([]string(nil), args...)
	redactNext := false
	for i, arg := range out {
		if redactNext {
			out[i] = "<redacted>"
			redactNext = false
			continue
		}
		if strings.HasPrefix(arg, "--") {
			nameValue := strings.TrimPrefix(arg, "--")
			name := nameValue
			if idx := strings.IndexAny(nameValue, "=:"); idx >= 0 {
				name = nameValue[:idx]
				if isSensitivePassthroughParamName(name) {
					out[i] = "--" + name + "=<redacted>"
				}
				continue
			}
			if isSensitivePassthroughParamName(name) {
				redactNext = true
			}
		}
	}
	return out
}

func redactPassthroughAuditValue(key string, value string) string {
	if isSensitivePassthroughParamName(key) {
		return "<redacted>"
	}
	return truncatePassthroughAuditValue(value)
}

func isSensitivePassthroughParamName(name string) bool {
	lower := strings.ToLower(name)
	for _, sensitive := range []string{"password", "passwd", "pass", "secret", "token", "apikey", "api_key", "accesskey", "access_key"} {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

func truncatePassthroughAuditValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 160 {
		return value
	}
	return value[:157] + "..."
}

func validatePassthroughCommand(cmd *PassthroughCommand) error {
	cmd.Name = strings.TrimSpace(cmd.Name)
	if !passthroughNameRe.MatchString(cmd.Name) {
		return fmt.Errorf("name must match %s", passthroughNameRe.String())
	}
	cmd.ScriptPath = strings.TrimSpace(cmd.ScriptPath)
	if cmd.ScriptPath == "" {
		return fmt.Errorf("script path is required")
	}
	cmd.Runtime = normalizePassthroughRuntime(cmd.Runtime, cmd.ScriptPath)
	if !isSupportedPassthroughRuntime(cmd.Runtime) {
		return fmt.Errorf("unsupported runtime %q", cmd.Runtime)
	}
	if cmd.TimeoutSeconds <= 0 {
		cmd.TimeoutSeconds = 120
	}
	if cmd.TimeoutSeconds > 3600 {
		return fmt.Errorf("timeout cannot exceed 3600 seconds")
	}
	for i := range cmd.Params {
		cmd.Params[i].Name = strings.TrimSpace(cmd.Params[i].Name)
		if !passthroughParamRe.MatchString(cmd.Params[i].Name) {
			return fmt.Errorf("param name %q must match %s", cmd.Params[i].Name, passthroughParamRe.String())
		}
		for j := 0; j < i; j++ {
			if cmd.Params[j].Name == cmd.Params[i].Name {
				return fmt.Errorf("duplicate param name %q", cmd.Params[i].Name)
			}
		}
		paramType := normalizePassthroughParamTypeKind(cmd.Params[i].Type)
		if !paramType.IsSupported() {
			return fmt.Errorf("unsupported param type %q", cmd.Params[i].Type)
		}
		cmd.Params[i].Type = paramType.String()
		if strings.TrimSpace(cmd.Params[i].Default) != "" {
			if err := validatePassthroughParamValue(cmd.Params[i], cmd.Params[i].Default); err != nil {
				return fmt.Errorf("invalid default for --%s: %w", cmd.Params[i].Name, err)
			}
		}
		if strings.TrimSpace(cmd.Params[i].Example) != "" {
			if err := validatePassthroughParamValue(cmd.Params[i], cmd.Params[i].Example); err != nil {
				return fmt.Errorf("invalid example for --%s: %w", cmd.Params[i].Name, err)
			}
		}
	}
	if err := validatePassthroughTemplateArgs(cmd.TemplateArgs, cmd.Params); err != nil {
		return err
	}
	return nil
}

func validatePassthroughTemplateArgs(templateArgs []string, params []PassthroughParam) error {
	paramNames := make(map[string]bool, len(params))
	for _, p := range params {
		paramNames[p.Name] = true
	}
	for _, arg := range templateArgs {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("template argument contains unsupported control characters")
		}
		for _, match := range passthroughTemplateAnyRe.FindAllStringSubmatch(arg, -1) {
			name := match[1]
			if !passthroughParamRe.MatchString(name) {
				return fmt.Errorf("template argument has invalid parameter placeholder ${%s}", name)
			}
			if !paramNames[name] {
				return fmt.Errorf("template argument references undefined parameter ${%s}", name)
			}
		}
		if strings.Contains(arg, "${") && !passthroughTemplateAnyRe.MatchString(arg) {
			return fmt.Errorf("template argument has an unclosed parameter placeholder")
		}
	}
	return nil
}

func normalizePassthroughRuntime(rt, script string) string {
	rt = strings.ToLower(strings.TrimSpace(rt))
	if rt != "" && rt != "auto" {
		return rt
	}
	ext := strings.ToLower(filepath.Ext(script))
	switch ext {
	case ".ps1":
		return "powershell"
	case ".bat", ".cmd":
		return "cmd"
	case ".sh":
		return "bash"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "node"
	default:
		return "direct"
	}
}

func isSupportedPassthroughRuntime(rt string) bool {
	switch rt {
	case "direct", "powershell", "pwsh", "cmd", "bash", "python", "node":
		return true
	default:
		return false
	}
}

func buildPassthroughProcess(cmd PassthroughCommand, values map[string]string) (string, []string, string, error) {
	if err := validatePassthroughCommand(&cmd); err != nil {
		return "", nil, "", err
	}
	scriptPath := os.ExpandEnv(cmd.ScriptPath)
	resolvedScript, scriptDir, err := resolvePassthroughProgram(scriptPath, cmd.Runtime)
	if err != nil {
		return "", nil, "", err
	}
	cwd := strings.TrimSpace(cmd.Cwd)
	if cwd == "" {
		cwd = scriptDir
	}
	cwd = os.ExpandEnv(cwd)
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", nil, "", err
	}
	if st, err := os.Stat(absCwd); err != nil {
		return "", nil, "", fmt.Errorf("working directory not found: %s", absCwd)
	} else if !st.IsDir() {
		return "", nil, "", fmt.Errorf("working directory is not a directory: %s", absCwd)
	}
	paramArgs, err := buildPassthroughArgs(cmd.Params, values)
	if err != nil {
		return "", nil, "", err
	}
	templateArgs, err := renderPassthroughTemplateArgs(cmd.TemplateArgs, cmd.Params, values)
	if err != nil {
		return "", nil, "", err
	}
	processArgs := paramArgs
	if len(cmd.TemplateArgs) > 0 {
		processArgs = templateArgs
	}
	switch cmd.Runtime {
	case "powershell":
		program := "powershell.exe"
		if runtime.GOOS != "windows" {
			program = "pwsh"
		}
		return program, append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolvedScript}, processArgs...), absCwd, nil
	case "pwsh":
		return "pwsh", append([]string{"-NoProfile", "-File", resolvedScript}, processArgs...), absCwd, nil
	case "cmd":
		return "cmd.exe", append([]string{"/C", resolvedScript}, processArgs...), absCwd, nil
	case "bash":
		return "bash", append([]string{resolvedScript}, processArgs...), absCwd, nil
	case "python":
		return "python", append([]string{resolvedScript}, processArgs...), absCwd, nil
	case "node":
		return "node", append([]string{resolvedScript}, processArgs...), absCwd, nil
	case "direct":
		return resolvedScript, processArgs, absCwd, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported runtime %q", cmd.Runtime)
	}
}

func previewPassthroughProcessArgs(cmd PassthroughCommand, values map[string]string) ([]string, error) {
	if err := validatePassthroughCommand(&cmd); err != nil {
		return nil, err
	}
	scriptPath := os.ExpandEnv(strings.TrimSpace(cmd.ScriptPath))
	paramArgs, err := buildPassthroughArgs(cmd.Params, values)
	if err != nil {
		return nil, err
	}
	templateArgs, err := renderPassthroughTemplateArgs(cmd.TemplateArgs, cmd.Params, values)
	if err != nil {
		return nil, err
	}
	processArgs := paramArgs
	if len(cmd.TemplateArgs) > 0 {
		processArgs = templateArgs
	}
	switch cmd.Runtime {
	case "powershell":
		program := "powershell.exe"
		if runtime.GOOS != "windows" {
			program = "pwsh"
		}
		return append([]string{program, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath}, processArgs...), nil
	case "pwsh":
		return append([]string{"pwsh", "-NoProfile", "-File", scriptPath}, processArgs...), nil
	case "cmd":
		return append([]string{"cmd.exe", "/C", scriptPath}, processArgs...), nil
	case "bash":
		return append([]string{"bash", scriptPath}, processArgs...), nil
	case "python":
		return append([]string{"python", scriptPath}, processArgs...), nil
	case "node":
		return append([]string{"node", scriptPath}, processArgs...), nil
	case "direct":
		return append([]string{scriptPath}, processArgs...), nil
	default:
		return nil, fmt.Errorf("unsupported runtime %q", cmd.Runtime)
	}
}

func resolvePassthroughProgram(program string, runtimeName string) (resolved string, defaultCwd string, err error) {
	program = strings.TrimSpace(program)
	if program == "" {
		return "", "", fmt.Errorf("script path is required")
	}
	hasPathSeparator := strings.ContainsAny(program, `/\`) || filepath.IsAbs(program)
	if runtimeName == "direct" && !hasPathSeparator {
		resolved, err := exec.LookPath(program)
		if err != nil {
			return "", "", fmt.Errorf("executable not found: %s", program)
		}
		cwd, err := os.Getwd()
		if err != nil || strings.TrimSpace(cwd) == "" {
			cwd = os.TempDir()
		}
		return resolved, cwd, nil
	}
	absProgram, err := filepath.Abs(program)
	if err != nil {
		return "", "", err
	}
	if st, err := os.Stat(absProgram); err != nil {
		return "", "", fmt.Errorf("script not found: %s", absProgram)
	} else if st.IsDir() {
		return "", "", fmt.Errorf("script path is a directory: %s", absProgram)
	}
	return absProgram, filepath.Dir(absProgram), nil
}

func executePassthroughProcess(ctx context.Context, start time.Time, commandName, program string, args []string, cwd string, timeout time.Duration) (PassthroughRunResult, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	execCmd := exec.Command(program, args...)
	if strings.TrimSpace(cwd) != "" {
		execCmd.Dir = cwd
	}
	var out bytes.Buffer
	execCmd.Stdout = &out
	execCmd.Stderr = &out
	hideCommandWindow(execCmd)
	coretool.PrepareCommandForTreeKill(execCmd)
	err := execCmd.Start()
	if err == nil {
		err = coretool.WaitCommandWithContext(runCtx, execCmd)
	}
	finished := time.Now()
	exitCode := 0
	status := passthroughRunStatusSuccess
	if err != nil {
		status = passthroughRunStatusFailed
		exitCode = -1
		if execCmd.ProcessState != nil {
			exitCode = execCmd.ProcessState.ExitCode()
		}
		if runCtx.Err() == context.DeadlineExceeded {
			status = passthroughRunStatusTimeout
		}
	} else if execCmd.ProcessState != nil {
		exitCode = execCmd.ProcessState.ExitCode()
	}
	result := PassthroughRunResult{
		CommandName: commandName,
		Status:      status,
		ExitCode:    exitCode,
		DurationMs:  finished.Sub(start).Milliseconds(),
		Output:      truncatePassthroughOutput(out.String(), 6000),
		Args:        append([]string{program}, args...),
		StartedAt:   start.Format(time.RFC3339),
		FinishedAt:  finished.Format(time.RFC3339),
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func buildPassthroughArgs(params []PassthroughParam, values map[string]string) ([]string, error) {
	args := make([]string, 0, len(params)*2)
	used := map[string]bool{}
	for idx, p := range params {
		value := ""
		hasValue := false
		if values != nil {
			value, hasValue = values[p.Name]
		}
		if !hasValue && values != nil {
			value, hasValue = values[fmt.Sprintf("_%d", idx+1)]
		}
		if !hasValue && p.Default != "" {
			value = p.Default
			hasValue = true
		}
		if p.Required && !hasValue {
			return nil, fmt.Errorf("missing required parameter --%s", p.Name)
		}
		if !hasValue {
			continue
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("parameter --%s contains unsupported control characters", p.Name)
		}
		if err := validatePassthroughParamValue(p, value); err != nil {
			return nil, err
		}
		args = append(args, "--"+p.Name, value)
		used[p.Name] = true
	}
	for key := range values {
		if key == "confirm" || key == "yes" || strings.HasPrefix(key, "_") {
			continue
		}
		if !used[key] {
			known := false
			for _, p := range params {
				if p.Name == key {
					known = true
					break
				}
			}
			if !known {
				return nil, fmt.Errorf("unknown parameter --%s", key)
			}
		}
	}
	return args, nil
}

func renderPassthroughTemplateArgs(templateArgs []string, params []PassthroughParam, values map[string]string) ([]string, error) {
	if len(templateArgs) == 0 {
		return nil, nil
	}
	paramNames := make(map[string]bool, len(params))
	for _, p := range params {
		paramNames[p.Name] = true
	}
	out := make([]string, 0, len(templateArgs))
	for _, arg := range templateArgs {
		rendered := passthroughTemplateParamRe.ReplaceAllStringFunc(arg, func(match string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
			if values != nil {
				if value, ok := values[name]; ok {
					return value
				}
			}
			for _, p := range params {
				if p.Name == name {
					return p.Default
				}
			}
			return match
		})
		for _, match := range passthroughTemplateParamRe.FindAllString(rendered, -1) {
			name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
			if !paramNames[name] {
				return nil, fmt.Errorf("template argument references undefined parameter ${%s}", name)
			}
			return nil, fmt.Errorf("template argument parameter ${%s} has no value", name)
		}
		out = append(out, rendered)
	}
	return out, nil
}

func validatePassthroughParamValue(p PassthroughParam, value string) error {
	switch normalizePassthroughParamTypeKind(p.Type) {
	case passthroughParamTypeNumber:
		if !passthroughNumberRe.MatchString(value) {
			return fmt.Errorf("parameter --%s must be a number", p.Name)
		}
	case passthroughParamTypeBoolean:
		lower := strings.ToLower(value)
		if lower != "true" && lower != "false" && lower != "1" && lower != "0" && lower != "yes" && lower != "no" {
			return fmt.Errorf("parameter --%s must be boolean", p.Name)
		}
	case passthroughParamTypePath:
		if strings.ContainsAny(value, "<>|?*") {
			return fmt.Errorf("parameter --%s is not a valid path", p.Name)
		}
	}
	return nil
}

func truncatePassthroughOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 32 {
		return truncatePassthroughOutputPrefix(s, max)
	}
	return truncatePassthroughOutputPrefix(s, max-32) + "\n... output truncated ..."
}

func truncatePassthroughOutputPrefix(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.ValidString(s[:max]) {
		max--
	}
	return s[:max]
}

func formatPassthroughRunResult(result PassthroughRunResult) string {
	if result.Output != "" {
		return result.Output
	}
	if normalizePassthroughRunStatus(result.Status) == passthroughRunStatusSuccess {
		return ""
	}
	return fmt.Sprintf("直通命令 %s %s，退出码：%d", result.CommandName, result.Status, result.ExitCode)
}

func passthroughHelpText(lang ...string) string {
	if len(lang) > 0 && normalizeAppLanguageKind(lang[0]) == appLanguageEnglish {
		return "Passthrough tasks:\n" +
			"/run <name> [--param value] [--confirm]\n" +
			"  Run a pre-registered script from Monitor > Passthrough Tasks. Works even when LLM/agent is unavailable.\n" +
			"/runctl list\n" +
			"  List registered passthrough tasks and whether they are enabled or require --confirm.\n" +
			"/runctl status\n" +
			"  Show task counts, /exec state, registry path, and audit count.\n" +
			"/runctl show <name>\n" +
			"  Show script path, runtime, working directory, timeout, and parameter definitions.\n" +
			"/runctl export <name>\n" +
			"  Export the /runctl save registration command for copying, migration, or rebuild.\n" +
			"/runctl save <name> --cmd 'command template' [--runtime direct] [--param \"name:type:required:default:example\"] [--params-json ...] --confirm\n" +
			"  Create or update a passthrough task remotely. Add --preview to preview argv without saving.\n" +
			"/runctl preview <name> [--param value]\n" +
			"  Preview the final argv for a task.\n" +
			"/runctl enable <name> / /runctl disable <name>\n" +
			"  Enable or disable a registered task.\n" +
			"/runctl delete <name> --confirm\n" +
			"  Delete a registered task and record the operation in audit logs.\n" +
			"/runctl exec enable / /runctl exec disable\n" +
			"  Enable or disable one-off /exec system commands. /exec still requires --confirm and does not use a shell.\n" +
			"/runctl audit [limit]\n" +
			"  Show recent /run, /exec, and control audit records. Default 10, max 50.\n" +
			"/runctl help\n" +
			"  Show passthrough help only.\n" +
			"/exec <program> [args...] --confirm\n" +
			"  One-off system command. Must be enabled first in Monitor > Passthrough Tasks.\n" +
			"Note: /run and /exec stdout/stderr are returned as-is and recorded in recent audit logs."
	}
	return "直通任务 / Passthrough:\n" +
		"/run <任务名> [--参数 值] [--confirm]\n" +
		"  执行在“监控 > 直通任务”中预先注册的脚本；LLM/agent 不可用时也能运行。\n" +
		"  可在“监控 > 直通任务”中新增、编辑、删除任务，并用测试值试运行确认可用。\n" +
		"  参数以 argv 形式传给脚本，例如脚本收到：--target D:\\workprj\\aicoder --deep true。\n" +
		"  示例：/run repair-env --target D:\\workprj\\aicoder --deep true --confirm\n" +
		"/runctl list\n" +
		"  查看已注册直通任务，以及是否启用、是否需要 --confirm。\n" +
		"/runctl status\n" +
		"  查看直通任务总数、/exec 是否开启、注册表路径和审计记录数量。\n" +
		"/runctl show <任务名>\n" +
		"  查看某个直通任务的脚本路径、运行时、工作目录、超时、参数定义。\n" +
		"/runctl export <任务名>\n" +
		"  只导出某个直通任务的 /runctl save 注册命令，方便从 IM 复制、迁移或重建。\n" +
		"/runctl save <任务名> --cmd '命令模板' [--runtime direct] [--param \"name:type:required:default:example\"] [--params-json '[{\"name\":\"target\",\"type\":\"path\",\"required\":true,\"example\":\"D:\\\\workprj\\\\aicoder\"}]'] --confirm\n" +
		"  远程新增或更新直通任务；命令模板按 argv 解析，参数可重复传入，用于 LLM/agent 不可用时做最小注册维护。\n" +
		"  加 --preview 只预览最终 argv，不保存，也不需要 --confirm；复杂形参建议使用 --params-json，JSON 中 Windows 路径需写成 D:\\\\workprj\\\\aicoder。\n" +
		"/runctl preview <任务名> [--参数 值]\n" +
		"  预览某个直通任务最终 argv；未传参数时优先使用形参 example/default，布尔参数可简写为 --deep。\n" +
		"/runctl enable <任务名> / /runctl disable <任务名>\n" +
		"  启用或禁用已注册直通任务；用于 LLM/agent 不可用时恢复某个预定义任务。\n" +
		"/runctl delete <任务名> --confirm\n" +
		"  删除已注册直通任务；用于远程移除误配置或危险任务，删除操作会写入审计记录。\n" +
		"/runctl exec enable / /runctl exec disable\n" +
		"  远程开启或关闭 /exec 一次性系统命令；/exec 仍需 --confirm，且不经过 shell。\n" +
		"/runctl audit [数量]\n" +
		"  查看最近 N 条 /run、/exec 与开关操作审计记录，默认 10 条，最多 50 条；包括来源、状态、退出码、耗时和 argv。\n" +
		"  审计中的 password、token、secret、api_key 等敏感参数值会显示为 <redacted>。\n" +
		"/runctl help\n" +
		"  只查看直通任务帮助。\n" +
		"/exec <程序> [参数...] --confirm\n" +
		"  一次性系统命令；需先在“监控 > 直通任务”中开启 /exec。\n" +
		"  /exec 不经过 shell，只运行 PATH 中可找到的程序或绝对路径，不解释管道、重定向、&& 等 shell 语法。\n" +
		"  如需把字面量 --confirm 传给目标程序，可在确认标记后使用 -- 分隔：/exec tool --confirm -- --confirm。\n" +
		"  示例：/exec git status --short --confirm\n" +
		"  示例：/exec powershell -NoProfile -File D:\\ops\\repair.ps1 --confirm\n" +
		"说明：/run 与 /exec 的 stdout/stderr 会原样返回发起通道，并记录到“最近审计记录”。"
}

func formatPassthroughAuditList(entries []PassthroughAuditEntry) string {
	return formatPassthroughAuditListWithLang(entries, "zh-Hans")
}

func formatPassthroughAuditListWithLang(entries []PassthroughAuditEntry, lang string) string {
	if len(entries) == 0 {
		if normalizeAppLanguageKind(lang) == appLanguageEnglish {
			return "No passthrough audit records."
		}
		return "暂无直通任务审计记录。"
	}
	var b strings.Builder
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		b.WriteString("Recent passthrough audit records:")
	} else {
		b.WriteString("最近直通任务审计记录：")
	}
	for _, entry := range entries {
		when := entry.StartedAt
		if parsed, err := time.Parse(time.RFC3339, entry.StartedAt); err == nil {
			when = parsed.Format("2006-01-02 15:04:05")
		}
		source := entry.Source
		if source == "" {
			source = "-"
		}
		fmt.Fprintf(&b, "\n- %s %s %s source=%s status=%s exit=%d %dms",
			when, entry.Kind, entry.CommandName, source, entry.Status, entry.ExitCode, entry.DurationMs)
		if entry.Error != "" {
			fmt.Fprintf(&b, "\n  error=%s", entry.Error)
		}
		if len(entry.Args) > 0 {
			fmt.Fprintf(&b, "\n  args=%s", formatPassthroughArgList(entry.Args))
		}
	}
	return b.String()
}

func formatPassthroughCommandShow(cmd PassthroughCommand) string {
	return formatPassthroughCommandShowWithLang(cmd, "zh-Hans")
}

func formatPassthroughCommandShowWithLang(cmd PassthroughCommand, lang string) string {
	var b strings.Builder
	title := cmd.Title
	if title == "" {
		title = cmd.Name
	}
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		fmt.Fprintf(&b, "Command: %s\nTitle: %s\nScript: %s\nRuntime: %s\nWorking directory: %s\nTimeout: %ds\nEnabled: %v\nRequires confirmation: %v",
			cmd.Name, title, cmd.ScriptPath, cmd.Runtime, cmd.Cwd, cmd.TimeoutSeconds, cmd.Enabled, cmd.ConfirmRequired)
		if len(cmd.TemplateArgs) > 0 {
			fmt.Fprintf(&b, "\nTemplate args: %s", formatPassthroughArgList(cmd.TemplateArgs))
		}
		if len(cmd.Params) > 0 {
			b.WriteString("\nParameters:")
			for _, p := range cmd.Params {
				req := ""
				if p.Required {
					req = " required"
				}
				fmt.Fprintf(&b, "\n- --%s %s%s", p.Name, p.Type, req)
			}
		}
		fmt.Fprintf(&b, "\nRun example: %s", passthroughCommandRunExample(cmd))
		fmt.Fprintf(&b, "\nRemote registration command: %s", passthroughRunctlSaveExample(cmd))
		return b.String()
	}
	fmt.Fprintf(&b, "命令：%s\n标题：%s\n脚本：%s\n运行时：%s\n工作目录：%s\n超时：%ds\n状态：%v\n需要确认：%v",
		cmd.Name, title, cmd.ScriptPath, cmd.Runtime, cmd.Cwd, cmd.TimeoutSeconds, cmd.Enabled, cmd.ConfirmRequired)
	if len(cmd.TemplateArgs) > 0 {
		fmt.Fprintf(&b, "\n模板参数：%s", formatPassthroughArgList(cmd.TemplateArgs))
	}
	if len(cmd.Params) > 0 {
		b.WriteString("\n参数：")
		for _, p := range cmd.Params {
			req := ""
			if p.Required {
				req = " 必填"
			}
			fmt.Fprintf(&b, "\n- --%s %s%s", p.Name, p.Type, req)
		}
	}
	fmt.Fprintf(&b, "\n运行示例：%s", passthroughCommandRunExample(cmd))
	fmt.Fprintf(&b, "\n远程注册命令：%s", passthroughRunctlSaveExample(cmd))
	return b.String()
}

func passthroughCommandRunExample(cmd PassthroughCommand) string {
	parts := []string{"/run", cmd.Name}
	for _, param := range cmd.Params {
		value := param.Example
		if value == "" {
			value = param.Default
		}
		if value == "" {
			value = "<" + param.Name + ">"
		}
		parts = append(parts, formatPassthroughNamedArg(param.Name, value)...)
	}
	if cmd.ConfirmRequired {
		parts = append(parts, "--confirm")
	}
	return strings.Join(parts, " ")
}

func formatPassthroughNamedArg(name string, value string) []string {
	if strings.HasPrefix(value, "--") {
		return []string{"--" + name + "=" + quotePassthroughArg(value)}
	}
	return []string{"--" + name, quotePassthroughArg(value)}
}

func passthroughRunctlSaveExample(cmd PassthroughCommand) string {
	commandParts := append([]string{cmd.ScriptPath}, cmd.TemplateArgs...)
	quotedCommandParts := make([]string, 0, len(commandParts))
	for _, part := range commandParts {
		quotedCommandParts = append(quotedCommandParts, quotePassthroughArg(part))
	}
	parts := []string{"/runctl", "save", cmd.Name, "--cmd", quotePassthroughSingleArg(strings.Join(quotedCommandParts, " "))}
	if cmd.Runtime != "" && cmd.Runtime != "direct" {
		parts = append(parts, "--runtime", quotePassthroughArg(cmd.Runtime))
	}
	if cmd.Cwd != "" {
		parts = append(parts, "--cwd", quotePassthroughArg(cmd.Cwd))
	}
	if len(cmd.Params) > 0 {
		data, err := json.Marshal(cmd.Params)
		if err == nil {
			parts = append(parts, "--params-json", quotePassthroughSingleArg(string(data)))
		}
	}
	if !cmd.ConfirmRequired {
		parts = append(parts, "--no-confirm")
	}
	if !cmd.Enabled {
		parts = append(parts, "--disabled")
	}
	parts = append(parts, "--confirm")
	return strings.Join(parts, " ")
}

func quotePassthroughSingleArg(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}

func parsePassthroughParamsText(raw string) ([]PassthroughParam, error) {
	var params []PassthroughParam
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		requiredText := strings.ToLower(strings.TrimSpace(parts[2]))
		required := requiredText == "" || requiredText == "required" || requiredText == "true" || requiredText == "yes" || requiredText == "1"
		param := PassthroughParam{
			Name:     strings.TrimSpace(parts[0]),
			Type:     strings.TrimSpace(parts[1]),
			Required: required,
		}
		if param.Type == "" {
			param.Type = "text"
		}
		if len(parts) > 3 {
			param.Default = strings.TrimSpace(parts[3])
		}
		if len(parts) > 4 {
			param.Example = strings.TrimSpace(strings.Join(parts[4:], ":"))
		}
		if strings.EqualFold(param.Type, "path") && looksLikeSplitWindowsPath(param.Default, param.Example) {
			param.Default = param.Default + ":" + param.Example
			param.Example = ""
		}
		if param.Name == "" {
			return nil, fmt.Errorf("param name is required")
		}
		params = append(params, param)
	}
	return params, nil
}

func looksLikeSplitWindowsPath(prefix string, suffix string) bool {
	prefix = strings.TrimSpace(prefix)
	suffix = strings.TrimSpace(suffix)
	return len(prefix) == 1 && ((prefix[0] >= 'A' && prefix[0] <= 'Z') || (prefix[0] >= 'a' && prefix[0] <= 'z')) && strings.HasPrefix(suffix, `\`)
}

func parsePassthroughParamsJSON(raw string) ([]PassthroughParam, error) {
	var params []PassthroughParam
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &params); err != nil {
		return nil, fmt.Errorf("invalid params json: %w", err)
	}
	for i := range params {
		params[i].Name = strings.TrimSpace(params[i].Name)
		params[i].Type = strings.TrimSpace(params[i].Type)
		params[i].Default = strings.TrimSpace(params[i].Default)
		params[i].Example = strings.TrimSpace(params[i].Example)
		if params[i].Type == "" {
			params[i].Type = "text"
		}
		if params[i].Name == "" {
			return nil, fmt.Errorf("param name is required")
		}
	}
	return params, nil
}

func parsePassthroughPreviewText(text string) (name string, values map[string]string, err error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return "", nil, err
	}
	if len(fields) < 3 || fields[0] != "/runctl" || fields[1] != "preview" {
		return "", nil, fmt.Errorf("usage: /runctl preview <name> [--param value]")
	}
	name = fields[2]
	values = map[string]string{}
	for i := 3; i < len(fields); i++ {
		f := fields[i]
		if !strings.HasPrefix(f, "--") {
			return "", nil, fmt.Errorf("unexpected argument %q; use --param value", f)
		}
		key := strings.TrimPrefix(f, "--")
		if key, value, ok, err := parsePassthroughInlineNamedValue(key); err != nil {
			return "", nil, err
		} else if ok {
			values[key] = value
			continue
		}
		if !passthroughParamRe.MatchString(key) {
			return "", nil, fmt.Errorf("invalid parameter name --%s", key)
		}
		if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "--") {
			values[key] = "true"
			continue
		}
		i++
		values[key] = fields[i]
	}
	return name, values, nil
}

func parsePassthroughSaveText(text string) (PassthroughCommand, bool, bool, error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return PassthroughCommand{}, false, false, err
	}
	if len(fields) < 3 || fields[0] != "/runctl" || fields[1] != "save" {
		return PassthroughCommand{}, false, false, fmt.Errorf("usage: /runctl save <name> --cmd \"command template\" [--param name:type:required:default:example] [--params-json json] --confirm")
	}
	cmd := PassthroughCommand{
		Name:            strings.TrimSpace(fields[2]),
		Runtime:         "direct",
		TimeoutSeconds:  120,
		ConfirmRequired: true,
		Enabled:         true,
	}
	if !passthroughNameRe.MatchString(cmd.Name) {
		return PassthroughCommand{}, false, false, fmt.Errorf("name must match %s", passthroughNameRe.String())
	}
	paramLines := []string{}
	paramJSONLines := []string{}
	confirmed := false
	previewOnly := false
	for i := 3; i < len(fields); i++ {
		field := fields[i]
		inlineValue := ""
		hasInlineValue := false
		if strings.HasPrefix(field, "--") {
			if idx := strings.Index(field, "="); idx >= 0 {
				inlineValue = field[idx+1:]
				field = field[:idx]
				hasInlineValue = true
			}
		}
		switch field {
		case "--confirm", "--yes":
			confirmed = true
		case "--preview":
			previewOnly = true
		case "--cmd", "--command":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			parts, err := splitPassthroughFields(value)
			if err != nil {
				return PassthroughCommand{}, false, false, fmt.Errorf("invalid command template: %w", err)
			}
			if len(parts) == 0 {
				return PassthroughCommand{}, false, false, fmt.Errorf("command template is empty")
			}
			cmd.ScriptPath = parts[0]
			cmd.TemplateArgs = parts[1:]
		case "--runtime":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			cmd.Runtime = value
		case "--cwd":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			cmd.Cwd = value
		case "--title":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			cmd.Title = value
		case "--desc", "--description":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			cmd.Description = value
		case "--timeout":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			timeout, err := strconv.Atoi(value)
			if err != nil || timeout <= 0 {
				return PassthroughCommand{}, false, false, fmt.Errorf("timeout must be a positive number")
			}
			cmd.TimeoutSeconds = timeout
		case "--param":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			paramLines = append(paramLines, value)
		case "--params-json", "--param-json":
			value, ok := passthroughNextValue(fields, &i, field, inlineValue, hasInlineValue)
			if !ok {
				return PassthroughCommand{}, false, false, fmt.Errorf("missing value for %s", field)
			}
			paramJSONLines = append(paramJSONLines, value)
		case "--no-confirm":
			cmd.ConfirmRequired = false
		case "--disabled":
			cmd.Enabled = false
		case "--enabled":
			cmd.Enabled = true
		default:
			return PassthroughCommand{}, false, false, fmt.Errorf("unexpected argument %q; usage: /runctl save <name> --cmd \"command template\" --confirm", field)
		}
	}
	if !confirmed && !previewOnly {
		return PassthroughCommand{}, false, false, fmt.Errorf("/runctl save requires --confirm")
	}
	if strings.TrimSpace(cmd.ScriptPath) == "" {
		return PassthroughCommand{}, false, false, fmt.Errorf("missing --cmd")
	}
	if len(paramLines) > 0 && len(paramJSONLines) > 0 {
		return PassthroughCommand{}, false, false, fmt.Errorf("use either --param or --params-json, not both")
	}
	if len(paramJSONLines) > 0 {
		params, err := parsePassthroughParamsJSON(strings.Join(paramJSONLines, "\n"))
		if err != nil {
			return PassthroughCommand{}, false, false, err
		}
		cmd.Params = params
	} else if len(paramLines) > 0 {
		params, err := parsePassthroughParamsText(strings.Join(paramLines, "\n"))
		if err != nil {
			return PassthroughCommand{}, false, false, err
		}
		cmd.Params = params
	}
	if err := validatePassthroughCommand(&cmd); err != nil {
		return PassthroughCommand{}, false, false, err
	}
	return cmd, confirmed, previewOnly, nil
}

func passthroughNextValue(fields []string, index *int, flag string, inlineValue string, hasInlineValue bool) (string, bool) {
	if hasInlineValue {
		return inlineValue, true
	}
	if *index+1 >= len(fields) || strings.HasPrefix(fields[*index+1], "--") {
		return "", false
	}
	*index = *index + 1
	return fields[*index], true
}

func parsePassthroughSetEnabledText(text string) (action string, name string, err error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return "", "", err
	}
	if len(fields) != 3 || fields[0] != "/runctl" || normalizePassthroughControlAction(fields[1]) == passthroughControlActionUnknown {
		return "", "", fmt.Errorf("usage: /runctl enable <name> or /runctl disable <name>")
	}
	name = strings.TrimSpace(fields[2])
	if !passthroughNameRe.MatchString(name) {
		return "", "", fmt.Errorf("name must match %s", passthroughNameRe.String())
	}
	return string(normalizePassthroughControlAction(fields[1])), name, nil
}

func parsePassthroughDeleteText(text string) (name string, confirmed bool, err error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return "", false, err
	}
	if len(fields) < 3 || fields[0] != "/runctl" || fields[1] != "delete" {
		return "", false, fmt.Errorf("usage: /runctl delete <name> --confirm")
	}
	name = strings.TrimSpace(fields[2])
	if !passthroughNameRe.MatchString(name) {
		return "", false, fmt.Errorf("name must match %s", passthroughNameRe.String())
	}
	for _, field := range fields[3:] {
		switch field {
		case "--confirm", "--yes":
			confirmed = true
		default:
			return "", false, fmt.Errorf("unexpected argument %q; usage: /runctl delete <name> --confirm", field)
		}
	}
	if !confirmed {
		return "", false, fmt.Errorf("/runctl delete requires --confirm")
	}
	return name, true, nil
}

func parsePassthroughExecSettingText(text string) (bool, error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return false, err
	}
	if len(fields) != 3 || fields[0] != "/runctl" || fields[1] != "exec" {
		return false, fmt.Errorf("usage: /runctl exec enable or /runctl exec disable")
	}
	action := normalizePassthroughControlAction(fields[2])
	switch {
	case action == passthroughControlActionEnable:
		return true, nil
	case action == passthroughControlActionDisable:
		return false, nil
	case fields[2] == "on" || fields[2] == "true" || fields[2] == "1":
		return true, nil
	case fields[2] == "off" || fields[2] == "false" || fields[2] == "0":
		return false, nil
	default:
		return false, fmt.Errorf("usage: /runctl exec enable or /runctl exec disable")
	}
}

func passthroughPreviewValues(cmd PassthroughCommand, values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		out[key] = value
	}
	for _, p := range cmd.Params {
		if _, ok := out[p.Name]; ok {
			continue
		}
		if p.Example != "" {
			out[p.Name] = p.Example
			continue
		}
		if p.Default != "" {
			out[p.Name] = p.Default
			continue
		}
		if p.Required || passthroughTemplateReferencesParam(cmd.TemplateArgs, p.Name) {
			out[p.Name] = passthroughPreviewSampleValue(p)
		}
	}
	return out
}

func passthroughTemplateReferencesParam(templateArgs []string, name string) bool {
	placeholder := "${" + name + "}"
	for _, arg := range templateArgs {
		if strings.Contains(arg, placeholder) {
			return true
		}
	}
	return false
}

func passthroughPreviewSampleValue(p PassthroughParam) string {
	switch normalizePassthroughParamTypeKind(p.Type) {
	case passthroughParamTypeNumber:
		return "1"
	case passthroughParamTypeBoolean:
		return "true"
	case passthroughParamTypePath:
		return "."
	default:
		return "sample"
	}
}

func formatPassthroughPreviewArgs(args []string) string {
	if len(args) == 0 {
		return "argv: (empty)"
	}
	return "argv: " + formatPassthroughArgList(args)
}

func formatPassthroughArgList(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quotePassthroughArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quotePassthroughArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.ContainsAny(arg, " \t\"'") {
		escaped := strings.ReplaceAll(arg, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return arg
}

func parsePassthroughAuditLimit(text string, defaultLimit int, maxLimit int) (int, error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return 0, err
	}
	if len(fields) == 2 {
		return defaultLimit, nil
	}
	if len(fields) != 3 || fields[0] != "/runctl" || fields[1] != "audit" {
		return 0, fmt.Errorf("usage: /runctl audit [limit]")
	}
	limit, err := strconv.Atoi(fields[2])
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("audit limit must be a positive number")
	}
	if maxLimit > 0 && limit > maxLimit {
		return maxLimit, nil
	}
	return limit, nil
}

func formatPassthroughStatus(registryPath string, commands []PassthroughCommand, settings PassthroughSettings, auditCount int) string {
	return formatPassthroughStatusWithLang(registryPath, commands, settings, auditCount, "zh-Hans")
}

func formatPassthroughStatusWithLang(registryPath string, commands []PassthroughCommand, settings PassthroughSettings, auditCount int, lang string) string {
	enabled := 0
	confirmRequired := 0
	for _, cmd := range commands {
		if cmd.Enabled {
			enabled++
		}
		if cmd.ConfirmRequired {
			confirmRequired++
		}
	}
	execStatus := "关闭"
	if settings.AllowExec {
		execStatus = "开启"
	}
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		execStatus = "off"
		if settings.AllowExec {
			execStatus = "on"
		}
		return fmt.Sprintf("Passthrough task status:\n- Tasks: %d total, %d enabled, %d require --confirm\n- /exec: %s\n- Audit records: %d\n- Registry: %s",
			len(commands), enabled, confirmRequired, execStatus, auditCount, registryPath)
	}
	return fmt.Sprintf("直通任务状态：\n- 任务：%d 个，启用 %d 个，需 --confirm %d 个\n- /exec：%s\n- 审计记录：%d 条\n- 注册表：%s",
		len(commands), enabled, confirmRequired, execStatus, auditCount, registryPath)
}

func slashHelpText(lang ...string) string {
	activeLang := "zh-Hans"
	if len(lang) > 0 {
		activeLang = lang[0]
	}
	return localizedIMSlashHelpText(activeLang) + "\n\n" + passthroughHelpText(activeLang)
}

func parsePassthroughRunText(text string) (name string, values map[string]string, confirmed bool, err error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return "", nil, false, err
	}
	if len(fields) < 2 || fields[0] != "/run" {
		return "", nil, false, fmt.Errorf("usage: /run <name> [--param value] [--confirm]")
	}
	name = fields[1]
	values = map[string]string{}
	positional := []string{}
	for i := 2; i < len(fields); i++ {
		f := fields[i]
		if f == "--confirm" || f == "--yes" {
			confirmed = true
			values["confirm"] = "true"
			continue
		}
		if strings.HasPrefix(f, "--") {
			key := strings.TrimPrefix(f, "--")
			if key, value, ok, err := parsePassthroughInlineNamedValue(key); err != nil {
				return "", nil, false, err
			} else if ok {
				values[key] = value
				continue
			}
			if !passthroughParamRe.MatchString(key) {
				return "", nil, false, fmt.Errorf("invalid parameter name --%s", key)
			}
			if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "--") {
				values[key] = "true"
				continue
			}
			values[key] = fields[i+1]
			i++
			continue
		}
		positional = append(positional, f)
	}
	for i, v := range positional {
		values[fmt.Sprintf("_%d", i+1)] = v
	}
	return name, values, confirmed, nil
}

func parsePassthroughInlineNamedValue(raw string) (key string, value string, ok bool, err error) {
	idx := strings.Index(raw, "=")
	if idx < 0 {
		return raw, "", false, nil
	}
	key = raw[:idx]
	value = raw[idx+1:]
	if !passthroughParamRe.MatchString(key) {
		return "", "", false, fmt.Errorf("invalid parameter name --%s", key)
	}
	return key, value, true, nil
}

func parsePassthroughExecText(text string) (program string, args []string, confirmed bool, err error) {
	fields, err := splitPassthroughFields(strings.TrimSpace(text))
	if err != nil {
		return "", nil, false, err
	}
	if len(fields) < 2 || fields[0] != "/exec" {
		return "", nil, false, fmt.Errorf("usage: /exec <program> [args...] --confirm")
	}
	program = strings.TrimSpace(fields[1])
	if strings.ContainsAny(program, "\x00\r\n") {
		return "", nil, false, fmt.Errorf("program contains unsupported control characters")
	}
	for i := 2; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			for _, literal := range fields[i+1:] {
				if strings.ContainsAny(literal, "\x00\r\n") {
					return "", nil, false, fmt.Errorf("argument contains unsupported control characters")
				}
				args = append(args, literal)
			}
			break
		}
		if f == "--confirm" || f == "--yes" {
			confirmed = true
			continue
		}
		if strings.ContainsAny(f, "\x00\r\n") {
			return "", nil, false, fmt.Errorf("argument contains unsupported control characters")
		}
		args = append(args, f)
	}
	return program, args, confirmed, nil
}

func splitPassthroughFields(s string) ([]string, error) {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	fieldStarted := false
	for _, r := range s {
		if escaped {
			if r != quote && r != '\\' {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
			escaped = false
			fieldStarted = true
			continue
		}
		if quote != 0 {
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
				fieldStarted = true
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			fieldStarted = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if fieldStarted {
				fields = append(fields, b.String())
				b.Reset()
				fieldStarted = false
			}
			continue
		}
		b.WriteRune(r)
		fieldStarted = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if escaped {
		b.WriteRune('\\')
		fieldStarted = true
	}
	if fieldStarted {
		fields = append(fields, b.String())
	}
	return fields, nil
}
