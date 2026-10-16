package excel

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	sheetNameRe = regexp.MustCompile(`<sheet[^>]*name="([^"]+)"[^>]*r:id="([^"]+)"`)
	relTargetRe = regexp.MustCompile(`Id="([^"]+)"[^>]*Target="([^"]+)"`)
	mergeRefRe  = regexp.MustCompile(`<mergeCell[^>]*ref="([^"]+)"`)
)

// patchExistingXLSXRange updates only the A1 rectangle inside an existing
// OOXML workbook zip. Other sheets, styles.xml, and cell style indices (`s=`)
// outside the rectangle are left intact.
func patchExistingXLSXRange(filePath, sheetName, rangeText string, rows [][]WriteCell) error {
	startCol, startRow, endCol, endRow, err := ParseRange(rangeText)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open workbook zip: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	sheetPath, err := sheetPathForName(files, sheetName)
	if err != nil {
		return err
	}
	sheetXML, err := readZipFile(files[sheetPath])
	if err != nil {
		return err
	}
	if mergeOverlaps(sheetXML, startCol, startRow, endCol, endRow) {
		return fmt.Errorf("range overlaps merged cells")
	}
	patched, err := patchSheetXML(sheetXML, startCol, startRow, rows)
	if err != nil {
		return err
	}
	if hasWriteValues(rows) && bytes.Equal(patched, sheetXML) {
		return fmt.Errorf("failed to apply range write to sheet %q", sheetName)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		if err != nil {
			_ = zw.Close()
			return err
		}
		if f.Name == sheetPath {
			if _, err := w.Write(patched); err != nil {
				_ = zw.Close()
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			_ = zw.Close()
			return err
		}
		_, copyErr := io.Copy(w, rc)
		_ = rc.Close()
		if copyErr != nil {
			_ = zw.Close()
			return copyErr
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filePath)
}

func sheetPathForName(files map[string]*zip.File, sheetName string) (string, error) {
	wb, err := readZipFile(files["xl/workbook.xml"])
	if err != nil {
		return "", fmt.Errorf("sheet %q not found", sheetName)
	}
	rels, _ := readZipFile(files["xl/_rels/workbook.xml.rels"])
	idToTarget := map[string]string{}
	for _, m := range relTargetRe.FindAllSubmatch(rels, -1) {
		idToTarget[string(m[1])] = string(m[2])
	}
	for _, m := range sheetNameRe.FindAllSubmatch(wb, -1) {
		if string(m[1]) != sheetName {
			continue
		}
		target := idToTarget[string(m[2])]
		if target == "" {
			break
		}
		target = strings.TrimPrefix(target, "/")
		if !strings.HasPrefix(target, "xl/") {
			target = "xl/" + strings.TrimPrefix(target, "../")
		}
		if files[target] == nil {
			target = filepath.ToSlash(filepath.Join("xl", strings.TrimPrefix(string(m[2]), "rId")+".xml"))
		}
		if files[target] != nil {
			return target, nil
		}
		// Common mapping rIdN -> worksheets/sheetN.xml
		guess := "xl/worksheets/sheet" + strings.TrimPrefix(string(m[2]), "rId") + ".xml"
		if files[guess] != nil {
			return guess, nil
		}
	}
	return "", fmt.Errorf("sheet %q not found", sheetName)
}

func mergeOverlaps(sheetXML []byte, startCol, startRow, endCol, endRow int) bool {
	for _, m := range mergeRefRe.FindAllSubmatch(sheetXML, -1) {
		sc, sr, ec, er, err := ParseRange(string(m[1]))
		if err != nil {
			continue
		}
		if sc <= endCol && ec >= startCol && sr <= endRow && er >= startRow {
			return true
		}
	}
	return false
}

func hasWriteValues(rows [][]WriteCell) bool {
	for _, row := range rows {
		for _, cell := range row {
			if cell.Value != nil {
				return true
			}
		}
	}
	return false
}

func patchSheetXML(sheetXML []byte, startCol, startRow int, rows [][]WriteCell) ([]byte, error) {
	out := sheetXML
	for i, row := range rows {
		rowNum := startRow + i
		for j, cell := range row {
			if cell.Value == nil {
				continue
			}
			ref := cellRef(startCol+j, rowNum)
			out = upsertCellXML(out, ref, rowNum, cell.Value)
		}
	}
	return out, nil
}

var styleAttrRe = regexp.MustCompile(`\bs="[^"]*"`)

func upsertCellXML(sheetXML []byte, ref string, rowNum int, value interface{}) []byte {
	inner := buildCellXML(ref, "", cellTypeAttr(value), cellInnerXML(value))
	openRe := regexp.MustCompile(`<c\b[^>]*\br="` + regexp.QuoteMeta(ref) + `"[^>]*/?>`)
	if loc := openRe.FindIndex(sheetXML); loc != nil {
		open := sheetXML[loc[0]:loc[1]]
		end := loc[1]
		if !isSelfClosingXML(open) {
			closeIdx := bytes.Index(sheetXML[loc[1]:], []byte("</c>"))
			if closeIdx < 0 {
				return sheetXML
			}
			end = loc[1] + closeIdx + len("</c>")
		}
		repl := []byte(buildCellXML(ref, styleAttrFromOpen(open), cellTypeAttr(value), cellInnerXML(value)))
		return concatBytes(sheetXML[:loc[0]], repl, sheetXML[end:])
	}
	rowPat := regexp.MustCompile(`<row r="` + strconv.Itoa(rowNum) + `"[^>]*/?>`)
	if loc := rowPat.FindIndex(sheetXML); loc != nil {
		open := sheetXML[loc[0]:loc[1]]
		if isSelfClosingXML(open) {
			expanded := bytes.TrimSuffix(bytes.TrimSpace(open), []byte("/>"))
			expanded = append(expanded, '>')
			rowXML := concatBytes(expanded, []byte(inner), []byte("</row>"))
			return concatBytes(sheetXML[:loc[0]], rowXML, sheetXML[loc[1]:])
		}
		return concatBytes(sheetXML[:loc[1]], []byte(inner), sheetXML[loc[1]:])
	}
	rowXML := []byte(`<row r="` + strconv.Itoa(rowNum) + `">` + inner + `</row>`)
	if idx := bytes.LastIndex(sheetXML, []byte("</sheetData>")); idx >= 0 {
		return concatBytes(sheetXML[:idx], rowXML, sheetXML[idx:])
	}
	return sheetXML
}

func buildCellXML(ref, style, typeAttr, body string) string {
	return `<c r="` + ref + `"` + style + typeAttr + `>` + body + `</c>`
}

func isSelfClosingXML(open []byte) bool {
	trimmed := bytes.TrimSpace(open)
	return bytes.HasSuffix(trimmed, []byte("/>"))
}

func styleAttrFromOpen(open []byte) string {
	m := styleAttrRe.Find(open)
	if m == nil {
		return ""
	}
	return " " + string(m)
}

func concatBytes(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func cellTypeAttr(value interface{}) string {
	if s, ok := value.(string); ok {
		if strings.HasPrefix(s, "=") {
			return ""
		}
		// GoExcel's reader understands t="str" via <v>, not OOXML inlineStr.
		return ` t="str"`
	}
	if _, ok := value.(bool); ok {
		return ` t="b"`
	}
	return ""
}

func cellInnerXML(value interface{}) string {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "=") {
			return "<f>" + xmlEscape(v[1:]) + "</f>"
		}
		return "<v>" + xmlEscape(v) + "</v>"
	case bool:
		if v {
			return "<v>1</v>"
		}
		return "<v>0</v>"
	default:
		return "<v>" + xmlEscape(fmt.Sprint(v)) + "</v>"
	}
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func readZipFile(f *zip.File) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("missing zip entry")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
