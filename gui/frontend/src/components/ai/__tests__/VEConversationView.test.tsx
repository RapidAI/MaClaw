import { createRef } from "react";
import { act, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    VEConversationView,
    formatError,
    classifyAttachmentType,
    formatFileSize,
    fileNameFromPath,
    createSessionWithTimeout,
    classifySessionInitError,
} from "../VEConversationView";
import type { VEConversationViewProps, VEConversationError, VEConversationHandle } from "../VEConversationView";
import type { Theme } from "../aiAssistantPanelTheme";
import { GroupDiscussionAttachmentPreviewDataURL, GroupDiscussionDownloadAttachment, GroupDiscussionGetConsultationDetail, OpenFileOrShowInFolder, SelectAIAssistantFiles } from "../../../../wailsjs/go/main/App";

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
    GroupDiscussionDownloadAttachment: vi.fn(async () => ({ local_path: "C:\\tmp\\report.pdf" })),
    GroupDiscussionAttachmentPreviewDataURL: vi.fn(async () => "data:image/png;base64,abc123"),
    GroupDiscussionGetConsultationDetail: vi.fn(async () => null),
    OpenFileOrShowInFolder: vi.fn(async () => undefined),
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
        (GroupDiscussionDownloadAttachment as any).mockReset();
        (GroupDiscussionDownloadAttachment as any).mockImplementation(async () => ({ local_path: "C:\\tmp\\report.pdf" }));
        (GroupDiscussionAttachmentPreviewDataURL as any).mockReset();
        (GroupDiscussionAttachmentPreviewDataURL as any).mockImplementation(async () => "data:image/png;base64,abc123");
        (GroupDiscussionGetConsultationDetail as any).mockReset();
        (GroupDiscussionGetConsultationDetail as any).mockImplementation(async () => null);
        (OpenFileOrShowInFolder as any).mockReset();
        (OpenFileOrShowInFolder as any).mockImplementation(async () => undefined);
        vi.useRealTimers();
    });

    it("uses a friendly direct VE name when the tab title is a raw id", () => {
        renderConversation({
            existingSessionId: "test-session-1",
            lang: "en",
            veId: "m_b1821505498d817c",
            veName: "m_b1821505498d817c",
            initialMessages: [{
                id: "raw-ve-name-msg",
                role: "assistant",
                content: "hello",
                timestamp: 1,
            }],
        });

        const list = screen.getByTestId("ve-message-list");
        expect(list.textContent).toContain("Digital employee");
        expect(list.textContent).not.toContain("m_b1821505498d817c");
        expect(screen.getByTestId("ve-input-textarea").getAttribute("placeholder")).toBe("Message Digital employee...");
    });

    it("uses the same wrapping rules for direct digital employee replies", () => {
        renderConversation({
            existingSessionId: "test-session-1",
            initialMessages: [{
                id: "wrap-1",
                role: "assistant",
                content: "一段很长的数字员工回复WithoutSpacesWithoutSpacesWithoutSpacesWithoutSpaces\n第二行",
                timestamp: 1,
            }],
        });

        const bubble = screen.getByTestId("ve-msg-content-wrap-1") as HTMLElement;
        expect(bubble.style.overflowWrap).toBe("anywhere");
        expect(bubble.style.whiteSpace).toBe("pre-wrap");
        expect(screen.getByText("第二行")).toBeTruthy();
    });

    it("renders compact markdown headings in digital employee replies as separate lines", () => {
        renderConversation({
            existingSessionId: "test-session-1",
            initialMessages: [{
                id: "weather-1",
                role: "assistant",
                content: "北京天气：####\u{1f4c5}今天\n晴天 0%####\u{1f4c5}明天\n多云",
                timestamp: 1,
            }],
        });

        expect(screen.getByText("北京天气：")).toBeTruthy();
        expect(screen.getByText("\u{1f4c5}今天")).toBeTruthy();
        expect(screen.getByText("晴天 0%")).toBeTruthy();
        expect(screen.getByText("\u{1f4c5}明天")).toBeTruthy();
        expect(screen.getByText("多云")).toBeTruthy();
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
            expect(formatError(err, true)).toBe("会话创建超时（15秒）");
        });

        it("formats send_failed error", () => {
            const err: VEConversationError = { type: "send_failed", message: "" };
            expect(formatError(err, false)).toBe("Message send failed");
        });

        it("includes backend detail for send_failed errors", () => {
            const err: VEConversationError = { type: "send_failed", message: "MESSAGE_DELIVERY_FAILED" };
            expect(formatError(err, true)).toBe("消息发送失败：MESSAGE_DELIVERY_FAILED");
            expect(formatError(err, false)).toBe("Message send failed: MESSAGE_DELIVERY_FAILED");
        });

        it("formats access confirmation states", () => {
            expect(formatError({ type: "auth_pending", message: "" }, false)).toBe("Waiting for access confirmation");
            expect(formatError({ type: "access_denied", message: "denied" }, false)).toBe("Access was not approved");
            expect(formatError({ type: "access_denied", message: "blocked" }, false)).toBe("This digital employee is unavailable");
        });

        it("classifies per-request confirmation as auth pending", () => {
            expect(classifySessionInitError(new Error("pending_confirmation"))).toBe("auth_pending");
            expect(classifySessionInitError(new Error("hub returned 202: waiting for digital employee owner confirmation"))).toBe("auth_pending");
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

        it("clears the timeout timer after a fast session resolves", async () => {
            const result = await createSessionWithTimeout(Promise.resolve("ready"), 5000);

            expect(result).toBe("ready");
            expect(vi.getTimerCount()).toBe(0);
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

        it("notifies parent when a session id is established", async () => {
            const onSessionIdChange = vi.fn();
            renderConversation({ onSessionIdChange });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(onSessionIdChange).toHaveBeenCalledWith("test-session-1");
        });

        it("shows error banner on session creation failure", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("offline"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner")).toBeTruthy();
        });

        it("shows waiting state when first access needs confirmation", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("pending_confirmation"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("等待对方确认");
        });

        it("retries session creation after access is allowed", async () => {
            const initiate = vi
                .fn()
                .mockRejectedValueOnce(new Error("pending_confirmation"))
                .mockResolvedValueOnce({ session_id: "allowed-session", ve_id: "ve-1", ve_name: "Test VE" });
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });
            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-1", decision: "allow_once", status: "allowed" } });
            });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(initiate).toHaveBeenCalledTimes(2);
            expect(screen.queryByTestId("ve-error-banner")).toBeNull();
        });

        it("does not recreate an existing session on duplicate access allow events", async () => {
            const { initiate } = renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-1", decision: "allow_once", status: "allowed" } });
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(1);
            expect(screen.queryByTestId("ve-error-banner")).toBeNull();
        });

        it("coalesces rapid duplicate access allow retries while session creation is pending", async () => {
            let resolveAllowed!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            const allowedSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => {
                resolveAllowed = resolve;
            });
            const initiate = vi
                .fn()
                .mockRejectedValueOnce(new Error("pending_confirmation"))
                .mockReturnValueOnce(allowedSession);
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-1", decision: "allow_once", status: "allowed" } });
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-1", decision: "allow_once", status: "allowed" } });
            });

            expect(initiate).toHaveBeenCalledTimes(2);
            resolveAllowed({ session_id: "allowed-session", ve_id: "ve-1", ve_name: "Test VE" });
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(initiate).toHaveBeenCalledTimes(2);
        });

        it("matches access result by target machine id", async () => {
            const initiate = vi
                .fn()
                .mockRejectedValueOnce(new Error("pending_confirmation"))
                .mockResolvedValueOnce({ session_id: "machine-session", ve_id: "ve_machine-a", ve_name: "Machine VE" });
            renderConversation({ veId: "machine-a", initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve_machine-a", target_machine_id: "machine-a", decision: "allow_once", status: "allowed" } });
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(2);
            expect(screen.queryByTestId("ve-error-banner")).toBeNull();
        });

        it("matches machine id auth result when opened by generated ve id", async () => {
            const initiate = vi
                .fn()
                .mockRejectedValueOnce(new Error("pending_confirmation"))
                .mockResolvedValueOnce({ session_id: "generated-session", ve_id: "ve_machine-a", ve_name: "Machine VE" });
            renderConversation({ veId: "ve_machine-a", initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_machine_id: "machine-a", decision: "allow_once", status: "allowed" } });
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(2);
            expect(screen.queryByTestId("ve-error-banner")).toBeNull();
        });

        it("ignores access result events that do not target this digital employee", async () => {
            const initiate = vi.fn().mockRejectedValue(new Error("pending_confirmation"));
            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { decision: "allow_once", status: "allowed" } });
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-other", decision: "allow_once", status: "allowed" } });
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(1);
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("等待对方确认");
        });

        it("keeps access denied when a stale session creation resolves after denial", async () => {
            let resolveSession!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            const pendingSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => {
                resolveSession = resolve;
            });
            const initiate = vi.fn().mockReturnValueOnce(pendingSession);
            const onSessionIdChange = vi.fn();
            renderConversation({ initiateConversation: initiate, onSessionIdChange });
            await act(async () => { await Promise.resolve(); });

            act(() => {
                eventHandlers.get("ve:auth_result")?.({ payload: { target_ve_id: "ve-1", decision: "deny", status: "denied" } });
            });
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("访问未通过");

            resolveSession({ session_id: "stale-session", ve_id: "ve-1", ve_name: "Test VE" });
            await act(async () => { await Promise.resolve(); });

            expect(onSessionIdChange).not.toHaveBeenCalledWith("stale-session");
            expect(screen.getByTestId("ve-error-banner").textContent).toContain("访问未通过");
        });

        it("shows timeout error when session creation exceeds 15s", async () => {
            // Use a promise that never resolves to simulate timeout
            const initiate = vi.fn().mockImplementation(
                () => new Promise(() => {}) // never resolves
            );
            renderConversation({ initiateConversation: initiate });
            // Advance past the 15s timeout
            await act(async () => { vi.advanceTimersByTime(15100); });
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

        it("ignores duplicate Enter presses while a connected send is in flight", async () => {
            let resolveSend: (() => void) | undefined;
            const send = vi.fn(() => new Promise<void>((resolve) => { resolveSend = resolve; }));
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Send once" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            fireEvent.keyDown(textarea, { key: "Enter" });

            expect(send).toHaveBeenCalledTimes(1);
            await act(async () => { resolveSend?.(); });
        });

        it("ignores duplicate Enter presses while queueing before session ready", async () => {
            let resolveInit: (value: { session_id: string; ve_id: string; ve_name: string }) => void = () => {};
            const initiate = vi.fn(() => new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveInit = resolve; }));
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ initiateConversation: initiate, sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Queue once" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => {
                resolveInit({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
                await vi.runOnlyPendingTimersAsync();
            });

            expect(send).toHaveBeenCalledTimes(1);
            expect(screen.getAllByText("Queue once")).toHaveLength(1);
        });

        it("queues the next user input until the current assistant stream ends", async () => {
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });
            expect(send).toHaveBeenCalledTimes(1);
            expect(send).toHaveBeenLastCalledWith("test-session-1", "First turn");

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(send).toHaveBeenCalledTimes(1);
            expect(screen.getAllByText("Second turn").length).toBeGreaterThan(0);

            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { await Promise.resolve(); });

            expect(send).toHaveBeenCalledTimes(2);
            expect(send).toHaveBeenLastCalledWith("test-session-1", "Second turn");
        });

        it("shows a thinking hint after sending until the first response chunk arrives", async () => {
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Slow reply" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(screen.getByTestId("ve-thinking-indicator").textContent).toContain("思考");

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ session_id: "test-session-1", content: "First chunk" });
            });

            expect(screen.queryByTestId("ve-thinking-indicator")).toBeNull();
            expect(screen.getByTestId("ve-streaming-indicator").textContent).toContain("First chunk");
        });

        it("shows a visible pre-input queue while waiting for the current reply", async () => {
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(screen.getByTestId("ve-queued-message-panel").textContent).toContain("Second turn");

            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { await Promise.resolve(); });

            expect(screen.queryByTestId("ve-queued-message-panel")).toBeNull();
        });

        it("keeps queued input below the previous assistant reply in the transcript", async () => {
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            const messageListBeforeEnd = screen.getByTestId("ve-message-list");
            expect(messageListBeforeEnd.textContent).toContain("First turn");
            expect(messageListBeforeEnd.textContent).not.toContain("Second turn");
            expect(screen.getByTestId("ve-queued-message-panel").textContent).toContain("Second turn");

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ session_id: "test-session-1", content: "Reply one" });
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { await Promise.resolve(); });

            const transcript = screen.getByTestId("ve-message-list").textContent || "";
            expect(transcript.indexOf("First turn")).toBeLessThan(transcript.indexOf("Reply one"));
            expect(transcript.indexOf("Reply one")).toBeLessThan(transcript.indexOf("Second turn"));
        });

        it("drains multiple queued inputs one reply boundary at a time", async () => {
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(textarea, { target: { value: "Third turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(send).toHaveBeenCalledTimes(1);
            expect(screen.getByTestId("ve-message-list").textContent).not.toContain("Second turn");
            expect(screen.getByTestId("ve-message-list").textContent).not.toContain("Third turn");
            expect(screen.getByTestId("ve-queued-message-panel").textContent).toContain("Second turn");
            expect(screen.getByTestId("ve-queued-message-panel").textContent).toContain("Third turn");

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ session_id: "test-session-1", content: "Reply one" });
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { await Promise.resolve(); });

            expect(send).toHaveBeenCalledTimes(2);
            expect(send).toHaveBeenLastCalledWith("test-session-1", "Second turn");
            expect(screen.getByTestId("ve-message-list").textContent).toContain("Second turn");
            expect(screen.getByTestId("ve-message-list").textContent).not.toContain("Third turn");
            expect(screen.getByTestId("ve-queued-message-panel").textContent).toContain("Third turn");

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ session_id: "test-session-1", content: "Reply two" });
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { await Promise.resolve(); });

            const transcript = screen.getByTestId("ve-message-list").textContent || "";
            expect(send).toHaveBeenCalledTimes(3);
            expect(send).toHaveBeenLastCalledWith("test-session-1", "Third turn");
            expect(transcript.indexOf("Reply one")).toBeLessThan(transcript.indexOf("Second turn"));
            expect(transcript.indexOf("Reply two")).toBeLessThan(transcript.indexOf("Third turn"));
            expect(screen.queryByTestId("ve-queued-message-panel")).toBeNull();
        });

        it("does not re-arm the response gate when stream end arrives before send resolves", async () => {
            let resolveFirstSend: (() => void) | undefined;
            const send = vi
                .fn()
                .mockImplementationOnce(() => new Promise<void>((resolve) => { resolveFirstSend = resolve; }))
                .mockResolvedValue(undefined);
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });
            await act(async () => { resolveFirstSend?.(); });

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(send).toHaveBeenCalledTimes(2);
            expect(send).toHaveBeenLastCalledWith("test-session-1", "Second turn");
        });

        it("does not update queue state after unmounting with an in-flight send", async () => {
            let resolveSend: (() => void) | undefined;
            const send = vi.fn(() => new Promise<void>((resolve) => { resolveSend = resolve; }));
            const { unmount } = renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "In flight" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            unmount();
            await act(async () => { resolveSend?.(); });

            expect(send).toHaveBeenCalledTimes(1);
        });

        it("releases queued input after reconnecting from an interrupted assistant stream", async () => {
            const initiate = vi
                .fn()
                .mockResolvedValueOnce({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" })
                .mockResolvedValueOnce({ session_id: "test-session-2", ve_id: "ve-1", ve_name: "Test VE" });
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ initiateConversation: initiate, sendMessage: send });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "First turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ session_id: "test-session-1", content: "Partial" });
            });
            expect(screen.getByTestId("ve-streaming-indicator")).toBeTruthy();

            act(() => {
                eventHandlers.get("ve:disconnected")?.({ session_id: "test-session-1" });
            });
            expect(screen.queryByTestId("ve-streaming-indicator")).toBeNull();

            fireEvent.change(textarea, { target: { value: "Second turn" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => {
                vi.advanceTimersByTime(2000);
                await vi.runOnlyPendingTimersAsync();
            });

            expect(send).toHaveBeenCalledTimes(2);
            expect(send).toHaveBeenLastCalledWith("test-session-2", "Second turn");
        });



        it("closes an open mention popover when the live chat becomes read-only", () => {
            const props = {
                existingSessionId: "test-session-1",
                lang: "en",
                participants: [{ id: "ve-a", name: "Agent A", online: true }],
            } satisfies Partial<VEConversationViewProps>;
            const { rerender } = renderConversation(props);

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "@", selectionStart: 1 } });
            expect(screen.getByTestId("mention-popover")).toBeTruthy();

            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    readOnly={true}
                    {...props}
                />
            );

            expect(screen.queryByTestId("mention-popover")).toBeNull();
        });

        it("ignores external mention inserts in read-only mode", () => {
            renderConversation({
                existingSessionId: "test-session-1",
                readOnly: true,
                participants: [{ id: "ve-a", name: "Agent A", online: true }],
                externalMentionInsert: { name: "Agent A", timestamp: 1 },
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            act(() => { vi.runOnlyPendingTimers(); });

            expect(textarea.value).toBe("");
            expect(screen.queryByTestId("mention-popover")).toBeNull();
        });

        it("shows participant suggestions when typing @ in a group chat", async () => {
            renderConversation({
                existingSessionId: "test-session-1",
                lang: "en",
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "local-maclaw", name: "Local AI", online: true },
                ],
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "@", selectionStart: 1 } });

            expect(screen.getByTestId("mention-popover")).toBeTruthy();
            expect(screen.getByTestId("mention-item-ve-a")).toBeTruthy();
            expect(screen.getByTestId("mention-item-local-maclaw")).toBeTruthy();

            fireEvent.click(screen.getByTestId("mention-item-local-maclaw"));
            expect(textarea.value).toBe("@Local AI ");
        });

        it("filters participant suggestions by the current @ query", async () => {
            renderConversation({
                existingSessionId: "test-session-1",
                lang: "en",
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "local-maclaw", name: "Local AI", online: true },
                ],
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "@Loc", selectionStart: 4 } });

            expect(screen.getByTestId("mention-popover")).toBeTruthy();
            expect(screen.getByTestId("mention-item-local-maclaw")).toBeTruthy();
            expect(screen.queryByTestId("mention-item-ve-a")).toBeNull();
        });

        it("does not match @ suggestions by raw participant ids", async () => {
            renderConversation({
                existingSessionId: "test-session-1",
                lang: "en",
                participants: [
                    { id: "m_b1821505498d817c", name: "Participant 1", online: true },
                    { id: "local-maclaw", name: "Local AI", online: true },
                ],
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "@m_b", selectionStart: 4 } });

            expect(screen.queryByTestId("mention-popover")).toBeNull();
        });

        it("selects a participant from the @ popover with Enter instead of sending", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                existingSessionId: "test-session-1",
                lang: "en",
                sendGroupMessage,
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "local-maclaw", name: "Local AI", online: true },
                ],
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "@Loc", selectionStart: 4 } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            expect(textarea.value).toBe("@Local AI ");
            expect(sendGroupMessage).not.toHaveBeenCalled();
        });

        it("does not show @ suggestions for email-like text", async () => {
            renderConversation({
                existingSessionId: "test-session-1",
                lang: "en",
                participants: [{ id: "ve-anna", name: "Anna", online: true }],
            });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "ops@Anna", selectionStart: 8 } });

            expect(screen.queryByTestId("mention-popover")).toBeNull();
        });

        it("routes localized local AI mention aliases to the local participant", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                sendGroupMessage,
                participants: [
                    { id: "local-maclaw", name: "Local AI", online: true },
                    { id: "ve-a", name: "Agent A", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@本机 AI 请处理" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "@本机 AI 请处理", ["local-maclaw"]);
        });

        it("routes local AI mentions typed as 本地AI", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                sendGroupMessage,
                participants: [
                    { id: "local-maclaw", name: "Local AI", online: true },
                    { id: "ve-a", name: "Agent A", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@本地AI 快速回答" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "@本地AI 快速回答", ["local-maclaw"]);
        });

        it("routes group messages with mentioned participant ids", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            const { send } = renderConversation({
                sendGroupMessage,
                participants: [
                    { id: "local-maclaw", name: "Local AI", online: true },
                    { id: "ve-a", name: "Agent A", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@Agent A please review" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "@Agent A please review", ["ve-a"]);
            expect(send).not.toHaveBeenCalled();
        });

        it("does not route raw participant ids as @mentions", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            const { send } = renderConversation({
                sendGroupMessage,
                participants: [
                    { id: "m_b1821505498d817c", name: "Participant 1", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@m_b1821505498d817c please review" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "@m_b1821505498d817c please review", []);
        });

        it("does not route partial @mention name matches", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                sendGroupMessage,
                participants: [
                    { id: "ve-ann", name: "Ann", online: true },
                    { id: "ve-anna", name: "Anna", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@Anna please review" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "@Anna please review", ["ve-anna"]);
        });

        it("routes @mentions typed after Chinese text without requiring a leading space", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                sendGroupMessage,
                participants: [{ id: "ve-anna", name: "Anna", online: true }],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "请@Anna看一下" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "请@Anna看一下", ["ve-anna"]);
        });

        it("does not route email-like text as an @mention", async () => {
            const sendGroupMessage = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                sendGroupMessage,
                participants: [{ id: "ve-anna", name: "Anna", online: true }],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "mail Anna at ops@Anna.com" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendGroupMessage).toHaveBeenCalledWith("test-session-1", "mail Anna at ops@Anna.com", []);
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
            expect(screen.getByRole("button", { name: /evidence\.pdf/ })).toBeTruthy();

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "See attachment" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(sendWithAttachments).toHaveBeenCalledWith("test-session-1", "See attachment", ["D:\\cases\\evidence.pdf"]);
        });

        it("preserves group mention routing when sending attachments", async () => {
            (SelectAIAssistantFiles as any).mockResolvedValueOnce(["D:\\cases\\evidence.pdf"]);
            const sendGroupMessageWithAttachments = vi.fn().mockResolvedValue(undefined);
            const sendWithAttachments = vi.fn().mockResolvedValue(undefined);
            renderConversation({
                existingSessionId: "test-session-1",
                sendMessageWithAttachments: sendWithAttachments,
                sendGroupMessageWithAttachments,
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "local-maclaw", name: "本机 AI", online: true },
                ],
            });

            fireEvent.click(screen.getByTestId("ve-attach-button"));
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "@Agent A see attachment" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(sendGroupMessageWithAttachments).toHaveBeenCalledWith("test-session-1", "@Agent A see attachment", ["ve-a"], ["D:\\cases\\evidence.pdf"]);
            expect(sendWithAttachments).not.toHaveBeenCalled();
        });

        it("opens the local path for the user's own sent attachment", async () => {
            (SelectAIAssistantFiles as any).mockResolvedValueOnce(["D:\\cases\\evidence.pdf"]);
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            fireEvent.click(screen.getByTestId("ve-attach-button"));
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "See attachment" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await vi.runAllTimersAsync(); });

            await act(async () => {
                fireEvent.click(screen.getByTestId("ve-att-chip-evidence.pdf"));
                await Promise.resolve();
            });

            expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("D:\\cases\\evidence.pdf");
            expect(GroupDiscussionDownloadAttachment).not.toHaveBeenCalled();
        });


        it("drops queued messages when the chat becomes read-only before session init finishes", async () => {
            let resolveInit: (value: { session_id: string; ve_id: string; ve_name: string }) => void = () => {};
            const initiate = vi.fn(() => new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveInit = resolve; }));
            const send = vi.fn().mockResolvedValue(undefined);
            const props = { initiateConversation: initiate, sendMessage: send } satisfies Partial<VEConversationViewProps>;
            const { rerender } = renderConversation(props);

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Queued then locked" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            expect(screen.getAllByText("Queued then locked").length).toBeGreaterThan(0);

            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    readOnly={true}
                    {...props}
                />
            );

            await act(async () => {
                resolveInit({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
                await vi.runOnlyPendingTimersAsync();
            });

            expect(send).not.toHaveBeenCalled();
        });

        it("does not replay a stale queued message after read-only mode is lifted", async () => {
            let resolveInit: (value: { session_id: string; ve_id: string; ve_name: string }) => void = () => {};
            const initiate = vi.fn(() => new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveInit = resolve; }));
            const send = vi.fn().mockResolvedValue(undefined);
            const props = { initiateConversation: initiate, sendMessage: send } satisfies Partial<VEConversationViewProps>;
            const { rerender } = renderConversation(props);

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Queued then stale" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    readOnly={true}
                    {...props}
                />
            );

            await act(async () => {
                resolveInit({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
                await vi.runOnlyPendingTimersAsync();
            });
            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    existingSessionId="test-session-1"
                    readOnly={false}
                    {...props}
                />
            );
            await act(async () => { await Promise.resolve(); });

            expect(send).not.toHaveBeenCalled();
        });

        it("flushes a message queued before the initial session is ready", async () => {
            let resolveInit: (value: { session_id: string; ve_id: string; ve_name: string }) => void = () => {};
            const initiate = vi.fn(() => new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveInit = resolve; }));
            const send = vi.fn().mockResolvedValue(undefined);
            renderConversation({ initiateConversation: initiate, sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Queued before session" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            expect(send).not.toHaveBeenCalled();
            expect(screen.getAllByText("Queued before session").length).toBeGreaterThan(0);
            await act(async () => {
                resolveInit({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
                await vi.runOnlyPendingTimersAsync();
            });

            expect(send).toHaveBeenCalledWith("test-session-1", "Queued before session");
        });

        it("marks the exact queued message that fails after the session becomes ready", async () => {
            let resolveInit: (value: { session_id: string; ve_id: string; ve_name: string }) => void = () => {};
            const initiate = vi.fn(() => new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveInit = resolve; }));
            const send = vi
                .fn()
                .mockRejectedValueOnce(new Error("first failed"))
                .mockResolvedValueOnce(undefined);
            const { container } = renderConversation({ initiateConversation: initiate, sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "first queued" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            fireEvent.change(textarea, { target: { value: "second queued" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            expect(screen.getAllByText("first queued").length).toBeGreaterThan(0);
            expect(screen.getAllByText("second queued").length).toBeGreaterThan(0);

            await act(async () => {
                resolveInit({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
                await vi.runOnlyPendingTimersAsync();
            });

            const messageNodes = Array.from(container.querySelectorAll('[data-testid^="ve-msg-"]'));
            const firstNode = messageNodes.find((node) => node.textContent?.includes("first queued"));
            const secondNode = messageNodes.find((node) => node.textContent?.includes("second queued"));
            expect(firstNode?.textContent).toContain("发送失败");
            expect(secondNode?.textContent).not.toContain("发送失败");
        });

        it("ignores disconnect events for other sessions", async () => {
            const { initiate } = renderConversation({ existingSessionId: "test-session-1" });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:disconnected")?.({ session_id: "other-session" });
            });
            await act(async () => { vi.advanceTimersByTime(2000); });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            expect(initiate).not.toHaveBeenCalled();
            expect(screen.queryByText(/Reconnecting/)).toBeNull();
        });

        it("coalesces repeated disconnect events into one reconnect timer", async () => {
            const initiate = vi.fn().mockResolvedValue({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
            renderConversation({ existingSessionId: "test-session-1", initiateConversation: initiate, lang: "en" });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:disconnected")?.({ session_id: "test-session-1" });
                eventHandlers.get("ve:disconnected")?.({ session_id: "test-session-1" });
            });
            expect(screen.getByText(/Reconnecting/)).toBeTruthy();
            await act(async () => { vi.advanceTimersByTime(2000); });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(1);
        });

        it("retries reconnect when the first session re-init fails", async () => {
            const initiate = vi
                .fn()
                .mockRejectedValueOnce(new Error("temporary offline"))
                .mockResolvedValueOnce({ session_id: "test-session-1", ve_id: "ve-1", ve_name: "Test VE" });
            renderConversation({ existingSessionId: "test-session-1", initiateConversation: initiate });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:disconnected")?.({ session_id: "test-session-1" });
            });

            await act(async () => { vi.advanceTimersByTime(2000); });
            await act(async () => { await Promise.resolve(); });
            expect(initiate).toHaveBeenCalledTimes(1);

            await act(async () => { vi.advanceTimersByTime(4000); });
            await act(async () => { await vi.runOnlyPendingTimersAsync(); });

            expect(initiate).toHaveBeenCalledTimes(2);
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

        it("hides the thinking hint when send fails before any response chunk", async () => {
            const send = vi.fn().mockRejectedValue(new Error("network error"));
            renderConversation({ existingSessionId: "test-session-1", sendMessage: send });

            const textarea = screen.getByTestId("ve-input-textarea");
            fireEvent.change(textarea, { target: { value: "Will fail before reply" } });
            fireEvent.keyDown(textarea, { key: "Enter" });
            await act(async () => { await Promise.resolve(); });

            expect(screen.queryByTestId("ve-thinking-indicator")).toBeNull();
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

        it("keeps wrapping rules while streaming", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "StreamingWithoutSpacesWithoutSpaces\nsecond line" });
            });

            const bubble = screen.getByTestId("ve-streaming-content") as HTMLElement;
            expect(bubble.style.overflowWrap).toBe("anywhere");
            expect(bubble.style.whiteSpace).toBe("pre-wrap");
            expect(screen.getByText("second line")).toBeTruthy();
        });

        it("normalizes compact emoji headings while streaming", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "晴天" });
            });
            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "####\u{1f4c5}明天\n多云" });
            });

            const indicator = screen.getByTestId("ve-streaming-indicator");
            expect(indicator.textContent).toContain("晴天");
            expect(screen.getByText("\u{1f4c5}明天")).toBeTruthy();
            expect(screen.getByText("多云")).toBeTruthy();
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

        it("renders attachment-only stream messages", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    content: "",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1", size_bytes: 4096 }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            expect(screen.queryByTestId("ve-streaming-indicator")).toBeNull();
            expect(screen.getByTestId("ve-message-list").textContent).toContain("report.pdf");
            expect(screen.getByTestId("ve-att-chip-report.pdf")).toBeTruthy();
            expect(screen.getByTestId("ve-message-list").querySelector('[data-testid^="ve-msg-content-"]')).toBeNull();
        });

        it("shows streamed attachments before the stream ends", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    content: "",
                    attachments: [{ type: "file", filename: "live.pdf", file_url: "/api/ve/files/download/live-1" }],
                });
            });

            const indicator = screen.getByTestId("ve-streaming-indicator");
            expect(indicator.textContent).toContain("live.pdf");
            expect(screen.getByTestId("ve-att-chip-live.pdf")).toBeTruthy();
        });

        it("merges multiple streamed attachment chunks into one final message", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    content: "Files ready",
                    attachments: [{ type: "file", filename: "one.pdf", file_url: "/api/ve/files/download/file-1" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    content: "",
                    attachments: [{ type: "file", filename: "two.pdf", file_url: "/api/ve/files/download/file-2" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            expect(screen.getByTestId("ve-message-list").textContent).toContain("Files ready");
            expect(screen.getByTestId("ve-att-chip-one.pdf")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-two.pdf")).toBeTruthy();
        });

        it("renders downloaded image attachments as thumbnails", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "image", filename: "photo.png", file_url: "/api/ve/files/download/img-1", local_path: "D:\\cache\\photo.png" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });
            const thumb = screen.getByTestId("ve-att-image-thumb-photo.png") as HTMLImageElement;
            expect(GroupDiscussionAttachmentPreviewDataURL).toHaveBeenCalledWith("test-session-1", "D:\\cache\\photo.png");
            expect(thumb.src).toContain("data:image/png;base64,abc123");
        });

        it("downloads remote image attachments to build thumbnails", async () => {
            (GroupDiscussionDownloadAttachment as any).mockResolvedValueOnce({ local_path: "D:\\cache\\remote-photo.png" });
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "image", filename: "remote-photo.png", file_url: "/api/ve/files/download/img-remote" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
                await Promise.resolve();
            });

            expect(GroupDiscussionDownloadAttachment).toHaveBeenCalledWith("test-session-1", "/api/ve/files/download/img-remote", "remote-photo.png");
            expect(GroupDiscussionAttachmentPreviewDataURL).toHaveBeenCalledWith("test-session-1", "D:\\cache\\remote-photo.png");
            expect(GroupDiscussionAttachmentPreviewDataURL).toHaveBeenCalledTimes(1);
            expect((screen.getByTestId("ve-att-image-thumb-remote-photo.png") as HTMLImageElement).src).toContain("data:image/png;base64,abc123");
        });

        it("does not render remote image file URLs directly before secure preview is ready", async () => {
            let resolvePreview: (value: string) => void = () => undefined;
            (GroupDiscussionDownloadAttachment as any).mockResolvedValueOnce({ local_path: "D:\\cache\\remote-photo.png" });
            (GroupDiscussionAttachmentPreviewDataURL as any).mockImplementation(() => new Promise<string>((resolve) => { resolvePreview = resolve; }));
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "image", filename: "remote-photo.png", file_url: "https://hub.example/api/ve/files/download/img-remote" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });
            expect(screen.getByTestId("ve-att-chip-remote-photo.png").getAttribute("title")).toBe("remote-photo.png");
            expect(screen.queryByTestId("ve-att-image-thumb-remote-photo.png")).toBeNull();

            await act(async () => {
                resolvePreview("data:image/png;base64,ready");
                await Promise.resolve();
            });

            expect((screen.getByTestId("ve-att-image-thumb-remote-photo.png") as HTMLImageElement).src).toContain("data:image/png;base64,ready");
        });

        it("opens the cached preview path for remote image attachments", async () => {
            (GroupDiscussionDownloadAttachment as any).mockResolvedValueOnce({ local_path: "D:\\cache\\remote-photo.png" });
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "image", filename: "remote-photo.png", file_url: "/api/ve/files/download/img-remote" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
                await Promise.resolve();
            });
            await act(async () => {
                for (let i = 0; i < 8; i += 1) await Promise.resolve();
            });
            expect(GroupDiscussionAttachmentPreviewDataURL).toHaveBeenCalledWith("test-session-1", "D:\\cache\\remote-photo.png");
            (GroupDiscussionDownloadAttachment as any).mockClear();

            await act(async () => {
                fireEvent.click(screen.getByTestId("ve-att-chip-remote-photo.png"));
                await Promise.resolve();
            });

            expect(GroupDiscussionDownloadAttachment).not.toHaveBeenCalled();
            expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("D:\\cache\\remote-photo.png");
        });

        it("uses a file-type badge for non-image attachments", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            expect(screen.getByTestId("ve-att-chip-report.pdf").textContent).toContain("PDF");
        });

        it("downloads streamed attachments through the authenticated Wails bridge before opening", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            await act(async () => {
                fireEvent.click(screen.getByTestId("ve-att-chip-report.pdf"));
                await Promise.resolve();
            });

            expect(GroupDiscussionDownloadAttachment).toHaveBeenCalledWith("test-session-1", "/api/ve/files/download/file-1", "report.pdf");
            expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("C:\\tmp\\report.pdf");
        });

        it("preserves downloaded local paths when stream end enriches an existing attachment", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1", size_bytes: 4096 }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1", local_path: "D:\\cache\\report.pdf", size_bytes: 4096 }],
                });
            });

            await act(async () => {
                fireEvent.click(screen.getByTestId("ve-att-chip-report.pdf"));
                await Promise.resolve();
            });

            expect(GroupDiscussionDownloadAttachment).not.toHaveBeenCalled();
            expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("D:\\cache\\report.pdf");
        });

        it("merges a local-path-only attachment enrichment without duplicating the chip", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1" }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", local_path: "D:\\cache\\report.pdf" }],
                });
            });

            expect(screen.getAllByTestId("ve-att-chip-report.pdf")).toHaveLength(1);
            await act(async () => {
                fireEvent.click(screen.getByTestId("ve-att-chip-report.pdf"));
                await Promise.resolve();
            });

            expect(GroupDiscussionDownloadAttachment).not.toHaveBeenCalled();
            expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("D:\\cache\\report.pdf");
        });

        it("does not merge same-named streamed attachments when their sizes conflict", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", file_url: "/api/ve/files/download/file-1", size_bytes: 100 }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", local_path: "D:\\cache\\report.pdf", size_bytes: 200 }],
                });
            });

            expect(screen.getAllByTestId("ve-att-chip-report.pdf")).toHaveLength(2);
        });

        it("normalizes malformed attachment metadata without leaking object strings", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: { bad: true }, filename: { bad: true }, file_url: { bad: true }, local_path: { bad: true } }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            const msgList = screen.getByTestId("ve-message-list");
            expect(msgList.textContent).toContain("attachment");
            expect(msgList.textContent).not.toContain("[object Object]");
        });

        it("ignores numeric local paths in streamed attachment metadata", async () => {
            renderConversation({ existingSessionId: "test-session-1" });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({
                    session_id: "test-session-1",
                    attachments: [{ type: "file", filename: "report.pdf", local_path: 12345 }],
                });
            });
            act(() => {
                eventHandlers.get("ve:stream_end")?.({ session_id: "test-session-1" });
            });

            const chip = screen.getByTestId("ve-att-chip-report.pdf") as HTMLButtonElement;
            expect(chip.disabled).toBe(true);
        });

        it("splits live stream output when the responding speaker changes", async () => {
            renderConversation({
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "ve-b", name: "Agent B", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ from_id: "ve-a", from_name: "Agent A", content: "A says hi" });
            });
            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ from_id: "ve-b", from_name: "Agent B", content: "B replies" });
            });

            const msgList = screen.getByTestId("ve-message-list");
            expect(msgList.textContent).toContain("Agent A");
            expect(msgList.textContent).toContain("A says hi");
            const indicator = screen.getByTestId("ve-streaming-indicator");
            expect(indicator.textContent).toContain("Agent B");
            expect(indicator.textContent).toContain("B replies");
            expect(indicator.textContent).not.toContain("A says hi");
        });

        it("uses participant names when stream sender names are raw ids", async () => {
            renderConversation({
                participants: [
                    { id: "m_b1821505498d817c", name: "Contract Bot", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ from_id: "m_b1821505498d817c", from_name: "m_b1821505498d817c", content: "Raw speaker hidden" });
            });
            let indicator = screen.getByTestId("ve-streaming-indicator");
            expect(indicator.textContent).toContain("Contract Bot");
            expect(indicator.textContent).not.toContain("m_b1821505498d817c");

            act(() => {
                eventHandlers.get("ve:stream_end")?.({});
            });
            const msgList = screen.getByTestId("ve-message-list");
            expect(msgList.textContent).toContain("Contract Bot");
            expect(msgList.textContent).not.toContain("m_b1821505498d817c");
        });

        it("clears stale stream sender identity when an empty stream ends", async () => {
            renderConversation({
                participants: [
                    { id: "ve-a", name: "Agent A", online: true },
                    { id: "ve-b", name: "Agent B", online: true },
                ],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ from_id: "ve-a", from_name: "Agent A", content: "" });
            });
            expect(screen.getByTestId("ve-streaming-indicator").textContent).toContain("Agent A");

            act(() => {
                eventHandlers.get("ve:stream_end")?.({});
            });
            expect(screen.queryByTestId("ve-streaming-indicator")).toBeNull();

            act(() => {
                eventHandlers.get("ve:stream_chunk")?.({ content: "Next" });
            });
            const indicator = screen.getByTestId("ve-streaming-indicator");
            expect(indicator.textContent).toContain("Test VE");
            expect(indicator.textContent).not.toContain("Agent A");
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

        it("keeps the textarea from overlapping the send button", async () => {
            renderConversation();
            await act(async () => { await vi.runAllTimersAsync(); });

            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            const sendButton = screen.getByTestId("ve-send-button") as HTMLButtonElement;

            expect(textarea.style.boxSizing).toBe("border-box");
            expect(sendButton.style.flexShrink).toBe("0");
            expect(sendButton.style.minWidth).toBe("54px");
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

        it("syncs a late existing session id into the mounted view", async () => {
            const ref = createRef<VEConversationHandle>();
            const initiate = vi.fn().mockResolvedValue({ session_id: "created-session", ve_id: "ve-1", ve_name: "Test VE" });
            const { rerender } = render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    initialOnlineStatus="offline"
                    initiateConversation={initiate}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                />
            );

            expect(ref.current?.getState().sessionId).toBeNull();
            rerender(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    initialOnlineStatus="offline"
                    existingSessionId="history-session"
                    initiateConversation={initiate}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                />
            );

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(initiate).not.toHaveBeenCalled();
            expect(ref.current?.getState().sessionId).toBe("history-session");
        });

        it("loads saved messages when opening a digital employee session", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speaker" }] },
                messages: [
                    { id: "m1", from_id: "human-1", from_name: "Me", kind: "statement", content: "之前的问题", created_at: "2026-05-01T00:00:00Z" },
                    { id: "m2", from_id: "ve-1", from_name: "Test VE", kind: "stream_chunk", content: "历史", created_at: "2026-05-01T00:00:01Z" },
                    { id: "m3", from_id: "ve-1", from_name: "Test VE", kind: "stream_chunk", content: "回复", created_at: "2026-05-01T00:00:02Z" },
                    { id: "m4", from_id: "ve-1", from_name: "Test VE", kind: "stream_end", created_at: "2026-05-01T00:00:03Z" },
                ],
            });

            renderConversation({ existingSessionId: "test-session-1" });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(GroupDiscussionGetConsultationDetail).toHaveBeenCalledWith("test-session-1");
            expect(screen.getByText("之前的问题")).toBeTruthy();
            expect(screen.getByText("历史回复")).toBeTruthy();
        });

        it("falls back to session messages when saved detail has no top-level messages", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: {
                    participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speaker" }],
                    messages: [],
                    Messages: [
                        { id: "session-m1", from_id: "human-1", from_name: "Me", kind: "statement", content: "session question", created_at: "2026-05-01T00:00:00Z" },
                        { id: "session-m2", from_id: "ve-1", from_name: "Test VE", kind: "statement", content: "session answer", created_at: "2026-05-01T00:00:01Z" },
                    ],
                },
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages.map((message) => message.role)).toEqual(["user", "assistant"]);
            expect(screen.getByText("session question")).toBeTruthy();
            expect(screen.getByText("session answer")).toBeTruthy();
        });

        it("restores attachments from saved digital employee history", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speaker" }] },
                messages: [
                    {
                        id: "history-file-msg",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "statement",
                        content: "Saved file",
                        created_at: "2026-05-01T00:00:00Z",
                        file_attachments: [{ filename: "saved-report.pdf", file_url: "/api/ve/files/download/saved-report", size_bytes: 4096 }],
                    },
                    {
                        id: "history-image-msg",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "statement",
                        content: "Saved image",
                        created_at: "2026-05-01T00:00:01Z",
                        image_attachments: [{ filename: "saved-photo.png", file_url: "/api/ve/files/download/saved-photo", local_path: "D:\\cache\\saved-photo.png" }],
                    },
                ],
            });

            renderConversation({ existingSessionId: "test-session-1" });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(screen.getByText("Saved file")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-saved-report.pdf")).toBeTruthy();
            expect(screen.getByText("Saved image")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-saved-photo.png")).toBeTruthy();
            expect(GroupDiscussionAttachmentPreviewDataURL).toHaveBeenCalledWith("test-session-1", "D:\\cache\\saved-photo.png");
        });

        it("deduplicates overlapping saved history attachments", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1" },
                session: { participants: [{ id: "ve-1", role_code: "speak" }] },
                messages: [
                    {
                        id: "history-duplicate-file",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "statement",
                        content: "duplicate file",
                        created_at: "2026-05-01T00:00:00Z",
                        attachments: [{ type: "file", filename: "duplicate.pdf", fileUrl: "/api/ve/files/download/duplicate", sizeBytes: 1024 }],
                        file_attachments: [{ filename: "duplicate.pdf", file_url: "/api/ve/files/download/duplicate", local_path: "D:\\cache\\duplicate.pdf", size_bytes: 1024 }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(screen.getByText("duplicate file")).toBeTruthy();
            expect(ref.current?.getState().messages[0]?.attachments).toHaveLength(1);
            expect(ref.current?.getState().messages[0]?.attachments?.[0]?.localPath).toBe("D:\\cache\\duplicate.pdf");
        });

        it("treats initiator-role history messages as local even without local relation", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speak" }] },
                messages: [
                    { id: "m-local", from_id: "human-1", from_name: "Me", kind: "statement", content: "local question", created_at: "2026-05-01T00:00:00Z" },
                    { id: "m-assistant", from_id: "ve-1", from_name: "Test VE", kind: "statement", content: "assistant answer", created_at: "2026-05-01T00:00:01Z" },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages.map((message) => message.role)).toEqual(["user", "assistant"]);
            expect(screen.getByText("local question")).toBeTruthy();
            expect(screen.getByText("assistant answer")).toBeTruthy();
        });

        it("accepts camelCase fields in saved digital employee history", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1" },
                session: { participants: [{ id: "human-1", roleCode: "initiator" }, { id: "ve-1", roleCode: "speak" }] },
                messages: [
                    { id: "camel-user", fromId: "human-1", fromName: "Me", kind: "statement", content: "camel question", createdAt: "2026-05-01T00:00:00Z" },
                    {
                        id: "camel-ve",
                        fromId: "ve-1",
                        fromName: "Test VE",
                        kind: "statement",
                        content: "camel answer",
                        createdAt: "2026-05-01T00:00:01Z",
                        fileAttachments: [{ filename: "camel-report.pdf", fileUrl: "/api/ve/files/download/camel-report", sizeBytes: 2048 }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages.map((message) => message.role)).toEqual(["user", "assistant"]);
            expect(screen.getByText("camel question")).toBeTruthy();
            expect(screen.getByText("camel answer")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-camel-report.pdf")).toBeTruthy();
        });

        it("accepts PascalCase fields in saved digital employee history", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1" },
                session: { participants: [{ ID: "human-1", RoleCode: "initiator" }, { ID: "ve-1", RoleCode: "speak" }] },
                messages: [
                    { ID: "pascal-user", FromID: "human-1", FromName: "Me", Kind: "statement", Content: "pascal question", CreatedAt: "2026-05-01T00:00:00Z" },
                    {
                        ID: "pascal-ve",
                        FromID: "ve-1",
                        FromName: "Test VE",
                        Kind: "stream_chunk",
                        Content: "pascal",
                        CreatedAt: "2026-05-01T00:00:01Z",
                    },
                    {
                        ID: "pascal-end",
                        FromID: "ve-1",
                        Kind: "stream_end",
                        Content: " answer",
                        Attachments: [{ Filename: "pascal-generic.txt", FileURL: "/api/ve/files/download/pascal-generic", SizeBytes: "512", MimeType: "text/plain" }],
                        FileAttachments: [{ Filename: "pascal-report.pdf", FileURL: "/api/ve/files/download/pascal-report", SizeBytes: "2048" }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages.map((message) => message.role)).toEqual(["user", "assistant"]);
            expect(screen.getByText("pascal question")).toBeTruthy();
            expect(screen.getByText("pascal answer")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-pascal-generic.txt")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-pascal-report.pdf")).toBeTruthy();
            expect(ref.current?.getState().messages[1]?.attachments?.map((attachment) => attachment.sizeBytes)).toEqual([512, 2048]);
        });

        it("ignores malformed history attachment lists and parses string sizes", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1" },
                session: { participants: [{ id: "ve-1", role_code: "speak" }] },
                messages: [
                    {
                        id: "history-size-string",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "statement",
                        content: "sized attachment",
                        created_at: "2026-05-01T00:00:00Z",
                        imageAttachments: { malformed: true },
                        file_attachments: [{ filename: "string-size.pdf", file_url: "/api/ve/files/download/string-size", size_bytes: "4096" }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(screen.getByText("sized attachment")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-string-size.pdf")).toBeTruthy();
            expect(ref.current?.getState().messages[0]?.attachments?.[0]?.sizeBytes).toBe(4096);
        });

        it("merges stream_end attachment enrichment while restoring history", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speak" }] },
                messages: [
                    {
                        id: "history-stream-file",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "stream_chunk",
                        content: "final file",
                        created_at: "2026-05-01T00:00:00Z",
                        file_attachments: [{ filename: "final-report.pdf", file_url: "/api/ve/files/download/final-report", size_bytes: 4096 }],
                    },
                    {
                        id: "history-stream-end",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "stream_end",
                        created_at: "2026-05-01T00:00:01Z",
                        file_attachments: [{ filename: "final-report.pdf", file_url: "/api/ve/files/download/final-report", local_path: "D:\\cache\\final-report.pdf", size_bytes: 4096 }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(screen.getByText("final file")).toBeTruthy();
            expect(screen.getByTestId("ve-att-chip-final-report.pdf")).toBeTruthy();
            expect(ref.current?.getState().messages).toHaveLength(1);
            expect(ref.current?.getState().messages[0]?.attachments?.[0]?.localPath).toBe("D:\\cache\\final-report.pdf");
        });

        it("keeps attachment-only stream_end history messages without prior chunks", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speak" }] },
                messages: [
                    {
                        id: "history-end-only",
                        from_id: "ve-1",
                        from_name: "Test VE",
                        kind: "stream_end",
                        created_at: "2026-05-01T00:00:00Z",
                        file_attachments: [{ filename: "end-only.pdf", file_url: "/api/ve/files/download/end-only", local_path: "D:\\cache\\end-only.pdf" }],
                    },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages).toHaveLength(1);
            expect(screen.getByTestId("ve-att-chip-end-only.pdf")).toBeTruthy();
            expect(ref.current?.getState().messages[0]?.attachments?.[0]?.localPath).toBe("D:\\cache\\end-only.pdf");
        });

        it("merges stream_end content into the active restored stream", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speak" }] },
                messages: [
                    { id: "history-stream-start", from_id: "ve-1", from_name: "Test VE", kind: "stream_chunk", content: "Hello", created_at: "2026-05-01T00:00:00Z" },
                    { id: "history-stream-final", from_id: "ve-1", from_name: "Test VE", kind: "stream_end", content: " world", created_at: "2026-05-01T00:00:01Z" },
                ],
            });

            const ref = createRef<VEConversationHandle>();
            render(
                <VEConversationView
                    ref={ref}
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn()}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={vi.fn()}
                />
            );
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(ref.current?.getState().messages).toHaveLength(1);
            expect(screen.getByText("Hello world")).toBeTruthy();
        });

        it("loads saved messages after initiate reuses an existing digital employee session", async () => {
            const initiate = vi.fn().mockResolvedValue({ session_id: "reused-session-1", ve_id: "ve-1", ve_name: "Test VE" });
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "reused-session-1", local_relation: "initiated_by_me" },
                session: { participants: [{ id: "human-1", role_code: "initiator" }, { id: "ve-1", role_code: "speaker" }] },
                messages: [
                    { id: "m-reused-1", from_id: "human-1", from_name: "Me", kind: "statement", content: "earlier question", created_at: "2026-05-01T00:00:00Z" },
                    { id: "m-reused-2", from_id: "ve-1", from_name: "Test VE", kind: "statement", content: "earlier answer", created_at: "2026-05-01T00:00:01Z" },
                ],
            });

            renderConversation({ initiateConversation: initiate });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(initiate).toHaveBeenCalledWith("ve-1");
            expect(GroupDiscussionGetConsultationDetail).toHaveBeenCalledWith("reused-session-1");
            expect(screen.getByText("earlier question")).toBeTruthy();
            expect(screen.getByText("earlier answer")).toBeTruthy();
        });

        it("does not replace already provided messages with fetched history", async () => {
            (GroupDiscussionGetConsultationDetail as any).mockResolvedValueOnce({
                discussion: { id: "test-session-1", local_relation: "initiated_by_me" },
                messages: [
                    { id: "history-msg", from_id: "ve-1", from_name: "Test VE", kind: "statement", content: "history answer", created_at: "2026-05-01T00:00:00Z" },
                ],
            });

            renderConversation({
                existingSessionId: "test-session-1",
                initialMessages: [{ id: "current-msg", role: "assistant", content: "current answer", timestamp: 1 }],
            });
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(GroupDiscussionGetConsultationDetail).not.toHaveBeenCalled();
            expect(screen.getByText("current answer")).toBeTruthy();
            expect(screen.queryByText("history answer")).toBeNull();
        });

        it("clears VE history with an explicit /clear command", async () => {
            const close = vi.fn().mockResolvedValue(undefined);
            const onConversationCleared = vi.fn();
            renderConversation({
                existingSessionId: "test-session-1",
                closeSession: close,
                onConversationCleared,
                initialMessages: [{ id: "old-msg", role: "assistant", content: "old answer", timestamp: 1 }],
            });

            expect(screen.getByText("old answer")).toBeTruthy();
            const textarea = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
            fireEvent.change(textarea, { target: { value: "/clear" } });
            fireEvent.keyDown(textarea, { key: "Enter" });

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.queryByText("old answer")).toBeNull();
            expect(textarea.value).toBe("");
            expect(close).toHaveBeenCalledWith("test-session-1");
            expect(onConversationCleared).toHaveBeenCalledTimes(1);
        });

        it("starts a fresh group session with all remote participants after clear", async () => {
            const initiateGroupConversation = vi.fn().mockResolvedValue({ session_id: "group-session-2", ve_id: "ve-1,ve-2", ve_name: "Group" });
            const registerLocalExecutorInGroup = vi.fn().mockResolvedValue({ participant_id: "machine-local", display_name: "Local AI" });
            const close = vi.fn().mockResolvedValue(undefined);
            const { rerender } = renderConversation({
                existingSessionId: "group-session-1",
                closeSession: close,
                initiateGroupConversation,
                registerLocalExecutorInGroup,
                participants: [
                    { id: "ve-1", name: "Agent A", online: true },
                    { id: "ve-2", name: "Agent B", online: true },
                    { id: "machine-local", name: "Local AI", online: true },
                ],
                initialMessages: [{ id: "old-msg", role: "assistant", content: "old answer", timestamp: 1 }],
                clearSignal: 0,
            });

            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Agent A"
                    theme={mockTheme}
                    lang="en"
                    existingSessionId="group-session-1"
                    initiateConversation={vi.fn()}
                    initiateGroupConversation={initiateGroupConversation}
                    registerLocalExecutorInGroup={registerLocalExecutorInGroup}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={close}
                    participants={[
                        { id: "ve-1", name: "Agent A", online: true },
                        { id: "ve-2", name: "Agent B", online: true },
                        { id: "machine-local", name: "Local AI", online: true },
                    ]}
                    clearSignal={1}
                />
            );

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(close).toHaveBeenCalledWith("group-session-1");
            expect(initiateGroupConversation).toHaveBeenCalledWith(["ve-1", "ve-2"]);
            expect(registerLocalExecutorInGroup).toHaveBeenCalledWith("group-session-2");
        });

        it("clears VE history when the tab manager sends a clear signal", async () => {
            const close = vi.fn().mockResolvedValue(undefined);
            const { rerender } = renderConversation({
                existingSessionId: "test-session-1",
                closeSession: close,
                initialMessages: [{ id: "old-msg", role: "assistant", content: "old answer", timestamp: 1 }],
                clearSignal: 0,
            });

            expect(screen.getByText("old answer")).toBeTruthy();
            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    existingSessionId="test-session-1"
                    initiateConversation={vi.fn().mockResolvedValue({ session_id: "test-session-2", ve_id: "ve-1", ve_name: "Test VE" })}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    closeSession={close}
                    clearSignal={1}
                />
            );

            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.queryByText("old answer")).toBeNull();
            expect(close).toHaveBeenCalledWith("test-session-1");
        });

        it("ignores stale session creation result after clear starts a fresh session", async () => {
            let resolveOld!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            let resolveFresh!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            const oldSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveOld = resolve; });
            const freshSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveFresh = resolve; });
            const initiate = vi.fn()
                .mockReturnValueOnce(oldSession)
                .mockReturnValueOnce(freshSession);
            const onSessionIdChange = vi.fn();
            const { rerender } = renderConversation({ initiateConversation: initiate, onSessionIdChange, clearSignal: 0 });
            await act(async () => { await Promise.resolve(); });
            expect(initiate).toHaveBeenCalledTimes(1);

            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Test VE"
                    theme={mockTheme}
                    lang="zh"
                    initiateConversation={initiate}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    onSessionIdChange={onSessionIdChange}
                    clearSignal={1}
                />
            );
            await act(async () => { await Promise.resolve(); });
            expect(initiate).toHaveBeenCalledTimes(2);

            resolveOld({ session_id: "stale-session", ve_id: "ve-1", ve_name: "Test VE" });
            await act(async () => { await Promise.resolve(); });
            expect(onSessionIdChange).not.toHaveBeenCalledWith("stale-session");

            resolveFresh({ session_id: "fresh-session", ve_id: "ve-1", ve_name: "Test VE" });
            await act(async () => { await Promise.resolve(); });
            expect(onSessionIdChange).toHaveBeenCalledWith("fresh-session");
            expect(onSessionIdChange).not.toHaveBeenCalledWith("stale-session");
        });

        it("does not register the local executor into a stale group session after clear", async () => {
            let resolveOld!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            let resolveFresh!: (value: { session_id: string; ve_id: string; ve_name: string }) => void;
            const oldSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveOld = resolve; });
            const freshSession = new Promise<{ session_id: string; ve_id: string; ve_name: string }>((resolve) => { resolveFresh = resolve; });
            const initiateGroupConversation = vi.fn()
                .mockReturnValueOnce(oldSession)
                .mockReturnValueOnce(freshSession);
            const registerLocalExecutorInGroup = vi.fn().mockResolvedValue({ participant_id: "machine-local" });
            const participants = [
                { id: "ve-1", name: "Agent A", online: true },
                { id: "machine-local", name: "Local AI", online: true },
            ];
            const { rerender } = renderConversation({
                participants,
                initiateGroupConversation,
                registerLocalExecutorInGroup,
                clearSignal: 0,
            });
            await act(async () => { await Promise.resolve(); });

            rerender(
                <VEConversationView
                    veId="ve-1"
                    veName="Agent A"
                    theme={mockTheme}
                    lang="en"
                    initiateGroupConversation={initiateGroupConversation}
                    registerLocalExecutorInGroup={registerLocalExecutorInGroup}
                    sendMessage={vi.fn()}
                    sendMessageWithAttachments={vi.fn()}
                    participants={participants}
                    clearSignal={1}
                />
            );
            await act(async () => { await Promise.resolve(); });

            resolveOld({ session_id: "stale-group-session", ve_id: "ve-1", ve_name: "Group" });
            await act(async () => { await Promise.resolve(); });
            expect(registerLocalExecutorInGroup).not.toHaveBeenCalledWith("stale-group-session");

            resolveFresh({ session_id: "fresh-group-session", ve_id: "ve-1", ve_name: "Group" });
            await act(async () => { await Promise.resolve(); });
            expect(registerLocalExecutorInGroup).toHaveBeenCalledWith("fresh-group-session");
            expect(registerLocalExecutorInGroup).not.toHaveBeenCalledWith("stale-group-session");
        });
    });
});
