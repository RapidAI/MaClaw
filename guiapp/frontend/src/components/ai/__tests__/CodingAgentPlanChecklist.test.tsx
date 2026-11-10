import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, fireEvent, act } from "@testing-library/react";
import { CodingAgentPlanChecklist, codingPlanProgressLabel } from "../CodingAgentPlanChecklist";
import { codingStepGlyph } from "../CodingWorkbenchControlPanel";

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

const theme = { isDark: false, text: "#111", errorText: "#dc2626" } as any;

afterEach(() => {
    cleanup();
    vi.useRealTimers();
});

describe("codingPlanProgressLabel", () => {
    it("counts finished steps", () => {
        expect(codingPlanProgressLabel([
            { index: 1, status: "passed" },
            { index: 2, status: "running" },
            { index: 3, status: "pending" },
        ])).toBe("1/3");
    });

    it("counts every done spelling and skipped, but never a failed step", () => {
        expect(codingPlanProgressLabel([
            { index: 1, status: "passed" },
            { index: 2, status: "completed" },
            { index: 3, status: "success" },
            { index: 4, status: "succeeded" },
            { index: 5, status: "skipped" },
            { index: 6, status: "canceled" },
            { index: 7, status: "failed" },
            { index: 8, status: "pending" },
        ])).toBe("6/8");
    });
});

describe("codingStepGlyph", () => {
    it("uses Codex checklist marks", () => {
        expect(codingStepGlyph("passed")).toBe("☑");
        expect(codingStepGlyph("running")).toBe("…");
        expect(codingStepGlyph("pending")).toBe("☐");
        expect(codingStepGlyph("failed")).toBe("✗");
    });
});

describe("CodingAgentPlanChecklist", () => {
    it("renders live steps and highlights the current one", () => {
        const { getByTestId, queryByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "passed" },
                    { index: 2, title: "改登录", status: "running" },
                    { index: 3, title: "补测试", status: "pending" },
                ]}
            />,
        );
        const card = getByTestId("coding-agent-plan-checklist") as HTMLElement;
        expect(card).toBeTruthy();
        expect(card.style.marginLeft).toBe("");
        expect(card.style.marginRight).toBe("");
        expect(card.style.marginBottom).toBe("8px");
        expect(card.style.width).toBe("");
        expect(card.style.maxWidth).toBe("");
        expect(getByTestId("coding-agent-plan-current").textContent).toContain("T2");
        expect(getByTestId("coding-agent-plan-step-2").getAttribute("data-status")).toBe("running");
        expect(queryByTestId("coding-agent-plan-approve")).toBeNull();
    });

    it("paints steps from the panel accent, not a standalone green", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "passed" },
                    { index: 2, title: "改登录", status: "running" },
                    { index: 3, title: "补测试", status: "failed" },
                ]}
            />,
        );
        // jsdom normalizes hex to rgb() inside color-mix — normalize the expected
        // token the same way so the assertion compares rendered values.
        const asRendered = (css: string) => {
            const probe = document.createElement("div");
            probe.style.color = css;
            return probe.style.color;
        };
        const done = getByTestId("coding-agent-plan-step-1") as HTMLElement;
        expect(done.style.color).toBe(asRendered(chrome.stepDoneFg));
        expect(done.style.color).not.toMatch(/#16a34a|rgb\(22,\s*163,\s*74\)/i);
        // Current step: accent pill + bold; finished steps stay plain rows.
        // (650 collapsed onto the standard 600 stop in the weight-ramp pass.)
        const active = getByTestId("coding-agent-plan-step-2") as HTMLElement;
        expect(active.style.background).toBe(asRendered(chrome.chipActiveBg));
        expect(active.style.fontWeight).toBe("600");
        expect(done.style.background).toBe("");
        const failed = getByTestId("coding-agent-plan-step-3") as HTMLElement;
        expect(failed.style.color).toBe(asRendered(chrome.stepFailedFg));
    });

    it("lights up the header badge and the row for every active spelling", () => {
        // `started` used to style the row but leave the header badge blank
        // because the header compared status strings inline.
        for (const status of ["running", "in_progress", "started"]) {
            const { getByTestId } = render(
                <CodingAgentPlanChecklist
                    lang="zh"
                    theme={theme}
                    chrome={chrome}
                    steps={[{ index: 1, title: "定位入口", status }]}
                />,
            );
            expect(getByTestId("coding-agent-plan-current").textContent).toContain("T1");
            cleanup();
        }
    });

    it("exposes the step state to screen readers", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
            />,
        );
        const active = getByTestId("coding-agent-plan-step-1");
        expect(active.getAttribute("aria-current")).toBe("step");
        expect(getByTestId("coding-agent-plan-step-1-sr-status").textContent).toContain("进行中");
        expect(getByTestId("coding-agent-plan-step-2").getAttribute("aria-current")).toBeNull();
        expect(getByTestId("coding-agent-plan-step-2-sr-status").textContent).toContain("待确认");
    });

    it("falls back to a localized status label when a step has no title", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[{ index: 1, status: "pending" }]}
            />,
        );
        const step = getByTestId("coding-agent-plan-step-1");
        expect(step.textContent).toContain("待确认");
        expect(step.textContent).not.toContain("pending");
    });

    it("shows a stored error on the step that recorded it", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 4, title: "运行 probe", status: "running" },
                    { index: 6, title: "修复渲染器", status: "pending", summary: "src\\game.cpp(114): error C3861: 'spawnFood': identifier not found" },
                ]}
            />,
        );
        expect(getByTestId("coding-agent-plan-current").textContent).toContain("T4");
        expect(getByTestId("coding-agent-plan-step-6-note").textContent).toContain("identifier not found");
        // A path-shaped note must stay inside the row instead of overflowing it.
        expect((getByTestId("coding-agent-plan-step-6-note") as HTMLElement).style.overflowWrap).toBe("anywhere");
    });

    it("shows a paraphrased understanding of the request", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                understanding="把现有控制台贪吃蛇改成图形界面版本，保留原有玩法。"
                pendingApproval
                ready
                steps={[
                    { index: 1, title: "阅读现有实现", status: "pending" },
                    { index: 2, title: "改写并验证", status: "pending" },
                ]}
            />,
        );
        expect(getByTestId("coding-agent-plan-understanding").textContent).toContain("控制台贪吃蛇");
        expect(getByTestId("coding-agent-plan-understanding").textContent).not.toContain("改为图形界面版");
        expect(getByTestId("coding-agent-plan-checklist").textContent || "").toContain("待开始实施");
        expect(getByTestId("coding-agent-plan-approve")).toBeTruthy();
    });

    it("hides start actions once the confirmed plan is already underway", () => {
        const { queryByTestId, getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                pendingApproval
                ready
                understanding="实测三个 SDK 的 PDF 处理性能。"
                steps={[
                    { index: 1, title: "确认各SDK入口与参数", status: "running" },
                    { index: 2, title: "检查两组PDF样本", status: "pending" },
                ]}
            />,
        );
        expect(getByTestId("coding-agent-plan-current").textContent).toContain("T1");
        expect(getByTestId("coding-agent-plan-checklist").textContent || "").not.toContain("待开始实施");
        expect(queryByTestId("coding-agent-plan-approve")).toBeNull();
        expect(queryByTestId("coding-agent-plan-skip")).toBeNull();
        expect(queryByTestId("coding-agent-plan-reject")).toBeNull();
    });

    it("shows start actions while a plan is awaiting approval", () => {
        const onApprove = vi.fn();
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                pendingApproval
                ready
                steps={[
                    { index: 1, title: "写代码", status: "pending" },
                    { index: 2, title: "验证", status: "pending" },
                ]}
                onApprove={onApprove}
            />,
        );
        fireEvent.click(getByTestId("coding-agent-plan-approve"));
        expect(onApprove).toHaveBeenCalledTimes(1);
    });

    it("summarizes progress in the header with a fraction and a bar", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "passed" },
                    { index: 2, title: "改登录", status: "running" },
                    { index: 3, title: "补测试", status: "pending" },
                ]}
            />,
        );
        const progress = getByTestId("coding-agent-plan-progress");
        expect(progress.textContent).toContain("1/3");
        const fill = progress.querySelector(".capc-fill") as HTMLElement;
        expect(fill.style.width).toBe("33%");
    });

    it("shows the summary until hover, then the full step list", () => {
        vi.useFakeTimers();
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
            />,
        );
        const card = getByTestId("coding-agent-plan-hover");
        const toggle = getByTestId("coding-agent-plan-toggle");
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        expect(getByTestId("coding-agent-plan-body").getAttribute("aria-hidden")).toBe("true");
        expect(getByTestId("coding-agent-plan-progress").textContent).toContain("0/2");
        // Rows stay mounted so the hover card does not rebuild them.
        expect(getByTestId("coding-agent-plan-step-1")).toBeTruthy();
        fireEvent.mouseEnter(card);
        expect(toggle.getAttribute("aria-expanded")).toBe("true");
        expect(getByTestId("coding-agent-plan-checklist").style.getPropertyValue("--capc-room")).toBe("120px");
        expect(getByTestId("coding-agent-plan-body").getAttribute("aria-hidden")).toBe("false");
        fireEvent.mouseLeave(card);
        expect(toggle.getAttribute("aria-expanded")).toBe("true");
        act(() => { vi.advanceTimersByTime(120); });
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        expect(getByTestId("coding-agent-plan-body").getAttribute("aria-hidden")).toBe("true");
        vi.useRealTimers();
    });

    it("keeps the list under the pointer instead of pinned to the left", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
            />,
        );
        const checklist = getByTestId("coding-agent-plan-checklist");
        const popover = checklist.querySelector(".capc-popover") as HTMLElement;
        vi.spyOn(checklist, "getBoundingClientRect").mockReturnValue({
            top: 0, bottom: 40, left: 10, right: 410, width: 400, height: 40, x: 10, y: 0, toJSON() { return {}; },
        });
        Object.defineProperty(checklist, "clientWidth", { configurable: true, value: 400 });
        Object.defineProperty(checklist, "clientLeft", { configurable: true, value: 0 });
        Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 200 });
        const toggle = getByTestId("coding-agent-plan-toggle");
        fireEvent.mouseMove(toggle, { clientX: 310 });
        expect(popover.style.left).toBe("200px");
        fireEvent.mouseMove(toggle, { clientX: 10 });
        expect(popover.style.left).toBe("0px");
    });

    it("keeps the list open when it appears under the pointer", () => {
        vi.useFakeTimers();
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
            />,
        );
        const hover = getByTestId("coding-agent-plan-hover");
        vi.spyOn(hover, "matches").mockImplementation((selector) => selector === ":hover");
        fireEvent.mouseEnter(hover);
        fireEvent.mouseLeave(hover);
        act(() => { vi.advanceTimersByTime(120); });
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("true");
        vi.useRealTimers();
    });

    it("closes once the pointer leaves a list that opened underneath it", () => {
        vi.useFakeTimers();
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
            />,
        );
        const hover = getByTestId("coding-agent-plan-hover");
        let over = true;
        vi.spyOn(hover, "matches").mockImplementation((selector) => selector === ":hover" && over);
        fireEvent.mouseEnter(hover);
        fireEvent.mouseLeave(hover);
        act(() => { vi.advanceTimersByTime(120); });
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("true");
        over = false;
        act(() => { vi.advanceTimersByTime(120); });
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("false");
        vi.useRealTimers();
    });

    it("caps the list at the space above the bar instead of a fixed floor", () => {
        const column = document.createElement("div");
        column.className = "aap-chat-column";
        document.body.appendChild(column);
        const rect = (top: number): DOMRect => ({
            top, bottom: top, left: 0, right: 0, width: 0, height: 0, x: 0, y: top, toJSON() { return {}; },
        });
        const { getByTestId, unmount } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[{ index: 1, title: "定位入口", status: "running" }]}
            />,
            { container: column },
        );
        const checklist = getByTestId("coding-agent-plan-checklist");
        vi.spyOn(column, "getBoundingClientRect").mockReturnValue(rect(100));
        vi.spyOn(checklist, "getBoundingClientRect").mockReturnValue(rect(180));
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-hover"));
        expect(checklist.style.getPropertyValue("--capc-room")).toBe("72px");
        unmount();
        column.remove();
    });

    it("stops the list at the transcript so it does not cover the chat header", () => {
        const column = document.createElement("div");
        column.className = "aap-chat-column";
        const output = document.createElement("div");
        output.dataset.testid = "ai-output-container";
        const mount = document.createElement("div");
        column.append(output, mount);
        document.body.appendChild(column);
        const rect = (top: number, height = 0): DOMRect => ({
            top, bottom: top + height, left: 0, right: height > 0 ? 100 : 0, width: height > 0 ? 100 : 0, height, x: 0, y: top, toJSON() { return {}; },
        });
        const { getByTestId, unmount } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[{ index: 1, title: "定位入口", status: "running" }]}
            />,
            { container: mount },
        );
        const checklist = getByTestId("coding-agent-plan-checklist");
        vi.spyOn(column, "getBoundingClientRect").mockReturnValue(rect(40, 500));
        vi.spyOn(output, "getBoundingClientRect").mockReturnValue(rect(120, 200));
        vi.spyOn(checklist, "getBoundingClientRect").mockReturnValue(rect(400, 40));
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-hover"));
        expect(checklist.style.getPropertyValue("--capc-room")).toBe("272px");
        unmount();
        column.remove();
    });

    it("ignores a hidden transcript and caps the list at the column", () => {
        const column = document.createElement("div");
        column.className = "aap-chat-column";
        const output = document.createElement("div");
        output.dataset.testid = "ai-output-container";
        const mount = document.createElement("div");
        column.append(output, mount);
        document.body.appendChild(column);
        const rect = (top: number, height = 0): DOMRect => ({
            top, bottom: top + height, left: 0, right: height > 0 ? 100 : 0, width: height > 0 ? 100 : 0, height, x: 0, y: top, toJSON() { return {}; },
        });
        const { getByTestId, unmount } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[{ index: 1, title: "定位入口", status: "running" }]}
            />,
            { container: mount },
        );
        const checklist = getByTestId("coding-agent-plan-checklist");
        vi.spyOn(column, "getBoundingClientRect").mockReturnValue(rect(100, 400));
        vi.spyOn(output, "getBoundingClientRect").mockReturnValue(rect(0, 0));
        vi.spyOn(checklist, "getBoundingClientRect").mockReturnValue(rect(180, 40));
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-hover"));
        expect(checklist.style.getPropertyValue("--capc-room")).toBe("72px");
        unmount();
        column.remove();
    });

    it("does not open the list from the start buttons, and keeps a keyboard focus open", () => {
        vi.useFakeTimers();
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                pendingApproval
                ready
                steps={[
                    { index: 1, title: "写代码", status: "pending" },
                    { index: 2, title: "验证", status: "pending" },
                ]}
            />,
        );
        const toggle = getByTestId("coding-agent-plan-toggle");
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-approve"));
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        fireEvent.focus(toggle);
        expect(toggle.getAttribute("aria-expanded")).toBe("true");
        fireEvent.mouseLeave(getByTestId("coding-agent-plan-hover"));
        act(() => { vi.advanceTimersByTime(120); });
        expect(toggle.getAttribute("aria-expanded")).toBe("true");
        fireEvent.blur(toggle);
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        // Clicking the summary must not take focus, so the composer keeps the caret.
        const down = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
        toggle.dispatchEvent(down);
        expect(down.defaultPrevented).toBe(true);
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        fireEvent.keyDown(toggle, { key: "Escape" });
        fireEvent.focus(toggle);
        expect(toggle.getAttribute("aria-expanded")).toBe("true");
        fireEvent.keyDown(toggle, { key: "Escape" });
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        vi.useRealTimers();
    });

    it("drops a stuck hover when the step list goes away", () => {
        vi.useFakeTimers();
        const props = {
            lang: "zh" as const,
            theme,
            chrome,
            steps: [
                { index: 1, title: "写代码", status: "running" },
                { index: 2, title: "验证", status: "pending" },
            ],
        };
        const { getByTestId, rerender } = render(<CodingAgentPlanChecklist {...props} />);
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-hover"));
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("true");
        rerender(<CodingAgentPlanChecklist {...props} pendingApproval steps={[]} />);
        rerender(<CodingAgentPlanChecklist {...props} />);
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("false");
        vi.useRealTimers();
    });

    it("keeps the start decision on the summary while the list stays on hover", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                pendingApproval
                steps={[
                    { index: 1, title: "写代码", status: "pending" },
                    { index: 2, title: "验证", status: "pending" },
                ]}
            />,
        );
        const toggle = getByTestId("coding-agent-plan-toggle");
        expect(toggle.getAttribute("aria-expanded")).toBe("false");
        expect(toggle.hasAttribute("disabled")).toBe(false);
        expect(getByTestId("coding-agent-plan-body").getAttribute("aria-hidden")).toBe("true");
        expect(getByTestId("coding-agent-plan-approve")).toBeTruthy();
    });

    it("opens the file list from the right side without opening the steps", () => {
        vi.useFakeTimers();
        const { getByTestId, getAllByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[
                    { index: 1, title: "定位入口", status: "running" },
                    { index: 2, title: "改登录", status: "pending" },
                ]}
                fileChanges={[
                    { path: "guiapp/file_a.go", added: 47, removed: 0 },
                    { path: "guiapp/file_b.go", added: 15, removed: 6 },
                ]}
            />,
        );
        const filesToggle = getByTestId("coding-agent-plan-files-toggle");
        expect(getByTestId("coding-agent-plan-files-label").textContent).toContain("2 个文件已修改");
        expect(filesToggle.textContent).toContain("+62");
        expect(filesToggle.textContent).toContain("-6");
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-files"));
        expect(filesToggle.getAttribute("aria-expanded")).toBe("true");
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("false");
        expect(getByTestId("coding-agent-plan-files-body").getAttribute("aria-hidden")).toBe("false");
        expect(getAllByTestId("coding-agent-plan-file").map((row) => row.textContent).join("\n")).toContain("file_a.go");
        expect(getByTestId("coding-agent-plan-files-body").className).toContain("capc-popover-card");
        fireEvent.mouseLeave(getByTestId("coding-agent-plan-files"));
        act(() => { vi.advanceTimersByTime(120); });
        expect(filesToggle.getAttribute("aria-expanded")).toBe("false");
        vi.useRealTimers();
    });

    it("hovering the steps does not open the file list", () => {
        const { getByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[{ index: 1, title: "定位入口", status: "running" }]}
                fileChanges={[{ path: "guiapp/file_a.go", added: 10, removed: 2 }]}
            />,
        );
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-hover"));
        expect(getByTestId("coding-agent-plan-toggle").getAttribute("aria-expanded")).toBe("true");
        expect(getByTestId("coding-agent-plan-files-toggle").getAttribute("aria-expanded")).toBe("false");
    });

    it("shows the file count without a step list", () => {
        const { getByTestId, queryByTestId } = render(
            <CodingAgentPlanChecklist
                lang="zh"
                theme={theme}
                chrome={chrome}
                steps={[]}
                fileChanges={[{ path: "guiapp/file_a.go", added: 4, removed: 1 }]}
            />,
        );
        expect(getByTestId("coding-agent-plan-files-label").textContent).toContain("1 个文件已修改");
        expect(queryByTestId("coding-agent-plan-toggle")).toBeNull();
        fireEvent.mouseEnter(getByTestId("coding-agent-plan-files"));
        expect(getByTestId("coding-agent-plan-files-body").textContent).toContain("file_a.go");
    });

    it("uses the composer side inset so the card matches the input width", () => {
        const partial = readFileSync(resolve(process.cwd(), "src/styles/partials/100-pet-store-mc.css"), "utf8");
        const assembled = readFileSync(resolve(process.cwd(), "src/App.css"), "utf8");
        const marker = "[data-testid='ai-input-resize-handle'],\n[data-testid='coding-agent-plan-checklist'] {";
        for (const css of [partial, assembled]) {
            const start = css.indexOf(marker);
            expect(start).toBeGreaterThan(-1);
            const block = css.slice(start, css.indexOf("}", start));
            expect(block).toContain("box-sizing: border-box");
            expect(block).toContain("min-width: 0");
            expect(block).toContain("align-self: stretch");
            expect(block).toContain("max-width: calc(100% - (2 * var(--mc-composer-inset-x, 24px)))");
            expect(block).toContain("margin-left: var(--mc-composer-inset-x, 24px)");
            expect(block).toContain("margin-right: var(--mc-composer-inset-x, 24px)");
            expect(block).not.toContain("overflow-wrap");
        }
        const layoutPartial = readFileSync(resolve(process.cwd(), "src/styles/partials/90-im-channels-markdown.css"), "utf8");
        for (const css of [layoutPartial, assembled]) {
            expect(css).toContain(".capc-step-title { min-width: 0; flex: 1; overflow-wrap: anywhere; }");
            expect(css).toContain("[data-testid='coding-agent-plan-understanding'] { overflow-wrap: anywhere; }");
            expect(css).toContain(".capc-step-idx { flex-shrink: 0; min-width: 24px; opacity: 0.86; font-variant-numeric: tabular-nums; white-space: nowrap; }");
            expect(css).toContain(".capc-popover { position: absolute; z-index: 6; left: 0; bottom: calc(100% - 6px); width: 50%; min-width: min(260px, 100%); max-width: 100%; box-sizing: border-box; padding-bottom: 6px; pointer-events: none; visibility: hidden; }");
            expect(css).toContain(".capc-popover-card { box-sizing: border-box; max-height: min(320px, var(--capc-room, 42vh)); overflow: auto; overscroll-behavior: contain; padding: 8px 10px; border-radius: 12px; box-shadow: 0 12px 32px rgba(34, 56, 82, 0.16); }");
        }
    });
});
