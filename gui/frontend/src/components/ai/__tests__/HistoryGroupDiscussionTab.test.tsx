// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HistoryGroupDiscussionTab } from "../HistoryGroupDiscussionTab";
import { lightTheme } from "../aiAssistantPanelTheme";

const { getDetailMock, sendHistoryMessageMock, sendInvitationMock, downloadAttachmentMock, openFileMock, listVirtualEmployeesMock, eventHandlers } = vi.hoisted(() => ({
    getDetailMock: vi.fn(),
    sendHistoryMessageMock: vi.fn(),
    sendInvitationMock: vi.fn(),
    downloadAttachmentMock: vi.fn(),
    openFileMock: vi.fn(),
    listVirtualEmployeesMock: vi.fn(),
    eventHandlers: new Map<string, (payload?: any) => void>(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GroupDiscussionGetConsultationDetail: getDetailMock,
    GroupDiscussionSendHistoryMessage: sendHistoryMessageMock,
    GroupDiscussionSendInvitation: sendInvitationMock,
    GroupDiscussionDownloadAttachment: downloadAttachmentMock,
    ListVirtualEmployees: listVirtualEmployeesMock,
    OpenFileOrShowInFolder: openFileMock,
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: any) => void) => {
        eventHandlers.set(event, handler);
        return () => eventHandlers.delete(event);
    }),
    EventsOff: vi.fn((event: string) => eventHandlers.delete(event)),
}));

const theme = {
    ...lightTheme,
    bg: "#ffffff",
    text: "#111827",
    textMuted: "#6b7280",
    divider: "#e5e7eb",
    fieldBg: "#f3f4f6",
    fieldBorder: "#d1d5db",
    inputBarBg: "#f9fafb",
    inputText: "#111827",
    errorBg: "#fef2f2",
    errorText: "#dc2626",
    errorBorder: "#fecaca",
    sendBtnColor: "#2563eb",
    sendBtnBorder: "#1d4ed8",
    sendBtnBg: "#1d4ed8",
};

function detail(overrides: any = {}) {
    return {
        discussion: {
            id: "disc-1",
            topic: "Review",
            status: "open",
            participant_ids: ["me", "ve-a"],
            ...overrides.discussion,
        },
        session: overrides.session,
        messages: overrides.messages || [
            { id: "m1", from_id: "me", from_name: "Me", content: "hello", created_at: "2026-01-01T00:00:00Z" },
        ],
    };
}

beforeEach(() => {
    getDetailMock.mockResolvedValue(detail());
    sendHistoryMessageMock.mockResolvedValue(undefined);
    sendInvitationMock.mockResolvedValue("invite-1");
    listVirtualEmployeesMock.mockResolvedValue([
        { id: "ve-new", machine_id: "machine-new", name: "Contract Helper", online_status: "online" },
    ]);
    downloadAttachmentMock.mockResolvedValue({ local_path: "D:/maclaw/data/file.pdf" });
    openFileMock.mockResolvedValue(undefined);
    eventHandlers.clear();
});

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe("HistoryGroupDiscussionTab", () => {
    it("disables sending in read-only sessions", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Read only" readOnly={true} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Read-only session");
        expect((input as HTMLTextAreaElement).disabled).toBe(true);
        expect(screen.getAllByText("Read-only").length).toBeGreaterThan(0);
        expect(sendHistoryMessageMock).not.toHaveBeenCalled();
        expect(screen.queryByTestId("group-add-participant-btn")).toBeNull();
    });

    it("sends through the guarded history API for writable sessions", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...");
        fireEvent.change(input, { target: { value: "continue" } });
        fireEvent.click(screen.getByText("Send"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledTimes(1));
        expect(sendHistoryMessageMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({ content: "continue", kind: "statement" }));
    });

    it("sends history @mentions as targeted messages", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...");
        fireEvent.change(input, { target: { value: "@Contract Bot please continue" } });
        fireEvent.click(screen.getByText("Send"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledTimes(1));
        expect(sendHistoryMessageMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({
            content: "@Contract Bot please continue",
            to_ids: ["ve-a"],
        }));
    });

    it("routes localized local AI mentions in history messages", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "machine-1"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "machine-1", name: "Local AI" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="zh-CN" />);

        const input = await screen.findByPlaceholderText("\u7ee7\u7eed\u8ba8\u8bba...");
        fireEvent.change(input, { target: { value: "@\u672c\u673a AI \u8bf7\u5904\u7406" } });
        fireEvent.click(screen.getByText("\u53d1\u9001"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledTimes(1));
        expect(sendHistoryMessageMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({
            content: "@\u672c\u673a AI \u8bf7\u5904\u7406",
            to_ids: ["machine-1"],
        }));
    });

    it("routes normalized local AI ids in history messages", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", " LOCAL-MACLAW "] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: " LOCAL-MACLAW ", name: "Local AI" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="zh-CN" />);

        const input = await screen.findByPlaceholderText("\u7ee7\u7eed\u8ba8\u8bba...");
        fireEvent.change(input, { target: { value: "@\u672c\u673a AI \u8bf7\u5904\u7406" } });
        fireEvent.click(screen.getByText("\u53d1\u9001"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledTimes(1));
        expect(sendHistoryMessageMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({
            to_ids: ["LOCAL-MACLAW"],
        }));
    });

    it("keeps the composer inside the chat column beside the participant panel", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        await screen.findByPlaceholderText("Continue discussion...");
        const mainColumn = screen.getByTestId("history-group-main-column");
        const composer = screen.getByTestId("history-group-composer-row");
        const chatView = screen.getByTestId("ve-group-chat-view");

        expect(mainColumn.contains(composer)).toBe(true);
        expect((chatView as HTMLElement).style.flex).toBe("1 1 0%");
        expect((chatView as HTMLElement).style.minHeight).toBe("0px");
    });

    it("aligns the initiator on the right and other history participants on the left", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["human-a", "ve-a", "local-maclaw"] },
            session: { participants: [
                { id: "human-a", name: "Alice", role_code: "initiator" },
                { id: "ve-a", name: "Contract Bot", role_code: "speak" },
                { id: "local-maclaw", name: "Local AI", role_code: "speak" },
            ] },
            messages: [
                { id: "m1", from_id: "human-a", from_name: "Alice", content: "hello", created_at: "2026-01-01T00:00:00Z" },
                { id: "m2", from_id: "ve-a", from_name: "Contract Bot", content: "reviewed", created_at: "2026-01-01T00:00:01Z" },
                { id: "m3", from_id: "local-maclaw", from_name: "Local AI", content: "noted", created_at: "2026-01-01T00:00:02Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        await screen.findByText("hello");

        expect((screen.getByTestId("group-msg-m1") as HTMLElement).style.alignItems).toBe("flex-end");
        expect((screen.getByTestId("group-msg-m2") as HTMLElement).style.alignItems).toBe("flex-start");
        expect((screen.getByTestId("group-msg-m3") as HTMLElement).style.alignItems).toBe("flex-start");
    });

    it("keeps invited history sessions left-aligned even when a sender id is me", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "owned_ve_invited", readonly: true, participant_ids: ["me", "human-a"] },
            session: { participants: [
                { id: "me", name: "Local Digital Employee", role_code: "speak" },
                { id: "human-a", name: "Alice", role_code: "initiator" },
            ] },
            messages: [
                { id: "m-local-ve", from_id: "me", from_name: "Local Digital Employee", content: "reviewed", created_at: "2026-01-01T00:00:00Z" },
                { id: "m-initiator", from_id: "human-a", from_name: "Alice", content: "please review", created_at: "2026-01-01T00:00:01Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Read only" readOnly={true} theme={theme} lang="en" />);

        await screen.findByText("reviewed");

        expect((screen.getByTestId("group-msg-m-local-ve") as HTMLElement).style.alignItems).toBe("flex-start");
        expect((screen.getByTestId("group-msg-m-initiator") as HTMLElement).style.alignItems).toBe("flex-start");
    });

    it("opens already downloaded attachments without downloading again", async () => {
        getDetailMock.mockResolvedValue(detail({
            messages: [{
                id: "m2",
                from_id: "ve-a",
                from_name: "VE",
                content: "attachment",
                created_at: "2026-01-01T00:00:00Z",
                file_attachments: [{ filename: "local.pdf", local_path: "D:/maclaw/data/local.pdf" }],
            }],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Attachments" readOnly={true} theme={theme} lang="en" />);

        fireEvent.click(await screen.findByText("local.pdf"));

        await waitFor(() => expect(openFileMock).toHaveBeenCalledWith("D:/maclaw/data/local.pdf"));
        expect(downloadAttachmentMock).not.toHaveBeenCalled();
    });

    it("downloads remote attachments and records the returned local path", async () => {
        getDetailMock.mockResolvedValue(detail({
            messages: [{
                id: "m3",
                from_id: "ve-a",
                from_name: "VE",
                content: "attachment",
                created_at: "2026-01-01T00:00:00Z",
                file_attachments: [{ filename: "remote.pdf", file_url: "/api/ve/files/file-1" }],
            }],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Attachments" readOnly={true} theme={theme} lang="en" />);

        fireEvent.click(await screen.findByText("remote.pdf"));

        await waitFor(() => expect(downloadAttachmentMock).toHaveBeenCalledWith("disc-1", "/api/ve/files/file-1", "remote.pdf"));
        expect(openFileMock).not.toHaveBeenCalled();
    });

    it("shows named participants and roles for review context", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { participant_ids: ["me", "ve-a", "external-lawyer"] },
            session: {
                participants: [
                    { id: "me", name: "Alice", role_code: "initiator" },
                    { id: "ve-a", name: "Contract Bot", role_code: "review" },
                    { id: "external-lawyer", role_code: "observe" },
                ],
            },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("Participants")).toBeTruthy();
        expect(screen.getByText("Alice (initiator)")).toBeTruthy();
        expect(screen.getByText("Contract Bot (review)")).toBeTruthy();
        expect(screen.getByText("Participant 3 (observe)")).toBeTruthy();
        expect(screen.queryByText("external-lawyer (observe)")).toBeNull();
    });

    it("uses participant names for message labels when messages only contain ids", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
            messages: [
                { id: "m1", from_id: "me", content: "hello", created_at: "2026-01-01T00:00:00Z" },
                { id: "m2", from_id: "ve-a", content: "reviewed", created_at: "2026-01-01T00:00:01Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="en" />);

        await screen.findByText("hello");
        expect(screen.getAllByText("Alice").length).toBeGreaterThan(0);
        expect(screen.getAllByText("Contract Bot").length).toBeGreaterThan(0);
        expect(screen.queryByText("ve-a")).toBeNull();
    });

    it("ignores raw message from_name values when resolving message labels", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { participant_ids: ["m_b1821505498d817c"] },
            session: { participants: [{ id: "m_b1821505498d817c", name: "Contract Bot" }] },
            messages: [
                { id: "m1", from_id: "m_b1821505498d817c", from_name: "m_b1821505498d817c", content: "reviewed", created_at: "2026-01-01T00:00:00Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="en" />);

        await screen.findByText("reviewed");
        expect(screen.getAllByText("Contract Bot").length).toBeGreaterThan(0);
        expect(screen.queryByText(/m_b182/)).toBeNull();
    });

    it("uses friendly participant labels when session names equal raw ids", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { participant_ids: ["m_b1821505498d817c"] },
            session: { participants: [{ id: "m_b1821505498d817c", name: "m_b1821505498d817c" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("Participant 1")).toBeTruthy();
        expect(screen.queryByText(/m_b182/)).toBeNull();
    });
    it("uses friendly participant labels instead of raw ids when names are unavailable", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { participant_ids: ["m_b1821505498d817c", "m_d64c1196e8d03c53"] },
            session: { participants: [{ id: "m_b1821505498d817c" }, { id: "m_d64c1196e8d03c53", role_code: "speak" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="zh" />);

        expect(await screen.findByText("参与者 1")).toBeTruthy();
        expect(screen.getByText("参与者 2 (speak)")).toBeTruthy();
        expect(screen.queryByText(/m_b182/)).toBeNull();
        expect(screen.queryByText(/m_d64/)).toBeNull();
    });

    it("marks the unified participant panel read-only for invited history sessions", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "owned_ve_invited", readonly: true, participant_ids: ["me", "ve-a"] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Participants" readOnly={true} theme={theme} lang="en" />);

        expect((await screen.findByTestId("group-participant-panel")).textContent).toContain("Read-only");
    });

    it("lets writable history sessions insert mentions from the unified participant panel", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.contextMenu(screen.getByText("Contract Bot"));
        fireEvent.click(await screen.findByTestId("context-menu-talk-to"));

        expect(input.value).toBe("@Contract Bot ");
    });

    it("inserts history mention at the current caret position", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "hello world" } });
        input.setSelectionRange(5, 5);
        fireEvent.contextMenu(screen.getByText("Contract Bot"));
        fireEvent.click(await screen.findByTestId("context-menu-talk-to"));

        expect(input.value).toBe("hello @Contract Bot world");
    });

    it("mentions the participant name without the displayed role suffix", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice", role_code: "initiator" }, { id: "ve-a", name: "Contract Bot", role_code: "review" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.contextMenu(screen.getByText("Contract Bot (review)"));
        fireEvent.click(await screen.findByTestId("context-menu-talk-to"));

        expect(input.value).toBe("@Contract Bot ");
    });


    it("closes the mention popover when history detail becomes read-only", async () => {
        getDetailMock
            .mockResolvedValueOnce(detail({
                discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
                session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
            }))
            .mockResolvedValueOnce(detail({
                discussion: { status: "closed", participant_ids: ["me", "ve-a"] },
                session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
            }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "@Con", selectionStart: 4 } });
        expect(screen.getByTestId("mention-popover")).toBeTruthy();

        fireEvent.click(screen.getByText("Refresh"));

        await screen.findByPlaceholderText("Read-only session");
        expect(screen.queryByTestId("mention-popover")).toBeNull();
    });

    it("shows participant suggestions when typing @ in writable history sessions", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "@Con", selectionStart: 4 } });

        expect(screen.getByTestId("mention-popover")).toBeTruthy();
        expect(screen.getByTestId("mention-item-ve-a")).toBeTruthy();
        expect(screen.queryByTestId("mention-item-me")).toBeNull();

        fireEvent.keyDown(input, { key: "Enter" });
        expect(input.value).toBe("@Contract Bot ");
        expect(sendHistoryMessageMock).not.toHaveBeenCalled();
    });

    it("does not match history @ suggestions by raw participant ids", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["m_b1821505498d817c"] },
            session: { participants: [{ id: "m_b1821505498d817c", name: "Participant 1" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "@m_b", selectionStart: 4 } });

        expect(screen.queryByTestId("mention-popover")).toBeNull();
    });

    it("inserts a fresh history mention every time the participant talk-to action is used", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...") as HTMLTextAreaElement;
        fireEvent.contextMenu(screen.getByText("Contract Bot"));
        fireEvent.click(await screen.findByTestId("context-menu-talk-to"));
        fireEvent.contextMenu(screen.getByText("Contract Bot"));
        fireEvent.click(await screen.findByTestId("context-menu-talk-to"));

        expect(input.value).toBe("@Contract Bot @Contract Bot ");
    });

    it("lets writable history sessions invite participants from the unified participant panel", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        await screen.findByPlaceholderText("Continue discussion...");
        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Contract Helper")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Helper"));

        await waitFor(() => expect(sendInvitationMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({ to_id: "machine-new", role: "speak", trusted: true })));
        await waitFor(() => expect(getDetailMock).toHaveBeenCalledTimes(2));
    });

    it("keeps the participant picker open when history invitation fails", async () => {
        sendInvitationMock.mockRejectedValueOnce(new Error("invite failed"));
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
            session: { participants: [{ id: "me", name: "Alice" }, { id: "ve-a", name: "Contract Bot" }] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        await screen.findByPlaceholderText("Continue discussion...");
        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Contract Helper")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Helper"));

        await waitFor(() => expect(screen.getByTestId("group-limit-error").textContent).toContain("Failed to add"));
        expect(screen.getByTestId("group-participant-picker")).toBeTruthy();
        expect(screen.getByRole("alert").textContent).toContain("invite failed");
    });

    it("reloads the discussion after sending a writable history message", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...");
        fireEvent.change(input, { target: { value: "follow up" } });
        fireEvent.click(screen.getByText("Send"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(getDetailMock).toHaveBeenCalledTimes(2));
        expect((input as HTMLTextAreaElement).value).toBe("");
    });

    it("opens a downloaded remote attachment locally on the next click", async () => {
        getDetailMock.mockResolvedValue(detail({
            messages: [{
                id: "m4",
                from_id: "ve-a",
                from_name: "VE",
                content: "attachment",
                created_at: "2026-01-01T00:00:00Z",
                file_attachments: [{ filename: "remote-again.pdf", file_url: "/api/ve/files/file-2" }],
            }],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Attachments" readOnly={true} theme={theme} lang="en" />);

        const attachment = await screen.findByText("remote-again.pdf");
        fireEvent.click(attachment);
        await waitFor(() => expect(downloadAttachmentMock).toHaveBeenCalledWith("disc-1", "/api/ve/files/file-2", "remote-again.pdf"));

        downloadAttachmentMock.mockClear();
        fireEvent.click(attachment);

        await waitFor(() => expect(openFileMock).toHaveBeenCalledWith("D:/maclaw/data/file.pdf"));
        expect(downloadAttachmentMock).not.toHaveBeenCalled();
    });


    it("keeps initiator-role details read-only when local relation is missing", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", role: "initiator", readonly: false, participant_ids: ["me", "ve-a"] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Missing relation" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Read-only session");
        expect((input as HTMLTextAreaElement).disabled).toBe(true);
        fireEvent.change(input, { target: { value: "should not send" } });
        fireEvent.click(screen.getByText("Send"));
        expect(sendHistoryMessageMock).not.toHaveBeenCalled();
    });

    it("lets authoritative initiated detail override a stale read-only summary", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "open", local_relation: "initiated_by_me", readonly: false, participant_ids: ["me", "ve-a"] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Writable detail" readOnly={true} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Continue discussion...");
        expect((input as HTMLTextAreaElement).disabled).toBe(false);
        fireEvent.change(input, { target: { value: "resume from history" } });
        fireEvent.click(screen.getByText("Send"));

        await waitFor(() => expect(sendHistoryMessageMock).toHaveBeenCalledWith("disc-1", expect.objectContaining({ content: "resume from history" })));
    });
    it("treats closed detail sessions as read-only even when opened from stale writable summary", async () => {
        getDetailMock.mockResolvedValue(detail({
            discussion: { status: "closed", participant_ids: ["me", "ve-a"] },
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Closed" readOnly={false} theme={theme} lang="en" />);

        const input = await screen.findByPlaceholderText("Read-only session");
        expect((input as HTMLTextAreaElement).disabled).toBe(true);
        expect(screen.getAllByText("Read-only").length).toBeGreaterThan(0);
        expect(screen.getByText("Ended - read-only")).toBeTruthy();
        fireEvent.change(input, { target: { value: "should not send" } });
        fireEvent.click(screen.getByText("Send"));
        expect(sendHistoryMessageMock).not.toHaveBeenCalled();
    });


    it("keeps the newest detail when overlapping reloads finish out of order", async () => {
        let resolveFirst: (value: any) => void = () => {};
        let resolveSecond: (value: any) => void = () => {};
        getDetailMock
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
            .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve; }));

        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Live history" readOnly={true} theme={theme} lang="en" />);
        act(() => {
            eventHandlers.get("ve-event")?.({ payload: { session_id: "disc-1", message: { session_id: "disc-1", kind: "answer" } } });
        });

        act(() => { resolveSecond(detail({ messages: [{ id: "new", from_id: "ve-a", content: "new detail", created_at: "2026-01-01T00:00:01Z" }] })); });
        expect(await screen.findByText("new detail")).toBeTruthy();
        expect((screen.getByText("Refresh") as HTMLButtonElement).disabled).toBe(false);

        act(() => { resolveFirst(detail({ messages: [{ id: "old", from_id: "ve-a", content: "old detail", created_at: "2026-01-01T00:00:00Z" }] })); });
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(screen.getByText("new detail")).toBeTruthy();
        expect(screen.queryByText("old detail")).toBeNull();
    });

    it("waits for stream_end before reloading streamed history updates", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Live history" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("hello")).toBeTruthy();
        act(() => {
            eventHandlers.get("ve-event")?.({ payload: { session_id: "disc-1", message: { session_id: "disc-1", kind: "stream_chunk" } } });
        });
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(getDetailMock).toHaveBeenCalledTimes(1);

        act(() => {
            eventHandlers.get("ve:stream_end")?.({ session_id: "disc-1" });
        });
        await waitFor(() => expect(getDetailMock).toHaveBeenCalledTimes(2));
    });

    it("reloads an open history tab when a matching pushed discussion event arrives", async () => {
        getDetailMock
            .mockResolvedValueOnce(detail())
            .mockResolvedValueOnce(detail({ messages: [
                { id: "m1", from_id: "me", from_name: "Me", content: "hello", created_at: "2026-01-01T00:00:00Z" },
                { id: "m2", from_id: "ve-a", from_name: "VE", content: "updated", created_at: "2026-01-01T00:00:01Z" },
            ] }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Live history" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("hello")).toBeTruthy();
        act(() => {
            eventHandlers.get("ve-event")?.({ payload: { session_id: "disc-1", message: { session_id: "disc-1" } } });
        });

        await waitFor(() => expect(getDetailMock).toHaveBeenCalledTimes(2));
        expect(await screen.findByText("updated")).toBeTruthy();
    });

    it("reloads when pushed events use discussion_id instead of session_id", async () => {
        getDetailMock
            .mockResolvedValueOnce(detail())
            .mockResolvedValueOnce(detail({ messages: [
                { id: "m1", from_id: "me", from_name: "Me", content: "hello", created_at: "2026-01-01T00:00:00Z" },
                { id: "m2", from_id: "ve-a", from_name: "VE", content: "discussion id update", created_at: "2026-01-01T00:00:01Z" },
            ] }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Live history" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("hello")).toBeTruthy();
        act(() => {
            eventHandlers.get("ve-event")?.({ payload: { discussion_id: "disc-1", message: { discussion_id: "disc-1" } } });
        });

        await waitFor(() => expect(getDetailMock).toHaveBeenCalledTimes(2));
        expect(await screen.findByText("discussion id update")).toBeTruthy();
    });

    it("ignores pushed discussion events for other sessions", async () => {
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Live history" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("hello")).toBeTruthy();
        act(() => {
            eventHandlers.get("ve-event")?.({ payload: { session_id: "disc-other" } });
        });

        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(getDetailMock).toHaveBeenCalledTimes(1);
    });

    it("coalesces streamed history chunks and hides stream end markers", async () => {
        getDetailMock.mockResolvedValue(detail({
            messages: [
                { id: "s1", from_id: "ve-a", from_name: "VE", kind: "stream_chunk", content: "Hel", created_at: "2026-01-01T00:00:00Z" },
                { id: "s2", from_id: "ve-a", from_name: "VE", kind: "stream_chunk", content: "lo", created_at: "2026-01-01T00:00:01Z" },
                { id: "s3", from_id: "ve-a", from_name: "VE", kind: "stream_end", content: "", created_at: "2026-01-01T00:00:02Z" },
                { id: "m2", from_id: "me", from_name: "Me", kind: "statement", content: "next", created_at: "2026-01-01T00:00:03Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Streamed" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("Hello")).toBeTruthy();
        expect(screen.getByText("next")).toBeTruthy();
        expect(screen.queryByTestId("group-msg-s2")).toBeNull();
        expect(screen.queryByTestId("group-msg-s3")).toBeNull();
    });

    it("keeps separate streamed replies after a stream end", async () => {
        getDetailMock.mockResolvedValue(detail({
            messages: [
                { id: "s1", from_id: "ve-a", from_name: "VE", kind: "stream_chunk", content: "First", created_at: "2026-01-01T00:00:00Z" },
                { id: "s2", from_id: "ve-a", from_name: "VE", kind: "stream_end", content: "", created_at: "2026-01-01T00:00:01Z" },
                { id: "s3", from_id: "ve-a", from_name: "VE", kind: "stream_chunk", content: "Second", created_at: "2026-01-01T00:00:02Z" },
                { id: "s4", from_id: "ve-a", from_name: "VE", kind: "stream_end", content: "", created_at: "2026-01-01T00:00:03Z" },
            ],
        }));
        render(<HistoryGroupDiscussionTab discussionId="disc-1" title="Streamed" readOnly={true} theme={theme} lang="en" />);

        expect(await screen.findByText("First")).toBeTruthy();
        expect(screen.getByText("Second")).toBeTruthy();
        expect(screen.queryByText("FirstSecond")).toBeNull();
    });
});
