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
