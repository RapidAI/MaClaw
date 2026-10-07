import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
import {
    buildCodingBannerChrome,
    CODING_BANNER_LOCAL_DARK_ACCENT,
    CODING_BANNER_LOCAL_DARK_ACCENT_STRONG,
    codingStepGlyph,
    codingStepStatusColor,
    codingStepStatusLabel,
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
    stepDoneFg: "color-mix(in srgb, #2f5f98 72%, #64748b)",
    stepFailedFg: "#dc2626",
    btnPrimaryBg: "#2f5f98",
    btnPrimaryFg: "#fff",
};

const darkThemeStub = {
    btnColor: "#b7d3ef", // accent/foreground — must NOT be used as filled CTA bg
    sendBtnBg: "#2f5f98",
    sendBtnColor: "#ffffff",
    titleBarBg: "#1a1d24",
    bg: "#12141a",
    titleBarBorder: "#2a2f3a",
    fieldBorder: "rgba(148,163,184,0.35)",
    fieldBg: "#1e222b",
    textMuted: "#a8b8c8",
    fieldLabel: "#cbd5e1",
    promptColor: "#a8b8c8",
    text: "#e2e8f0",
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

    it("uses the main product accent for remote panels and the canvas color in light mode", () => {
        const remoteDark = buildCodingBannerChrome({ isDark: true, remote: true, theme: darkThemeStub });
        expect(remoteDark.accent).toBe(darkThemeStub.btnColor);
        expect(remoteDark.accent).not.toBe("#38bdf8");
        const localLight = buildCodingBannerChrome({ isDark: false, remote: false, theme: darkThemeStub });
        expect(localLight.accent).toBe(darkThemeStub.btnColor);
        expect(localLight.surface).toBe(darkThemeStub.bg);
        const remoteLight = buildCodingBannerChrome({ isDark: false, remote: true, theme: darkThemeStub });
        expect(remoteLight.accent).toBe(darkThemeStub.btnColor);
        expect(remoteLight.accent).not.toBe("#0284c7");
        expect(remoteLight.surface).toBe(darkThemeStub.bg);
        expect(remoteLight.accentStrong).toBe(darkThemeStub.text);
    });

    it("uses sendBtnBg/sendBtnColor for primary filled CTAs (not light btnColor)", () => {
        const c = buildCodingBannerChrome({ isDark: true, remote: true, theme: darkThemeStub });
        expect(c.btnPrimaryBg).toBe("#2f5f98");
        expect(c.btnPrimaryFg).toBe("#ffffff");
        expect(c.btnPrimaryBg).not.toBe(darkThemeStub.btnColor);
        // Labels remain readable (fieldLabel preferred over washed textMuted alone)
        expect(c.muted).toBe("#cbd5e1");
    });

    it("falls back safely when sendBtn pair is missing", () => {
        const c = buildCodingBannerChrome({
            isDark: true,
            remote: false,
            theme: { ...darkThemeStub, sendBtnBg: undefined as unknown as string, sendBtnColor: undefined as unknown as string },
        });
        // Missing sendBtn → uses light btnColor fill with auto dark ink (not white-on-light)
        expect(c.btnPrimaryBg).toBe("#b7d3ef");
        expect(c.btnPrimaryFg).toBe("#111111");
    });

    it("uses dark ink on graphite-like light send fills", () => {
        const c = buildCodingBannerChrome({
            isDark: true,
            remote: true,
            theme: { ...darkThemeStub, sendBtnBg: "#d4d4d4", sendBtnColor: "#111111" },
        });
        expect(c.btnPrimaryBg).toBe("#d4d4d4");
        expect(c.btnPrimaryFg).toBe("#111111");
    });
});

describe("codingStepStatusColor", () => {
    it("keeps finished steps on the shell accent instead of a standalone green", () => {
        expect(codingStepStatusColor("passed", chrome)).toBe(chrome.stepDoneFg);
        expect(codingStepStatusColor("completed", chrome)).toBe(chrome.stepDoneFg);
        expect(codingStepStatusColor("passed", chrome)).not.toMatch(/#16a34a/i);
        expect(codingStepStatusColor("failed", chrome)).toBe(chrome.stepFailedFg);
        expect(codingStepStatusColor("running", chrome)).toBe(chrome.accentStrong);
        expect(codingStepStatusColor("pending", chrome)).toBe(chrome.muted);
    });

    it("accepts every spelling of a step state", () => {
        // Alternate spellings the backend also emits must not fall through to muted.
        expect(codingStepStatusColor("completed", chrome)).toBe(chrome.stepDoneFg);
        expect(codingStepStatusColor("verify_failed", chrome)).toBe(chrome.stepFailedFg);
        expect(codingStepStatusColor("in_progress", chrome)).toBe(chrome.accentStrong);
        expect(codingStepGlyph("completed")).toBe("☑");
        expect(codingStepGlyph("error")).toBe("✗");
        expect(codingStepGlyph("in_progress")).toBe("…");
    });

    it("derives the finished token from the panel accent and keeps danger AA-safe", () => {
        const c = buildCodingBannerChrome({ isDark: false, remote: true, theme: { ...darkThemeStub, btnColor: "#2e75cb" } });
        expect(c.stepDoneFg).toBe("color-mix(in srgb, #2e75cb 85%, #cbd5e1)");
        expect(c.stepDoneFg).not.toMatch(/#16a34a/i);
        // Theme.errorText (#e5484d) is only 3.9:1 on white — the AA-fixed
        // --theme-danger token must win instead.
        expect(c.stepFailedFg).toBe("var(--theme-danger, #dc2626)");
        expect(c.stepFailedFg).not.toMatch(/#e5484d/i);
    });
});

describe("codingStepStatusLabel", () => {
    it("localizes execution states for the timeline", () => {
        expect(codingStepStatusLabel("zh-Hans", "passed")).toBe("已完成");
        expect(codingStepStatusLabel("zh-Hans", "running")).toBe("进行中");
        expect(codingStepStatusLabel("zh-Hans", "verify_failed")).toBe("失败");
        expect(codingStepStatusLabel("zh-Hans", "pending")).toBe("待确认");
        expect(codingStepStatusLabel("en", "completed")).toBe("Completed");
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

    it("recognises the alternate spellings of failed/running/done", () => {
        expect(deriveChipStatus("en", [{ index: 4, status: "verify_failed" }], false)).toBe("T4 ✗");
        expect(deriveChipStatus("en", [{ index: 5, status: "in_progress" }], false)).toBe("T5…");
        expect(deriveChipStatus("en", [{ index: 6, status: "completed" }], false)).toMatch(/Done|完成/i);
    });

    it("shows pending approval label when requested", () => {
        expect(deriveChipStatus("en", [], true)).toMatch(/Pending|待批/i);
    });

    it("returns Ready when idle with no steps", () => {
        expect(deriveChipStatus("en", [], false)).toMatch(/Ready|就绪|就緒/i);
    });
});

describe("CodingWorkbenchControlPanel", () => {
    it("uses maintenance intent instead of the remote coding implementation label", () => {
        const { getByTestId } = render(
            <CodingWorkbenchControlPanel
                lang="zh-Hans"
                theme={{ text: "#111", isDark: false } as any}
                chrome={chrome}
                remote
                intent="remote_maintenance"
                remoteHost="ops.example.test"
                stepStatuses={[]}
                pendingApproval={false}
                conflictCount={0}
                expanded={false}
                onExpandedChange={vi.fn()}
                envDescription="先只读诊断，高风险修复需要确认。"
            >
                <div />
            </CodingWorkbenchControlPanel>,
        );
        const chip = getByTestId("remote-coding-env-banner");
        expect(chip.textContent || "").toContain("远程维护");
        expect(chip.getAttribute("title") || "").toContain("远程维护任务");
    });

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

    it("rests at defaultTop and drags the collapsed chip without toggling", () => {
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
                expanded={false}
                onExpandedChange={onExpandedChange}
                defaultTop={80}
            >
                <div>body</div>
            </CodingWorkbenchControlPanel>,
        );
        const root = getByTestId("coding-control-float-root") as HTMLElement;
        expect(root.style.top).toBe("80px");

        const chip = getByTestId("coding-env-banner");
        // Sub-threshold wiggle stays a click.
        fireEvent.pointerDown(chip, { button: 0, pointerId: 1, clientX: 100, clientY: 100 });
        fireEvent.pointerMove(document, { pointerId: 1, clientX: 101, clientY: 102 });
        fireEvent.pointerUp(document, { pointerId: 1, clientX: 101, clientY: 102 });
        fireEvent.click(chip);
        expect(onExpandedChange).toHaveBeenCalledWith(true);
        expect(root.style.transform).toBe("");

        onExpandedChange.mockClear();
        // Real drag moves the chip and suppresses the trailing click.
        fireEvent.pointerDown(chip, { button: 0, pointerId: 2, clientX: 100, clientY: 100 });
        fireEvent.pointerMove(document, { pointerId: 2, clientX: 100, clientY: 160 });
        expect(root.style.transform).toBe("translate(0px, 60px)");
        fireEvent.pointerUp(document, { pointerId: 2, clientX: 100, clientY: 160 });
        fireEvent.click(chip);
        expect(onExpandedChange).not.toHaveBeenCalled();

        // A plain click after the drag still toggles.
        fireEvent.click(chip);
        expect(onExpandedChange).toHaveBeenCalledWith(true);
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
