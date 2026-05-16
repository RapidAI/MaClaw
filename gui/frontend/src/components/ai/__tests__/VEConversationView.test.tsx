import { act, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    VEConversationView,
    formatError,
    classifyAttachmentType,
    formatFileSize,
    fileNameFromPath,
    createSessionWithTimeout,
} from "../VEConversationView";
import type { VEConversationViewProps, VEConversationError } from "../VEConversationView";
import type { Theme } from "../aiAssistantPanelTheme";
import { SelectAIAssistantFiles } from "../../../../wailsjs/go/main/App";

// Mock Wails runtime
const eventHandlers = new Map<string, (...args: any[]) => void>();
vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((eventName: string, handler: (...args: any[]) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SelectAIAssistantFiles: vi.fn(async () => []),
}));

const mockTheme: Theme = {
    bg: "#fff",
    titleBarBg: "#f0f0f0",
    titleBarBorder: "#ddd",
    titleText: "#333",
    text: "#222",
    textMuted: "#888",
    inputBarBg: "#fff",
    inputBarBorder: "#6366f1",
    inputText: "#222",
    codeBg: "#f5f5f5",
    codeText: "#b5314a",
    codeBlockBg: "#f5f5f5",
    codeBlockBorder: "#ddd",
    codeBlockLang: "#888",
    borderLeft: "#6366f1",
    responseBorderLeft: "#22c55e",
    headingColor: "#111",
    linkColor: "#2563eb",
    pathColor: "#4ade80",
    promptColor: "#6366f1",
    userColor: "#2563eb",
    divider: "#e5e7eb",
    fieldBg: "#f9fafb",
    fieldBorder: "#d1d5db",
    fieldLabel: "#555",
    errorText: "#dc2626",
    errorBg: "#fef2f2",
    errorBorder: "#fecaca",
    emptyHint: "#888",
    boldColor: "#111",
    italicColor: "#555",
    bulletColor: "#6366f1",
    quoteBorder: "#d1d5db",
    quoteText: "#555",
    btnColor: "#6366f1",
    btnBorder: "#6366f1",
    actionBtnColor: "#6366f1",
    closeBtnColor: "#dc2626",
    sendBtnColor: "#6366f1",
    sendBtnBorder: "#6366f1",
    sendBtnBg: "#6366f1",
};

function renderConversation(overrides: Partial<VEConversationViewProps> = {}) {
    const initiate = vi.fn().mockResolvedValue({
        session_id: "test-session-1",
        ve_id: "ve-1",
        ve_name: "Test VE",
    });
    const send = vi.fn().mockResolvedValue(undefined);
    const sendWithAttachments = vi.fn().mockResolvedValue(undefined);
    const close = vi.fn().mockResolvedValue(undefined);

    const result = render(
        <VEConversationView
            veId="ve-1"
            veName="Test VE"
            theme={mockTheme}
            lang="zh"
            initiateConversation={initiate}
            sendMessage={send}
            sendMessageWithAttachments={sendWithAttachments}
            closeSession={close}
            {...overrides}
        />
    );

    return { ...result, initiate, send, sendWithAttachments, close };
}

describe("VEConversationView", () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useFakeTimers();
    });

    afterEach(() => {
        (SelectAIAssistantFiles as any).mockReset();
        (SelectAIAssistantFiles as any).mockImplementation(async () => []);
        vi.useRealTimers();
    });

    // --- Utility function tests ---

    describe("formatError", () => {
        it("formats hub_disconnected error in Chinese", () => {
            const err: VEConversationError = { type: "hub_disconnected", message: "" };
            expect(formatError(err, true)).toBe("Hub 连接中断");
        });

        it("formats ve_offline error in English", () => {
            const err: VEConversationError = { type: "ve_offline", message: "" };
            expect(formatError(err, false)).toBe("Digital employee is offline");
        });

        it("formats session_timeout error", () => {
            const err: VEConversationError = { type: "session_timeout", message: "" };
            expect(formatError(err, true)).toBe("会话创建超时（5秒）");
        });

        it("formats send_failed error", () => {
            const err: VEConversationError = { type: "send_failed", message: "" };
            expect(formatError(err, false)).toBe("Message send failed");
        });
    });

    describe("classifyAttachmentType", () => {
        it("classifies image files", () => {
            expect(classifyAttachmentType("photo.png")).toBe("image");
            expect(classifyAttachmentType("pic.jpg")).toBe("image");
            expect(classifyAttachmentType("anim.gif")).toBe("image");
        });

        it("classifies text files", () => {
            expect(classifyAttachmentType("readme.md")).toBe("text");
            expect(classifyAttachmentType("data.json")).toBe("text");
            expect(classifyAttachmentType("main.go")).toBe("text");
        });

        it("classifies document files", () => {
            expect(classifyAttachmentType("report.pdf")).toBe("file");
            expect(classifyAttachmentType("doc.docx")).toBe("file");
        });

        it("classifies unknown extensions as file", () => {
            expect(classifyAttachmentType("data.xyz")).toBe("file");
        });
    });

    describe("fileNameFromPath", () => {
        it("extracts file names from native paths", () => {
            expect(fileNameFromPath("D:\\cases\\evidence.pdf")).toBe("evidence.pdf");
            expect(fileNameFromPath("/tmp/report.md")).toBe("report.md");
        });
    });

    describe("formatFileSize", () => {
        it("formats bytes", () => {
            expect(formatFileSize(500)).toBe("500B");
        });

        it("formats kilobytes", () => {
            expect(formatFileSize(2048)).toBe("2.0KB");
        });

        it("formats megabytes", () => {
            expect(formatFileSize(5 * 1024 * 1024)).toBe("5.0MB");
        });
    });

    describe("createSessionWithTimeout", () => {
        it("resolves when promise completes before timeout", async () => {
            const result = await createSessionWithTimeout(
                Promise.resolve({ session_id: "s1" }),
                5000
            );
            expect(result).toEqual({ session_id: "s1" });
        });

        it("rejects with timeout error when promise takes too long", async () => {
            vi.useRealTimers(); // Need real timers for this test
            const slowPromise = new Promise((resolve) => setTimeout(resolve, 10000));
            await expect(
                createSessionWithTimeout(slowPromise, 50)
            ).rejects.toThrow("session_timeout");
        });
    });

    // --- Component rendering ---

    describe("session initialization", () => {
        it("renders the conversation view", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-conversation-view")).toBeTruthy();
        });

        it("calls initiateConversation on mount", async () => {
            const { initiate } = renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(initiate).toHaveBeenCalledWith("ve-1");
        });

        it("shows error banner on session creation failure", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("offline"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner")).toBeTruthy();
        });

        it("shows timeout error when session creation exceeds 5s", async () => {
            // Use a promise that never resolves to simulate timeout
            const initiate = vi.fn().mockImplementation(
                () => new Promise(() => {}) // never resolves
            );
            renderConversation({ initiateConversation: initiate });
            // Advance past the 5s timeout
            await act(async () => { vi.advanceTimersByTime(5100); });
            expect(screen.getByTestId("ve-error-banner")).toBeTruthy();
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("超时");
        });
    });

    // --- Message sending ---

    describe("message sending", () => {
        it("sends message on Enter key", async () => {
            const { send } = renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Hello VE" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(send).toHaveBeenCalledWith("test-session-1", "Hello VE");
        });

        it("adds user message to message list", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Test message" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            const msgList = screen.getByTestId("ve-message-list");
            expect(msgList.textContent).toContain("Test message");
        });

        it("does not send empty messages", async () => {
            const { send } = renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "   " } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(send).not.toHaveBeenCalled();
        });

        it("sends selected native file paths with attachment messages", async () => {
            (SelectAIAssistantFiles as any).mockResolvedValueOnce(["D:\\cases\\evidence.pdf"]);
            const { sendWithAttachments } = renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            fireEvent.click(screen.getByTestId("ve-attach-button"));
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-attachment-preview-bar").textContent).toContain("evidence.pdf");

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "See attachment" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendWithAttachments).toHaveBeenCalledWith("test-session-1", "See attachment", ["D:\\cases\\evidence.pdf"]);
        });

        it("preserves selected attachment paths when sending after reconnect", async () => {
            (SelectAIAssistantFiles as any).mockResolvedValueOnce(["D:\\cases\\queued.pdf"]);
            const { initiate, sendWithAttachments } = renderConversation({ existingSessionId: "test-session-1" });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            fireEvent.click(screen.getByTestId("ve-attach-button"));
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            await act(async () => {
                eventHandlers.get("ve:disconnected")?.({ session_id: "test-session-1" });
            });
            const textarea = screen.getByTestId("ve-input-textarea");
            await act(async () => {
                fireEvent.change(textarea, { target: { value: "Queued attachment" } });
                fireEvent.keyDown(textarea, { key: "Enter" });
            });

            expect(sendWithAttachments).not.toHaveBeenCalled();
            await act(async () => { vi.advanceTimersByTime(2000); });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            expect(initiate).toHaveBeenCalledWith("ve-1");
            expect(sendWithAttachments).toHaveBeenCalledWith("test-session-1", "Queued attachment", ["D:\\cases\\queued.pdf"]);
        });

        it("marks message as failed on send error", async () => {
            const send = vi.fn().mockRejectedValue(new Error("network error"));
            renderConversation({ sendMessage: send });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Will fail" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner")).toBeTruthy();
        });
    });

    // --- Streaming ---

    describe("streaming response", () => {
        it("shows streaming indicator on stream_chunk event", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    content: "Hello ",
                });
            });

            expect(screen.getByTestId("ve-streaming-indicator")).toBeTruthy();
            expect(screen.getByTestId("ve-streaming-indicator").textContent).toContain("Hello ");
        });

        it("accumulates stream chunks", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "Hello " });
            });
            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "world!" });
            });

            expect(screen.getByTestId("ve-streaming-indicator").textContent).toContain("Hello world!");
        });

        it("completes streaming on stream_end event", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "Complete response" });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({});
            });

            // Streaming indicator should be gone
            expect(screen.queryByTestId("ve-streaming-indicator")).toBeNull();
            // Message should be in the list
            const msgList = screen.getByTestId("ve-message-list");
            expect(msgList.textContent).toContain("Complete response");
        });
    });

    // --- Error states ---

    describe("error states", () => {
        it("displays hub disconnected error", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("hub disconnected"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("Hub 连接中断");
        });

        it("displays VE offline error", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("Digital employee is offline"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("数字员工当前不在线");
        });
    });

    // --- Input area ---

    describe("input area", () => {
        it("renders textarea and send button", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-input-textarea")).toBeTruthy();
            expect(screen.getByTestId("ve-send-button")).toBeTruthy();
        });

        it("renders attachment button", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-attach-button")).toBeTruthy();
        });

        it("clears input after sending", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "Hello" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(textarea.value).toBe("");
        });
    });
});
