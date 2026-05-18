import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantActiveTabContent } from "../AssistantActiveTabContent";
import { LOCAL_TAB } from "../AITabTypes";
import type { AITab, AITabState } from "../AITabTypes";
import type { Theme } from "../aiAssistantPanelTheme";

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
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
});
