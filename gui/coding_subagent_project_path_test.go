package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveEffectiveProjectPath_NilTask_ReturnsDeclared(t *testing.T) {
	result := resolveEffectiveProjectPathForTask(nil, "/home/user/project")
	if result != "/home/user/project" {
		t.Fatalf("expected declared path, got %q", result)
	}
}

func TestResolveEffectiveProjectPath_NoAbsolutePaths_ReturnsDeclared(t *testing.T) {
	task := &TaskItem{
		Title:       "Implement login",
		Description: "Add user authentication",
		Files:       []string{"src/auth.go", "src/auth_test.go"},
	}
	result := resolveEffectiveProjectPathForTask(task, "/home/user/project")
	if result != "/home/user/project" {
		t.Fatalf("expected declared path for relative files, got %q", result)
	}
}

func TestResolveEffectiveProjectPath_FilesWithinDeclared_ReturnsDeclared(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	task := &TaskItem{
		Title: "Fix bug",
		Files: []string{`D:\workprj\aicoder\gui\main.go`},
	}
	result := resolveEffectiveProjectPathForTask(task, `D:\workprj\aicoder`)
	if !strings.EqualFold(result, `D:\workprj\aicoder`) {
		t.Fatalf("expected declared path when files are within, got %q", result)
	}
}

func TestResolveEffectiveProjectPath_FilesOutsideDeclared_DoesNotExpandRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	task := &TaskItem{
		Title: "Optimize PII detection",
		Files: []string{
			`D:\AI learning\AI coding\PII_detect\pii_pipeline_v7.py`,
			`D:\AI learning\AI coding\PII_detect\utils.py`,
		},
	}
	result := resolveEffectiveProjectPathForTask(task, `D:\workprj\aicoder`)
	expected := filepath.Clean(`D:\workprj\aicoder`)
	if !strings.EqualFold(result, expected) {
		t.Fatalf("expected frozen project root %q, got %q", expected, result)
	}
	if !taskReferencesOutsideProjectPath(task, expected) {
		t.Fatal("expected external reference to require scope approval")
	}
}

func TestResolveEffectiveProjectPath_PathFromDescription_DoesNotExpandRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	task := &TaskItem{
		Title:       "Optimize OCR pipeline",
		Description: "修改 D:\\AI learning\\AI coding\\PII_detect\\pii_pipeline_v7.py 文件中的检测逻辑",
		Files:       []string{}, // Files may be empty but path is in description
	}
	result := resolveEffectiveProjectPathForTask(task, `D:\workprj\aicoder`)
	if !strings.EqualFold(result, `D:\workprj\aicoder`) {
		t.Fatalf("expected frozen declared path, got %q", result)
	}
	if !taskReferencesOutsideProjectPath(task, result) {
		t.Fatal("expected path from description to require scope approval")
	}
}

func TestTaskReferencesOutsideProjectPath_WithinDeclaredPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	task := &TaskItem{Files: []string{`D:\workprj\aicoder\gui\main.go`}}
	if taskReferencesOutsideProjectPath(task, `D:\workprj\aicoder`) {
		t.Fatal("expected in-project reference not to require scope approval")
	}
}

func TestResolveEffectiveProjectPath_RootPathRejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	// Two files on different drive roots — common ancestor would be empty or root.
	task := &TaskItem{
		Title: "Cross-drive task",
		Files: []string{`C:\projects\a.go`, `D:\projects\b.go`},
	}
	result := resolveEffectiveProjectPathForTask(task, `C:\workprj`)
	// Should fall back to declared because common ancestor is root/empty.
	if !strings.EqualFold(result, `C:\workprj`) {
		t.Fatalf("expected fallback to declared path for cross-drive, got %q", result)
	}
}

func TestResolveEffectiveProjectPath_NearRootRejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	// Files directly under D:\ — common ancestor is D:\ which is near-root.
	task := &TaskItem{
		Title: "Top-level files",
		Files: []string{`D:\file1.py`, `D:\file2.py`},
	}
	result := resolveEffectiveProjectPathForTask(task, `C:\workprj`)
	if !strings.EqualFold(result, `C:\workprj`) {
		t.Fatalf("expected fallback to declared for near-root ancestor, got %q", result)
	}
}

func TestCommonAncestorDir_SinglePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	paths := []string{`D:\AI learning\AI coding\PII_detect\pii_pipeline_v7.py`}
	result := commonAncestorDir(paths)
	expected := filepath.Clean(`D:\AI learning\AI coding\PII_detect`)
	if !strings.EqualFold(result, expected) {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestCommonAncestorDir_MultiplePaths_SameDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	paths := []string{
		`D:\AI learning\AI coding\PII_detect\pii_pipeline_v7.py`,
		`D:\AI learning\AI coding\PII_detect\utils.py`,
		`D:\AI learning\AI coding\PII_detect\models\ocr.py`,
	}
	result := commonAncestorDir(paths)
	expected := filepath.Clean(`D:\AI learning\AI coding\PII_detect`)
	if !strings.EqualFold(result, expected) {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestCommonAncestorDir_MultiplePaths_DifferentSubdirs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	paths := []string{
		`D:\AI learning\AI coding\PII_detect\pipeline.py`,
		`D:\AI learning\AI coding\data_prep\clean.py`,
	}
	result := commonAncestorDir(paths)
	expected := filepath.Clean(`D:\AI learning\AI coding`)
	if !strings.EqualFold(result, expected) {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExtractAbsolutePathsFromText_WindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	text := "请修改 D:\\AI learning\\AI coding\\PII_detect\\pii_pipeline_v7.py 中的检测逻辑"
	paths := extractAbsolutePathsFromText(text)
	if len(paths) == 0 {
		t.Fatal("expected at least one path extracted")
	}
	if !strings.Contains(strings.ToLower(paths[0]), `pii_pipeline_v7.py`) {
		t.Fatalf("expected path containing pii_pipeline_v7.py, got %q", paths[0])
	}
}

func TestExtractAbsolutePathsFromText_NoAbsolutePaths(t *testing.T) {
	text := "修改 src/main.go 文件中的处理逻辑"
	paths := extractAbsolutePathsFromText(text)
	if len(paths) != 0 {
		t.Fatalf("expected no paths for relative text, got %v", paths)
	}
}

func TestIsRootOrNearRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	cases := []struct {
		path string
		want bool
	}{
		{`C:\`, true},
		{`D:\`, true},
		{`C:\Users`, true},                  // one level deep
		{`D:\AI learning`, true},            // one level deep
		{`D:\AI learning\AI coding`, false}, // two levels deep
		{`D:\workprj\aicoder`, false},
	}
	for _, tc := range cases {
		got := isRootOrNearRoot(tc.path)
		if got != tc.want {
			t.Errorf("isRootOrNearRoot(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCollectTaskAbsolutePaths_Combined(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path test")
	}
	task := &TaskItem{
		Title:       "Fix D:\\projects\\app\\main.go",
		Description: "Also update D:\\projects\\app\\config\\settings.yaml",
		Files:       []string{`D:\projects\app\main.go`, "relative/file.go"},
	}
	paths := collectTaskAbsolutePaths(task)
	// Should have: main.go (from Files), settings.yaml (from Description), main.go (from Title — deduped)
	if len(paths) < 2 {
		t.Fatalf("expected at least 2 absolute paths, got %d: %v", len(paths), paths)
	}
	// relative/file.go should NOT be included
	for _, p := range paths {
		if strings.Contains(p, "relative") {
			t.Fatalf("relative path should not be in absolute paths: %v", paths)
		}
	}
}

// --- Layer 2: scopeApprovalState unit tests ---

func TestScopeApproval_NilState_ReturnsRejection(t *testing.T) {
	var s *scopeApprovalState
	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg == "" {
		t.Fatal("nil state should produce rejection message")
	}
}

func TestScopeApproval_NilCallback_ReturnsRejection(t *testing.T) {
	s := newScopeApprovalState(nil, false)
	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg == "" {
		t.Fatal("nil callback should produce rejection message")
	}
}

func TestScopeApproval_CallbackDeny_ReturnsRejection(t *testing.T) {
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false)
	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg == "" {
		t.Fatal("deny decision should produce rejection message")
	}
}

func TestScopeApproval_CallbackAllowOnce_ReturnsEmpty(t *testing.T) {
	calls := 0
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalAllowOnce
	}, false)
	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("allow_once should return empty, got %q", msg)
	}
	// Second call to same path should ask again (not remembered).
	msg = s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("second allow_once should return empty, got %q", msg)
	}
	if calls != 2 {
		t.Fatalf("expected callback called twice for allow_once, got %d", calls)
	}
}

func TestScopeApproval_CallbackAllowDir_RemembersDirectory(t *testing.T) {
	calls := 0
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalAllowDir
	}, false)
	// First call triggers callback.
	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("allow_dir should return empty, got %q", msg)
	}
	if calls != 1 {
		t.Fatalf("expected 1 callback call, got %d", calls)
	}
	// Second call to same directory should NOT trigger callback (pre-approved).
	msg = s.check("edit_file", `D:\outside\another.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("pre-approved dir should return empty, got %q", msg)
	}
	if calls != 1 {
		t.Fatalf("expected callback NOT called again after allow_dir, got %d calls", calls)
	}
}

func TestScopeApproval_FullAccessSkipsCallback(t *testing.T) {
	calls := 0
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalDeny
	}, true)

	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("full access should allow without rejection, got %q", msg)
	}
	if calls != 0 {
		t.Fatalf("full access should not call approval callback, got %d calls", calls)
	}
}

func TestScopeApproval_CallbackFullAccess_RemembersForTask(t *testing.T) {
	calls := 0
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalFullAccess
	}, false)

	msg := s.check("write_file", `D:\outside\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("full_access decision should allow current call, got %q", msg)
	}
	if calls != 1 {
		t.Fatalf("expected first call to ask for approval, got %d calls", calls)
	}

	msg = s.check("read_file", `D:\other\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("full_access decision should allow later paths, got %q", msg)
	}
	if calls != 1 {
		t.Fatalf("expected full_access to skip later callbacks, got %d calls", calls)
	}
}

func TestScopeApproval_AllowDir_CaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific case-insensitive test")
	}
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalAllowDir
	}, false)
	s.check("write_file", `D:\Outside\file.go`, `D:\project`)
	// Same dir, different case — should be pre-approved.
	msg := s.check("read_file", `d:\outside\other.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("case-insensitive dir should be pre-approved, got %q", msg)
	}
}

func TestScopeApproval_AllowDir_SubdirectoryApproved(t *testing.T) {
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalAllowDir
	}, false)
	// Approve D:\outside via file in that dir.
	s.check("write_file", `D:\outside\file.go`, `D:\project`)
	// Subdirectory should also be approved.
	msg := s.check("read_file", `D:\outside\sub\deep\file.go`, `D:\project`)
	if msg != "" {
		t.Fatalf("subdirectory of approved dir should pass, got %q", msg)
	}
}

func TestScopeApproval_RequestFieldsCorrect(t *testing.T) {
	var captured ScopeApprovalRequest
	s := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		captured = req
		return ScopeApprovalDeny
	}, false)
	s.check("bash", `D:\other\scripts`, `D:\myproject`)
	if captured.ToolName != "bash" {
		t.Fatalf("expected tool=bash, got %q", captured.ToolName)
	}
	if captured.Path != `D:\other\scripts` {
		t.Fatalf("expected path=D:\\other\\scripts, got %q", captured.Path)
	}
	if captured.ProjectPath != `D:\myproject` {
		t.Fatalf("expected projectPath=D:\\myproject, got %q", captured.ProjectPath)
	}
	if captured.Directory == "" {
		t.Fatal("expected non-empty directory")
	}
}

func TestResolveScopeApproval_UnknownID_NoOp(t *testing.T) {
	// Should not panic.
	ResolveScopeApproval("nonexistent_id", "allow_dir")
}

func TestResolveScopeApproval_SendsDecisionToChannel(t *testing.T) {
	ch := make(chan ScopeApprovalDecision, 1)
	id := storePendingScopeApproval(nil, ScopeApprovalRequest{}, ch)

	ResolveScopeApproval(id, "allow_dir")

	select {
	case d := <-ch:
		if d != ScopeApprovalAllowDir {
			t.Fatalf("expected allow_dir, got %q", d)
		}
	default:
		t.Fatal("expected decision on channel")
	}
}

func TestResolveScopeApproval_DoubleResolve_NoOp(t *testing.T) {
	ch := make(chan ScopeApprovalDecision, 1)
	id := storePendingScopeApproval(nil, ScopeApprovalRequest{}, ch)

	ResolveScopeApproval(id, "allow_once")
	// Second resolve should be no-op (LoadAndDelete already removed).
	ResolveScopeApproval(id, "deny")

	select {
	case d := <-ch:
		if d != ScopeApprovalAllowOnce {
			t.Fatalf("expected first decision allow_once, got %q", d)
		}
	default:
		t.Fatal("expected decision on channel")
	}
	// Channel should be empty (second resolve was no-op).
	select {
	case d := <-ch:
		t.Fatalf("expected no second decision, got %q", d)
	default:
	}
}
