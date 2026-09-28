import { describe, expect, it, vi } from "vitest";
import { closeAssistantProjectTab, type AssistantProjectTabCloseDeps } from "../assistantProjectTabClose";

function closeDeps(overrides: Partial<AssistantProjectTabCloseDeps> = {}): AssistantProjectTabCloseDeps {
    return {
        getTabs: () => [{ id: "tab-1", type: "local", title: "任务", closable: false }],
        getTabState: () => undefined,
        saveTabState: vi.fn(),
        closeTab: vi.fn(),
        messages: [],
        activeTabIdRef: { current: "tab-1" },
        latestDisplayMessagesRef: { current: [] },
        latestProjectCloseSnapshotRef: { current: null },
        clearedProjectTabIdsRef: { current: new Set() },
        projectConversationHydrationGenerationByTabIdRef: { current: new Map() },
        projectConversationHydrationByTabIdRef: { current: new Map() },
        projectTabRoundsRef: { current: new Map() },
        detachedProjectRoundsRef: { current: new Map() },
        projectTabMsgIdsRef: { current: new Set() },
        projectPrepareTimersRef: { current: new Map() },
        deferredProjectInitialSendsRef: { current: new Map() },
        pendingRemoteInitialSendRef: { current: new Map() },
        previewStateMapRef: { current: new Map() },
        previewOwnerTabRef: { current: "local" },
        previewOwnerResetPendingRef: { current: false },
        skillRecordingTabId: null,
        setSkillRecordingTabId: vi.fn(),
        abandonSkillRecording: vi.fn(),
        setProjectTabPreparing: vi.fn(),
        setProjectTabRouteVersion: vi.fn(),
        setDetachedProjectRoundVersion: vi.fn(),
        persistProjectTabMsgIds: vi.fn(),
        ...overrides,
    };
}

describe("closeAssistantProjectTab", () => {
    it("abandons an in-progress skill recording instead of stopping it into a hidden save", () => {
        const abandonSkillRecording = vi.fn();
        const closeTab = vi.fn();
        closeAssistantProjectTab("tab-1", closeDeps({
            skillRecordingTabId: "tab-1",
            abandonSkillRecording,
            closeTab,
        }));

        expect(abandonSkillRecording).toHaveBeenCalledTimes(1);
        expect(closeTab).toHaveBeenCalledWith("tab-1");
    });

    it("leaves another tab's recording alone", () => {
        const abandonSkillRecording = vi.fn();
        closeAssistantProjectTab("tab-1", closeDeps({
            skillRecordingTabId: "tab-2",
            abandonSkillRecording,
        }));

        expect(abandonSkillRecording).not.toHaveBeenCalled();
    });
});
