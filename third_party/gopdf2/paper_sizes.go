package gopdf

import "strings"

// Extended paper size definitions (ISO A-series, B-series, and common US sizes).
// All values are in points (1 point = 1/72 inch).

// ISO A-series (portrait)
var (
	PageSizeA6  = &Rect{W: 298, H: 420, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA7  = &Rect{W: 210, H: 298, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA8  = &Rect{W: 148, H: 210, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA9  = &Rect{W: 105, H: 148, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA10 = &Rect{W: 74, H: 105, unitOverride: defaultUnitConfig{Unit: UnitPT}}
)

// ISO A-series landscape
var (
	PageSizeA0Landscape  = &Rect{W: 3371, H: 2384, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA1Landscape  = &Rect{W: 2384, H: 1685, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA2Landscape  = &Rect{W: 1684, H: 1190, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA5Landscape  = &Rect{W: 595, H: 420, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA6Landscape  = &Rect{W: 420, H: 298, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA7Landscape  = &Rect{W: 298, H: 210, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA8Landscape  = &Rect{W: 210, H: 148, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA9Landscape  = &Rect{W: 148, H: 105, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeA10Landscape = &Rect{W: 105, H: 74, unitOverride: defaultUnitConfig{Unit: UnitPT}}
)

// ISO B-series
var (
	PageSizeB0 = &Rect{W: 2835, H: 4008, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB1 = &Rect{W: 2004, H: 2835, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB2 = &Rect{W: 1417, H: 2004, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB3 = &Rect{W: 1001, H: 1417, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	// B4 and B5 already defined in page_sizes.go
	PageSizeB6  = &Rect{W: 363, H: 516, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB7  = &Rect{W: 258, H: 363, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB8  = &Rect{W: 181, H: 258, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB9  = &Rect{W: 127, H: 181, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeB10 = &Rect{W: 91, H: 127, unitOverride: defaultUnitConfig{Unit: UnitPT}}
)

// US sizes
var (
	PageSizeLetterLandscape = &Rect{W: 792, H: 612, unitOverride: defaultUnitConfig{Unit: UnitPT}}
	PageSizeLegalLandscape  = &Rect{W: 1008, H: 612, unitOverride: defaultUnitConfig{Unit: UnitPT}}
)

// paperSizeMap maps lowercase paper size names to their Rect definitions.
var paperSizeMap = map[string]*Rect{
	// ISO A-series portrait
	"a0": PageSizeA0, "a1": PageSizeA1, "a2": PageSizeA2,
	"a3": PageSizeA3, "a4": PageSizeA4, "a5": PageSizeA5,
	"a6": PageSizeA6, "a7": PageSizeA7, "a8": PageSizeA8,
	"a9": PageSizeA9, "a10": PageSizeA10,

	// ISO A-series landscape
	"a0-l": PageSizeA0Landscape, "a1-l": PageSizeA1Landscape,
	"a2-l": PageSizeA2Landscape, "a3-l": PageSizeA3Landscape,
	"a4-l": PageSizeA4Landscape, "a5-l": PageSizeA5Landscape,
	"a6-l": PageSizeA6Landscape, "a7-l": PageSizeA7Landscape,
	"a8-l": PageSizeA8Landscape, "a9-l": PageSizeA9Landscape,
	"a10-l": PageSizeA10Landscape,

	// ISO B-series
	"b0": PageSizeB0, "b1": PageSizeB1, "b2": PageSizeB2,
	"b3": PageSizeB3, "b4": PageSizeB4, "b5": PageSizeB5,
	"b6": PageSizeB6, "b7": PageSizeB7, "b8": PageSizeB8,
	"b9": PageSizeB9, "b10": PageSizeB10,

	// US sizes
	"letter": PageSizeLetter, "letter-l": PageSizeLetterLandscape,
	"legal": PageSizeLegal, "legal-l": PageSizeLegalLandscape,
	"tabloid": PageSizeTabloid, "ledger": PageSizeLedger,
	"statement": PageSizeStatement, "executive": PageSizeExecutive,
	"folio": PageSizeFolio, "quarto": PageSizeQuarto,
}

// PaperSize returns the page size Rect for a given paper name.
// Supported names: a0–a10, b0–b10, letter, legal, tabloid, ledger,
// statement, executive, folio, quarto.
// Append "-l" for landscape (e.g. "a4-l", "letter-l").
// The name is case-insensitive. Returns nil if the name is not recognized.
func PaperSize(name string) *Rect {
	r, ok := paperSizeMap[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	// Return a copy so callers can't mutate the global.
	cp := *r
	return &cp
}

// PaperSizeNames returns all supported paper size names.
func PaperSizeNames() []string {
	names := make([]string, 0, len(paperSizeMap))
	for k := range paperSizeMap {
		names = append(names, k)
	}
	return names
}
