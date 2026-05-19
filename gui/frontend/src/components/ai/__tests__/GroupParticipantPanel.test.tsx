// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GroupParticipantPanel } from "../GroupParticipantPanel";
import { lightTheme } from "../aiAssistantPanelTheme";

const { eventHandlers, listVirtualEmployeesMock } = vi.hoisted(() => ({
    eventHandlers: new Map<string, Array<(payload?: any) => void>>(),
    listVirtualEmployeesMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: any) => void) => {
        const handlers = eventHandlers.get(event) || [];
        handlers.push(handler);
        eventHandlers.set(event, handlers);
        return () => eventHandlers.set(event, (eventHandlers.get(event) || []).filter((item) => item !== handler));
    }),
    EventsOff: vi.fn((event: string) => eventHandlers.delete(event)),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    ListVirtualEmployees: listVirtualEmployeesMock,
}));

const theme = {
    ...lightTheme,
    titleBarBg: "#ffffff",
    bg: "#ffffff",
    text: "#111827",
    textMuted: "#6b7280",
    divider: "#e5e7eb",
    fieldBg: "#f3f4f6",
    btnColor: "#2563eb",
    errorBg: "#fef2f2",
    errorText: "#dc2626",
    errorBorder: "#fecaca",
};

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    eventHandlers.clear();

});

describe("GroupParticipantPanel", () => {
    it("ignores status events for users outside the current participant list", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        const participantRow = screen.getByText("Agent 1").parentElement as HTMLElement;
        const dot = participantRow.firstElementChild as HTMLElement;
        expect(dot.style.background).toBe("rgb(34, 197, 94)");

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ ve_id: "ve-other", online_status: "offline" });
            }
        });
        expect(dot.style.background).toBe("rgb(34, 197, 94)");

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ ve_id: "ve-1", online_status: "offline" });
            }
        });
        expect((screen.getByText("Agent 1").parentElement?.firstElementChild as HTMLElement).style.background).toBe("rgb(107, 114, 128)");
    });
    it("uses participant names instead of ids in row titles", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "m_b1821505498d817c", name: "安娜", online: true }]}
                theme={theme}
                lang="zh"
            />
        );

        expect(screen.getByTitle("安娜")).toBeTruthy();
        expect(screen.queryByTitle("m_b1821505498d817c")).toBeNull();
    });

    it("falls back when participant name looks like a profile id", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "profile-raw", name: "profile-raw", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByText("Participant 1")).toBeTruthy();
        expect(screen.queryByText("profile-raw")).toBeNull();
        expect(screen.queryByTitle("profile-raw")).toBeNull();
    });

    it("falls back when participant name equals a raw id", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "m_b1821505498d817c", name: "m_b1821505498d817c", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByText("Participant 1")).toBeTruthy();
        expect(screen.queryByText("m_b1821505498d817c")).toBeNull();
        expect(screen.queryByTitle("m_b1821505498d817c")).toBeNull();
    });
    it("uses localized fallback names for Chinese participants", () => {
        render(
            <GroupParticipantPanel
                participants={[
                    { id: "m_b1821505498d817c", name: "m_b1821505498d817c", online: true },
                    { id: "local-maclaw", name: "local-maclaw", online: true, isLocal: true },
                ]}
                theme={theme}
                lang="zh-CN"
            />
        );

        expect(screen.getByText("\u53c2\u4e0e\u8005 1")).toBeTruthy();
        expect(screen.getByText("本机AI")).toBeTruthy();
        expect(screen.queryByText("m_b1821505498d817c")).toBeNull();
        expect(screen.queryByTitle("local-maclaw")).toBeNull();
    });
    it("renders Chinese fallback invite labels without mojibake", () => {
        render(
            <GroupParticipantPanel
                participants={[]}
                theme={theme}
                lang="zh-CN"
                onInvite={vi.fn()}
            />
        );

        expect(screen.getByText("\u6682\u65e0\u53c2\u4e0e\u8005")).toBeTruthy();
        const invite = screen.getByTestId("group-panel-invite-btn");
        expect(invite.textContent).toContain("\u9080\u8bf7");
        expect(invite.getAttribute("title")).toBe("\u9080\u8bf7\u6570\u5b57\u5458\u5de5");
    });

    it("renders the Chinese talk-to context menu label", () => {
        const onTalkTo = vi.fn();
        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-1", name: "\u5b89\u5a1c", online: true }]}
                theme={theme}
                lang="zh-CN"
                onTalkTo={onTalkTo}
            />
        );

        fireEvent.contextMenu(screen.getByText("\u5b89\u5a1c"));
        const item = screen.getByTestId("context-menu-talk-to");
        expect(item.textContent).toContain("\u4e0e\u5b83\u4ea4\u8c08");

        fireEvent.click(item);
        expect(onTalkTo).toHaveBeenCalledWith({ id: "ve-1", name: "\u5b89\u5a1c", online: true });
    });

    it("uses live group config for the add-participant limit", async () => {
        const participants = Array.from({ length: 5 }, (_, index) => ({
            id: "ve-" + (index + 1),
            name: "Agent " + (index + 1),
            online: true,
        }));
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "ve-6", machine_id: "ve-6", name: "Extra Bot", online_status: "online" },
        ]);

        render(
            <GroupParticipantPanel
                participants={participants}
                theme={theme}
                lang="en"
                onAddParticipant={vi.fn()}
            />
        );

        act(() => {
            for (const handler of eventHandlers.get("ve:group_config") || []) {
                handler({ max_group_participants: 6 });
            }
        });

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));

        await waitFor(() => expect(screen.getByTestId("group-participant-picker")).toBeTruthy());
        expect(screen.getByText("Extra Bot")).toBeTruthy();
        expect(screen.queryByTestId("group-limit-error")).toBeNull();
    });
});
