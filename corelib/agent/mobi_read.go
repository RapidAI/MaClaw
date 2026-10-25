package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// extractMOBIText reads a Mobipocket book (.mobi) and returns its text.
// PalmDOC and HUFF/CDIC compression are both read. Images are skipped.
// An encrypted book is refused; this reader does not accept a password.
func extractMOBIText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("MOBI", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	parsed, err := extractMOBIBytes(data)
	return parsed, mobiMalformed(err)
}

// mobiMalformed marks a structural parse failure as a container rejection.
// The office tool otherwise treats an ordinary parser error as recoverable
// and tells the model to craft a second parser for the same bytes. Encryption
// and size limits already have their own refusals and must keep them.
func mobiMalformed(err error) error {
	if err == nil || errors.Is(err, errOfficeReadEncryptedContainer) || errors.Is(err, errOfficeReadOutputTooLarge) || errors.Is(err, errOfficeReadInputTooLarge) || errors.Is(err, errOfficeReadUnsafeContainer) || errors.Is(err, errOfficeReadSourceChanged) {
		return err
	}
	return fmt.Errorf("%w: %w", err, errOfficeReadUnsafeContainer)
}

const (
	mobiCompressionNone    = 1
	mobiCompressionPalmDOC = 2
	mobiCompressionHuff    = 17480
	mobiMaxRawBytes        = 8 << 20
	mobiMaxRecords         = 65535
)

func extractMOBIBytes(data []byte) (string, error) {
	if len(data) < 78 {
		return "", fmt.Errorf("无法解析 MOBI: 文件过短")
	}
	ident := strings.ToUpper(string(data[60:68]))
	if ident != "BOOKMOBI" && ident != "TEXTREAD" {
		return "", fmt.Errorf("无法解析 MOBI: 不是 Mobipocket 电子书")
	}
	nrec := int(binary.BigEndian.Uint16(data[76:78]))
	if nrec < 1 || nrec > mobiMaxRecords || 78+nrec*8 > len(data) {
		return "", fmt.Errorf("无法解析 MOBI: 记录表无效")
	}
	offsets := make([]int, nrec+1)
	for i := 0; i < nrec; i++ {
		offsets[i] = int(binary.BigEndian.Uint32(data[78+i*8 : 82+i*8]))
		if offsets[i] < 0 || offsets[i] > len(data) {
			return "", fmt.Errorf("无法解析 MOBI: 记录偏移无效")
		}
		if i > 0 && offsets[i] < offsets[i-1] {
			return "", fmt.Errorf("无法解析 MOBI: 记录顺序无效")
		}
	}
	offsets[nrec] = len(data)

	rec0 := data[offsets[0]:offsets[1]]
	if len(rec0) < 16 {
		return "", fmt.Errorf("无法解析 MOBI: 缺少文件头")
	}
	compression := binary.BigEndian.Uint16(rec0[0:2])
	textLen := int(binary.BigEndian.Uint32(rec0[4:8]))
	textRecords := int(binary.BigEndian.Uint16(rec0[8:10]))
	encryption := binary.BigEndian.Uint16(rec0[12:14])
	if encryption != 0 {
		return "", errOfficeReadEncryptedContainer
	}
	if textRecords <= 0 || textRecords >= nrec {
		return "", fmt.Errorf("无法解析 MOBI: 正文记录数无效")
	}
	if textLen < 0 || textLen > mobiMaxRawBytes {
		return "", errOfficeReadOutputTooLarge
	}

	var codec func([]byte) (string, error)
	var title string
	var authors []string
	var extraFlags uint16
	var huff *mobiHuff
	if len(rec0) >= 20 && string(rec0[16:20]) == "MOBI" {
		headerLen := 0
		if len(rec0) >= 24 {
			headerLen = int(binary.BigEndian.Uint32(rec0[20:24]))
		}
		if len(rec0) >= 32 {
			codec = mobiCodec(binary.BigEndian.Uint32(rec0[28:32]))
		}
		if len(rec0) >= 0x5C {
			toff := int(binary.BigEndian.Uint32(rec0[0x54:0x58]))
			tlen := int(binary.BigEndian.Uint32(rec0[0x58:0x5C]))
			if toff >= 0 && tlen > 0 && toff <= len(rec0) && tlen <= len(rec0)-toff {
				title = strings.TrimSpace(mobiDecode(codec, rec0[toff:toff+tlen]))
			}
		}
		if headerLen >= 0xE4 && len(rec0) >= 0xF4 {
			extraFlags = binary.BigEndian.Uint16(rec0[0xF2:0xF4])
		}
		if compression == mobiCompressionHuff {
			if len(rec0) < 0x78 {
				return "", fmt.Errorf("无法解析 MOBI: 缺少 HUFF 索引")
			}
			huffAt := int(binary.BigEndian.Uint32(rec0[0x70:0x74]))
			huffN := int(binary.BigEndian.Uint32(rec0[0x74:0x78]))
			loaded, err := loadMOBIHuff(data, offsets, huffAt, huffN)
			if err != nil {
				return "", err
			}
			huff = loaded
		}
		if headerLen > 0 && 16+headerLen < len(rec0) && len(rec0) >= 0x84 {
			if binary.BigEndian.Uint32(rec0[0x80:0x84])&0x40 != 0 {
				authors = mobiEXTHAuthors(rec0[16+headerLen:], codec)
				if long := mobiEXTHText(rec0[16+headerLen:], 503, codec); long != "" {
					title = long
				}
			}
		}
	}
	if codec == nil {
		// TEXTREAD and other books without a MOBI header leave the codepage unset.
		codec = mobiCodec(0)
	}

	var raw bytes.Buffer
	for i := 1; i <= textRecords; i++ {
		chunk := data[offsets[i]:offsets[i+1]]
		plain, err := mobiUnpackRecord(chunk, compression, extraFlags, huff)
		if err != nil && extraFlags != 0 {
			plain, err = mobiUnpackRecord(chunk, compression, 0, huff)
		}
		if err != nil {
			return "", err
		}
		if raw.Len()+len(plain) > mobiMaxRawBytes {
			return "", errOfficeReadOutputTooLarge
		}
		raw.Write(plain)
	}
	body := strings.TrimSpace(mobiHTMLText(mobiDecode(codec, raw.Bytes())))
	if body == "" {
		return "", fmt.Errorf("MOBI 里没有可读取的正文")
	}
	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if len(authors) > 0 {
		b.WriteString("作者：")
		b.WriteString(strings.Join(authors, ", "))
		b.WriteString("\n\n")
	}
	b.WriteString(body)
	return strings.TrimSpace(b.String()), nil
}

func mobiCodec(codepage uint32) func([]byte) (string, error) {
	switch codepage {
	case 65001:
		return func(b []byte) (string, error) { return string(b), nil }
	case 1252:
		return mobiDecode1252
	default:
		// Codepage 0 is unset, not UTF-8. Keep bytes that are already UTF-8 and
		// read every other byte string as Windows-1252, which is what PalmDOC
		// and a missing MOBI header actually store.
		return func(b []byte) (string, error) {
			if utf8.Valid(b) {
				return string(b), nil
			}
			return mobiDecode1252(b)
		}
	}
}

func mobiDecode1252(b []byte) (string, error) {
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func mobiDecode(codec func([]byte) (string, error), b []byte) string {
	if codec == nil {
		return string(b)
	}
	s, err := codec(b)
	if err != nil {
		return string(b)
	}
	return strings.TrimRight(s, "\x00")
}

func mobiUnpackRecord(chunk []byte, compression, extraFlags uint16, huff *mobiHuff) ([]byte, error) {
	// n == len(chunk) is a record whose bytes are only the trailing entry.
	// Leaving that record in place writes the entry itself into the text.
	if n := mobiTrailingSize(chunk, extraFlags); n > 0 && n <= len(chunk) {
		chunk = chunk[:len(chunk)-n]
	}
	switch compression {
	case mobiCompressionNone:
		return append([]byte(nil), chunk...), nil
	case mobiCompressionPalmDOC:
		return palmDOCUnpack(chunk)
	case mobiCompressionHuff:
		if huff == nil {
			return nil, fmt.Errorf("无法解析 MOBI: 缺少 HUFF 字典")
		}
		return huff.unpack(chunk, 0)
	default:
		return nil, fmt.Errorf("无法解析 MOBI: 不支持的压缩方式 %d", compression)
	}
}

// mobiTrailingSize peels one trailing entry per set bit from 15 down to 1.
// Each entry carries its own backward-encoded byte length, so the walk only
// decides how many entries to remove. Bit 0 is the overlap width of the byte
// left after those entries.
func mobiTrailingSize(data []byte, flags uint16) int {
	if flags == 0 || len(data) == 0 {
		return 0
	}
	num := 0
	size := len(data)
	for bit := 15; bit >= 1; bit-- {
		if flags&(1<<uint(bit)) == 0 {
			continue
		}
		sz := mobiBackEncodedSize(data[:size])
		if sz <= 0 || sz > size {
			return 0
		}
		size -= sz
		num += sz
	}
	if flags&1 != 0 && size > 0 {
		sz := int(data[size-1]&3) + 1
		if sz <= size {
			num += sz
		}
	}
	return num
}

// mobiBackEncodedSize reads a trailing-entry length stored at the end of data.
// The last byte holds the low 7 bits, and each preceding byte holds the next
// 7 bits. The high bit ends the integer. Calibre encodes 0x11111 as the bytes
// 0x84 0x22 0x11. Four bytes with no terminator are not a length.
func mobiBackEncodedSize(data []byte) int {
	num := 0
	bitpos := 0
	size := len(data)
	for i := 0; i < 4; i++ {
		if size == 0 {
			return 0
		}
		v := data[size-1]
		size--
		num |= int(v&0x7f) << bitpos
		bitpos += 7
		if v&0x80 != 0 {
			return num
		}
	}
	return 0
}

func palmDOCUnpack(in []byte) ([]byte, error) {
	out := make([]byte, 0, len(in)*2)
	for p := 0; p < len(in); {
		c := in[p]
		p++
		switch {
		case c >= 1 && c <= 8:
			if p+int(c) > len(in) {
				return nil, fmt.Errorf("无法解析 MOBI: PalmDOC 数据被截断")
			}
			out = append(out, in[p:p+int(c)]...)
			p += int(c)
		case c < 128:
			out = append(out, c)
		case c >= 192:
			out = append(out, ' ', c^0x80)
		default:
			if p >= len(in) {
				return nil, fmt.Errorf("无法解析 MOBI: PalmDOC 数据被截断")
			}
			pair := int(c)<<8 | int(in[p])
			p++
			distance := (pair >> 3) & 0x07ff
			length := (pair & 7) + 3
			if distance <= 0 || distance > len(out) {
				return nil, fmt.Errorf("无法解析 MOBI: PalmDOC 引用无效")
			}
			for i := 0; i < length; i++ {
				out = append(out, out[len(out)-distance])
			}
		}
		if len(out) > mobiMaxRawBytes {
			return nil, errOfficeReadOutputTooLarge
		}
	}
	return out, nil
}

type mobiHuffEntry struct {
	codelen int
	term    bool
	maxcode uint64
}

type mobiHuffPhrase struct {
	data []byte
	leaf bool
	busy bool
}

type mobiHuff struct {
	dict1   [256]mobiHuffEntry
	mincode []uint64
	maxcode []uint64
	phrases []mobiHuffPhrase
}

func loadMOBIHuff(data []byte, offsets []int, at, n int) (*mobiHuff, error) {
	if at < 0 || n < 2 || at+n > len(offsets)-1 {
		return nil, fmt.Errorf("无法解析 MOBI: HUFF 记录无效")
	}
	h := &mobiHuff{}
	first := data[offsets[at]:offsets[at+1]]
	if err := h.loadHuff(first); err != nil {
		return nil, err
	}
	for i := 1; i < n; i++ {
		if err := h.loadCdic(data[offsets[at+i]:offsets[at+i+1]]); err != nil {
			return nil, err
		}
	}
	if len(h.phrases) == 0 {
		return nil, fmt.Errorf("无法解析 MOBI: HUFF 字典为空")
	}
	return h, nil
}

func (h *mobiHuff) loadHuff(rec []byte) error {
	if len(rec) < 16 || string(rec[0:8]) != "HUFF\x00\x00\x00\x18" {
		return fmt.Errorf("无法解析 MOBI: HUFF 头无效")
	}
	off1 := int(binary.BigEndian.Uint32(rec[8:12]))
	off2 := int(binary.BigEndian.Uint32(rec[12:16]))
	if off1 < 0 || off2 < 0 || off1+256*4 > len(rec) || off2+64*4 > len(rec) {
		return fmt.Errorf("无法解析 MOBI: HUFF 表越界")
	}
	for i := 0; i < 256; i++ {
		v := binary.BigEndian.Uint32(rec[off1+i*4 : off1+i*4+4])
		codelen := int(v & 0x1f)
		if codelen == 0 || codelen > 32 {
			return fmt.Errorf("无法解析 MOBI: HUFF 码长无效")
		}
		term := v&0x80 != 0
		if codelen <= 8 && !term {
			return fmt.Errorf("无法解析 MOBI: HUFF 短码不是终结码")
		}
		maxcode := uint64(v >> 8)
		maxcode = ((maxcode + 1) << (32 - codelen)) - 1
		h.dict1[i] = mobiHuffEntry{codelen: codelen, term: term, maxcode: maxcode}
	}
	dict2 := make([]uint32, 64)
	for i := 0; i < 64; i++ {
		dict2[i] = binary.BigEndian.Uint32(rec[off2+i*4 : off2+i*4+4])
	}
	h.mincode = make([]uint64, 33)
	h.maxcode = make([]uint64, 33)
	evens := make([]uint32, 0, 33)
	odds := make([]uint32, 0, 33)
	evens = append(evens, 0)
	odds = append(odds, 0)
	for i := 0; i < 64; i += 2 {
		evens = append(evens, dict2[i])
		odds = append(odds, dict2[i+1])
	}
	for codelen := 0; codelen < len(evens) && codelen < 33; codelen++ {
		shift := uint(32 - codelen)
		h.mincode[codelen] = uint64(evens[codelen]) << shift
		h.maxcode[codelen] = ((uint64(odds[codelen]) + 1) << shift) - 1
	}
	return nil
}

func (h *mobiHuff) loadCdic(rec []byte) error {
	if len(rec) < 16 || string(rec[0:8]) != "CDIC\x00\x00\x00\x10" {
		return fmt.Errorf("无法解析 MOBI: CDIC 头无效")
	}
	phrases := int(binary.BigEndian.Uint32(rec[8:12]))
	bits := int(binary.BigEndian.Uint32(rec[12:16]))
	if bits < 0 || bits > 16 {
		return fmt.Errorf("无法解析 MOBI: CDIC 位宽无效")
	}
	available := phrases - len(h.phrases)
	n := 1 << bits
	if available < n {
		n = available
	}
	if n <= 0 {
		return nil
	}
	if 16+n*2 > len(rec) {
		return fmt.Errorf("无法解析 MOBI: CDIC 索引越界")
	}
	for i := 0; i < n; i++ {
		off := int(binary.BigEndian.Uint16(rec[16+i*2 : 18+i*2]))
		if 16+off+2 > len(rec) {
			return fmt.Errorf("无法解析 MOBI: CDIC 词条越界")
		}
		blen := int(binary.BigEndian.Uint16(rec[16+off : 18+off]))
		size := blen & 0x7fff
		if 18+off+size > len(rec) {
			return fmt.Errorf("无法解析 MOBI: CDIC 词条越界")
		}
		slice := append([]byte(nil), rec[18+off:18+off+size]...)
		h.phrases = append(h.phrases, mobiHuffPhrase{data: slice, leaf: blen&0x8000 != 0})
	}
	return nil
}

func (h *mobiHuff) unpack(data []byte, depth int) ([]byte, error) {
	if depth > 32 {
		return nil, fmt.Errorf("无法解析 MOBI: HUFF 递归过深")
	}
	if len(data) == 0 {
		return nil, nil
	}
	padded := append(append([]byte(nil), data...), make([]byte, 8)...)
	bitsleft := len(data) * 8
	pos := 0
	x := binary.BigEndian.Uint64(padded[pos : pos+8])
	n := 32
	var out bytes.Buffer
	for {
		if n <= 0 {
			pos += 4
			if pos+8 > len(padded) {
				break
			}
			x = binary.BigEndian.Uint64(padded[pos : pos+8])
			n += 32
		}
		code := uint64((x >> uint(n)) & 0xffffffff)
		entry := h.dict1[code>>24]
		codelen := entry.codelen
		maxcode := entry.maxcode
		if !entry.term {
			for codelen < len(h.mincode) && code < h.mincode[codelen] {
				codelen++
			}
			if codelen >= len(h.maxcode) {
				return nil, fmt.Errorf("无法解析 MOBI: HUFF 码长越界")
			}
			maxcode = h.maxcode[codelen]
		}
		if codelen <= 0 || codelen > 32 {
			return nil, fmt.Errorf("无法解析 MOBI: HUFF 码长无效")
		}
		n -= codelen
		bitsleft -= codelen
		if bitsleft < 0 {
			break
		}
		diff := int64(maxcode) - int64(code)
		if diff < 0 {
			return nil, fmt.Errorf("无法解析 MOBI: HUFF 码字无效")
		}
		index := int(uint64(diff) >> uint(32-codelen))
		if index < 0 || index >= len(h.phrases) {
			return nil, fmt.Errorf("无法解析 MOBI: HUFF 词条索引无效")
		}
		phrase := &h.phrases[index]
		chunk := phrase.data
		if !phrase.leaf {
			if phrase.busy {
				return nil, fmt.Errorf("无法解析 MOBI: HUFF 字典循环")
			}
			phrase.busy = true
			decoded, err := h.unpack(phrase.data, depth+1)
			phrase.busy = false
			if err != nil {
				return nil, err
			}
			phrase.data = decoded
			phrase.leaf = true
			chunk = decoded
		}
		if out.Len()+len(chunk) > mobiMaxRawBytes {
			return nil, errOfficeReadOutputTooLarge
		}
		out.Write(chunk)
	}
	return out.Bytes(), nil
}

func mobiEXTHAuthors(raw []byte, codec func([]byte) (string, error)) []string {
	text := mobiEXTHTexts(raw, 100, codec)
	return text
}

func mobiEXTHText(raw []byte, want uint32, codec func([]byte) (string, error)) string {
	items := mobiEXTHTexts(raw, want, codec)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func mobiEXTHTexts(raw []byte, want uint32, codec func([]byte) (string, error)) []string {
	if len(raw) < 12 || string(raw[0:4]) != "EXTH" {
		return nil
	}
	length := int(binary.BigEndian.Uint32(raw[4:8]))
	count := int(binary.BigEndian.Uint32(raw[8:12]))
	if length < 12 || length > len(raw) || count < 0 || count > 4096 {
		return nil
	}
	pos := 12
	var out []string
	for i := 0; i < count && pos+8 <= length; i++ {
		idx := binary.BigEndian.Uint32(raw[pos : pos+4])
		size := int(binary.BigEndian.Uint32(raw[pos+4 : pos+8]))
		if size < 8 || pos+size > length {
			return out
		}
		if idx == want {
			item := strings.TrimSpace(mobiDecode(codec, raw[pos+8:pos+size]))
			if item != "" {
				out = append(out, item)
			}
		}
		pos += size
	}
	return out
}

func mobiHTMLText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// Script and style are removed on the original bytes. strings.ToLower can
	// change the length (U+0130), so a parallel lowercase copy cannot be used
	// as an index back into s.
	s = mobiDropHTMLElement(s, "script")
	s = mobiDropHTMLElement(s, "style")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		tag := strings.ToLower(s[i+1 : i+end])
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "/"))
		name := tag
		if sp := strings.IndexAny(name, " \t\n/"); sp >= 0 {
			name = name[:sp]
		}
		switch name {
		case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "blockquote", "section":
			b.WriteByte('\n')
		}
		i += end + 1
	}
	text := html.UnescapeString(b.String())
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\u00a0", " "))
		if line == "" {
			blank++
			if blank <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blank = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func mobiDropHTMLElement(s, name string) string {
	open := "<" + name
	close := "</" + name + ">"
	var b strings.Builder
	for {
		i := mobiIndexASCIIFold(s, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		// <scripture> and <style-guide> are different elements. The byte after
		// the name must end the tag name (>, /, or whitespace).
		nameEnd := i + len(open)
		if nameEnd < len(s) && mobiHTMLNameContinues(s[nameEnd]) {
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		b.WriteString(s[:i])
		gt := strings.IndexByte(s[nameEnd:], '>')
		if gt < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		after := nameEnd + gt + 1
		end := mobiIndexASCIIFold(s[after:], close)
		if end < 0 {
			s = s[after:]
			continue
		}
		s = s[after+end+len(close):]
	}
}

// mobiIndexASCIIFold finds lowerASCII in s. A-Z matches a-z. The search stays
// on s so the returned index is a byte offset into the original string.
func mobiIndexASCIIFold(s, lowerASCII string) int {
	n := len(lowerASCII)
	if n == 0 || len(s) < n {
		return -1
	}
	from := 0
	for from+n <= len(s) {
		rel := strings.IndexByte(s[from:], lowerASCII[0])
		if rel < 0 {
			return -1
		}
		i := from + rel
		if i+n <= len(s) && mobiASCIIPrefixFold(s[i:], lowerASCII) {
			return i
		}
		from = i + 1
	}
	return -1
}

// mobiHTMLNameContinues reports whether c is still part of an HTML tag name.
// A match of "<script" must not consume "<scripture>" or "<style-guide>".
func mobiHTMLNameContinues(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == ':' || c == '_' || c == '.' || c >= 0x80:
		return true
	default:
		return false
	}
}

func mobiASCIIPrefixFold(s, lowerASCII string) bool {
	if len(s) < len(lowerASCII) {
		return false
	}
	for i := 0; i < len(lowerASCII); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lowerASCII[i] {
			return false
		}
	}
	return true
}
