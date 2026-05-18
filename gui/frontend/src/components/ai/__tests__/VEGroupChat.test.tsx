// @vitest-environment jsdom
import { StrictMode } from "react";
import { cleanup, fireEvent, render, screen, waitFor, act } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    VEGroupChatView,
    ParticipantSelector,
    GroupMessageBubble,
    ParticipantOfflineNotice,
    buildGroupTabTitle,
    getParticipantColor,
    useGroupConfig,
    DEFAULT_MAX_GROUP_PARTICIPANTS,
    MAX_UPPER_LIMIT,
    TAB_TITLE_MAX_LENGTH,
} from "../VEGroupChat";
import type { GroupParticipant, GroupMessage } from "../VEGroupChat";
import type { VirtualEmployeeEntry } from "../VirtualEmployeeTab";
import { lightTheme } from "../aiAssistantPanelTheme";

// Mock Wails runtime
const mockEventsOn = vi.fn((_event: string, _handler: any) => vi.fn());
const mockEventsOff = vi.fn();

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: (event: string, handler: any) => mockEventsOn(event, handler),
    EventsOff: (...args: unknown[]) => mockEventsOff(...args),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

// --- Test Theme ---
const testTheme = {
    ...lightTheme,
    bg: "#ffffff",
    text: "#1a1a1a",
    textMuted: "#6b7280",
    divider: "#e5e7eb",
    fieldBg: "#f3f4f6",
    fieldBorder: "#d1d5db",
    inputText: "#1a1a1a",
    inputBarBg: "#f9fafb",
    sendBtnColor: "#3b82f6",
    sendBtnBorder: "#2563eb",
    sendBtnBg: "#2563eb",
    borderLeft: "#3b82f6",
    responseBorderLeft: "#10b981",
    errorBg: "#fef2f2",
    errorText: "#dc2626",
    errorBorder: "#fecaca",
    closeBtnColor: "#dc2626",
};

// --- Test Data ---
const mockParticipants: GroupParticipant[] = [
    { id: "ve-1", name: "数字员工A", online: true },
    { id: "ve-2", name: "数字员工B", online: true },
];

const mockMessages: GroupMessage[] = [
    { id: "msg-1", fromId: "user", fromName: "User", content: "Hello", timestamp: 1000 },
    { id: "msg-2", fromId: "ve-1", fromName: "Assistant A", content: "Hi", timestamp: 2000 },
    { id: "msg-3", fromId: "ve-2", fromName: "Assistant B", content: "How can I help?", timestamp: 3000 },
];

const mockAvailableVEs: VirtualEmployeeEntry[] = [
    { id: "ve-3", name: "数字员工C", skill_description: "翻译", access_policy: "public", status: "active", online_status: "online" },
    { id: "ve-4", name: "数字员工D", skill_description: "翻译", access_policy: "public", status: "active", online_status: "online" },
    { id: "ve-1", name: "数字员工A", skill_description: "翻译", access_policy: "public", status: "active", online_status: "online" },
];

// ─── buildGroupTabTitle ──────────────────────────────────────────────

describe("buildGroupTabTitle", () => {
    it("returns '群聊' for empty participants", () => {
        expect(buildGroupTabTitle([])).toBe("群聊");
    });

    it("returns single name for one participant", () => {
        expect(buildGroupTabTitle([{ id: "1", name: "Alice", online: true }])).toBe("Alice");
    });

    it("joins multiple names with comma", () => {
        const participants = [
            { id: "1", name: "Alice", online: true },
            { id: "2", name: "Bob", online: true },
        ];
        expect(buildGroupTabTitle(participants)).toBe("Alice, Bob");
    });

    it("truncates with ... when exceeding max length", () => {
        const participants = [
            { id: "1", name: "VeryLongNameAlice", online: true },
            { id: "2", name: "VeryLongNameBob", online: true },
            { id: "3", name: "VeryLongNameCharlie", online: true },
        ];
        const title = buildGroupTabTitle(participants);
        expect(title.length).toBeLessThanOrEqual(TAB_TITLE_MAX_LENGTH + 3); // +3 for "..."
        expect(title.endsWith("...")).toBe(true);
    });

    it("does not truncate when within limit", () => {
        const participants = [
            { id: "1", name: "A", online: true },
            { id: "2", name: "B", online: true },
        ];
        const title = buildGroupTabTitle(participants);
        expect(title).toBe("A, B");
        expect(title.endsWith("...")).toBe(false);
    });
});

// ─── getParticipantColor ─────────────────────────────────────────────

describe("getParticipantColor", () => {
    it("returns different colors for different indices", () => {
        const c0 = getParticipantColor(0);
        const c1 = getParticipantColor(1);
        expect(c0).not.toBe(c1);
    });

    it("wraps around for indices beyond color array length", () => {
        const c0 = getParticipantColor(0);
        const c10 = getParticipantColor(10);
        expect(c0).toBe(c10);
    });
});

// ─── ParticipantSelector ─────────────────────────────────────────────

describe("ParticipantSelector", () => {
    it("renders the + button", () => {
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );
        expect(screen.getByTestId("group-add-participant-btn")).toBeTruthy();
    });

    it("shows participant picker on click", async () => {
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => {
            expect(screen.getByTestId("group-participant-picker")).toBeTruthy();
        });
    });

    it("loads the participant picker under React StrictMode", async () => {
        render(
            <StrictMode>
                <ParticipantSelector
                    sessionId="session-1"
                    currentParticipants={mockParticipants}
                    maxGroupParticipants={5}
                    theme={testTheme}
                    onAdd={vi.fn()}
                    listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
                />
            </StrictMode>
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy());
    });

    it("filters out VEs already in the group", async () => {
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => {
            // ve-1 is already in the group, should not appear
            expect(screen.queryByTestId("group-picker-item-ve-1")).toBeNull();
            // ve-3 and ve-4 should appear
            expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy();
            expect(screen.getByTestId("group-picker-item-ve-4")).toBeTruthy();
        });
    });

    it("shows limit error when max participants reached", () => {
        const fullParticipants: GroupParticipant[] = [
            { id: "ve-1", name: "A", online: true },
            { id: "ve-2", name: "B", online: true },
            { id: "ve-3", name: "C", online: true },
            { id: "ve-4", name: "D", online: true },
            { id: "ve-5", name: "E", online: true },
        ];

        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={fullParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        expect(screen.getByTestId("group-limit-error")).toBeTruthy();
        expect(screen.getByTestId("group-limit-error").textContent).toContain("群聊人数已满（最多");
    });

    it("does not expose raw ids for unnamed available VEs", async () => {
        const unnamedVEs: VirtualEmployeeEntry[] = [
            { id: "m_b1821505498d817c", name: "", skill_description: "", access_policy: "public", status: "active", online_status: "online" },
        ];

        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={[]}
                maxGroupParticipants={5}
                theme={testTheme}
                lang="zh"
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(unnamedVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => expect(screen.getByText("数字员工 1")).toBeTruthy());
        expect(screen.queryByText("m_b1821505498d817c")).toBeNull();
    });

    it("does not expose raw-looking available VE names even when ids differ", async () => {
        const rawNamedVEs: VirtualEmployeeEntry[] = [
            { id: "profile-raw", machine_id: "machine-raw", name: "m_b1821505498d817c", skill_description: "", access_policy: "public", status: "active", online_status: "online" },
        ];

        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={[]}
                maxGroupParticipants={5}
                theme={testTheme}
                lang="en"
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve(rawNamedVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => expect(screen.getByText("Digital employee 1")).toBeTruthy());
        expect(screen.queryByText("m_b1821505498d817c")).toBeNull();
    });

    it("calls onAdd when a VE is selected", async () => {
        const onAdd = vi.fn();
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={onAdd}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => {
            expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy();
        });

        fireEvent.click(screen.getByTestId("group-picker-item-ve-3"));

        expect(onAdd).toHaveBeenCalledWith(
            expect.objectContaining({ id: "ve-3", name: "数字员工C" })
        );
    });

    it("keeps the picker open while an async participant add is pending", async () => {
        let resolveAdd: (() => void) | undefined;
        const onAdd = vi.fn(() => new Promise<void>((resolve) => { resolveAdd = resolve; }));
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                lang="en"
                onAdd={onAdd}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy());
        fireEvent.click(screen.getByTestId("group-picker-item-ve-3"));

        expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ id: "ve-3" }));
        expect(screen.getByTestId("group-participant-picker")).toBeTruthy();
        expect(screen.getByText("Adding...")).toBeTruthy();

        await act(async () => { resolveAdd?.(); });
        await waitFor(() => expect(screen.queryByTestId("group-participant-picker")).toBeNull());
    });

    it("ignores outside clicks while an async participant add is pending", async () => {
        let resolveAdd: (() => void) | undefined;
        const onAdd = vi.fn(() => new Promise<void>((resolve) => { resolveAdd = resolve; }));
        render(
            <div>
                <button data-testid="outside-button">Outside</button>
                <ParticipantSelector
                    sessionId="session-1"
                    currentParticipants={mockParticipants}
                    maxGroupParticipants={5}
                    theme={testTheme}
                    lang="en"
                    onAdd={onAdd}
                    listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
                />
            </div>
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy());
        fireEvent.click(screen.getByTestId("group-picker-item-ve-3"));

        expect(screen.getByText("Adding...")).toBeTruthy();
        fireEvent.mouseDown(screen.getByTestId("outside-button"));
        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        expect(screen.getByTestId("group-participant-picker")).toBeTruthy();
        expect((screen.getByTestId("group-add-participant-btn") as HTMLButtonElement).disabled).toBe(true);

        await act(async () => { resolveAdd?.(); });
        await waitFor(() => expect(screen.queryByTestId("group-participant-picker")).toBeNull());
    });

    it("keeps the picker open and shows an error when async participant add fails", async () => {
        const onAdd = vi.fn().mockResolvedValue(null);
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                lang="en"
                onAdd={onAdd}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByTestId("group-picker-item-ve-3")).toBeTruthy());
        fireEvent.click(screen.getByTestId("group-picker-item-ve-3"));

        await waitFor(() => expect(screen.getByTestId("group-limit-error").textContent).toContain("Failed to add"));
        expect(screen.getByTestId("group-participant-picker")).toBeTruthy();
    });

    it("shows empty state when no VEs available", async () => {
        render(
            <ParticipantSelector
                sessionId="session-1"
                currentParticipants={mockParticipants}
                maxGroupParticipants={5}
                theme={testTheme}
                onAdd={vi.fn()}
                listVirtualEmployees={() => Promise.resolve([])}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => {
            expect(screen.getByTestId("group-picker-empty")).toBeTruthy();
        });
    });
});

// ─── GroupMessageBubble ──────────────────────────────────────────────

describe("GroupMessageBubble", () => {
    it("renders participant name label", () => {
        const msg: GroupMessage = {
            id: "msg-1",
            fromId: "ve-1",
            fromName: "数字员工A",
            content: "Hello!",
            timestamp: 1000,
        };

        render(
            <GroupMessageBubble
                message={msg}
                participantIndex={0}
                theme={testTheme}
            />
        );

        expect(screen.getByTestId("group-msg-label-msg-1").textContent).toContain("A");
    });

    it("renders message content", () => {
        const msg: GroupMessage = {
            id: "msg-2",
            fromId: "ve-2",
            fromName: "数字员工B",
            content: "Test content",
            timestamp: 2000,
        };

        render(
            <GroupMessageBubble
                message={msg}
                participantIndex={1}
                theme={testTheme}
            />
        );

        expect(screen.getByTestId("group-msg-msg-2").textContent).toContain("Test content");
    });

    it("renders compact markdown headings in group digital employee messages", () => {
        const msg: GroupMessage = {
            id: "msg-weather",
            fromId: "ve-1",
            fromName: "Weather Bot",
            content: "天气：####\u{1f4c5}今天\n晴 0%####\u{1f4c5}明天\n多云",
            timestamp: 3000,
        };

        render(
            <GroupMessageBubble
                message={msg}
                participantIndex={0}
                theme={testTheme}
            />
        );

        expect(screen.getByText("天气：")).toBeTruthy();
        expect(screen.getByText("\u{1f4c5}今天")).toBeTruthy();
        expect(screen.getByText("晴 0%")).toBeTruthy();
        expect(screen.getByText("\u{1f4c5}明天")).toBeTruthy();
        expect(screen.getByText("多云")).toBeTruthy();
    });

    it("renders attachments when present", () => {
        const msg: GroupMessage = {
            id: "msg-3",
            fromId: "ve-1",
            fromName: "数字员工A",
            content: "See attached",
            timestamp: 3000,
            attachments: [
                { type: "file", filename: "report.pdf" },
                { type: "image", filename: "photo.png", fileUrl: "http://example.com/photo.png" },
            ],
        };

        render(
            <GroupMessageBubble
                message={msg}
                participantIndex={0}
                theme={testTheme}
            />
        );

        expect(screen.getByTestId("group-msg-att-msg-3-0")).toBeTruthy();
        expect(screen.getByTestId("group-msg-att-msg-3-1")).toBeTruthy();
    });

    it("omits the empty text bubble for attachment-only messages", () => {
        const msg: GroupMessage = {
            id: "msg-attachment-only",
            fromId: "ve-1",
            fromName: "Digital employee",
            content: "",
            timestamp: 3000,
            attachments: [{ type: "file", filename: "evidence.pdf" }],
        };

        render(
            <GroupMessageBubble
                message={msg}
                participantIndex={0}
                theme={testTheme}
            />
        );

        expect(screen.queryByTestId("group-msg-content-msg-attachment-only")).toBeNull();
        expect(screen.getByTestId("group-msg-att-msg-attachment-only-0")).toBeTruthy();
    });
});

// ─── ParticipantOfflineNotice ────────────────────────────────────────

describe("ParticipantOfflineNotice", () => {
    it("renders offline notice with participant name (zh)", () => {
        render(
            <ParticipantOfflineNotice
                participantName="数字员工A"
                theme={testTheme}
            />
        );

        expect(screen.getByTestId("group-offline-notice-数字员工A").textContent).toContain("数字员工");
    });

    it("renders offline notice in English", () => {
        render(
            <ParticipantOfflineNotice
                participantName="数字员工A"
                theme={testTheme}
                lang="en"
            />
        );

        expect(screen.getByTestId("group-offline-notice-数字员工A").textContent).toBe("数字员工A went offline");
    });
});

// ─── VEGroupChatView ─────────────────────────────────────────────────

describe("VEGroupChatView", () => {
    it("renders the group chat view with header and messages", () => {
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={mockMessages}
                theme={testTheme}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        expect(screen.getByTestId("ve-group-chat-view")).toBeTruthy();
        expect(screen.getByTestId("group-chat-header")).toBeTruthy();
        expect(screen.getByTestId("group-message-list")).toBeTruthy();
    });

    it("adds participants by machine_id when discoverable id differs", async () => {
        const onAddParticipant = vi.fn().mockResolvedValue(undefined);
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={[]}
                theme={testTheme}
                lang="en"
                onAddParticipant={onAddParticipant}
                listVirtualEmployees={() => Promise.resolve([
                    { id: "profile-3", machine_id: "machine-3", name: "Machine Bot", skill_description: "Review", access_policy: "public", status: "active", online_status: "online" },
                ])}
            />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Machine Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Machine Bot"));

        await waitFor(() => expect(onAddParticipant).toHaveBeenCalledWith("machine-3"));
    });

    it("displays participant count in header", () => {
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={mockMessages}
                theme={testTheme}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        expect(screen.getByTestId("group-chat-header").textContent).toContain("2");
    });

    it("renders all messages with participant labels", () => {
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={mockMessages}
                theme={testTheme}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        expect(screen.getByTestId("group-msg-msg-1")).toBeTruthy();
        expect(screen.getByTestId("group-msg-msg-2")).toBeTruthy();
        expect(screen.getByTestId("group-msg-msg-3")).toBeTruthy();

        // Check labels
        expect(screen.getByTestId("group-msg-label-msg-1").textContent).toBe("User");
        expect(screen.getByTestId("group-msg-label-msg-2").textContent).toBe("Assistant A");
        expect(screen.getByTestId("group-msg-label-msg-3").textContent).toBe("Assistant B");
    });

    it("uses participant names when group message labels are raw ids", () => {
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={[{ id: "m_b1821505498d817c", name: "Contract Bot", online: true }]}
                messages={[{ id: "raw-msg", fromId: "m_b1821505498d817c", fromName: "m_b1821505498d817c", content: "reviewed", timestamp: 1000 }]}
                theme={testTheme}
                lang="en"
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        expect(screen.getByTestId("group-msg-label-raw-msg").textContent).toBe("Contract Bot");
        expect(screen.getByTestId("group-message-list").textContent).not.toContain("m_b1821505498d817c");
    });

    it("keeps wrapping rules for group message bubbles", () => {
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={[{ id: "wrap-msg", fromId: "ve-1", fromName: "Assistant A", content: "LongWordWithoutSpaces\nsecond line", timestamp: 1000 }]}
                theme={testTheme}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        const bubble = screen.getByTestId("group-msg-content-wrap-msg") as HTMLElement;
        expect(bubble.style.overflowWrap).toBe("anywhere");
        expect(bubble.style.whiteSpace).toBe("pre-wrap");
        expect(screen.getByText("second line")).toBeTruthy();
    });

    it("calls onTitleChange when participants change", () => {
        const onTitleChange = vi.fn();
        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={[]}
                theme={testTheme}
                onTitleChange={onTitleChange}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        expect(onTitleChange).toHaveBeenCalledWith("数字员工A, 数字员工B");
    });

    it("shows offline notice when participant goes offline via event", async () => {
        // Capture the event handler
        let statusChangeHandler: ((data: any) => void) | null = null;
        mockEventsOn.mockImplementation((event: string, handler: any) => {
            if (event === "ve:status_change") {
                statusChangeHandler = handler;
            }
            return vi.fn();
        });

        render(
            <VEGroupChatView
                sessionId="session-1"
                participants={mockParticipants}
                messages={[]}
                theme={testTheme}
                listVirtualEmployees={() => Promise.resolve(mockAvailableVEs)}
            />
        );

        // Simulate participant going offline
        act(() => {
            if (statusChangeHandler) {
                statusChangeHandler({ ve_id: "ve-1", online_status: "offline" });
            }
        });

        await waitFor(() => {
            expect(screen.getByTestId("group-offline-notice-数字员工A")).toBeTruthy();
        });
    });
});

// ─── useGroupConfig ──────────────────────────────────────────────────

describe("useGroupConfig", () => {
    function TestComponent({ initialMax }: { initialMax?: number }) {
        const config = useGroupConfig(initialMax);
        return <div data-testid="config-value">{config.maxGroupParticipants}</div>;
    }

    it("uses default max when no initial value provided", () => {
        render(<TestComponent />);
        expect(screen.getByTestId("config-value").textContent).toBe(
            String(DEFAULT_MAX_GROUP_PARTICIPANTS)
        );
    });

    it("uses provided initial max", () => {
        render(<TestComponent initialMax={8} />);
        expect(screen.getByTestId("config-value").textContent).toBe("8");
    });

    it("updates max when ve:group_config event is received", async () => {
        let configHandler: ((data: any) => void) | null = null;
        mockEventsOn.mockImplementation((event: string, handler: any) => {
            if (event === "ve:group_config") {
                configHandler = handler;
            }
            return vi.fn();
        });

        render(<TestComponent initialMax={5} />);

        expect(screen.getByTestId("config-value").textContent).toBe("5");

        // Simulate config update event
        act(() => {
            if (configHandler) {
                configHandler({ max_group_participants: 8 });
            }
        });

        await waitFor(() => {
            expect(screen.getByTestId("config-value").textContent).toBe("8");
        });
    });

    it("ignores invalid config values (> MAX_UPPER_LIMIT)", async () => {
        let configHandler: ((data: any) => void) | null = null;
        mockEventsOn.mockImplementation((event: string, handler: any) => {
            if (event === "ve:group_config") {
                configHandler = handler;
            }
            return vi.fn();
        });

        render(<TestComponent initialMax={5} />);

        act(() => {
            if (configHandler) {
                configHandler({ max_group_participants: 15 }); // exceeds MAX_UPPER_LIMIT=10
            }
        });

        // Should remain unchanged
        expect(screen.getByTestId("config-value").textContent).toBe("5");
    });

    it("ignores invalid config values (< 1)", async () => {
        let configHandler: ((data: any) => void) | null = null;
        mockEventsOn.mockImplementation((event: string, handler: any) => {
            if (event === "ve:group_config") {
                configHandler = handler;
            }
            return vi.fn();
        });

        render(<TestComponent initialMax={5} />);

        act(() => {
            if (configHandler) {
                configHandler({ max_group_participants: 0 });
            }
        });

        // Should remain unchanged
        expect(screen.getByTestId("config-value").textContent).toBe("5");
    });
});






