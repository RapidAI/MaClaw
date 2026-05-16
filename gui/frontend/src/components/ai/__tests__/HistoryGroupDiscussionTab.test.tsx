// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HistoryGroupDiscussionTab } from "../HistoryGroupDiscussionTab";
import { lightTheme } from "../aiAssistantPanelTheme";

const { getDetailMock, sendHistoryMessageMock, downloadAttachmentMock, openFileMock, eventHandlers } = vi.hoisted(() => ({
    getDetailMock: vi.fn(),
    sendHistoryMessageMock: vi.fn(),
    downloadAttachmentMock: vi.fn(),
    openFileMock: vi.fn(),
    eventHandlers: new Map<string, (payload?: any) => void>(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GroupDiscussionGetConsultationDetail: getDetailMock,
    GroupDiscussionSendHistoryMessage: sendHistoryMessageMock,
    GroupDiscussionDownloadAttachment: downloadAttachmentMock,
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
        expect(screen.getByText("Read-only")).toBeTruthy();
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
        expect(screen.getByText("external-lawyer (observe)")).toBeTruthy();
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
        expect(screen.getByText("Read-only")).toBeTruthy();
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
