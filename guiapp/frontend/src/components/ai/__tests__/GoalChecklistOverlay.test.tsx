// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { GoalChecklistOverlay } from "../GoalChecklistOverlay";
import { lightTheme } from "../aiAssistantPanelTheme";

const TEXT = "目标清单\n✓ 1. 环境与工具链探测\n→ 2. 技术选型与架构设计\n○ 3. 工程骨架与主循环\n进度：第 5/50 轮";

function mount(text = TEXT) {
    return render(<GoalChecklistOverlay lang="zh-cn" theme={lightTheme} text={text} />);
}

describe("GoalChecklistOverlay", () => {
    it("is expanded by default and shows the checklist card", () => {
        mount();
        const root = screen.getByTestId("goal-checklist-float");
        expect(root.dataset.expanded).toBe("true");
        expect(root.dataset.pinned).toBe("true");
        expect(screen.getByTestId("goal-checklist-overlay-card")).toBeTruthy();
        expect(screen.getByText("技术选型与架构设计")).toBeTruthy();
    });

    it("collapses to a summary bar that peeks on hover and folds on leave", () => {
        mount();
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        const root = screen.getByTestId("goal-checklist-float");
        expect(root.dataset.expanded).toBe("false");
        expect(root.dataset.pinned).toBe("false");
        // The collapsed bar still shows the goal summary.
        const bar = screen.getByTestId("goal-checklist-overlay-bar");
        expect(bar.textContent).toContain("目标清单");
        expect(bar.textContent).toContain("1/3");
        // Hover previews the card; leaving folds it back.
        fireEvent.mouseEnter(root);
        expect(root.dataset.expanded).toBe("true");
        fireEvent.mouseLeave(root);
        expect(root.dataset.expanded).toBe("false");
    });

    it("clicking the collapsed bar pins the card open", () => {
        mount();
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        const root = screen.getByTestId("goal-checklist-float");
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-bar"));
        expect(root.dataset.expanded).toBe("true");
        expect(root.dataset.pinned).toBe("true");
        // Pinned: leaving the float keeps it open.
        fireEvent.mouseLeave(root);
        expect(root.dataset.expanded).toBe("true");
        // The header toggle collapses it again.
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        expect(root.dataset.expanded).toBe("false");
    });

    it("clicking the header toggle while peeking keeps the card open", () => {
        mount();
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        const root = screen.getByTestId("goal-checklist-float");
        fireEvent.mouseEnter(root);
        expect(root.dataset.expanded).toBe("true");
        expect(root.dataset.pinned).toBe("false");
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        expect(root.dataset.pinned).toBe("true");
        fireEvent.mouseLeave(root);
        expect(root.dataset.expanded).toBe("true");
    });

    it("drags the float and suppresses the trailing click", () => {
        mount();
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        const root = screen.getByTestId("goal-checklist-float") as HTMLElement;
        // Sub-threshold wiggle stays a click.
        fireEvent.pointerDown(screen.getByTestId("goal-checklist-overlay-bar"), { button: 0, pointerId: 1, clientX: 100, clientY: 100 });
        fireEvent.pointerMove(document, { pointerId: 1, clientX: 101, clientY: 102 });
        fireEvent.pointerUp(document, { pointerId: 1, clientX: 101, clientY: 102 });
        fireEvent.click(screen.getByTestId("goal-checklist-overlay-bar"));
        expect(root.dataset.pinned).toBe("true");
        expect(root.style.transform).toBe("");

        fireEvent.click(screen.getByTestId("goal-checklist-overlay-toggle"));
        // A real drag moves the float without toggling it open.
        const bar = screen.getByTestId("goal-checklist-overlay-bar");
        fireEvent.pointerDown(bar, { button: 0, pointerId: 2, clientX: 100, clientY: 100 });
        fireEvent.pointerMove(document, { pointerId: 2, clientX: 100, clientY: 160 });
        expect(root.style.transform).toBe("translate(0px, 60px)");
        fireEvent.pointerUp(document, { pointerId: 2, clientX: 100, clientY: 160 });
        fireEvent.click(bar);
        expect(root.dataset.pinned).toBe("false");
    });
});
