package gopdf

import (
	"encoding/binary"
	"errors"
)

// ============================================================
// Minimal TrueType parsing for the built-in rasterizer: reads only the
// tables the renderer needs (head, maxp, loca, glyf, cmap, hmtx) and
// tolerates missing optional tables — gopdf's subset fonts omit several
// tables that full TTF parsers require.
// ============================================================

// minTTF is a minimal parsed TrueType font.
type minTTF struct {
	unitsPerEm       uint16
	indexToLocFormat uint16
	numGlyphs        uint16
	loca             []uint32 // per-glyph offsets into glyf
	glyf             []byte
	// charCode -> glyph index (from a unicode cmap subtable)
	chars map[int]uint16
	// advance widths per glyph (font units); last repeats for the tail
	widths []uint16
}

var errMinTTF = errors.New("not a supported TrueType font")

// parseMinTTF parses raw TrueType data. cmap and hmtx are optional.
func parseMinTTF(data []byte) (*minTTF, error) {
	if len(data) < 12 || data[0] != 0 || data[1] != 1 || data[2] != 0 || data[3] != 0 {
		return nil, errMinTTF
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	type tbl struct{ offset, length uint32 }
	tables := make(map[string]tbl, numTables)
	for i := 0; i < numTables; i++ {
		base := 12 + 16*i
		if base+16 > len(data) {
			break
		}
		tag := string(data[base : base+4])
		tables[tag] = tbl{
			offset: binary.BigEndian.Uint32(data[base+8 : base+12]),
			length: binary.BigEndian.Uint32(data[base+12 : base+16]),
		}
	}

	m := &minTTF{chars: map[int]uint16{}}

	// head
	if h, ok := tables["head"]; ok && int(h.offset+54) <= len(data) {
		m.unitsPerEm = binary.BigEndian.Uint16(data[h.offset+18 : h.offset+20])
		m.indexToLocFormat = binary.BigEndian.Uint16(data[h.offset+50 : h.offset+52])
	} else {
		return nil, errMinTTF
	}
	if m.unitsPerEm == 0 {
		m.unitsPerEm = 1000
	}

	// maxp
	if p, ok := tables["maxp"]; ok && int(p.offset+6) <= len(data) {
		m.numGlyphs = binary.BigEndian.Uint16(data[p.offset+4 : p.offset+6])
	}

	// loca
	loc, ok := tables["loca"]
	if !ok {
		return nil, errMinTTF
	}
	if m.numGlyphs == 0 {
		return nil, errMinTTF
	}
	if m.indexToLocFormat == 0 {
		n := int(loc.length) / 2
		lo := int(loc.offset)
		for i := 0; i < n && lo+2*i+2 <= len(data); i++ {
			m.loca = append(m.loca, uint32(binary.BigEndian.Uint16(data[lo+2*i:lo+2*i+2]))*2)
		}
	} else {
		n := int(loc.length) / 4
		lo := int(loc.offset)
		for i := 0; i < n && lo+4*i+4 <= len(data); i++ {
			m.loca = append(m.loca, binary.BigEndian.Uint32(data[lo+4*i:lo+4*i+4]))
		}
	}

	// glyf
	if g, ok := tables["glyf"]; ok {
		end := int(g.offset) + int(g.length)
		if end > len(data) {
			end = len(data)
		}
		if int(g.offset) <= len(data) {
			m.glyf = data[g.offset:end]
		}
	}

	// hmtx (optional)
	if h, ok := tables["hmtx"]; ok && m.numGlyphs > 0 {
		for gid := 0; gid < int(m.numGlyphs); gid++ {
			off := int(h.offset) + 4*gid
			if off+2 <= len(data) {
				m.widths = append(m.widths, binary.BigEndian.Uint16(data[off:off+2]))
			} else {
				m.widths = append(m.widths, m.widths[max0(len(m.widths)-1)])
			}
		}
	}

	// cmap (optional): scan for a unicode subtable (platform 0 or 3/encoding 1)
	if c, ok := tables["cmap"]; ok {
		parseCmapSubtables(data, int(c.offset), int(c.length), m)
	}
	return m, nil
}

// parseCmapSubtables walks the cmap directory and decodes the first
// usable unicode subtable (format 0, 4 or 6).
func parseCmapSubtables(data []byte, off, length int, m *minTTF) {
	if off+4 > len(data) {
		return
	}
	numSub := int(binary.BigEndian.Uint16(data[off+2 : off+4]))
	for i := 0; i < numSub; i++ {
		rec := off + 4 + 8*i
		if rec+8 > len(data) {
			return
		}
		platform := binary.BigEndian.Uint16(data[rec : rec+2])
		encoding := binary.BigEndian.Uint16(data[rec+2 : rec+4])
		subOff := int(binary.BigEndian.Uint32(data[rec+4 : rec+8]))
		isUnicode := platform == 0 || (platform == 3 && (encoding == 1 || encoding == 10))
		if !isUnicode {
			continue
		}
		if off+subOff+6 > len(data) {
			continue
		}
		format := binary.BigEndian.Uint16(data[off+subOff : off+subOff+2])
		switch format {
		case 0:
			parseCmapFormat0(data, off+subOff, m)
			return
		case 4:
			parseCmapFormat4(data, off+subOff, m)
			return
		case 6:
			parseCmapFormat6(data, off+subOff, m)
			return
		}
	}
}

func parseCmapFormat0(data []byte, off int, m *minTTF) {
	if off+262 > len(data) {
		return
	}
	for c := 0; c < 256; c++ {
		g := uint16(data[off+6+c])
		if g != 0 {
			m.chars[c] = g
		}
	}
}

func parseCmapFormat4(data []byte, off int, m *minTTF) {
	if off+14 > len(data) {
		return
	}
	segCountX2 := int(binary.BigEndian.Uint16(data[off+6 : off+8]))
	segCount := segCountX2 / 2
	endOff := off + 14
	startOff := endOff + segCountX2 + 2
	deltaOff := startOff + segCountX2
	rangeOff := deltaOff + segCountX2
	for s := 0; s < segCount; s++ {
		endC := int(binary.BigEndian.Uint16(data[endOff+2*s : endOff+2*s+2]))
		startC := int(binary.BigEndian.Uint16(data[startOff+2*s : startOff+2*s+2]))
		delta := int16(binary.BigEndian.Uint16(data[deltaOff+2*s : deltaOff+2*s+2]))
		rangeGlyph := binary.BigEndian.Uint16(data[rangeOff+2*s : rangeOff+2*s+2])
		if startC == 0xFFFF {
			continue
		}
		for c := startC; c <= endC; c++ {
			var g uint16
			if rangeGlyph == 0 {
				g = uint16((c + int(delta)) & 0xFFFF)
			} else {
				gi := rangeOff + 2*s + (c-startC)*2 + int(rangeGlyph)
				if gi+2 > len(data) {
					continue
				}
				g = binary.BigEndian.Uint16(data[gi : gi+2])
				if g != 0 {
					g = uint16((int(g) + int(delta)) & 0xFFFF)
				}
			}
			if g != 0 {
				m.chars[c] = g
			}
		}
	}
}

func parseCmapFormat6(data []byte, off int, m *minTTF) {
	if off+12 > len(data) {
		return
	}
	first := int(binary.BigEndian.Uint16(data[off+6 : off+8]))
	count := int(binary.BigEndian.Uint16(data[off+8 : off+10]))
	for i := 0; i < count; i++ {
		if off+10+2*i+2 <= len(data) {
			g := binary.BigEndian.Uint16(data[off+10+2*i : off+10+2*i+2])
			if g != 0 {
				m.chars[first+i] = g
			}
		}
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// glyphOutline returns the contours of one glyph (quadratic points,
// OnCurve marked), composites resolved.
func (m *minTTF) glyphOutline(gid uint16, depth int) ([][]glyphPt, error) {
	if depth > 8 {
		return nil, nil
	}
	if int(gid)+1 >= len(m.loca) {
		return nil, nil
	}
	start, end := m.loca[gid], m.loca[gid+1]
	if end <= start || int(end) > len(m.glyf) {
		return nil, nil
	}
	g := m.glyf[start:end]
	if len(g) < 10 {
		return nil, nil
	}
	numberOfContours := int16(binary.BigEndian.Uint16(g[0:2]))
	if numberOfContours >= 0 {
		return m.parseSimpleGlyf(g, int(numberOfContours))
	}
	// composite
	var out [][]glyphPt
	const (
		argWords = 0x0001
		hasScale = 0x0008
		more     = 0x0020
		hasXY    = 0x0040
		has2x2   = 0x0080
	)
	pos := 10
	for {
		if pos+4 > len(g) {
			break
		}
		flags := binary.BigEndian.Uint16(g[pos : pos+2])
		cgid := binary.BigEndian.Uint16(g[pos+2 : pos+4])
		pos += 4
		var dx, dy float64
		if flags&argWords != 0 {
			if pos+4 > len(g) {
				break
			}
			dx = float64(int16(binary.BigEndian.Uint16(g[pos : pos+2])))
			dy = float64(int16(binary.BigEndian.Uint16(g[pos+2 : pos+4])))
			pos += 4
		} else {
			if pos+2 > len(g) {
				break
			}
			dx = float64(int8(g[pos]))
			dy = float64(int8(g[pos+1]))
			pos += 2
		}
		xs, ys := 1.0, 1.0
		x01, x10 := 0.0, 0.0
		if flags&hasScale != 0 {
			if pos+2 > len(g) {
				break
			}
			xs = f2dot14(g[pos : pos+2])
			ys = xs
			pos += 2
		} else if flags&hasXY != 0 {
			if pos+4 > len(g) {
				break
			}
			xs = f2dot14(g[pos : pos+2])
			ys = f2dot14(g[pos+2 : pos+4])
			pos += 4
		} else if flags&has2x2 != 0 {
			if pos+8 > len(g) {
				break
			}
			xs = f2dot14(g[pos : pos+2])
			x01 = f2dot14(g[pos+2 : pos+4])
			x10 = f2dot14(g[pos+4 : pos+6])
			ys = f2dot14(g[pos+6 : pos+8])
			pos += 8
		}
		sub, err := m.glyphOutline(cgid, depth+1)
		if err != nil {
			continue
		}
		for _, contour := range sub {
			comp := make([]glyphPt, 0, len(contour))
			for _, p := range contour {
				comp = append(comp, glyphPt{
					x:       p.x*xs + p.y*x10 + dx,
					y:       p.x*x01 + p.y*ys + dy,
					onCurve: p.onCurve,
				})
			}
			out = append(out, comp)
		}
		if flags&more == 0 {
			break
		}
	}
	return out, nil
}

func f2dot14(b []byte) float64 {
	return float64(int16(binary.BigEndian.Uint16(b))) / 16384.0
}

type glyphPt struct {
	x, y    float64
	onCurve bool
}

// parseSimpleGlyf decodes the point stream of a simple glyph.
func (m *minTTF) parseSimpleGlyf(g []byte, numberOfContours int) ([][]glyphPt, error) {
	if numberOfContours <= 0 || len(g) < 10 {
		return nil, nil
	}
	endPts := make([]int, numberOfContours)
	pos := 10
	for i := 0; i < numberOfContours; i++ {
		if pos+2 > len(g) {
			return nil, nil
		}
		endPts[i] = int(binary.BigEndian.Uint16(g[pos : pos+2]))
		pos += 2
	}
	numPoints := endPts[numberOfContours-1] + 1
	instrLen := int(binary.BigEndian.Uint16(g[pos : pos+2]))
	pos += 2 + instrLen

	// flags
	flags := make([]byte, numPoints)
	for i := 189; i < 195 && i < len(g); i++ {
	}
	for i := 0; i < numPoints && pos < len(g); {
		f := g[pos]
		pos++
		flags[i] = f
		i++
		if f&0x08 != 0 && pos < len(g) { // repeat
			rep := int(g[pos])
			pos++
			for j := 0; j < rep && i < numPoints; j++ {
				flags[i] = f
				i++
			}
		}
	}

	xs := make([]float64, numPoints)
	x := 0.0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		if f&0x02 != 0 {
			if pos >= len(g) {
				return nil, nil
			}
			d := int(g[pos])
			if f&0x10 == 0 {
				d = -d
			}
			x += float64(d)
			pos++
		} else if f&0x10 != 0 {
			// X_SAME: delta 0 (same as previous x)
		} else {
			if pos+2 > len(g) {
				return nil, nil
			}
			x += float64(int16(binary.BigEndian.Uint16(g[pos : pos+2])))
			pos += 2
		}
		xs[i] = x
	}

	ys := make([]float64, numPoints)
	y := 0.0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		if f&0x04 != 0 {
			if pos >= len(g) {
				return nil, nil
			}
			d := int(g[pos])
			if f&0x20 == 0 {
				d = -d
			}
			y += float64(d)
			pos++
		} else if f&0x20 != 0 {
			// Y_SAME: delta 0
		} else {
			if pos+2 > len(g) {
				return nil, nil
			}
			y += float64(int16(binary.BigEndian.Uint16(g[pos : pos+2])))
			pos += 2
		}
		ys[i] = y
	}

	out := make([][]glyphPt, 0, numberOfContours)
	startPt := 0
	for _, ep := range endPts {
		n := ep + 1 - startPt
		if n <= 0 {
			startPt = ep + 1
			continue
		}
		contour := make([]glyphPt, 0, n)
		for k := 0; k < n; k++ {
			idx := startPt + k
			contour = append(contour, glyphPt{x: xs[idx], y: ys[idx], onCurve: flags[idx]&0x01 != 0})
		}
		out = append(out, contour)
		startPt = ep + 1
	}
	return out, nil
}

// charToGlyph maps a simple-font char code through the cmap (0 = notdef).
func (m *minTTF) charToGlyph(code int) uint16 {
	if g, ok := m.chars[code]; ok {
		return g
	}
	return 0
}

// glyphWidth returns the advance width of a glyph in font units.
func (m *minTTF) glyphWidth(gid uint16) float64 {
	if int(gid) < len(m.widths) {
		return float64(m.widths[gid])
	}
	return 0
}
