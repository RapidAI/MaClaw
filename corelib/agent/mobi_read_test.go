package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractMOBITextPalmDoc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.mobi")
	html := "<html><body><p>叶轮间隙为 0.2 mm。</p><script>secret()</script><p>第二段</p></body></html>"
	if err := os.WriteFile(path, buildTestMOBI("泵手册", html, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "mobi" {
		t.Fatalf("mobi format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"# 泵手册", "叶轮间隙为 0.2 mm。", "第二段"} {
		if !strings.Contains(text, want) {
			t.Fatalf("mobi text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "secret") || strings.Contains(text, "<p>") {
		t.Fatalf("markup leaked:\n%s", text)
	}
}

func TestExtractMOBITextRejectsEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.mobi")
	raw := buildTestMOBI("locked", "<p>hidden</p>", 1)
	// PalmDOC encryption field is record 0 bytes 12-13. Record 0 starts at the
	// first record offset stored in the PDB record list.
	off := int(binary.BigEndian.Uint32(raw[78:82]))
	binary.BigEndian.PutUint16(raw[off+12:off+14], 2)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ExtractOfficeText(path)
	if !errors.Is(err, errOfficeReadEncryptedContainer) {
		t.Fatalf("encrypted mobi err=%v", err)
	}
}

func TestDocumentAttachmentFormatMOBIKeepsItsOwnSuffix(t *testing.T) {
	format, mime, ok := DocumentAttachmentFormat("Love and Sex.mobi", "")
	if !ok || format != DocumentAttachmentFormatMOBI || mime != "application/x-mobipocket-ebook" {
		t.Fatalf("name format=%q mime=%q ok=%v", format, mime, ok)
	}
	format, mime, ok = DocumentAttachmentFormat("file", "application/x-mobipocket-ebook; charset=binary")
	if !ok || format != DocumentAttachmentFormatMOBI || mime != "application/x-mobipocket-ebook" {
		t.Fatalf("mime format=%q mime=%q ok=%v", format, mime, ok)
	}
	if got := DocumentAttachmentTempSuffix(`E:\books\Love and Sex.mobi`, format); got != ".mobi" {
		t.Fatalf("named suffix=%q", got)
	}
	if got := DocumentAttachmentTempSuffix("", DocumentAttachmentFormatMOBI); got != ".mobi" {
		t.Fatalf("format suffix=%q", got)
	}
	if _, _, ok := DocumentAttachmentFormat("book.epub", ""); ok {
		t.Fatal("epub stays outside the trusted attachment vocabulary")
	}
}

func TestExtractMOBITextRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.mobi")
	if err := os.WriteFile(path, []byte("this is not a book"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ExtractOfficeText(path)
	if err == nil || !strings.Contains(err.Error(), "MOBI") || !errors.Is(err, errOfficeReadUnsafeContainer) {
		t.Fatalf("garbage mobi err=%v", err)
	}
	out := formatOfficeReadFailure(path, "mobi", err)
	if strings.Contains(out, "craft_tool") || strings.Contains(out, "manage_skill") {
		t.Fatalf("malformed mobi invited another parser:\n%s", out)
	}
	if !strings.Contains(out, officeReadErrorClassMarker+"malformed") {
		t.Fatalf("malformed class missing:\n%s", out)
	}
}

func TestMOBIHTMLTextKeepsTailAfterUnclosedScript(t *testing.T) {
	got := mobiHTMLText("<p>前文</p><script leftover")
	if !strings.Contains(got, "前文") || !strings.Contains(got, "leftover") {
		t.Fatalf("unclosed script dropped the tail: %q", got)
	}
}

func TestMOBIHTMLTextKeepsLongerTagNames(t *testing.T) {
	got := mobiHTMLText("<p>前文</p><scripture>保留</scripture><style-guide>也保留</style-guide><script type=\"text/javascript\">secret()</script><p>后文</p>")
	if strings.Contains(got, "secret") {
		t.Fatalf("script leaked: %q", got)
	}
	for _, want := range []string{"前文", "保留", "也保留", "后文"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestMOBIHTMLTextDropsScriptWhenCaseFoldChangesWidth(t *testing.T) {
	// U+0130 lowercases to two code points. Indexing a ToLower copy would
	// slice the original string at the wrong byte and could keep the script.
	got := mobiHTMLText("\u0130<p>叶轮</p><SCRIPT>secret()</SCRIPT><style>hidden</style><p>第二段</p>")
	if strings.Contains(got, "secret") || strings.Contains(got, "hidden") {
		t.Fatalf("element leaked: %q", got)
	}
	for _, want := range []string{"\u0130", "叶轮", "第二段"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestExtractMOBISampleWhenPresent(t *testing.T) {
	path := os.Getenv("MACLAW_MOBI_SAMPLE")
	if path == "" {
		t.Skip("MACLAW_MOBI_SAMPLE not set")
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "mobi" {
		t.Fatalf("sample format=%q err=%v textLen=%d", format, err, len(text))
	}
	// The title is prepended and can itself contain the probe words. This file's
	// stripped body is about 690k runes; a trailer or tag bug that drops the
	// tail lands well under that.
	if len([]rune(text)) < 600000 {
		t.Fatalf("sample text too short: runes=%d", len([]rune(text)))
	}
	if !strings.Contains(text, "Robot") && !strings.Contains(text, "robot") && !strings.Contains(text, "Love") {
		t.Fatalf("sample text did not look like the book, runes=%d", len([]rune(text)))
	}
}

func TestMOBITrailingSizeBackwardEncoded(t *testing.T) {
	if got := mobiBackEncodedSize([]byte{0x84, 0x22, 0x11}); got != 0x11111 {
		t.Fatalf("calibre 0x11111 decoded as %#x", got)
	}
	if got := mobiBackEncodedSize([]byte{0x85}); got != 5 {
		t.Fatalf("one-byte size = %d", got)
	}
	if got := mobiBackEncodedSize([]byte{0x01, 0x02, 0x03, 0x04}); got != 0 {
		t.Fatalf("unterminated size = %d", got)
	}

	// 200 is stored as 0x81 0x48. Shifting the other way reads 0x2401 and
	// refuses to strip, leaving the entry bytes in the record.
	entry := append(bytes.Repeat([]byte{'A'}, 198), 0x81, 0x48)
	rec := append([]byte("hello"), entry...)
	if got := mobiTrailingSize(rec, 0x0002); got != 200 {
		t.Fatalf("trailing = %d", got)
	}
	plain, err := mobiUnpackRecord(rec, mobiCompressionNone, 0x0002, nil)
	if err != nil || string(plain) != "hello" {
		t.Fatalf("unpack %q err=%v", plain, err)
	}
	// A record that is only the size byte must not come back as text.
	plain, err = mobiUnpackRecord([]byte{0x81}, mobiCompressionNone, 0x0002, nil)
	if err != nil || len(plain) != 0 {
		t.Fatalf("full-record trailer leaked %q err=%v", plain, err)
	}

	// Flag bit 2 is stored after flag bit 1, so it is consumed from the end first.
	layered := append(append([]byte("helloabcd"), 0x85), 'y', 'z', 0x83)
	if got := mobiTrailingSize(layered, (1<<2)|(1<<1)); got != 8 {
		t.Fatalf("layered trailing = %d", got)
	}
	plain, err = mobiUnpackRecord(layered, mobiCompressionNone, (1<<2)|(1<<1), nil)
	if err != nil || string(plain) != "hello" {
		t.Fatalf("layered unpack %q err=%v", plain, err)
	}

	// Bit 0 is the overlap count on the byte that remains after the entries.
	overlap := append([]byte("hello"), 0x00, 0x11, 0x82)
	if got := mobiTrailingSize(overlap, 0x0003); got != 3 {
		t.Fatalf("overlap trailing = %d", got)
	}
}

func TestMOBIUnsetCodepageIsNotUTF8(t *testing.T) {
	// 0xE9 is é in Windows-1252 and an illegal UTF-8 start byte.
	raw := []byte("caf\xe9")
	got := mobiDecode(mobiCodec(0), raw)
	if got != "café" {
		t.Fatalf("codepage 0 = %q", got)
	}
	got = mobiDecode(mobiCodec(1252), []byte("caf\xc3\xa9"))
	if got != "caf\u00c3\u00a9" {
		t.Fatalf("explicit 1252 changed a declared codepage: %q", got)
	}
	got = mobiDecode(mobiCodec(0), []byte("caf\xc3\xa9"))
	if got != "café" {
		t.Fatalf("valid UTF-8 with codepage 0 = %q", got)
	}

	text, err := extractMOBIBytes(buildAncientMOBI(raw))
	if err != nil || !strings.Contains(text, "café") {
		t.Fatalf("ancient text=%q err=%v", text, err)
	}
}

func TestPalmDOCUnpackCopiesOverlap(t *testing.T) {
	// 0x8008 is distance 1, length 3. The copy must read bytes it just wrote.
	out, err := palmDOCUnpack(append([]byte("Z"), 0x80, 0x08))
	if err != nil || string(out) != "ZZZZ" {
		t.Fatalf("overlap unpack %q err=%v", out, err)
	}
}

func TestExtractMOBITextStripsMultibyteTrailingEntry(t *testing.T) {
	html := "<p>叶轮间隙为 0.2 mm。</p>"
	trailer := append(bytes.Repeat([]byte{'A'}, 198), 0x81, 0x48)
	path := filepath.Join(t.TempDir(), "trail.mobi")
	if err := os.WriteFile(path, buildTestMOBIWithFlags("泵手册", html, trailer, 0x0002), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "mobi" {
		t.Fatalf("format=%q err=%v textLen=%d", format, err, len(text))
	}
	if !strings.Contains(text, "叶轮间隙为 0.2 mm。") || strings.Contains(text, "AAA") {
		t.Fatalf("multibyte trailer leaked or body missing, len=%d", len(text))
	}
}

func truncateMOBITest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// buildAncientMOBI is a TEXTREAD book with no MOBI header. Its text uses the
// PalmDOC default codepage rather than UTF-8.
func buildAncientMOBI(body []byte) []byte {
	rec0 := make([]byte, 16)
	binary.BigEndian.PutUint16(rec0[0:2], mobiCompressionNone)
	binary.BigEndian.PutUint32(rec0[4:8], uint32(len(body)))
	binary.BigEndian.PutUint16(rec0[8:10], 1)
	binary.BigEndian.PutUint16(rec0[10:12], 4096)
	pdbHead := 78 + 2*8
	out := make([]byte, pdbHead+len(rec0)+len(body))
	copy(out[0:32], []byte("Ancient"))
	copy(out[60:68], "TEXTREAD")
	binary.BigEndian.PutUint16(out[76:78], 2)
	binary.BigEndian.PutUint32(out[78:82], uint32(pdbHead))
	binary.BigEndian.PutUint32(out[86:90], uint32(pdbHead+len(rec0)))
	copy(out[pdbHead:], rec0)
	copy(out[pdbHead+len(rec0):], body)
	return out
}

// buildTestMOBI writes an uncompressed or identity PalmDOC book with one text record.
func buildTestMOBI(title, html string, compression uint16) []byte {
	titleBytes := []byte(title)
	body := []byte(html)
	// Record 0: 16 byte PalmDOC + 0x80 MOBI header + title.
	headerLen := 0x80
	rec0 := make([]byte, 16+headerLen+len(titleBytes))
	binary.BigEndian.PutUint16(rec0[0:2], compression)
	binary.BigEndian.PutUint32(rec0[4:8], uint32(len(body)))
	binary.BigEndian.PutUint16(rec0[8:10], 1)
	binary.BigEndian.PutUint16(rec0[10:12], 4096)
	copy(rec0[16:20], "MOBI")
	binary.BigEndian.PutUint32(rec0[20:24], uint32(headerLen))
	binary.BigEndian.PutUint32(rec0[24:28], 2)
	binary.BigEndian.PutUint32(rec0[28:32], 65001)
	titleAt := 16 + headerLen
	binary.BigEndian.PutUint32(rec0[0x54:0x58], uint32(titleAt))
	binary.BigEndian.PutUint32(rec0[0x58:0x5C], uint32(len(titleBytes)))
	copy(rec0[titleAt:], titleBytes)

	pdbHead := 78 + 2*8
	rec0At := pdbHead
	rec1At := rec0At + len(rec0)
	out := make([]byte, rec1At+len(body))
	copy(out[0:32], []byte("Manual"))
	copy(out[60:68], "BOOKMOBI")
	binary.BigEndian.PutUint16(out[76:78], 2)
	binary.BigEndian.PutUint32(out[78:82], uint32(rec0At))
	binary.BigEndian.PutUint32(out[86:90], uint32(rec1At))
	copy(out[rec0At:], rec0)
	copy(out[rec1At:], body)
	return out
}

// buildTestMOBIWithFlags writes an uncompressed book whose MOBI header is long
// enough to carry extra flags, then appends a trailing entry to the text record.
func buildTestMOBIWithFlags(title, html string, trailer []byte, extraFlags uint16) []byte {
	titleBytes := []byte(title)
	body := append([]byte(html), trailer...)
	headerLen := 0xE8
	rec0 := make([]byte, 16+headerLen+len(titleBytes))
	binary.BigEndian.PutUint16(rec0[0:2], mobiCompressionNone)
	binary.BigEndian.PutUint32(rec0[4:8], uint32(len(html)))
	binary.BigEndian.PutUint16(rec0[8:10], 1)
	binary.BigEndian.PutUint16(rec0[10:12], 4096)
	copy(rec0[16:20], "MOBI")
	binary.BigEndian.PutUint32(rec0[20:24], uint32(headerLen))
	binary.BigEndian.PutUint32(rec0[24:28], 2)
	binary.BigEndian.PutUint32(rec0[28:32], 65001)
	titleAt := 16 + headerLen
	binary.BigEndian.PutUint32(rec0[0x54:0x58], uint32(titleAt))
	binary.BigEndian.PutUint32(rec0[0x58:0x5C], uint32(len(titleBytes)))
	binary.BigEndian.PutUint16(rec0[0xF2:0xF4], extraFlags)
	copy(rec0[titleAt:], titleBytes)

	pdbHead := 78 + 2*8
	rec0At := pdbHead
	rec1At := rec0At + len(rec0)
	out := make([]byte, rec1At+len(body))
	copy(out[0:32], []byte("Manual"))
	copy(out[60:68], "BOOKMOBI")
	binary.BigEndian.PutUint16(out[76:78], 2)
	binary.BigEndian.PutUint32(out[78:82], uint32(rec0At))
	binary.BigEndian.PutUint32(out[86:90], uint32(rec1At))
	copy(out[rec0At:], rec0)
	copy(out[rec1At:], body)
	return out
}
