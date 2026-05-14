// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HistoryGroupDiscussionTab } from "../HistoryGroupDiscussionTab";
import { lightTheme } from "../aiAssistantPanelTheme";

const { getDetailMock, sendHistoryMessageMock, downloadAttachmentMock, openFileMock } = vi.hoisted(() => ({
    getDetailMock: vi.fn(),
    sendHistoryMessageMock: vi.fn(),
    downloadAttachmentMock: vi.fn(),
    openFileMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GroupDiscussionGetConsultationDetail: getDetailMock,
    GroupDiscussionSendHistoryMessage: sendHistoryMessageMock,
    GroupDiscussionDownloadAttachment: downloadAttachmentMock,
    OpenFileOrShowInFolder: openFileMock,
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((_event: string, _handler: unknown) => vi.fn()),
    EventsOff: vi.fn(),
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
});
