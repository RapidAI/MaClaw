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

func TestWritePPTXDetailedReportsTheOutcomeThroughTheError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.pptx")

	if _, err := WritePPTXDetailed(map[string]interface{}{"data": map[string]interface{}{"title": "t"}}); err == nil || !strings.Contains(err.Error(), "office_write_path_required") {
		t.Fatalf("missing path err=%v", err)
	}
	if _, err := WritePPTXDetailed(map[string]interface{}{"path": path}); err == nil || !strings.Contains(err.Error(), "office_write_data_required") {
		t.Fatalf("missing data err=%v", err)
	}
	if _, err := WritePPTXDetailed(map[string]interface{}{"path": path, "data": "{not json"}); err == nil || !strings.Contains(err.Error(), "office_write_data_malformed") {
		t.Fatalf("malformed data err=%v", err)
	}
	if _, err := WritePPTXDetailed(map[string]interface{}{"path": path, "data": map[string]interface{}{}}); err == nil {
		t.Fatal("empty outline must fail")
	}

	text, err := WritePPTXDetailed(map[string]interface{}{"path": path, "data": map[string]interface{}{
		"title": "庆祝布偶宝宝5岁生日",
		"slides": []interface{}{
			map[string]interface{}{"title": "关于布偶宝宝", "bullets": []interface{}{"温顺粘人", "蓝眼睛"}},
		},
	}})
	if err != nil || !strings.Contains(text, path) {
		t.Fatalf("write text=%q err=%v", text, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("deck missing: %v", statErr)
	}

	chartPath := filepath.Join(t.TempDir(), "charts.pptx")
	text, err = WritePPTXDetailed(map[string]interface{}{"path": chartPath, "data": map[string]interface{}{
		"title": "对照",
		"slides": []interface{}{
			map[string]interface{}{
				"title": "公开证据",
				"charts": []interface{}{
					map[string]interface{}{
						"chart_type": "bar",
						"categories": []interface{}{"Stars", "Forks"},
						"series": []interface{}{
							map[string]interface{}{"name": "公开", "values": []interface{}{139, 26}},
						},
					},
				},
			},
		},
	}})
	if err != nil || !strings.Contains(text, chartPath) {
		t.Fatalf("chart write text=%q err=%v", text, err)
	}
}

// The advertised write_excel schema (legacy registry and governed office
// surface alike) shows rows of plain cells ("rows": [["a", "b"]]). The
// writer contract requires {"value": ...} objects. The choke-point
// normalization must accept the advertised form, so a model that follows the
// schema succeeds (2026-08-26: managed-surface spreadsheet writes always
// died at the writer unmarshal).
func TestWriteExcelDetailedAcceptsPlainCellRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.xlsx")
	plain := map[string]interface{}{
		"sheets": []interface{}{
			map[string]interface{}{
				"name": "Sheet1",
				"rows": []interface{}{
					[]interface{}{"名称", "数量"},
					[]interface{}{"布偶猫", 5.0},
				},
			},
		},
	}
	written, err := WriteExcelDetailed(map[string]interface{}{"path": path, "data": plain})
	if err != nil || !strings.Contains(written, "已成功写入") {
		t.Fatalf("plain-cell write=%q err=%v", written, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("spreadsheet missing: %v", statErr)
	}
	// The JSON-string data form takes the same path.
	path2 := filepath.Join(t.TempDir(), "plain2.xlsx")
	written2, err := WriteExcelDetailed(map[string]interface{}{"path": path2, "data": `{"sheets":[{"name":"S","rows":[["x"]]}]}`})
	if err != nil || !strings.Contains(written2, "已成功写入") {
		t.Fatalf("string-data plain-cell write=%q err=%v", written2, err)
	}
}
