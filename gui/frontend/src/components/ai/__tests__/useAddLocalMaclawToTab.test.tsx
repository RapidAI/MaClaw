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
        registerLocalExecutorMock.mockResolvedValueOnce({ participant_id: "machine-local", display_name: "Local AI" });
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
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "machine-local"], "session-1", { "ve-a": "Group", "machine-local": "Local AI" }, ["machine-local"]);
    });

    it("creates a session before registering local AI when no session id exists", async () => {
        registerLocalExecutorMock.mockResolvedValueOnce({ participant_id: "machine-local", display_name: "Local AI" });
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
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "machine-local"], "session-new", { "ve-a": "Group", "machine-local": "Local AI" }, ["machine-local"]);
    });

    it("preserves existing group participant names when adding local AI", async () => {
        registerLocalExecutorMock.mockResolvedValueOnce({ participant_id: "machine-local", display_name: "Local AI" });
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = {
            id: "group-1",
            type: "group",
            title: "Group",
            veId: "ve-a",
            participants: ["ve-a", "ve-b"],
            participantNames: { "ve-a": "Agent A", "ve-b": "Contract Bot" },
            closable: true,
        };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "ve-b", "machine-local"], "session-1", {
            "ve-a": "Agent A",
            "ve-b": "Contract Bot",
            "machine-local": "Local AI",
        }, ["machine-local"]);
    });

    it("does not show local AI as joined when local registration fails", async () => {
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

        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
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
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a", " LOCAL-MACLAW "], closable: true };
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

    it("does not register duplicate local AI participants by canonical display name", async () => {
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a", "machine-local"], participantNames: { "machine-local": "Local AI" }, closable: true };
        const { result } = renderHook(() => useAddLocalMaclawToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab);
        });

        expect(registerLocalExecutorMock).not.toHaveBeenCalled();
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
    });
});
