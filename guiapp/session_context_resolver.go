package guiapp

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/reposcanner"
	"golang.org/x/sync/singleflight"
)

const maxCachedRepoOverviews = 32

type overviewCacheKey struct {
	projectPath string
	tokenBudget int
}

type overviewCacheEntry struct {
	key      overviewCacheKey
	markdown string
}

type repoOverviewScanner func(string, reposcanner.ScanConfig) (*reposcanner.ScanResult, error)

// SessionContextResolver 自动推断会话启动参数
type SessionContextResolver struct {
	app *App

	overviewCache map[overviewCacheKey]*list.Element
	overviewLRU   *list.List
	overviewMu    sync.Mutex
	overviewSF    singleflight.Group
	scanOverview  repoOverviewScanner
}

// NewSessionContextResolver creates a new SessionContextResolver.
func NewSessionContextResolver(app *App) *SessionContextResolver {
	return &SessionContextResolver{
		app:           app,
		overviewCache: make(map[overviewCacheKey]*list.Element),
		overviewLRU:   list.New(),
	}
}

// ResolveProject 按优先级推断项目路径:
// (a) 当前桌面端打开的项目
// (b) 最近使用的项目
// (c) 默认项目
// 无法推断时返回空字符串，由调用方展示项目列表。
func (r *SessionContextResolver) ResolveProject() (projectPath string, reason string) {
	// Priority (a): currently open project in the desktop app
	current := r.app.GetCurrentProjectPath()
	if current != "" {
		return current, "当前打开的项目"
	}

	// Priority (b) & (c): load config for project list
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return "", ""
	}

	// Priority (b): most recently used project (first in the list)
	if len(cfg.Projects) > 0 {
		return cfg.Projects[0].Path, "最近使用的项目"
	}

	// Priority (c): default project via CurrentProject ID
	if cfg.CurrentProject != "" {
		for _, p := range cfg.Projects {
			if p.Id == cfg.CurrentProject {
				return p.Path, "默认项目"
			}
		}
	}

	return "", ""
}

// ResolveTool 根据三层优先级推荐工具：
// 1. 用户配置的默认工具 (AppConfig.DefaultTool)
// 2. 品牌默认工具 (BrandConfig.DefaultTool)
// 3. 项目语言/框架启发式推荐
// 检查工具目录的安装和健康状态进行推荐。
// 无法推荐时返回空字符串。
func (r *SessionContextResolver) ResolveTool(projectPath, taskDescription string) (toolName string, reason string) {
	toolManager := NewToolManager(r.app)

	// --- Tier 1: User-configured default tool ---
	cfg, err := r.app.LoadConfig()
	if err == nil {
		userDefault := strings.ToLower(strings.TrimSpace(cfg.DefaultTool))
		if userDefault != "" {
			if r.isToolAvailable(userDefault, toolManager) {
				return userDefault, "用户配置的默认工具"
			}
			r.app.log(fmt.Sprintf("[ResolveTool] user default tool %q not available, falling through", userDefault))
		}
	}

	// --- Tier 2: Brand default tool ---
	brandDefault := strings.ToLower(strings.TrimSpace(brand.Current().DefaultTool))
	if brandDefault != "" {
		if r.isToolAvailable(brandDefault, toolManager) {
			return brandDefault, "品牌默认工具"
		}
		r.app.log(fmt.Sprintf("[ResolveTool] brand default tool %q not available, falling through to heuristics", brandDefault))
	}

	// --- Tier 3: Existing heuristic logic ---
	type recommendation struct {
		tool   string
		reason string
	}

	var candidates []recommendation

	if projectPath != "" {
		switch {
		case fileExists(filepath.Join(projectPath, "go.mod")):
			candidates = []recommendation{
				{"opencode", "Go 项目推荐使用 OpenCode"},
				{"claude", "Go 项目推荐使用 Claude"},
			}
		case fileExists(filepath.Join(projectPath, "package.json")):
			candidates = []recommendation{
				{"claude", "Node.js 项目推荐使用 Claude"},
			}
		case fileExists(filepath.Join(projectPath, "requirements.txt")) || fileExists(filepath.Join(projectPath, "setup.py")):
			candidates = []recommendation{
				{"claude", "Python 项目推荐使用 Claude"},
			}
		}
	}

	// Default: claude as the most versatile tool
	if len(candidates) == 0 {
		candidates = []recommendation{
			{"claude", "默认推荐使用 Claude"},
		}
	}

	// Check if the recommended tool is installed and healthy
	for _, c := range candidates {
		status := toolManager.GetToolStatus(c.tool)
		if status.Installed && strings.TrimSpace(status.Path) != "" {
			return c.tool, c.reason
		}
	}

	// Fallback: try any installed tool from the catalog
	fallbackOrder := []string{"claude", "codex", "opencode", "codebuddy", "iflow", "kilo"}
	for _, name := range fallbackOrder {
		status := toolManager.GetToolStatus(name)
		if status.Installed && strings.TrimSpace(status.Path) != "" {
			return name, "已安装的可用工具"
		}
	}

	return "", ""
}

// isToolAvailable checks whether a tool exists in the catalog (including extra tools)
// and is installed and healthy.
func (r *SessionContextResolver) isToolAvailable(toolName string, toolManager *ToolManager) bool {
	// Check if tool exists in catalog (includes extra tools via brand.Current().ExtraTools)
	if _, ok := lookupRemoteToolMetadata(toolName); !ok {
		return false
	}
	// Check if tool is installed and healthy
	status := toolManager.GetToolStatus(toolName)
	return status.Installed && strings.TrimSpace(status.Path) != ""
}

// ResolveRepoOverview returns a compressed markdown overview of the project
// at the given path, suitable for LLM context injection. Results are cached
// per project path and token budget. Returns empty string if the path is empty or scanning fails.
func (r *SessionContextResolver) ResolveRepoOverview(projectPath string, tokenBudget int) string {
	if projectPath == "" {
		return ""
	}
	if tokenBudget <= 0 {
		tokenBudget = 3000
	}
	key := overviewCacheKey{projectPath: projectPath, tokenBudget: tokenBudget}
	if cached, ok := r.cachedOverview(key); ok {
		return cached
	}

	flightKey := projectPath + "\x00" + strconv.Itoa(tokenBudget)
	value, _, _ := r.overviewSF.Do(flightKey, func() (any, error) {
		if cached, ok := r.cachedOverview(key); ok {
			return cached, nil
		}
		cfg := reposcanner.DefaultScanConfig()
		cfg.TokenBudget = tokenBudget
		cfg.DeepMode = false

		scan := r.scanOverview
		if scan == nil {
			scan = func(path string, cfg reposcanner.ScanConfig) (*reposcanner.ScanResult, error) {
				return reposcanner.Scan(path, cfg, nil)
			}
		}
		result, err := scan(projectPath, cfg)
		markdown := ""
		if err == nil && result != nil {
			markdown = result.CompressedMD
		}
		r.cacheOverview(key, markdown)
		return markdown, nil
	})
	markdown, _ := value.(string)
	return markdown
}

func (r *SessionContextResolver) cachedOverview(key overviewCacheKey) (string, bool) {
	r.overviewMu.Lock()
	defer r.overviewMu.Unlock()
	element, ok := r.overviewCache[key]
	if !ok {
		return "", false
	}
	r.overviewLRU.MoveToFront(element)
	return element.Value.(*overviewCacheEntry).markdown, true
}

func (r *SessionContextResolver) cacheOverview(key overviewCacheKey, markdown string) {
	r.overviewMu.Lock()
	defer r.overviewMu.Unlock()
	if r.overviewCache == nil {
		r.overviewCache = make(map[overviewCacheKey]*list.Element)
	}
	if r.overviewLRU == nil {
		r.overviewLRU = list.New()
	}
	if element, ok := r.overviewCache[key]; ok {
		element.Value.(*overviewCacheEntry).markdown = markdown
		r.overviewLRU.MoveToFront(element)
		return
	}
	element := r.overviewLRU.PushFront(&overviewCacheEntry{key: key, markdown: markdown})
	r.overviewCache[key] = element
	for len(r.overviewCache) > maxCachedRepoOverviews {
		oldest := r.overviewLRU.Back()
		entry := oldest.Value.(*overviewCacheEntry)
		delete(r.overviewCache, entry.key)
		r.overviewLRU.Remove(oldest)
	}
}

// fileExists returns true if the path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
