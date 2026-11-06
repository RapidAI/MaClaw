package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileCompanionCatalogFoldersAndFilterPlacements(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("library items = %d", len(listed.Items))
	}
	item := listed.Items[0]

	if _, err := app.CreateFileCompanionFolder("  "); err == nil || err.Error() != "请填写文件夹名称" {
		t.Fatalf("blank name err = %v", err)
	}
	long := strings.Repeat("名", fileCompanionFolderNameCap+1)
	if _, err := app.CreateFileCompanionFolder(long); err == nil || err.Error() != "文件夹名称过长" {
		t.Fatalf("long name err = %v", err)
	}
	first, err := app.CreateFileCompanionFolder(" 论文 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Folders) != 1 || first.Folders[0].Name != "论文" || first.Rev < 1 {
		t.Fatalf("created = %+v", first)
	}
	if _, err := app.CreateFileCompanionFolder("论文"); err == nil || err.Error() != "已有同名文件夹" {
		t.Fatalf("duplicate err = %v", err)
	}
	second, err := app.CreateFileCompanionFolder("笔记")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := app.RenameFileCompanionFolder(first.Folders[0].ID, "课程")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Folders[0].Name != "课程" || renamed.Folders[0].ID != first.Folders[0].ID {
		t.Fatalf("renamed = %+v", renamed.Folders)
	}
	moved, err := app.MoveFileCompanionLibraryItem(item.ID, item.Path, renamed.Folders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(moved, item) != renamed.Folders[0].ID {
		t.Fatalf("folder = %q", fileCompanionItemFolder(moved, item))
	}
	// A library rewrite must not drop the folders.
	if err := app.writeFileCompanionLibraryLocked(listed.Items); err != nil {
		t.Fatal(err)
	}
	kept, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Folders) != 2 || fileCompanionItemFolder(kept, item) != renamed.Folders[0].ID {
		t.Fatalf("catalog after library write = %+v", kept)
	}
	if _, err := app.DeleteFileCompanionFolder(renamed.Folders[0].ID, "", ""); err == nil || err.Error() != "文件夹里还有文档" {
		t.Fatalf("delete occupied err = %v", err)
	}
	merged, err := app.DeleteFileCompanionFolder(renamed.Folders[0].ID, "merge", second.Folders[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Folders) != 1 || merged.Folders[0].Name != "笔记" {
		t.Fatalf("merged folders = %+v", merged.Folders)
	}
	if fileCompanionItemFolder(merged, item) != second.Folders[1].ID {
		t.Fatalf("merged placement = %q", fileCompanionItemFolder(merged, item))
	}
	cleared, err := app.MoveFileCompanionLibraryItem(item.ID, item.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(cleared, item) != "" {
		t.Fatalf("uncategorized = %q", fileCompanionItemFolder(cleared, item))
	}
	if _, err := app.CreateFileCompanionFolder("未分类"); err == nil || err.Error() != "全部和未分类不能用作文件夹名称" {
		t.Fatalf("reserved name err = %v", err)
	}
	again, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if again.Rev != cleared.Rev {
		t.Fatalf("list rewrote the catalog: rev %d -> %d", cleared.Rev, again.Rev)
	}
}

func TestFileCompanionCatalogKeepsTheFolderWhenTheRowIDChanges(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	if _, err := app.MoveFileCompanionLibraryItem(listed.Items[0].ID, listed.Items[0].Path, folderID); err != nil {
		t.Fatal(err)
	}
	replaced := listed.Items[0]
	replaced.ID = "cloud:replacement"
	replaced.Source = "cloud"
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{replaced}); err != nil {
		t.Fatal(err)
	}
	kept, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(kept, replaced) != folderID {
		t.Fatalf("folder after id change = %q", fileCompanionItemFolder(kept, replaced))
	}
}

func TestFileCompanionCatalogDeleteRemoveKeepsTheSourceFile(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "keep.md")
	notes := filepath.Join(home, "stay.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notes, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "keep.md", Size: 5})
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: notes, Name: "stay.md", Size: 4})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 2 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	var keep, stay FileCompanionLibraryItem
	for _, item := range listed.Items {
		switch item.Name {
		case "keep.md":
			keep = item
		case "stay.md":
			stay = item
		}
	}
	created, err := app.CreateFileCompanionFolder("待整理")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	if _, err := app.MoveFileCompanionLibraryItem(keep.ID, keep.Path, folderID); err != nil {
		t.Fatal(err)
	}
	removed, err := app.DeleteFileCompanionFolder(folderID, "remove", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Folders) != 0 {
		t.Fatalf("folders left = %+v", removed.Folders)
	}
	if _, err := os.Stat(paper); err != nil {
		t.Fatalf("source file = %v", err)
	}
	after := app.listFileCompanionLibraryStored()
	for _, item := range after.Items {
		if item.ID == keep.ID || fileCompanionSameLibraryPath(item.Path, paper) {
			t.Fatalf("removed row still listed: %+v", item)
		}
	}
	foundStay := false
	for _, item := range after.Items {
		if item.ID == stay.ID {
			foundStay = true
		}
	}
	if !foundStay {
		t.Fatal("the document outside the folder was removed")
	}
}

func TestFileCompanionCatalogRemoveOwnedShareReturnsUncategorized(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "keep.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "cd_0123456789abcdef0123456789abcdef"
	cloud := FileCompanionLibraryItem{
		ID:        "cloud:" + token,
		Name:      "keep.md",
		Path:      paper,
		Source:    "cloud",
		Mine:      true,
		Token:     token,
		ExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		OpenedAt:  time.Now().UTC().Format(time.RFC3339),
		Size:      5,
	}
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{cloud}); err != nil {
		t.Fatal(err)
	}
	created, err := app.CreateFileCompanionFolder("待整理")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	if _, err := app.MoveFileCompanionLibraryItem(cloud.ID, cloud.Path, folderID); err != nil {
		t.Fatal(err)
	}
	removed, err := app.DeleteFileCompanionFolder(folderID, "remove", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Folders) != 0 {
		t.Fatalf("folders left = %+v", removed.Folders)
	}
	if _, err := os.Stat(paper); err != nil {
		t.Fatalf("source file = %v", err)
	}
	after := app.listFileCompanionLibraryStored()
	if len(after.Items) != 1 || after.Items[0].Source == "cloud" {
		t.Fatalf("restored library = %+v", after.Items)
	}
	if fileCompanionItemFolder(removed, after.Items[0]) != "" {
		t.Fatalf("restored folder = %q catalog=%+v", fileCompanionItemFolder(removed, after.Items[0]), removed.Placements)
	}
	listed, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(listed, after.Items[0]) != "" {
		t.Fatalf("folder came back = %q", fileCompanionItemFolder(listed, after.Items[0]))
	}
}

func TestFileCompanionCatalogUncategorizeOneRowOfASharedFile(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	local := listed.Items[0]
	cloud := local
	cloud.ID = "cloud:same-file"
	cloud.Source = "cloud"
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{local, cloud}); err != nil {
		t.Fatal(err)
	}
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	moved, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, folderID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(moved, local) != folderID || fileCompanionItemFolder(moved, cloud) != folderID {
		t.Fatalf("shared file folders = %q %q", fileCompanionItemFolder(moved, local), fileCompanionItemFolder(moved, cloud))
	}
	cleared, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(cleared, local) != "" {
		t.Fatalf("local stayed in %q", fileCompanionItemFolder(cleared, local))
	}
	if fileCompanionItemFolder(cleared, cloud) != folderID {
		t.Fatalf("cloud folder = %q", fileCompanionItemFolder(cleared, cloud))
	}
	again, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(again, local) != "" || fileCompanionItemFolder(again, cloud) != folderID {
		t.Fatalf("list pulled the row back: local %q cloud %q", fileCompanionItemFolder(again, local), fileCompanionItemFolder(again, cloud))
	}
	if again.Rev != cleared.Rev {
		t.Fatalf("list rewrote the catalog: rev %d -> %d", cleared.Rev, again.Rev)
	}
}

func TestFileCompanionCatalogMoveTargetsTheRequestedRowWhenASiblingSharesThePath(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	local := listed.Items[0]
	cloud := local
	cloud.ID = "cloud:same-file"
	cloud.Source = "cloud"

	// The cloud row is first, so a path match would hit it before the local id.
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{cloud, local}); err != nil {
		t.Fatal(err)
	}
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	moved, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, folderID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(moved, local) != folderID || fileCompanionItemFolder(moved, cloud) != folderID {
		t.Fatalf("shared file folders = %q %q", fileCompanionItemFolder(moved, local), fileCompanionItemFolder(moved, cloud))
	}
	cleared, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(cleared, local) != "" {
		t.Fatalf("local stayed in %q", fileCompanionItemFolder(cleared, local))
	}
	if fileCompanionItemFolder(cleared, cloud) != folderID {
		t.Fatalf("cloud left the folder: %q", fileCompanionItemFolder(cleared, cloud))
	}

	// The local row is first. Moving the later cloud row must not retarget it.
	other, err := app.CreateFileCompanionFolder("笔记")
	if err != nil {
		t.Fatal(err)
	}
	otherID := ""
	for _, folder := range other.Folders {
		if folder.Name == "笔记" {
			otherID = folder.ID
		}
	}
	if otherID == "" {
		t.Fatal("笔记 was not created")
	}
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{local, cloud}); err != nil {
		t.Fatal(err)
	}
	retargeted, err := app.MoveFileCompanionLibraryItem(cloud.ID, cloud.Path, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(retargeted, local) != "" {
		t.Fatalf("local was pulled into %q", fileCompanionItemFolder(retargeted, local))
	}
	if fileCompanionItemFolder(retargeted, cloud) != otherID {
		t.Fatalf("cloud folder = %q", fileCompanionItemFolder(retargeted, cloud))
	}
	again, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if again.Rev != retargeted.Rev {
		t.Fatalf("list rewrote the catalog: rev %d -> %d", retargeted.Rev, again.Rev)
	}
	if fileCompanionItemFolder(again, local) != "" || fileCompanionItemFolder(again, cloud) != otherID {
		t.Fatalf("list moved the rows: local %q cloud %q", fileCompanionItemFolder(again, local), fileCompanionItemFolder(again, cloud))
	}
}

func TestFileCompanionCatalogReplacedIDKeepsTheFolderWhenASiblingIsUncategorized(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	local := listed.Items[0]
	cloud := local
	cloud.ID = "cloud:same-file"
	cloud.Source = "cloud"
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{local, cloud}); err != nil {
		t.Fatal(err)
	}
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	if _, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, folderID); err != nil {
		t.Fatal(err)
	}
	cleared, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Placements[fileCompanionFolderItemKey(local.ID)] != fileCompanionFolderUncategorized {
		t.Fatalf("local sentinel = %q", cleared.Placements[fileCompanionFolderItemKey(local.ID)])
	}
	if cleared.Placements[fileCompanionFolderPathKey(local.Path)] != folderID {
		t.Fatalf("path key = %q", cleared.Placements[fileCompanionFolderPathKey(local.Path)])
	}
	cloud.ID = "cloud:replacement"
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{local, cloud}); err != nil {
		t.Fatal(err)
	}
	kept, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(kept, local) != "" {
		t.Fatalf("local pulled back into %q", fileCompanionItemFolder(kept, local))
	}
	if fileCompanionItemFolder(kept, cloud) != folderID {
		t.Fatalf("replaced cloud folder = %q", fileCompanionItemFolder(kept, cloud))
	}
	again, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if again.Rev != kept.Rev {
		t.Fatalf("list rewrote the catalog: rev %d -> %d", kept.Rev, again.Rev)
	}
}

func TestFileCompanionCatalogKeepsPlacementsWhenTheLibraryCannotBeRead(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	item := listed.Items[0]
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	moved, err := app.MoveFileCompanionLibraryItem(item.ID, item.Path, folderID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.fileCompanionLibraryPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if kept.Rev != moved.Rev {
		t.Fatalf("list rewrote the catalog: rev %d -> %d", moved.Rev, kept.Rev)
	}
	if kept.Placements[fileCompanionFolderItemKey(item.ID)] != folderID {
		t.Fatalf("placement dropped: %+v", kept.Placements)
	}
	if _, err := app.CreateFileCompanionFolder("笔记"); err == nil || err.Error() != "文档列表无法读取" {
		t.Fatalf("create err = %v", err)
	}
	again, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if again.Rev != moved.Rev || again.Placements[fileCompanionFolderItemKey(item.ID)] != folderID {
		t.Fatalf("catalog after failed create = rev %d placements %+v", again.Rev, again.Placements)
	}
}

func TestFileCompanionCatalogFolderCap(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for i := 0; i < fileCompanionFolderCap; i++ {
		if _, err := app.CreateFileCompanionFolder(fmt.Sprintf("文件夹%02d", i)); err != nil {
			t.Fatalf("folder %d: %v", i, err)
		}
	}
	if _, err := app.CreateFileCompanionFolder("溢出"); err == nil || err.Error() != "文件夹数量已达上限" {
		t.Fatalf("cap err = %v", err)
	}
}

func TestFileCompanionDefaultFolder(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	set, err := app.SetFileCompanionDefaultFolder(folderID)
	if err != nil || set.DefaultID != folderID {
		t.Fatalf("set = %+v err=%v", set, err)
	}
	listed, err := app.ListFileCompanionCatalog()
	if err != nil || listed.DefaultID != folderID {
		t.Fatalf("listed default = %q err=%v", listed.DefaultID, err)
	}
	cleared, err := app.SetFileCompanionDefaultFolder("none")
	if err != nil || cleared.DefaultID != "" {
		t.Fatalf("clear = %+v err=%v", cleared, err)
	}
	again, err := app.SetFileCompanionDefaultFolder(folderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Placements) != 0 {
		t.Fatalf("default placed the document: %+v", again.Placements)
	}
	moved, err := app.MoveFileCompanionLibraryItem("", paper, folderID)
	if err != nil || moved.DefaultID != folderID {
		t.Fatalf("move = %+v err=%v", moved, err)
	}
	if moved.Placements[fileCompanionFolderPathKey(paper)] != folderID {
		t.Fatalf("path placement = %+v", moved.Placements)
	}
	back, err := app.MoveFileCompanionLibraryItem("", paper, "")
	if err != nil || back.DefaultID != folderID {
		t.Fatalf("uncategorize kept default = %+v err=%v", back, err)
	}
	if _, err := app.DeleteFileCompanionFolder(folderID, "", ""); err != nil {
		t.Fatal(err)
	}
	after, err := app.ListFileCompanionCatalog()
	if err != nil || after.DefaultID != "" {
		t.Fatalf("deleted default = %q err=%v", after.DefaultID, err)
	}
	if again.DefaultID != folderID {
		t.Fatalf("before delete = %+v", again)
	}
	if _, err := app.SetFileCompanionDefaultFolder("fld_missing"); err == nil || err.Error() != "没有这个文件夹" {
		t.Fatalf("missing err = %v", err)
	}
}

func TestFileCompanionMoveUnknownPath(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, "missing.pdf")
	moved, err := app.MoveFileCompanionLibraryItem("", missing, created.Folders[0].ID)
	if err == nil || err.Error() != "没有找到这个项目" {
		t.Fatalf("missing = %+v err=%v", moved, err)
	}
	listed, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Placements) != 0 {
		t.Fatalf("placements = %+v", listed.Placements)
	}
}

func TestFileCompanionFolderForPathUsesTheMoveTarget(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	paper := filepath.Join(home, "paper.md")
	if err := os.WriteFile(paper, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionDocument(FileCompanionDocument{Path: paper, Name: "paper.md", Size: 5})
	created, err := app.CreateFileCompanionFolder("论文")
	if err != nil {
		t.Fatal(err)
	}
	folderID := created.Folders[0].ID
	got, err := app.FileCompanionFolderForPath(paper)
	if err != nil || got != "" {
		t.Fatalf("new file = %q err=%v", got, err)
	}
	if _, err := app.MoveFileCompanionLibraryItem("", paper, folderID); err != nil {
		t.Fatal(err)
	}
	before, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	got, err = app.FileCompanionFolderForPath(paper)
	if err != nil || got != folderID {
		t.Fatalf("placed = %q err=%v", got, err)
	}
	after, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if after.Rev != before.Rev {
		t.Fatalf("read wrote the catalog rev %d -> %d", before.Rev, after.Rev)
	}
	missing := filepath.Join(home, "missing.pdf")
	got, err = app.FileCompanionFolderForPath(missing)
	if err != nil || got != "" {
		t.Fatalf("missing = %q err=%v", got, err)
	}
	listed := app.listFileCompanionLibraryStored()
	if len(listed.Items) != 1 {
		t.Fatalf("items = %d", len(listed.Items))
	}
	local := listed.Items[0]
	cloud := local
	cloud.ID = "cloud:same-file"
	cloud.Source = "cloud"
	if err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{local, cloud}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, ""); err != nil {
		t.Fatal(err)
	}
	kept, err := app.ListFileCompanionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if kept.Placements[fileCompanionFolderPathKey(paper)] != folderID {
		t.Fatalf("sibling path key = %+v", kept.Placements)
	}
	// The local row is explicit 未分类 and sits first, but the list hides it
	// behind the cloud project. The dialog has to offer the card on screen.
	shown := app.listFileCompanionLibraryStored()
	if len(shown.Items) != 1 || shown.Items[0].ID != cloud.ID {
		t.Fatalf("screen rows = %+v", shown.Items)
	}
	got, err = app.FileCompanionFolderForPath(paper)
	if err != nil || got != folderID {
		t.Fatalf("visible row = %q err=%v", got, err)
	}
	notes, err := app.CreateFileCompanionFolder("笔记")
	if err != nil {
		t.Fatal(err)
	}
	notesID := ""
	for _, folder := range notes.Folders {
		if folder.Name == "笔记" {
			notesID = folder.ID
		}
	}
	if notesID == "" {
		t.Fatal("笔记 was not created")
	}
	moved, err := app.MoveFileCompanionLibraryItem("", paper, notesID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(moved, local) != "" {
		t.Fatalf("hidden local moved to %q", fileCompanionItemFolder(moved, local))
	}
	if fileCompanionItemFolder(moved, cloud) != notesID {
		t.Fatalf("visible row = %q", fileCompanionItemFolder(moved, cloud))
	}
	// The card on screen is now 未分类. The hidden local still has 论文, and
	// that path key must not become the offer or the empty-id move.
	if _, err := app.MoveFileCompanionLibraryItem(cloud.ID, cloud.Path, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MoveFileCompanionLibraryItem(local.ID, local.Path, folderID); err != nil {
		t.Fatal(err)
	}
	got, err = app.FileCompanionFolderForPath(paper)
	if err != nil || got != "" {
		t.Fatalf("visible uncategorized inherited %q err=%v", got, err)
	}
	retargeted, err := app.MoveFileCompanionLibraryItem("", paper, notesID)
	if err != nil {
		t.Fatal(err)
	}
	if fileCompanionItemFolder(retargeted, cloud) != notesID {
		t.Fatalf("empty-id move hit %q", fileCompanionItemFolder(retargeted, cloud))
	}
	if fileCompanionItemFolder(retargeted, local) != folderID {
		t.Fatalf("hidden local left 论文: %q", fileCompanionItemFolder(retargeted, local))
	}
}

func TestFileCompanionPlaceBootAsksOnce(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPlace([]string{`C:\papers\2510.16079.pdf`})
	boot := app.GetFileCompanionBoot()
	if len(boot.Paths) != 1 || len(boot.Place) != 1 || boot.Place[0] != `C:\papers\2510.16079.pdf` {
		t.Fatalf("boot = %+v", boot)
	}
	again := app.GetFileCompanionBoot()
	if len(again.Paths) != 0 || len(again.Place) != 0 {
		t.Fatalf("second boot = %+v", again)
	}
	plain := &App{}
	plain.seedFileCompanionPaths([]string{`C:\papers\notes.md`})
	opened := plain.GetFileCompanionBoot()
	if len(opened.Place) != 0 || len(opened.Paths) != 1 {
		t.Fatalf("open-file boot = %+v", opened)
	}
}
