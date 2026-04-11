package commands

import (
	"context"
	"flag"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// RunMemory 执行 memory 子命令。
func RunMemory(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory <list|search|save|delete|compress|backup|auto-compress|stats|embed-status|graph|strength>")
	}
	switch args[0] {
	case "list":
		return memoryList(dataDir, args[1:])
	case "search":
		return memorySearch(dataDir, args[1:])
	case "save":
		return memorySave(dataDir, args[1:])
	case "delete":
		return memoryDelete(dataDir, args[1:])
	case "compress":
		return memoryCompress(dataDir, args[1:])
	case "backup":
		return memoryBackup(dataDir, args[1:])
	case "auto-compress":
		return memoryAutoCompress(dataDir, args[1:])
	case "stats":
		return memoryStats(dataDir)
	case "embed-status":
		return memoryEmbedStatus(dataDir)
	case "graph":
		return memoryGraph(dataDir, args[1:])
	case "strength":
		return memoryStrength(dataDir)
	default:
		return NewUsageError("unknown memory action: %s", args[0])
	}
}

func openMemoryStore(dataDir string) (*memory.Store, error) {
	path := filepath.Join(dataDir, "memories.json")
	return memory.NewStore(path)
}

func memoryList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory list", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词搜索")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.List(memory.Category(*category), *keyword)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := e.Content
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memorySearch(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory search", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词")
	limit := fs.Int("limit", 10, "最大返回条数")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	// 允许直接传关键词作为位置参数
	kw := *keyword
	if kw == "" && fs.NArg() > 0 {
		kw = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.Search(memory.Category(*category), kw, *limit)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memorySave(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory save", flag.ExitOnError)
	content := fs.String("content", "", "记忆内容（必填）")
	category := fs.String("category", "user_fact", "分类")
	tags := fs.String("tags", "", "标签（逗号分隔）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *content == "" {
		return NewUsageError("usage: memory save --content <text> [--category <cat>] [--tags <t1,t2>]")
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	var tagList []string
	if *tags != "" {
		tagList = strings.Split(*tags, ",")
	}

	entry := memory.Entry{
		Content:  *content,
		Category: memory.Category(*category),
		Tags:     tagList,
	}
	if err := store.Save(entry); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"status": "saved"})
	}
	fmt.Println("Memory saved.")
	return nil
}

func memoryDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: memory delete <id>")
	}
	id := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	if err := store.Delete(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "deleted"})
	}
	fmt.Printf("Memory %s deleted.\n", id)
	return nil
}

func memoryCompress(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory compress", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	// 无 LLM 时仅做 dedup（传 nil LLM）
	compressor := memory.NewCompressor(store, nil, nil)
	result, err := compressor.Compress(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(result)
	}
	fmt.Printf("Memory Compress Result:\n")
	fmt.Printf("  Total entries:  %d\n", result.TotalEntries)
	fmt.Printf("  Dedup removed:  %d\n", result.DedupCount)
	fmt.Printf("  Merged:         %d\n", result.MergedCount)
	fmt.Printf("  Compressed:     %d\n", result.CompressedCount)
	fmt.Printf("  Skipped:        %d\n", result.SkippedCount)
	fmt.Printf("  Errors:         %d\n", result.ErrorCount)
	fmt.Printf("  Saved chars:    %d\n", result.SavedChars)
	if result.BackupName != "" {
		fmt.Printf("  Backup:         %s\n", result.BackupName)
	}
	return nil
}

func memoryBackup(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory backup <list|restore|delete>")
	}
	switch args[0] {
	case "list":
		return memoryBackupList(dataDir, args[1:])
	case "restore":
		return memoryBackupRestore(dataDir, args[1:])
	case "delete":
		return memoryBackupDelete(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory backup action: %s", args[0])
	}
}

func memoryBackupList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	backups, err := compressor.ListBackups()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(backups)
	}
	if len(backups) == 0 {
		fmt.Println("No backups found.")
		return nil
	}
	fmt.Printf("%-40s %-22s %-10s %s\n", "NAME", "CREATED", "SIZE", "ENTRIES")
	fmt.Println(strings.Repeat("-", 85))
	for _, b := range backups {
		fmt.Printf("%-40s %-22s %-10d %d\n", b.Name, b.CreatedAt, b.SizeBytes, b.EntryCount)
	}
	return nil
}

func memoryBackupRestore(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup restore", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup restore <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.RestoreBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s restored.\n", name)
	return nil
}

func memoryBackupDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup delete", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup delete <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.DeleteBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s deleted.\n", name)
	return nil
}

func memoryAutoCompress(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory auto-compress <on|off|status>")
	}
	store := NewFileConfigStore(dataDir)
	switch args[0] {
	case "on":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		cfg.MemoryAutoCompress = true
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("自动压缩已开启。")
		return nil
	case "off":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		cfg.MemoryAutoCompress = false
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("自动压缩已关闭。")
		return nil
	case "status":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		if cfg.MemoryAutoCompress {
			fmt.Println("自动压缩: 开启")
		} else {
			fmt.Println("自动压缩: 关闭")
		}
		return nil
	default:
		return NewUsageError("usage: maclaw-tui memory auto-compress <on|off|status>")
	}
}

func memoryStats(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.List("", "")
	total := len(entries)
	var active, dormant, superseded int
	var withEmb, withGraph int
	scopeGlobal, scopeProject := 0, 0
	catCounts := make(map[memory.Category]int)
	tierSemantic, tierEpisodic := 0, 0

	for _, e := range entries {
		catCounts[e.Category]++
		switch e.Status {
		case memory.StatusDormant:
			dormant++
		case memory.StatusSuperseded:
			superseded++
		default:
			active++
		}
		if len(e.Embedding) > 0 {
			withEmb++
		}
		if len(e.RelatedIDs) > 0 {
			withGraph++
		}
		if e.Scope == memory.ScopeGlobal {
			scopeGlobal++
		} else {
			scopeProject++
		}
		if e.Category.Tier() == memory.TierSemantic {
			tierSemantic++
		} else {
			tierEpisodic++
		}
	}

	fmt.Printf("Memory Store Stats:\n")
	fmt.Printf("  Total entries:    %d\n", total)
	fmt.Printf("  Active:           %d\n", active)
	fmt.Printf("  Dormant:          %d\n", dormant)
	fmt.Printf("  Superseded:       %d\n", superseded)
	fmt.Printf("  With embedding:   %d\n", withEmb)
	fmt.Printf("  With graph links: %d\n", withGraph)
	fmt.Printf("  Scope global:     %d\n", scopeGlobal)
	fmt.Printf("  Scope project:    %d\n", scopeProject)
	fmt.Printf("  Tier semantic:    %d\n", tierSemantic)
	fmt.Printf("  Tier episodic:    %d\n", tierEpisodic)
	fmt.Printf("  Categories:\n")
	for cat, count := range catCounts {
		fmt.Printf("    %-25s %d\n", cat, count)
	}
	return nil
}

func memoryEmbedStatus(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	store.RLock()
	entries := store.Entries()
	total := len(entries)
	withEmb := 0
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			withEmb++
		}
	}
	store.RUnlock()

	embedder := store.Embedder()
	embedderType := "Noop"
	modelPath := "(none)"
	if embedder != nil && !embedding.IsNoop(embedder) {
		embedderType = "Gemma"
		modelPath = embedding.DefaultModelPath()
	}

	fmt.Printf("Embedding Status:\n")
	fmt.Printf("  Total entries:           %d\n", total)
	fmt.Printf("  With embeddings:         %d\n", withEmb)
	fmt.Printf("  Without embeddings:      %d\n", total-withEmb)
	fmt.Printf("  Embedder type:           %s\n", embedderType)
	fmt.Printf("  Model path:              %s\n", modelPath)
	return nil
}

func memoryGraph(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory graph <id>")
	}
	id := args[0]

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	neighbors := store.GraphNeighbors(id)
	if len(neighbors) == 0 {
		fmt.Printf("No graph neighbors for entry %s.\n", id)
		return nil
	}

	// Build entry lookup for content preview.
	store.RLock()
	entryByID := make(map[string]*memory.Entry)
	for i := range store.Entries() {
		entryByID[store.Entries()[i].ID] = &store.Entries()[i]
	}
	store.RUnlock()

	fmt.Printf("Graph neighbors for %s:\n\n", id)
	fmt.Printf("%-26s %-10s %s\n", "NEIGHBOR", "STRENGTH", "CONTENT")
	fmt.Println(strings.Repeat("-", 76))

	// Sort neighbor IDs for stable output.
	type neighborInfo struct {
		id       string
		strength float64
	}
	sorted := make([]neighborInfo, 0, len(neighbors))
	for nid, str := range neighbors {
		sorted = append(sorted, neighborInfo{id: nid, strength: str})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].strength > sorted[j].strength
	})

	for _, n := range sorted {
		content := "(not found)"
		if e, ok := entryByID[n.id]; ok {
			content = strings.ReplaceAll(e.Content, "\n", " ")
			if len(content) > 36 {
				content = content[:33] + "..."
			}
		}
		fmt.Printf("%-26s %-10.4f %s\n", n.id, n.strength, content)
	}
	return nil
}

func memoryStrength(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	store.RLock()
	entries := make([]memory.Entry, len(store.Entries()))
	copy(entries, store.Entries())
	store.RUnlock()

	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}

	now := time.Now()

	type strengthEntry struct {
		entry    memory.Entry
		current  float64
		dormant  bool
	}

	items := make([]strengthEntry, 0, len(entries))
	for _, e := range entries {
		cur := e.Strength
		if cur > 0 {
			hours := now.Sub(e.UpdatedAt).Hours()
			if hours < 0 {
				hours = 0
			}
			cur = e.Strength * math.Exp(-0.003*hours)
		}
		isDormant := cur < 0.1 && e.Status != memory.StatusSuperseded && !e.Category.IsProtected()
		items = append(items, strengthEntry{entry: e, current: cur, dormant: isDormant})
	}

	// Sort by current strength ascending (weakest first).
	sort.Slice(items, func(i, j int) bool {
		return items[i].current < items[j].current
	})

	fmt.Printf("%-26s %-10s %-12s %-20s %s\n", "ID", "STRENGTH", "STATUS", "LAST ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 96))

	for _, it := range items {
		status := string(it.entry.Status)
		if status == "" {
			status = "active"
		}
		marker := "  "
		if it.dormant || it.entry.Status == memory.StatusDormant {
			marker = "⚠ "
		}

		content := strings.ReplaceAll(it.entry.Content, "\n", " ")
		if len(content) > 24 {
			content = content[:21] + "..."
		}

		lastAccess := it.entry.UpdatedAt.Format("2006-01-02 15:04")

		fmt.Printf("%s%-24s %-10.4f %-12s %-20s %s\n",
			marker, it.entry.ID, it.current, status, lastAccess, content)
	}
	return nil
}
