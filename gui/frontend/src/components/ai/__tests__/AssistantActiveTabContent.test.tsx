import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantActiveTabContent } from "../AssistantActiveTabContent";
import { LOCAL_TAB } from "../AITabTypes";
import type { AITab, AITabState } from "../AITabTypes";
import type { Theme } from "../aiAssistantPanelTheme";

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    ListVirtualEmployees: vi.fn(async () => [
        { id: "ve-2", name: "Contract Bot", online_status: "online", status: "online", access_policy: "public", skill_description: "Contracts" },
    ]),
}));

const theme = {
    bg: "#fff",
    text: "#222",
    textMuted: "#888",
    inputBarBg: "#fff",
    inputBarBorder: "#6366f1",
    inputText: "#222",
    fieldBg: "#f9fafb",
    divider: "#e5e7eb",
    responseBorderLeft: "#22c55e",
    errorBg: "#fef2f2",
    errorText: "#dc2626",
    errorBorder: "#fecaca",
    closeBtnColor: "#dc2626",
    sendBtnBg: "#6366f1",
    sendBtnBorder: "#6366f1",
    sendBtnColor: "#6366f1",
} as Theme;

describe("AssistantActiveTabContent", () => {
    it("keeps digital employee tab history and draft mounted while switching tabs", () => {
        const agentA: AITab = { id: "ve-a-tab", type: "ve", title: "Agent A", veId: "ve-a", closable: true };
        const agentB: AITab = { id: "ve-b-tab", type: "ve", title: "Agent B", veId: "ve-b", closable: true };
        const tabs: AITab[] = [LOCAL_TAB, agentA, agentB];
        const states = new Map<string, AITabState>([
            [agentA.id, { sessionId: "session-a", history: [{ id: "a-1", role: "user", content: "hello from A", timestamp: 1 }], inputText: "", scrollTop: 0 }],
            [agentB.id, { sessionId: "session-b", history: [{ id: "b-1", role: "user", content: "hello from B", timestamp: 2 }], inputText: "", scrollTop: 0 }],
        ]);
        const getTabState = (tabId: string) => states.get(tabId);
        const saveTabState = vi.fn();

        const { rerender } = render(
            <AssistantActiveTabContent activeTab={agentA} tabs={tabs} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={getTabState} saveTabState={saveTabState} />
        );

        expect(screen.getByText("hello from A")).toBeTruthy();
        fireEvent.change(screen.getByLabelText("Message Agent A"), { target: { value: "draft for A" } });

        rerender(
            <AssistantActiveTabContent activeTab={agentB} tabs={tabs} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={getTabState} saveTabState={saveTabState} />
        );
        expect(screen.getByText("hello from B")).toBeTruthy();
        fireEvent.change(screen.getByLabelText("Message Agent B"), { target: { value: "draft for B" } });

        rerender(
            <AssistantActiveTabContent activeTab={agentA} tabs={tabs} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={getTabState} saveTabState={saveTabState} />
        );

        expect(screen.getByText("hello from A")).toBeTruthy();
        expect((screen.getByLabelText("Message Agent A") as HTMLTextAreaElement).value).toBe("draft for A");
    });

    it("does not keep inactive read-only history tabs mounted", () => {
        const liveTab: AITab = { id: "ve-live", type: "ve", title: "Live Agent", veId: "ve-live", closable: true };
        const historyTab: AITab = { id: "history-disc-1", type: "group", title: "Archived review", discussionId: "disc-1", readOnly: true, participants: ["ve-a"], closable: true };
        const tabs: AITab[] = [LOCAL_TAB, liveTab, historyTab];
        const getTabState = (tabId: string) => new Map<string, AITabState>([
            [liveTab.id, { sessionId: "session-live", history: [], inputText: "", scrollTop: 0 }],
        ]).get(tabId);

        render(
            <AssistantActiveTabContent activeTab={liveTab} tabs={tabs} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={getTabState} saveTabState={vi.fn()} />
        );

        expect(screen.queryByTestId("ai-history-group-tab-disc-1")).toBeNull();
    });

    it("passes read-only state to live group participant panel", () => {
        const groupTab: AITab = { id: "group-readonly", type: "group", title: "Review group", veId: "ve-a", readOnly: true, participants: ["ve-a", "local-maclaw"], closable: true };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        expect(screen.getByTestId("group-participant-panel").textContent).toContain("Read-only");
        expect((screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement).disabled).toBe(true);
        expect((screen.getByTestId("ve-send-button") as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId("ve-attach-button") as HTMLButtonElement).disabled).toBe(true);
    });

    it("updates participant display names when tab metadata changes", () => {
        const baseTab: AITab = { id: "group-live", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "ve-b", "local-maclaw"], closable: true };
        const { rerender } = render(
            <AssistantActiveTabContent activeTab={baseTab} tabs={[LOCAL_TAB, baseTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        expect(screen.getByText("Participant 2")).toBeTruthy();
        expect(screen.queryByText("ve-b")).toBeNull();

        const namedTab: AITab = { ...baseTab, participantNames: { "ve-b": "Contract Bot" } };
        rerender(
            <AssistantActiveTabContent activeTab={namedTab} tabs={[LOCAL_TAB, namedTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        expect(screen.getByText("Contract Bot")).toBeTruthy();
        expect(screen.queryByText("ve-b")).toBeNull();
    });

    it("does not show a no-op participant picker when add callback is unavailable", () => {
        const groupTab: AITab = { id: "group-no-add", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a"], closable: true };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        expect(screen.getByTestId("group-participant-panel")).toBeTruthy();
        expect(screen.queryByTestId("group-add-participant-btn")).toBeNull();
    });

    it("shows the unified participant panel for a freshly converted one-participant group", async () => {
        const groupTab: AITab = { id: "group-new", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a"], closable: true };
        const onAddParticipantToTab = vi.fn();

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} onAddParticipantToTab={onAddParticipantToTab} />
        );

        expect(screen.getByTestId("group-participant-panel")).toBeTruthy();
        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));

        expect(onAddParticipantToTab).toHaveBeenCalledWith(groupTab, "ve-2", "Contract Bot");
    });

    it("keeps the participant picker usable before a live group session id is saved", async () => {
        const groupTab: AITab = { id: "group-unsaved", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };
        const onAddParticipantToTab = vi.fn();

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} onAddParticipantToTab={onAddParticipantToTab} />
        );

        expect(screen.queryByTestId("group-panel-invite-btn")).toBeNull();
        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));

        expect(onAddParticipantToTab).toHaveBeenCalledWith(groupTab, "ve-2", "Contract Bot");
    });

    it("inserts a mention from the participant context menu", () => {
        const groupTab: AITab = { id: "group-talk", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        fireEvent.contextMenu(screen.getByText("Local AI"));
        fireEvent.click(screen.getByTestId("context-menu-talk-to"));

        expect((screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement).value).toBe("@Local AI ");
    });

    it("inserts a mention at the current caret position", () => {
        const groupTab: AITab = { id: "group-talk-caret", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "hello world", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        const input = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
        input.setSelectionRange(5, 5);
        fireEvent.contextMenu(screen.getByText("Local AI"));
        fireEvent.click(screen.getByTestId("context-menu-talk-to"));

        expect(input.value).toBe("hello @Local AI world");
    });

    it("hides raw participant ids in live group panel and mention suggestions", () => {
        const groupTab: AITab = {
            id: "group-raw-live",
            type: "group",
            title: "m_b1821505498d817c",
            veId: "m_b1821505498d817c",
            participants: ["m_b1821505498d817c", "local-maclaw"],
            closable: true,
        };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        const panel = screen.getByTestId("group-participant-panel");
        expect(panel.textContent).toContain("Participant 1");
        expect(panel.textContent).toContain("Local AI");
        expect(panel.textContent).not.toContain("m_b1821505498d817c");

        const input = screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "@Par", selectionStart: 4 } });
        expect(screen.getByTestId("mention-popover").textContent).toContain("Participant 1");
        expect(screen.getByTestId("mention-popover").textContent).not.toContain("m_b1821505498d817c");
    });

    it("inserts a live mention without the displayed role suffix", () => {
        const groupTab: AITab = {
            id: "group-talk-role",
            type: "group",
            title: "Agent A",
            veId: "ve-a",
            participants: ["ve-a", "ve-b"],
            participantNames: { "ve-b": "Contract Bot (review)" },
            closable: true,
        };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        fireEvent.contextMenu(screen.getByText("Contract Bot (review)"));
        fireEvent.click(screen.getByTestId("context-menu-talk-to"));

        expect((screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement).value).toBe("@Contract Bot ");
    });

    it("inserts a fresh mention each time the participant talk-to action is used", () => {
        const groupTab: AITab = { id: "group-talk-repeat", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} />
        );

        fireEvent.contextMenu(screen.getByText("Local AI"));
        fireEvent.click(screen.getByTestId("context-menu-talk-to"));
        fireEvent.contextMenu(screen.getByText("Local AI"));
        fireEvent.click(screen.getByTestId("context-menu-talk-to"));

        expect((screen.getByTestId("ve-input-textarea") as HTMLTextAreaElement).value).toBe("@Local AI @Local AI ");
    });

    it("lets the unified participant panel add a live group participant", async () => {
        const groupTab: AITab = { id: "group-live", type: "group", title: "Agent A", veId: "ve-a", participants: ["ve-a", "local-maclaw"], closable: true };
        const onAddParticipantToTab = vi.fn();

        render(
            <AssistantActiveTabContent activeTab={groupTab} tabs={[LOCAL_TAB, groupTab]} isLocalTabActive={false} isProjectTabActive={false} lang="en" theme={theme} getTabState={() => ({ sessionId: "session-1", history: [], inputText: "", scrollTop: 0 })} saveTabState={vi.fn()} onAddParticipantToTab={onAddParticipantToTab} />
        );

        fireEvent.click(screen.getByTestId("group-add-participant-btn"));
        await waitFor(() => expect(screen.getByText("Contract Bot")).toBeTruthy());
        fireEvent.click(screen.getByText("Contract Bot"));

        expect(onAddParticipantToTab).toHaveBeenCalledWith(groupTab, "ve-2", "Contract Bot");
    });
});
