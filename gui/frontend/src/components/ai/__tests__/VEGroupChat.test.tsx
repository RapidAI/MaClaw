// @vitest-environment jsdom
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
    borderLeft: "#3b82f6",
    responseBorderLeft: "#10b981",
    errorBg: "#fef2f2",
    errorText: "#dc2626",
    errorBorder: "#fecaca",
    closeBtnColor: "#dc2626",
};

// --- Test Data ---
const mockParticipants: GroupParticipant[] = [
    { id: "ve-1", name: "AI助手A", online: true },
    { id: "ve-2", name: "AI助手B", online: true },
];

const mockMessages: GroupMessage[] = [
    { id: "msg-1", fromId: "user", fromName: "User", content: "Hello", timestamp: 1000 },
    { id: "msg-2", fromId: "ve-1", fromName: "Assistant A", content: "Hi", timestamp: 2000 },
    { id: "msg-3", fromId: "ve-2", fromName: "Assistant B", content: "How can I help?", timestamp: 3000 },
];

const mockAvailableVEs: VirtualEmployeeEntry[] = [
    { id: "ve-3", name: "AI助手C", skill_description: "翻译", access_policy: "public", status: "active", online_status: "online" },
    { id: "ve-4", name: "AI助手D", skill_description: "编程", access_policy: "public", status: "active", online_status: "online" },
    { id: "ve-1", name: "AI助手A", skill_description: "写作", access_policy: "public", status: "active", online_status: "online" },
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
        expect(screen.getByTestId("group-limit-error").textContent).toContain("群聊人数已满");
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
            expect.objectContaining({ id: "ve-3", name: "AI助手C" })
        );
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
            fromName: "AI助手A",
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
            fromName: "AI助手B",
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

    it("renders attachments when present", () => {
        const msg: GroupMessage = {
            id: "msg-3",
            fromId: "ve-1",
            fromName: "AI助手A",
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
});

// ─── ParticipantOfflineNotice ────────────────────────────────────────

describe("ParticipantOfflineNotice", () => {
    it("renders offline notice with participant name (zh)", () => {
        render(
            <ParticipantOfflineNotice
                participantName="AI助手A"
                theme={testTheme}
            />
        );

        expect(screen.getByTestId("group-offline-notice-AI助手A").textContent).toContain("AI助手A");
    });

    it("renders offline notice in English", () => {
        render(
            <ParticipantOfflineNotice
                participantName="Assistant"
                theme={testTheme}
                lang="en"
            />
        );

        expect(screen.getByTestId("group-offline-notice-Assistant").textContent).toBe("Assistant went offline");
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

        expect(onTitleChange).toHaveBeenCalledWith("AI助手A, AI助手B");
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
            expect(screen.getByTestId("group-offline-notice-AI助手A")).toBeTruthy();
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






