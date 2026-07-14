import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
import { CodingConflictSidePanel } from "../CodingConflictSidePanel";
import type { Theme } from "../aiAssistantPanelTheme";

const theme = {
    bg: "#fff",
    text: "#111",
    textMuted: "#64748b",
    titleBarBg: "#f8fafc",
    titleBarBorder: "#e2e8f0",
    divider: "#e2e8f0",
    fieldBorder: "#d8dee8",
    fieldBg: "#f8fafc",
    btnColor: "#2f5f98",
    headingColor: "#1e4a7a",
    promptColor: "#64748b",
} as Theme;

afterEach(() => cleanup());

describe("CodingConflictSidePanel", () => {
    const baseProps = () => {
        const onClose = vi.fn();
        const tripleScrollRefs = { current: { base: null, main: null, theirs: null } };
        return {
            onClose,
            props: {
                lang: "en" as const,
                theme,
                splitRatio: 0.55,
                startPreviewResize: () => {},
                busy: false,
                conflicts: [{ id: "c1", step_index: 2, path: "src/a.ts" }],
                activeId: "c1",
                diffs: [{ path: "src/a.ts", status: "modified", three_way: "@@ -1 +1 @@" }],
                selected: [] as string[],
                focusFile: "",
                preview: null,
                previewSide: "main" as const,
                editDraft: "",
                triple: null,
                conflictLog: ["adopted src/a.ts"],
                onClose,
                onOpenConflict: vi.fn(),
                onDiscardAll: () => {},
                onDiscard: () => {},
                onResolveBatch: () => {},
                onToggleFile: () => {},
                onSelectAll: () => {},
                onClearSelection: () => {},
                onResolveSelected: () => {},
                onAdoptFile: () => {},
                onKeepMainFile: () => {},
                onAdoptBaseFile: () => {},
                onOpenFile: () => {},
                onLoadPreview: () => {},
                onApplyPreviewSide: () => {},
                onWriteEdit: () => {},
                onEditDraftChange: () => {},
                onExportLog: () => {},
                onClearLog: () => {},
                syncTripleScroll: () => {},
                tripleScrollRefs,
            },
        };
    };

    it("renders conflicts and closes from header", () => {
        const { onClose, props } = baseProps();
        const { getByTestId } = render(<CodingConflictSidePanel {...props} />);
        expect(getByTestId("coding-conflict-side-panel")).toBeTruthy();
        expect(getByTestId("coding-conflict-panel")).toBeTruthy();
        expect(getByTestId("coding-conflict-three-way").textContent || "").toContain("@@");
        fireEvent.click(getByTestId("coding-conflict-side-close"));
        expect(onClose).toHaveBeenCalled();
    });

    it("closes on Escape when focus is not in a form field", () => {
        const { onClose, props } = baseProps();
        render(<CodingConflictSidePanel {...props} />);
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onClose).toHaveBeenCalled();
    });

    it("does not close on Escape while editing draft", () => {
        const { onClose, props } = baseProps();
        const { getByTestId } = render(
            <CodingConflictSidePanel
                {...props}
                focusFile="src/a.ts"
                preview={{ side: "main", path: "src/a.ts", content: "x" }}
                editDraft="x"
            />,
        );
        const field = getByTestId("coding-conflict-edit-draft") as HTMLTextAreaElement;
        field.focus();
        fireEvent.keyDown(field, { key: "Escape" });
        expect(onClose).not.toHaveBeenCalled();
    });

    it("embedded mode closes on Escape while the CF tab is visible", () => {
        const { onClose, props } = baseProps();
        render(
            <div>
                <button type="button" data-testid="outside">out</button>
                <CodingConflictSidePanel {...props} embedded />
            </div>,
        );
        fireEvent.keyDown(document.querySelector("[data-testid='outside']")!, { key: "Escape" });
        expect(onClose).toHaveBeenCalled();
    });

    it("embedded mode ignores Escape while mounted under a hidden CF slot", () => {
        const { onClose, props } = baseProps();
        render(
            <div aria-hidden="true" data-testid="hidden-slot">
                <CodingConflictSidePanel {...props} embedded />
            </div>,
        );
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onClose).not.toHaveBeenCalled();
    });

    it("shows loading state when active but diffs empty", () => {
        const { props } = baseProps();
        const { getByTestId, queryByTestId } = render(
            <CodingConflictSidePanel {...props} diffs={[]} />,
        );
        expect(getByTestId("coding-conflict-side-loading")).toBeTruthy();
        expect(queryByTestId("coding-conflict-panel")).toBeNull();
    });

    it("shows resolution progress from peak total", () => {
        const { props } = baseProps();
        const { getByTestId } = render(
            <CodingConflictSidePanel {...props} progressTotal={3} conflicts={[{ id: "c1" }, { id: "c2" }]} />,
        );
        expect(getByTestId("coding-conflict-progress")).toBeTruthy();
        expect(getByTestId("coding-conflict-progress-label").textContent || "").toMatch(/1 of 3|1\/3/);
        expect((getByTestId("coding-conflict-progress-bar") as HTMLElement).style.width).toBe("33%");
    });

    it("syntax-highlights triple pane code for known extensions", () => {
        const { props } = baseProps();
        const { getByTestId } = render(
            <CodingConflictSidePanel
                {...props}
                focusFile="src/a.ts"
                preview={{ side: "main", path: "src/a.ts", content: "const x = 1;" }}
                triple={{
                    main: { content: "const x = 1;" },
                    theirs: { content: "const y = 2;" },
                    base: { content: "const z = 0;" },
                }}
            />,
        );
        // First code cell should contain tokenized spans (keyword color on "const").
        const codeCell = getByTestId("coding-conflict-triple-code-base");
        expect(codeCell.querySelectorAll("span").length).toBeGreaterThan(0);
    });
});
