import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
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

afterEach(() => cleanup());

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
        expect(getByTestId("coding-agent-plan-checklist")).toBeTruthy();
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
        const active = getByTestId("coding-agent-plan-step-2") as HTMLElement;
        expect(active.style.background).toBe(asRendered(chrome.chipActiveBg));
        expect(active.style.fontWeight).toBe("650");
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
});
