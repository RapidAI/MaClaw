/**
 * Spreadsheet preview parsing with no React and no app imports, so a
 * worker can run it off the UI thread. The panel re-exports the tested
 * entry points.
 */

type XLSXModule = typeof import("xlsx");

/** Cap the rendered grid so a whole-column format cannot freeze the pane. */
const MAX_PREVIEW_ROWS = 500;
const MAX_PREVIEW_COLS = 64;

export type SheetGrid = {
    rows: string[][];
    /** Merge start → span; covered cells are skipped while rendering. */
    spans: Map<string, { rowSpan: number; colSpan: number }>;
    covered: Set<string>;
    colCount: number;
    /** Rows/cols requested from the sheet after clamping to the caps. */
    shownRows: number;
    shownCols: number;
    truncatedRows: number;
    truncatedCols: number;
};

export type ParsedWorkbook = {
    names: string[];
    grids: SheetGrid[];
};

function emptySheetGrid(): SheetGrid {
    return { rows: [], spans: new Map(), covered: new Set(), colCount: 0, shownRows: 0, shownCols: 0, truncatedRows: 0, truncatedCols: 0 };
}

function declaredSheetRange(XLSX: XLSXModule, sheet: import("xlsx").WorkSheet | undefined): import("xlsx").Range | null {
    if (!sheet) return null;
    const stored = sheet["!fullref"];
    const ref = typeof stored === "string" && stored ? stored : sheet["!ref"];
    if (typeof ref !== "string" || !ref) return null;
    try {
        return XLSX.utils.decode_range(ref);
    } catch {
        return null;
    }
}

/**
 * The grid renders `cell.w`, the formatted cached value. SheetJS otherwise
 * also keeps an HTML copy of every shared string (`.h`) and the formula text
 * (`.f`). Neither is drawn.
 */
function previewReadOptions(sheetRows: number, sheets?: string[]): import("xlsx").ParsingOptions {
    const opts: import("xlsx").ParsingOptions = {
        type: "array",
        cellHTML: false,
        cellFormula: false,
        sheetRows,
    };
    if (sheets) opts.sheets = sheets;
    return opts;
}

/** Cells already hold the strings they show. The table itself is only a second copy. */
function releaseWorkbookStrings(wb: import("xlsx").WorkBook): void {
    delete (wb as { Strings?: unknown }).Strings;
}

/**
 * `.r` is the shared-string XML and `.h` is its HTML. The grid reads `.v`
 * and `.w`.
 */
function releaseCellMarkup(wb: import("xlsx").WorkBook): void {
    for (const name of wb.SheetNames) {
        const sheet = wb.Sheets[name];
        if (!sheet || Array.isArray(sheet)) continue;
        for (const key of Object.keys(sheet)) {
            if (key.startsWith("!")) continue;
            const cell = sheet[key] as { r?: unknown; h?: unknown } | undefined;
            if (!cell || typeof cell !== "object") continue;
            delete cell.r;
            delete cell.h;
        }
    }
}

let emptyWorkbookBytes: Uint8Array | null = null;

function copyByteSource(written: unknown): Uint8Array {
    if (written instanceof Uint8Array) return new Uint8Array(written);
    if (written instanceof ArrayBuffer) return new Uint8Array(written);
    return Uint8Array.from(written as ArrayLike<number>);
}

function emptyWorkbookFor(XLSX: XLSXModule): Uint8Array {
    if (emptyWorkbookBytes) return emptyWorkbookBytes;
    const blank = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(blank, { "!ref": "A1" }, "S");
    emptyWorkbookBytes = copyByteSource(XLSX.write(blank, { type: "array", bookType: "xlsx", bookSST: false }));
    return emptyWorkbookBytes;
}

/**
 * A zip parse stores the shared-string table on the parser, not only on the
 * workbook. The next zip parse is what replaces it. Leaving the spreadsheet
 * does not parse another workbook, so an empty one is parsed here.
 */
function releaseParserStringCache(XLSX: XLSXModule): void {
    try {
        XLSX.read(emptyWorkbookFor(XLSX).slice(), previewReadOptions(1));
    } catch {
        emptyWorkbookBytes = null;
    }
}

/**
 * `sheetRows` counts absolute row index, so a used range that begins on row
 * 500 or later stores no cells on the first pass. Read those sheets again,
 * and only those sheets, through the end of their own 500-row window.
 * OOXML honors `sheets`. Legacy .xls does not, and a higher cutoff on the
 * whole book would keep extra rows of every earlier sheet.
 *
 * The second parse builds a new shared-string table. The first table stays
 * on this workbook unless it is dropped before that parse starts.
 */
function rereadSheetsPastPreviewCap(XLSX: XLSXModule, bytes: Uint8Array, wb: import("xlsx").WorkBook): void {
    if (bytes.length < 2 || bytes[0] !== 0x50 || bytes[1] !== 0x4b) return;
    const names: string[] = [];
    let sheetRows = MAX_PREVIEW_ROWS;
    for (const name of wb.SheetNames) {
        const range = declaredSheetRange(XLSX, wb.Sheets[name]);
        if (!range || range.s.r < MAX_PREVIEW_ROWS) continue;
        names.push(name);
        const end = range.s.r + MAX_PREVIEW_ROWS;
        if (end > sheetRows) sheetRows = end;
    }
    if (names.length === 0) return;
    releaseWorkbookStrings(wb);
    const again = XLSX.read(bytes, previewReadOptions(sheetRows, names));
    for (const name of names) {
        const sheet = again.Sheets[name];
        if (sheet && sheet["!ref"]) wb.Sheets[name] = sheet;
    }
}

/**
 * Parse a workbook for the preview grid. `sheetRows` drops cells at or past
 * that absolute row index, so a sheet that starts at A1 does not become one
 * cell object per row. SheetJS copies the original extent onto `!fullref`
 * when it actually truncates. The parser's shared-string table is replaced
 * before this returns, after cells have copied the text they display.
 */
export function readPreviewWorkbook(XLSX: XLSXModule, bytes: Uint8Array): import("xlsx").WorkBook {
    try {
        const wb = XLSX.read(bytes, previewReadOptions(MAX_PREVIEW_ROWS));
        rereadSheetsPastPreviewCap(XLSX, bytes, wb);
        releaseCellMarkup(wb);
        releaseWorkbookStrings(wb);
        return wb;
    } finally {
        releaseParserStringCache(XLSX);
    }
}

/**
 * Build the visible grid from a worksheet. `!fullref`, when present, is the
 * sheet's original extent. `!ref` is only a fallback: after `sheetRows`,
 * SheetJS shrinks it to the last cell it stored, on both axes. The preview
 * window is the first 500 rows and 64 columns of the original extent, so the
 * truncation notice still matches that extent. Merged cells collapse into
 * row/col spans on their top-left cell.
 */
export function buildSheetGrid(XLSX: XLSXModule, sheet: import("xlsx").WorkSheet): SheetGrid {
    const empty: SheetGrid = { rows: [], spans: new Map(), covered: new Set(), colCount: 0, shownRows: 0, shownCols: 0, truncatedRows: 0, truncatedCols: 0 };
    if (!sheet || !sheet["!ref"]) return empty;
    const fullRef = sheet["!fullref"];
    const declared = XLSX.utils.decode_range(typeof fullRef === "string" && fullRef ? fullRef : sheet["!ref"]);
    const totalRows = declared.e.r - declared.s.r + 1;
    const totalCols = declared.e.c - declared.s.c + 1;
    if (totalRows <= 0 || totalCols <= 0) return empty;
    const range = {
        s: { r: declared.s.r, c: declared.s.c },
        e: {
            r: Math.min(declared.e.r, declared.s.r + MAX_PREVIEW_ROWS - 1),
            c: Math.min(declared.e.c, declared.s.c + MAX_PREVIEW_COLS - 1),
        },
    };
    const shownRows = range.e.r - range.s.r + 1;
    const shownCols = range.e.c - range.s.c + 1;
    const raw = XLSX.utils.sheet_to_json<string[]>(sheet, {
        header: 1,
        defval: "",
        raw: false,
        range,
    }) as string[][];
    const width = range.e.c - range.s.c + 1;
    const rows = raw.map((row) => {
        const next = Array.isArray(row) ? row.map((cell) => String(cell ?? "")) : [];
        while (next.length < width) next.push("");
        return next;
    });
    // Template files often declare far more formatted-but-empty rows and
    // columns than they fill; trailing blanks read as a broken preview. The
    // content extent covers merge ranges too, so a wide merged title keeps
    // its span instead of being clipped to the last text cell.
    const merges = Array.isArray(sheet["!merges"]) ? sheet["!merges"] : [];
    let lastRow = -1;
    let lastCol = -1;
    for (let r = 0; r < rows.length; r += 1) {
        const row = rows[r];
        for (let c = row.length - 1; c > lastCol; c -= 1) {
            if (row[c] !== "") {
                lastCol = Math.max(lastCol, c);
                break;
            }
        }
        if (row.some((cell) => cell !== "")) lastRow = Math.max(lastRow, r);
    }
    for (const merge of merges) {
        if (merge.e.r < range.s.r || merge.e.c < range.s.c) continue;
        if (merge.s.r > range.e.r || merge.s.c > range.e.c) continue;
        lastRow = Math.max(lastRow, Math.min(merge.e.r, range.e.r) - range.s.r);
        lastCol = Math.max(lastCol, Math.min(merge.e.c, range.e.c) - range.s.c);
    }
    if (lastRow >= 0) rows.length = lastRow + 1;
    else rows.length = 0;
    const colCount = lastCol + 1;
    for (let r = 0; r < rows.length; r += 1) rows[r] = rows[r].slice(0, colCount);
    const spans = new Map<string, { rowSpan: number; colSpan: number }>();
    const covered = new Set<string>();
    for (const merge of merges) {
        const startR = merge.s.r - range.s.r;
        const startC = merge.s.c - range.s.c;
        const endR = merge.e.r - range.s.r;
        const endC = merge.e.c - range.s.c;
        if (endR < 0 || endC < 0 || startR >= rows.length || startC >= colCount) continue;
        const clipR = Math.min(endR, rows.length - 1);
        const clipC = Math.min(endC, colCount - 1);
        if (startR >= 0 && startC >= 0) {
            spans.set(`${startR}:${startC}`, { rowSpan: clipR - startR + 1, colSpan: clipC - startC + 1 });
        }
        for (let r = Math.max(0, startR); r <= clipR; r += 1) {
            for (let c = Math.max(0, startC); c <= clipC; c += 1) {
                if (r === startR && c === startC) continue;
                covered.add(`${r}:${c}`);
            }
        }
    }
    return {
        rows,
        spans,
        covered,
        colCount,
        // Truncation counts what the caps cut off the declared range, not the
        // trailing blanks trimmed above — those are merely empty, not hidden
        // content.
        shownRows,
        shownCols,
        truncatedRows: Math.max(0, totalRows - shownRows),
        truncatedCols: Math.max(0, totalCols - shownCols),
    };
}

/** Project every sheet into the bounded grid, then the parser workbook can be dropped. */
export function previewWorkbookGrids(XLSX: XLSXModule, wb: import("xlsx").WorkBook): { names: string[]; grids: SheetGrid[] } {
    const names = wb.SheetNames.filter((name) => Boolean(wb.Sheets[name]));
    const grids = names.map((name) => {
        try {
            return buildSheetGrid(XLSX, wb.Sheets[name]);
        } catch {
            return emptySheetGrid();
        }
    });
    return { names, grids };
}

/**
 * The grid already copied the text it shows. Cell objects, the stylesheet,
 * and the content-types directory are not read again.
 */
export function releasePreviewWorkbook(wb: import("xlsx").WorkBook): void {
    const sheets = wb.Sheets;
    if (sheets) {
        for (const name of Object.keys(sheets)) delete sheets[name];
    }
    const heavy = wb as {
        Styles?: unknown;
        Themes?: unknown;
        Directory?: unknown;
        Props?: unknown;
        Custprops?: unknown;
        Workbook?: unknown;
        SSF?: unknown;
        Deps?: unknown;
        vbaraw?: unknown;
    };
    delete heavy.Styles;
    delete heavy.Themes;
    delete heavy.Directory;
    delete heavy.Props;
    delete heavy.Custprops;
    delete heavy.Workbook;
    delete heavy.SSF;
    delete heavy.Deps;
    delete heavy.vbaraw;
}

export type WireSheetGrid = {
    rows: string[][];
    spans: Array<[string, { rowSpan: number; colSpan: number }]>;
    covered: string[];
    colCount: number;
    shownRows: number;
    shownCols: number;
    truncatedRows: number;
    truncatedCols: number;
};

/** Worker messages cannot carry Map or Set. The grid is rebuilt on the UI thread. */
export function wireSheetGrid(grid: SheetGrid): WireSheetGrid {
    return {
        rows: grid.rows,
        spans: Array.from(grid.spans.entries()),
        covered: Array.from(grid.covered),
        colCount: grid.colCount,
        shownRows: grid.shownRows,
        shownCols: grid.shownCols,
        truncatedRows: grid.truncatedRows,
        truncatedCols: grid.truncatedCols,
    };
}

export function sheetGridFromWire(wire: WireSheetGrid): SheetGrid {
    const spans = new Map<string, { rowSpan: number; colSpan: number }>();
    for (const entry of wire.spans || []) {
        if (!entry || entry.length < 2 || !entry[1]) continue;
        spans.set(String(entry[0]), {
            rowSpan: Number(entry[1].rowSpan) || 1,
            colSpan: Number(entry[1].colSpan) || 1,
        });
    }
    const rows = Array.isArray(wire.rows)
        ? wire.rows.map((row) => (Array.isArray(row) ? row.map((cell) => String(cell ?? "")) : []))
        : [];
    return {
        rows,
        spans,
        covered: new Set((wire.covered || []).map((key) => String(key))),
        colCount: Number(wire.colCount) || 0,
        shownRows: Number(wire.shownRows) || 0,
        shownCols: Number(wire.shownCols) || 0,
        truncatedRows: Number(wire.truncatedRows) || 0,
        truncatedCols: Number(wire.truncatedCols) || 0,
    };
}
