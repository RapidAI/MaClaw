package main

// openhuman_background.go wires the background engines (MemoryTree seal,
// Subconscious, AutoFetch) into the GUI App lifecycle.
//
// These engines run as background goroutines and are started after the
// interaction infrastructure is ready (not at cold startup).

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/memory/tree"
)

// --- C1: Memory Tree Seal Scheduler ---

// startMemoryTreeSealScheduler runs daily/weekly/monthly seal jobs.
// Called from initOpenHumanModules after memory store is available.
func (a *App) startMemoryTreeSealScheduler() {
	// Wait for memory store to be available (it's lazy-initialized)
	for i := 0; i < 60; i++ {
		if a.memoryStore != nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if a.memoryStore == nil {
		log.Printf("[memory-tree] memory store not available after 5 minutes, seal scheduler not started")
		return
	}

	storePath := filepath.Join(corelib.MaclawBaseDir(), "data", "memory_tree")
	if err := os.MkdirAll(storePath, 0755); err != nil {
		log.Printf("[memory-tree] failed to create dir: %v", err)
		return
	}

	store := newJSONTreeStore(storePath)
	config := tree.DefaultSealConfig()

	// Use LLM summarizer if available, otherwise concatenate
	var summarizer tree.Summarizer
	if a.isMaclawLLMConfigured() {
		summarizer = func(content string) (string, error) {
			return a.llmSummarizeForTree(content)
		}
	}

	sealer := tree.NewSealer(store, summarizer, config)

	go func() {
		// Initial seal on startup (catch up on missed seals)
		time.Sleep(2 * time.Minute) // wait for memory store to be populated
		ctx := context.Background()
		_ = ctx

		yesterday := time.Now().Add(-24 * time.Hour)
		if n, err := sealer.SealDaily(yesterday); err == nil && n > 0 {
			log.Printf("[memory-tree] startup: sealed %d daily nodes for yesterday", n)
		}

		// Periodic seal: check every 6 hours
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			// Daily seal for yesterday
			if n, _ := sealer.SealDaily(now.Add(-24 * time.Hour)); n > 0 {
				log.Printf("[memory-tree] sealed %d daily nodes", n)
			}
			// Weekly seal on Mondays
			if now.Weekday() == time.Monday {
				weekStart := now.Add(-7 * 24 * time.Hour)
				if n, _ := sealer.SealWeekly(weekStart); n > 0 {
					log.Printf("[memory-tree] sealed %d weekly nodes", n)
				}
			}
			// Monthly seal on 1st
			if now.Day() == 1 {
				monthStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
				if n, _ := sealer.SealMonthly(monthStart); n > 0 {
					log.Printf("[memory-tree] sealed %d monthly nodes", n)
				}
			}
		}
	}()
	log.Printf("[memory-tree] seal scheduler started")
}

// llmSummarizeForTree uses the configured LLM to summarize content for tree sealing.
func (a *App) llmSummarizeForTree(content string) (string, error) {
	cfg := a.GetMaclawLLMConfig()
	if cfg.URL == "" || cfg.Model == "" {
		return "", nil
	}
	// Truncate input to avoid exceeding context
	runes := []rune(content)
	if len(runes) > 8000 {
		content = string(runes[:8000]) + "\n...(truncated)"
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": "你是一个知识摘要助手。请将以下内容压缩为简洁的摘要（≤500字），保留关键事实、决策和数据。"},
		map[string]string{"role": "user", "content": content},
	}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "memory-tree-background"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, a.managers.HTTPClient(), 30*time.Second)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// --- C2: Subconscious Engine Startup ---

// startSubconsciousEngine initializes and starts the background reflection engine.
func (a *App) startSubconsciousEngine() {
	// Wait for memory store to be available
	for i := 0; i < 60; i++ {
		if a.memoryStore != nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if a.memoryStore == nil {
		return
	}
	cfg := memory.DefaultSubconsciousConfig()
	engine := memory.NewSubconsciousEngine(a.memoryStore, cfg)
	// Subsystems will be wired when LLM-based implementations are available.
	// For now the engine runs but subsystems are nil (no-op ticks).
	engine.Start()
	a.ohModules.subconsciousEngine = engine
	log.Printf("[subconscious] engine started (interval=%s)", cfg.Interval)
}

// --- C3: AutoFetch Engine with RSS Connector ---

// startAutoFetchEngine initializes the auto-fetch engine with configured connectors.
func (a *App) startAutoFetchEngine() {
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.AutoFetchEnabled {
		return
	}

	interval := 20 * time.Minute
	if cfg.AutoFetchIntervalMin > 0 {
		interval = time.Duration(cfg.AutoFetchIntervalMin) * time.Minute
	}

	sink := func(items []agent.DataItem) error {
		if a.memoryStore == nil {
			return nil
		}
		for _, item := range items {
			tags := append(append([]string{}, item.Tags...), "auto_fetch", item.Source)
			identityTagCount := len(tags)
			if strings.TrimSpace(item.URL) != "" {
				tags = append(tags, "url:"+strings.TrimSpace(item.URL))
				identityTagCount = 3
			}
			_, _ = a.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
				Title:            item.Title,
				Content:          item.Title + "\n\n" + item.Content,
				Tags:             tags,
				IdentityTagCount: identityTagCount,
				Scope:            memory.ScopeProject,
				SourceType:       item.Source,
				SourceURL:        item.URL,
			})
		}
		return nil
	}

	engine := agent.NewAutoFetchEngine(sink, interval)

	// Add RSS connector if configured
	if len(cfg.AutoFetchRSSFeeds) > 0 {
		engine.AddConnector(&rssConnector{feeds: cfg.AutoFetchRSSFeeds})
	}

	// Add file watch connector if configured
	if len(cfg.AutoFetchWatchDirs) > 0 {
		engine.AddConnector(&fileWatchConnector{dirs: cfg.AutoFetchWatchDirs})
	}

	engine.Start()
	a.ohModules.autoFetchEngine = engine
	log.Printf("[auto-fetch] engine started (interval=%s, connectors=%d)", interval, len(cfg.AutoFetchRSSFeeds)+len(cfg.AutoFetchWatchDirs))
}

// --- RSS Connector ---

type rssConnector struct {
	feeds []string
}

func (c *rssConnector) Name() string       { return "rss" }
func (c *rssConnector) IsConfigured() bool { return len(c.feeds) > 0 }
func (c *rssConnector) FetchNew(ctx context.Context, since time.Time) ([]agent.DataItem, error) {
	// Minimal RSS fetch implementation — reads each feed URL and extracts items.
	// Full XML parsing would use encoding/xml; for now we do a simple title extraction.
	var items []agent.DataItem
	for _, feedURL := range c.feeds {
		fetched := fetchRSSFeed(ctx, feedURL, since)
		items = append(items, fetched...)
	}
	return items, nil
}

func fetchRSSFeed(ctx context.Context, feedURL string, since time.Time) []agent.DataItem {
	// Lightweight RSS fetch — uses the existing web fetch infrastructure.
	// In production this would use encoding/xml to parse RSS/Atom properly.
	// For now, return empty (connector is registered but passive until
	// a proper RSS parser is added).
	_ = ctx
	_ = feedURL
	_ = since
	return nil
}

// --- File Watch Connector ---

type fileWatchConnector struct {
	dirs []string
}

func (c *fileWatchConnector) Name() string       { return "file_watch" }
func (c *fileWatchConnector) IsConfigured() bool { return len(c.dirs) > 0 }
func (c *fileWatchConnector) FetchNew(ctx context.Context, since time.Time) ([]agent.DataItem, error) {
	var items []agent.DataItem
	for _, dir := range c.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().Before(since) {
				continue
			}
			// Only process text files
			name := entry.Name()
			if !isTextFile(name) {
				continue
			}
			path := filepath.Join(dir, name)
			content, err := os.ReadFile(path)
			if err != nil || len(content) == 0 {
				continue
			}
			// Truncate large files
			text := string(content)
			if len([]rune(text)) > 2000 {
				text = string([]rune(text)[:2000]) + "\n..."
			}
			items = append(items, agent.DataItem{
				Source:    "file_watch",
				Title:     name,
				Content:   text,
				Timestamp: info.ModTime(),
				Tags:      []string{filepath.Ext(name)},
			})
		}
	}
	return items, nil
}

func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".csv", ".log":
		return true
	}
	return false
}

// --- JSON-based TreeStore implementation ---

type jsonTreeStore struct {
	dir string
}

func newJSONTreeStore(dir string) *jsonTreeStore {
	return &jsonTreeStore{dir: dir}
}

func (s *jsonTreeStore) Save(node *tree.TreeNode) error {
	// Minimal implementation: store nodes as individual JSON files
	data, err := jsonMarshalIndent(node)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, node.ID+".json")
	return os.WriteFile(path, data, 0644)
}

func (s *jsonTreeStore) Get(id string) (*tree.TreeNode, error) {
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var node tree.TreeNode
	if err := jsonUnmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

func (s *jsonTreeStore) ListByLevel(level tree.TreeLevel) ([]*tree.TreeNode, error) {
	return s.scanNodes(func(n *tree.TreeNode) bool { return n.Level == level })
}

func (s *jsonTreeStore) ListByLevelAndDate(level tree.TreeLevel, from, to time.Time) ([]*tree.TreeNode, error) {
	return s.scanNodes(func(n *tree.TreeNode) bool {
		return n.Level == level && !n.CreatedAt.Before(from) && n.CreatedAt.Before(to)
	})
}

func (s *jsonTreeStore) ListChildren(parentID string) ([]*tree.TreeNode, error) {
	return s.scanNodes(func(n *tree.TreeNode) bool { return n.ParentID == parentID })
}

func (s *jsonTreeStore) Delete(id string) error {
	return os.Remove(filepath.Join(s.dir, id+".json"))
}

func (s *jsonTreeStore) Search(query string, maxResults int) ([]*tree.TreeNode, error) {
	query = strings.ToLower(query)
	return s.scanNodes(func(n *tree.TreeNode) bool {
		return strings.Contains(strings.ToLower(n.Content), query)
	})
}

func (s *jsonTreeStore) scanNodes(filter func(*tree.TreeNode) bool) ([]*tree.TreeNode, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []*tree.TreeNode
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var node tree.TreeNode
		if err := jsonUnmarshal(data, &node); err != nil {
			continue
		}
		if filter(&node) {
			result = append(result, &node)
		}
	}
	return result, nil
}

// --- JSON helpers (avoid import conflicts with other files in package) ---

func jsonMarshalIndent(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// --- C4: ForkContext integration for CodingSubAgent ---
// The ForkableContext is created once per orchestrator run (shared system prompt
// + tool definitions), and each task gets a Fork with its own conversation.
// This enables KV-cache reuse across tasks when the LLM provider supports it.

// buildCodingSubAgentForkablePrefix constructs the shared prefix for coding tasks.
// This includes the coding system prompt and tool definitions — content that is
// identical across all tasks in an orchestrator run.
func buildCodingSubAgentForkablePrefix(systemPrompt string) []interface{} {
	return []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
	}
}
