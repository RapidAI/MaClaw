import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
import {
    buildCodingBannerChrome,
    CODING_BANNER_LOCAL_DARK_ACCENT,
    CODING_BANNER_LOCAL_DARK_ACCENT_STRONG,
    codingStepStatusColor,
    CodingWorkbenchControlPanel,
    deriveChipStatus,
} from "../CodingWorkbenchControlPanel";
import { isFormFieldTarget } from "../codingUiGuards";

const chrome = {
    accent: "#2f5f98",
    accentStrong: "#1e4a7a",
    surface: "#f5f8fc",
    border: "#d8dee8",
    chipActiveBg: "rgba(47,95,152,0.1)",
    chipIdleBg: "#fff",
    chipIdleBorder: "#d8dee8",
    iconWellBg: "rgba(47,95,152,0.08)",
    insetBg: "#fff",
    muted: "#64748b",
    btnPrimaryBg: "#2f5f98",
    btnPrimaryFg: "#fff",
};

const darkThemeStub = {
    btnColor: "#2f5f98",
    titleBarBg: "#1a1d24",
    bg: "#12141a",
    titleBarBorder: "#2a2f3a",
    fieldBorder: "rgba(148,163,184,0.35)",
    fieldBg: "#1e222b",
    textMuted: "#a8b8c8",
    promptColor: "#a8b8c8",
};

afterEach(() => cleanup());

describe("buildCodingBannerChrome", () => {
    it("uses muted sage (not neon green) for dark local coding panel", () => {
        const c = buildCodingBannerChrome({ isDark: true, remote: false, theme: darkThemeStub });
        expect(c.accent).toBe(CODING_BANNER_LOCAL_DARK_ACCENT);
        expect(c.accentStrong).toBe(CODING_BANNER_LOCAL_DARK_ACCENT_STRONG);
        expect(c.accent).not.toMatch(/#4ade80/i);
        expect(c.accentStrong).not.toMatch(/#86efac/i);
        // Light wash only — surface mix uses 6% accent, not a green slab.
        expect(c.surface).toMatch(/6%/);
        expect(c.surface).not.toMatch(/12%/);
        expect(c.border).toMatch(/16%/);
    });

    it("keeps sky blue for dark remote and steel blue for light local", () => {
        const remoteDark = buildCodingBannerChrome({ isDark: true, remote: true, theme: darkThemeStub });
        expect(remoteDark.accent).toBe("#38bdf8");
        const localLight = buildCodingBannerChrome({ isDark: false, remote: false, theme: darkThemeStub });
        expect(localLight.accent).toBe("#2f5f98");
        expect(localLight.surface).toBe("#f5f8fc");
    });
});

describe("codingStepStatusColor", () => {
    it("softens passed/failed on dark and keeps semantic contrast on light", () => {
        expect(codingStepStatusColor("passed", true, chrome)).toBe(CODING_BANNER_LOCAL_DARK_ACCENT);
        expect(codingStepStatusColor("failed", true, chrome)).toBe("#e07a72");
        expect(codingStepStatusColor("passed", false, chrome)).toBe("#16a34a");
        expect(codingStepStatusColor("failed", false, chrome)).toBe("#dc2626");
        expect(codingStepStatusColor("running", true, chrome)).toBe(chrome.accentStrong);
        expect(codingStepStatusColor("pending", true, chrome)).toBe(chrome.muted);
    });
});

describe("deriveChipStatus", () => {
    it("prefers failed step over running", () => {
        expect(deriveChipStatus("en", [
            { index: 1, status: "failed" },
            { index: 2, status: "running" },
        ], false)).toBe("T1 ✗");
    });

    it("shows running step when nothing failed", () => {
        expect(deriveChipStatus("en", [{ index: 3, status: "running" }], false)).toBe("T3…");
    });

    it("shows pending approval label when requested", () => {
        expect(deriveChipStatus("en", [], true)).toMatch(/Pending|待批/i);
    });

    it("returns Ready when idle with no steps", () => {
        expect(deriveChipStatus("en", [], false)).toMatch(/Ready|就绪|就緒/i);
    });
});

describe("CodingWorkbenchControlPanel", () => {
    it("puts env description on chip title and keeps chip above popover", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId, rerender } = render(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote
                remoteHost="10.0.0.8"
                stepStatuses={[]}
                pendingApproval={false}
                conflictCount={0}
                expanded={false}
                onExpandedChange={onExpandedChange}
                envDescription="Full remote workbench: code runs on the remote host via SSH; Skill/MCP. Multi-turn. Source preview."
            >
                <div data-testid="coding-panel-body">body</div>
            </CodingWorkbenchControlPanel>,
        );
        const chip = getByTestId("remote-coding-env-banner");
        const title = chip.getAttribute("title") || "";
        expect(title.toLowerCase()).toMatch(/source preview/i);
        expect(title.toLowerCase()).toMatch(/ssh/i);
        expect(chip.getAttribute("aria-expanded")).toBe("false");

        fireEvent.click(chip);
        expect(onExpandedChange).toHaveBeenCalledWith(true);

        rerender(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote
                remoteHost="10.0.0.8"
                stepStatuses={[{ index: 1, status: "failed", title: "build" }]}
                pendingApproval={false}
                conflictCount={2}
                expanded
                onExpandedChange={onExpandedChange}
                envDescription="Full remote workbench via SSH. Multi-turn. Source preview."
            >
                <div data-testid="coding-panel-body">body</div>
            </CodingWorkbenchControlPanel>,
        );

        const root = getByTestId("coding-control-float-root");
        const popover = getByTestId("coding-control-popover");
        // Chip is first child; popover follows (drops below chip).
        expect(root.children[0]).toBe(getByTestId("remote-coding-env-banner"));
        expect(root.children[1]).toBe(popover);
        expect(getByTestId("coding-control-chip-conflicts").textContent || "").toMatch(/2/);
        expect(getByTestId("remote-coding-env-banner").textContent || "").toMatch(/T1/);
        expect(getByTestId("coding-panel-body")).toBeTruthy();
    });

    it("does not collapse via chip click when lockExpanded", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId } = render(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote={false}
                stepStatuses={[]}
                pendingApproval
                conflictCount={0}
                lockExpanded
                expanded
                onExpandedChange={onExpandedChange}
            >
                <div>body</div>
            </CodingWorkbenchControlPanel>,
        );
        fireEvent.click(getByTestId("coding-env-banner"));
        expect(onExpandedChange).not.toHaveBeenCalled();
        expect(getByTestId("coding-control-float-root").getAttribute("data-expanded")).toBe("true");
    });

    it("collapses on Escape and outside pointerdown when not locked", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId } = render(
            <div>
                <button type="button" data-testid="outside-btn">outside</button>
                <CodingWorkbenchControlPanel
                    lang="en"
                    theme={{ text: "#111", isDark: false } as any}
                    chrome={chrome}
                    remote={false}
                    stepStatuses={[]}
                    pendingApproval={false}
                    conflictCount={0}
                    expanded
                    onExpandedChange={onExpandedChange}
                >
                    <div>body</div>
                </CodingWorkbenchControlPanel>
            </div>,
        );
        expect(getByTestId("coding-control-popover")).toBeTruthy();
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onExpandedChange).toHaveBeenCalledWith(false);

        onExpandedChange.mockClear();
        fireEvent.pointerDown(getByTestId("outside-btn"), { button: 0 });
        expect(onExpandedChange).toHaveBeenCalledWith(false);
    });

    it("does not pin bottom when collapsed", () => {
        const { getByTestId } = render(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote={false}
                stepStatuses={[]}
                pendingApproval={false}
                conflictCount={0}
                expanded={false}
                onExpandedChange={() => {}}
            >
                <div>body</div>
            </CodingWorkbenchControlPanel>,
        );
        const root = getByTestId("coding-control-float-root") as HTMLElement;
        expect(root.style.bottom).toBe("");
        expect(root.getAttribute("data-expanded")).toBe("false");
    });

    it("does not collapse on Escape while focus is in a form field", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId } = render(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote={false}
                stepStatuses={[]}
                pendingApproval={false}
                conflictCount={0}
                expanded
                onExpandedChange={onExpandedChange}
            >
                <textarea data-testid="inner-field" defaultValue="draft" />
            </CodingWorkbenchControlPanel>,
        );
        const field = getByTestId("inner-field") as HTMLTextAreaElement;
        field.focus();
        fireEvent.keyDown(field, { key: "Escape" });
        expect(onExpandedChange).not.toHaveBeenCalled();
    });

    it("yields Escape only when conflict panel is visible, not when CF slot is hidden", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId, rerender } = render(
            <div>
                <div data-testid="cf-slot">
                    <div data-testid="coding-conflict-side-panel">cf</div>
                </div>
                <CodingWorkbenchControlPanel
                    lang="en"
                    theme={{ text: "#111", isDark: false } as any}
                    chrome={chrome}
                    remote={false}
                    stepStatuses={[]}
                    pendingApproval={false}
                    conflictCount={1}
                    expanded
                    onExpandedChange={onExpandedChange}
                >
                    <div>body</div>
                </CodingWorkbenchControlPanel>
            </div>,
        );
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onExpandedChange).not.toHaveBeenCalled();

        onExpandedChange.mockClear();
        rerender(
            <div>
                <div data-testid="cf-slot" aria-hidden="true">
                    <div data-testid="coding-conflict-side-panel">cf</div>
                </div>
                <CodingWorkbenchControlPanel
                    lang="en"
                    theme={{ text: "#111", isDark: false } as any}
                    chrome={chrome}
                    remote={false}
                    stepStatuses={[]}
                    pendingApproval={false}
                    conflictCount={1}
                    expanded
                    onExpandedChange={onExpandedChange}
                >
                    <div>body</div>
                </CodingWorkbenchControlPanel>
            </div>,
        );
        expect(getByTestId("coding-conflict-side-panel")).toBeTruthy();
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onExpandedChange).toHaveBeenCalledWith(false);
    });

    it("hides idle Ready status on the chip", () => {
        const { getByTestId } = render(
            <CodingWorkbenchControlPanel
                lang="en"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote={false}
                stepStatuses={[]}
                pendingApproval={false}
                conflictCount={0}
                expanded={false}
                onExpandedChange={() => {}}
            >
                <div>body</div>
            </CodingWorkbenchControlPanel>,
        );
        const text = getByTestId("coding-env-banner").textContent || "";
        expect(text).toMatch(/Coding|编程/);
        expect(text).not.toMatch(/Ready|就绪|就緒/);
    });

    it("isFormFieldTarget detects inputs", () => {
        const input = document.createElement("input");
        expect(isFormFieldTarget(input)).toBe(true);
        expect(isFormFieldTarget(document.createElement("div"))).toBe(false);
    });

    it("does not collapse when clicking elements marked ignore-outside", () => {
        const onExpandedChange = vi.fn();
        const { getByTestId } = render(
            <div>
                <div data-testid="toast" data-coding-float-ignore-outside="">
                    <button type="button" data-testid="toast-btn">ok</button>
                </div>
                <CodingWorkbenchControlPanel
                    lang="en"
                    theme={{ text: "#111", isDark: false } as any}
                    chrome={chrome}
                    remote={false}
                    stepStatuses={[]}
                    pendingApproval={false}
                    conflictCount={0}
                    expanded
                    onExpandedChange={onExpandedChange}
                >
                    <div>body</div>
                </CodingWorkbenchControlPanel>
            </div>,
        );
        fireEvent.pointerDown(getByTestId("toast-btn"), { button: 0 });
        expect(onExpandedChange).not.toHaveBeenCalled();
    });
});
