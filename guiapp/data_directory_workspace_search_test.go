package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchDataDirectoryWorkspacesFindsSandboxNameAndText(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("只在外部工作目录\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := app.createTaskRecordWithWorkingDir("经营材料", "# 经营材料\n", []string{taskManagementTag, taskUserCreatedTag}, outside, true)
	if created.ProjectPath == "" {
		t.Fatal("task was not created")
	}
	workspace := filepath.Join(created.ProjectPath, "workspace")
	writeDataDirectorySearchFile(t, filepath.Join(workspace, "papers", "合同草案.txt"), "无关正文\n")
	writeDataDirectorySearchFile(t, filepath.Join(workspace, "papers", "brief.md"), "竞品分析结论写在这里\n")
	writeDataDirectorySearchFile(t, filepath.Join(workspace, "node_modules", "pkg", "skip.txt"), "依赖目录里的句子\n")
	writeDataDirectorySearchFile(t, filepath.Join(created.ProjectPath, "secret.txt"), "任务目录外的句子\n")
	writeDataDirectorySearchFile(t, filepath.Join(workspace, "报告.pdf"), "pdf正文不应被搜到\n")

	byName := app.SearchDataDirectoryWorkspaces("合同草案", 8)
	if len(byName) != 1 || byName[0].RelativePath != "papers/合同草案.txt" || byName[0].Match != "name" {
		t.Fatalf("name hits = %+v", byName)
	}
	if byName[0].TaskName != "经营材料" || byName[0].ProjectPath != created.ProjectPath {
		t.Fatalf("identity = %+v", byName[0])
	}
	if !strings.Contains(byName[0].Preview, "papers/合同草案.txt") {
		t.Fatalf("preview = %q", byName[0].Preview)
	}

	byText := app.SearchDataDirectoryWorkspaces("竞品分析", 8)
	if len(byText) != 1 || byText[0].Match != "content" || byText[0].RelativePath != "papers/brief.md" {
		t.Fatalf("content hits = %+v", byText)
	}
	if got := app.SearchDataDirectoryWorkspaces("只在外部工作目录", 8); len(got) != 0 {
		t.Fatalf("custom working dir was searched: %+v", got)
	}
	if got := app.SearchDataDirectoryWorkspaces("依赖目录里的句子", 8); len(got) != 0 {
		t.Fatalf("node_modules was searched: %+v", got)
	}
	if got := app.SearchDataDirectoryWorkspaces("任务目录外的句子", 8); len(got) != 0 {
		t.Fatalf("task metadata dir was searched: %+v", got)
	}
	if got := app.SearchDataDirectoryWorkspaces("pdf正文", 8); len(got) != 0 {
		t.Fatalf("pdf body was searched: %+v", got)
	}
	pdf := app.SearchDataDirectoryWorkspaces("报告.pdf", 8)
	if len(pdf) != 1 || pdf[0].Match != "name" || pdf[0].RelativePath != "报告.pdf" {
		t.Fatalf("pdf name hits = %+v", pdf)
	}
	if app.SearchDataDirectoryWorkspaces("   ", 8) != nil {
		t.Fatal("blank query should return nil")
	}

	preview, err := app.GetDataDirectoryWorkspaceFilePreview(created.ProjectPath, "papers/brief.md")
	if err != nil || !strings.Contains(preview.Content, "竞品分析") {
		t.Fatalf("preview = %+v err=%v", preview, err)
	}
	if _, err := app.GetDataDirectoryWorkspaceFilePreview(created.ProjectPath, "../secret.txt"); err == nil {
		t.Fatal("preview escaped the workspace")
	}

	longPath := filepath.Join(workspace, "long.md")
	writeDataDirectorySearchFile(t, longPath, "长文开头的竞品分析\n"+strings.Repeat("a", 300*1024))
	longHit := app.SearchDataDirectoryWorkspaces("长文开头的竞品分析", 8)
	if len(longHit) != 1 || longHit[0].RelativePath != "long.md" || longHit[0].Match != "content" {
		t.Fatalf("prefix hits = %+v", longHit)
	}
	writeDataDirectorySearchFile(t, filepath.Join(workspace, "tail.md"), strings.Repeat("b", 300*1024)+"只有末尾才出现的句子")
	if got := app.SearchDataDirectoryWorkspaces("只有末尾才出现的句子", 8); len(got) != 0 {
		t.Fatalf("match past the read cap was returned: %+v", got)
	}

	stray := filepath.Join(app.GetDataDir(), "tasks", "stray", "workspace", "orphan.md")
	writeDataDirectorySearchFile(t, stray, "没有任务记录的句子\n")
	if got := app.SearchDataDirectoryWorkspaces("没有任务记录的句子", 8); len(got) != 0 {
		t.Fatalf("orphan workspace was searched: %+v", got)
	}
}

func TestSearchDataDirectoryWorkspacesStopsWhenSuperseded(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 4; i++ {
		writeDataDirectorySearchFile(t, filepath.Join(root, "notes", "part-"+string(rune('a'+i))+".md"), "无关正文\n")
	}
	writeDataDirectorySearchFile(t, filepath.Join(root, "notes", "late.md"), "后来才读到的句子\n")
	reads := 0
	hits := searchDataDirectoryWorkspaces([]dataDirectoryWorkspaceRoot{{
		Name:        "停止",
		ProjectPath: root,
		Workspace:   root,
	}}, "后来才读到的句子", 8, func() bool {
		reads++
		return reads < 3
	})
	if len(hits) != 0 {
		t.Fatalf("superseded search returned %+v", hits)
	}
	if reads < 3 {
		t.Fatalf("stopped before the walk observed cancellation: %d", reads)
	}
}

func TestSearchDataDirectoryWorkspacesSkipsHiddenAndPrefersName(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	visible := app.createTaskRecordWithWorkingDir("可见任务", "# 可见\n", []string{taskManagementTag, taskUserCreatedTag}, "", true)
	hidden := app.createTaskRecordWithWorkingDir("隐藏任务", "# 隐藏\n", []string{taskManagementTag, taskUserCreatedTag}, "", true)
	if visible.ProjectPath == "" || hidden.ProjectPath == "" {
		t.Fatal("tasks were not created")
	}
	writeDataDirectorySearchFile(t, filepath.Join(visible.ProjectPath, "workspace", "预算说明.txt"), "普通正文\n")
	writeDataDirectorySearchFile(t, filepath.Join(visible.ProjectPath, "workspace", "notes.md"), "预算讨论写在正文里\n")
	writeDataDirectorySearchFile(t, filepath.Join(hidden.ProjectPath, "workspace", "hidden.md"), "隐藏工作区独有句子\n")
	app.memoryStore.ProjectIndex().SetHidden(hidden.ProjectPath, true)

	ranked := app.SearchDataDirectoryWorkspaces("预算", 1)
	if len(ranked) != 1 || ranked[0].RelativePath != "预算说明.txt" || ranked[0].Match != "name" {
		t.Fatalf("ranked = %+v", ranked)
	}
	if got := app.SearchDataDirectoryWorkspaces("隐藏工作区独有句子", 8); len(got) != 0 {
		t.Fatalf("hidden workspace was searched: %+v", got)
	}
	if _, err := app.GetDataDirectoryWorkspaceFilePreview(hidden.ProjectPath, "hidden.md"); err == nil {
		t.Fatal("hidden workspace preview should fail")
	}
}

func writeDataDirectorySearchFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
