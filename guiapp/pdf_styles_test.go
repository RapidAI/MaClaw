package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

func listPDFStylesForTest(t *testing.T, app *App) []docgen.StyleInfo {
	t.Helper()
	listed, err := app.ListPDFStyles()
	if err != nil {
		t.Fatal(err)
	}
	if listed.Warning != "" {
		t.Fatalf("unexpected warning %q", listed.Warning)
	}
	return listed.Styles
}

func TestPDFStyleSelectionPersistsAndDefaultsToGazette(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	infos := listPDFStylesForTest(t, app)
	if len(infos) != 5 || infos[0].ID != "gazette" || !infos[0].Active || infos[0].Motif != docgen.MotifMasthead {
		t.Fatalf("default list = %+v", infos)
	}
	if err := app.SetPDFStyle("atlas"); err != nil {
		t.Fatal(err)
	}
	if docgen.ActiveStyle().ID != "atlas" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
	reloaded := listPDFStylesForTest(t, &App{testHomeDir: app.testHomeDir})
	var atlas docgen.StyleInfo
	for _, info := range reloaded {
		if info.ID == "atlas" {
			atlas = info
		}
	}
	if !atlas.Active || atlas.Motif != docgen.MotifBand {
		t.Fatalf("reloaded atlas = %+v", atlas)
	}
	if err := app.SetPDFStyle("missing"); err == nil {
		t.Fatal("missing style accepted")
	}
	if err := app.DeletePDFStyle("gazette"); err == nil {
		t.Fatal("builtin style deleted")
	}
}

func TestParseAndStoreGeneratedPDFStyle(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	raw := `{
	  "id": "harbor",
	  "label": "港务",
	  "summary": "深色港务通报",
	  "keywords": ["港务", "通报"],
	  "motif": "rail",
	  "colors": {"ink":"D4D4D8","navy":"134E4A","accent":"0F766E","gold":"B45309","meta":"E5E5E5","rule":"0F766E","quote":"115E59","link":"0F766E","on_dark":"111111"}
	}`
	style, err := parseGeneratedPDFStyle("说明\n" + raw)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SetPDFStyle("brief"); err != nil {
		t.Fatal(err)
	}
	stored, err := app.storeGeneratedPDFStyle(style)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != "harbor" || stored.Motif != docgen.MotifRail {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.Colors.Ink == "D4D4D8" || stored.Colors.OnDark == "111111" {
		t.Fatalf("pale colors kept: %+v", stored.Colors)
	}
	body, err := os.ReadFile(app.pdfStylePath())
	if err != nil {
		t.Fatal(err)
	}
	var file pdfStyleFile
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	if file.Active != "brief" || len(file.Styles) != 1 || file.Styles[0].ID != "harbor" {
		t.Fatalf("file = %+v", file)
	}
	if docgen.ActiveStyle().ID != "brief" {
		t.Fatalf("active changed to %s", docgen.ActiveStyle().ID)
	}
	infos := listPDFStylesForTest(t, app)
	if len(infos) != 6 {
		t.Fatalf("len = %d", len(infos))
	}
	if err := app.DeletePDFStyle("harbor"); err != nil {
		t.Fatal(err)
	}
	if _, ok := docgen.LookupStyle("harbor"); ok {
		t.Fatal("deleted style still installed")
	}
	if docgen.ActiveStyle().ID != "brief" {
		t.Fatalf("active after delete = %s", docgen.ActiveStyle().ID)
	}
}

func TestPDFStyleLoadSkipsDuplicateAndKeepsActiveBuiltin(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	colors := docgen.StyleColors{Ink: "1C1917", Navy: "134E4A", Accent: "0F766E", Gold: "B45309", Meta: "44403C", Rule: "0F766E", Quote: "115E59", Link: "0F766E", OnDark: "F0FDFA"}
	harbor := docgen.CustomStyle{ID: "harbor", Label: "港务", Summary: "深色港务通报", Keywords: []string{"港务"}, Motif: "rail", Colors: colors}
	dup := harbor
	dup.Label = "重复"
	broken := harbor
	broken.ID = "broken"
	broken.Label = ""
	if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor, dup, broken}}); err != nil {
		t.Fatal(err)
	}
	infos := listPDFStylesForTest(t, app)
	if len(infos) != 6 {
		t.Fatalf("len = %d", len(infos))
	}
	if docgen.ActiveStyle().ID != "atlas" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
	if _, ok := docgen.LookupStyle("harbor"); !ok {
		t.Fatal("first custom style was dropped")
	}
}

func TestDeleteActiveCustomPDFStyleRestoresGazette(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	colors := docgen.StyleColors{Ink: "1C1917", Navy: "134E4A", Accent: "0F766E", Gold: "B45309", Meta: "44403C", Rule: "0F766E", Quote: "115E59", Link: "0F766E", OnDark: "F0FDFA"}
	if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{
		Active: "harbor",
		Styles: []docgen.CustomStyle{{ID: "harbor", Label: "港务", Summary: "深色港务通报", Keywords: []string{"港务"}, Motif: "rail", Colors: colors}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.DeletePDFStyle("harbor"); err != nil {
		t.Fatal(err)
	}
	if docgen.ActiveStyle().ID != "gazette" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
	if _, ok := docgen.LookupStyle("harbor"); ok {
		t.Fatal("deleted style still installed")
	}
}

func TestPDFStyleWritesKeepRecordsTheCatalogSkips(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	colors := docgen.StyleColors{Ink: "1C1917", Navy: "134E4A", Accent: "0F766E", Gold: "B45309", Meta: "44403C", Rule: "0F766E", Quote: "115E59", Link: "0F766E", OnDark: "F0FDFA"}
	harbor := docgen.CustomStyle{ID: "harbor", Label: "港务", Summary: "深色港务通报", Keywords: []string{"港务"}, Motif: "rail", Colors: colors}
	broken := harbor
	broken.ID = "broken"
	broken.Label = ""
	if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor, broken}}); err != nil {
		t.Fatal(err)
	}
	if err := app.SetPDFStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	file := readPDFStyleTestFile(t, app)
	if file.Active != "ledger" || len(file.Styles) != 2 || file.Styles[1].ID != "broken" {
		t.Fatalf("selection rewrote the catalog: %+v", file)
	}
	if _, ok := docgen.LookupStyle("broken"); ok {
		t.Fatal("invalid record was installed")
	}
	if err := app.DeletePDFStyle("harbor"); err != nil {
		t.Fatal(err)
	}
	file = readPDFStyleTestFile(t, app)
	if file.Active != "ledger" || len(file.Styles) != 1 || file.Styles[0].ID != "broken" {
		t.Fatalf("delete dropped an unrelated record: %+v", file)
	}
	pier := harbor
	pier.ID = "pier"
	pier.Label = "码头"
	if _, err := app.storeGeneratedPDFStyle(pier); err != nil {
		t.Fatal(err)
	}
	file = readPDFStyleTestFile(t, app)
	if file.Active != "ledger" || len(file.Styles) != 2 || file.Styles[0].ID != "broken" || file.Styles[1].ID != "pier" {
		t.Fatalf("generate dropped an unrepaired record: %+v", file)
	}
}

func readPDFStyleTestFile(t *testing.T, app *App) pdfStyleFile {
	t.Helper()
	body, err := os.ReadFile(app.pdfStylePath())
	if err != nil {
		t.Fatal(err)
	}
	var file pdfStyleFile
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestGeneratedPDFStyleErrorsStayReadable(t *testing.T) {
	_, err := parseGeneratedPDFStyle(`{"id":"wave-1","label":"浪","summary":"一种说明","keywords":["波浪"],"motif":"wave","colors":{"ink":"1C1917","navy":"0E2A47","accent":"C2410C","gold":"A16207","meta":"3F3F46","rule":"C2410C","quote":"7C2D12","link":"1D4ED8","on_dark":"F8FAFC"}}`)
	if err == nil || strings.Contains(err.Error(), "pdf_style_") {
		t.Fatalf("error = %v", err)
	}
}

func TestPDFStyleCallErrorHidesTheEndpoint(t *testing.T) {
	secret := "https://token.example.test/v1/chat"
	deadline := &url.Error{Op: "Post", URL: secret, Err: context.DeadlineExceeded}
	got := pdfStyleCallError(deadline)
	if got == nil || got.Error() != "生成超时，请再试一次" || strings.Contains(got.Error(), "token.example") {
		t.Fatalf("deadline: %v", got)
	}
	refused := &url.Error{Op: "Post", URL: secret, Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}
	got = pdfStyleCallError(refused)
	if got == nil || got.Error() != "模型请求失败" || strings.Contains(got.Error(), "token.example") {
		t.Fatalf("dial: %v", got)
	}
	keep := errors.New("模型请求失败（HTTP 503）")
	if pdfStyleCallError(keep).Error() != keep.Error() {
		t.Fatalf("status rewritten: %v", pdfStyleCallError(keep))
	}
	text, err := pdfStyleModelText("", errors.New("empty caption response"))
	if text != "" || err == nil || err.Error() != "模型没有返回内容" {
		t.Fatalf("empty: %q %v", text, err)
	}
}

func TestListPDFStylesKeepsBuiltinsWhenTheFileCannotBeParsed(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	for _, body := range []string{"{", "[]"} {
		app := &App{testHomeDir: t.TempDir()}
		writeRawPDFStyleFile(t, app, body)
		if err := docgen.SetActiveStyle("ledger"); err != nil {
			t.Fatal(err)
		}
		listed, err := app.ListPDFStyles()
		if err != nil {
			t.Fatal(err)
		}
		if listed.Warning != pdfStyleCorruptWarning {
			t.Fatalf("body %q warning %q", body, listed.Warning)
		}
		if len(listed.Styles) != 5 || listed.Styles[0].ID != "gazette" || !listed.Styles[0].Active {
			t.Fatalf("body %q list %+v", body, listed.Styles)
		}
		if docgen.ActiveStyle().ID != "gazette" {
			t.Fatalf("body %q active %s", body, docgen.ActiveStyle().ID)
		}
		if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != body {
			t.Fatalf("body changed from %q to %q", body, got)
		}
		if _, err := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(err) {
			t.Fatalf("list moved the file aside: %v", err)
		}
	}
}

func TestSetPDFStyleQuarantinesAnUnreadableCatalogOnce(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	writeRawPDFStyleFile(t, app, "{")
	if err := app.SetPDFStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath()+".bad")); got != "{" {
		t.Fatalf("backup = %q", got)
	}
	file := readPDFStyleTestFile(t, app)
	if file.Active != "ledger" || len(file.Styles) != 0 {
		t.Fatalf("written file = %+v", file)
	}
	if docgen.ActiveStyle().ID != "ledger" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
	writeRawPDFStyleFile(t, app, "[")
	err := app.SetPDFStyle("atlas")
	if err == nil || !strings.Contains(err.Error(), "pdf_styles.json.bad") {
		t.Fatalf("second quarantine = %v", err)
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != "[" {
		t.Fatalf("second file = %q", got)
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath()+".bad")); got != "{" {
		t.Fatalf("backup overwritten = %q", got)
	}
	if docgen.ActiveStyle().ID != "ledger" {
		t.Fatalf("active changed to %s", docgen.ActiveStyle().ID)
	}
}

func TestRejectedPDFStyleChangeLeavesAnUnreadableCatalogInPlace(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	writeRawPDFStyleFile(t, app, "{")
	if err := docgen.SetActiveStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	if err := app.SetPDFStyle("missing"); err == nil || err.Error() != "没有这套风格" {
		t.Fatalf("select = %v", err)
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != "{" {
		t.Fatalf("select moved the file to %q", got)
	}
	if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
		t.Fatalf("select left a backup: %v", statErr)
	}
	if docgen.ActiveStyle().ID != "ledger" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
	rejected := docgen.CustomStyle{ID: "harbor", Label: "", Summary: "深色港务通报", Keywords: []string{"港务"}, Motif: "rail"}
	if _, err := app.storeGeneratedPDFStyle(rejected); err == nil {
		t.Fatal("invalid style stored")
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != "{" {
		t.Fatalf("generate moved the file to %q", got)
	}
	if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
		t.Fatalf("generate left a backup: %v", statErr)
	}
}

func TestUnreadableCatalogReturnsWhenTheReplacementFails(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
		renamePDFStyleFile = os.Rename
	})
	app := &App{testHomeDir: t.TempDir()}
	writeRawPDFStyleFile(t, app, "{")
	if err := docgen.SetActiveStyle("brief"); err != nil {
		t.Fatal(err)
	}
	renamePDFStyleFile = func(oldPath, newPath string) error {
		if strings.HasSuffix(oldPath, ".tmp") {
			return os.ErrInvalid
		}
		return os.Rename(oldPath, newPath)
	}
	if err := app.SetPDFStyle("ledger"); err == nil {
		t.Fatal("replacement succeeded")
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != "{" {
		t.Fatalf("file = %q", got)
	}
	if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
		t.Fatalf("backup stayed behind: %v", statErr)
	}
	if _, statErr := os.Stat(app.pdfStylePath() + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temp file stayed behind: %v", statErr)
	}
	if docgen.ActiveStyle().ID != "brief" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
}

func TestDeletePDFStyleLeavesAnUnreadableCatalogUntouched(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	app := &App{testHomeDir: t.TempDir()}
	writeRawPDFStyleFile(t, app, "{")
	if err := docgen.SetActiveStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	err := app.DeletePDFStyle("harbor")
	if err == nil || err.Error() != "风格文件无法读取，未改动原文件" {
		t.Fatalf("delete = %v", err)
	}
	if got := string(readRawPDFStyleFile(t, app.pdfStylePath())); got != "{" {
		t.Fatalf("file = %q", got)
	}
	if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
		t.Fatalf("delete moved the file aside: %v", statErr)
	}
	if docgen.ActiveStyle().ID != "ledger" {
		t.Fatalf("active = %s", docgen.ActiveStyle().ID)
	}
}

func TestPDFStyleReadRepairsAnInterruptedReplace(t *testing.T) {
	t.Cleanup(func() {
		_ = docgen.SetCustomStyles(nil)
		_ = docgen.SetActiveStyle("")
	})
	colors := docgen.StyleColors{Ink: "1C1917", Navy: "134E4A", Accent: "0F766E", Gold: "B45309", Meta: "44403C", Rule: "0F766E", Quote: "115E59", Link: "0F766E", OnDark: "F0FDFA"}
	harbor := docgen.CustomStyle{ID: "harbor", Label: "港务", Summary: "深色港务通报", Keywords: []string{"港务"}, Motif: "rail", Colors: colors}

	t.Run("empty", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		writeRawPDFStyleFile(t, app, "")
		listed, err := app.ListPDFStyles()
		if err != nil {
			t.Fatal(err)
		}
		if listed.Warning != "" || len(listed.Styles) != 5 || !listed.Styles[0].Active {
			t.Fatalf("list %+v warning %q", listed.Styles, listed.Warning)
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
			t.Fatalf("empty file quarantined: %v", statErr)
		}
		if err := app.SetPDFStyle("ledger"); err != nil {
			t.Fatal(err)
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".bad"); !os.IsNotExist(statErr) {
			t.Fatalf("empty file quarantined on select: %v", statErr)
		}
		if got := readPDFStyleTestFile(t, app); got.Active != "ledger" {
			t.Fatalf("active file = %+v", got)
		}
	})

	t.Run("bom", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		writeRawPDFStyleFile(t, app, "\uFEFF"+`{"active":"atlas","styles":[]}`)
		listed, err := app.ListPDFStyles()
		if err != nil {
			t.Fatal(err)
		}
		if listed.Warning != "" || docgen.ActiveStyle().ID != "atlas" {
			t.Fatalf("warning %q active %s", listed.Warning, docgen.ActiveStyle().ID)
		}
		if !bytes.HasPrefix(readRawPDFStyleFile(t, app.pdfStylePath()), []byte{0xEF, 0xBB, 0xBF}) {
			t.Fatal("list rewrote the file")
		}
	})

	t.Run("bak", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor}}); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(app.pdfStylePath(), app.pdfStylePath()+".bak"); err != nil {
			t.Fatal(err)
		}
		infos := listPDFStylesForTest(t, app)
		if len(infos) != 6 || docgen.ActiveStyle().ID != "atlas" {
			t.Fatalf("recovered %+v active %s", infos, docgen.ActiveStyle().ID)
		}
		if _, ok := docgen.LookupStyle("harbor"); !ok {
			t.Fatal("custom style was not recovered")
		}
		got := readPDFStyleTestFile(t, app)
		if got.Active != "atlas" || len(got.Styles) != 1 || got.Styles[0].ID != "harbor" {
			t.Fatalf("restored file = %+v", got)
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".bak"); !os.IsNotExist(statErr) {
			t.Fatalf("bak still beside the restored file: %v", statErr)
		}
	})

	t.Run("tmp", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor}}); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(app.pdfStylePath(), app.pdfStylePath()+".bak"); err != nil {
			t.Fatal(err)
		}
		ledger, err := json.MarshalIndent(pdfStyleFile{Active: "ledger"}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(app.pdfStylePath()+".tmp", ledger, 0o644); err != nil {
			t.Fatal(err)
		}
		infos := listPDFStylesForTest(t, app)
		if len(infos) != 5 || docgen.ActiveStyle().ID != "ledger" {
			t.Fatalf("list %+v active %s", infos, docgen.ActiveStyle().ID)
		}
		if _, ok := docgen.LookupStyle("harbor"); ok {
			t.Fatal("older bak replaced the newer temp catalog")
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".bak"); statErr != nil {
			t.Fatalf("bak = %v", statErr)
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".tmp"); !os.IsNotExist(statErr) {
			t.Fatalf("tmp was not moved: %v", statErr)
		}
	})

	t.Run("broken tmp", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor}}); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(app.pdfStylePath(), app.pdfStylePath()+".bak"); err != nil {
			t.Fatal(err)
		}
		writeRawPDFStyleFile(t, app, "{")
		if err := os.Rename(app.pdfStylePath(), app.pdfStylePath()+".tmp"); err != nil {
			t.Fatal(err)
		}
		infos := listPDFStylesForTest(t, app)
		if len(infos) != 6 || docgen.ActiveStyle().ID != "atlas" {
			t.Fatalf("list %+v active %s", infos, docgen.ActiveStyle().ID)
		}
		if _, statErr := os.Stat(app.pdfStylePath() + ".tmp"); statErr != nil {
			t.Fatalf("broken tmp = %v", statErr)
		}
	})

	t.Run("live corrupt", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		if err := writePDFStyleFile(app.pdfStylePath(), pdfStyleFile{Active: "atlas", Styles: []docgen.CustomStyle{harbor}}); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(app.pdfStylePath(), app.pdfStylePath()+".bak"); err != nil {
			t.Fatal(err)
		}
		writeRawPDFStyleFile(t, app, "{")
		listed, err := app.ListPDFStyles()
		if err != nil {
			t.Fatal(err)
		}
		if listed.Warning != pdfStyleCorruptWarning || docgen.ActiveStyle().ID != "gazette" {
			t.Fatalf("warning %q active %s", listed.Warning, docgen.ActiveStyle().ID)
		}
		if string(readRawPDFStyleFile(t, app.pdfStylePath())) != "{" {
			t.Fatal("corrupt file was replaced")
		}
		bak, err := os.ReadFile(app.pdfStylePath() + ".bak")
		if err != nil {
			t.Fatal(err)
		}
		var saved pdfStyleFile
		if err := json.Unmarshal(bak, &saved); err != nil || saved.Active != "atlas" {
			t.Fatalf("bak = %s err %v", bak, err)
		}
	})
}

func writeRawPDFStyleFile(t *testing.T, app *App, body string) {
	t.Helper()
	path := app.pdfStylePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRawPDFStyleFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
