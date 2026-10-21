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
    it("shows pause while the goal is running and continue after it stops", async () => {
        const api = installApp();
        const view = render(<GoalQuickBar lang="zh-Hans" theme={theme} sessionKey="desktop-user" />);
        await waitFor(() => expect(view.getByTestId("goal-quick-bar")).toBeTruthy());
        expect(view.getByTestId("goal-bar-label").textContent).toBe("进行中");
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
