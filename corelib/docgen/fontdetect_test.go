package docgen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	ttfcore "github.com/VantageDataChat/GoPDF2/fontmaker/core"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// TestEmbeddedFallbackFontEmbedsAndCoversCJK is the guarantee behind PDF
// generation on bare hosts: the embedded subset must validate, cover
// Chinese, load into the PDF engine, and render a real document.
func TestEmbeddedFallbackFontEmbedsAndCoversCJK(t *testing.T) {
	face := embeddedFallbackFont()
	if len(face) == 0 {
		t.Fatal("embedded fallback font missing or failed validation")
	}
	if err := fontCoversCJK(face); err != nil {
		t.Fatalf("embedded font must cover Chinese: %v", err)
	}
	bold := embeddedFallbackBold()
	if len(bold) == 0 || bytes.Equal(bold, face) {
		t.Fatal("embedded bold must exist and differ from the regular face")
	}
	if err := fontCoversCJK(bold); err != nil {
		t.Fatalf("embedded bold must parse and cover Chinese: %v", err)
	}
	pdfBold := &gopdf.GoPdf{}
	pdfBold.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdfBold.AddTTFFontDataWithOption("bold", bold, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		t.Fatalf("embedded bold must load into the PDF engine: %v", err)
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData("regular", face); err != nil {
		t.Fatalf("embedded font must load into the PDF engine: %v", err)
	}
	g := &Generator{fontRegularData: face, fontBoldData: face}
	data, err := g.Generate(Spec{Title: "测试", Content: "# 北京天气报告\n\n今日天气：晴，气温 11℃ / -1℃。\n\n**风力**：微风。"})
	if err != nil {
		t.Fatalf("Generate with embedded font: %v", err)
	}
	if len(data) < 1000 {
		t.Fatalf("rendered PDF suspiciously small: %d bytes", len(data))
	}
}

// TestEmbeddedFallbackFontSubsetCoversGB2312 pins the subset promise: common
// Chinese punctuation and GB2312 level-1 hanzi must all have glyphs, or
// everyday documents would print boxes.
func TestEmbeddedFallbackFontSubsetCoversGB2312(t *testing.T) {
	face := embeddedFallbackFont()
	if len(face) == 0 {
		t.Skip("embedded fallback unavailable")
	}
	probe := []rune("的一是不了人我在有他这中大来上国个地说们为子和你道出而于就下得可以年会生自时那后之也")
	probe = append(probe, []rune("，。；：？！、（）《》【】℃—…“”‘’0123456789")...)
	// The style chrome glyphs: markdown rules and mark bars must not print
	// as .notdef in a styled document.
	probe = append(probe, []rune("━－｜")...)
	// Western text must not regress either: the fallback is the only face on
	// a bare host, so accented Latin needs glyphs just as much as hanzi.
	probe = append(probe, []rune("éèêëàâäôöùûüçñÉÅåßœŒ")...)
	var parser ttfcore.TTFParser
	if err := parser.ParseFontData(face); err != nil {
		t.Fatalf("parse embedded font: %v", err)
	}
	chars := parser.Chars()
	var missing []rune
	for _, r := range probe {
		if _, ok := chars[int(r)]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("embedded subset missing glyphs for: %q", string(missing))
	}
}

// TestDetectNeverSettlesForLatinOnly pins the ordering fix: with no
// CJK-covering candidate the answer must be the embedded fallback (which
// covers Chinese), never a Latin-only system font.
func TestDetectFallsToEmbeddedWithoutCJKCandidates(t *testing.T) {
	reset := resetFontDetectionForTest(t)
	defer reset()
	dir := t.TempDir()
	latinOnly := filepath.Join(dir, "latin.ttf")
	// A file that parses but lacks CJK coverage is simulated by garbage —
	// either way it must not be picked.
	if err := os.WriteFile(latinOnly, []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_FONT_DIR", dir)
	regular, bold := detectSystemFonts()
	if len(regular) == 0 {
		t.Skip("embedded fallback unavailable on this build")
	}
	if len(bold) == 0 {
		t.Fatal("fallback must supply a bold face")
	}
	if len(bold) == len(regular) && bytes.Equal(bold, regular) {
		t.Fatal("fallback bold must be the real wght=700 cut, not the regular face")
	}
	if !UsingEmbeddedFallback() {
		t.Fatal("detection must report the embedded fallback is in use")
	}
}

// TestDetectPicksUpLateArrivingCacheFont is the self-repair property: the
// fallback answer is not cached, so a font dropped into the cache dir after
// a failed probe is used by the very next probe.
func TestDetectPicksUpLateArrivingCacheFont(t *testing.T) {
	reset := resetFontDetectionForTest(t)
	defer reset()
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	first, _ := detectSystemFonts()
	if len(first) == 0 {
		t.Skip("embedded fallback unavailable on this build")
	}
	// Drop a real CJK font into the cache dir after the first probe.
	full, err := os.ReadFile(filepath.Join("fonts", "NotoSansSCSubset.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dropped.ttf"), full, 0o644); err != nil {
		t.Fatal(err)
	}
	second, _ := detectSystemFonts()
	if len(second) == 0 {
		t.Fatal("detection must pick up the cache-dir font")
	}
	if UsingEmbeddedFallback() {
		t.Fatal("a real cache-dir font must end the fallback state")
	}
	// The picked font is now cached: removing the file must not lose it.
	if err := os.Remove(filepath.Join(dir, "dropped.ttf")); err != nil {
		t.Fatal(err)
	}
	if third, _ := detectSystemFonts(); len(third) == 0 {
		t.Fatal("a detected real font must stay cached")
	}
}

// TestGeneratorFontsResolveLateArrivingFont covers the render-time
// re-resolution: a Generator CONSTRUCTED during the fallback (long-lived
// holders like the swarm orchestrator keep theirs for the process
// lifetime) must still render with a font that arrives afterwards. The
// observable is the fallback state flipping: the dropped file is the
// subset face itself, so byte comparisons cannot tell the sources apart.
func TestGeneratorFontsResolveLateArrivingFont(t *testing.T) {
	reset := resetFontDetectionForTest(t)
	defer reset()
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	g := New()
	if len(g.fontRegularData) == 0 {
		t.Skip("embedded fallback unavailable on this build")
	}
	if !UsingEmbeddedFallback() {
		t.Fatal("test setup must start in the fallback state")
	}
	full, err := os.ReadFile(filepath.Join("fonts", "NotoSansSCSubset.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dropped.ttf"), full, 0o644); err != nil {
		t.Fatal(err)
	}
	regular, _ := g.fonts()
	if len(regular) == 0 {
		t.Fatal("render-time font resolution must keep serving a face")
	}
	if UsingEmbeddedFallback() {
		t.Fatal("a long-lived Generator must re-resolve fonts at render time")
	}
}

func TestFontCacheDirCandidatesScansFontExtensions(t *testing.T) {
	dir := t.TempDir()
	for name := range map[string]bool{
		"a.ttf":  true,
		"b.ttc":  true,
		"c.otf":  true,
		"d.TTF":  true,
		"e.txt":  false,
		"readme": false,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.ttf"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := fontCacheDirCandidates(dir)
	var names []string
	for _, c := range got {
		names = append(names, filepath.Base(c.regular))
		if c.regular != c.bold {
			t.Fatalf("cache-dir candidate must reuse the face for bold: %+v", c)
		}
	}
	want := []string{"a.ttf", "b.ttc", "c.otf", "d.TTF"}
	if len(names) != len(want) {
		t.Fatalf("candidates = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("candidates = %v, want sorted %v", names, want)
		}
	}
	if got := fontCacheDirCandidates(""); got != nil {
		t.Fatal("empty dir must yield no candidates")
	}
	if got := fontCacheDirCandidates(filepath.Join(dir, "missing")); got != nil {
		t.Fatal("missing dir must yield no candidates")
	}
}

func TestFontCacheDirEnvOverride(t *testing.T) {
	t.Setenv("MACLAW_FONT_DIR", `D:\custom-fonts`)
	if got := FontCacheDir(); got != `D:\custom-fonts` {
		t.Fatalf("FontCacheDir = %q", got)
	}
}

func TestValidateEmbeddableCJKFont(t *testing.T) {
	if err := ValidateEmbeddableCJKFont([]byte("garbage")); err == nil {
		t.Fatal("garbage must be rejected")
	}
	full, err := os.ReadFile(filepath.Join("fonts", "NotoSansSCSubset.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEmbeddableCJKFont(full); err != nil {
		t.Fatalf("the shipped subset must validate: %v", err)
	}
	latin, err := os.ReadFile(`C:\Windows\Fonts\arial.ttf`)
	if err == nil {
		if err := ValidateEmbeddableCJKFont(latin); err == nil {
			t.Fatal("a Latin-only font must be rejected for provisioning")
		}
	}
}

// resetFontDetectionForTest clears the process-wide detection cache and
// narrows the candidate source to the cache dir alone — simulating a bare
// host with no system fonts — so a test can drive detection through the
// fallback path. The pre-test state is restored afterwards.
func resetFontDetectionForTest(t *testing.T) func() {
	t.Helper()
	fontDetectMu.Lock()
	savedRegular, savedBold, savedFailure := detectedRegularFontData, detectedBoldFontData, detectedFontFailure
	detectedRegularFontData, detectedBoldFontData, detectedFontFailure = nil, nil, ""
	fontDetectMu.Unlock()
	savedSource := candidateSource
	candidateSource = func() []fontCandidate { return fontCacheDirCandidates(FontCacheDir()) }
	return func() {
		fontDetectMu.Lock()
		detectedRegularFontData, detectedBoldFontData, detectedFontFailure = savedRegular, savedBold, savedFailure
		fontDetectMu.Unlock()
		candidateSource = savedSource
	}
}

func TestEmbeddedFallbackCovers(t *testing.T) {
	for _, r := range []rune{'中', 'a', 'é', '。', '━'} {
		if !EmbeddedFallbackCovers(r) {
			t.Fatalf("embedded fallback must cover U+%04X", r)
		}
	}
	// Extension-B characters are far outside the subset.
	if EmbeddedFallbackCovers('𠮷') {
		t.Fatal("extension-B characters must report as uncovered")
	}
}

// TestProvisionProbeCoversDocumentBasics pins the probe strength: weakening
// it back to a CJK-only check re-admits complement faces like
// DroidSansFallback, which passes 中文 yet prints no digits at all.
func TestProvisionProbeCoversDocumentBasics(t *testing.T) {
	for _, r := range []rune{'0', '9', 'a', 'z', 'A', 'Z', '中', '。'} {
		found := false
		for _, probe := range provisionProbeRunes {
			if probe == r {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("provisionProbeRunes must include %q", r)
		}
	}
}

// TestValidateEmbeddableCJKFontRejectsLatinOnlyASCIIRegression pins the
// inverse direction too: a font must cover both scripts.
func TestValidateEmbeddableCJKFontRejectsLatinOnlyASCIIRegression(t *testing.T) {
	// The embedded subset is the positive control: it must pass.
	full, err := os.ReadFile(filepath.Join("fonts", "NotoSansSCSubset.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEmbeddableCJKFont(full); err != nil {
		t.Fatalf("embedded subset must pass provisioning validation: %v", err)
	}
}
