import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TabParticipantInviteDialog } from "../TabParticipantInviteDialog";
import type { AITab } from "../AITabTypes";
import type { Theme } from "../aiAssistantPanelTheme";

const { listVirtualEmployeesMock, getDiscussionDetailMock } = vi.hoisted(() => ({
    listVirtualEmployeesMock: vi.fn(),
    getDiscussionDetailMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    ListVirtualEmployees: listVirtualEmployeesMock,
    GroupDiscussionGetConsultationDetail: getDiscussionDetailMock,
}));

const theme = {
    bg: "#fff",
    text: "#111",
    textMuted: "#666",
    divider: "#ddd",
    errorText: "#dc2626",
} as Theme;

const baseTab: AITab = {
    id: "group-1",
    type: "group",
    title: "Agent A",
    veId: "machine-a",
    participants: ["machine-a"],
    closable: true,
};

describe("TabParticipantInviteDialog", () => {
    beforeEach(() => {
        listVirtualEmployeesMock.mockReset();
        getDiscussionDetailMock.mockReset();
        getDiscussionDetailMock.mockResolvedValue({ discussion: { participant_ids: [] }, session: { participants: [] } });
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "profile-a", machine_id: "machine-a", name: "Agent A", online_status: "online" },
            { id: "profile-b", machine_id: "machine-b", name: "Contract Bot", online_status: "online" },
            { id: "profile-c", machine_id: "machine-c", name: "Offline Bot", online_status: "offline" },
        ]);
    });

    it("lists only online digital employees not already in the tab", async () => {
        const onAdd = vi.fn();
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={onAdd} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        expect(screen.queryByText("Agent A")).toBeNull();
        expect(screen.queryByText("Offline Bot")).toBeNull();
    });

    it("opens for writable history group tabs without a primary ve id", async () => {
        const onAdd = vi.fn();
        const historyTab: AITab = { id: "history-1", type: "group", title: "History", discussionId: "disc-1", participants: ["me", "machine-a"], closable: true };
        render(<TabParticipantInviteDialog tab={historyTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={onAdd} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        expect(screen.queryByText("Agent A")).toBeNull();
    });

    it("filters participants from live history detail even when tab metadata is stale", async () => {
        getDiscussionDetailMock.mockResolvedValue({
            discussion: { participant_ids: ["me", "machine-a", "machine-b"] },
            session: { participants: [{ id: "me" }, { id: "machine-a" }, { id: "machine-b" }] },
        });
        const historyTab: AITab = { id: "history-1", type: "group", title: "History", discussionId: "disc-1", participants: ["me", "machine-a"], closable: true };
        render(<TabParticipantInviteDialog tab={historyTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={vi.fn()} />);

        await waitFor(() => expect(getDiscussionDetailMock).toHaveBeenCalledWith("disc-1"));
        await waitFor(() => expect(screen.getByTestId("tab-participant-invite-empty")).toBeTruthy());
        expect(screen.queryByText("Contract Bot")).toBeNull();
    });

    it("loads live detail and digital employee list in parallel", async () => {
        let resolveDetail: ((value: unknown) => void) | undefined;
        getDiscussionDetailMock.mockImplementation(() => new Promise((resolve) => { resolveDetail = resolve; }));
        const historyTab: AITab = { id: "history-1", type: "group", title: "History", discussionId: "disc-1", participants: ["me", "machine-a"], closable: true };
        render(<TabParticipantInviteDialog tab={historyTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={vi.fn()} />);

        await waitFor(() => expect(getDiscussionDetailMock).toHaveBeenCalledWith("disc-1"));
        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(1));

        resolveDetail?.({
            discussion: { participant_ids: ["me", "machine-a"] },
            session: { participants: [{ id: "me" }, { id: "machine-a" }] },
        });
        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
    });

    it("treats malformed digital employee list responses as empty", async () => {
        listVirtualEmployeesMock.mockResolvedValue({ employees: [] });
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={vi.fn()} />);

        await waitFor(() => expect(screen.getByTestId("tab-participant-invite-empty")).toBeTruthy());
    });

    it("filters live history participants across hub ve_ machine aliases", async () => {
        getDiscussionDetailMock.mockResolvedValue({
            discussion: { participant_ids: ["me", "ve-machine-b"] },
            session: { participants: [{ id: "me" }, { id: "ve-machine-b" }] },
        });
        const historyTab: AITab = { id: "history-1", type: "group", title: "History", discussionId: "disc-1", participants: ["me"], closable: true };
        render(<TabParticipantInviteDialog tab={historyTab} lang="en" theme={theme} onClose={vi.fn()} onAddParticipantToTab={vi.fn()} />);

        await waitFor(() => expect(getDiscussionDetailMock).toHaveBeenCalledWith("disc-1"));
        await waitFor(() => expect(screen.getByText("Agent A")).toBeTruthy());
        expect(screen.queryByText("Contract Bot")).toBeNull();
    });

    it("closes on Escape before an add is in flight", async () => {
        const onClose = vi.fn();
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={onClose} onAddParticipantToTab={vi.fn()} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.keyDown(document, { key: "Escape" });

        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("keeps the dialog open and reports add failures", async () => {
        const onClose = vi.fn();
        const onAdd = vi.fn().mockResolvedValue(null);
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={onClose} onAddParticipantToTab={onAdd} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));

        await waitFor(() => expect(screen.getByTestId("tab-participant-invite-error").textContent).toContain("Failed to add"));
        expect(onClose).not.toHaveBeenCalled();
    });

    it("shows backend add failure details", async () => {
        const onClose = vi.fn();
        const onAdd = vi.fn().mockRejectedValue(new Error("invitation sent but participant machine-b has not joined discussion session-1 yet"));
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={onClose} onAddParticipantToTab={onAdd} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));

        await waitFor(() => expect(screen.getByTestId("tab-participant-invite-error").textContent).toContain("participant machine-b has not joined"));
        expect(onClose).not.toHaveBeenCalled();
    });

    it("does not close while an add is in flight", async () => {
        let resolveAdd: ((value: unknown) => void) | undefined;
        const onClose = vi.fn();
        const onAdd = vi.fn(() => new Promise((resolve) => { resolveAdd = resolve; }));
        render(<TabParticipantInviteDialog tab={baseTab} lang="en" theme={theme} onClose={onClose} onAddParticipantToTab={onAdd} />);

        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));
        await waitFor(() => expect(screen.getByText("Adding...")).toBeTruthy());

        fireEvent.keyDown(document, { key: "Escape" });
        fireEvent.mouseDown(screen.getByTestId("tab-participant-invite-dialog"));
        fireEvent.click(screen.getByTestId("tab-participant-invite-close"));

        expect(onClose).not.toHaveBeenCalled();
        resolveAdd?.(true);
        await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    });
});
