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
