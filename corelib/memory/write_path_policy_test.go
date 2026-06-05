package memory

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestProductionMemoryWritesUseCorelibHelpers(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	allowedFiles := map[string]bool{
		filepath.ToSlash(filepath.Join("corelib", "memory", "manual.go")):             true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "store.go")):              true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "upsert.go")):             true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "artifact.go")):           true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "generated_insight.go")):  true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "upsert.go")):             true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "project_knowledge.go")):  true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "session_checkpoint.go")): true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "summary.go")):            true,
	}
	categorySpecificUpsertAllowedFiles := map[string]bool{
		filepath.ToSlash(filepath.Join("corelib", "memory", "generated_insight.go")):  true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "artifact.go")):           true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "project_knowledge.go")):  true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "session_checkpoint.go")): true,
		filepath.ToSlash(filepath.Join("corelib", "memory", "summary.go")):            true,
	}
	forbidden := []*regexp.Regexp{
		regexp.MustCompile("\\bmemoryStore\\.(Save|Update)\\("),
		regexp.MustCompile("\\.Save\\((core)?memory\\.Entry\\s*\\{"),
		regexp.MustCompile("\\.Save\\(entry\\)"),
		regexp.MustCompile("return .*\\.Save\\(entry\\)"),
		regexp.MustCompile("\\.UpsertEntryByID\\("),
	}

	var findings []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", "build", "dist", ".claude", "iWorkerCenter":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "corelib/memory/") && !categorySpecificUpsertAllowedFiles[rel] {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(data)
			if finding := findRawCategorySpecificGeneratedUpsert(rel, content); finding != "" {
				findings = append(findings, finding)
			}
			if finding := findRawCategorySpecificGeneratedEntryLiteral(rel, content); finding != "" {
				findings = append(findings, finding)
			}
		}
		if allowedFiles[rel] {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			for _, re := range forbidden {
				if re.MatchString(line) {
					findings = append(findings, rel+":"+itoaPolicy(lineNo)+": "+strings.TrimSpace(line))
				}
			}
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) > 0 {
		t.Fatalf("production memory writes must go through corelib/memory helpers; direct writes found:\n%s", strings.Join(findings, "\n"))
	}
}

func TestCoreMemoryDirectEntryWritesStayInAllowedBoundaries(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	allowed := map[string]map[string]bool{
		filepath.ToSlash(filepath.Join("corelib", "memory", "archive.go")): {
			"(*ArchiveStore).Add":             true,
			"(*ArchiveStore).addLocked":       true,
			"(*ArchiveStore).removeIDsLocked": true,
			"(*ArchiveStore).GC":              true,
			"(*ArchiveStore).load":            true,
		},
		filepath.ToSlash(filepath.Join("corelib", "memory", "store.go")): {
			"(*Store).UpdateEntriesAndDeleteIDs":      true,
			"(*Store).upsertEntriesByID":              true,
			"(*Store).syncGraphLinksLocked":           true,
			"(*Store).replaceEntriesAndRebuildLocked": true,
			"(*Store).replaceEntriesAndRebuildAsync":  true,
			"(*Store).insertPreparedEntryLocked":      true,
		},
	}

	var findings []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "corelib", "memory"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		currentFunc := ""
		for lineNo, line := range strings.Split(string(data), "\n") {
			if fn := policyFunctionName(line); fn != "" {
				currentFunc = fn
			}
			trimmed := strings.TrimSpace(line)
			if !isDirectEntriesWrite(trimmed) {
				continue
			}
			if allowed[rel][currentFunc] {
				continue
			}
			findings = append(findings, rel+":"+itoaPolicy(lineNo+1)+": direct entries write outside allowed boundary "+currentFunc+": "+trimmed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) > 0 {
		t.Fatalf("direct entries writes must stay inside documented store/archive/sync reconstruction boundaries:\n%s", strings.Join(findings, "\n"))
	}
}

func TestHostAdaptersUseCorelibMemoryStoreFactory(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	allowedFactoryCallers := map[string]bool{
		filepath.ToSlash(filepath.Join("gui", "app.go")):                                     true,
		filepath.ToSlash(filepath.Join("tui", "app.go")):                                     true,
		filepath.ToSlash(filepath.Join("tui", "pipe_mode.go")):                               true,
		filepath.ToSlash(filepath.Join("tui", "commands", "memory.go")):                      true,
		filepath.ToSlash(filepath.Join("corelib", "agentservice", "core_agent_executor.go")): true,
	}
	forbidden := regexp.MustCompile(`\b(?:memory|corememory)\.NewStore\s*\(`)
	factory := regexp.MustCompile(`\b(?:memory|corememory)\.NewStoreWithMode(?:AndLegacyJSON)?\s*\(`)

	var findings []string
	for _, root := range []string{"gui", "tui", "maclawsrv", "MaClawSrv", filepath.ToSlash(filepath.Join("corelib", "agentservice"))} {
		err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(root)), func(path string, d os.DirEntry, err error) error {
			if err != nil && os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(data)
			if forbidden.MatchString(content) {
				findings = append(findings, rel+": host adapter must use corelib/memory StoreFactory, not NewStore")
			}
			if factory.MatchString(content) && !allowedFactoryCallers[rel] {
				findings = append(findings, rel+": StoreFactory opening should stay in the host bootstrap, not feature code")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(findings) > 0 {
		t.Fatalf("host memory adapters must share corelib/memory StoreFactory:\n%s", strings.Join(findings, "\n"))
	}
}

func TestHostAdaptersUseCorelibMemoryMaintenance(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	forbidden := regexp.MustCompile(`\b(?:memory|corememory)\.(?:NewCompressor|NewPipeline|NewSynthesizer|NewConsolidator|NewProfileConsolidator|NewOnlineExtractor|NewRecallGating|NewKnowledgeExtractor(?:WithConsolidator)?)\s*\(`)

	var findings []string
	for _, root := range []string{"gui", "tui", "maclawsrv", "MaClawSrv", filepath.ToSlash(filepath.Join("corelib", "agentservice"))} {
		err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(root)), func(path string, d os.DirEntry, err error) error {
			if err != nil && os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if forbidden.MatchString(string(data)) {
				findings = append(findings, rel+": host adapter must use corelib/memory Maintenance, not direct runtime constructors")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(findings) > 0 {
		t.Fatalf("host memory maintenance must share corelib/memory Maintenance:\n%s", strings.Join(findings, "\n"))
	}
}

func TestAuxiliaryMemoryNamedStoresAreDocumentedAsNonLongTermMemory(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	checks := map[string][]string{
		filepath.ToSlash(filepath.Join("corelib", "agent", "conversation_memory.go")): {
			"conversation/session-state", "not Maclaw long-term memory", "corelib/memory.Store",
		},
		filepath.ToSlash(filepath.Join("corelib", "agentservice", "knowledge_integration.go")): {
			"cited documents/cards/facts", "not Maclaw long-term memory", "corelib/memory.Store",
		},
		filepath.ToSlash(filepath.Join("corelib", "agentservice", "memory_store.go")): {
			"control-plane", "not Maclaw long-term memory", "corelib/memory.Store",
		},
		filepath.ToSlash(filepath.Join("corelib", "agentservice", "record_store_memory.go")): {
			"structured-record", "not Maclaw long-term memory", "corelib/memory.Store",
		},
		filepath.ToSlash(filepath.Join("corelib", "tool", "tool_memory.go")): {
			"not a replacement", "corelib/memory.Store", "execution hints",
		},
	}
	var findings []string
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				findings = append(findings, rel+": missing non-long-term-memory marker "+want)
			}
		}
	}
	if len(findings) > 0 {
		t.Fatalf("auxiliary memory-like stores must document that corelib/memory owns long-term memory:\n%s", strings.Join(findings, "\n"))
	}
}

func TestGUIMemoryRuntimeUsesMaintenanceFacade(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "gui", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "memory.NewMaintenance") || !strings.Contains(content, ".InstallRuntime()") || !strings.Contains(content, ".Pipeline()") || !strings.Contains(content, "guiMemoryEventEmitter") {
		t.Fatalf("gui memory runtime must be assembled through corelib/memory Maintenance facade")
	}
	for _, forbidden := range []string{"memory.NewPipeline", "memory.NewSynthesizer", "memory.NewOnlineExtractor", "memory.NewProfileConsolidator", "memory.NewConsolidator", "memory.NewRecallGating", "memory.NewKnowledgeExtractor"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("gui app should not assemble memory runtime directly with %s", forbidden)
		}
	}
}
func TestHostManualMemorySaveUsesCorelibManualHelper(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	checks := map[string][]string{
		filepath.Join("tui", "commands", "memory.go"): {
			"store.SaveManualMemory", "memory.HandleTool(store, map[string]interface{}{\n\t\t\"action\":   \"save\"",
		},
		filepath.Join("gui", "app_wails_bindings.go"): {
			"a.memoryStore.SaveManualMemory", "memory.HandleTool(a.memoryStore, map[string]interface{}{\n\t\t\"action\":   \"save\"",
		},
	}
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if !strings.Contains(content, wants[0]) {
			t.Fatalf("%s manual memory save must route through corelib memory manual helper", rel)
		}
		if strings.Contains(content, wants[1]) {
			t.Fatalf("%s manual memory save must not route through governed tool save", rel)
		}
	}
}

func TestHostMemoryDeleteUsesCorelibToolAction(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	checks := map[string][]string{
		filepath.Join("tui", "commands", "memory.go"): {
			"memory.HandleTool(store", "\"action\": \"delete\"", "store.Delete(id)",
		},
		filepath.Join("gui", "app_wails_bindings.go"): {
			"memory.HandleTool(a.memoryStore", "\"action\": \"delete\"", "a.memoryStore.Delete(id)",
		},
	}
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if !strings.Contains(content, wants[0]) || !strings.Contains(content, wants[1]) {
			t.Fatalf("%s memory delete must route through corelib memory HandleTool delete action", rel)
		}
		if strings.Contains(content, wants[2]) {
			t.Fatalf("%s memory delete must not bypass corelib memory tool action", rel)
		}
	}
}
func TestGUIInferenceDiagnosticsUseCorelibProjection(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "gui", "app_wails_bindings.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"type InferenceDiagnosticsData = memory.InferenceDiagnosticsData", "InferenceDiagnosticsForHost", "TestInferenceForHost"} {
		if !strings.Contains(content, want) {
			t.Fatalf("gui inference diagnostics must use corelib/memory projection %s", want)
		}
	}
	for _, forbidden := range []string{"a.memoryStore.InferenceEngine()", "a.memoryStore.SemanticGraph()", "memory.ExpandQuery(query)"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("gui inference diagnostics must not duplicate corelib/memory inference projection: %s", forbidden)
		}
	}
}
func TestTUIMemoryInspectionUsesCorelibFormatters(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "tui", "commands", "memory.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"FormatEmbedStatusForTool", "FormatGraphNeighborsForTool", "FormatStrengthForTool", "FormatInferenceResultForTool"} {
		if !strings.Contains(content, want) {
			t.Fatalf("tui memory diagnostics must use corelib/memory formatter %s", want)
		}
	}
	for _, forbidden := range []string{"store.RLock()", "store.Entries()", "store.GraphNeighbors(", "store.InferenceEngine()", "store.SemanticGraph()", "store.Embedder()"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("tui memory diagnostics must not inspect store internals directly: %s", forbidden)
		}
	}
}
func TestHostMemoryEvalUsesCorelibFormatters(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "tui", "commands", "memory.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"memory.FormatRecallEvalReportForTool", "memory.FormatRecallMaintenanceEvalReportForTool"} {
		if !strings.Contains(content, want) {
			t.Fatalf("tui memory eval must use corelib/memory formatter %s", want)
		}
	}
	for _, forbidden := range []string{"func printRecallEvalReport", "func printRecallMaintenanceEvalReport"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("tui memory eval must not duplicate corelib/memory formatter %s", forbidden)
		}
	}
}
func TestHostMemoryInspectionUsesCorelibToolFacades(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`\.ConsolidateMemoryCandidates\s*\(`),
		regexp.MustCompile(`\.ListMemoryCandidates\s*\(`),
		regexp.MustCompile(`\.ThemeDiagnostics\s*\(`),
		regexp.MustCompile(`\.ThemeExplanations\s*\(`),
		regexp.MustCompile(`\.ThemeHealth\s*\(`),
		regexp.MustCompile(`\.ThemeManager\(\)\.TopThemes\s*\(`),
		regexp.MustCompile(`\.ApplyThemeMaintenancePlan\s*\(`),
	}

	var findings []string
	for _, root := range []string{"gui", "tui", "maclawsrv", "MaClawSrv", filepath.ToSlash(filepath.Join("corelib", "agentservice"))} {
		err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(root)), func(path string, d os.DirEntry, err error) error {
			if err != nil && os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(data)
			for _, re := range forbidden {
				if re.MatchString(content) {
					findings = append(findings, rel+": host memory inspection must use corelib/memory Tool facades")
					break
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(findings) > 0 {
		t.Fatalf("host memory inspection/maintenance must stay behind corelib/memory tool facades:\n%s", strings.Join(findings, "\n"))
	}
}
func TestGUIWailsCompressionControlsUseMaintenanceFacade(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "gui", "app_wails_bindings.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"getOrCreateMemoryMaintenance",
		"maintenance.Compress(ctx)",
		"maintenance.ListBackups()",
		"maintenance.RestoreBackup(backupName)",
		"maintenance.DeleteBackup(backupName)",
		"maintenance.StartCompressor()",
		"maintenance.StopCompressor()",
		"maintenance.CompressorStatus()",
		"maintenance.IsCompressing()",
		"maintenance.SetMaxBackups(n)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("gui Wails memory controls must use corelib/memory Maintenance facade; missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"mc := a.getOrCreateCompressor()",
		"a.memoryCompressor.Status()",
		"a.memoryCompressor.IsCompressing()",
		"mc.Compress(ctx)",
		"mc.ListBackups()",
		"mc.RestoreBackup(backupName)",
		"mc.DeleteBackup(backupName)",
		"mc.Start()",
		"mc.Stop()",
		"mc.SetMaxBackups(n)",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("gui Wails memory controls must not bypass Maintenance facade with %s", forbidden)
		}
	}
}
func TestGUIMemoryCompressorIsCorelibAdapter(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "gui", "memory_compressor.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"type MemoryCompressor = memory.Compressor",
		"memory.NewMaintenance",
		"owned by corelib/memory",
		"return app.newMemoryCompressor(store)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("gui memory compressor must be a corelib adapter; missing %q", want)
		}
	}
	if strings.Contains(content, "func (mc *MemoryCompressor) dedup") || strings.Contains(content, "func (mc *MemoryCompressor) mergeSemanticDuplicates") || strings.Contains(content, "func (mc *MemoryCompressor) compressEntry") {
		t.Fatalf("gui memory compressor must not reimplement corelib memory maintenance")
	}
}

func TestDerivedConsolidatorsUseConsolidationGate(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	checks := map[string][]string{
		filepath.Join("corelib", "memory", "synthesizer.go"): {
			"AssessConsolidationGate", "gate.Allowed", "EvidenceIDs", "Boundary",
		},
		filepath.Join("corelib", "memory", "profile_consolidator.go"): {
			"AssessConsolidationGate", "gate.Allowed", "EvidenceIDs", "Boundary",
		},
	}
	var findings []string
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				findings = append(findings, rel+": missing derived consolidation guard "+want)
			}
		}
	}
	if len(findings) > 0 {
		t.Fatalf("derived schema/profile consolidation must be gated and evidence-bound:\n%s", strings.Join(findings, "\n"))
	}
}

func policyFunctionName(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "func ") {
		return ""
	}
	method := regexp.MustCompile(`^func \([^)]*\*([A-Za-z0-9_]+)\)\s*([A-Za-z0-9_]+)\s*\(`)
	if m := method.FindStringSubmatch(line); len(m) == 3 {
		return "(*" + m[1] + ")." + m[2]
	}
	fn := regexp.MustCompile(`^func\s+([A-Za-z0-9_]+)\s*\(`)
	if m := fn.FindStringSubmatch(line); len(m) == 2 {
		return m[1]
	}
	return ""
}

func isDirectEntriesWrite(line string) bool {
	if line == "" || strings.HasPrefix(line, "//") || !strings.Contains(line, ".entries") {
		return false
	}
	if strings.Contains(line, "==") || strings.Contains(line, "!=") || strings.Contains(line, "<=") || strings.Contains(line, ">=") {
		return false
	}
	if strings.HasPrefix(line, "for ") {
		return false
	}
	if strings.Contains(line, "++") || strings.Contains(line, "--") {
		return strings.Contains(line, ".entries[")
	}
	idx := strings.Index(line, "=")
	if idx < 0 {
		return false
	}
	lhs := strings.TrimSpace(line[:idx])
	for _, prefix := range []string{"s.entries", "a.entries", "mc.store.entries"} {
		if lhs == prefix || strings.HasPrefix(lhs, prefix+"[") {
			return true
		}
	}
	return false
}

func itoaPolicy(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func findRawCategorySpecificGeneratedUpsert(rel, content string) string {
	const marker = "UpsertEntryByTags("
	for searchFrom := 0; ; {
		idx := strings.Index(content[searchFrom:], marker)
		if idx < 0 {
			return ""
		}
		start := searchFrom + idx
		end := strings.Index(content[start:], "})")
		if end < 0 {
			return ""
		}
		block := content[start : start+end]
		if strings.Contains(block, "CategoryProjectKnowledge") || strings.Contains(block, "CategoryTaskArtifact") || strings.Contains(block, "CategoryConversationSummary") || strings.Contains(block, "CategorySessionCheckpoint") {
			return rel + ":" + itoaPolicy(1+strings.Count(content[:start], "\n")) + ": category-specific generated memory upsert must use a corelib helper"
		}
		searchFrom = start + len(marker)
	}
}

func findRawCategorySpecificGeneratedEntryLiteral(rel, content string) string {
	const marker = "Entry{"
	for searchFrom := 0; ; {
		idx := strings.Index(content[searchFrom:], marker)
		if idx < 0 {
			return ""
		}
		start := searchFrom + idx
		end := strings.Index(content[start:], "}")
		if end < 0 {
			return ""
		}
		block := content[start : start+end]
		if strings.Contains(block, "CategoryProjectKnowledge") || strings.Contains(block, "CategoryTaskArtifact") || strings.Contains(block, "CategoryConversationSummary") || strings.Contains(block, "CategorySessionCheckpoint") {
			return rel + ":" + itoaPolicy(1+strings.Count(content[:start], "\n")) + ": category-specific generated memory literal must use a corelib helper"
		}
		searchFrom = start + len(marker)
	}
}
