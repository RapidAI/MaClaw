import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AITab, AITabState } from "./AITabTypes";
import { GroupParticipantPanel, type Participant } from "./GroupParticipantPanel";
import { HistoryGroupDiscussionTab } from "./HistoryGroupDiscussionTab";
import { VEConversationView, type VEConversationHandle, type VEMessage } from "./VEConversationView";

type AssistantActiveTabContentProps = {
    activeTab: AITab;
    isLocalTabActive: boolean;
    isProjectTabActive: boolean;
    lang: string;
    theme: any;
    /** Get saved state for a tab (from useAITabManager) */
    getTabState?: (tabId: string) => AITabState | undefined;
    /** Save state for a tab (from useAITabManager) */
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void;
};

/**
 * Renders the content area for non-local, non-project tabs (VE and group).
 *
 * Project tabs are rendered inline in AIAssistantPanel alongside the local tab
 * (sharing the same AssistantConversationBody + AssistantInputStack layout but
 * with independent state). This component only handles VE and group tab types.
 */
export function AssistantActiveTabContent({ activeTab, isLocalTabActive, isProjectTabActive, lang, theme, getTabState, saveTabState }: AssistantActiveTabContentProps) {
    // Local and project tabs are rendered by AIAssistantPanel directly
    if (isLocalTabActive || isProjectTabActive) return null;

    let content: React.ReactNode = null;

    if (activeTab.type === "ve" && activeTab.veId) {
        // VE 1:1 tab: render VEConversationView without participant panel.
        // Uses UnifiedVEGroupWrapper with no participants to share the same
        // component type as group tabs, preventing React unmount on tab upgrade.
        content = (
            <UnifiedVEGroupWrapper
                key={activeTab.id}
                tab={activeTab}
                theme={theme}
                lang={lang}
                getTabState={getTabState}
                saveTabState={saveTabState}
            />
        );
    } else if (activeTab.type === "group") {
        const isLiveGroup = !!activeTab.veId && Array.isArray(activeTab.participants) && activeTab.participants.length > 0;

        if (isLiveGroup) {
            // Group tab: render VEConversationView with participant panel.
            // Same component type as VE tab (UnifiedVEGroupWrapper) so React
            // preserves the VEConversationView instance when upgrading from VE to group.
            content = (
                <UnifiedVEGroupWrapper
                    key={activeTab.id}
                    tab={activeTab}
                    theme={theme}
                    lang={lang}
                    getTabState={getTabState}
                    saveTabState={saveTabState}
                />
            );
        } else {
            content = (
                <HistoryGroupDiscussionTab
                    key={activeTab.id}
                    discussionId={activeTab.discussionId || activeTab.id.replace(/^history-/, "")}
                    title={activeTab.title}
                    readOnly={!!activeTab.readOnly}
                    theme={theme}
                    lang={lang}
                />
            );
        }
    }

    if (!content) return null;

    // Single flex wrapper — fills remaining space in the parent flex column (ai-panel-body).
    // flex:1 claims available space; minHeight:0 prevents min-height:auto overflow;
    // height:0 provides an explicit percentage-height reference for children using height:100%
    // (CSS spec: percentage heights resolve against the parent's explicit height, not flex-computed height).
    return (
        <div style={{ flex: 1, minHeight: 0, height: 0, display: "flex", flexDirection: "column", overflow: "hidden" }}>
            {content}
        </div>
    );
}

// --- Shared Hook: useVEStatePersistence ---
// Manages VEConversationView state persistence on unmount (tab switch).

function useVEStatePersistence(
    tabId: string,
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void,
) {
    const veRef = useRef<VEConversationHandle>(null);
    const tabIdRef = useRef(tabId);
    tabIdRef.current = tabId;
    const saveTabStateRef = useRef(saveTabState);
    saveTabStateRef.current = saveTabState;

    useEffect(() => {
        return () => {
            const handle = veRef.current;
            if (!handle || !saveTabStateRef.current) return;
            const s = handle.getState();
            saveTabStateRef.current(tabIdRef.current, {
                history: s.messages as unknown[],
                sessionId: s.sessionId || undefined,
                inputText: s.inputText,
                scrollTop: 0,
            });
        };
    }, []); // Empty deps — cleanup only runs on unmount

    return veRef;
}

// --- Unified VE/Group Tab Wrapper ---
// CRITICAL DESIGN: Both VE 1:1 tabs and group tabs use the SAME component type.
// This is the mechanism that prevents React from unmounting VEConversationView
// when upgradeVETabToGroup changes tab.type from "ve" to "group".
//
// React reconciliation rule: same key + same component type means update, not unmount.
// If we used two different components (VETabWrapper vs LiveGroupTabWrapper),
// React would unmount the old and mount the new, destroying conversation state.
//
// By using one component for both modes, the tab type change only triggers a
// re-render with updated props as participants are added,
// and VEConversationView stays mounted with all its state intact.

interface UnifiedVEGroupWrapperProps {
    tab: AITab;
    theme: any;
    lang: string;
    getTabState?: (tabId: string) => AITabState | undefined;
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void;
}

function UnifiedVEGroupWrapper({ tab, theme, lang, getTabState, saveTabState }: UnifiedVEGroupWrapperProps) {
    const veRef = useVEStatePersistence(tab.id, saveTabState);

    const savedState = getTabState?.(tab.id);
    const savedMessages = savedState?.history as VEMessage[] | undefined;
    const savedSessionId = savedState?.sessionId || tab.discussionId;
    const savedInputText = savedState?.inputText;
    const handleSessionIdChange = useCallback((sessionId: string) => {
        const current = getTabState?.(tab.id);
        if (current?.sessionId === sessionId) return;
        saveTabState?.(tab.id, { ...current, sessionId });
    }, [getTabState, saveTabState, tab.id]);

    // Determine if we're in group mode (show participant panel)
    const isGroupMode = Array.isArray(tab.participants) && tab.participants.length > 1;

    // External @mention insert state (triggered by right-click "Talk to" in participant panel)
    const [externalMentionInsert, setExternalMentionInsert] = useState<{ name: string; timestamp: number } | null>(null);

    // Handle "Talk to" from participant panel right-click menu
    const handleTalkTo = useCallback((participant: Participant) => {
        const displayName = participant.isLocal
            ? (lang?.startsWith("zh") || !lang ? "本机AI" : "Local AI")
            : participant.name;
        setExternalMentionInsert({ name: displayName, timestamp: Date.now() });
    }, [lang]);

    // Memoize participant list for GroupParticipantPanel display
    const panelParticipants: Participant[] = useMemo(() =>
        (tab.participants || []).map((pid) => ({
            id: pid,
            name: pid === "local-maclaw"
                ? "" // GroupParticipantPanel uses isLocal flag for display name
                : (pid === tab.veId ? tab.title : pid),
            online: true,
            isLocal: pid === "local-maclaw",
        })),
        [tab.participants, tab.veId, tab.title]
    );

    // Participants for @mention popover: all participants are mentionable.
    // In group chat, the user (human operator) is the message sender, NOT a participant.
    // Both remote VE and local AI are participants that can be @mentioned.
    const mentionParticipants = useMemo(() =>
        (tab.participants || []).map((pid) => ({
            id: pid,
            name: pid === "local-maclaw"
                ? (lang?.startsWith("zh") || !lang ? "本机AI" : "Local AI")
                : (pid === tab.veId ? tab.title : pid),
            online: true,
        })),
        [tab.participants, tab.veId, tab.title, lang]
    );

    // CRITICAL: Always use the same DOM structure regardless of isGroupMode.
    // If we conditionally wrap VEConversationView in different parent elements,
    // React will unmount and remount it when isGroupMode changes (because the
    // root element type changes from VEConversationView to div).
    //
    // Solution: Always render the flex row layout. In 1:1 mode, the participant
    // panel is simply not rendered (display:none or conditional null).
    // VEConversationView's position in the tree never changes.
    return (
        <div
            data-testid="live-group-tab"
            style={{
                display: "flex",
                flexDirection: "row",
                height: "100%",
                width: "100%",
            }}
        >
            <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>
                <VEConversationView
                    ref={veRef}
                    veId={tab.veId!}
                    veName={tab.title}
                    theme={theme}
                    lang={lang}
                    initialOnlineStatus={tab.onlineStatus}
                    existingSessionId={savedSessionId}
                    initialMessages={savedMessages}
                    initialInputText={savedInputText}
                    participants={isGroupMode ? mentionParticipants : undefined}
                    externalMentionInsert={isGroupMode ? externalMentionInsert : undefined}
                    onSessionIdChange={handleSessionIdChange}
                />
            </div>
            {isGroupMode && (
                <GroupParticipantPanel
                    participants={panelParticipants}
                    theme={theme}
                    lang={lang}
                    sessionId={savedSessionId}
                    onTalkTo={handleTalkTo}
                />
            )}
        </div>
    );
}
