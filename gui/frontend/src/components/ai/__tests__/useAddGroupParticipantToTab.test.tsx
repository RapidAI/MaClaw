import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAddGroupParticipantToTab } from "../useAddGroupParticipantToTab";
import type { AITab } from "../AITabTypes";

const addVEToGroupMock = vi.fn();
const initiateVEConversationMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    AddVEToGroup: (...args: unknown[]) => addVEToGroupMock(...args),
    InitiateVEConversation: (...args: unknown[]) => initiateVEConversationMock(...args),
}));

describe("useAddGroupParticipantToTab", () => {
    beforeEach(() => {
        addVEToGroupMock.mockReset();
        initiateVEConversationMock.mockReset();
        initiateVEConversationMock.mockResolvedValue({ session_id: "session-new" });
    });

    it("adds the participant through Hub and updates tab metadata", async () => {
        addVEToGroupMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-b", "Contract Bot");
        });

        expect(initiateVEConversationMock).not.toHaveBeenCalled();
        expect(addVEToGroupMock).toHaveBeenCalledWith("session-1", "ve-b");
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "ve-b"], "session-1", { "ve-b": "Contract Bot" });
    });

    it("uses a readable fallback when the selected VE has no name", async () => {
        addVEToGroupMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "m_b1821505498d817c", "m_b1821505498d817c");
        });

        expect(upgradeVETabToGroup).toHaveBeenCalledWith(
            "group-1",
            ["ve-a", "m_b1821505498d817c"],
            "session-1",
            { "m_b1821505498d817c": "Digital employee" }
        );
    });

    it("preserves the original direct VE display name while adding another participant", async () => {
        addVEToGroupMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "ve-tab-1", type: "ve", title: "Contract Reviewer", veId: "ve-a", closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-b", "Contract Bot");
        });

        expect(upgradeVETabToGroup).toHaveBeenCalledWith("ve-tab-1", ["ve-a", "ve-b"], "session-1", {
            "ve-a": "Contract Reviewer",
            "ve-b": "Contract Bot",
        });
    });

    it("creates a backend session before adding when no session id exists", async () => {
        addVEToGroupMock.mockResolvedValueOnce(undefined);
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-b", "Contract Bot");
        });

        expect(initiateVEConversationMock).toHaveBeenCalledWith("ve-a");
        expect(addVEToGroupMock).toHaveBeenCalledWith("session-new", "ve-b");
        expect(upgradeVETabToGroup).toHaveBeenCalledWith("group-1", ["ve-a", "ve-b"], "session-new", { "ve-b": "Contract Bot" });
    });


    it("does not update metadata when session creation fails", async () => {
        const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
        initiateVEConversationMock.mockRejectedValueOnce(new Error("offline"));
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-b", "Contract Bot");
        });

        expect(addVEToGroupMock).not.toHaveBeenCalled();
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
        errorSpy.mockRestore();
    });

    it("does not update local metadata when Hub add fails", async () => {
        const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
        addVEToGroupMock.mockRejectedValueOnce(new Error("offline"));
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-b", "Contract Bot");
        });

        expect(addVEToGroupMock).toHaveBeenCalledWith("session-1", "ve-b");
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
        errorSpy.mockRestore();
    });

    it("does not add duplicate participants", async () => {
        const upgradeVETabToGroup = vi.fn();
        const tab: AITab = { id: "group-1", type: "group", title: "Group", veId: "ve-a", participants: ["ve-a"], closable: true };
        const { result } = renderHook(() => useAddGroupParticipantToTab({
            getTabState: () => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 }),
            upgradeVETabToGroup,
        }));

        await act(async () => {
            await result.current(tab, "ve-a", "Agent A");
        });

        expect(addVEToGroupMock).not.toHaveBeenCalledWith("session-1", "ve-a");
        expect(upgradeVETabToGroup).not.toHaveBeenCalled();
    });
});
