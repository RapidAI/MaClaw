import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GoalQuickBar } from "../GoalQuickBar";
import type { Theme } from "../aiAssistantPanelTheme";

const handlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>());

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: (name: string, cb: (payload: unknown) => void) => {
        handlers.set(name, cb);
        return () => handlers.delete(name);
    },
    EventsOff: vi.fn(),
}));

const theme = {
    text: "#243044",
    textMuted: "#66788a",
    headingColor: "#1c2b3a",
    divider: "#d5deea",
    inputBarBorder: "#d5deea",
    inputBarBg: "#f7f9fc",
    bg: "#fff",
} as Theme;

const active = {
    visible: true,
    user_id: "desktop-user",
    goal_id: "g1",
    objective: "以现有性能为基础，提升20%",
    status: "active",
    phase: "execute",
    turns_used: 2,
    max_turns: 50,
    summary: "",
    awaiting_user: false,
};

function installApp(overrides: Record<string, ReturnType<typeof vi.fn>> = {}) {
    const api = {
        GetSessionGoalBar: vi.fn().mockResolvedValue(active),
        PauseSessionGoal: vi.fn().mockResolvedValue({ ...active, status: "paused" }),
        ContinueSessionGoal: vi.fn().mockResolvedValue({ ...active, status: "active" }),
        CancelSessionGoal: vi.fn().mockResolvedValue({ visible: false, user_id: "desktop-user", status: "cleared" }),
        UpdateSessionGoal: vi.fn().mockResolvedValue({ ...active, objective: "改成新的目标" }),
        ...overrides,
    };
    (window as unknown as { go?: { main?: { App?: typeof api } } }).go = { main: { App: api } };
    return api;
}

afterEach(() => {
    cleanup();
    handlers.clear();
    delete (window as unknown as { go?: unknown }).go;
});

describe("GoalQuickBar", () => {
    it("stays within the composer card width", async () => {
        installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-quick-bar")).toBeTruthy());
        const bar = view.getByTestId("goal-quick-bar") as HTMLElement;
        expect(bar.className).toContain("mc-goal-quick-bar");
        expect(bar.style.marginLeft).toBe("");
        expect(bar.style.marginRight).toBe("");
        expect(bar.style.width).toBe("");
        expect(bar.style.maxWidth).toBe("");

        const partial = readFileSync(resolve(process.cwd(), "src/styles/partials/100-pet-store-mc.css"), "utf8");
        const assembled = readFileSync(resolve(process.cwd(), "src/App.css"), "utf8");
        for (const css of [partial, assembled]) {
            expect(css).toContain("--mc-composer-inset-x: 24px");
            expect(css).toContain("margin: 0 var(--mc-composer-inset-x, 24px) 18px !important");
            expect(css).toContain(".mc-goal-quick-bar,\n[data-testid='ai-input-resize-handle']");
            expect(css).toContain("max-width: calc(100% - (2 * var(--mc-composer-inset-x, 24px)))");
            expect(css).toContain("margin-left: var(--mc-composer-inset-x, 24px)");
            expect(css).toContain("margin-right: var(--mc-composer-inset-x, 24px)");
            expect(css).not.toContain(".mc-goal-quick-bar {\n  box-sizing: border-box;\n  width:");
            expect(css).toContain("--mc-composer-inset-x: 12px");
            expect(css).toContain("margin: 0 var(--mc-composer-inset-x, 12px) 12px !important");
        }
    });

    it("shows pause while the goal is running and continue after it stops", async () => {
        const api = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-quick-bar")).toBeTruthy());
        expect(view.getByTestId("goal-bar-label").textContent).toBe("进行中");
        expect(view.getByTestId("goal-bar-icon")).toBeTruthy();
        const bar = view.getByTestId("goal-quick-bar");
        const icon = view.getByTestId("goal-bar-icon");
        const objective = view.getByTestId("goal-bar-objective");
        expect(bar.compareDocumentPosition(icon) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(icon.compareDocumentPosition(objective) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(view.getByTestId("goal-bar-objective").textContent).toContain("提升20%");
        const buttons = view.getByTestId("goal-quick-bar").querySelectorAll("button");
        expect(Array.from(buttons).map(button => button.getAttribute("data-testid"))).toEqual([
            "goal-bar-cancel",
            "goal-bar-pause",
            "goal-bar-edit",
        ]);
        expect(view.getByTestId("goal-bar-pause").getAttribute("aria-label")).toBe("暂停");
        expect(view.queryByTestId("goal-bar-continue")).toBeNull();

        fireEvent.click(view.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(api.PauseSessionGoal).toHaveBeenCalledWith("desktop-user"));
        await waitFor(() => expect(view.getByTestId("goal-bar-continue").getAttribute("aria-label")).toBe("继续"));
    });

    it("continues a paused goal and confirms before deleting it", async () => {
        const api = installApp({
            GetSessionGoalBar: vi.fn().mockResolvedValue({ ...active, status: "paused", summary: "Auto-paused: no tool calls" }),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("已暂停"));
        expect(view.getByTestId("goal-bar-label").getAttribute("title")).toContain("no tool");
        fireEvent.click(view.getByTestId("goal-bar-continue"));
        await waitFor(() => expect(api.ContinueSessionGoal).toHaveBeenCalledWith("desktop-user"));
        await waitFor(() => expect(view.getByTestId("goal-bar-pause")).toBeTruthy());

        fireEvent.click(view.getByTestId("goal-bar-cancel"));
        expect(view.getByTestId("goal-bar-cancel").getAttribute("aria-label")).toBe("确认删除");
        expect(api.CancelSessionGoal).not.toHaveBeenCalled();
        fireEvent.click(view.getByTestId("goal-bar-cancel"));
        await waitFor(() => expect(api.CancelSessionGoal).toHaveBeenCalledWith("desktop-user"));
        await waitFor(() => expect(view.queryByTestId("goal-quick-bar")).toBeNull());
    });

    it("stays hidden when the goal has succeeded", async () => {
        installApp({
            GetSessionGoalBar: vi.fn().mockResolvedValue({ ...active, status: "complete", visible: false }),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.queryByTestId("goal-quick-bar")).toBeNull());
    });

    it("edits the goal command from the third icon", async () => {
        const api = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-edit")).toBeTruthy());
        fireEvent.click(view.getByTestId("goal-bar-edit"));
        const input = view.getByTestId("goal-bar-edit-input") as HTMLInputElement;
        expect(input.value).toBe("以现有性能为基础，提升20%");
        fireEvent.change(input, { target: { value: "改成新的目标" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(api.UpdateSessionGoal).toHaveBeenCalledWith("desktop-user", "改成新的目标"));
        await waitFor(() => expect(view.getByTestId("goal-bar-objective").textContent).toBe("改成新的目标"));
    });

    it("keeps a pause when a read that overlapped the click returns the old goal", async () => {
        let releasePause: (snap: typeof active) => void = () => {};
        const pauseHeld = new Promise<typeof active>(resolve => {
            releasePause = resolve;
        });
        let releaseRead: (snap: typeof active) => void = () => {};
        const readHeld = new Promise<typeof active>(resolve => {
            releaseRead = resolve;
        });
        let reads = 0;
        const api = installApp({
            GetSessionGoalBar: vi.fn().mockImplementation(() => {
                reads += 1;
                if (reads === 1) return Promise.resolve(active);
                return readHeld;
            }),
            PauseSessionGoal: vi.fn().mockImplementation(() => pauseHeld),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("进行中"));

        fireEvent.click(view.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(api.PauseSessionGoal).toHaveBeenCalledWith("desktop-user"));
        const readsAtClick = reads;
        await act(async () => {
            handlers.get("goal-state-changed")?.({ ...active, user_id: "desktop-user", status: "active" });
        });
        await waitFor(() => expect(reads).toBe(readsAtClick + 1));

        await act(async () => {
            releasePause({ ...active, status: "paused", summary: "paused by user" });
        });
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("已暂停"));
        await act(async () => {
            releaseRead(active);
        });
        expect(view.getByTestId("goal-bar-label").textContent).toBe("已暂停");
        expect(view.getByTestId("goal-bar-continue").getAttribute("aria-label")).toBe("继续");
    });

    it("does not bring a finished goal back when an older read completes", async () => {
        let releaseStale: (snap: typeof active) => void = () => {};
        const stale = new Promise<typeof active>(resolve => {
            releaseStale = resolve;
        });
        let reads = 0;
        installApp({
            GetSessionGoalBar: vi.fn().mockImplementation(() => {
                reads += 1;
                if (reads === 1) return Promise.resolve(active);
                if (reads === 2) return stale;
                return Promise.resolve({ ...active, status: "complete", visible: false });
            }),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("进行中"));

        await act(async () => {
            handlers.get("goal-state-changed")?.({ ...active, user_id: "desktop-user", status: "active" });
        });
        await waitFor(() => expect(reads).toBe(2));
        await act(async () => {
            handlers.get("goal-state-changed")?.({
                ...active,
                user_id: "desktop-user",
                status: "complete",
                visible: false,
            });
        });
        await waitFor(() => expect(view.queryByTestId("goal-quick-bar")).toBeNull());

        await act(async () => {
            releaseStale(active);
        });
        expect(view.queryByTestId("goal-quick-bar")).toBeNull();
    });

    it("does not let a click result repaint a goal that already finished", async () => {
        let releasePause: (snap: typeof active) => void = () => {};
        const pauseHeld = new Promise<typeof active>(resolve => {
            releasePause = resolve;
        });
        const api = installApp({
            GetSessionGoalBar: vi.fn()
                .mockResolvedValueOnce(active)
                .mockResolvedValue({ ...active, status: "complete", visible: false }),
            PauseSessionGoal: vi.fn().mockImplementation(() => pauseHeld),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("进行中"));

        fireEvent.click(view.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(api.PauseSessionGoal).toHaveBeenCalledWith("desktop-user"));
        await act(async () => {
            handlers.get("goal-state-changed")?.({
                ...active,
                user_id: "desktop-user",
                status: "complete",
                visible: false,
            });
        });
        await waitFor(() => expect(view.queryByTestId("goal-quick-bar")).toBeNull());

        await act(async () => {
            releasePause(active);
        });
        await waitFor(() => expect(api.GetSessionGoalBar.mock.calls.length).toBeGreaterThan(1));
        expect(view.queryByTestId("goal-quick-bar")).toBeNull();
    });

    it("saves a goal edit when the field loses focus", async () => {
        const api = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-edit")).toBeTruthy());
        fireEvent.click(view.getByTestId("goal-bar-edit"));
        const input = view.getByTestId("goal-bar-edit-input") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "改成新的目标" } });
        fireEvent.blur(input);
        await waitFor(() => expect(api.UpdateSessionGoal).toHaveBeenCalledWith("desktop-user", "改成新的目标"));
        await waitFor(() => expect(view.getByTestId("goal-bar-objective").textContent).toBe("改成新的目标"));
    });

    it("says when pause does not change the goal", async () => {
        installApp({
            PauseSessionGoal: vi.fn().mockResolvedValue(active),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-pause")).toBeTruthy());
        fireEvent.click(view.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(view.getByTestId("goal-bar-notice").textContent).toContain("未生效"));
        expect(view.getByTestId("goal-bar-pause")).toBeTruthy();
    });

    it("saves an edit before pause, and does not pause when the save does not land", async () => {
        const saved = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-edit")).toBeTruthy());
        fireEvent.click(view.getByTestId("goal-bar-edit"));
        fireEvent.change(view.getByTestId("goal-bar-edit-input"), { target: { value: "改成新的目标" } });
        fireEvent.click(view.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(saved.UpdateSessionGoal).toHaveBeenCalledWith("desktop-user", "改成新的目标"));
        await waitFor(() => expect(saved.PauseSessionGoal).toHaveBeenCalledWith("desktop-user"));

        cleanup();
        const rejected = installApp({
            UpdateSessionGoal: vi.fn().mockResolvedValue(active),
        });
        const again = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(again.getByTestId("goal-bar-edit")).toBeTruthy());
        fireEvent.click(again.getByTestId("goal-bar-edit"));
        fireEvent.change(again.getByTestId("goal-bar-edit-input"), { target: { value: "改成新的目标" } });
        fireEvent.click(again.getByTestId("goal-bar-pause"));
        await waitFor(() => expect(rejected.UpdateSessionGoal).toHaveBeenCalledWith("desktop-user", "改成新的目标"));
        await waitFor(() => expect(again.getByTestId("goal-bar-notice").textContent).toContain("未生效"));
        expect(rejected.PauseSessionGoal).not.toHaveBeenCalled();
        expect((again.getByTestId("goal-bar-edit-input") as HTMLInputElement).value).toBe("改成新的目标");
    });

    it("keeps an open edit until delete is confirmed", async () => {
        const api = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-edit")).toBeTruthy());
        fireEvent.click(view.getByTestId("goal-bar-edit"));
        fireEvent.change(view.getByTestId("goal-bar-edit-input"), { target: { value: "先别删" } });
        fireEvent.click(view.getByTestId("goal-bar-cancel"));
        expect(api.CancelSessionGoal).not.toHaveBeenCalled();
        expect(api.UpdateSessionGoal).not.toHaveBeenCalled();
        expect((view.getByTestId("goal-bar-edit-input") as HTMLInputElement).value).toBe("先别删");
        fireEvent.click(view.getByTestId("goal-bar-cancel"));
        await waitFor(() => expect(api.CancelSessionGoal).toHaveBeenCalledWith("desktop-user"));
    });

    it("offers pause while the goal is waiting for a reply", async () => {
        installApp({
            GetSessionGoalBar: vi.fn().mockResolvedValue({ ...active, awaiting_user: true }),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("等待回复"));
        expect(view.getByTestId("goal-bar-pause").getAttribute("aria-label")).toBe("暂停");
        expect(view.queryByTestId("goal-bar-continue")).toBeNull();
    });

    it("labels a dead remote session apart from an ordinary pause", async () => {
        installApp({
            GetSessionGoalBar: vi.fn().mockResolvedValue({
                ...active,
                status: "paused",
                summary: "这个任务的执行环境是 dev.example 的 /src/app。SSH 会话还没有连上，请在远程连接面板重新连接后再继续。",
            }),
        });
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-bar-label").textContent).toBe("远程未连接"));
        expect(view.getByTestId("goal-bar-label").getAttribute("title")).toContain("dev.example");
        expect(view.getByTestId("goal-bar-continue").getAttribute("aria-label")).toBe("继续");
    });
});
