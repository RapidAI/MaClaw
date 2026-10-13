package guiapp

import (
	"strings"
	"testing"
)

func TestCodingTurnContextAppliesStableIdentity(t *testing.T) {
	task := &TaskItem{Index: 3, Title: "Refactor runner"}
	ctx := newCodingTurnContext("run/42", task, "/project")

	event := ctx.TaskEvent("running", task, "")
	if event.RunID != "run/42" {
		t.Fatalf("expected run id to be preserved, got %q", event.RunID)
	}
	if event.TurnID != "coding-turn-run-42-T3" {
		t.Fatalf("expected sanitized stable turn id, got %q", event.TurnID)
	}
	if event.TaskID != "T3" || event.Title != "Refactor runner" {
		t.Fatalf("expected task metadata, got %#v", event)
	}
}

func TestCodingTurnContextWrapProgressBackfillsStructuredEvents(t *testing.T) {
	ctx := newCodingTurnContext("7", &TaskItem{Index: 2, Title: "Wire tools"}, "/project")
	var progress []string
	wrapped := ctx.WrapProgress(func(text string) {
		progress = append(progress, text)
	})

	emitCodingAgentEvent(wrapped, CodingAgentEvent{
		Version: 1,
		Agent:   "coding",
		Event:   "tool_started",
		Phase:   "running",
		Detail:  "read_file",
	})
	wrapped("plain progress")

	if len(progress) != 2 {
		t.Fatalf("expected structured and plain progress, got %#v", progress)
	}
	if !strings.Contains(progress[0], `"run_id":"7"`) ||
		!strings.Contains(progress[0], `"turn_id":"coding-turn-7-T2"`) ||
		!strings.Contains(progress[0], `"task_id":"T2"`) ||
		!strings.Contains(progress[0], `"title":"Wire tools"`) {
		t.Fatalf("expected wrapped event to include turn identity, got %q", progress[0])
	}
	if progress[1] != "plain progress" {
		t.Fatalf("expected plain progress to pass through, got %#v", progress)
	}
}

func TestCodingTurnContextWrapProgressPreservesToolOutcome(t *testing.T) {
	ctx := newCodingTurnContext("8", &TaskItem{Index: 1, Title: "Run tool"}, "/project")
	var progress []string
	wrapped := ctx.WrapProgress(func(text string) {
		progress = append(progress, text)
	})

	emitCodingAgentEvent(wrapped, CodingAgentEvent{
		Version:  1,
		Agent:    "coding",
		Event:    "tool_finished",
		Phase:    "running",
		Detail:   "bash",
		Outcome:  "blocked",
		Severity: "diagnostic",
	})

	if len(progress) != 1 {
		t.Fatalf("expected one progress event, got %#v", progress)
	}
	if !strings.Contains(progress[0], `"event":"tool_finished"`) ||
		!strings.Contains(progress[0], `"outcome":"blocked"`) ||
		!strings.Contains(progress[0], `"severity":"diagnostic"`) ||
		!strings.Contains(progress[0], `"turn_id":"coding-turn-8-T1"`) {
		t.Fatalf("expected wrapped tool outcome event, got %q", progress[0])
	}
}

func TestFormatCodingAgentEventPreservesZeroCountForCountedEvents(t *testing.T) {
	formatted := formatCodingAgentEvent(CodingAgentEvent{
		Event:   "quality_summary",
		Phase:   "result",
		Outcome: "passed",
		Count:   0,
	})
	if !strings.Contains(formatted, `"count":0`) {
		t.Fatalf("expected zero count to be emitted for counted event, got %q", formatted)
	}

	taskStatus := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status",
		Phase: "running",
		Count: 0,
	})
	if strings.Contains(taskStatus, `"count":0`) {
		t.Fatalf("task status should not emit noisy zero count, got %q", taskStatus)
	}
}

func TestFormatCodingAgentEventPreservesZeroDurationForFinishedTools(t *testing.T) {
	formatted := formatCodingAgentEvent(CodingAgentEvent{
		Event:      "tool_finished",
		Phase:      "running",
		Detail:     "read_file",
		DurationMS: 0,
	})
	if !strings.Contains(formatted, `"duration_ms":0`) {
		t.Fatalf("expected zero duration to be emitted for finished tool, got %q", formatted)
	}

	started := formatCodingAgentEvent(CodingAgentEvent{
		Event:      "tool_started",
		Phase:      "running",
		Detail:     "read_file",
		DurationMS: 0,
	})
	if strings.Contains(started, `"duration_ms":0`) {
		t.Fatalf("tool started should not emit noisy zero duration, got %q", started)
	}
}

func TestCodingDiffSnapshotEvent(t *testing.T) {
	snapshot := newCodingDiffSnapshot(
		[]string{"z.go", "a.go"},
		[]string{"new.go", "a.go"},
		"diff --git a/a.go b/a.go\n+changed",
	)
	event := newCodingAgentDiffSummaryEvent(&TaskItem{Index: 8, Title: "Diff task"}, "", snapshot)

	if event.Event != "diff_summary" || event.Phase != "result" {
		t.Fatalf("unexpected diff event identity: %#v", event)
	}
	if event.Count != 3 {
		t.Fatalf("expected 3 changed files, got %d", event.Count)
	}
	if got := strings.Join(event.Files, ","); got != "a.go,new.go,z.go" {
		t.Fatalf("expected sorted unique files, got %q", got)
	}
	if !strings.Contains(event.Detail, "3 files") || !strings.Contains(event.Detail, "2 created") {
		t.Fatalf("expected diff detail to summarize counts, got %q", event.Detail)
	}
	if len(event.FileChanges) != 3 {
		t.Fatalf("expected file change rows for the table, got %#v", event.FileChanges)
	}
}

func TestCodingDiffSnapshotEventIncludesLineStats(t *testing.T) {
	snapshot := newCodingDiffSnapshot([]string{"src/auth/login.go", "src/new.go"}, []string{"src/new.go"}, "")
	stat := &SubAgentDiffStat{
		FilesChanged: 2,
		Insertions:   48,
		Deletions:    6,
		FileStats: []SubAgentFileDiffStat{
			{Path: "src/auth/login.go", Insertions: 12, Deletions: 6},
			{Path: "src/new.go", Insertions: 36, Deletions: 0},
		},
	}
	event := newCodingAgentDiffSummaryEventWithStat(&TaskItem{Index: 9, Title: "Stat task"}, "", snapshot, stat)
	if event.Added != 48 || event.Removed != 6 || !strings.Contains(event.Detail, "+48 -6") {
		t.Fatalf("expected aggregate line stats on the event, got %#v", event)
	}
	if len(event.FileChanges) != 2 {
		t.Fatalf("expected two file rows, got %#v", event.FileChanges)
	}
	if event.FileChanges[0].Path != "src/auth/login.go" || event.FileChanges[0].Added != 12 || event.FileChanges[0].Removed != 6 {
		t.Fatalf("login.go row = %#v", event.FileChanges[0])
	}
	if event.FileChanges[1].Path != "src/new.go" || event.FileChanges[1].Added != 36 || event.FileChanges[1].Removed != 0 {
		t.Fatalf("new.go row = %#v", event.FileChanges[1])
	}
}

func TestMergeSubAgentDiffStatUsesToolFallbacksWhenGitStatIsEmpty(t *testing.T) {
	snapshot := newCodingDiffSnapshot([]string{"CMakeLists.txt", "snake.cpp"}, nil, "Untracked files:\n- CMakeLists.txt\n- snake.cpp")
	merged := mergeSubAgentDiffStat(nil, []SubAgentFileDiffStat{
		{Path: "CMakeLists.txt", Insertions: 5, Deletions: 0},
		{Path: "snake.cpp", Insertions: 213, Deletions: 0},
	})
	event := newCodingAgentDiffSummaryEventWithStat(&TaskItem{Index: 2, Title: "GUI snake"}, "", snapshot, merged)
	if event.Added != 218 || event.Removed != 0 {
		t.Fatalf("expected tool fallback totals, got added=%d removed=%d", event.Added, event.Removed)
	}
	if len(event.FileChanges) != 2 {
		t.Fatalf("expected two file rows, got %#v", event.FileChanges)
	}
	if event.FileChanges[0].Path != "CMakeLists.txt" || event.FileChanges[0].Added != 5 {
		t.Fatalf("CMakeLists.txt row = %#v", event.FileChanges[0])
	}
	if event.FileChanges[1].Path != "snake.cpp" || event.FileChanges[1].Added != 213 {
		t.Fatalf("snake.cpp row = %#v", event.FileChanges[1])
	}
}

func TestMergeSubAgentDiffStatReplacesZeroGitRowsWithoutDuplicating(t *testing.T) {
	git := &SubAgentDiffStat{
		FilesChanged: 2,
		FileStats: []SubAgentFileDiffStat{
			{Path: "CMakeLists.txt", Insertions: 0, Deletions: 0},
			{Path: "snake.cpp", Insertions: 0, Deletions: 0},
		},
	}
	merged := mergeSubAgentDiffStat(git, []SubAgentFileDiffStat{
		{Path: "CMakeLists.txt", Insertions: 5, Deletions: 0},
		{Path: "snake.cpp", Insertions: 213, Deletions: 0},
	})
	if merged == nil || merged.FilesChanged != 2 || len(merged.FileStats) != 2 {
		t.Fatalf("should update in place, got %#v", merged)
	}
	if merged.Insertions != 218 || merged.Deletions != 0 {
		t.Fatalf("totals = +%d -%d", merged.Insertions, merged.Deletions)
	}
	if lookupAdded, lookupRemoved := lookupSubAgentFileDiffStat(merged, "snake.cpp"); lookupAdded != 213 || lookupRemoved != 0 {
		t.Fatalf("snake.cpp lookup = +%d -%d", lookupAdded, lookupRemoved)
	}
}

func TestMergeSubAgentDiffStatKeepsGitNonzeroOverFallback(t *testing.T) {
	git := &SubAgentDiffStat{
		FilesChanged: 1,
		Insertions:   12,
		Deletions:    3,
		FileStats:    []SubAgentFileDiffStat{{Path: "a.go", Insertions: 12, Deletions: 3}},
	}
	merged := mergeSubAgentDiffStat(git, []SubAgentFileDiffStat{{Path: "a.go", Insertions: 99, Deletions: 1}})
	if merged.Insertions != 12 || merged.Deletions != 3 || merged.FileStats[0].Insertions != 12 {
		t.Fatalf("git nonzero should win, got %#v", merged)
	}
}

func TestCodingDiffSnapshotDedupesRelativeAndAbsolutePaths(t *testing.T) {
	snapshot := newCodingDiffSnapshot([]string{"snake.cpp"}, nil, "")
	stat := &SubAgentDiffStat{
		FilesChanged: 1,
		Insertions:   213,
		Deletions:    0,
		FileStats:    []SubAgentFileDiffStat{{Path: `F:/test-prog/snake.cpp`, Insertions: 213, Deletions: 0}},
	}
	event := newCodingAgentDiffSummaryEventWithStat(&TaskItem{Index: 3, Title: "Dedupe"}, "", snapshot, stat)
	if event.Count != 1 || len(event.FileChanges) != 1 {
		t.Fatalf("relative and absolute should collapse, got %#v", event)
	}
	if event.FileChanges[0].Path != "snake.cpp" || event.FileChanges[0].Added != 213 {
		t.Fatalf("should keep relative path with git stats, got %#v", event.FileChanges[0])
	}
	if got := strings.Join(event.Files, ","); got != "snake.cpp" {
		t.Fatalf("files = %q", got)
	}
}

func TestMergeSubAgentDiffStatKeepsDifferentDirectorySameBasename(t *testing.T) {
	git := &SubAgentDiffStat{
		FileStats: []SubAgentFileDiffStat{{Path: "a/main.go", Insertions: 3, Deletions: 0}},
	}
	merged := mergeSubAgentDiffStat(git, []SubAgentFileDiffStat{{Path: "b/main.go", Insertions: 5, Deletions: 0}})
	if merged == nil || len(merged.FileStats) != 2 || merged.Insertions != 8 {
		t.Fatalf("same basename in different dirs should stay separate, got %#v", merged)
	}
}

func TestCodingAgentDisplayPathRelativizesAbsAndRemote(t *testing.T) {
	if got := codingAgentDisplayPath(`D:\workprj\demo\hello_world.cpp`, `D:\workprj\demo`); got != "hello_world.cpp" {
		t.Fatalf("local root-relative = %q", got)
	}
	if got := codingAgentDisplayPath(`D:\workprj\demo\src\main.go`, `D:\workprj\demo`); got != "src/main.go" {
		t.Fatalf("local nested = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/app/src/main.go", "/opt/app"); got != "src/main.go" {
		t.Fatalf("remote unix = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/app/src/pkg/foo.go", "/opt/app"); got != "src/pkg/foo.go" {
		t.Fatalf("deep relative path must stay intact for preview: %q", got)
	}
	if got := codingAgentDisplayPath(`D:\Work\Demo\src\main.go`, `d:\work\demo`); got != "src/main.go" {
		t.Fatalf("case-insensitive root = %q", got)
	}
	if got := codingAgentDisplayPath("D:/workprj/demo/hello_world.cpp", ""); got != "D:/workprj/demo/hello_world.cpp" {
		t.Fatalf("unrooted abs path must stay resolvable: %q", got)
	}
	if got := codingAgentDisplayPath(`D:\workprj\demo\.\src\..\src\main.go`, `D:\workprj\demo\`); got != "src/main.go" {
		t.Fatalf("dot segments and trailing root slash = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/App/foo.go", "/opt/app"); got != "/opt/App/foo.go" {
		t.Fatalf("remote linux paths must stay case-sensitive: %q", got)
	}
	if got := codingAgentDisplayPath("/opt2/foo.go", "/opt"); got != "/opt2/foo.go" {
		t.Fatalf("prefix must require a path boundary: %q", got)
	}
	if got := codingAgentDisplayPath("D:/hello.cpp", "D:/"); got != "hello.cpp" {
		t.Fatalf("drive-root project: %q", got)
	}
	if got := codingAgentDisplayPath("/foo.go", "/"); got != "foo.go" {
		t.Fatalf("unix-root project: %q", got)
	}
	files := codingToolEventFiles("write_file", `{"path":"D:\\workprj\\demo\\hello_world.cpp"}`, `D:\workprj\demo`)
	if len(files) != 1 || files[0] != "hello_world.cpp" {
		t.Fatalf("tool event files = %#v", files)
	}
}

func TestCodingToolEventTargetShowsSearchAndExplore(t *testing.T) {
	root := `D:\workprj\demo`
	if got := codingToolEventTarget("Glob", `{"glob_pattern":"**/*.go"}`, root); got != "**/*.go" {
		t.Fatalf("glob target = %q", got)
	}
	if got := codingToolEventTarget("ripgrep", `{"pattern":"func main","path":"D:\\workprj\\demo\\src\\main.go"}`, root); got != "src/main.go · func main" {
		t.Fatalf("ripgrep target = %q", got)
	}
	if got := codingToolEventTarget("ripgrep", `{"pattern":"func main","path":"D:\\workprj\\demo"}`, root); got != "func main" {
		t.Fatalf("project-root search target = %q", got)
	}
	if got := codingToolEventTarget("ripgrep", `{"pattern":"func main","path":"src"}`, root); got != "func main · src" {
		t.Fatalf("directory search target = %q", got)
	}
	if got := codingToolEventTarget("code_navigation", `{"operation":"explore","query":"buildTask","file":"guiapp/coding_subagent.go"}`, root); got != "guiapp/coding_subagent.go · buildTask" {
		t.Fatalf("explore target = %q", got)
	}
	if got := codingToolEventTarget("list_directory", `{"directory":"src"}`, root); got != "src" {
		t.Fatalf("list target = %q", got)
	}
	if got := codingToolEventTarget("list_directory", `{"path":"."}`, root); got != "" {
		t.Fatalf("root list target = %q", got)
	}
	if got := codingToolEventTarget("read_file", `{"path":"a.go"}`, root); got != "" {
		t.Fatalf("read target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"codegraph explore \"buildTask\""}`, root); got != "buildTask" {
		t.Fatalf("codegraph explore target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"codegraph explore --json \"handler.ServeHTTP\""}`, root); got != "handler.ServeHTTP" {
		t.Fatalf("codegraph symbol target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"codegraph explore \"handler.ServeHTTP\" --limit 20"}`, root); got != "handler.ServeHTTP" {
		t.Fatalf("codegraph limit target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"codegraph explore --limit 20 \"handler.ServeHTTP\""}`, root); got != "handler.ServeHTTP" {
		t.Fatalf("codegraph limit-first target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"echo codegraph explore buildTask"}`, root); got != "" {
		t.Fatalf("echo codegraph target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"bash -lc \"codegraph explore buildTask\""}`, root); got != "buildTask" {
		t.Fatalf("wrapped codegraph target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"gopls definition -json src/main.go:10:2"}`, root); got != "src/main.go" {
		t.Fatalf("gopls flag target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"./tools/codegraph.cmd node D:\\workprj\\demo\\guiapp\\coding_subagent.go"}`, root); got != "guiapp/coding_subagent.go" {
		t.Fatalf("codegraph node target = %q", got)
	}
	if got := codingToolEventTarget("ssh_bash", `{"command":"gopls definition src/main.go:10:2"}`, root); got != "src/main.go" {
		t.Fatalf("gopls target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"lsp-cli definition --file src/main.go --query buildTask"}`, root); got != "src/main.go · buildTask" {
		t.Fatalf("lsp-cli target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"go test ./..."}`, root); got != "" {
		t.Fatalf("plain bash target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"findstr /n /i \"bmp frame\" F:\\test-prog\\snake.cpp"}`, `F:\test-prog`); got != "snake.cpp · bmp frame" {
		t.Fatalf("findstr target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"findstr /n /i \"bmp frame_ window_live BITMAPFILEHEADER WriteFile\" F:\\test-prog\\snake.cpp"}`, `F:\test-prog`); got != "snake.cpp · bmp frame_ window_live BITMAPFILEHEADER WriteFile" {
		t.Fatalf("findstr log target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"findstr /c:playTone snake.cpp"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("findstr /c target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Select-String -Path snake.cpp -Pattern playTone"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("select-string target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cd F:\\test-prog; Select-String -Path snake.cpp -Pattern 'playTone|tonecheck|ToneCheck|TONE_|g_sound|Beep' | ForEach-Object { $_.Line }"}`, `F:\test-prog`); got != "snake.cpp · playTone|tonecheck|ToneCheck|TONE_|g_sound|Beep" {
		t.Fatalf("select-string pipeline target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"sls -Path snake.cpp -Pattern playTone"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("sls target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"rg -e playTone snake.cpp"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("rg -e target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"rg.exe playTone snake.cpp"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("rg.exe target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"git grep playTone"}`, root); got != "" {
		t.Fatalf("git grep target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"echo findstr playTone snake.cpp"}`, root); got != "" {
		t.Fatalf("echo findstr target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"findstr /n /i \"bmp frame\" snake.cpp 2>&1"}`, root); got != "snake.cpp · bmp frame" {
		t.Fatalf("findstr redirect target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"rg playTone snake.cpp > out.txt"}`, root); got != "snake.cpp · playTone" {
		t.Fatalf("rg redirect target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cd F:\\test-prog; Select-String -Path build\\CMakeCache.txt -Pattern \"CMAKE_CXX_COMPILER:|CMAKE_LINKER\" | ForEach-Object { $_.Line }"}`, `F:\test-prog`); got != "build/CMakeCache.txt · CMAKE_CXX_COMPILER:|CMAKE_LINKER" {
		t.Fatalf("select-string cache target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cmake --build build 2>&1 | Select-Object -Last 25"}`, root); got != "" {
		t.Fatalf("cmake pipeline target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cd F:\\test-prog; cmd /c \"findstr /i /c:\\\"tone\\\" /c:\\\"sound\\\" /c:\\\"silent\\\" /c:\\\"gui\\\" build\\\\selftest_before.txt\""}`, `F:\test-prog`); got != "build/selftest_before.txt · tone | sound | silent | gui" {
		t.Fatalf("findstr /c list target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cd F:\\test-prog; Get-Content build\\selftest_before.txt | Select-String -Pattern \"FAIL|passed|gui \""}`, `F:\test-prog`); got != "build/selftest_before.txt · FAIL|passed|gui" {
		t.Fatalf("get-content select-string target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"cd F:\\test-prog; .\\build\\Release\\snake.exe --selftest 2>&1 | Select-String -Pattern \"FAIL|passed|logic|gui \""}`, `F:\test-prog`); got != "FAIL|passed|logic|gui" {
		t.Fatalf("exe select-string target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"grep -n \"selftest\\|tonecheck\\|main(\\|WinMain\\|runSelfTest\\|wWinMain\\|__argc\\|GetCommandLine\" F:\\test-prog\\snake.cpp"}`, `F:\test-prog`); got != `snake.cpp · selftest\|tonecheck\|main(\|WinMain\|runSelfTest\|wWinMain\|__argc\|GetCommandLine` {
		t.Fatalf("grep paren target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Select-String -Path F:\\test-prog\\build\\CMakeCache.txt -Pattern \"CMAKE_GENERATOR:|CMAKE_BUILD_TYPE|CMAKE_CXX_COMPILER:\""}`, `F:\test-prog`); got != "build/CMakeCache.txt · CMAKE_GENERATOR:|CMAKE_BUILD_TYPE|CMAKE_CXX_COMPILER:" {
		t.Fatalf("absolute select-string target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Get-Content build\\selftest_before.txt | Select-String -Pattern snake.cpp"}`, `F:\test-prog`); got != "build/selftest_before.txt · snake.cpp" {
		t.Fatalf("dotted pattern target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Get-Content -Tail 20 build\\selftest_before.txt | Select-String -Pattern FAIL"}`, `F:\test-prog`); got != "build/selftest_before.txt · FAIL" {
		t.Fatalf("get-content tail target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Get-Content build\\selftest_before.txt -Tail 20 | Select-String -Pattern FAIL"}`, `F:\test-prog`); got != "build/selftest_before.txt · FAIL" {
		t.Fatalf("get-content tail-after target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Get-Content build\\selftest_before.txt 2>&1 | Select-String -Pattern FAIL"}`, `F:\test-prog`); got != "build/selftest_before.txt · FAIL" {
		t.Fatalf("get-content redirect target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"Get-Content build\\selftest_before.txt & Select-String -Pattern FAIL"}`, `F:\test-prog`); got != "FAIL" {
		t.Fatalf("background select-string target = %q", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"public-c findstr snake.cpp"}`, root); got != "" {
		t.Fatalf("public-c findstr target = %q, want empty", got)
	}
	if got := codingToolEventTarget("bash", `{"command":"git status --short"}`, root); got != "" {
		t.Fatalf("git status target = %q, want empty", got)
	}
}

func TestSuppressCodingWorkbenchStatusProgress(t *testing.T) {
	var got []string
	passthrough := suppressCodingWorkbenchStatusProgress(func(s string) { got = append(got, s) }, false)
	passthrough("[Status] Task received")
	passthrough("Coding Agent: running T1")
	hidden := suppressCodingWorkbenchStatusProgress(func(s string) { got = append(got, s) }, true)
	hidden("[Status] Task received")
	hidden("Coding Agent Event: {\"event\":\"tool_started\"}")
	if len(got) != 3 {
		t.Fatalf("unexpected progress: %#v", got)
	}
	if got[0] != "[Status] Task received" || !strings.HasPrefix(got[1], "Coding Agent:") || !strings.HasPrefix(got[2], "Coding Agent Event:") {
		t.Fatalf("coding workbench must drop only [Status]: %#v", got)
	}
	if suppressCodingWorkbenchStatusProgress(nil, true) != nil {
		t.Fatal("nil callback should stay nil")
	}
}

func TestAttachCodingToolFileChangesFromWriteAndEditArgs(t *testing.T) {
	write := CodingAgentEvent{Files: []string{"hello.cpp"}}
	attachCodingToolFileChanges(&write, "write_file", `{"path":"hello.cpp","content":"a\nb\nc\n"}`, nil)
	if write.Added != 3 || write.Removed != 0 || len(write.FileChanges) != 1 || write.FileChanges[0].Path != "hello.cpp" {
		t.Fatalf("write estimate = %#v", write)
	}
	edit := CodingAgentEvent{Files: []string{"a.go"}}
	attachCodingToolFileChanges(&edit, "ssh_edit_file", `{"path":"a.go","old_str":"x\ny","new_str":"x\ny\nz"}`, nil)
	if edit.Added != 1 || edit.Removed != 0 {
		t.Fatalf("edit estimate = %#v", edit)
	}
	read := CodingAgentEvent{Files: []string{"a.go"}}
	attachCodingToolFileChanges(&read, "read_file", `{"path":"a.go"}`, nil)
	if len(read.FileChanges) != 0 {
		t.Fatalf("read should not get line stats: %#v", read)
	}
}
