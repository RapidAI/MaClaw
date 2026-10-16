package guiapp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PrecheckResult 预检结果
type PrecheckResult struct {
	ToolReady    bool   `json:"tool_ready"`
	ProjectReady bool   `json:"project_ready"`
	ModelReady   bool   `json:"model_ready"`
	ToolHint     string `json:"tool_hint,omitempty"`
	ModelHint    string `json:"model_hint,omitempty"`
	AllPassed    bool   `json:"all_passed"`
}

// SessionPrecheck 启动前环境预检
type SessionPrecheck struct {
	app *App
}

// NewSessionPrecheck creates a new SessionPrecheck.
func NewSessionPrecheck(app *App) *SessionPrecheck {
	return &SessionPrecheck{app: app}
}

// Check 执行预检，3 秒超时。
// 检查项：工具二进制存在且可执行、项目路径存在、模型配置已设置。
// 超时项标记为 true（不阻塞启动）。
func (p *SessionPrecheck) Check(toolName, projectPath string) PrecheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result := PrecheckResult{
		ToolReady:    true,
		ProjectReady: true,
		ModelReady:   true,
	}

	type checkResult struct {
		field string
		ok    bool
		hint  string
	}

	ch := make(chan checkResult, 3)

	// Check tool binary
	go func() {
		ok, hint := p.checkTool(toolName)
		ch <- checkResult{field: "tool", ok: ok, hint: hint}
	}()

	// Check project path
	go func() {
		ok := p.checkProject(projectPath)
		ch <- checkResult{field: "project", ok: ok}
	}()

	// Check model config
	go func() {
		ok, hint := p.checkModel(toolName)
		ch <- checkResult{field: "model", ok: ok, hint: hint}
	}()

	// Collect results; timeout items default to true (don't block startup)
	for i := 0; i < 3; i++ {
		select {
		case cr := <-ch:
			switch cr.field {
			case "tool":
				result.ToolReady = cr.ok
				if !cr.ok {
					result.ToolHint = cr.hint
				}
			case "project":
				result.ProjectReady = cr.ok
			case "model":
				result.ModelReady = cr.ok
				if !cr.ok {
					result.ModelHint = cr.hint
				}
			}
		case <-ctx.Done():
			// Timeout: remaining checks stay true (unknown → don't block).
			// Use goto to break out of the for loop, since break only
			// exits the select.
			goto done
		}
	}
done:

	result.AllPassed = result.ToolReady && result.ProjectReady && result.ModelReady
	return result
}

// checkTool verifies the tool binary is available.
// First checks exec.LookPath, then falls back to the tool catalog ReadinessHint.
func (p *SessionPrecheck) checkTool(toolName string) (bool, string) {
	normalized := normalizeRemoteToolName(toolName)

	// Try exec.LookPath for the binary name from the catalog
	meta, ok := lookupRemoteToolMetadata(normalized)
	if !ok {
		return false, "未知工具: " + toolName
	}

	_, err := exec.LookPath(meta.BinaryName)
	if err == nil {
		return true, ""
	}

	// Also check via ToolManager for more thorough detection
	tm := NewToolManager(p.app)
	status := tm.GetToolStatus(normalized)
	if status.Installed && strings.TrimSpace(status.Path) != "" {
		return true, ""
	}

	return false, meta.ReadinessHint
}

// checkProject verifies the project path exists and is accessible.
func (p *SessionPrecheck) checkProject(projectPath string) bool {
	if projectPath == "" {
		return false
	}
	_, err := os.Stat(projectPath)
	return err == nil
}

// checkModel verifies the tool has a model configured with an API key set.
func (p *SessionPrecheck) checkModel(toolName string) (bool, string) {
	cfg, err := p.app.LoadConfig()
	if err != nil {
		return false, "无法加载配置"
	}

	normalized := normalizeRemoteToolName(toolName)
	toolCfg, err := remoteToolConfig(cfg, normalized)
	if err != nil {
		return false, "未知工具配置: " + toolName
	}

	// Check if there's a current model selected and it has an API key
	if toolCfg.CurrentModel == "" && len(toolCfg.Models) == 0 {
		return false, "请为 " + remoteToolDisplayName(toolName) + " 配置模型和 API Key"
	}

	// Find the active model and check for API key
	for _, m := range toolCfg.Models {
		if m.ModelName == toolCfg.CurrentModel || toolCfg.CurrentModel == "" {
			if isValidProvider(m) {
				return true, ""
			}
			return false, "请为 " + remoteToolDisplayName(toolName) + " 的模型 " + m.ModelName + " 设置 API Key"
		}
	}

	return false, "请为 " + remoteToolDisplayName(toolName) + " 配置模型和 API Key"
}
