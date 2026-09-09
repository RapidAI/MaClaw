import { act, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    VEAuthorizationDialog,
    VEAuthorizationRequestCenter,
    VEAuthBlinkingIndicator,
} from "../VEAuthorizationDialog";
import type { Theme } from "../aiAssistantPanelTheme";
import { EventsEmit } from "../../../../wailsjs/runtime";

// Mock Wails runtime
const eventHandlers = new Map<string, (...args: any[]) => void>();
const LoadConfigMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((eventName: string, handler: (...args: any[]) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
    EventsEmit: vi.fn(),
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

describe("VEAuthorizationDialog", () => {
    beforeEach(() => {
        eventHandlers.clear();
        LoadConfigMock.mockResolvedValue({ group_discussion: { auth_request_sound_muted: true } });
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    describe("dialog visibility", () => {
        it("does not render when no pending requests", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );
            expect(screen.queryByTestId("ve-auth-dialog")).toBeNull();
        });

        it("renders dialog when auth request event is received", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-1",
                    requester_name: "Alice",
                    requester_machine_id: "machine-abc",
                    target_ve_id: "ve-1",
                    target_ve_name: "翻译助手",
                });
            });

            expect(screen.getByTestId("ve-auth-dialog")).toBeTruthy();
            expect(screen.getByTestId("ve-auth-request-req-1")).toBeTruthy();
        });

        it("displays requester info correctly", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-2",
                    requester_name: "Bob",
                    requester_machine_id: "machine-xyz",
                    target_ve_id: "ve-2",
                    target_ve_name: "代码审查",
                });
            });

            const reqEl = screen.getByTestId("ve-auth-request-req-2");
            expect(reqEl.textContent).toContain("Bob");
            expect(reqEl.textContent).not.toContain("machine-xyz");
            expect(reqEl.textContent).toContain("代码审查");
        });

        it("uses readable fallbacks instead of raw machine ids", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-fallback",
                    requester_name: "m_b1821505498d817c",
                    requester_machine_id: "m_b1821505498d817c",
                    target_ve_id: "ve-raw",
                    target_ve_name: "ve-raw",
                });
            });

            const reqEl = screen.getByTestId("ve-auth-request-req-fallback");
            expect(reqEl.textContent).toContain("请求者");
            expect(reqEl.textContent).toContain("数字员工");
            expect(reqEl.textContent).not.toContain("m_b1821505498d817c");
            expect(reqEl.textContent).not.toContain("ve-raw");
            expect(reqEl.textContent).not.toContain("机器ID");
        });


        it("uses readable fallbacks when names look like raw ids but ids differ", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-raw-name-diff-id",
                    requester_name: "m_b1821505498d817c",
                    requester_machine_id: "profile-1",
                    target_ve_id: "target-1",
                    target_ve_name: "ve-raw",
                });
            });

            const reqEl = screen.getByTestId("ve-auth-request-req-raw-name-diff-id");
            expect(reqEl.textContent).toContain("Requester");
            expect(reqEl.textContent).toContain("Digital employee");
            expect(reqEl.textContent).not.toContain("m_b1821505498d817c");
            expect(reqEl.textContent).not.toContain("ve-raw");
        });

        it("does not add duplicate requests", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );

            const reqData = {
                id: "req-dup",
                requester_name: "Alice",
                requester_machine_id: "m-1",
                target_ve_id: "ve-1",
                target_ve_name: "VE",
            };

            act(() => { eventHandlers.get("ve:auth_request")?.({ ...reqData }); });
            act(() => { eventHandlers.get("ve:auth_request")?.({ ...reqData }); });

            // Should only have one request element
            const elements = screen.getAllByTestId("ve-auth-request-req-dup");
            expect(elements.length).toBe(1);
        });
    });

    describe("allow/deny actions", () => {
        it("calls respondAuthRequest with 'allow' on allow button click", async () => {
            const respond = vi.fn().mockResolvedValue(undefined);
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={respond} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-allow",
                    requester_name: "Alice",
                    requester_machine_id: "m-1",
                    target_ve_id: "ve-1",
                    target_ve_name: "VE",
                });
            });

            fireEvent.click(screen.getByTestId("ve-auth-allow-req-allow"));
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(respond).toHaveBeenCalledWith("req-allow", "allow");
        });

        it("calls respondAuthRequest with 'deny' on deny button click", async () => {
            const respond = vi.fn().mockResolvedValue(undefined);
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={respond} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-deny",
                    requester_name: "Bob",
                    requester_machine_id: "m-2",
                    target_ve_id: "ve-2",
                    target_ve_name: "VE2",
                });
            });

            fireEvent.click(screen.getByTestId("ve-auth-deny-req-deny"));
            await act(async () => { await vi.runAllTimersAsync(); });

            expect(respond).toHaveBeenCalledWith("req-deny", "deny");
        });

        it("removes request from list after successful response", async () => {
            const respond = vi.fn().mockResolvedValue(undefined);
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={respond} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-remove",
                    requester_name: "Alice",
                    requester_machine_id: "m-1",
                    target_ve_id: "ve-1",
                    target_ve_name: "VE",
                });
            });

            expect(screen.getByTestId("ve-auth-request-req-remove")).toBeTruthy();

            fireEvent.click(screen.getByTestId("ve-auth-allow-req-remove"));
            await act(async () => { await vi.runAllTimersAsync(); });

            // Dialog should disappear (no more requests)
            expect(screen.queryByTestId("ve-auth-dialog")).toBeNull();
        });

        it("keeps request on response error", async () => {
            const respond = vi.fn().mockRejectedValue(new Error("network error"));
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={respond} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-err",
                    requester_name: "Alice",
                    requester_machine_id: "m-1",
                    target_ve_id: "ve-1",
                    target_ve_name: "VE",
                });
            });

            fireEvent.click(screen.getByTestId("ve-auth-allow-req-err"));
            await act(async () => { await vi.runAllTimersAsync(); });

            // Request should still be visible
            expect(screen.getByTestId("ve-auth-request-req-err")).toBeTruthy();
        });
    });

    describe("multiple requests", () => {
        it("handles multiple concurrent requests", () => {
            render(
                <VEAuthorizationDialog theme={mockTheme} lang="zh" respondAuthRequest={vi.fn()} />
            );

            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-a", requester_name: "A", requester_machine_id: "m-a",
                    target_ve_id: "ve-1", target_ve_name: "VE1",
                });
            });
            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    id: "req-b", requester_name: "B", requester_machine_id: "m-b",
                    target_ve_id: "ve-2", target_ve_name: "VE2",
                });
            });

            expect(screen.getByTestId("ve-auth-request-req-a")).toBeTruthy();
            expect(screen.getByTestId("ve-auth-request-req-b")).toBeTruthy();
        });
    });
});

describe("VEAuthBlinkingIndicator", () => {
    beforeEach(() => {
        eventHandlers.clear();
        LoadConfigMock.mockResolvedValue({ group_discussion: { auth_request_sound_muted: true } });
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("does not render when no pending requests", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);
        expect(screen.queryByTestId("ve-auth-blink-indicator")).toBeNull();
    });

    it("renders when auth request event is received", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({});
        });

        expect(screen.getByTestId("ve-auth-blink-indicator")).toBeTruthy();
    });

    it("shows pending count", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.(); });
        act(() => { eventHandlers.get("ve:auth_request")?.(); });

        expect(screen.getByTestId("ve-auth-blink-indicator").textContent).toContain("2");
    });

    it("deduplicates pending count by request id", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.({ request_id: "blink-req-1" }); });
        act(() => { eventHandlers.get("ve:auth_request")?.({ request_id: "blink-req-1" }); });

        expect(screen.getByTestId("ve-auth-blink-indicator").textContent).toContain("1");
    });

    it("removes handled request by request id", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.({ request_id: "blink-req-a" }); });
        act(() => { eventHandlers.get("ve:auth_request")?.({ request_id: "blink-req-b" }); });
        act(() => { eventHandlers.get("ve:auth_handled")?.({ request_id: "blink-req-a" }); });

        expect(screen.getByTestId("ve-auth-blink-indicator").textContent).toContain("1");
    });

    it("disappears when all requests are handled", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.(); });
        expect(screen.getByTestId("ve-auth-blink-indicator")).toBeTruthy();

        act(() => { eventHandlers.get("ve:auth_handled")?.(); });
        expect(screen.queryByTestId("ve-auth-blink-indicator")).toBeNull();
    });

    it("disappears when local handled event is dispatched", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.(); });
        expect(screen.getByTestId("ve-auth-blink-indicator")).toBeTruthy();

        act(() => { window.dispatchEvent(new CustomEvent("ve:auth_handled:local")); });
        expect(screen.queryByTestId("ve-auth-blink-indicator")).toBeNull();
    });

    it("blinks at 500ms interval", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.(); });

        const indicator = screen.getByTestId("ve-auth-blink-indicator");
        const initialOpacity = indicator.style.opacity;

        act(() => { vi.advanceTimersByTime(500); });
        const afterBlink = screen.getByTestId("ve-auth-blink-indicator").style.opacity;

        // Opacity should have changed (blink toggle)
        expect(afterBlink).not.toBe(initialOpacity);
    });
});

describe("VEAuthorizationRequestCenter", () => {
    beforeEach(() => {
        eventHandlers.clear();
        LoadConfigMock.mockResolvedValue({ group_discussion: { auth_request_sound_muted: true } });
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("does not render when no pending requests", () => {
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />);
        expect(screen.queryByTestId("ve-auth-request-center")).toBeNull();
    });

    it("shows blinking trigger and opens request list", () => {
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-1",
                    requester_name: "Alice",
                    requester_machine_id: "machine-a",
                    target_ve_id: "ve-1",
                    target_ve_name: "Analyst",
                },
            });
        });

        const trigger = screen.getByTestId("ve-auth-request-trigger");
        expect(trigger.className).toContain("ve-auth-request-trigger--blink");
        expect(trigger.textContent).toContain("1");

        fireEvent.click(trigger);
        expect(screen.getByTestId("ve-auth-request-center-req-1")).toBeTruthy();
        expect(screen.getByText("Alice")).toBeTruthy();
        expect(screen.getByText("Analyst")).toBeTruthy();
    });

    it("keeps request list inside the assistant title bar lane", () => {
        const rectSpy = vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
            const testId = this.getAttribute("data-testid");
            if (testId === "ai-title-bar") return { left: 510, top: 38, right: 1328, bottom: 76, width: 818, height: 38, x: 510, y: 38, toJSON: () => ({}) } as DOMRect;
            if (testId === "ve-auth-request-trigger") return { left: 824, top: 43, right: 854, bottom: 71, width: 30, height: 28, x: 824, y: 43, toJSON: () => ({}) } as DOMRect;
            return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
        });
        vi.stubGlobal("innerWidth", 1366);
        vi.stubGlobal("innerHeight", 768);

        render(
            <div data-testid="ai-title-bar">
                <VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />
            </div>
        );

        act(() => {
            eventHandlers.get("ve:auth_request")?.({ payload: { request_id: "center-req-lane" } });
        });
        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));

        const popover = screen.getByTestId("ve-auth-request-popover");
        expect(parseFloat(popover.style.left)).toBeGreaterThanOrEqual(522);
        expect(popover.style.position).toBe("fixed");

        rectSpy.mockRestore();
        vi.unstubAllGlobals();
    });

    it("keeps request list inside a short viewport", () => {
        const rectSpy = vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
            const testId = this.getAttribute("data-testid");
            if (testId === "ai-title-bar") return { left: 0, top: 0, right: 420, bottom: 38, width: 420, height: 38, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
            if (testId === "ve-auth-request-trigger") return { left: 360, top: 190, right: 390, bottom: 218, width: 30, height: 28, x: 360, y: 190, toJSON: () => ({}) } as DOMRect;
            return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
        });
        vi.stubGlobal("innerWidth", 420);
        vi.stubGlobal("innerHeight", 260);

        render(
            <div data-testid="ai-title-bar">
                <VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />
            </div>
        );

        act(() => {
            eventHandlers.get("ve:auth_request")?.({ payload: { request_id: "center-req-short" } });
        });
        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));

        const popover = screen.getByTestId("ve-auth-request-popover");
        const top = parseFloat(popover.style.top);
        const maxHeight = parseFloat(popover.style.maxHeight);
        expect(top).toBeLessThanOrEqual(86);
        expect(top + maxHeight).toBeLessThanOrEqual(246);

        rectSpy.mockRestore();
        vi.unstubAllGlobals();
    });

    it("plays configured ringtone when request arrives", async () => {
        LoadConfigMock.mockResolvedValue({
            group_discussion: {
                auth_request_sound_preset: "bright",
                auth_request_sound_muted: false,
            },
        });
        const originalAudioContext = window.AudioContext;
        const oscillatorStart = vi.fn();
        const oscillatorStop = vi.fn();
        const createOscillator = vi.fn(() => ({
            type: "sine",
            frequency: { setValueAtTime: vi.fn() },
            connect: vi.fn(),
            start: oscillatorStart,
            stop: oscillatorStop,
        }));
        const createGain = vi.fn(() => ({
            gain: {
                setValueAtTime: vi.fn(),
                exponentialRampToValueAtTime: vi.fn(),
            },
            connect: vi.fn(),
        }));
        const close = vi.fn();
        const AudioContextCtor = vi.fn(function () {
            return {
                currentTime: 0,
                state: "running",
                destination: {},
                createOscillator,
                createGain,
                close,
            };
        });
        Object.defineProperty(window, "AudioContext", { configurable: true, writable: true, value: AudioContextCtor });

        try {
            render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />);
            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    payload: {
                        request_id: "center-req-ring",
                        requester_name: "Alice",
                        requester_machine_id: "machine-a",
                        target_ve_id: "ve-1",
                        target_ve_name: "Analyst",
                    },
                });
            });

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
                await Promise.resolve();
            });

            expect(AudioContextCtor).toHaveBeenCalledTimes(1);
            expect(createOscillator).toHaveBeenCalled();
            expect(oscillatorStart).toHaveBeenCalled();
        } finally {
            Object.defineProperty(window, "AudioContext", { configurable: true, writable: true, value: originalAudioContext });
        }
    });

    it("does not start ringtone after request is handled during config load", async () => {
        let resolveConfig: ((value: unknown) => void) | undefined;
        LoadConfigMock.mockImplementation(() => new Promise((resolve) => { resolveConfig = resolve; }));
        const originalAudioContext = window.AudioContext;
        const AudioContextCtor = vi.fn(function () {
            return {
                currentTime: 0,
                state: "running",
                destination: {},
                createOscillator: vi.fn(),
                createGain: vi.fn(),
                close: vi.fn(),
            };
        });
        Object.defineProperty(window, "AudioContext", { configurable: true, writable: true, value: AudioContextCtor });

        try {
            const respond = vi.fn().mockResolvedValue(undefined);
            render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={respond} />);
            act(() => {
                eventHandlers.get("ve:auth_request")?.({
                    payload: {
                        request_id: "center-req-fast-handle",
                        requester_name: "Alice",
                        requester_machine_id: "machine-a",
                        target_ve_id: "ve-1",
                        target_ve_name: "Analyst",
                    },
                });
            });

            fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));
            fireEvent.click(screen.getByTestId("ve-auth-allow-long-center-req-fast-handle"));
            await act(async () => { await Promise.resolve(); });

            resolveConfig?.({ group_discussion: { auth_request_sound_muted: false } });
            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });

            expect(AudioContextCtor).not.toHaveBeenCalled();
            expect(screen.queryByTestId("ve-auth-request-center")).toBeNull();
        } finally {
            Object.defineProperty(window, "AudioContext", { configurable: true, writable: true, value: originalAudioContext });
        }
    });

    it("sends selected decision and removes handled request", async () => {
        const respond = vi.fn().mockResolvedValue(undefined);
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={respond} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-allow",
                    requester_name: "Bob",
                    requester_machine_id: "machine-b",
                    target_ve_id: "ve-2",
                    target_ve_name: "Helper",
                },
            });
        });

        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));
        fireEvent.click(screen.getByTestId("ve-auth-allow-long-center-req-allow"));
        await act(async () => { await vi.runAllTimersAsync(); });

        expect(respond).toHaveBeenCalledWith("center-req-allow", "allow_long");
        expect(EventsEmit).toHaveBeenCalledWith("ve:auth_handled", { request_id: "center-req-allow" });
        expect(screen.queryByTestId("ve-auth-request-center")).toBeNull();
    });

    it("keeps request visible and shows error when response fails", async () => {
        const respond = vi.fn().mockRejectedValue(new Error("network down"));
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={respond} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-fail",
                    requester_name: "Dana",
                    requester_machine_id: "machine-d",
                    target_ve_id: "ve-4",
                    target_ve_name: "Reviewer",
                },
            });
        });

        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));
        fireEvent.click(screen.getByTestId("ve-auth-deny-center-req-fail"));
        await act(async () => { await vi.runAllTimersAsync(); });

        expect(screen.getByTestId("ve-auth-request-center-req-fail")).toBeTruthy();
        expect(screen.getByRole("alert").textContent).toContain("network down");
    });

    it("ignores duplicate clicks while response is in flight", async () => {
        let resolveResponse: (() => void) | undefined;
        const respond = vi.fn().mockImplementation(() => new Promise<void>((resolve) => { resolveResponse = resolve; }));
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={respond} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-double",
                    requester_name: "Eli",
                    requester_machine_id: "machine-e",
                    target_ve_id: "ve-5",
                    target_ve_name: "Ops",
                },
            });
        });

        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));
        const allowButton = screen.getByTestId("ve-auth-allow-once-center-req-double");
        fireEvent.click(allowButton);
        fireEvent.click(allowButton);
        expect(respond).toHaveBeenCalledTimes(1);

        await act(async () => { resolveResponse?.(); await vi.runAllTimersAsync(); });
        expect(screen.queryByTestId("ve-auth-request-center")).toBeNull();
    });

    it("removes expired requests and hides the trigger", () => {
        const now = new Date("2026-05-30T10:00:00.000Z");
        vi.setSystemTime(now);
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={vi.fn()} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-expiring",
                    requester_name: "Eli",
                    requester_machine_id: "machine-e",
                    target_ve_id: "ve-5",
                    target_ve_name: "Ops",
                    expires_at: new Date(now.getTime() + 1000).toISOString(),
                },
            });
        });

        expect(screen.getByTestId("ve-auth-request-center")).toBeTruthy();
        act(() => { vi.advanceTimersByTime(1000); });
        expect(screen.queryByTestId("ve-auth-request-center")).toBeNull();
    });

    it.each([
        ["deny", "ve-auth-deny-center-req-action"],
        ["block", "ve-auth-block-center-req-action"],
        ["allow_once", "ve-auth-allow-once-center-req-action"],
    ])("sends %s decision", async (decision, testId) => {
        const respond = vi.fn().mockResolvedValue(undefined);
        render(<VEAuthorizationRequestCenter theme={mockTheme} lang="en" respondAuthRequest={respond} />);

        act(() => {
            eventHandlers.get("ve:auth_request")?.({
                payload: {
                    request_id: "center-req-action",
                    requester_name: "Casey",
                    requester_machine_id: "machine-c",
                    target_ve_id: "ve-3",
                    target_ve_name: "Assistant",
                },
            });
        });

        fireEvent.click(screen.getByTestId("ve-auth-request-trigger"));
        fireEvent.click(screen.getByTestId(testId));
        await act(async () => { await vi.runAllTimersAsync(); });

        expect(respond).toHaveBeenCalledWith("center-req-action", decision);
    });
});
