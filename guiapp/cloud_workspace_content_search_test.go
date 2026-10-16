package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudWorkspaceSearchSnippetKeepsTheMatch(t *testing.T) {
	body := strings.Repeat("前言。", 80) + "竞品分析结论写在这里" + strings.Repeat("后记。", 80)
	got := cloudWorkspaceSearchSnippet(body, "竞品分析")
	if !strings.Contains(got, "竞品分析") {
		t.Fatalf("snippet = %q", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("snippet should trim both sides: %q", got)
	}
	if cloudWorkspaceSearchSnippet("没有这句话", "竞品分析") != "" {
		t.Fatal("expected no snippet")
	}
	if got := cloudWorkspaceSearchSnippet("Budget Notes", "budget"); !strings.Contains(got, "Budget") {
		t.Fatalf("case fold snippet = %q", got)
	}
	// ß lowercases to ss, so a rune index into the lowercase text panics or
	// shifts the window. The snippet has to keep the original match.
	folded := cloudWorkspaceSearchSnippet(strings.Repeat("ß", 50)+"竞品分析", "竞品分析")
	if !strings.Contains(folded, "竞品分析") {
		t.Fatalf("expanded case fold snippet = %q", folded)
	}
	expanded := cloudWorkspaceSearchSnippet(strings.Repeat("İ", 40)+"竞品分析"+strings.Repeat("后记。", 40), "竞品分析")
	if !strings.Contains(expanded, "竞品分析") || !strings.HasPrefix(expanded, "…") || !strings.HasSuffix(expanded, "…") {
		t.Fatalf("byte-length case fold snippet = %q", expanded)
	}
}

func TestCloudWorkspaceSearchByteIndexKeepsTheMatch(t *testing.T) {
	// K shrinks by 2 bytes. U+023A grows by 1, so two of them cancel that
	// shrink. The lowercased string has the same length, and the match is not
	// at the lowercased byte offset.
	text := "KHit" + "\u023A\u023A"
	lower := strings.ToLower(text)
	if len(lower) != len(text) {
		t.Fatalf("length %d vs %d", len(text), len(lower))
	}
	at := strings.Index(lower, "hit")
	if at < 0 {
		t.Fatal("folded text lost the match")
	}
	got := cloudWorkspaceSearchByteIndex(text, lower, at)
	if !strings.HasPrefix(text[got:], "Hit") {
		t.Fatalf("index %d", got)
	}
}

func TestSearchCloudWorkspaceContentFindsNameAndText(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	root := seedCloudWorkspaceSearchCache(t, app, "cws_notes01", map[string]string{
		"papers/brief.md": "竞品分析结论写在这里\n",
		"papers/合同草案.txt": "无关正文\n",
		"papers/extra.md": "未列入清单的正文\n",
	}, []string{"papers/brief.md", "papers/合同草案.txt", "../secret.txt", ".git/config"})
	created := app.createTaskRecordWithWorkingDir("经营材料", "# 经营材料\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_notes01")}, root, true)
	if created.ProjectPath == "" {
		t.Fatal("task was not created")
	}

	byName := app.SearchCloudWorkspaceContent("合同草案", 8)
	if len(byName) != 1 || byName[0].RelativePath != "papers/合同草案.txt" || byName[0].Match != "name" {
		t.Fatalf("name hits = %+v", byName)
	}
	if byName[0].WorkspaceName != "经营材料" || byName[0].ProjectPath != created.ProjectPath || byName[0].WorkspaceID != "cws_notes01" {
		t.Fatalf("identity = %+v", byName[0])
	}
	if !strings.Contains(byName[0].Preview, "papers/合同草案.txt") {
		t.Fatalf("preview = %q", byName[0].Preview)
	}

	byText := app.SearchCloudWorkspaceContent("竞品分析", 8)
	if len(byText) != 1 || byText[0].Match != "content" || byText[0].RelativePath != "papers/brief.md" {
		t.Fatalf("content hits = %+v", byText)
	}
	if !strings.Contains(byText[0].Preview, "竞品分析") {
		t.Fatalf("content preview = %q", byText[0].Preview)
	}
	if app.SearchCloudWorkspaceContent("未列入清单", 8) != nil {
		t.Fatal("files outside the listing must not match")
	}
	if app.SearchCloudWorkspaceContent("   ", 8) != nil {
		t.Fatal("blank query should not search")
	}
}

func TestSearchCloudWorkspaceContentSkipsOtherHiddenSealedAndBinaryBodies(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	notes := seedCloudWorkspaceSearchCache(t, app, "cws_rank01", map[string]string{
		"预算说明.txt":      "名字里已经有关键词\n",
		"notes/body.md": "正文里提到预算数字\n",
		"slides/报告.pdf": "pdf正文里的预算不应被读出",
	}, []string{"预算说明.txt", "notes/body.md", "slides/报告.pdf"})
	app.createTaskRecordWithWorkingDir("排名材料", "# 排名\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_rank01")}, notes, true)

	other := seedCloudWorkspaceSearchCache(t, app, "cws_other01", map[string]string{
		"only-here.md": "只在另一个工作区\n",
	}, []string{"only-here.md"})
	app.createTaskRecordWithWorkingDir("另一个空间", "# 另一个\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_other01")}, other, true)

	hiddenRoot := seedCloudWorkspaceSearchCache(t, app, "cws_hidden01", map[string]string{
		"secret.md": "隐藏工作区独有句子\n",
	}, []string{"secret.md"})
	hidden := app.createTaskRecordWithWorkingDir("隐藏空间", "# 隐藏\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_hidden01")}, hiddenRoot, true)
	app.memoryStore.ProjectIndex().SetHidden(hidden.ProjectPath, true)

	sealedRoot := seedCloudWorkspaceSearchCache(t, app, "cws_sealed01", map[string]string{
		"sealed.md": "密封缓存里的句子\n",
		"可见名称.txt":  "密封后仍可按文件名找到\n",
	}, []string{"sealed.md", "可见名称.txt"})
	if err := os.WriteFile(cloudWorkspaceCacheSealMarkerPath(sealedRoot), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.createTaskRecordWithWorkingDir("密封空间", "# 密封\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_sealed01")}, sealedRoot, true)

	ranked := app.SearchCloudWorkspaceContent("预算", 1)
	if len(ranked) != 1 || ranked[0].RelativePath != "预算说明.txt" || ranked[0].Match != "name" {
		t.Fatalf("name match should outrank body text: %+v", ranked)
	}
	if got := app.SearchCloudWorkspaceContent("pdf正文", 8); len(got) != 0 {
		t.Fatalf("pdf body = %+v", got)
	}
	pdf := app.SearchCloudWorkspaceContent("报告.pdf", 8)
	if len(pdf) != 1 || pdf[0].Match != "name" || pdf[0].WorkspaceID != "cws_rank01" {
		t.Fatalf("pdf name = %+v", pdf)
	}
	otherHits := app.SearchCloudWorkspaceContent("只在另一个工作区", 8)
	if len(otherHits) != 1 || otherHits[0].WorkspaceID != "cws_other01" {
		t.Fatalf("other workspace = %+v", otherHits)
	}
	if got := app.SearchCloudWorkspaceContent("隐藏工作区独有句子", 8); len(got) != 0 {
		t.Fatalf("hidden workspace = %+v", got)
	}
	if got := app.SearchCloudWorkspaceContent("密封缓存里的句子", 8); len(got) != 0 {
		t.Fatalf("sealed body = %+v", got)
	}
	sealedName := app.SearchCloudWorkspaceContent("可见名称", 8)
	if len(sealedName) != 1 || sealedName[0].WorkspaceID != "cws_sealed01" || sealedName[0].Match != "name" {
		t.Fatalf("sealed name = %+v", sealedName)
	}
}

func TestSearchCloudWorkspaceContentReadsPastMissingAndLongFiles(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	listed := make([]string, 0, cloudWorkspaceSearchMaxContentFiles+3)
	for i := 0; i < cloudWorkspaceSearchMaxContentFiles; i++ {
		listed = append(listed, fmt.Sprintf("missing/file-%03d.md", i))
	}
	listed = append(listed, "notes/present.md", "notes/long.md", "notes/tail.md")
	root := seedCloudWorkspaceSearchCache(t, app, "cws_budget01", map[string]string{
		"notes/present.md": "后面的已下载正文\n",
		"notes/long.md":    "长文开头的竞品分析\n" + strings.Repeat("a", 300*1024),
		"notes/tail.md":    strings.Repeat("b", 300*1024) + "只有末尾才出现的句子",
	}, listed)
	app.createTaskRecordWithWorkingDir("预算缓存", "# 预算\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_budget01")}, root, true)

	hits := app.SearchCloudWorkspaceContent("已下载正文", 8)
	if len(hits) != 1 || hits[0].RelativePath != "notes/present.md" || hits[0].Match != "content" {
		t.Fatalf("missing files consumed the read budget: %+v", hits)
	}
	longHit := app.SearchCloudWorkspaceContent("长文开头的竞品分析", 8)
	if len(longHit) != 1 || longHit[0].RelativePath != "notes/long.md" || longHit[0].Match != "content" {
		t.Fatalf("prefix hits = %+v", longHit)
	}
	if got := app.SearchCloudWorkspaceContent("只有末尾才出现的句子", 8); len(got) != 0 {
		t.Fatalf("match past the read cap was returned: %+v", got)
	}
}

func TestSearchCloudWorkspaceContentStopsWhenSuperseded(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	listed := make([]string, 0, 6)
	files := map[string]string{}
	for i := 0; i < 5; i++ {
		rel := fmt.Sprintf("notes/part-%d.md", i)
		listed = append(listed, rel)
		files[rel] = "无关正文\n"
	}
	listed = append(listed, "notes/late.md")
	files["notes/late.md"] = "后来才读到的句子\n"
	root := seedCloudWorkspaceSearchCache(t, app, "cws_stop01", files, listed)
	reads := 0
	hits := searchCloudWorkspaceContent([]cloudWorkspaceSearchRoot{{
		ID:          "cws_stop01",
		Name:        "停止",
		ProjectPath: root,
		CacheRoot:   root,
	}}, "后来才读到的句子", 8, func() bool {
		reads++
		return reads < 3
	})
	if len(hits) != 0 {
		t.Fatalf("superseded search returned %+v", hits)
	}
	if reads < 3 {
		t.Fatalf("stopped before a content read: %d", reads)
	}
}

func TestSearchCloudWorkspaceContentReadsReadonlyCacheWithoutATask(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	id := "cws_share01"
	root := app.cloudWorkspaceReadOnlyCachePath(app.cloudWorkspaceTenantID(), id)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "shared.md"), []byte("分享文稿中的句子\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("cd", 32)
	if err := writeCloudWorkspaceListing(root, &cloudWorkspaceManifest{Revision: "rev", Entries: []cloudWorkspaceManifestEntry{{Path: "docs/shared.md", SHA256: sum, Size: 1}}}); err != nil {
		t.Fatal(err)
	}
	hits := app.SearchCloudWorkspaceContent("分享文稿", 8)
	if len(hits) != 1 || hits[0].WorkspaceID != id || hits[0].Match != "content" {
		t.Fatalf("readonly hits = %+v", hits)
	}
	if hits[0].ProjectPath != normalizeProjectSessionPath(root) {
		t.Fatalf("project path = %q, cache = %q", hits[0].ProjectPath, root)
	}
}

func seedCloudWorkspaceSearchCache(t *testing.T, app *App, id string, files map[string]string, listed []string) string {
	t.Helper()
	root := app.cloudWorkspaceCachePath(app.cloudWorkspaceTenantID(), id)
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := strings.Repeat("ab", 32)
	entries := make([]cloudWorkspaceManifestEntry, 0, len(listed))
	for _, rel := range listed {
		entries = append(entries, cloudWorkspaceManifestEntry{Path: rel, SHA256: sum, Size: 1})
	}
	if err := writeCloudWorkspaceListing(root, &cloudWorkspaceManifest{Revision: "rev", Entries: entries}); err != nil {
		t.Fatal(err)
	}
	return root
}
