// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GroupParticipantPanel } from "../GroupParticipantPanel";
import { lightTheme } from "../aiAssistantPanelTheme";
import { EventsOn } from "../../../../wailsjs/runtime";

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

        const dot = screen.getByTestId("participant-status-ve-1");
        expect(dot.style.background).toBe("rgb(79, 127, 111)");

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ ve_id: "ve-other", online_status: "offline" });
            }
        });
        expect(dot.style.background).toBe("rgb(79, 127, 111)");

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ ve_id: "ve-1", online_status: "offline" });
            }
        });
        expect(screen.getByTestId("participant-status-ve-1").style.background).toBe("rgb(107, 114, 128)");
    });

    it("accepts nested employee status events keyed by machine id", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ payload: { employee: { id: "profile-1", machine_id: "machine-1", online_status: "offline" } } });
            }
        });

        expect(screen.getByTestId("participant-status-machine-1").style.background).toBe("rgb(107, 114, 128)");
    });

    it("accepts status events across hub-generated ve aliases", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ payload: { employee: { machine_id: "machine-1", online_status: "offline" } } });
            }
        });

        expect(screen.getByTestId("participant-status-ve-machine-1").style.background).toBe("rgb(107, 114, 128)");
    });

    it("accepts status events across slash and space normalized aliases", () => {
        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-machine 1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ payload: { employee: { machine_id: "machine/1", online_status: "offline" } } });
            }
        });

        expect(screen.getByTestId("participant-status-ve-machine 1").style.background).toBe("rgb(107, 114, 128)");
    });

    it("shows type icons for remote and local participants", () => {
        render(
            <GroupParticipantPanel
                participants={[
                    { id: "ve-1", name: "Agent 1", online: true },
                    { id: "local-maclaw", name: "Local AI", online: true, isLocal: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByLabelText("Digital employee")).toBeTruthy();
        expect(screen.getByLabelText("Local AI")).toBeTruthy();
        expect(screen.getByTestId("participant-status-ve-1")).toBeTruthy();
        expect(screen.getByTestId("participant-status-local-maclaw")).toBeTruthy();
    });

    it("renders digital employee avatar pictures in participant rows", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar },
        ]);

        render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        const image = await screen.findByTestId("participant-avatar-machine-1");
        expect(image.getAttribute("src")).toBe(avatar);
        expect(screen.queryByLabelText("Local AI")).toBeNull();
    });

    it("resolves avatars for hub-generated ve aliases", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar },
        ]);

        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        const image = await screen.findByTestId("participant-avatar-ve-machine-1");
        expect(image.getAttribute("src")).toBe(avatar);
    });

    it("prefers participant avatar data already supplied by the active tab", () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        render(
            <GroupParticipantPanel
                participants={[{ id: "ve-1", name: "Agent 1", online: true, avatarDataURL: avatar }]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByTestId("participant-avatar-ve-1").getAttribute("src")).toBe(avatar);
        expect(listVirtualEmployeesMock).not.toHaveBeenCalled();
    });

    it("keeps a supplied participant avatar if later props omit it and refresh fails", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock.mockRejectedValueOnce(new Error("temporary unavailable"));

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[{ id: "ve-1", name: "Agent 1", online: true, avatarDataURL: avatar }]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByTestId("participant-avatar-ve-1").getAttribute("src")).toBe(avatar);

        rerender(
            <GroupParticipantPanel
                participants={[{ id: "ve-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("participant-avatar-ve-1").getAttribute("src")).toBe(avatar);
    });

    it("caches supplied avatars even when another participant needs backend refresh", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock
            .mockResolvedValueOnce([])
            .mockRejectedValueOnce(new Error("temporary unavailable"));

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[
                    { id: "ve-1", name: "Agent 1", online: true, avatarDataURL: avatar },
                    { id: "ve-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("participant-avatar-ve-1").getAttribute("src")).toBe(avatar);

        rerender(
            <GroupParticipantPanel
                participants={[
                    { id: "ve-1", name: "Agent 1", online: true },
                    { id: "ve-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(2));
        expect(screen.getByTestId("participant-avatar-ve-1").getAttribute("src")).toBe(avatar);
    });

    it("skips avatar refresh work while mounted in a hidden tab", async () => {
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: "data:image/png;base64,iVBORw0KGgo=" },
        ]);

        render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
                active={false}
            />
        );

        await Promise.resolve();
        expect(listVirtualEmployeesMock).not.toHaveBeenCalled();
        expect(screen.queryByTestId("participant-avatar-machine-1")).toBeNull();
    });

    it("does not refetch avatars when only participant display metadata changes", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock.mockResolvedValue([
            { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar },
        ]);

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        await screen.findByTestId("participant-avatar-machine-1");
        expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(1);

        rerender(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent One", online: false }]}
                theme={theme}
                lang="en"
            />
        );

        await Promise.resolve();
        expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("participant-avatar-machine-1").getAttribute("src")).toBe(avatar);
    });

    it("keeps already resolved avatars when a later refresh fails", async () => {
        const avatar = "data:image/png;base64,iVBORw0KGgo=";
        listVirtualEmployeesMock
            .mockResolvedValueOnce([
                { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar },
            ])
            .mockRejectedValueOnce(new Error("temporary unavailable"));

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        await screen.findByTestId("participant-avatar-machine-1");

        rerender(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-1", name: "Agent 1", online: true },
                    { id: "machine-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(2));
        expect(screen.getByTestId("participant-avatar-machine-1").getAttribute("src")).toBe(avatar);
    });

    it("keeps already resolved avatars when a later refresh returns partial data", async () => {
        const avatar1 = "data:image/png;base64,iVBORw0KGgo=";
        const avatar2 = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ";
        listVirtualEmployeesMock
            .mockResolvedValueOnce([
                { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar1 },
            ])
            .mockResolvedValueOnce([
                { id: "ve-profile-2", machine_id: "machine-2", name: "Agent 2", online_status: "online", avatar_data_url: avatar2 },
            ]);

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );

        await screen.findByTestId("participant-avatar-machine-1");

        rerender(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-1", name: "Agent 1", online: true },
                    { id: "machine-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(2));
        expect(screen.getByTestId("participant-avatar-machine-1").getAttribute("src")).toBe(avatar1);
        expect(screen.getByTestId("participant-avatar-machine-2").getAttribute("src")).toBe(avatar2);
    });

    it("drops cached avatars for participants that leave the panel", async () => {
        const avatar1 = "data:image/png;base64,iVBORw0KGgo=";
        const avatar2 = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ";
        listVirtualEmployeesMock
            .mockResolvedValueOnce([
                { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar1 },
                { id: "ve-profile-2", machine_id: "machine-2", name: "Agent 2", online_status: "online", avatar_data_url: avatar2 },
            ])
            .mockResolvedValueOnce([
                { id: "ve-profile-1", machine_id: "machine-1", name: "Agent 1", online_status: "online", avatar_data_url: avatar1 },
            ])
            .mockRejectedValueOnce(new Error("temporary unavailable"));

        const { rerender } = render(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-1", name: "Agent 1", online: true },
                    { id: "machine-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await screen.findByTestId("participant-avatar-machine-2");

        rerender(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );
        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(2));

        rerender(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-1", name: "Agent 1", online: true },
                    { id: "machine-2", name: "Agent 2", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        await waitFor(() => expect(listVirtualEmployeesMock).toHaveBeenCalledTimes(3));
        expect(screen.getByTestId("participant-avatar-machine-1").getAttribute("src")).toBe(avatar1);
        expect(screen.queryByTestId("participant-avatar-machine-2")).toBeNull();
    });

    it("does not resubscribe status events when only participant display metadata changes", () => {
        const { rerender } = render(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent 1", online: true }]}
                theme={theme}
                lang="en"
            />
        );
        const statusSubscriptions = () => (EventsOn as any).mock.calls.filter((call: unknown[]) => call[0] === "ve:status_change").length;

        expect(statusSubscriptions()).toBe(1);
        rerender(
            <GroupParticipantPanel
                participants={[{ id: "machine-1", name: "Agent One", online: false }]}
                theme={theme}
                lang="en"
            />
        );

        expect(statusSubscriptions()).toBe(1);
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

    it("deduplicates generated VE aliases in participant count and rows", () => {
        render(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-a", name: "Agent A", online: true },
                    { id: "ve-machine-a", name: "Agent A duplicate", online: true },
                    { id: "machine-b", name: "Agent B", online: true },
                ]}
                theme={theme}
                lang="en"
            />
        );

        expect(screen.getByText("Participants (2)")).toBeTruthy();
        expect(screen.getByTitle("Agent A")).toBeTruthy();
        expect(screen.queryByTitle("Agent A duplicate")).toBeNull();
        expect(screen.getByTitle("Agent B")).toBeTruthy();
    });

    it("does not count duplicate aliases against invite capacity", () => {
        const onInvite = vi.fn();
        render(
            <GroupParticipantPanel
                participants={[
                    { id: "machine-a", name: "Agent A", online: true },
                    { id: "ve-machine-a", name: "Agent A duplicate", online: true },
                ]}
                theme={theme}
                lang="en"
                maxParticipants={2}
                onInvite={onInvite}
            />
        );

        const invite = screen.getByTestId("group-panel-invite-btn") as HTMLButtonElement;
        expect(invite.disabled).toBe(false);
        fireEvent.click(invite);
        expect(onInvite).toHaveBeenCalledTimes(1);
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
