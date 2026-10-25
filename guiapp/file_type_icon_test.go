package guiapp

import "testing"

func TestAIAssistantFileTypeIconDataURLRejectsEmptyPath(t *testing.T) {
	if _, err := (&App{}).AIAssistantFileTypeIconDataURL("  "); err == nil {
		t.Fatal("expected an error for an empty attachment path")
	}
}

// The cloud drive must not retry a host that cannot answer at all.
func TestAIAssistantFileTypeIconDataURLIfAssociatedRejectsEmptyPath(t *testing.T) {
	if _, err := (&App{}).AIAssistantFileTypeIconDataURLIfAssociated("  "); err == nil {
		t.Fatal("expected an error for an empty attachment path")
	}
}

func TestFileTypeIconCacheKeyGroupsDocumentsAndKeepsPrograms(t *testing.T) {
	pdf := fileTypeIconCacheKey(`D:\docs\信息安全数学基础.pdf`)
	if pdf != fileTypeIconCacheKey(`C:/other/notes.PDF`) || pdf != ".pdf" {
		t.Fatalf("pdf key = %q", pdf)
	}
	if fileTypeIconCacheKey(`D:\bin\notepad.exe`) == fileTypeIconCacheKey(`D:\bin\calc.EXE`) {
		t.Fatal("executables should not share one icon")
	}
	if !fileTypeIconUsesOwnIcon(`D:\bin\app.exe`) || fileTypeIconUsesOwnIcon(`D:\docs\a.pdf`) {
		t.Fatal("only embedded-icon types should open the file")
	}
}

// The cloud drive lists files that live on Hub, so the row has a type but no
// local path. The lookup must still answer from the association.
func TestFileTypeIconCacheKeyAnswersFromExtensionAlone(t *testing.T) {
	if got := fileTypeIconCacheKey("file.pdf"); got != ".pdf" {
		t.Fatalf("bare type key = %q, want .pdf", got)
	}
	if got := fileTypeIconCacheKey("file.XLSX"); got != ".xlsx" {
		t.Fatalf("bare type key = %q, want .xlsx", got)
	}
}
