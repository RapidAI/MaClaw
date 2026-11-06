package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
