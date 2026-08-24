package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func officeWriteSheetPayload() map[string]interface{} {
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

func TestWriteExcelDetailedReportsTheOutcomeThroughTheError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.xlsx")

	if _, err := WriteExcelDetailed(map[string]interface{}{"data": officeWriteSheetPayload()}); err == nil || !strings.Contains(err.Error(), "office_write_path_required") {
		t.Fatalf("missing path err=%v", err)
	}
	if _, err := WriteExcelDetailed(map[string]interface{}{"path": path}); err == nil || !strings.Contains(err.Error(), "office_write_data_required") {
		t.Fatalf("missing data err=%v", err)
	}
	if _, err := WriteExcelDetailed(map[string]interface{}{"path": path, "data": "{not json"}); err == nil || !strings.Contains(err.Error(), "office_write_data_malformed") {
		t.Fatalf("malformed data err=%v", err)
	}

	// An empty sheet set is the failure whose message shares no word with any
	// other, which is why the outcome has to travel in the error.
	text, err := WriteExcelDetailed(map[string]interface{}{"path": path, "data": map[string]interface{}{"sheets": []interface{}{}}})
	if err == nil || !strings.Contains(err.Error(), "office_write_failed") {
		t.Fatalf("empty sheet set text=%q err=%v", text, err)
	}
	for _, word := range []string{"缺少", "错误", "失败"} {
		if strings.Contains(text, word) {
			t.Fatalf("empty sheet prose %q carries %q, so this test no longer covers the unsearchable failure", text, word)
		}
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("a failed write must not create the file")
	}

	written, err := WriteExcelDetailed(map[string]interface{}{"path": path, "data": officeWriteSheetPayload()})
	if err != nil || !strings.Contains(written, "已成功写入") {
		t.Fatalf("write=%q err=%v", written, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("spreadsheet missing: %v", statErr)
	}
}

func TestToolWriteExcelKeepsItsLegacyProse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.xlsx")
	if got := ToolWriteExcel(map[string]interface{}{}); got != "缺少 file_path 参数（也可用 path）" {
		t.Fatalf("missing path prose=%q", got)
	}
	if got := ToolWriteExcel(map[string]interface{}{"path": path}); got != "缺少 data 参数" {
		t.Fatalf("missing data prose=%q", got)
	}
	if got := ToolWriteExcel(map[string]interface{}{"path": path, "data": map[string]interface{}{"sheets": []interface{}{}}}); got != "data.sheets 不能为空" {
		t.Fatalf("empty sheet set prose=%q", got)
	}
	if got := ToolWriteExcel(map[string]interface{}{"path": path, "data": officeWriteSheetPayload()}); !strings.HasPrefix(got, "已成功写入 XLSX 文件: ") {
		t.Fatalf("success prose=%q", got)
	}
}
