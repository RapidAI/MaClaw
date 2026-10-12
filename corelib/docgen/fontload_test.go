package docgen

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	gopdf "github.com/VantageDataChat/GoPDF2"
	ttfcore "github.com/VantageDataChat/GoPDF2/fontmaker/core"
)

// windowsTTCFixture returns a real TrueType Collection available on Windows
// dev machines; tests skip when it is absent (e.g. CI on other platforms).
func windowsTTCFixture(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("fixture only available on windows")
	}
	const path = `C:\Windows\Fonts\msyh.ttc`
	if _, err := os.Stat(path); err != nil {
		t.Skip("msyh.ttc not available")
	}
	return path
}

func TestLoadFontFileReducesTTCToEmbeddableSfnt(t *testing.T) {
	path := windowsTTCFixture(t)
	data, err := loadFontFile(path)
	if err != nil {
		t.Fatalf("loadFontFile(%s) failed: %v", path, err)
	}
	if !bytes.Equal(data[:4], []byte{0x00, 0x01, 0x00, 0x00}) {
		t.Fatalf("expected plain sfnt magic, got %x", data[:4])
	}
	var parser ttfcore.TTFParser
	if err := parser.ParseByReader(bytes.NewReader(data)); err != nil {
		t.Fatalf("PDF engine TTFParser rejected extracted font: %v", err)
	}
}

// ttcFirstFontSpans returns the source (offset, length) of every table of
// the first font in a raw TrueType Collection.
func ttcFirstFontSpans(t *testing.T, raw []byte) map[string][2]int {
	t.Helper()
	firstOffset := int64(binary.BigEndian.Uint32(raw[12:16]))
	numTables := int(binary.BigEndian.Uint16(raw[firstOffset+4 : firstOffset+6]))
	dirStart := firstOffset + 12
	spans := make(map[string][2]int, numTables)
	for i := 0; i < numTables; i++ {
		entry := raw[dirStart+int64(i)*16 : dirStart+int64(i+1)*16]
		spans[string(entry[0:4])] = [2]int{
			int(binary.BigEndian.Uint32(entry[8:12])),
			int(binary.BigEndian.Uint32(entry[12:16])),
		}
	}
	return spans
}

func TestExtractTTCCopiesTableBytesVerbatim(t *testing.T) {
	path := windowsTTCFixture(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	extracted, err := normalizeFontData(raw)
	if err != nil {
		t.Fatalf("normalizeFontData: %v", err)
	}
	spans := ttcFirstFontSpans(t, raw)
	numTables := int(binary.BigEndian.Uint16(extracted[4:6]))
	if numTables != len(spans) {
		t.Fatalf("table count mismatch: extracted %d, source %d", numTables, len(spans))
	}
	checked := 0
	entries := extracted[12 : 12+numTables*16]
	for i := 0; i < numTables; i++ {
		entry := entries[i*16 : (i+1)*16]
		tag := string(entry[0:4])
		src, ok := spans[tag]
		if !ok {
			t.Fatalf("extracted font has table %s absent from source", tag)
		}
		off := int(binary.BigEndian.Uint32(entry[8:12]))
		length := int(binary.BigEndian.Uint32(entry[12:16]))
		if length != src[1] {
			t.Fatalf("table %s length changed: %d vs %d", tag, length, src[1])
		}
		want := raw[src[0] : src[0]+src[1]]
		got := extracted[off : off+length]
		if tag == "head" {
			// head.checkSumAdjustment is deliberately recomputed for the
			// rebuilt layout; the rest of the table must stay identical.
			want = append(append([]byte{}, want[:8]...), want[12:]...)
			got = append(append([]byte{}, got[:8]...), got[12:]...)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("table %s bytes differ after extraction", tag)
		}
		checked++
	}
	if checked < 8 {
		t.Fatalf("expected a realistic font with several tables, checked %d", checked)
	}
}

func TestExtractTTCIsCompactAndSpecValid(t *testing.T) {
	path := windowsTTCFixture(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	extracted, err := normalizeFontData(raw)
	if err != nil {
		t.Fatalf("normalizeFontData: %v", err)
	}
	// Only the first font's tables are copied — never the whole collection.
	if len(extracted) > len(raw) {
		t.Fatalf("extracted font (%d) must not exceed the source collection (%d)", len(extracted), len(raw))
	}
	// Rebuilt sfnt must satisfy the whole-font checksum invariant.
	if sum := sfntCheckSum(extracted); sum != checkSumMagicSum {
		t.Fatalf("checkSumAdjustment invalid: whole-font checksum = %#x, want %#x", sum, checkSumMagicSum)
	}
	// Tables must live inside the rebuilt buffer and stay 4-byte aligned.
	numTables := int(binary.BigEndian.Uint16(extracted[4:6]))
	entries := extracted[12 : 12+numTables*16]
	for i := 0; i < numTables; i++ {
		entry := entries[i*16 : (i+1)*16]
		off := int(binary.BigEndian.Uint32(entry[8:12]))
		length := int(binary.BigEndian.Uint32(entry[12:16]))
		if off%4 != 0 {
			t.Fatalf("table %s offset %d is not 4-byte aligned", string(entry[0:4]), off)
		}
		if off+length > len(extracted) {
			t.Fatalf("table %s runs past end of rebuilt font", string(entry[0:4]))
		}
	}
}

func TestNormalizeFontDataRejectsOTTO(t *testing.T) {
	data := append([]byte{}, ottoMagic...)
	data = append(data, make([]byte, 100)...)
	if _, err := normalizeFontData(data); err == nil {
		t.Fatal("expected OTTO font to be rejected")
	}
}

func TestNormalizeFontDataRejectsGarbage(t *testing.T) {
	data := append([]byte{}, 'N', 'O', 'P', 'E')
	data = append(data, make([]byte, 100)...)
	if _, err := normalizeFontData(data); err == nil {
		t.Fatal("expected garbage magic to be rejected")
	}
}

func TestNormalizeFontDataRejectsTCFFSubFont(t *testing.T) {
	// Build a synthetic TTC whose first sub-font claims OTTO (CFF outlines).
	buf := &bytes.Buffer{}
	buf.Write(ttcMagic)
	binary.Write(buf, binary.BigEndian, uint16(1)) // major
	binary.Write(buf, binary.BigEndian, uint16(0)) // minor
	binary.Write(buf, binary.BigEndian, uint32(1)) // numFonts
	binary.Write(buf, binary.BigEndian, uint32(16))
	dir := buf.Bytes()
	dir = append(dir, ottoMagic...)
	dir = append(dir, make([]byte, 64)...)
	if _, err := normalizeFontData(dir); err == nil {
		t.Fatal("expected CFF-outline TTC to be rejected")
	}
}

func TestNormalizeFontDataAcceptsPlainTTF(t *testing.T) {
	const path = `C:\Windows\Fonts\Deng.ttf`
	if _, err := os.Stat(path); err != nil {
		t.Skip("Deng.ttf not available")
	}
	data, err := loadFontFile(path)
	if err != nil {
		t.Fatalf("loadFontFile failed: %v", err)
	}
	if !bytes.Equal(data[:4], []byte{0x00, 0x01, 0x00, 0x00}) {
		t.Fatalf("expected plain sfnt passthrough, got %x", data[:4])
	}
}

func TestGenerateWithTTCCollectionFont(t *testing.T) {
	path := windowsTTCFixture(t)
	regular, err := loadFontFile(path)
	if err != nil {
		t.Fatalf("loadFontFile: %v", err)
	}
	g := &Generator{fontRegularData: regular, fontBoldData: regular}
	spec := Spec{
		Title:       "伴读导出验证",
		ProjectName: "伴读",
		Content:     "# 伴读导出验证\n\n这是从 TrueType Collection 字体渲染的中文段落，验证 TTC 提取后可正常生成 PDF。\n\n- 列表项一\n- 列表项二\n",
		Timestamp:   time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC),
	}
	pdfBytes, err := g.Generate(spec)
	if err != nil {
		t.Fatalf("Generate with TTC font failed: %v", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF document")
	}
}

// TestDarwinCandidatesStayEmbeddable guards the macOS font knowledge that is
// easy to forget: PingFang is CFF-outline and can never be embedded, so it
// must not come back into the candidate list.
func TestDarwinCandidatesStayEmbeddable(t *testing.T) {
	for _, c := range fontCandidatesFor("darwin") {
		for _, p := range []string{c.regular, c.bold} {
			if strings.Contains(strings.ToLower(p), "pingfang") {
				t.Fatalf("candidate %s is CFF-outline and can never be embedded", p)
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".ttc" && ext != ".ttf" && ext != ".otf" {
				t.Fatalf("candidate %s has unsupported extension %q", p, ext)
			}
		}
	}
}

// TestLoadFontCandidatesFallsThrough verifies the mechanism behind the macOS
// fix: a candidate that exists but cannot be embedded must be skipped, not
// fatal — and the reason must be recorded for the user-facing error.
func TestLoadFontCandidatesFallsThrough(t *testing.T) {
	const plain = `C:\Windows\Fonts\Deng.ttf`
	if _, err := os.Stat(plain); err != nil {
		t.Skip("Deng.ttf not available")
	}
	dir := t.TempDir()
	cffOnly := filepath.Join(dir, "cff-only.ttf")
	if err := os.WriteFile(cffOnly, append(append([]byte{}, ottoMagic...), make([]byte, 128)...), 0o644); err != nil {
		t.Fatal(err)
	}
	regular, bold, failure := loadFontCandidates([]fontCandidate{
		{cffOnly, cffOnly},
		{plain, plain},
	})
	if len(regular) == 0 || len(bold) == 0 {
		t.Fatal("detection must fall through to the usable candidate")
	}
	if failure == "" {
		t.Fatal("rejection reason must be recorded for the no-font error")
	}
	if !bytes.HasPrefix(regular, []byte{0x00, 0x01, 0x00, 0x00}) {
		t.Fatalf("expected plain sfnt data, got %x", regular[:4])
	}
}

// TestFontCoversCJKSeparatesChineseFromLatinOnly guards the silent failure
// mode: an embeddable font without CJK glyphs yields a PDF of empty boxes.
func TestFontCoversCJKSeparatesChineseFromLatinOnly(t *testing.T) {
	const latin = `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(latin); err != nil {
		t.Skip("arial.ttf not available")
	}
	latinData, err := os.ReadFile(latin)
	if err != nil {
		t.Skip("arial.ttf unreadable")
	}
	if err := fontCoversCJK(latinData); err == nil {
		t.Fatal("a Latin-only font must be reported as lacking CJK coverage")
	}

	path := windowsTTCFixture(t)
	cjkData, err := loadFontFile(path)
	if err != nil {
		t.Fatalf("loadFontFile: %v", err)
	}
	if err := fontCoversCJK(cjkData); err != nil {
		t.Fatalf("a Chinese font must pass the coverage probe: %v", err)
	}
}

func TestLoadFontCandidatesPrefersCJKOverLatinOnly(t *testing.T) {
	const latin = `C:\Windows\Fonts\arial.ttf`
	const cjk = `C:\Windows\Fonts\Deng.ttf`
	if _, err := os.Stat(latin); err != nil {
		t.Skip("arial.ttf not available")
	}
	want, err := os.ReadFile(cjk)
	if err != nil {
		t.Skip("Deng.ttf not available")
	}
	got, _, _ := loadFontCandidates([]fontCandidate{{latin, latin}, {cjk, cjk}})
	if !bytes.Equal(got, want) {
		t.Fatalf("detection must skip the Latin-only font and pick the CJK one")
	}
}

func TestLoadFontCandidatesAcceptsLatinOnlyAsLastResort(t *testing.T) {
	const latin = `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(latin); err != nil {
		t.Skip("arial.ttf not available")
	}
	got, _, _ := loadFontCandidates([]fontCandidate{{latin, latin}})
	if len(got) == 0 {
		t.Fatal("Latin-only systems must still be able to export English documents")
	}
}

func TestNewFallsThroughToUsableFont(t *testing.T) {
	// Candidate validation happens inside detectSystemFonts; on any machine
	// where at least one candidate is usable, HasFont must hold.
	g := New()
	if !g.HasFont() {
		t.Skip("no usable system font on this machine")
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := g.addFonts(pdf); err != nil {
		t.Fatalf("detected font must load into the PDF engine: %v", err)
	}
}

// TestRebuiltSfntSearchFieldsMatchSpec pins the binary-search hint fields of
// the rebuilt header. entrySelector counts *tables* (log2 of the power of two
// used for searchRange), not directory bytes — getting this wrong produced 8
// for a 25-table font where the original says 4, which strict parsers reject.
func TestRebuiltSfntSearchFieldsMatchSpec(t *testing.T) {
	path := windowsTTCFixture(t)
	data, err := loadFontFile(path)
	if err != nil {
		t.Fatal(err)
	}
	numTables := binary.BigEndian.Uint16(data[4:6])
	searchRange := binary.BigEndian.Uint16(data[6:8])
	entrySelector := binary.BigEndian.Uint16(data[8:10])
	rangeShift := binary.BigEndian.Uint16(data[10:12])

	if int(searchRange)+int(rangeShift) != int(numTables)*sfntDirEntryLen {
		t.Fatalf("searchRange+rangeShift = %d, want %d", int(searchRange)+int(rangeShift), int(numTables)*sfntDirEntryLen)
	}
	// entrySelector must be log2(searchRange/16): searchRange is 16*2^es.
	wantES := uint16(0)
	for pow := uint16(1); pow*2*sfntDirEntryLen <= searchRange; pow *= 2 {
		wantES++
	}
	if entrySelector != wantES {
		t.Fatalf("entrySelector = %d, want %d (numTables=%d, searchRange=%d)",
			entrySelector, wantES, numTables, searchRange)
	}
	if searchRange == 0 || uint32(searchRange) > uint32(numTables)*uint32(sfntDirEntryLen) {
		t.Fatalf("searchRange %d out of range for %d tables", searchRange, numTables)
	}
}

// TestMeasurementPDFReuseKeepsHeightsStable pins the optimisation itself:
// the pager reuses one document for every measurement instead of building a
// new one per candidate split (~130x faster). That is only safe if heights
// do not depend on what the shared document drew earlier, so measure the
// same markdown repeatedly and require identical results.
func TestMeasurementPDFReuseKeepsHeightsStable(t *testing.T) {
	path := windowsTTCFixture(t)
	data, err := loadFontFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := &Generator{fontRegularData: data, fontBoldData: data}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	m, err := g.newMeasurer(layout)
	if err != nil {
		t.Fatal(err)
	}
	markdown := "## 小节\n\n中文正文一段，用于确认重复测量结果稳定。\n\n第二段内容。\n\n"
	want, err := m.measureMarkdownHeight(layout, 60, markdown, false)
	if err != nil {
		t.Fatal(err)
	}
	// Interleave a different measurement: if the shared document leaked state
	// between measurements, the next height would drift.
	for i := 0; i < 5; i++ {
		other, err := m.measureMarkdownHeight(layout, 60, "另一个完全不同的段落用于打乱状态。\n\n### 三级标题\n\n", false)
		if err != nil {
			t.Fatal(err)
		}
		if other <= 0 {
			t.Fatalf("interleaved measurement returned %v", other)
		}
		got, err := m.measureMarkdownHeight(layout, 60, markdown, false)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("measurement #%d drifted with a reused document: got %v, want %v", i, got, want)
		}
	}
	// A second measurer must agree with the first: no state escapes one render.
	fresh, err := g.newMeasurer(layout)
	if err != nil {
		t.Fatal(err)
	}
	again, err := fresh.measureMarkdownHeight(layout, 60, markdown, false)
	if err != nil {
		t.Fatal(err)
	}
	if again != want {
		t.Fatalf("a fresh measurer disagrees with a used one: got %v, want %v", again, want)
	}
}

// TestConcurrentExportsShareOneGenerator pins that Generator stays stateless
// for concurrent use. SwarmOrchestrator and guiapp cache a single generator
// for the process lifetime, so anything mutable during rendering must live in
// per-render state (measurer) rather than on the Generator itself.
func TestConcurrentExportsShareOneGenerator(t *testing.T) {
	path := windowsTTCFixture(t)
	data, err := loadFontFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := &Generator{fontRegularData: data, fontBoldData: data}
	var content strings.Builder
	for i := 0; i < 20; i++ {
		content.WriteString("## 标题\n\n中文正文段落用于并发验证。\n\n")
	}
	spec := Spec{Title: "并发", Content: content.String(), Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	want, err := g.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := g.Generate(spec)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, want) {
				t.Error("concurrent export produced different output")
			}
		}()
	}
	wg.Wait()
}
