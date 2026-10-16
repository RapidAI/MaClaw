import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { XlsxPreviewPanel, buildSheetGrid, isXlsxFileName } from "../XlsxPreviewPanel";
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

    it("returns an empty grid for a sheet without a range", async () => {
        const XLSX = await import("xlsx");
        const grid = buildSheetGrid(XLSX, {} as import("xlsx").WorkSheet);
        expect(grid.rows).toHaveLength(0);
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
});
