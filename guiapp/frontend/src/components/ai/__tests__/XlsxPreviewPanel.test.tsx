import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { XlsxPreviewPanel, buildSheetGrid, isXlsxFileName, previewWorkbookGrids, readPreviewWorkbook, releasePreviewWorkbook } from "../XlsxPreviewPanel";
import { sheetGridFromWire, wireSheetGrid } from "../xlsxPreviewParse";
import { PREVIEW_BAR_DELAY_MS } from "../PreviewProgressBar";
import type { CodePreviewTheme } from "../FileTabBar";
import { PreviewTaskResultFile } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    PreviewTaskResultFile: vi.fn(),
}));

const preview = vi.mocked(PreviewTaskResultFile);

const theme = {
    bg: "#ffffff",
    text: "#0f172a",
    tabBg: "#f8fafc",
    tabActiveBg: "#dbeafe",
    border: "#e5e7eb",
    textMuted: "#6b7280",
    tabActiveText: "#2563eb",
    diffDeleteText: "#dc2626",
    lineNumBg: "#f1f5f9",
} as unknown as CodePreviewTheme;

const VALID_URL = "/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

async function workbookBytes(build: (XLSX: typeof import("xlsx"), wb: import("xlsx").WorkBook) => void): Promise<ArrayBuffer> {
    const XLSX = await import("xlsx");
    const wb = XLSX.utils.book_new();
    build(XLSX, wb);
    return XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
}

function mockPreviewWithBytes(bytes: ArrayBuffer) {
    preview.mockResolvedValue({ path: "D:/t/book.xlsx", kind: "xlsx", preview_url: VALID_URL } as never);
    vi.stubGlobal("fetch", vi.fn(async () => ({
        ok: true,
        arrayBuffer: async () => bytes,
    })));
}

beforeEach(() => {
    preview.mockReset();
    vi.unstubAllGlobals();
});

describe("isXlsxFileName", () => {
    it("matches the spreadsheet family and rejects other names", () => {
        expect(isXlsxFileName("book.xlsx")).toBe(true);
        expect(isXlsxFileName("old.XLS")).toBe(true);
        expect(isXlsxFileName("表.et")).toBe(true);
        expect(isXlsxFileName("data.ods")).toBe(true);
        expect(isXlsxFileName("brief.docx")).toBe(false);
        expect(isXlsxFileName("deck.pptx.txt")).toBe(false);
    });
});

describe("buildSheetGrid", () => {
    it("clamps the grid to the preview caps and reports truncation", async () => {
        const XLSX = await import("xlsx");
        const rows: unknown[][] = Array.from({ length: 600 }, (_, r) => [`r${r}`, r]);
        const ws = XLSX.utils.aoa_to_sheet(rows);
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(500);
        expect(grid.shownRows).toBe(500);
        expect(grid.colCount).toBe(2);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
    });

    it("trims trailing formatted-but-empty rows and columns", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([
            ["姓名", "金额"],
            [],
            ["马勇", "1200"],
            [],
            [],
        ]);
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 9 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(3);
        expect(grid.colCount).toBe(2);
        expect(grid.rows[1]).toEqual(["", ""]);
        // The cap did not cut anything (10 declared cols < 64); the missing
        // trailing columns were empty, not truncated.
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(0);
    });

    it("returns an empty grid for a sheet without content", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([[]]);
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 99, c: 5 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(0);
        expect(grid.colCount).toBe(0);
    });

    it("keeps merged cells as spans and skips covered cells", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([["A", "B", "C"], ["x", "", "z"]]);
        ws["!merges"] = [{ s: { r: 0, c: 0 }, e: { r: 0, c: 1 } }];
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.spans.get("0:0")).toEqual({ rowSpan: 1, colSpan: 2 });
        expect(grid.covered.has("0:1")).toBe(true);
        expect(grid.covered.has("0:0")).toBe(false);
    });

    it("round-trips merge spans through a worker message", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([["A", "B"], ["x", "y"]]);
        ws["!merges"] = [{ s: { r: 0, c: 0 }, e: { r: 0, c: 1 } }];
        const grid = buildSheetGrid(XLSX, ws);
        const restored = sheetGridFromWire(wireSheetGrid(grid));
        expect(restored.rows).toEqual(grid.rows);
        expect(restored.spans.get("0:0")).toEqual({ rowSpan: 1, colSpan: 2 });
        expect(restored.covered.has("0:1")).toBe(true);
        expect(restored.shownCols).toBe(grid.shownCols);
        expect(restored.truncatedRows).toBe(grid.truncatedRows);
    });

    it("clips merges that cross the preview caps", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet(Array.from({ length: 600 }, (_, r) => [`r${r}`]));
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        ws["!merges"] = [
            { s: { r: 0, c: 0 }, e: { r: 600, c: 0 } },
            { s: { r: 1, c: 1 }, e: { r: 1, c: 90 } },
            { s: { r: 499, c: 2 }, e: { r: 520, c: 2 } },
        ];
        const grid = buildSheetGrid(XLSX, ws);
        // Row cap is 500, col cap is 64: spans clip to the rendered grid.
        expect(grid.spans.get("0:0")).toEqual({ rowSpan: 500, colSpan: 1 });
        expect(grid.spans.get("1:1")).toEqual({ rowSpan: 1, colSpan: 63 });
        expect(grid.spans.get("499:2")).toEqual({ rowSpan: 1, colSpan: 1 });
    });

    it("reports truncation from !fullref when !ref is only the kept window", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet(Array.from({ length: 500 }, (_, r) => [`r${r}`, r]));
        // sheetRows stores the original extent on !fullref and may shrink !ref
        // to the columns present in the rows it kept.
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 499, c: 1 } });
        ws["!fullref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(500);
        expect(grid.shownRows).toBe(500);
        expect(grid.colCount).toBe(2);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
        expect(grid.rows[0][0]).toBe("r0");
        expect(grid.rows[499][0]).toBe("r499");
    });

    it("keeps the cap window when !ref was shrunk to the last stored cell", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([
            ["姓名", "金额"],
            ["马勇", 1200],
            ["张三", 30],
        ]);
        // Dimension still covers 600×80, but the reader only stored three rows.
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 2, c: 1 } });
        ws["!fullref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(3);
        expect(grid.rows[1][0]).toBe("马勇");
        expect(grid.colCount).toBe(2);
        expect(grid.shownRows).toBe(500);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
    });

    it("starts the cap window at the declared origin, not at A1", async () => {
        const XLSX = await import("xlsx");
        const origin = XLSX.utils.decode_cell("A11");
        const placed = XLSX.utils.aoa_to_sheet([["标题", 1], ["下一行", 2]]);
        const ws: import("xlsx").WorkSheet = {};
        for (const address of Object.keys(placed)) {
            if (address.startsWith("!")) continue;
            const cell = XLSX.utils.decode_cell(address);
            ws[XLSX.utils.encode_cell({ r: cell.r + origin.r, c: cell.c })] = placed[address];
        }
        // Used range begins on Excel row 11. The stored !ref ends at that block.
        ws["!ref"] = XLSX.utils.encode_range({ s: origin, e: { r: origin.r + 1, c: 1 } });
        ws["!fullref"] = XLSX.utils.encode_range({ s: origin, e: { r: origin.r + 599, c: 79 } });
        const grid = buildSheetGrid(XLSX, ws);
        expect(grid.rows).toHaveLength(2);
        expect(grid.rows[0][0]).toBe("标题");
        expect(grid.shownRows).toBe(500);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
    });

    it("returns an empty grid for a sheet without a range", async () => {
        const XLSX = await import("xlsx");
        const grid = buildSheetGrid(XLSX, {} as import("xlsx").WorkSheet);
        expect(grid.rows).toHaveLength(0);
    });
});

describe("readPreviewWorkbook", () => {
    it("keeps the first preview rows and the original extent", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet(Array.from({ length: 600 }, (_, r) => [`r${r}`, r]));
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, "明细");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        const sheet = parsed.Sheets[parsed.SheetNames[0]];
        const grid = buildSheetGrid(XLSX, sheet);
        expect(grid.rows).toHaveLength(500);
        expect(grid.shownRows).toBe(500);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
        expect(grid.colCount).toBe(2);
        expect(grid.rows[0][0]).toBe("r0");
        expect(grid.rows[499][0]).toBe("r499");
    });

    it("reports the cap when the dimension is longer than the stored cells", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([
            ["姓名", "金额"],
            ["马勇", 1200],
            ["张三", 30],
        ]);
        ws["!ref"] = XLSX.utils.encode_range({ s: { r: 0, c: 0 }, e: { r: 599, c: 79 } });
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, "明细");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        const grid = buildSheetGrid(XLSX, parsed.Sheets[parsed.SheetNames[0]]);
        expect(grid.rows).toHaveLength(3);
        expect(grid.rows[1][0]).toBe("马勇");
        expect(grid.shownRows).toBe(500);
        expect(grid.shownCols).toBe(64);
        expect(grid.truncatedRows).toBe(100);
        expect(grid.truncatedCols).toBe(16);
    });

    it("keeps a sheet that starts past the absolute row cap", async () => {
        const XLSX = await import("xlsx");
        const early = XLSX.utils.aoa_to_sheet(Array.from({ length: 600 }, (_, r) => [`e${r}`]));
        const late: import("xlsx").WorkSheet = {};
        // Two cells, so the file dimension has a colon. A one-cell dimension
        // is ref="A601", which SheetJS does not keep, and the capped read
        // then has no range to recover.
        late[XLSX.utils.encode_cell({ r: 600, c: 0 })] = { t: "s", v: "底部" };
        late[XLSX.utils.encode_cell({ r: 601, c: 0 })] = { t: "s", v: "下一行" };
        late["!ref"] = XLSX.utils.encode_range({ s: { r: 600, c: 0 }, e: { r: 601, c: 0 } });
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, early, "前面");
        XLSX.utils.book_append_sheet(wb, late, "后面");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        const earlyGrid = buildSheetGrid(XLSX, parsed.Sheets["前面"]);
        expect(earlyGrid.rows).toHaveLength(500);
        expect(earlyGrid.rows[499][0]).toBe("e499");
        expect(earlyGrid.truncatedRows).toBe(100);
        const lateGrid = buildSheetGrid(XLSX, parsed.Sheets["后面"]);
        expect(lateGrid.rows.map((row) => row[0])).toEqual(["底部", "下一行"]);
        expect(lateGrid.truncatedRows).toBe(0);
        // The first parse's string table must not stay beside the second one.
        expect((parsed as { Strings?: unknown }).Strings).toBeUndefined();
    });

    it("shows cached formula values and formatted text without formula or html copies", async () => {
        const XLSX = await import("xlsx");
        const ws: import("xlsx").WorkSheet = {
            A1: { t: "s", v: "金额" },
            B1: { t: "n", v: 1200.5, z: "#,##0.00" },
            C1: { t: "n", v: 2401, f: "B1*2" },
            D1: { t: "n", v: 44927, z: "yyyy-mm-dd" },
            "!ref": "A1:D1",
        };
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, "明细");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx", bookSST: true }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        const sheet = parsed.Sheets[parsed.SheetNames[0]];
        const grid = buildSheetGrid(XLSX, sheet);
        expect(grid.rows[0]).toEqual(["金额", "1,200.50", "2401", "2023-01-01"]);
        expect(sheet["A1"]?.h).toBeUndefined();
        expect(sheet["A1"]?.r).toBeUndefined();
        expect(sheet["A1"]?.v).toBe("金额");
        expect(sheet["C1"]?.f).toBeUndefined();
        expect(sheet["C1"]?.v).toBe(2401);
        expect((parsed as { Strings?: unknown }).Strings).toBeUndefined();
    });

    it("replaces the parser string table after the cells have their text", async () => {
        const real = await import("xlsx");
        const sizes: number[] = [];
        const XLSX = Object.assign({}, real, {
            read(data: Parameters<typeof real.read>[0], opts?: Parameters<typeof real.read>[1]) {
                const result = real.read(data, opts);
                const strings = (result as { Strings?: unknown }).Strings;
                sizes.push(Array.isArray(strings) ? strings.length : -1);
                return result;
            },
        }) as typeof real;
        const ws: import("xlsx").WorkSheet = {
            A1: { t: "s", v: "金额" },
            "!ref": "A1",
        };
        const wb = real.utils.book_new();
        real.utils.book_append_sheet(wb, ws, "明细");
        const bytes = real.write(wb, { type: "array", bookType: "xlsx", bookSST: true }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        expect(sizes.some((size) => size > 0)).toBe(true);
        expect(sizes[sizes.length - 1]).toBe(0);
        expect(buildSheetGrid(XLSX, parsed.Sheets[parsed.SheetNames[0]]).rows[0][0]).toBe("金额");
    });

    it("drops the legacy string table after the cells have their text", async () => {
        const XLSX = await import("xlsx");
        const ws = XLSX.utils.aoa_to_sheet([["金额", 1200]]);
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, "明细");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xls" }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        const grid = buildSheetGrid(XLSX, parsed.Sheets[parsed.SheetNames[0]]);
        expect(grid.rows[0][0]).toBe("金额");
        expect(grid.rows[0][1]).toBe("1200");
        expect((parsed as { Strings?: unknown }).Strings).toBeUndefined();
    });

    it("keeps the shown grid after the parsed workbook is released", async () => {
        const XLSX = await import("xlsx");
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["姓名", "金额"], ["马勇", "1200"]]), "发放明细表");
        XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["签到"], ["张三"]]), "专家签到表");
        const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
        const parsed = readPreviewWorkbook(XLSX, new Uint8Array(bytes));
        (parsed as { Styles?: unknown }).Styles = { sentinel: true };
        const preview = previewWorkbookGrids(XLSX, parsed);
        releasePreviewWorkbook(parsed);
        expect(preview.names).toEqual(["发放明细表", "专家签到表"]);
        expect(preview.grids[0].rows[1][0]).toBe("马勇");
        expect(preview.grids[1].rows[1][0]).toBe("张三");
        expect(parsed.Sheets["发放明细表"]).toBeUndefined();
        expect((parsed as { Styles?: unknown }).Styles).toBeUndefined();
    });
});

describe("XlsxPreviewPanel", () => {
    it("renders the first sheet as a grid and switches sheets from the tabs", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["姓名", "金额"], ["马勇", "1200"]]), "发放明细表");
            XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["签到"], ["张三"]]), "专家签到表");
        });
        mockPreviewWithBytes(bytes);
        const { getByTestId, getAllByTestId, queryByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-panel")).toBeTruthy());
        const table = getByTestId("xlsx-preview-table");
        expect(table.textContent).toContain("马勇");
        expect(table.textContent).toContain("1200");
        expect(table.textContent).not.toContain("张三");

        const tabs = getAllByTestId("xlsx-preview-sheet-tab");
        expect(tabs).toHaveLength(2);
        expect(tabs[0].getAttribute("data-active")).toBe("true");
        fireEvent.click(tabs[1]);
        await waitFor(() => expect(getByTestId("xlsx-preview-table").textContent).toContain("张三"));
        expect(queryByTestId("xlsx-preview-fallback")).toBeNull();
        expect(queryByTestId("xlsx-preview-error")).toBeNull();
    });

    it("keeps the open sheet when the language or the extract text changes", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            const sheet = XLSX.utils.aoa_to_sheet(Array.from({ length: 600 }, (_, r) => [`r${r}`]));
            XLSX.utils.book_append_sheet(wb, sheet, "明细");
            XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["签到"], ["张三"]]), "专家签到表");
        });
        mockPreviewWithBytes(bytes);
        const view = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" fallbackContent="" />,
        );
        await waitFor(() => expect(view.getByTestId("xlsx-preview-truncated").textContent).toContain("仅显示前 500 行"));
        fireEvent.click(view.getAllByTestId("xlsx-preview-sheet-tab")[1]);
        await waitFor(() => expect(view.getByTestId("xlsx-preview-table").textContent).toContain("张三"));
        const calls = preview.mock.calls.length;
        view.rerender(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="en" fallbackContent="后来的提取文本" />,
        );
        await act(async () => {});
        expect(preview.mock.calls.length).toBe(calls);
        expect(view.getByTestId("xlsx-preview-table").textContent).toContain("张三");
        expect(view.queryByTestId("xlsx-preview-fallback")).toBeNull();
        fireEvent.click(view.getAllByTestId("xlsx-preview-sheet-tab")[0]);
        await waitFor(() => expect(view.getByTestId("xlsx-preview-truncated").textContent).toContain("first 500 rows"));
    });

    it("right-aligns numbers and grouped numbers, keeps malformed groups as text", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["1,234", "3,4,5", "12,345,678", "备注"]]), "明细");
        });
        mockPreviewWithBytes(bytes);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-table")).toBeTruthy());
        const cells = getByTestId("xlsx-preview-table").querySelectorAll("td");
        expect(cells[0].style.textAlign).toBe("right");
        expect(cells[1].style.textAlign).toBe("left");
        expect(cells[2].style.textAlign).toBe("right");
        expect(cells[3].style.textAlign).toBe("left");
    });

    it("renders merged headers with colSpan", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            const ws = XLSX.utils.aoa_to_sheet([["季度", "", "合计"], ["一", "二", "9"]]);
            ws["!merges"] = [{ s: { r: 0, c: 0 }, e: { r: 0, c: 1 } }];
            XLSX.utils.book_append_sheet(wb, ws, "汇总");
        });
        mockPreviewWithBytes(bytes);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-panel")).toBeTruthy());
        const row = getByTestId("xlsx-preview-table").querySelector("tr");
        expect(row?.children).toHaveLength(2);
        expect(row?.children[0].getAttribute("colspan")).toBe("2");
    });

    it("falls back to the text extract when the workbook cannot be parsed", async () => {
        // A truncated ZIP container makes SheetJS throw; a zero-filled buffer
        // would parse as an empty sheet instead.
        const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04, 1, 2, 3, 4, 5, 6, 7, 8]).buffer;
        mockPreviewWithBytes(bytes);
        const { getByTestId, queryByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" fallbackContent="发放明细表 第1行" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-fallback")).toBeTruthy());
        expect(getByTestId("xlsx-preview-fallback").textContent).toContain("发放明细表");
        expect(queryByTestId("xlsx-preview-error")).toBeNull();
    });

    it("uses the server extract when parsing fails and the tab has other text", async () => {
        const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04, 1, 2, 3, 4, 5, 6, 7, 8]).buffer;
        preview.mockResolvedValue({
            path: "D:/t/book.xlsx",
            kind: "xlsx",
            preview_url: VALID_URL,
            content: "服务端提取的表格",
        } as never);
        vi.stubGlobal("fetch", vi.fn(async () => ({
            ok: true,
            arrayBuffer: async () => bytes,
        })));
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" fallbackContent="标签里的其他文字" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-fallback")).toBeTruthy());
        expect(getByTestId("xlsx-preview-fallback").textContent).toContain("服务端提取的表格");
        expect(getByTestId("xlsx-preview-fallback").textContent).not.toContain("标签里的其他文字");
    });

    it("shows an error when parsing fails and no extract is available", async () => {
        const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04, 1, 2, 3, 4, 5, 6, 7, 8]).buffer;
        mockPreviewWithBytes(bytes);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-error")).toBeTruthy());
    });

    it("falls back to the extract when the host does not serve a preview lease", async () => {
        preview.mockResolvedValue({ path: "D:/t/book.xlsx", kind: "xlsx", content: "提取文本" } as never);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" fallbackContent="提取文本" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-fallback")).toBeTruthy());
        expect(getByTestId("xlsx-preview-fallback").textContent).toContain("提取文本");
    });

    it("says the grid is the first rows when the sheet is longer", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            XLSX.utils.book_append_sheet(
                wb,
                XLSX.utils.aoa_to_sheet(Array.from({ length: 600 }, (_, r) => [`r${r}`])),
                "明细",
            );
        });
        mockPreviewWithBytes(bytes);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-truncated")).toBeTruthy());
        expect(getByTestId("xlsx-preview-truncated").textContent).toContain("仅显示前 500 行");
        const bodyRows = getByTestId("xlsx-preview-table").querySelectorAll("tr");
        expect(bodyRows).toHaveLength(500);
        expect(bodyRows[0].textContent).toContain("r0");
        expect(bodyRows[499].textContent).toContain("r499");
    });

    it("shows an empty note for a workbook whose sheets have no cells", async () => {
        const bytes = await workbookBytes((XLSX, wb) => {
            XLSX.utils.book_append_sheet(wb, {}, "空白");
        });
        mockPreviewWithBytes(bytes);
        const { getByTestId } = render(
            <XlsxPreviewPanel absPath="D:/t/book.xlsx" theme={theme} lang="zh-Hans" />,
        );
        await waitFor(() => expect(getByTestId("xlsx-preview-empty")).toBeTruthy());
    });

    it("shows a byte progress bar when the workbook is slow to arrive", async () => {
        preview.mockResolvedValue({ path: "D:/t/book.xlsx", kind: "xlsx", preview_url: VALID_URL } as never);
        let release = () => {};
        const gate = new Promise<void>((resolve) => {
            release = resolve;
        });
        let reads = 0;
        vi.stubGlobal("fetch", vi.fn(async () => ({
            ok: true,
            headers: { get: (name: string) => (name.toLowerCase() === "content-length" ? "100" : null) },
            body: {
                getReader: () => ({
                    read: async () => {
                        reads += 1;
                        if (reads === 1) return { done: false, value: new Uint8Array(40) };
                        await gate;
                        return { done: true, value: undefined };
                    },
                    cancel: async () => {},
                }),
            },
            arrayBuffer: async () => new ArrayBuffer(0),
        })));
        const view = render(<XlsxPreviewPanel absPath="D:/t/slow.xlsx" theme={theme} lang="zh-Hans" />);
        expect(view.getByTestId("xlsx-preview-loading")).toBeTruthy();
        expect(view.queryByTestId("preview-progress-bar")).toBeNull();
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, PREVIEW_BAR_DELAY_MS + 80));
        });
        const bar = view.getByTestId("preview-progress-bar");
        expect(bar.getAttribute("data-mode")).toBe("determinate");
        expect(bar.getAttribute("aria-valuenow")).toBe("40");
        expect(view.getByRole("status").textContent).toBe("正在读取表格…");
        expect(view.getByRole("status").contains(bar)).toBe(false);
        expect(view.getByTestId("xlsx-preview-loading").textContent).toContain("正在读取表格");
        expect(view.getByTestId("xlsx-preview-loading").textContent).toContain("40%");
        view.unmount();
        release();
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 0));
        });
    });
});
