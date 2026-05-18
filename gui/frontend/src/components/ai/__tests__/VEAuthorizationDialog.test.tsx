import { act, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    VEAuthorizationDialog,
    VEAuthBlinkingIndicator,
} from "../VEAuthorizationDialog";
import type { Theme } from "../aiAssistantPanelTheme";

// Mock Wails runtime
const eventHandlers = new Map<string, (...args: any[]) => void>();
vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((eventName: string, handler: (...args: any[]) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
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

    it("disappears when all requests are handled", () => {
        render(<VEAuthBlinkingIndicator theme={mockTheme} lang="zh" />);

        act(() => { eventHandlers.get("ve:auth_request")?.(); });
        expect(screen.getByTestId("ve-auth-blink-indicator")).toBeTruthy();

        act(() => { eventHandlers.get("ve:auth_handled")?.(); });
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
