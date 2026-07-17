package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// knowledgeImportResult is the per-file result for JSON output.
type knowledgeImportResult struct {
	Path     string `json:"path"`
	Status   string `json:"status"` // "imported", "skipped_duplicate", "failed", "unsupported"
	SourceID string `json:"source_id,omitempty"`
	Nodes    int    `json:"nodes,omitempty"`
	Error    string `json:"error,omitempty"`
}

// knowledgeImportSummary is the aggregate result for JSON output.
type knowledgeImportSummary struct {
	TotalFiles int                    `json:"total_files"`
	Imported   int                    `json:"imported"`
	Skipped    int                    `json:"skipped"`
	Failed     int                    `json:"failed"`
	Results    []knowledgeImportResult `json:"results,omitempty"`
}

// RunKnowledge executes the knowledge subcommand group.
func RunKnowledge(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui knowledge <import|list|search|status|delete|clear>")
	}
	switch args[0] {
	case "import":
		return knowledgeImport(dataDir, args[1:])
	case "list":
		return knowledgeList(dataDir, args[1:])
	case "search":
		return knowledgeSearch(dataDir, args[1:])
	case "status":
		return knowledgeStatus(dataDir)
	case "delete":
		return knowledgeDelete(dataDir, args[1:])
	case "clear":
		return knowledgeClear(dataDir, args[1:])
	default:
		return NewUsageError("unknown knowledge command: %s", args[0])
	}
}

// openKnowledgeStore opens the knowledge SQLite store from the given data directory.
func openKnowledgeStore(dataDir string) (*knowledge.SQLiteStore, error) {
	dbPath := filepath.Join(dataDir, "knowledge.db")
	return knowledge.NewSQLiteStore(dbPath)
}

// knowledgeImport handles file and directory import into the knowledge base.
func knowledgeImport(dataDir string, args []string) error {
	fs := flag.NewFlagSet("knowledge import", flag.ExitOnError)
	project := fs.String("project", "", "Associate with project path")
	labels := fs.String("labels", "", "Comma-separated labels")
	scope := fs.String("scope", "project", "Save scope: project|personal|local_only")
	includeExts := fs.String("include-exts", "", "Override extension filter (comma-separated)")
	dryRun := fs.Bool("dry-run", false, "Scan only, don't import")
	jsonOutput := fs.Bool("json", false, "Output JSON format")
	fs.Parse(args)

	paths := fs.Args()
	if len(paths) == 0 {
		return NewUsageError("usage: maclaw-tui knowledge import [flags] <path...>")
	}

	// Open the knowledge store.
	store, err := openKnowledgeStore(dataDir)
	if err != nil {
		if *jsonOutput {
			summary := knowledgeImportSummary{Failed: 1, Results: []knowledgeImportResult{{
				Path:   paths[0],
				Status: "failed",
				Error:  fmt.Sprintf("failed to open knowledge store: %v", err),
			}}}
			return json.NewEncoder(Stdout()).Encode(summary)
		}
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	// Build import request from flags.
	req := knowledge.DirectoryImportRequest{
		ProjectPath: *project,
		SaveScope:   *scope,
		DryRun:      *dryRun,
		Recursive:   true,
	}
	if *labels != "" {
		req.Labels = strings.Split(*labels, ",")
	}
	if *includeExts != "" {
		req.IncludeExts = strings.Split(*includeExts, ",")
	}

	ctx := context.Background()
	summary := knowledgeImportSummary{}

	// Process each path sequentially.
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			r := knowledgeImportResult{Path: p, Status: "failed", Error: err.Error()}
			summary.Results = append(summary.Results, r)
			summary.Failed++
			if !*jsonOutput {
				Eprintf("Error: %s: %v\n", p, err)
			}
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			r := knowledgeImportResult{Path: p, Status: "failed", Error: err.Error()}
			summary.Results = append(summary.Results, r)
			summary.Failed++
			if !*jsonOutput {
				Eprintf("Error: %s: %v\n", p, err)
			}
			continue
		}

		var result knowledge.DirectoryImportResult
		if info.IsDir() {
			req.RootPath = absPath
			result, err = store.ImportDirectory(ctx, req)
		} else {
			req.RootPath = filepath.Dir(absPath)
			result, err = store.ImportFiles(ctx, req, []string{absPath})
		}

		if err != nil {
			r := knowledgeImportResult{Path: p, Status: "failed", Error: err.Error()}
			summary.Results = append(summary.Results, r)
			summary.Failed++
			if !*jsonOutput {
				Eprintf("Error: %s: %v\n", p, err)
			}
			continue
		}

		// Convert DirectoryImportResult items to per-file results.
		for _, item := range result.Items {
			r := knowledgeImportResult{Path: item.FilePath}
			switch item.Status {
			case "imported", "completed":
				r.Status = "imported"
				r.SourceID = item.SourceID
				summary.Imported++
			case "duplicate", "skipped_duplicate":
				r.Status = "skipped_duplicate"
				summary.Skipped++
			case "unsupported":
				r.Status = "unsupported"
				summary.Skipped++
			case "failed":
				r.Status = "failed"
				r.Error = item.ErrorMessage
				summary.Failed++
			default:
				r.Status = item.Status
				if item.ErrorMessage != "" {
					r.Error = item.ErrorMessage
					summary.Failed++
				} else {
					summary.Skipped++
				}
			}
			summary.Results = append(summary.Results, r)
		}

		// If no items were returned (single file import), synthesize a result.
		if len(result.Items) == 0 {
			r := knowledgeImportResult{Path: absPath}
			summary.Imported += result.ImportedFiles
			summary.Skipped += result.DuplicateFiles + result.SkippedFiles
			summary.Failed += result.FailedFiles
			if result.ImportedFiles > 0 {
				r.Status = "imported"
			} else if result.DuplicateFiles > 0 {
				r.Status = "skipped_duplicate"
			} else if result.FailedFiles > 0 {
				r.Status = "failed"
			} else {
				r.Status = "imported"
			}
			summary.Results = append(summary.Results, r)
		}

		// Print human-readable output per path (non-JSON mode).
		if !*jsonOutput {
			if info.IsDir() {
				Printf("Directory: %s\n", absPath)
				Printf("  Total files: %d, Imported: %d, Skipped: %d, Failed: %d\n",
					result.TotalFiles, result.ImportedFiles, result.DuplicateFiles+result.SkippedFiles, result.FailedFiles)
			} else {
				if result.ImportedFiles > 0 {
					Printf("Imported: %s (nodes: %d)\n", absPath, result.ImportedFiles)
				} else if result.DuplicateFiles > 0 {
					Printf("Skipped (duplicate): %s\n", absPath)
				} else if result.FailedFiles > 0 {
					Printf("Failed: %s\n", absPath)
				}
			}
		}
	}

	summary.TotalFiles = len(summary.Results)

	// JSON output mode: emit valid JSON to stdout.
	if *jsonOutput {
		return json.NewEncoder(Stdout()).Encode(summary)
	}

	// Human-readable summary for multiple paths.
	if len(paths) > 1 {
		Printf("\nSummary: %d total, %d imported, %d skipped, %d failed\n",
			summary.TotalFiles, summary.Imported, summary.Skipped, summary.Failed)
	}

	return nil
}

// knowledgeList prints a table of all knowledge sources.
func knowledgeList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("knowledge list", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output JSON format")
	fs.Parse(args)

	store, err := openKnowledgeStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{})
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}

	if *jsonOutput {
		return PrintJSON(sources)
	}

	if len(sources) == 0 {
		Println("No knowledge sources found.")
		return nil
	}

	// Print table header.
	Printf("%-12s %-10s %-30s %-10s %5s %5s %s\n",
		"ID", "Kind", "Title/Path", "Status", "Nodes", "Cards", "Updated")
	Println(strings.Repeat("-", 100))

	for _, s := range sources {
		title := s.Title
		if title == "" {
			title = s.URI
		}
		title = TruncateDisplay(title, 30)
		id := TruncateDisplay(s.ID, 12)
		updated := s.UpdatedAt.Format("2006-01-02")
		if s.UpdatedAt.IsZero() {
			updated = "-"
		}
		Printf("%-12s %-10s %-30s %-10s %5d %5d %s\n",
			id, s.Kind, title, s.Status, s.NodeCount, s.CardCount, updated)
	}

	Printf("\nTotal: %d sources\n", len(sources))
	return nil
}

// knowledgeSearch performs FTS search and prints ranked results.
func knowledgeSearch(dataDir string, args []string) error {
	fs := flag.NewFlagSet("knowledge search", flag.ExitOnError)
	limit := fs.Int("limit", 10, "Maximum number of results")
	jsonOutput := fs.Bool("json", false, "Output JSON format")
	fs.Parse(args)

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		return NewUsageError("usage: maclaw-tui knowledge search [flags] <query>")
	}

	store, err := openKnowledgeStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()
	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: *limit,
	})
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if *jsonOutput {
		return PrintJSON(results)
	}

	if len(results) == 0 {
		Println("No results found.")
		return nil
	}

	Printf("Search results for: %q (%d results)\n\n", query, len(results))
	for i, r := range results {
		sourceLabel := r.Source.Title
		if sourceLabel == "" {
			sourceLabel = r.Source.URI
		}
		sourceLabel = TruncateDisplay(sourceLabel, 40)

		snippet := r.Snippet
		if snippet == "" {
			snippet = r.Claim
		}
		if snippet == "" {
			snippet = r.Summary
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = TruncateDisplay(snippet, 80)

		Printf("%d. [%.2f] %s\n", i+1, r.Score, sourceLabel)
		if snippet != "" {
			Printf("   %s\n", snippet)
		}
		Println()
	}
	return nil
}

// knowledgeStatus prints knowledge base statistics.
func knowledgeStatus(dataDir string) error {
	dbPath := filepath.Join(dataDir, "knowledge.db")

	// Check if the database file exists.
	info, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		Println("Knowledge base not initialized.")
		Printf("Database path: %s (not found)\n", dbPath)
		Println("Import documents with: maclaw-tui knowledge import <path>")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat knowledge database: %w", err)
	}

	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()
	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	Println("Knowledge Base Status")
	Println(strings.Repeat("=", 40))
	Printf("  Database:       %s\n", dbPath)
	Printf("  Database size:  %s\n", formatFileSize(info.Size()))
	Printf("  Last modified:  %s\n", info.ModTime().Format("2006-01-02 15:04:05"))
	Println()
	Println("Statistics:")
	Printf("  Total sources:  %d\n", stats.Sources)
	Printf("  Total nodes:    %d\n", stats.DocumentNodes)
	Printf("  Total cards:    %d\n", stats.Cards)
	Printf("  Total facts:    %d\n", stats.Facts)
	Printf("  Import batches: %d\n", stats.Batches)

	if len(stats.SourcesByKind) > 0 {
		Println()
		Println("Sources by kind:")
		for kind, count := range stats.SourcesByKind {
			Printf("  %-12s %d\n", kind, count)
		}
	}

	if len(stats.SourcesByStatus) > 0 {
		Println()
		Println("Sources by status:")
		for status, count := range stats.SourcesByStatus {
			Printf("  %-12s %d\n", status, count)
		}
	}

	return nil
}

// formatFileSize formats a byte count into a human-readable string.
func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// knowledgeDelete removes a source and all its nodes/cards/facts.
// Prompts for confirmation unless --force is provided.
func knowledgeDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("knowledge delete", flag.ExitOnError)
	force := fs.Bool("force", false, "Skip confirmation prompt")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui knowledge delete [--force] <source-id>")
	}
	sourceID := fs.Arg(0)

	store, err := openKnowledgeStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Look up the source to show details in the confirmation prompt.
	source, err := store.GetSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}

	// Prompt for confirmation unless --force is set.
	if !*force {
		title := source.Title
		if title == "" {
			title = source.URI
		}
		Printf("Delete source %s (%s)?\n", source.ID, title)
		Printf("  Kind: %s, Nodes: %d, Cards: %d\n", source.Kind, source.NodeCount, source.CardCount)
		Print("Confirm deletion? [y/N]: ")

		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("aborted")
		}
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			Println("Aborted.")
			return nil
		}
	}

	if err := store.DeleteSource(ctx, sourceID); err != nil {
		return fmt.Errorf("failed to delete source: %w", err)
	}

	Printf("Source %s deleted.\n", sourceID)
	return nil
}

// knowledgeClear removes all sources from the knowledge store.
// Requires --force flag to prevent accidental data loss.
func knowledgeClear(dataDir string, args []string) error {
	fs := flag.NewFlagSet("knowledge clear", flag.ExitOnError)
	force := fs.Bool("force", false, "Required flag to confirm clearing all sources")
	fs.Parse(args)

	if !*force {
		return NewUsageError("knowledge clear requires --force flag to confirm removal of all sources")
	}

	store, err := openKnowledgeStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open knowledge store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()

	// List all sources to delete them one by one.
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 5000})
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}

	if len(sources) == 0 {
		Println("Knowledge base is already empty.")
		return nil
	}

	deleted := 0
	for _, src := range sources {
		if err := store.DeleteSource(ctx, src.ID); err != nil {
			Eprintf("warning: failed to delete source %s: %v\n", src.ID, err)
			continue
		}
		deleted++
	}

	Printf("Cleared %d source(s) from knowledge base.\n", deleted)
	return nil
}
