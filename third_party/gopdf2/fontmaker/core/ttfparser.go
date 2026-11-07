package core

import (
	//"encoding/binary"
	//"encoding/hex"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var ERROR_NO_UNICODE_ENCODING_FOUND = errors.New("No Unicode encoding found")
var ERROR_UNEXPECTED_SUBTABLE_FORMAT = errors.New("Unexpected subtable format")
var ERROR_INCORRECT_MAGIC_NUMBER = errors.New("Incorrect magic number")
var ERROR_POSTSCRIPT_NAME_NOT_FOUND = errors.New("PostScript name not found")

// TTFParser true type font parser
type TTFParser struct {
	tables map[string]TableDirectoryEntry
	//head
	unitsPerEm       uint
	xMin             int
	yMin             int
	xMax             int
	yMax             int
	indexToLocFormat int
	//Hhea
	numberOfHMetrics uint
	ascender         int
	descender        int
	//end Hhea

	numGlyphs      uint
	widths         []uint
	chars          map[int]uint
	postScriptName string
	fontData       []byte

	//os2
	os2Version    uint
	Embeddable    bool
	Bold          bool
	typoAscender  int
	typoDescender int
	capHeight     int
	sxHeight      int

	//post
	italicAngle        int
	underlinePosition  int
	underlineThickness int
	isFixedPitch       bool
	sTypoLineGap       int
	usWinAscent        uint
	usWinDescent       uint

	//cmap
	IsShortIndex  bool
	LocaTable     []uint
	SegCount      uint
	StartCount    []uint
	EndCount      []uint
	IdRangeOffset []uint
	IdDelta       []uint
	GlyphIdArray  []uint
	symbol        bool

	//cmap format 12
	groupingTables []CmapFormat12GroupingTable

	//data of font
	cachedFontData []byte

	//kerning
	useKerning bool //user config for use or not use kerning
	kern       *KernTable
}

var Symbolic = 1 << 2
var Nonsymbolic = (1 << 5)

// Kern get KernTable
func (t *TTFParser) Kern() *KernTable {
	return t.kern
}

// UnderlinePosition position of underline
func (t *TTFParser) UnderlinePosition() int {
	return t.underlinePosition
}

// GroupingTables get cmap format12 grouping table
func (t *TTFParser) GroupingTables() []CmapFormat12GroupingTable {
	return t.groupingTables
}

// UnderlineThickness thickness of underline
func (t *TTFParser) UnderlineThickness() int {
	return t.underlineThickness
}

func (t *TTFParser) XHeight() int {
	if t.os2Version >= 2 && t.sxHeight != 0 {
		return t.sxHeight
	} else {
		return int((0.66) * float64(t.ascender))
	}
}

func (t *TTFParser) XMin() int {
	return t.xMin
}

func (t *TTFParser) YMin() int {
	return t.yMin
}

func (t *TTFParser) XMax() int {
	return t.xMax
}

func (t *TTFParser) YMax() int {
	return t.yMax
}

func (t *TTFParser) ItalicAngle() int {
	return t.italicAngle
}

func (t *TTFParser) Flag() int {
	flag := 0
	if t.symbol {
		flag |= Symbolic
	} else {
		flag |= Nonsymbolic
	}
	return flag
}

func (t *TTFParser) Ascender() int {
	if t.typoAscender == 0 {
		return t.ascender
	}
	return int(t.usWinAscent)
}

func (t *TTFParser) Descender() int {
	if t.typoDescender == 0 {
		return t.descender
	}
	descender := int(t.usWinDescent)
	if t.descender < 0 {
		descender = descender * (-1)
	}
	return descender
}

func (t *TTFParser) TypoAscender() int {
	return t.typoAscender
}

func (t *TTFParser) TypoDescender() int {
	return t.typoDescender
}

// CapHeight https://en.wikipedia.org/wiki/Cap_height
func (t *TTFParser) CapHeight() int {
	return t.capHeight
}

// NumGlyphs number of glyph
func (t *TTFParser) NumGlyphs() uint {
	return t.numGlyphs
}

func (t *TTFParser) UnitsPerEm() uint {
	return t.unitsPerEm
}

func (t *TTFParser) NumberOfHMetrics() uint {
	return t.numberOfHMetrics
}

func (t *TTFParser) Widths() []uint {
	return t.widths
}

func (t *TTFParser) Chars() map[int]uint {
	return t.chars
}

func (t *TTFParser) GetTables() map[string]TableDirectoryEntry {
	return t.tables
}

// SetUseKerning set useKerning must set before Parse
func (t *TTFParser) SetUseKerning(use bool) {
	t.useKerning = use
}

// Parse parse
func (t *TTFParser) Parse(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}
	return t.ParseFontData(data)
}

// ParseByReader parse by io.reader
func (t *TTFParser) ParseByReader(rd io.Reader) error {
	fontData, err := io.ReadAll(rd)
	if err != nil {
		return err
	}

	return t.ParseFontData(fontData)
}

// ParseFontData parses font data.
func (t *TTFParser) ParseFontData(fontData []byte) error {
	fd := bytes.NewReader(fontData)

	version, err := t.Read(fd, 4)
	if err != nil {
		return err
	}
	if !bytes.Equal(version, []byte{0x00, 0x01, 0x00, 0x00}) {
		return errors.New("Unrecognized file (font) format")
	}

	i := uint(0)
	numTables, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}
	err = t.Skip(fd, 3*2) //searchRange, entrySelector, rangeShift
	if err != nil {
		return err
	}

	t.tables = make(map[string]TableDirectoryEntry)
	for i < numTables {

		tag, err := t.Read(fd, 4)
		if err != nil {
			return err
		}

		checksum, err := t.ReadULong(fd)
		if err != nil {
			return err
		}

		offset, err := t.ReadULong(fd)
		if err != nil {
			return err
		}

		length, err := t.ReadULong(fd)
		if err != nil {
			return err
		}
		var table TableDirectoryEntry
		table.Offset = uint(offset)
		table.CheckSum = checksum
		table.Length = length
		t.tables[t.BytesToString(tag)] = table
		i++
	}

	err = t.ParseHead(fd)
	if err != nil {
		return err
	}

	err = t.ParseHhea(fd)
	if err != nil {
		return err
	}

	err = t.ParseMaxp(fd)
	if err != nil {
		return err
	}
	err = t.ParseHmtx(fd)
	if err != nil {
		return err
	}
	err = t.ParseCmap(fd)
	if err != nil {
		return err
	}
	err = t.ParseName(fd)
	if err != nil {
		return err
	}
	err = t.ParseOS2(fd)
	if err != nil {
		return err
	}
	err = t.ParsePost(fd)
	if err != nil {
		return err
	}
	err = t.ParseLoca(fd)
	if err != nil {
		return err
	}

	if t.useKerning {
		err = t.Parsekern(fd)
		if err != nil {
			return err
		}
	}

	t.cachedFontData = fontData

	return nil
}

func (t *TTFParser) FontData() []byte {
	return t.cachedFontData
}

// ParseLoca parse loca table https://www.microsoft.com/typography/otspec/loca.htm
func (t *TTFParser) ParseLoca(fd *bytes.Reader) error {

	t.IsShortIndex = false
	if t.indexToLocFormat == 0 {
		t.IsShortIndex = true
	}

	err := t.Seek(fd, "loca")
	if err != nil {
		return err
	}
	var locaTable []uint
	table := t.tables["loca"]
	if t.IsShortIndex {
		//do ShortIndex
		entries := table.Length / 2
		i := uint(0)
		for i < entries {
			item, err := t.ReadUShort(fd)
			if err != nil {
				return err
			}
			locaTable = append(locaTable, item*2)
			i++
		}
	} else {
		entries := table.Length / 4
		i := uint(0)
		for i < entries {
			item, err := t.ReadULong(fd)
			if err != nil {
				return err
			}
			locaTable = append(locaTable, item)
			i++
		}
	}
	t.LocaTable = locaTable
	return nil
}

// ParsePost parse post table https://www.microsoft.com/typography/otspec/post.htm
func (t *TTFParser) ParsePost(fd *bytes.Reader) error {

	err := t.Seek(fd, "post")
	if err != nil {
		return err
	}

	err = t.Skip(fd, 4) // version
	if err != nil {
		return err
	}

	t.italicAngle, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	err = t.Skip(fd, 2) // Skip decimal part
	if err != nil {
		return err
	}

	t.underlinePosition, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.underlineThickness, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	isFixedPitch, err := t.ReadULong(fd)
	if err != nil {
		return err
	}
	t.isFixedPitch = (isFixedPitch != 0)

	return nil
}

// ParseOS2 parse OS2 table https://www.microsoft.com/typography/otspec/OS2.htm
func (t *TTFParser) ParseOS2(fd *bytes.Reader) error {
	err := t.Seek(fd, "OS/2")
	if err != nil {
		return err
	}
	version, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}
	t.os2Version = version

	err = t.Skip(fd, 3*2) // xAvgCharWidth, usWeightClass, usWidthClass
	if err != nil {
		return err
	}
	fsType, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}
	t.Embeddable = (fsType != 2) && ((fsType & 0x200) == 0)

	err = t.Skip(fd, (11*2)+10+(4*4)+4)
	if err != nil {
		return err
	}
	fsSelection, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}
	t.Bold = ((fsSelection & 32) != 0)
	err = t.Skip(fd, 2*2) // usFirstCharIndex, usLastCharIndex
	if err != nil {
		return err
	}
	t.typoAscender, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.typoDescender, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.sTypoLineGap, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.usWinAscent, err = t.ReadUShort(fd)
	if err != nil {
		return err
	}

	t.usWinDescent, err = t.ReadUShort(fd)
	if err != nil {
		return err
	}

	if version >= 2 {

		err = t.Skip(fd, 2*4)
		if err != nil {
			return err
		}

		t.sxHeight, err = t.ReadShort(fd)
		if err != nil {
			return err
		}

		t.capHeight, err = t.ReadShort(fd)
		if err != nil {
			return err
		}

	} else {
		t.capHeight = t.ascender
	}

	return nil
}

// ParseName parse name table https://www.microsoft.com/typography/otspec/name.htm
func (t *TTFParser) ParseName(fd *bytes.Reader) error {

	//$this->Seek('name');
	err := t.Seek(fd, "name")
	if err != nil {
		return err
	}

	tableOffset, err := t.FTell(fd)
	if err != nil {
		return err
	}

	t.postScriptName = ""
	err = t.Skip(fd, 2) // format
	if err != nil {
		return err
	}

	count, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}

	stringOffset, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}

	for i := 0; i < int(count); i++ {
		err = t.Skip(fd, 3*2) // platformID, encodingID, languageID
		if err != nil {
			return err
		}
		nameID, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		length, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		offset, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		if nameID == 6 {
			// PostScript name
			_, err = fd.Seek(int64(tableOffset+stringOffset+offset), 0)
			if err != nil {
				return err
			}

			stmp, err := t.Read(fd, int(length))
			if err != nil {
				return err
			}

			var tmpStmp []byte
			for _, v := range stmp {
				if v != 0 {
					tmpStmp = append(tmpStmp, v)
				}
			}
			//s := fmt.Sprintf("%s", string(tmpStmp)) //strings(stmp)
			s := string(tmpStmp)
			s = strings.Replace(s, strconv.Itoa(0), "", -1)
			s, err = t.PregReplace("|[ \\[\\](){}<>/%]|", "", s)
			if err != nil {
				return err
			}
			t.postScriptName = s
			break
		}
	}

	if t.postScriptName == "" {
		return ERROR_POSTSCRIPT_NAME_NOT_FOUND
	}

	return nil
}

func (t *TTFParser) PregReplace(pattern string, replacement string, subject string) (string, error) {

	reg, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	str := reg.ReplaceAllString(subject, replacement)
	return str, nil
}

// ParseCmap parse cmap table format 4 https://www.microsoft.com/typography/otspec/cmap.htm
func (t *TTFParser) ParseCmap(fd *bytes.Reader) error {
	err := t.Seek(fd, "cmap")
	if err != nil {
		return err
	}
	err = t.Skip(fd, 2) // version
	if err != nil {
		return err
	}
	numTables, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}

	offset31 := uint(0)
	for i := 0; i < int(numTables); i++ {
		platformID, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		encodingID, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		offset, err := t.ReadULong(fd)
		if err != nil {
			return err
		}

		t.symbol = false //init
		if platformID == 3 && encodingID == 1 {
			if encodingID == 0 {
				t.symbol = true
			}
			offset31 = offset
		}
	} //end for

	if offset31 == 0 {
		//No Unicode encoding found
		return ERROR_NO_UNICODE_ENCODING_FOUND
	}

	var startCount, endCount, idDelta, idRangeOffset, glyphIDArray []uint

	_, err = fd.Seek(int64(t.tables["cmap"].Offset+offset31), 0)
	if err != nil {
		return err
	}

	format, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}

	if format != 4 {
		//Unexpected subtable format
		return ERROR_UNEXPECTED_SUBTABLE_FORMAT
	}

	length, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}

	err = t.Skip(fd, 2) // language
	if err != nil {
		return err
	}
	segCount, err := t.ReadUShort(fd)
	if err != nil {
		return err
	}
	segCount = segCount / 2
	t.SegCount = segCount
	err = t.Skip(fd, 3*2) // searchRange, entrySelector, rangeShift
	if err != nil {
		return err
	}

	glyphCount := (length - (16 + 8*segCount)) / 2

	for i := 0; i < int(segCount); i++ {
		tmp, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		endCount = append(endCount, tmp)
	}
	t.EndCount = endCount

	err = t.Skip(fd, 2) // reservedPad
	if err != nil {
		return err
	}

	for i := 0; i < int(segCount); i++ {
		tmp, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		startCount = append(startCount, tmp)
	}
	t.StartCount = startCount

	for i := 0; i < int(segCount); i++ {
		tmp, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		idDelta = append(idDelta, tmp)
	}
	t.IdDelta = idDelta

	offset, err := t.FTell(fd)
	if err != nil {
		return err
	}
	for i := 0; i < int(segCount); i++ {
		tmp, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		idRangeOffset = append(idRangeOffset, tmp)
	}
	t.IdRangeOffset = idRangeOffset
	//_ = glyphIdArray
	for i := 0; i < int(glyphCount); i++ {
		tmp, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		glyphIDArray = append(glyphIDArray, tmp)
	}
	t.GlyphIdArray = glyphIDArray

	t.chars = make(map[int]uint)
	for i := 0; i < int(segCount); i++ {
		c1 := startCount[i]
		c2 := endCount[i]
		d := idDelta[i]
		ro := idRangeOffset[i]
		if ro > 0 {
			_, err = fd.Seek(int64(offset+uint(2*i)+ro), 0)
			if err != nil {
				return err
			}
		}

		for c := c1; c <= c2; c++ {
			var gid uint
			if c == 0xFFFF {
				break
			}
			if ro > 0 {
				gid, err = t.ReadUShort(fd)
				if err != nil {
					return err
				}
				if gid > 0 {
					gid += d
				}
			} else {
				gid = c + d
			}

			if gid >= 65536 {
				gid -= 65536
			}
			if gid > 0 {
				t.chars[int(c)] = gid
			}
		}

	}

	_, err = t.ParseCmapFormat12(fd)
	if err != nil {
		return err
	}

	return nil
}

func (t *TTFParser) FTell(fd *bytes.Reader) (uint, error) {
	offset, err := fd.Seek(0, io.SeekCurrent)
	return uint(offset), err
}

// ParseHmtx parse hmtx table  https://www.microsoft.com/typography/otspec/hmtx.htm
func (t *TTFParser) ParseHmtx(fd *bytes.Reader) error {
	err := t.Seek(fd, "hmtx")
	if err != nil {
		return err
	}

	i := uint(0)
	for i < t.numberOfHMetrics {
		advanceWidth, err := t.ReadUShort(fd)
		if err != nil {
			return err
		}
		err = t.Skip(fd, 2)
		if err != nil {
			return err
		}
		t.widths = append(t.widths, advanceWidth)
		i++
	}
	if t.numberOfHMetrics < t.numGlyphs {
		lastWidth := t.widths[t.numberOfHMetrics-1]
		t.widths, err = t.ArrayPadUint(t.widths, t.numGlyphs, lastWidth)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *TTFParser) ArrayPadUint(arr []uint, size uint, val uint) ([]uint, error) {
	var result []uint
	i := uint(0)
	for i < size {
		if int(i) < len(arr) {
			result = append(result, arr[i])
		} else {
			result = append(result, val)
		}
		i++
	}

	return result, nil
}

// ParseHead parse head table  https://www.microsoft.com/typography/otspec/Head.htm
func (t *TTFParser) ParseHead(fd *bytes.Reader) error {

	err := t.Seek(fd, "head")
	if err != nil {
		return err
	}

	err = t.Skip(fd, 3*4) // version, fontRevision, checkSumAdjustment
	if err != nil {
		return err
	}
	magicNumber, err := t.ReadULong(fd)
	if err != nil {
		return err
	}

	if magicNumber != 0x5F0F3CF5 {
		return ERROR_INCORRECT_MAGIC_NUMBER
	}

	err = t.Skip(fd, 2)
	if err != nil {
		return err
	}

	t.unitsPerEm, err = t.ReadUShort(fd)
	if err != nil {
		return err
	}

	err = t.Skip(fd, 2*8) // created, modified
	if err != nil {
		return err
	}

	t.xMin, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.yMin, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.xMax, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.yMax, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	err = t.Skip(fd, 2*3) //skip macStyle,lowestRecPPEM,fontDirectionHint
	if err != nil {
		return err
	}

	t.indexToLocFormat, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	return nil
}

// ParseHhea parse hhea table  https://www.microsoft.com/typography/otspec/hhea.htm
func (t *TTFParser) ParseHhea(fd *bytes.Reader) error {

	err := t.Seek(fd, "hhea")
	if err != nil {
		return err
	}

	err = t.Skip(fd, 4) //skip version
	if err != nil {
		return err
	}

	t.ascender, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	t.descender, err = t.ReadShort(fd)
	if err != nil {
		return err
	}

	err = t.Skip(fd, 13*2)
	if err != nil {
		return err
	}

	t.numberOfHMetrics, err = t.ReadUShort(fd)
	if err != nil {
		return err
	}

	return nil
}

// ParseMaxp parse maxp table  https://www.microsoft.com/typography/otspec/Maxp.htm
func (t *TTFParser) ParseMaxp(fd *bytes.Reader) error {
	err := t.Seek(fd, "maxp")
	if err != nil {
		return err
	}
	err = t.Skip(fd, 4)
	if err != nil {
		return err
	}
	t.numGlyphs, err = t.ReadUShort(fd)
	if err != nil {
		return err
	}
	return nil
}

// ErrTableNotFound error table not found
var ErrTableNotFound = errors.New("table not found")

// Seek seek by tag
func (t *TTFParser) Seek(fd *bytes.Reader, tag string) error {
	table, ok := t.tables[tag]
	if !ok {
		return ErrTableNotFound
	}
	val := table.Offset
	_, err := fd.Seek(int64(val), 0)
	if err != nil {
		return err
	}
	return nil
}

// BytesToString convert bytes to string
func (t *TTFParser) BytesToString(b []byte) string {
	return string(b) //strings.TrimSpace(string(b))
}

// ReadUShort read ushort
func (t *TTFParser) ReadUShort(fd *bytes.Reader) (uint, error) {
	buff, err := t.Read(fd, 2)
	if err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint16(buff)
	return uint(n), nil
}

// ReadShort read short
func (t *TTFParser) ReadShort(fd *bytes.Reader) (int, error) {
	u, err := t.ReadUShort(fd)
	if err != nil {
		return 0, err
	}

	var v int
	if u >= 0x8000 {
		v = int(u) - 65536
	} else {
		v = int(u)
	}
	return v, nil
}

// ReadShortInt16 read short return int16
func (t *TTFParser) ReadShortInt16(fd *bytes.Reader) (int16, error) {
	n, err := t.ReadShort(fd)
	if err != nil {
		return 0, err
	}
	return int16(n), nil
}

// ReadULong read ulong
func (t *TTFParser) ReadULong(fd *bytes.Reader) (uint, error) {
	buff, err := t.Read(fd, 4)
	if err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint32(buff)
	return uint(n), nil
}

// Skip skip
func (t *TTFParser) Skip(fd *bytes.Reader, length int) error {
	_, err := fd.Seek(int64(length), 1)
	if err != nil {
		return err
	}
	return nil
}

// Read read
func (t *TTFParser) Read(fd *bytes.Reader, length int) ([]byte, error) {
	buff := make([]byte, length)
	readlength, err := fd.Read(buff)
	if err != nil {
		return nil, err
	}
	if readlength != length {
		return nil, errors.New("file out of length")
	}
	return buff, nil
}

// GlyphPoint is a single point of a glyph outline.
// OnCurve marks on-curve points; off-curve points are quadratic control
// points between their neighbors.
type GlyphPoint struct {
	X, Y    float64
	OnCurve bool
}

// ParseGlyphOutline parses the outline of one glyph from the glyf table.
// Returns one slice of points per contour (closed loops) and whether the
// glyph is a composite (whose components are already resolved here when
// the font data was parsed with a glyf-capable pass — simple fonts only
// return their own contours).
func (t *TTFParser) ParseGlyphOutline(fd *bytes.Reader, gid uint) ([][]GlyphPoint, error) {
	table, ok := t.tables["glyf"]
	if !ok {
		return nil, ERROR_NO_UNICODE_ENCODING_FOUND
	}
	if int(gid) >= len(t.LocaTable) {
		return nil, errors.New("glyph id out of range")
	}
	start := t.LocaTable[gid]
	var end uint
	if int(gid+1) < len(t.LocaTable) {
		end = t.LocaTable[gid+1]
	} else {
		end = table.Length
	}
	if end <= start {
		return nil, nil // empty glyph
	}
	if _, err := fd.Seek(int64(table.Offset)+int64(start), io.SeekStart); err != nil {
		return nil, err
	}
	numberOfContours, err := t.ReadShort(fd)
	if err != nil {
		return nil, err
	}
	if _, err := t.SkipInstr(fd, 8); err != nil {
		return nil, err
	}
	if numberOfContours < 0 {
		return t.parseCompositeGlyph(fd, int(-numberOfContours))
	}
	return t.parseSimpleGlyph(fd, int(numberOfContours))
}

// parseSimpleGlyph decodes a simple (non-composite) glyf entry.
func (t *TTFParser) parseSimpleGlyph(fd *bytes.Reader, numberOfContours int) ([][]GlyphPoint, error) {
	endPts := make([]uint16, numberOfContours)
	for i := 0; i < numberOfContours; i++ {
		v, err := t.ReadUShort(fd)
		if err != nil {
			return nil, err
		}
		endPts[i] = uint16(v)
	}
	instrLenV, err := t.ReadUShort(fd)
	if err != nil {
		return nil, err
	}
	instrLen := int(instrLenV)
	if _, err := t.SkipInstr(fd, instrLen); err != nil {
		return nil, err
	}

	numPoints := 0
	if numberOfContours > 0 {
		numPoints = int(endPts[numberOfContours-1]) + 1
	}
	if numPoints == 0 {
		return nil, nil
	}

	// flags
	flags := make([]byte, numPoints)
	for i := 0; i < numPoints; {
		fv, err := t.ReadUShort(fd)
		if err != nil {
			return nil, err
		}
		f := byte(fv)
		flags[i] = f
		i++
		if f&0x08 != 0 { // REPEAT_FLAG
			rep, err := t.ReadRawByte(fd)
			if err != nil {
				return nil, err
			}
			for j := 0; j < int(rep) && i < numPoints; j++ {
				flags[i] = f
				i++
			}
		}
	}

	// x coordinates (deltas)
	xs := make([]float64, numPoints)
	x := 0.0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		if f&0x02 != 0 { // XSHORT
			d, err := t.ReadRawByte(fd)
			if err != nil {
				return nil, err
			}
			if f&0x10 == 0 {
				x -= float64(d)
			} else {
				x += float64(d)
			}
		} else if f&0x10 == 0 { // same as previous
		} else {
			d, err := t.ReadShort(fd)
			if err != nil {
				return nil, err
			}
			x += float64(d)
		}
		xs[i] = x
	}

	// y coordinates (deltas)
	ys := make([]float64, numPoints)
	y := 0.0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		if f&0x04 != 0 { // YSHORT
			d, err := t.ReadRawByte(fd)
			if err != nil {
				return nil, err
			}
			if f&0x20 == 0 {
				y -= float64(d)
			} else {
				y += float64(d)
			}
		} else if f&0x20 == 0 {
		} else {
			d, err := t.ReadShort(fd)
			if err != nil {
				return nil, err
			}
			y += float64(d)
		}
		ys[i] = y
	}

	// split into contours
	out := make([][]GlyphPoint, 0, numberOfContours)
	startPt := 0
	for _, ep := range endPts {
		n := int(ep) + 1 - startPt
		if n <= 0 {
			startPt = int(ep) + 1
			continue
		}
		contour := make([]GlyphPoint, 0, n)
		for k := 0; k < n; k++ {
			idx := startPt + k
			contour = append(contour, GlyphPoint{X: xs[idx], Y: ys[idx], OnCurve: flags[idx]&0x01 != 0})
		}
		out = append(out, contour)
		startPt = int(ep) + 1
	}
	return out, nil
}

// parseCompositeGlyph resolves composite glyphs by following their
// components (translation and scale are supported; rotations are not).
func (t *TTFParser) parseCompositeGlyph(fd *bytes.Reader, numContours int) ([][]GlyphPoint, error) {
	const (
		ARG_1_AND_2_ARE_WORDS    = 0x0001
		WE_HAVE_A_SCALE          = 0x0008
		MORE_COMPONENTS          = 0x0020
		WE_HAVE_AN_X_AND_Y_SCALE = 0x0040
		WE_HAVE_A_TWO_BY_TWO     = 0x0080
	)
	var out [][]GlyphPoint
	for {
		flags, err := t.ReadUShort(fd)
		if err != nil {
			return nil, err
		}
		gid, err := t.ReadUShort(fd)
		if err != nil {
			return nil, err
		}
		var dx, dy float64
		if flags&ARG_1_AND_2_ARE_WORDS != 0 {
			a, e1 := t.ReadShort(fd)
			b, e2 := t.ReadShort(fd)
			if e1 != nil || e2 != nil {
				return nil, e1
			}
			dx, dy = float64(a), float64(b)
		} else {
			a, e1 := t.ReadRawByte(fd)
			b, e2 := t.ReadRawByte(fd)
			if e1 != nil || e2 != nil {
				return nil, e1
			}
			// args are signed bytes
			if a >= 128 {
				dx = float64(int8(a))
			} else {
				dx = float64(a)
			}
			if b >= 128 {
				dy = float64(int8(b))
			} else {
				dy = float64(b)
			}
		}
		scale := 1.0
		xs, ys := 1.0, 1.0
		x01, x10 := 0.0, 0.0
		if flags&WE_HAVE_A_SCALE != 0 {
			v, err := t.ReadF2Dot14(fd)
			if err != nil {
				return nil, err
			}
			scale = v
			xs, ys = scale, scale
		} else if flags&WE_HAVE_AN_X_AND_Y_SCALE != 0 {
			vx, err := t.ReadF2Dot14(fd)
			if err != nil {
				return nil, err
			}
			vy, err := t.ReadF2Dot14(fd)
			if err != nil {
				return nil, err
			}
			xs, ys = vx, vy
		} else if flags&WE_HAVE_A_TWO_BY_TWO != 0 {
			vx, e1 := t.ReadF2Dot14(fd)
			v01, e2 := t.ReadF2Dot14(fd)
			v10, e3 := t.ReadF2Dot14(fd)
			vy, e4 := t.ReadF2Dot14(fd)
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
				return nil, e1
			}
			xs, x01, x10, ys = vx, v01, v10, vy
		}
		sub, err := t.ParseGlyphOutline(fd, gid)
		if err != nil {
			return nil, err
		}
		for _, contour := range sub {
			comp := make([]GlyphPoint, 0, len(contour))
			for _, p := range contour {
				nx := p.X*xs + p.Y*x10 + dx
				ny := p.X*x01 + p.Y*ys + dy
				comp = append(comp, GlyphPoint{X: nx, Y: ny, OnCurve: p.OnCurve})
			}
			out = append(out, comp)
		}
		if flags&MORE_COMPONENTS == 0 {
			break
		}
	}
	_ = numContours
	return out, nil
}

// ReadF2Dot14 reads a 16.16-style fixed point number with 14 fractional bits.
func (t *TTFParser) ReadF2Dot14(fd *bytes.Reader) (float64, error) {
	raw, err := t.ReadUShort(fd)
	if err != nil {
		return 0, err
	}
	return float64(int16(raw)) / 16384.0, nil
}

// SkipInstr advances the reader by n bytes.
func (t *TTFParser) SkipInstr(fd *bytes.Reader, n int) (int, error) {
	got, err := fd.Seek(int64(n), io.SeekCurrent)
	return int(got), err
}

// ReadRawByte reads a single byte.
func (t *TTFParser) ReadRawByte(fd *bytes.Reader) (byte, error) {
	var b [1]byte
	n, err := fd.Read(b[:])
	if err != nil || n == 0 {
		return 0, io.EOF
	}
	return b[0], nil
}

// LookupChar maps a character code (from the font cmap) to a glyph index.
func (t *TTFParser) LookupChar(code int) (uint, bool) {
	g, ok := t.chars[code]
	return g, ok
}

// LookupWidth returns the advance width of a glyph in font units.
func (t *TTFParser) LookupWidth(gid uint) (uint, bool) {
	if int(gid) < len(t.widths) {
		return t.widths[gid], true
	}
	return 0, false
}

// DefaultWidth returns the default advance width (last hmtx entry).
func (t *TTFParser) DefaultWidth() uint {
	if n := len(t.widths); n > 0 {
		return t.widths[n-1]
	}
	return 0
}

// ParseGlyphOutlineFromData parses the glyph outline using the parser's
// internal copy of the font data (kept from the most recent ParseFontData).
func (t *TTFParser) ParseGlyphOutlineFromData(gid uint) ([][]GlyphPoint, error) {
	fontData := t.FontData()
	if fontData == nil {
		return nil, errors.New("no font data retained")
	}
	fd := bytes.NewReader(fontData)
	return t.ParseGlyphOutline(fd, gid)
}
