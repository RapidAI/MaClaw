import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { VirtualEmployeeTab, truncateText, policyIcon } from '../VirtualEmployeeTab';
import type { VirtualEmployeeEntry, VETabProps } from '../VirtualEmployeeTab';
import type { Theme } from '../aiAssistantPanelTheme';
import { safeAvatarDataURL, safeAvatarSourceDataURL } from '../virtualEmployeeAvatar';

// Mock Wails runtime
const eventHandlers = new Map<string, (...args: any[]) => void>();
vi.mock('../../../../wailsjs/runtime', () => ({
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

const sampleVEs: VirtualEmployeeEntry[] = [
    {
        id: "ve-1",
        name: "AI 翻译助手",
        skill_description: "专业中英文翻译，支持技术文档和商务文件",
        access_policy: "public",
        status: "active",
        online_status: "online",
    },
    {
        id: "ve-2",
        name: "代码审查专家这个名字超过了二十个字符的限制",
        skill_description: "精通 Go/TypeScript/Python 代码审查，能发现潜在的安全漏洞和性能问题以及架构设计缺陷",
        access_policy: "per_request",
        status: "active",
        online_status: "offline",
    },
    {
        id: "ve-3",
        name: "数据分析师",
        skill_description: "数据清洗、可视化和统计分析",
        access_policy: "whitelist",
        status: "active",
        online_status: "online",
    },
];

function renderVETab(overrides: Partial<VETabProps> = {}, listResult?: VirtualEmployeeEntry[] | Error) {
    const onStartConversation = vi.fn();
    const onSetFavorite = vi.fn();
    const onRemoveFavorite = vi.fn();
    const listFn = vi.fn().mockImplementation(() => {
        if (listResult instanceof Error) return Promise.reject(listResult);
        return Promise.resolve(listResult ?? sampleVEs);
    });

    const result = render(
        <VirtualEmployeeTab
            onStartConversation={onStartConversation}
            onSetFavorite={onSetFavorite}
            onRemoveFavorite={onRemoveFavorite}
            theme={mockTheme}
            lang="zh"
            listVirtualEmployees={listFn}
            {...overrides}
        />
    );

    return { ...result, onStartConversation, onSetFavorite, onRemoveFavorite, listFn };
}

describe('VirtualEmployeeTab', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    // --- Helper function tests ---

    describe('truncateText', () => {
        it('returns original text if within limit', () => {
            expect(truncateText("hello", 20)).toBe("hello");
        });

        it('truncates and adds ellipsis when exceeding limit', () => {
            const longText = "这是一个超过二十个字符的很长的名字测试用例文本";
            expect(longText.length).toBeGreaterThan(20);
            expect(truncateText(longText, 20)).toBe(longText.slice(0, 20) + "…");
        });

        it('handles empty string', () => {
            expect(truncateText("", 20)).toBe("");
        });
    });

    describe('policyIcon', () => {
        it('returns correct icons for each policy', () => {
            expect(policyIcon("public")).toBe("🌐");
            expect(policyIcon("whitelist")).toBe("✅");
            expect(policyIcon("blacklist")).toBe("🚫");
            expect(policyIcon("per_request")).toBe("🔒");
            expect(policyIcon("unknown")).toBe("❓");
        });
    });

    describe('safeAvatarDataURL', () => {
        it('accepts image data URLs', () => {
            expect(safeAvatarDataURL('data:image/jpeg;base64,/9j/')).toBe('data:image/jpeg;base64,/9j/');
        });

        it('accepts final avatars by decoded byte size instead of raw data URL length', () => {
            const avatar = `data:image/jpeg;base64,${btoa(`\xff\xd8\xff${'\0'.repeat(800 * 1024)}`)}`;
            expect(avatar.length).toBeGreaterThan(1024 * 1024);
            expect(safeAvatarDataURL(avatar)).toBe(avatar);
        });

        it('rejects script, remote, and malformed URLs', () => {
            expect(safeAvatarDataURL('javascript:alert(1)')).toBe('');
            expect(safeAvatarDataURL('https://example.com/avatar.png')).toBe('');
            expect(safeAvatarDataURL('data:image/png;base64,QUJD')).toBe('');
            expect(safeAvatarDataURL('data:image/jpeg;base64,iVBORw0KGgo=')).toBe('');
            expect(safeAvatarDataURL('data:image/png;base64,%%%')).toBe('');
            expect(safeAvatarDataURL('data:image/png;base64,=')).toBe('');
        });

        it('rejects oversized avatar data URLs before decoding', () => {
            expect(safeAvatarDataURL(`data:image/png;base64,${'a'.repeat(2 * 1024 * 1024)}`)).toBe('');
        });

        it('allows source photos larger than the final saved avatar limit', () => {
            const sourcePhoto = `data:image/png;base64,${btoa(`\x89PNG\r\n\x1a\n${'\0'.repeat(1100 * 1024)}`)}`;
            expect(safeAvatarDataURL(sourcePhoto)).toBe('');
            expect(safeAvatarSourceDataURL(sourcePhoto)).toBe(sourcePhoto);
        });
    });

    // --- Loading state ---

    describe('loading state', () => {
        it('shows loading indicator initially', async () => {
            renderVETab();
            expect(screen.getByTestId("ve-loading")).toBeTruthy();
        });

        it('renders results after loading completes', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-list-container")).toBeTruthy();
        });
    });

    // --- Empty states ---

    describe('empty states', () => {
        it('shows hub unavailable message on error', async () => {
            renderVETab({}, new Error("network error"));
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-empty-hub")).toBeTruthy();
        });

        it('shows empty list message when no VEs returned', async () => {
            renderVETab({}, []);
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-empty-list")).toBeTruthy();
        });
    });

    // --- List rendering ---

    describe('list rendering', () => {
        it('renders all VE items', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-item-ve-1")).toBeTruthy();
            expect(screen.getByTestId("ve-item-ve-2")).toBeTruthy();
            expect(screen.getByTestId("ve-item-ve-3")).toBeTruthy();
        });

        it('uses readable list names instead of raw ids', async () => {
            renderVETab({}, [{
                id: "profile-raw",
                machine_id: "machine-raw",
                name: "m_b1821505498d817c",
                skill_description: "",
                access_policy: "public",
                status: "active",
                online_status: "online",
            }]);
            await act(async () => { await vi.runAllTimersAsync(); });

            const item = screen.getByTestId("ve-item-profile-raw");
            expect(item.textContent).toContain("数字员工 1");
            expect(item.textContent).not.toContain("m_b1821505498d817c");
            expect(item.getAttribute("title")).toBe("数字员工 1");
        });

        it('shows green dot for online and gray dot for offline', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            const onlineIndicator = screen.getByTestId("ve-status-ve-1");
            const offlineIndicator = screen.getByTestId("ve-status-ve-2");
            expect(onlineIndicator.style.background).toBe("rgb(34, 197, 94)"); // #22c55e
            expect(offlineIndicator.style.background).toBe("rgb(156, 163, 175)"); // #9ca3af
        });

        it('shows "待确认" badge for per_request policy', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.getByTestId("ve-badge-ve-2")).toBeTruthy();
            expect(screen.getByTestId("ve-badge-ve-2").textContent).toBe("待确认");
        });

        it('does not show badge for non-per_request policies', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(screen.queryByTestId("ve-badge-ve-1")).toBeNull();
            expect(screen.queryByTestId("ve-badge-ve-3")).toBeNull();
        });
    });

    // --- Interactions ---

    describe('interactions', () => {
        it('calls onStartConversation on click', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            fireEvent.click(screen.getByTestId("ve-item-ve-1"));
            expect(onStartConversation).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('calls onStartConversation from keyboard activation', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            fireEvent.keyDown(screen.getByTestId("ve-item-ve-1"), { key: "Enter" });
            fireEvent.keyDown(screen.getByTestId("ve-item-ve-2"), { key: " " });
            expect(onStartConversation).toHaveBeenNthCalledWith(1, sampleVEs[0]);
            expect(onStartConversation).toHaveBeenNthCalledWith(2, sampleVEs[1]);
        });

        it('shows context menu on right-click', async () => {
            renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            expect(screen.getByTestId("ve-context-menu")).toBeTruthy();
            expect(screen.getByTestId("ve-menu-conversation")).toBeTruthy();
            expect(screen.getByTestId("ve-menu-set-favorite")).toBeTruthy();
        });

        it('calls onStartConversation from context menu "对话"', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-conversation"));
            expect(onStartConversation).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('calls onSetFavorite from context menu "设为常用"', async () => {
            const { onSetFavorite } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-set-favorite"));
            expect(onSetFavorite).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('treats machine-id favorites as already favorited in the context menu', async () => {
            const employee = {
                id: "profile-1",
                machine_id: "machine-1",
                name: "Machine Bot",
                skill_description: "Ops",
                access_policy: "public" as const,
                status: "active",
                online_status: "online" as const,
            };
            const { onRemoveFavorite, onSetFavorite } = renderVETab({ favoriteEmployeeIds: ["machine-1"] }, [employee]);
            await act(async () => { await vi.runAllTimersAsync(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-profile-1"));
            fireEvent.click(screen.getByTestId("ve-menu-set-favorite"));

            expect(onRemoveFavorite).toHaveBeenCalledWith(employee);
            expect(onSetFavorite).not.toHaveBeenCalled();
        });
    });

    // --- Real-time updates (throttled refresh) ---

    describe('real-time updates', () => {
        it('refreshes on ve:list_update event with 500ms throttle', async () => {
            const { listFn } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(listFn).toHaveBeenCalledTimes(1);

            // Trigger event
            act(() => { eventHandlers.get("ve:list_update")?.(); });
            expect(listFn).toHaveBeenCalledTimes(2);

            // Second event within 500ms should be throttled
            act(() => { eventHandlers.get("ve:list_update")?.(); });
            expect(listFn).toHaveBeenCalledTimes(2); // still 2

            // After 500ms, pending refresh fires
            await act(async () => { vi.advanceTimersByTime(500); });
            expect(listFn).toHaveBeenCalledTimes(3);
        });

        it('refreshes on ve:status_change event', async () => {
            const { listFn } = renderVETab();
            await act(async () => { await vi.runAllTimersAsync(); });
            expect(listFn).toHaveBeenCalledTimes(1);

            act(() => { eventHandlers.get("ve:status_change")?.(); });
            expect(listFn).toHaveBeenCalledTimes(2);
        });
    });
});
