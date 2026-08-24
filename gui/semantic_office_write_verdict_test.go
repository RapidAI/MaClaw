package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func trustedOfficeWriteSheets() map[string]interface{} {
	return map[string]interface{}{
		"sheets": []interface{}{
			map[string]interface{}{
				"name": "Sheet1",
				"rows": []interface{}{
					[]interface{}{map[string]interface{}{"value": "cell"}},
				},
			},
		},
	}
}

// The managed office write used to decide the outcome by searching the tool's
// prose for 缺少/错误/失败. An empty sheet set fails with none of those words,
// so the adapter announced a spreadsheet it had never created.
func TestTrustedOfficeWriteDoesNotAnnounceASpreadsheetItNeverWrote(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace

	got, err := h.writeTrustedOffice(principal, "book.xlsx", map[string]interface{}{"sheets": []interface{}{}})
	if err == nil {
		t.Fatalf("empty sheet set reported success=%q", got)
	}
	if !strings.Contains(err.Error(), "trusted_office_write_failed") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "book.xlsx")); statErr == nil {
		t.Fatal("a failed write must not leave a spreadsheet behind")
	}
}

// The success line carries the path, so scanning it for those same words let a
// filename overrule a write that had already happened.
func TestTrustedOfficeWriteJudgesTheWriteAndNotTheFilename(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace

	got, err := h.writeTrustedOffice(principal, "失败统计.xlsx", trustedOfficeWriteSheets())
	if err != nil {
		t.Fatalf("write=%q err=%v", got, err)
	}
	if !strings.Contains(got, "失败统计.xlsx") || strings.Contains(got, workspace) {
		t.Fatalf("write=%q", got)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "失败统计.xlsx")); statErr != nil {
		t.Fatalf("spreadsheet missing: %v", statErr)
	}
}
