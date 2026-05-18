import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAddLocalMaclawToTab } from "../useAddLocalMaclawToTab";
import type { AITab } from "../AITabTypes";

const registerLocalExecutorMock = vi.fn();
const initiateVEConversationMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    RegisterLocalExecutorInGroup: (...args: unknown[]) => registerLocalExecutorMock(...args),
    InitiateVEConversation: (...args: unknown[]) => initiateVEConversationMock(...args),
}));

describe("useAddLocalMaclawToTab", () => {
    beforeEach(() => {
        registerLocalExecutorMock.mockReset();
        initiateVEConversationMock.mockReset();
        initiateVEConversationMock.mockResolvedValue({ session_id: "session-new" });
    });

    it("registers local AI with an existing session before updating tab metadata", async () => {
        registerLocalExecutorMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(initiateVEConversationMock).not.toHaveBeenCalled();
        expect(registerLocalExecutorMock).toHaveBeenCalledWith("session-1");
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "local-maclaw"], "session-1", { "ve-a": "Group", "local-maclaw": "Local AI" });
    });

    it("creates a session before registering local AI when no session id exists", async () => {
        registerLocalExecutorMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(initiateVEConversationMock).toHaveBeenCalledWith("ve-a");
        expect(registerLocalExecutorMock).toHaveBeenCalledWith("session-new");
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "local-maclaw"], "session-new", { "ve-a": "Group", "local-maclaw": "Local AI" });
    });

    it("keeps the participant panel visible when local registration fails", async () => {
        const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
        registerLocalExecutorMock.mockRejectedValueOnce(new Error("register failed"));
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "local-maclaw"], "session-1", { "ve-a": "Group", "local-maclaw": "Local AI" });
        errorSpy.mockRestore();
    });

    it("does not mutate history group tabs without a primary VE", async () => {
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "history-1", type: "group", title: "History", participants: ["me", "ve-a"], closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "disc-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(initiateVEConversationMock).not.toHaveBeenCalled();
        expect(registerLocalExecutorMock).not.toHaveBeenCalled();
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
    });

    it("does not register duplicate local AI participants", async () => {
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(initiateVEConversationMock).not.toHaveBeenCalled();
        expect(registerLocalExecutorMock).not.toHaveBeenCalled();
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
    });
});
