package commands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"os"
	"strings"
	"time"
)

// RunMemory 执行 memory 子命令。
func RunMemory(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory <list|search|recall|candidates|themes|eval|save|delete|compress|backup|auto-compress|stats|embed-status|graph|strength|infer>")
	}
	switch args[0] {
	case "list":
		return memoryList(dataDir, args[1:])
	case "search":
		return memorySearch(dataDir, args[1:])
	case "recall":
		return memoryRecall(dataDir, args[1:])
	case "candidates":
		return memoryCandidates(dataDir, args[1:])
	case "themes":
		return memoryThemes(dataDir, args[1:])
	case "eval":
		return memoryEval(dataDir, args[1:])
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
	case "infer":
		return memoryInfer(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory action: %s", args[0])
	}
}

func openMemoryStore(dataDir string) (*memory.Store, error) {
	return memory.OpenDataDirStore(dataDir, memory.StoreModeAuto)
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
		Println("No memories found.")
		return nil
	}
	Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := e.Content
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
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
		Println("No memories found.")
		return nil
	}
	Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memoryRecall(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory recall", flag.ExitOnError)
	category := fs.String("category", "", "filter by category")
	query := fs.String("query", "", "recall query")
	limit := fs.Int("limit", 10, "max results")
	mode := fs.String("mode", "hybrid", "recall mode: hybrid|lightmem|adaptive|auto")
	project := fs.String("project", "", "project path for scoped recall")
	debug := fs.Bool("debug", false, "include adaptive recall debug plan")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	q := *query
	if q == "" && fs.NArg() > 0 {
		q = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	result, err := memoryRecallResultByMode(store, q, memory.Category(*category), *mode, *project, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		if *debug {
			return PrintJSON(result)
		}
		return PrintJSON(result.Entries)
	}
	formatted := memory.FormatRecallResultForTool(store, q, result, *debug, true)
	if formatted == "No relevant memories found." {
		Println("No memories found.")
		return nil
	}
	Print(formatted)
	return nil
}

func memoryRecallResultByMode(store *memory.Store, query string, category memory.Category, mode string, projectPath string, limit int) (memory.ToolRecallResult, error) {
	return store.RecallByMode(query, category, mode, projectPath, limit)
}

func memoryRecallByMode(store *memory.Store, query string, category memory.Category, mode string, projectPath string, limit int) ([]memory.Entry, *memory.AdaptiveRecallPlan, error) {
	result, err := memoryRecallResultByMode(store, query, category, mode, projectPath, limit)
	if err != nil {
		return nil, nil, err
	}
	return result.Entries, result.AdaptivePlan, nil
}

func memoryCandidates(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory candidates", flag.ExitOnError)
	keyword := fs.String("keyword", "", "filter by keyword")
	limit := fs.Int("limit", 50, "max candidates")
	apply := fs.Bool("apply", false, "run candidate consolidation before listing")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	kw := *keyword
	if kw == "" && fs.NArg() > 0 {
		kw = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	result := store.MemoryCandidatesForTool(context.Background(), kw, *limit, *apply)

	if *jsonOut {
		return PrintJSON(result)
	}

	Print(memory.FormatMemoryCandidatesResultForTool(result))
	return nil
}

func memoryThemes(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory themes", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max themes")
	stats := fs.Bool("stats", false, "include theme health stats")
	evidence := fs.Int("evidence", 0, "representative evidence entries per theme")
	diagnose := fs.Bool("diagnose", false, "include actionable theme diagnostics")
	issueLimit := fs.Int("issue-limit", 50, "max diagnostic issues")
	plan := fs.Bool("plan", false, "include non-destructive theme maintenance plan")
	apply := fs.Bool("apply", false, "apply safe theme maintenance actions")
	actionLimit := fs.Int("action-limit", 20, "max maintenance actions")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	themeOpts := memory.ToolThemesOptions{
		Limit:         *limit,
		Stats:         *stats,
		EvidenceLimit: *evidence,
		Diagnose:      *diagnose,
		IssueLimit:    *issueLimit,
		Plan:          *plan,
		Apply:         *apply,
		ActionLimit:   *actionLimit,
	}
	result := store.MemoryThemesForTool(themeOpts)

	if *jsonOut {
		return PrintJSON(memory.MemoryThemesJSONPayloadForTool(result, themeOpts))
	}
	Print(memory.FormatMemoryThemesResultForTool(result, themeOpts))
	return nil
}
func memoryEval(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory eval", flag.ExitOnError)
	casesPath := fs.String("cases", "", "JSON file with recall eval cases")
	limit := fs.Int("limit", 10, "max recall results per strategy")
	maintenance := fs.Bool("maintenance", false, "apply safe theme maintenance and re-run eval")
	issueLimit := fs.Int("issue-limit", 50, "max diagnostic issues for maintenance eval")
	actionLimit := fs.Int("action-limit", 20, "max maintenance actions for maintenance eval")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	if *casesPath == "" && fs.NArg() > 0 {
		*casesPath = fs.Arg(0)
	}
	if *casesPath == "" {
		return NewUsageError("usage: memory eval --cases <cases.json> [--limit N] [--json]")
	}

	cases, err := loadRecallEvalCases(*casesPath)
	if err != nil {
		return err
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	if *maintenance {
		report := store.EvaluateRecallStrategiesWithMaintenance(cases, *limit, *issueLimit, *actionLimit)
		if *jsonOut {
			return PrintJSON(report)
		}
		Print(memory.FormatRecallMaintenanceEvalReportForTool(report))
		return nil
	}

	report := store.EvaluateRecallStrategies(cases, *limit)
	if *jsonOut {
		return PrintJSON(report)
	}
	Print(memory.FormatRecallEvalReportForTool(report))
	return nil
}

func loadRecallEvalCases(path string) ([]memory.RecallEvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []memory.RecallEvalCase
	if err := json.Unmarshal(data, &cases); err == nil {
		return cases, nil
	}
	var wrapper struct {
		Cases []memory.RecallEvalCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Cases, nil
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

	if err := store.SaveManualMemory(*content, memory.Category(*category), tagList); err != nil {
		return err
	}
	out := memory.FormatMemorySavedForTool(*content)
	if *jsonOut {
		return PrintJSON(map[string]string{"status": "saved", "message": out})
	}
	Println(out)
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

	out := memory.HandleTool(store, map[string]interface{}{
		"action": "delete",
		"id":     id,
	}, memory.ToolOptions{})
	if strings.HasPrefix(out, "delete memory failed:") || strings.HasPrefix(out, "missing ") {
		return fmt.Errorf("%s", out)
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "deleted", "message": out})
	}
	Println(out)
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
	maintenance := memory.NewMaintenance(store, nil, nil)
	result, err := maintenance.Compress(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(result)
	}
	Printf("Memory Compress Result:\n")
	Printf("  Total entries:  %d\n", result.TotalEntries)
	Printf("  Dedup removed:  %d\n", result.DedupCount)
	Printf("  Merged:         %d\n", result.MergedCount)
	Printf("  Compressed:     %d\n", result.CompressedCount)
	Printf("  Skipped:        %d\n", result.SkippedCount)
	Printf("  Errors:         %d\n", result.ErrorCount)
	Printf("  Saved chars:    %d\n", result.SavedChars)
	if result.BackupName != "" {
		Printf("  Backup:         %s\n", result.BackupName)
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

	maintenance := memory.NewMaintenance(store, nil, nil)
	backups, err := maintenance.ListBackups()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(backups)
	}
	if len(backups) == 0 {
		Println("No backups found.")
		return nil
	}
	Printf("%-40s %-22s %-10s %s\n", "NAME", "CREATED", "SIZE", "ENTRIES")
	Println(strings.Repeat("-", 85))
	for _, b := range backups {
		Printf("%-40s %-22s %-10d %d\n", b.Name, b.CreatedAt, b.SizeBytes, b.EntryCount)
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

	maintenance := memory.NewMaintenance(store, nil, nil)
	if err := maintenance.RestoreBackup(name); err != nil {
		return err
	}
	Printf("Backup %s restored.\n", name)
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

	maintenance := memory.NewMaintenance(store, nil, nil)
	if err := maintenance.DeleteBackup(name); err != nil {
		return err
	}
	Printf("Backup %s deleted.\n", name)
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
		Println("自动压缩已开启。")
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
		Println("自动压缩已关闭。")
		return nil
	case "status":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		if cfg.MemoryAutoCompress {
			Println("自动压缩: 开启")
		} else {
			Println("自动压缩: 关闭")
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
	Print(memory.FormatStoreStatsForTool(store.Stats()))
	return nil
}

func memoryEmbedStatus(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	Print(memory.FormatEmbedStatusForTool(store.EmbedStatusForTool()))
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

	Print(memory.FormatGraphNeighborsForTool(id, store.GraphNeighborsForTool(id)))
	return nil
}

func memoryStrength(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	Print(memory.FormatStrengthForTool(store.StrengthForTool(time.Now())))
	return nil
}

func memoryInfer(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory infer <query>\n\nRuns the multi-hop inference engine on the given query and displays derived facts.")
	}
	query := strings.Join(args, " ")

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	result := store.InferForTool(query, memory.InferenceOptions{
		MaxDerived:      20,
		MinConfidence:   0.40,
		MaxVisitedFacts: 200,
	})
	Print(memory.FormatInferenceResultForTool(query, result))
	return nil
}
