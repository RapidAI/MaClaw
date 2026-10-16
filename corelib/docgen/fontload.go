package docgen

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	ttfcore "github.com/VantageDataChat/GoPDF2/fontmaker/core"
)

var (
	sfntTrueTypeMagic = []byte{0x00, 0x01, 0x00, 0x00}
	sfntTrueTagMagic  = []byte{'t', 'r', 'u', 'e'}
	ttcMagic          = []byte{'t', 't', 'c', 'f'}
	ottoMagic         = []byte{'O', 'T', 'T', 'O'}
)

const (
	sfntHeaderLen   = 12
	sfntDirEntryLen = 16
	ttcHeaderLen    = 12

	// maxEmbeddableFontBytes guards the rebuild allocation on 32-bit
	// platforms against an absurd directory before make() panics.
	maxEmbeddableFontBytes = 1 << 30

	checkSumMagicSum = 0xB1B0AFBA
)

// loadFontFile loads a font file and returns plain sfnt TrueType bytes the
// PDF engine can embed. TrueType Collections (.ttc) — the only container
// macOS and most Linux distros ship their CJK system fonts in — are reduced
// to their first font. CFF-outline fonts (OTTO) are rejected because the
// PDF engine can only embed TrueType (glyf) outlines.
//
// A .ttc is read table by table, so the collection's other fonts never enter
// memory: a 90MB system collection only costs the bytes of the one font used.
func loadFontFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fontData, err := normalizeFontReaderAt(f, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fontData, nil
}

// normalizeFontData sniffs the container magic and returns embeddable
// TrueType bytes.
func normalizeFontData(data []byte) ([]byte, error) {
	return normalizeFontReaderAt(bytes.NewReader(data), int64(len(data)))
}

func normalizeFontReaderAt(r io.ReaderAt, size int64) ([]byte, error) {
	if size < sfntHeaderLen {
		return nil, fmt.Errorf("字体文件过小（%d 字节）", size)
	}
	header := make([]byte, sfntHeaderLen)
	if err := readAtFull(r, header, 0); err != nil {
		return nil, err
	}
	switch {
	case bytes.Equal(header[:4], sfntTrueTypeMagic), bytes.Equal(header[:4], sfntTrueTagMagic):
		data := make([]byte, size)
		if err := readAtFull(r, data, 0); err != nil {
			return nil, err
		}
		if err := requireTrueTypeOutline(data); err != nil {
			return nil, err
		}
		return data, nil
	case bytes.Equal(header[:4], ttcMagic):
		return extractTTCFirstFont(r, size)
	case bytes.Equal(header[:4], ottoMagic):
		return nil, fmt.Errorf("字体为 OpenType/CFF 轮廓，无法嵌入 PDF")
	default:
		return nil, fmt.Errorf("无法识别的字体文件格式")
	}
}

// cjkProbeRunes are code points a Chinese document cannot render without.
// A font that maps none of them would produce a PDF full of empty boxes —
// silently succeeding is worse than failing, so detection probes coverage.
var cjkProbeRunes = []rune{'中', '文'}

// fontCoversCJK reports whether the font maps the CJK probe code points.
func fontCoversCJK(data []byte) error {
	var parser ttfcore.TTFParser
	if err := parser.ParseFontData(data); err != nil {
		return err
	}
	chars := parser.Chars()
	for _, r := range cjkProbeRunes {
		if _, ok := chars[int(r)]; !ok {
			return fmt.Errorf("字体缺少中文字符 %q 的字形", r)
		}
	}
	return nil
}

func readAtFull(r io.ReaderAt, buf []byte, off int64) error {
	n, err := r.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("字体文件在偏移 %d 处被截断", off)
	}
	return nil
}

// sfntNumTables reads and validates numTables from a table directory.
func sfntNumTables(dir []byte) (uint16, error) {
	if len(dir) < sfntHeaderLen {
		return 0, fmt.Errorf("字体表目录不完整")
	}
	numTables := binary.BigEndian.Uint16(dir[4:6])
	if numTables == 0 || len(dir) < sfntHeaderLen+int(numTables)*sfntDirEntryLen {
		return 0, fmt.Errorf("字体表目录不完整")
	}
	return numTables, nil
}

// requireTrueTypeOutline verifies the font has a glyf table (TrueType
// outlines) and is not a CFF font in disguise.
func requireTrueTypeOutline(dir []byte) error {
	numTables, err := sfntNumTables(dir)
	if err != nil {
		return err
	}
	entries := dir[sfntHeaderLen:]
	var hasGlyf, hasCFF bool
	for i := 0; i < int(numTables); i++ {
		switch string(entries[i*sfntDirEntryLen : i*sfntDirEntryLen+4]) {
		case "glyf":
			hasGlyf = true
		case "CFF ":
			hasCFF = true
		}
	}
	if hasCFF && !hasGlyf {
		return fmt.Errorf("字体为 OpenType/CFF 轮廓，无法嵌入 PDF")
	}
	if !hasGlyf {
		return fmt.Errorf("字体缺少 glyf 表，无法嵌入 PDF")
	}
	return nil
}

// tablePlan is one table's source location and its place in the rebuilt font.
type tablePlan struct {
	tag       string
	offset    int64
	length    uint32
	newOffset int64
	paddedLen int64
}

// extractTTCFirstFont rebuilds a standalone TrueType font from the first
// font of a TrueType Collection: it copies a new sfnt header, rewrites the
// table directory to the new (compact) layout and reads each table in place.
// Tables are 4-byte aligned as the spec recommends, and head's
// checkSumAdjustment is recomputed so the result is a spec-valid font.
func extractTTCFirstFont(r io.ReaderAt, size int64) ([]byte, error) {
	ttcHeader := make([]byte, ttcHeaderLen+4) // tag, 2 version halves, numFonts, first offset
	if err := readAtFull(r, ttcHeader, 0); err != nil {
		return nil, fmt.Errorf("TTC 文件头不完整: %w", err)
	}
	numFonts := binary.BigEndian.Uint32(ttcHeader[8:12])
	if numFonts == 0 {
		return nil, fmt.Errorf("TTC 不包含任何字体")
	}
	firstOffset := int64(binary.BigEndian.Uint32(ttcHeader[12:16]))
	if firstOffset < sfntHeaderLen || firstOffset+sfntHeaderLen > size {
		return nil, fmt.Errorf("TTC 首字体偏移越界")
	}

	subHeader := make([]byte, sfntHeaderLen)
	if err := readAtFull(r, subHeader, firstOffset); err != nil {
		return nil, fmt.Errorf("TTC 内字体表目录不完整: %w", err)
	}
	version := subHeader[:4]
	if bytes.Equal(version, ottoMagic) {
		return nil, fmt.Errorf("TTC 内字体为 OpenType/CFF 轮廓，无法嵌入 PDF")
	}
	if !bytes.Equal(version, sfntTrueTypeMagic) && !bytes.Equal(version, sfntTrueTagMagic) {
		return nil, fmt.Errorf("TTC 内字体格式无法识别")
	}
	numTables := binary.BigEndian.Uint16(subHeader[4:6])
	if numTables == 0 {
		return nil, fmt.Errorf("TTC 内字体不含任何表")
	}
	dirLen := sfntHeaderLen + int(numTables)*sfntDirEntryLen
	dir := make([]byte, dirLen)
	if err := readAtFull(r, dir, firstOffset); err != nil {
		return nil, fmt.Errorf("TTC 内字体表目录不完整: %w", err)
	}
	if err := requireTrueTypeOutline(dir); err != nil {
		return nil, fmt.Errorf("TTC 内字体不可嵌入: %w", err)
	}

	// Layout: header + directory + aligned table payloads.
	total := int64(dirLen)
	plans := make([]tablePlan, 0, numTables)
	srcEntries := dir[sfntHeaderLen:]
	for i := 0; i < int(numTables); i++ {
		entry := srcEntries[i*sfntDirEntryLen : (i+1)*sfntDirEntryLen]
		offset := int64(binary.BigEndian.Uint32(entry[8:12]))
		length := binary.BigEndian.Uint32(entry[12:16])
		if offset < 0 || int64(length) > size || int64(length) > maxEmbeddableFontBytes || offset+int64(length) > size {
			return nil, fmt.Errorf("TTC 内字体表 %s 越界或过长", string(entry[0:4]))
		}
		paddedLen := int64(align4(length))
		if total+paddedLen > size+int64(dirLen) {
			return nil, fmt.Errorf("TTC 内字体表 %s 长度异常", string(entry[0:4]))
		}
		plans = append(plans, tablePlan{
			tag:       string(entry[0:4]),
			offset:    offset,
			length:    length,
			newOffset: total,
			paddedLen: paddedLen,
		})
		total += paddedLen
	}

	if total > maxEmbeddableFontBytes {
		return nil, fmt.Errorf("TTC 内首字体过大（%d 字节）", total)
	}
	out := make([]byte, total)
	copy(out[0:4], sfntTrueTypeMagic)
	binary.BigEndian.PutUint16(out[4:6], numTables)
	searchRange := sfntSearchRange(numTables)
	binary.BigEndian.PutUint16(out[6:8], searchRange)
	binary.BigEndian.PutUint16(out[8:10], sfntEntrySelector(numTables))
	binary.BigEndian.PutUint16(out[10:12], uint16(int(numTables)*sfntDirEntryLen-int(searchRange)))

	for i, p := range plans {
		srcEntry := srcEntries[i*sfntDirEntryLen : (i+1)*sfntDirEntryLen]
		newEntry := out[sfntHeaderLen+i*sfntDirEntryLen : sfntHeaderLen+(i+1)*sfntDirEntryLen]
		copy(newEntry[0:8], srcEntry[0:8]) // tag + checksum
		binary.BigEndian.PutUint32(newEntry[8:12], uint32(p.newOffset))
		binary.BigEndian.PutUint32(newEntry[12:16], p.length)
		if p.length == 0 {
			continue
		}
		if err := readAtFull(r, out[p.newOffset:p.newOffset+int64(p.length)], p.offset); err != nil {
			return nil, fmt.Errorf("读取 TTC 表 %s 失败: %w", p.tag, err)
		}
	}

	applyCheckSumAdjustment(out, plans)
	return out, nil
}

// applyCheckSumAdjustment fixes head.checkSumAdjustment so that the
// checksum of the whole rebuilt font equals 0xB1B0AFBA.
func applyCheckSumAdjustment(font []byte, plans []tablePlan) {
	for _, p := range plans {
		if p.tag != "head" || p.length < 12 {
			continue
		}
		headOff := int(p.newOffset)
		binary.BigEndian.PutUint32(font[headOff+8:headOff+12], 0)
		binary.BigEndian.PutUint32(font[headOff+8:headOff+12], checkSumMagicSum-sfntCheckSum(font))
		return
	}
}

// sfntCheckSum sums the font as big-endian uint32 words, zero-padding any
// trailing partial word.
func sfntCheckSum(font []byte) uint32 {
	var sum uint32
	for i := 0; i < len(font); i += 4 {
		word := uint32(0)
		for j := 0; j < 4; j++ {
			if i+j < len(font) {
				word = word<<8 | uint32(font[i+j])
			} else {
				word <<= 8
			}
		}
		sum += word
	}
	return sum
}

func align4(n uint32) uint32 {
	return (n + 3) & ^uint32(3)
}

// sfntSearchRange / sfntEntrySelector derive the binary-search hint fields of
// the sfnt header from numTables, per spec: searchRange is (largest power of
// two <= numTables) * 16 and entrySelector is log2 of that same power — note
// entrySelector counts *tables*, not directory bytes.
func sfntSearchRange(numTables uint16) uint16 {
	pow := 1
	for pow*2 <= int(numTables) {
		pow *= 2
	}
	return uint16(pow * sfntDirEntryLen)
}

func sfntEntrySelector(numTables uint16) uint16 {
	var sel uint16
	for pow := 1; pow*2 <= int(numTables); pow *= 2 {
		sel++
	}
	return sel
}
