import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { VirtualEmployeeTab, truncateText, policyIcon, policyLabel } from '../VirtualEmployeeTab';
import type { VirtualEmployeeEntry, VETabProps } from '../VirtualEmployeeTab';
import type { Theme } from '../aiAssistantPanelTheme';
import { safeAvatarDataURL, safeAvatarSourceDataURL } from '../virtualEmployeeAvatar';
import { isVirtualEmployeeOnline } from '../virtualEmployeeStatus';

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
        name: "Translator",
        skill_description: "Chinese English document translation",
        access_policy: "public",
        status: "active",
        online_status: "online",
    },
    {
        id: "ve-2",
        name: "Code Review Expert With A Very Long Name",
        skill_description: "Go/TypeScript/Python code review",
        access_policy: "per_request",
        status: "active",
        online_status: "online",
    },
    {
        id: "ve-3",
        name: "Data Analyst",
        skill_description: "Data cleaning and visualization",
        access_policy: "whitelist",
        status: "active",
        online_status: "online",
    },
    {
        id: "ve-4",
        name: "Offline Assistant",
        skill_description: "Temporarily offline",
        access_policy: "public",
        status: "active",
        online_status: "offline",
    },
];

function renderVETab(overrides: Partial<VETabProps> = {}, listResult?: VirtualEmployeeEntry[] | Error) {
    const onStartConversation = vi.fn();
    const onSetFavorite = vi.fn();
    const onRemoveFavorite = vi.fn();
    const onRenameEmployee = vi.fn();
    const listFn = vi.fn().mockImplementation(() => {
        if (listResult instanceof Error) return Promise.reject(listResult);
        return Promise.resolve(listResult ?? sampleVEs);
    });

    const result = render(
        <VirtualEmployeeTab
            onStartConversation={onStartConversation}
            onSetFavorite={onSetFavorite}
            onRemoveFavorite={onRemoveFavorite}
            onRenameEmployee={onRenameEmployee}
            theme={mockTheme}
            lang="zh"
            listVirtualEmployees={listFn}
            {...overrides}
        />
    );

    return { ...result, onStartConversation, onSetFavorite, onRemoveFavorite, onRenameEmployee, listFn };
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

    describe('isVirtualEmployeeOnline', () => {
        it('normalizes case and whitespace', () => {
            expect(isVirtualEmployeeOnline({ online_status: " Online " })).toBe(true);
            expect(isVirtualEmployeeOnline({ online_status: " OFFLINE " })).toBe(false);
            expect(isVirtualEmployeeOnline(null)).toBe(false);
        });
    });

    describe('truncateText', () => {
        it('returns original text if within limit', () => {
            expect(truncateText("hello", 20)).toBe("hello");
        });

        it('truncates and adds ellipsis when exceeding limit', () => {
            const longText = "this is a very long digital employee display name";
            expect(longText.length).toBeGreaterThan(20);
            expect(truncateText(longText, 20)).toBe(longText.slice(0, 20) + "\u2026");
        });

        it('handles empty string', () => {
            expect(truncateText("", 20)).toBe("");
        });
    });

    describe('policyIcon', () => {
        it('returns correct icons for each policy', () => {
            expect(policyIcon("public")).toBe("\u{1F310}");
            expect(policyIcon("whitelist")).toBe("\u2705");
            expect(policyIcon("blacklist")).toBe("\u{1F6AB}");
            expect(policyIcon("per_request")).toBe("\u{1F512}");
            expect(policyIcon("unknown")).toBe("\u2753");
        });

        it('returns readable labels for each policy', () => {
            expect(policyLabel("per_request", "zh")).toBeTruthy();
            expect(policyLabel("per_request", "en")).toBe("Approval required");
            expect(policyLabel("blacklist", "zh")).toBeTruthy();
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
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-list-container")).toBeTruthy();
        });
    });

    // --- Empty states ---

    describe('empty states', () => {
        it('shows hub unavailable message on error', async () => {
            renderVETab({}, new Error("network error"));
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-empty-hub")).toBeTruthy();
        });

        it('shows empty list message when no VEs returned', async () => {
            renderVETab({}, []);
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-empty-list")).toBeTruthy();
        });

        it('treats non-array list responses as empty', async () => {
            const listFn = vi.fn().mockResolvedValue({ employees: sampleVEs });
            renderVETab({ listVirtualEmployees: listFn as unknown as VETabProps["listVirtualEmployees"] });
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-empty-list")).toBeTruthy();
        });

        it('shows empty list message when all returned VEs are offline', async () => {
            renderVETab({}, [sampleVEs[3]]);
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-empty-list").textContent).toContain("\u5728\u7ebf");
        });
    });

    // --- List rendering ---

    describe('list rendering', () => {
        it('renders online VE items only', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-item-ve-1")).toBeTruthy();
            expect(screen.getByTestId("ve-item-ve-2")).toBeTruthy();
            expect(screen.getByTestId("ve-item-ve-3")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-4")).toBeNull();
        });

        it('treats online status case and whitespace as online', async () => {
            renderVETab({}, [
                { ...sampleVEs[0], id: "ve-spaced-online", online_status: " Online " as any },
                { ...sampleVEs[1], id: "ve-spaced-offline", online_status: " Offline " as any },
            ]);
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-item-ve-spaced-online")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-spaced-offline")).toBeNull();
        });

        it('filters online VE items by search query', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(screen.getByTestId("ve-search-input"), { target: { value: "Go/TypeScript" } });

            expect(screen.queryByTestId("ve-item-ve-1")).toBeNull();
            expect(screen.getByTestId("ve-item-ve-2")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-3")).toBeNull();
        });

        it('shows empty state when search has no online matches', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(screen.getByTestId("ve-search-input"), { target: { value: "no-such-employee" } });

            expect(screen.getByTestId("ve-empty-list")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-1")).toBeNull();
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
            await act(async () => { await Promise.resolve(); });

            const item = screen.getByTestId("ve-item-profile-raw");
            expect(item.textContent).toContain("1");
            expect(item.textContent).not.toContain("m_b1821505498d817c");
            expect(item.getAttribute("title")).toContain("1");
        });

        it('uses local renamed display names in the main employee list', async () => {
            renderVETab({ favoriteEmployeeNames: { "ve-1": "Dragon Bot" } });
            await act(async () => { await Promise.resolve(); });

            expect(screen.getByTestId("ve-item-ve-1").textContent).toContain("Dragon Bot");
            expect(screen.getByTestId("ve-item-ve-1").textContent).not.toContain("Translator");
        });

        it('opens conversations with local renamed display names', async () => {
            const { onStartConversation } = renderVETab({ favoriteEmployeeNames: { "ve-1": "Dragon Bot" } });
            await act(async () => { await Promise.resolve(); });

            fireEvent.click(screen.getByTestId("ve-item-ve-1"));

            expect(onStartConversation).toHaveBeenCalledWith(expect.objectContaining({ id: "ve-1", name: "Dragon Bot" }));
        });

        it('filters by local renamed display names', async () => {
            renderVETab({ favoriteEmployeeNames: { "ve-1": "Dragon Bot" } });
            await act(async () => { await Promise.resolve(); });

            fireEvent.change(screen.getByTestId("ve-search-input"), { target: { value: "dragon" } });

            expect(screen.getByTestId("ve-item-ve-1")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-2")).toBeNull();
        });

        it('shows green dot for online employees', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });
            const onlineIndicator = screen.getByTestId("ve-status-ve-1");
            expect(onlineIndicator.style.background).toBe("rgb(34, 197, 94)"); // #22c55e
            expect(screen.queryByTestId("ve-status-ve-4")).toBeNull();
        });

        it('shows "需同意" badge for per_request policy', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-badge-ve-2")).toBeTruthy();
            expect(screen.getByTestId("ve-badge-ve-2").textContent).toBe("\u9700\u540c\u610f");
            expect(screen.getByTestId("ve-badge-ve-2").getAttribute("title")).toBeTruthy();
        });

        it('does not show badge for non-per_request policies', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });
            expect(screen.queryByTestId("ve-badge-ve-1")).toBeNull();
            expect(screen.queryByTestId("ve-badge-ve-3")).toBeNull();
        });
    });

    // --- Interactions ---

    describe('interactions', () => {
        it('calls onStartConversation on click', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByTestId("ve-item-ve-1"));
            expect(onStartConversation).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('calls onStartConversation from keyboard activation', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            fireEvent.keyDown(screen.getByTestId("ve-item-ve-1"), { key: "Enter" });
            fireEvent.keyDown(screen.getByTestId("ve-item-ve-3"), { key: " " });
            expect(onStartConversation).toHaveBeenNthCalledWith(1, sampleVEs[0]);
            expect(onStartConversation).toHaveBeenNthCalledWith(2, sampleVEs[2]);
        });

        it('shows context menu on right-click', async () => {
            renderVETab();
            await act(async () => { await Promise.resolve(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            expect(screen.getByTestId("ve-context-menu")).toBeTruthy();
            expect(screen.getByTestId("ve-menu-conversation")).toBeTruthy();
            expect(screen.getByTestId("ve-menu-set-favorite")).toBeTruthy();
        });

        it('calls onStartConversation from context menu "对话"', async () => {
            const { onStartConversation } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-conversation"));
            expect(onStartConversation).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('starts context-menu conversations with local renamed display names', async () => {
            const { onStartConversation } = renderVETab({ favoriteEmployeeNames: { "ve-1": "Dragon Bot" } });
            await act(async () => { await Promise.resolve(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-conversation"));

            expect(onStartConversation).toHaveBeenCalledWith(expect.objectContaining({ id: "ve-1", name: "Dragon Bot" }));
        });

        it('calls onSetFavorite from context menu "设为常用"', async () => {
            const { onSetFavorite } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-set-favorite"));
            expect(onSetFavorite).toHaveBeenCalledWith(sampleVEs[0]);
        });

        it('renames an employee from the context menu and updates the list label', async () => {
            const { onRenameEmployee } = renderVETab();
            await act(async () => { await Promise.resolve(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));
            fireEvent.click(screen.getByTestId("ve-menu-rename"));
            fireEvent.change(screen.getByTestId("ve-rename-input"), { target: { value: "Docs Translator" } });
            fireEvent.click(screen.getByTestId("ve-rename-save"));
            await act(async () => { await Promise.resolve(); });

            expect(onRenameEmployee).toHaveBeenCalledWith(sampleVEs[0], "Docs Translator");
            expect(screen.getByTestId("ve-item-ve-1").textContent).toContain("Docs Translator");
        });

        it('does not show favorite action for resident employees', async () => {
            const resident = { ...sampleVEs[0], resident: true };
            renderVETab({}, [resident]);
            await act(async () => { await Promise.resolve(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-ve-1"));

            expect(screen.getByTestId("ve-menu-conversation")).toBeTruthy();
            expect(screen.queryByTestId("ve-menu-set-favorite")).toBeNull();
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
            await act(async () => { await Promise.resolve(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-profile-1"));
            fireEvent.click(screen.getByTestId("ve-menu-set-favorite"));

            expect(onRemoveFavorite).toHaveBeenCalledWith(employee);
            expect(onSetFavorite).not.toHaveBeenCalled();
        });

        it('treats generated VE alias favorites as already favorited in the context menu', async () => {
            const employee = {
                id: "ve_machine-1",
                machine_id: "machine-1",
                name: "Machine Bot",
                skill_description: "Ops",
                access_policy: "public" as const,
                status: "active",
                online_status: "online" as const,
            };
            const { onRemoveFavorite, onSetFavorite } = renderVETab({ favoriteEmployeeIds: ["ve-machine-1"] }, [employee]);
            await act(async () => { await Promise.resolve(); });

            fireEvent.contextMenu(screen.getByTestId("ve-item-ve_machine-1"));
            fireEvent.click(screen.getByTestId("ve-menu-set-favorite"));

            expect(onRemoveFavorite).toHaveBeenCalledWith(employee);
            expect(onSetFavorite).not.toHaveBeenCalled();
        });
    });

    // --- Real-time updates (throttled refresh) ---

    describe('real-time updates', () => {
        it('refreshes on ve:list_update event with 500ms throttle', async () => {
            const { listFn } = renderVETab();
            await act(async () => { await Promise.resolve(); });
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
            await act(async () => { await Promise.resolve(); });
            expect(listFn).toHaveBeenCalledTimes(1);

            act(() => { eventHandlers.get("ve:status_change")?.(); });
            expect(listFn).toHaveBeenCalledTimes(2);
        });

        it('refreshes when the manual refresh button is clicked', async () => {
            const { listFn } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            expect(listFn).toHaveBeenCalledTimes(1);

            fireEvent.click(screen.getByTestId("ve-refresh-button"));
            expect(listFn).toHaveBeenCalledTimes(2);
        });

        it('keeps the newest refresh result when requests resolve out of order', async () => {
            let resolveOld: ((value: VirtualEmployeeEntry[]) => void) | undefined;
            let resolveNew: ((value: VirtualEmployeeEntry[]) => void) | undefined;
            const oldResult = [{
                ...sampleVEs[0],
                id: "ve-old",
                name: "Old Result",
            }];
            const secondResult = [{
                ...sampleVEs[0],
                id: "ve-new",
                name: "New Result",
            }];
            const listFn = vi.fn()
                .mockResolvedValueOnce(sampleVEs)
                .mockImplementationOnce(() => new Promise<VirtualEmployeeEntry[]>((resolve) => { resolveOld = resolve; }))
                .mockImplementationOnce(() => new Promise<VirtualEmployeeEntry[]>((resolve) => { resolveNew = resolve; }));

            renderVETab({ listVirtualEmployees: listFn });
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByTestId("ve-refresh-button"));
            act(() => { eventHandlers.get("ve:status_change")?.(); });
            expect(listFn).toHaveBeenCalledTimes(3);

            await act(async () => {
                resolveNew?.(secondResult);
                await Promise.resolve();
            });
            expect(screen.getByTestId("ve-item-ve-new")).toBeTruthy();

            await act(async () => {
                resolveOld?.(oldResult);
                await Promise.resolve();
            });
            expect(screen.getByTestId("ve-item-ve-new")).toBeTruthy();
            expect(screen.queryByTestId("ve-item-ve-old")).toBeNull();
        });

        it('clears the refreshing state when a full reload supersedes a background refresh', async () => {
            let resolveBackground: ((value: VirtualEmployeeEntry[]) => void) | undefined;
            const firstList = vi.fn()
                .mockResolvedValueOnce(sampleVEs)
                .mockImplementationOnce(() => new Promise<VirtualEmployeeEntry[]>((resolve) => { resolveBackground = resolve; }));
            const secondList = vi.fn().mockResolvedValue(sampleVEs.slice(0, 1));
            const rendered = renderVETab({ listVirtualEmployees: firstList });
            await act(async () => { await Promise.resolve(); });

            fireEvent.click(screen.getByTestId("ve-refresh-button"));
            expect((screen.getByTestId("ve-refresh-button") as HTMLButtonElement).disabled).toBe(true);

            rendered.rerender(
                <VirtualEmployeeTab
                    onStartConversation={vi.fn()}
                    onSetFavorite={vi.fn()}
                    onRemoveFavorite={vi.fn()}
                    theme={mockTheme}
                    lang="zh"
                    listVirtualEmployees={secondList}
                />
            );
            await act(async () => { await Promise.resolve(); });
            expect((screen.getByTestId("ve-refresh-button") as HTMLButtonElement).disabled).toBe(false);

            await act(async () => {
                resolveBackground?.(sampleVEs);
                await Promise.resolve();
            });
            expect((screen.getByTestId("ve-refresh-button") as HTMLButtonElement).disabled).toBe(false);
        });

        it('keeps the current list when a background refresh fails', async () => {
            const listFn = vi.fn()
                .mockResolvedValueOnce(sampleVEs)
                .mockRejectedValueOnce(new Error("temporary outage"));
            renderVETab({ listVirtualEmployees: listFn });
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId("ve-item-ve-1")).toBeTruthy();

            fireEvent.click(screen.getByTestId("ve-refresh-button"));
            await act(async () => { await Promise.resolve(); });

            expect(screen.getByTestId("ve-list-container")).toBeTruthy();
            expect(screen.getByTestId("ve-item-ve-1")).toBeTruthy();
            expect(screen.queryByTestId("ve-empty-hub")).toBeNull();
        });

        it('refreshes every 30 seconds', async () => {
            const { listFn } = renderVETab();
            await act(async () => { await Promise.resolve(); });
            expect(listFn).toHaveBeenCalledTimes(1);

            await act(async () => { vi.advanceTimersByTime(30000); });
            expect(listFn).toHaveBeenCalledTimes(2);
        });
    });
});
