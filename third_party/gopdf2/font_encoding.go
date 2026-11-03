package gopdf

import (
	xencoding "golang.org/x/text/encoding"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// ============================================================
// Font encoding support for text extraction.
//
// Real-world PDFs frequently lack a /ToUnicode CMap. To recover
// text we combine, in priority order:
//  1. /ToUnicode CMap (code -> unicode string, ligature-aware)
//  2. /Encoding /Differences glyph names -> Unicode (AGL)
//  3. /Encoding base encoding tables (WinAnsi, MacRoman, …)
//  4. Type0 predefined CMaps (UniGB-UCS2-H, GBK-EUC-H, …)
//  5. Legacy byte encodings via x/text (GBK, Big5, EUC-KR, …)
// ============================================================

// codeToUnicode is the decoded ToUnicode mapping: character code -> text.
type codeToUnicode map[uint16]string

// predefined CMap families used by Type0 fonts.
const (
	cmapFamilyUCS2 = iota // UniGB-UCS2-H, UniJIS-UCS2-H, UniKS-UCS2-H …
	cmapFamilyGBK
	cmapFamilyBig5
	cmapFamilyEUCKR
	cmapFamilySJIS
	cmapFamilyEUCJP
	cmapFamilyUnknown
)

// predefinedCMapFamily classifies a Type0 /Encoding predefined CMap name.
func predefinedCMapFamily(name string) int {
	switch {
	case name == "":
		return cmapFamilyUnknown
	case strings.HasPrefix(name, "UniGB-UCS2"):
		return cmapFamilyUCS2
	case strings.HasPrefix(name, "UniJIS-UCS2"):
		return cmapFamilyUCS2
	case strings.HasPrefix(name, "UniKS-UCS2"):
		return cmapFamilyUCS2
	case strings.HasPrefix(name, "GBK-EUC"), strings.HasPrefix(name, "GBEvolution"):
		return cmapFamilyGBK
	case strings.HasPrefix(name, "B5pc"), strings.HasPrefix(name, "UniCNS-UCS2"), strings.HasPrefix(name, "BigFive"):
		return cmapFamilyBig5
	case strings.HasPrefix(name, "KSC"):
		return cmapFamilyEUCKR
	case strings.HasPrefix(name, "90ms-RKSJ"), strings.HasPrefix(name, "90pv-RKSJ"), strings.HasPrefix(name, "RKSJ"):
		return cmapFamilySJIS
	case strings.HasPrefix(name, "EUC-H"), strings.HasPrefix(name, "EUC-V"), strings.HasPrefix(name, "78-EUC"):
		return cmapFamilyEUCJP
	case strings.HasPrefix(name, "78ms-RKSJ"):
		return cmapFamilySJIS
	case strings.HasPrefix(name, "UniGB-"), strings.HasPrefix(name, "UniJIS-"), strings.HasPrefix(name, "UniKS-"), strings.HasPrefix(name, "UniCNS-"):
		// other Uni* CMaps are 4-byte CID variants; approximate 2-byte decoding
		return cmapFamilyUCS2
	default:
		return cmapFamilyUnknown
	}
}

// cjkEncodings caches the immutable x/text encodings per family. The Encoding
// values are safe for concurrent use; a fresh Decoder (cheap, stateful) is
// created per call, so extraction is safe from multiple goroutines.
var cjkEncodings = map[int]xencoding.Encoding{
	cmapFamilyGBK:   simplifiedchinese.GBK,
	cmapFamilyBig5:  traditionalchinese.Big5,
	cmapFamilyEUCKR: korean.EUCKR,
	cmapFamilySJIS:  japanese.ShiftJIS,
	cmapFamilyEUCJP: japanese.EUCJP,
}

// decodeCJKBytes decodes raw bytes with the legacy encoding matching the family.
func decodeCJKBytes(family int, data []byte) string {
	enc, ok := cjkEncodings[family]
	if !ok {
		return string(data)
	}
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return string(data)
	}
	return string(out)
}

// utf16BeRunes decodes big-endian UTF-16 data into runes, joining surrogate pairs.
func utf16BeRunes(data []byte) []rune {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	codes := make([]uint16, len(data)/2)
	for i := range codes {
		codes[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	return utf16.Decode(codes)
}

// winAnsiUnicode maps CP1252 high bytes (0x80-0x9F not in Latin-1) to Unicode.
// Bytes 0xA0-0xFF equal Latin-1; 0x00-0x7F are ASCII.
var winAnsiUnicode = [32]rune{
	0x20AC, // 0x80 EURO SIGN
	0xFFFD, // 0x81 undefined
	0x201A, // 0x82 SINGLE LOW-9 QUOTATION MARK
	0x0192, // 0x83 LATIN SMALL LETTER F WITH HOOK
	0x201E, // 0x84 DOUBLE LOW-9 QUOTATION MARK
	0x2026, // 0x85 HORIZONTAL ELLIPSIS
	0x2020, // 0x86 DAGGER
	0x2021, // 0x87 DOUBLE DAGGER
	0x02C6, // 0x88 MODIFIER LETTER CIRCUMFLEX ACCENT
	0x2030, // 0x89 PER MILLE SIGN
	0x0160, // 0x8A LATIN CAPITAL LETTER S WITH CARON
	0x2039, // 0x8B SINGLE LEFT-POINTING ANGLE QUOTATION MARK
	0x0152, // 0x8C LATIN CAPITAL LIGATURE OE
	0xFFFD, // 0x8D undefined
	0x017D, // 0x8E LATIN CAPITAL LETTER Z WITH CARON
	0xFFFD, // 0x8F undefined
	0xFFFD, // 0x90 undefined
	0x2018, // 0x91 LEFT SINGLE QUOTATION MARK
	0x2019, // 0x92 RIGHT SINGLE QUOTATION MARK
	0x201C, // 0x93 LEFT DOUBLE QUOTATION MARK
	0x201D, // 0x94 RIGHT DOUBLE QUOTATION MARK
	0x2022, // 0x95 BULLET
	0x2013, // 0x96 EN DASH
	0x2014, // 0x97 EM DASH
	0x02DC, // 0x98 SMALL TILDE
	0x2122, // 0x99 TRADE MARK SIGN
	0x0161, // 0x9A LATIN SMALL LETTER S WITH CARON
	0x203A, // 0x9B SINGLE RIGHT-POINTING ANGLE QUOTATION MARK
	0x0153, // 0x9C LATIN SMALL LIGATURE OE
	0xFFFD, // 0x9D undefined
	0x017E, // 0x9E LATIN SMALL LETTER Z WITH CARON
	0x0178, // 0x9F LATIN CAPITAL LETTER Y WITH DIAERESIS
}

// decodeWinAnsiByte decodes a single WinAnsiEncoding (CP1252) byte.
func decodeWinAnsiByte(b byte) (rune, bool) {
	if b >= 0x80 && b <= 0x9F {
		r := winAnsiUnicode[b-0x80]
		return r, r != 0xFFFD
	}
	return rune(b), true
}

// pdfDocUnicode maps PDFDocEncoding high bytes that differ from Latin-1.
var pdfDocUnicode = map[byte]rune{
	0x80: 0x02D8, 0x81: 0x02C7, 0x82: 0x02C6, 0x83: 0x02D9,
	0x84: 0x02DD, 0x85: 0x02DB, 0x86: 0x02DA, 0x87: 0x02DC,
	0x88: 0x201A, 0x89: 0x0192, 0x8A: 0x201E, 0x8B: 0x2026,
	0x8C: 0x2020, 0x8D: 0x2021, 0x8E: 0x00C2, 0x8F: 0x00CA,
	0x90: 0x00C1, 0x91: 0x00CB, 0x92: 0x00C8, 0x93: 0x00CD,
	0x94: 0x00CE, 0x95: 0x00CF, 0x96: 0x00CC, 0x97: 0x00D3,
	0x98: 0x00D4, 0x99: 0xF8FF, 0x9A: 0x00D2, 0x9B: 0x00DA,
	0x9C: 0x00DB, 0x9D: 0x00D9, 0x9E: 0x0131, 0x9F: 0x02C6,
	0xA0: 0x02DC, 0xA1: 0x00AF, 0xA2: 0x02D8, 0xA3: 0x02D9,
	0xA4: 0x02DD, 0xA5: 0x00B4, 0xA6: 0x02C7, 0xA7: 0x02DC,
	0xA8: 0xF8FF, 0xA9: 0xF8FF, 0xAA: 0x02C6, 0xAB: 0xF8FF,
	0xAC: 0xF8FF, 0xAD: 0xF8FF, 0xAE: 0xF8FF, 0xAF: 0xF8FF,
	0xB6: 0x201A, 0xB7: 0x201E, 0xB8: 0x2030,
	0xBB: 0x2039, 0xBC: 0x0152, 0xBD: 0x0153, 0xBE: 0x0160, 0xBF: 0x0153,
	0xC1: 0x00C1, 0xC2: 0x00C2, 0xC3: 0x00C0, 0xC4: 0x00C5,
	0xC5: 0x00C3, 0xC6: 0x00C5, 0xC7: 0x00C7, 0xC8: 0x00C9,
	0xC9: 0x00CA, 0xCA: 0x00C8, 0xCB: 0x00CB, 0xCC: 0x00CD,
	0xCD: 0x00CE, 0xCE: 0x00CF, 0xCF: 0x00CC, 0xD0: 0x00D1,
	0xD1: 0x00D2, 0xD2: 0x00D3, 0xD3: 0x00D4, 0xD4: 0x00D6,
	0xD5: 0x00D5, 0xD6: 0x00D8, 0xD7: 0x00D6, 0xD8: 0x00D9,
	0xD9: 0x00DA, 0xDA: 0x00DB, 0xDB: 0x00D9, 0xDC: 0x00DF,
	0xDD: 0x00E0, 0xDE: 0x00E1, 0xDF: 0x00E2, 0xE0: 0x00E3,
	0xE1: 0x00E4, 0xE2: 0x00E5, 0xE3: 0x00E7, 0xE4: 0x00E9,
	0xE5: 0x00EA, 0xE6: 0x00E8, 0xE7: 0x00EB, 0xE8: 0x00ED,
	0xE9: 0x00EE, 0xEA: 0x00EF, 0xEB: 0x00EC, 0xEC: 0x00F1,
	0xED: 0x00F2, 0xEE: 0x00F3, 0xEF: 0x00F4, 0xF0: 0x00F6,
	0xF1: 0x00F5, 0xF2: 0x00F8, 0xF3: 0x00F6, 0xF4: 0x00FA,
	0xF5: 0x00F9, 0xF6: 0x00FB, 0xF7: 0x00FC, 0xF8: 0x2020,
	0xF9: 0x00B0, 0xFA: 0x00A2, 0xFB: 0x00A3, 0xFC: 0x00A7,
	0xFD: 0x2022, 0xFE: 0x00B6, 0xFF: 0x00DF,
}

// decodePDFDocByte decodes a single PDFDocEncoding byte.
func decodePDFDocByte(b byte) (rune, bool) {
	if b < 0x80 {
		return rune(b), true
	}
	if r, ok := pdfDocUnicode[b]; ok {
		return r, true
	}
	return rune(b), true // approximate rest as Latin-1
}

// macRomanUnicode maps MacRomanEncoding high bytes (0x80-0xFF) to Unicode.
var macRomanUnicode = [128]rune{
	0x00C4, 0x00C5, 0x00C7, 0x00C9, 0x00D1, 0x00D6, 0x00DC, 0x00E1,
	0x00E0, 0x00E2, 0x00E4, 0x00E3, 0x00E5, 0x00E7, 0x00E9, 0x00E8,
	0x00EA, 0x00EB, 0x00ED, 0x00EC, 0x00EE, 0x00EF, 0x00F1, 0x00F3,
	0x00F2, 0x00F4, 0x00F6, 0x00F5, 0x00FA, 0x00F9, 0x00FB, 0x00FC,
	0x2020, 0x00B0, 0x00A2, 0x00A3, 0x00A7, 0x2022, 0x00B6, 0x00DF,
	0x00AE, 0x00A9, 0x2122, 0x00B4, 0x00A8, 0x2260, 0x00C6, 0x00D8,
	0x221E, 0x00B1, 0x2264, 0x2265, 0x00A5, 0x00B5, 0x2202, 0x2211,
	0x220F, 0x03C0, 0x222B, 0x00AA, 0x00BA, 0x03A9, 0x00E6, 0x00F8,
	0x00BF, 0x00A1, 0x00AC, 0x221A, 0x0192, 0x2248, 0x2206, 0x00AB,
	0x00BB, 0x2026, 0x00A0, 0x00C0, 0x00C3, 0x00D5, 0x0152, 0x0153,
	0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00F7, 0x25CA,
	0x00FF, 0x0178, 0x2044, 0x20AC, 0x2039, 0x203A, 0xFB01, 0xFB02,
	0x2021, 0x00B7, 0x201A, 0x201E, 0x2030, 0x00C2, 0x00CA, 0x00C1,
	0x00CB, 0x00C8, 0x00CD, 0x00CE, 0x00CF, 0x00CC, 0x00D3, 0x00D4,
	0xF8FF, 0x00D2, 0x00DA, 0x00DB, 0x00D9, 0x0131, 0x02C6, 0x02DC,
	0x00AF, 0x02D8, 0x02D9, 0x02DA, 0x00B8, 0x02DD, 0x02DB, 0x02C7,
}

// decodeMacRomanByte decodes a single MacRomanEncoding byte.
func decodeMacRomanByte(b byte) (rune, bool) {
	if b >= 0x80 {
		return macRomanUnicode[b-0x80], true
	}
	return rune(b), true
}

// decodeBaseEncodingByte decodes one byte using a named base encoding.
func decodeBaseEncodingByte(b byte, base string) (rune, bool) {
	switch base {
	case "WinAnsiEncoding":
		return decodeWinAnsiByte(b)
	case "MacRomanEncoding":
		return decodeMacRomanByte(b)
	case "PDFDocEncoding":
		return decodePDFDocByte(b)
	default: // StandardEncoding ~ ASCII for text purposes
		return rune(b), true
	}
}

// fontNameSuggestsCJK reports whether a BaseFont name hints at a CJK font
// (e.g. "SimSun_GB2312", "MSungStd-Light-UCS2", "PMingLiU", "#B7#C2#CB#CE_GB2312").
func fontNameSuggestsCJK(baseFont string) bool {
	n := strings.ToLower(baseFont)
	for _, hint := range []string{"gb2312", "gbk", "gb ", "simsun", "simhei", "simfang", "simkai", "nsimsun",
		"mingliu", "msung", "liu", "song", "sung", "kai", "hei", "fang", "gothic", "mincho",
		"batang", "gulim", "dotum", "gungsuh", "unicode"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

// tryDecodingWithAltEncoders handles fonts whose strings are CJK-encoded
// (usually GBK) despite claiming a Latin base encoding. Returns the decoded
// text and true when an alternative decode produces plausible CJK content
// while the base-encoding decode produces mojibake.
func tryDecodingWithAltEncoders(data []byte, fi *fontInfo) (string, bool) {
	if len(data) < 2 {
		return "", false
	}
	hasHighBytes := false
	for _, b := range data {
		if b >= 0x80 {
			hasHighBytes = true
			break
		}
	}
	if !hasHighBytes {
		return "", false
	}
	candidates := []int{cmapFamilyGBK, cmapFamilyBig5, cmapFamilySJIS}
	if fontNameSuggestsCJK(fi.baseFont) {
		// trust the name hint first
		for _, fam := range candidates {
			out := decodeCJKBytes(fam, data)
			if countCJK(out) > len([]rune(out))/3 {
				return out, true
			}
		}
		return "", false
	}
	// No name hint: only try when base decode looks like mojibake.
	var sb strings.Builder
	for _, b := range data {
		if r, ok := decodeBaseEncodingByte(b, fi.baseEncoding); ok {
			sb.WriteRune(r)
		} else {
			sb.WriteByte(b)
		}
	}
	baseOut := sb.String()
	// mojibake heuristic: many Latin-1 accented chars + GBK lead-byte pattern
	mojibake := countMojibake(baseOut) > len([]rune(baseOut))/3
	if !mojibake {
		return "", false
	}
	for _, fam := range candidates {
		out := decodeCJKBytes(fam, data)
		if countCJK(out) > len([]rune(out))/4 {
			return out, true
		}
	}
	return "", false
}

func countCJK(s string) int {
	n := 0
	for _, r := range s {
		if (r >= 0x2E80 && r <= 0x9FFF) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) {
			n++
		}
	}
	return n
}

// countMojibake counts characters typical of CJK-decoded-as-Latin1 output
// (Latin-1 accented blocks 0xC0-0xFF, plus Ä Å Ç style artifacts).
func countMojibake(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x00C0 && r <= 0x00FF {
			n++
		}
	}
	return n
}

// glyphNameUnicode maps Adobe Glyph List names to Unicode. Covers the names
// most common in WinAnsi /Differences arrays plus a set of standard extras.
var glyphNameUnicode = map[string]rune{
	"space": 0x0020, "exclam": 0x0021, "quotedbl": 0x0022, "numbersign": 0x0023,
	"dollar": 0x0024, "percent": 0x0025, "ampersand": 0x0026, "quotesingle": 0x0027,
	"parenleft": 0x0028, "parenright": 0x0029, "asterisk": 0x002A, "plus": 0x002B,
	"comma": 0x002C, "hyphen": 0x002D, "period": 0x002E, "slash": 0x002F,
	"zero": 0x0030, "one": 0x0031, "two": 0x0032, "three": 0x0033, "four": 0x0034,
	"five": 0x0035, "six": 0x0036, "seven": 0x0037, "eight": 0x0038, "nine": 0x0039,
	"colon": 0x003A, "semicolon": 0x003B, "less": 0x003C, "equal": 0x003D,
	"greater": 0x003E, "question": 0x003F, "at": 0x0040,
	"A": 0x0041, "B": 0x0042, "C": 0x0043, "D": 0x0044, "E": 0x0045, "F": 0x0046,
	"G": 0x0047, "H": 0x0048, "I": 0x0049, "J": 0x004A, "K": 0x004B, "L": 0x004C,
	"M": 0x004D, "N": 0x004E, "O": 0x004F, "P": 0x0050, "Q": 0x0051, "R": 0x0052,
	"S": 0x0053, "T": 0x0054, "U": 0x0055, "V": 0x0056, "W": 0x0057, "X": 0x0058,
	"Y": 0x0059, "Z": 0x005A,
	"bracketleft": 0x005B, "backslash": 0x005C, "bracketright": 0x005D,
	"asciicircum": 0x005E, "underscore": 0x005F, "grave": 0x0060,
	"a": 0x0061, "b": 0x0062, "c": 0x0063, "d": 0x0064, "e": 0x0065, "f": 0x0066,
	"g": 0x0067, "h": 0x0068, "i": 0x0069, "j": 0x006A, "k": 0x006B, "l": 0x006C,
	"m": 0x006D, "n": 0x006E, "o": 0x006F, "p": 0x0070, "q": 0x0071, "r": 0x0072,
	"s": 0x0073, "t": 0x0074, "u": 0x0075, "v": 0x0076, "w": 0x0077, "x": 0x0078,
	"y": 0x0079, "z": 0x007A,
	"braceleft": 0x007B, "bar": 0x007C, "braceright": 0x007D, "asciitilde": 0x007E,
	"quoteleft": 0x2018, "quoteright": 0x2019, "quotedblleft": 0x201C,
	"quotedblright": 0x201D, "quotesinglbase": 0x201A, "quotedblbase": 0x201E,
	"guillemotleft": 0x00AB, "guillemotright": 0x00BB, "guilsinglleft": 0x2039,
	"guilsinglright": 0x203A,
	"endash":         0x2013, "emdash": 0x2014, "bullet": 0x2022, "dagger": 0x2020,
	"daggerdbl": 0x2021, "ellipsis": 0x2026, "perthousand": 0x2030,
	"fraction": 0x2044, "florin": 0x0192, "circumflex": 0x02C6, "caron": 0x02C7,
	"breve": 0x02D8, "dotaccent": 0x02D9, "ring": 0x02DA, "ogonek": 0x02DB,
	"tilde": 0x02DC, "hungarumlaut": 0x02DD, "macron": 0x00AF, "acute": 0x00B4,
	"cedilla": 0x00B8,
	"fi":      0xFB01, "fl": 0xFB02, "ff": 0xFB00, "ffi": 0xFB03, "ffl": 0xFB04,
	"Euro": 0x20AC, "trademark": 0x2122, "registered": 0x00AE, "copyright": 0x00A9,
	"trademarkserif": 0x2122, "registerserif": 0x00AE, "copyrightserif": 0x00A9,
	"exclamdown": 0x00A1, "cent": 0x00A2, "sterling": 0x00A3, "currency": 0x00A4,
	"yen": 0x00A5, "brokenbar": 0x00A6, "section": 0x00A7, "dieresis": 0x00A8,
	"ordfeminine": 0x00AA, "logicalnot": 0x00AC, "mu": 0x00B5, "paragraph": 0x00B6,
	"periodcentered": 0x00B7, "onesuperior": 0x00B9, "ordmasculine": 0x00BA,
	"onequarter": 0x00BC, "onehalf": 0x00BD, "threequarters": 0x00BE,
	"questiondown": 0x00BF, "Agrave": 0x00C0, "Aacute": 0x00C1, "Acircumflex": 0x00C2,
	"Atilde": 0x00C3, "Adieresis": 0x00C4, "Aring": 0x00C5, "AE": 0x00C6,
	"Ccedilla": 0x00C7, "Egrave": 0x00C8, "Eacute": 0x00C9, "Ecircumflex": 0x00CA,
	"Edieresis": 0x00CB, "Igrave": 0x00CC, "Iacute": 0x00CD, "Icircumflex": 0x00CE,
	"Idieresis": 0x00CF, "Eth": 0x00D0, "Ntilde": 0x00D1, "Ograve": 0x00D2,
	"Oacute": 0x00D3, "Ocircumflex": 0x00D4, "Otilde": 0x00D5, "Odieresis": 0x00D6,
	"multiply": 0x00D7, "Oslash": 0x00D8, "Ugrave": 0x00D9, "Uacute": 0x00DA,
	"Ucircumflex": 0x00DB, "Udieresis": 0x00DC, "Yacute": 0x00DD, "Thorn": 0x00DE,
	"germandbls": 0x00DF, "agrave": 0x00E0, "aacute": 0x00E1, "acircumflex": 0x00E2,
	"atilde": 0x00E3, "adieresis": 0x00E4, "aring": 0x00E5, "ae": 0x00E6,
	"ccedilla": 0x00E7, "egrave": 0x00E8, "eacute": 0x00E9, "ecircumflex": 0x00EA,
	"edieresis": 0x00EB, "igrave": 0x00EC, "iacute": 0x00ED, "icircumflex": 0x00EE,
	"idieresis": 0x00EF, "eth": 0x00F0, "ntilde": 0x00F1, "ograve": 0x00F2,
	"oacute": 0x00F3, "ocircumflex": 0x00F4, "otilde": 0x00F5, "odieresis": 0x00F6,
	"divide": 0x00F7, "oslash": 0x00F8, "ugrave": 0x00F9, "uacute": 0x00FA,
	"ucircumflex": 0x00FB, "udieresis": 0x00FC, "yacute": 0x00FD, "thorn": 0x00FE,
	"ydieresis": 0x00FF,
	// Greek
	"Alpha": 0x0391, "Beta": 0x0392, "Gamma": 0x0393, "Delta": 0x0394,
	"Epsilon": 0x0395, "Zeta": 0x0396, "Eta": 0x0397, "Theta": 0x0398,
	"Iota": 0x0399, "Kappa": 0x039A, "Lambda": 0x039B, "Mu": 0x039C,
	"Nu": 0x039D, "Xi": 0x039E, "Omicron": 0x039F, "Pi": 0x03A0,
	"Rho": 0x03A1, "Sigma": 0x03A3, "Tau": 0x03A4, "Upsilon": 0x03A5,
	"Phi": 0x03A6, "Chi": 0x03A7, "Psi": 0x03A8, "Omega": 0x03A9,
	"alpha": 0x03B1, "beta": 0x03B2, "gamma": 0x03B3, "delta": 0x03B4,
	"epsilon": 0x03B5, "zeta": 0x03B6, "eta": 0x03B7, "theta": 0x03B8,
	"iota": 0x03B9, "kappa": 0x03BA, "lambda": 0x03BB, "nu": 0x03BD,
	"xi": 0x03BE, "omicron": 0x03BF, "pi": 0x03C0, "rho": 0x03C1,
	"sigma": 0x03C3, "tau": 0x03C4, "upsilon": 0x03C5, "phi": 0x03C6,
	"chi": 0x03C7, "psi": 0x03C8, "omega": 0x03C9,
	"sigma1": 0x03C2, "theta1": 0x03D1, "phi1": 0x03D5, "omega1": 0x03D6,
	"Upsilon1": 0x03D2, "epsilon1": 0x03F5,
	// Math & symbols
	"plusminus": 0x00B1, "minus": 0x2212, "lessequal": 0x2264, "greaterequal": 0x2265,
	"notequal": 0x2260, "equivalence": 0x2261, "infinity": 0x221E,
	"integral": 0x222B, "product": 0x220F, "summation": 0x2211, "radical": 0x221A,
	"partialdiff": 0x2202, "gradient": 0x2207, "element": 0x2208, "notelement": 0x2209,
	"universal": 0x2200, "existential": 0x2203, "emptyset": 0x2205,
	"approxequal": 0x2248, "propersubset": 0x2282, "propersuperset": 0x2283,
	"reflexsubset": 0x2286, "reflexsuperset": 0x2287, "angleleft": 0x2329,
	"angleright": 0x232A, "aleph": 0x2135, "If": 0x0131,
	"arrowup": 0x2191, "arrowdown": 0x2193, "arrowleft": 0x2190, "arrowright": 0x2192,
	"arrowboth": 0x2194, "arrowupdn": 0x2195, "arrowdblup": 0x21D1,
	"arrowdbldown": 0x21D3, "arrowdblleft": 0x21D0, "arrowdblright": 0x21D2,
	"arrowdblboth": 0x21D4,
	"lozenge":      0x25CA, "diamond": 0x25C7, "square": 0x25A1, "circle": 0x25CB,
	"triangle": 0x25B3, "dotmath": 0x22C5, "asteriskmath": 0x2217,
	"divisionslash": 0x2215, "logicaland": 0x2227, "logicalor": 0x2228,
	"intersection": 0x2229, "union": 0x222A,
	"therefore": 0x2234, "suchthat": 0x220B,
	"Lslash": 0x0141, "lslash": 0x0142,
	"OE": 0x0152, "oe": 0x0153, "Scaron": 0x0160, "scaron": 0x0161,
	"Zcaron": 0x017D, "zcaron": 0x017E, "Ydieresis": 0x0178,
	"Scommaaccent": 0x0218, "scommaaccent": 0x0219,
	"Zacute": 0x0179, "zacute": 0x017A, "Zdotaccent": 0x017B, "zdotaccent": 0x017C,
}

// decodeGlyphName maps a glyph name (including "uniXXXX" and "uXXXX[XX]" forms)
// to a rune. Returns ok=false when unknown.
func decodeGlyphName(name string) (rune, bool) {
	if r, ok := glyphNameUnicode[name]; ok {
		return r, true
	}
	if strings.HasPrefix(name, "uni") && len(name) >= 7 {
		// uniXXXX possibly with multiple components: uni00410042
		rest := name[3:]
		okAll := len(rest)%4 == 0 && len(rest) > 0
		// Only decode single component here; multi-component names are rare
		// in Differences arrays and would need multi-rune support.
		if okAll {
			r := decodeHexRune(rest[:4])
			if r != 0xFFFD {
				return r, true
			}
		}
		return 0, false
	}
	if (strings.HasPrefix(name, "u") || strings.HasPrefix(name, "U")) && len(name) >= 5 {
		hexPart := name[1:]
		if isAllHex(hexPart) {
			r := decodeHexRune(hexPart)
			if r != 0xFFFD {
				return r, true
			}
		}
	}
	// "gXX" / "cidXX" forms can't be resolved without font data.
	return 0, false
}

func isAllHex(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func decodeHexRune(hex string) rune {
	var v uint32
	for i := 0; i < len(hex) && i < 8; i++ {
		ch := hex[i]
		var d uint32
		switch {
		case ch >= '0' && ch <= '9':
			d = uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			d = uint32(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			d = uint32(ch-'A') + 10
		default:
			return 0xFFFD
		}
		v = v<<4 | d
	}
	if v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
		return 0xFFFD
	}
	return rune(v)
}

// parseDifferences parses a /Differences array from a font dict.
// Format: /Differences [24 /breve 26 /caron ...] or with numeric ranges
// followed by multiple names: [65 /A /B /C].
// Returns code -> glyph name.
func parseDifferences(dict string) map[byte]string {
	diffs := make(map[byte]string)
	idx := strings.Index(dict, "/Differences")
	if idx < 0 {
		return diffs
	}
	rest := dict[idx+len("/Differences"):]
	start := strings.Index(rest, "[")
	if start < 0 {
		return diffs
	}
	end := strings.Index(rest[start:], "]")
	if end < 0 {
		return diffs
	}
	arr := rest[start+1 : start+end]

	fields := strings.Fields(arr)
	code := 0
	for _, f := range fields {
		if n, err := parsePDFFloat(f); err == nil && !strings.HasPrefix(f, "/") {
			code = int(n)
			continue
		}
		if strings.HasPrefix(f, "/") {
			name := strings.TrimPrefix(f, "/")
			if code >= 0 && code <= 255 {
				diffs[byte(code)] = name
			}
			code++
		}
	}
	return diffs
}

// parsePDFFloat parses a PDF numeric token.
func parsePDFFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
