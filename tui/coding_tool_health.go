package main

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// codingToolStatus 表示编程工具的健康状态。
type codingToolStatus int

const (
	codingToolUnknown     codingToolStatus = iota // 未检查
	codingToolAvailable                           // 可用
	codingToolUnavailable                         // 不可用（配置/安装问题）
	codingToolAuthFailed                          // 运行时认证失败（API Key 过期等）
)

func (s codingToolStatus) String() string {
	switch s {
	case codingToolAvailable:
		return "available"
	case codingToolUnavailable:
		return "unavailable"
	case codingToolAuthFailed:
		return "auth_failed"
	default:
		return "unknown"
	}
}

// codingToolCheck 缓存单个编程工具的检查结果。
type codingToolCheck struct {
	Status    codingToolStatus
	Reason    string    // 不可用原因
	CheckedAt time.Time // 检查时间
}

// codingToolHealthCache 管理所有编程工具的健康状态缓存。
type codingToolHealthCache struct {
	mu    sync.RWMutex
	tools map[string]*codingToolCheck
}

func newCodingToolHealthCache() *codingToolHealthCache {
	return &codingToolHealthCache{
		tools: make(map[string]*codingToolCheck),
	}
}

// Get 获取工具的缓存状态。
func (c *codingToolHealthCache) Get(toolName string) (*codingToolCheck, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.tools[normalizeToolName(toolName)]
	return check, ok
}

// Set 设置工具的健康状态。
func (c *codingToolHealthCache) Set(toolName string, status codingToolStatus, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[normalizeToolName(toolName)] = &codingToolCheck{
		Status:    status,
		Reason:    reason,
		CheckedAt: time.Now(),
	}
}

// MarkAuthFailed 标记工具认证失败（运行时检测到）。
func (c *codingToolHealthCache) MarkAuthFailed(toolName, reason string) {
	name := normalizeToolName(toolName)
	c.Set(name, codingToolAuthFailed, reason)
	log.Printf("[coding-tool-health] %s 标记为认证失败: %s", name, reason)
}

// IsAvailable 检查工具是否可用（已检查且状态为 available）。
func (c *codingToolHealthCache) IsAvailable(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.tools[normalizeToolName(toolName)]
	return ok && check.Status == codingToolAvailable
}

// IsBlocked 检查工具是否被阻止使用（不可用或认证失败）。
func (c *codingToolHealthCache) IsBlocked(toolName string) (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.tools[normalizeToolName(toolName)]
	if !ok {
		return false, "" // 未检查，不阻止
	}
	switch check.Status {
	case codingToolUnavailable, codingToolAuthFailed:
		return true, check.Reason
	default:
		return false, ""
	}
}

// UnavailableToolsSummary 返回所有不可用工具的摘要（用于 system prompt）。
// 按工具名排序以保证输出稳定。
func (c *codingToolHealthCache) UnavailableToolsSummary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// 收集不可用工具名并排序
	var names []string
	for name, check := range c.tools {
		if check.Status == codingToolUnavailable || check.Status == codingToolAuthFailed {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		check := c.tools[name]
		parts = append(parts, fmt.Sprintf("- %s: %s (%s)", name, check.Reason, check.Status))
	}
	return strings.Join(parts, "\n")
}

// checkCodingToolHealth 对指定编程工具执行预检查。
// 检查内容：1) 配置是否存在且有效  2) 二进制是否已安装
// 返回 true 表示可用。
func checkCodingToolHealth(toolName string) (bool, string) {
	name := normalizeToolName(toolName)

	// 1. 加载配置
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return false, fmt.Sprintf("加载配置失败: %v", err)
	}

	// 2. 检查工具配置（provider + API Key）
	tc, err := toolConfigFromApp(cfg, name)
	if err != nil {
		return false, fmt.Sprintf("工具配置不存在: %v", err)
	}

	selected := resolveSelectedModel(tc)
	if selected == nil {
		return false, fmt.Sprintf("工具 %s 未配置 provider，请先在设置中配置", name)
	}
	if !isValidToolProvider(*selected) {
		return false, fmt.Sprintf("工具 %s 的 provider %q 未配置 API Key", name, selected.ModelName)
	}

	// 3. 检查二进制是否安装
	if !isCodingToolBinaryAvailable(name) {
		return false, fmt.Sprintf("工具 %s 未安装（二进制文件未找到）", name)
	}

	return true, ""
}

// isCodingToolBinaryAvailable 检查编程工具的二进制文件是否可用。
func isCodingToolBinaryAvailable(toolName string) bool {
	// 先查私有安装目录
	if _, found := remote.ResolveToolPath(toolName); found {
		return true
	}
	// 再查 PATH
	binNames := remote.BinaryNames(toolName)
	for _, bn := range binNames {
		if _, err := exec.LookPath(bn); err == nil {
			return true
		}
	}
	return false
}

// authFailurePatterns 是运行时认证失败的关键词模式。
// 使用较具体的短语避免误匹配（如端口号 8401、文件路径等）。
var authFailurePatterns = []string{
	"401 unauthorized",
	"http 401",
	"status 401",
	"status: 401",
	"unauthorized",
	"invalid api key",
	"invalid_api_key",
	"api key is invalid",
	"api key expired",
	"token expired",
	"invalid token",
	"invalid_auth",
	"authentication failed",
	"auth error",
	"could not authenticate",
	"not authenticated",
	"permission denied",
	"access denied",
	"403 forbidden",
}

// DetectAuthFailure 检查输出文本中是否包含认证失败的信号。
// 返回 true 和匹配的模式（如果检测到认证失败）。
func DetectAuthFailure(output string) (bool, string) {
	lower := strings.ToLower(output)
	for _, pattern := range authFailurePatterns {
		if strings.Contains(lower, pattern) {
			return true, pattern
		}
	}
	return false, ""
}

// codingToolFallbackHint 返回编程工具不可用时给 LLM 的提示。
func codingToolFallbackHint(toolName, reason string) string {
	return fmt.Sprintf(
		"⚠️ 编程工具 %s 当前不可用（%s）。\n"+
			"请使用 bash、read_file、write_file 等基础工具自行完成编程任务。\n"+
			"如果任务无法在没有编程工具的情况下完成，请明确告知用户。",
		toolName, reason,
	)
}


