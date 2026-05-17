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
        content = (
            <VETabWrapper
                key={activeTab.id}
                tabId={activeTab.id}
                veId={activeTab.veId}
                veName={activeTab.title}
                theme={theme}
                lang={lang}
                initialOnlineStatus={activeTab.onlineStatus}
                getTabState={getTabState}
                saveTabState={saveTabState}
            />
        );
    } else if (activeTab.type === "group") {
        const isLiveGroup = !!activeTab.veId && Array.isArray(activeTab.participants) && activeTab.participants.length > 0;

        if (isLiveGroup) {
            content = (
                <LiveGroupTabWrapper
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

// --- VE Tab Wrapper ---
// Manages the lifecycle of VEConversationView state persistence.
// On unmount (tab switch), snapshots conversation state via ref and saves to tabStatesRef.

interface VETabWrapperProps {
    tabId: string;
    veId: string;
    veName: string;
    theme: any;
    lang: string;
    initialOnlineStatus?: "online" | "offline";
    getTabState?: (tabId: string) => AITabState | undefined;
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void;
}

function VETabWrapper({ tabId, veId, veName, theme, lang, initialOnlineStatus, getTabState, saveTabState }: VETabWrapperProps) {
    const veRef = useVEStatePersistence(tabId, saveTabState);

    const savedState = getTabState?.(tabId);
    const savedMessages = savedState?.history as VEMessage[] | undefined;
    const savedSessionId = savedState?.sessionId;
    const savedInputText = savedState?.inputText;

    return (
        <VEConversationView
            ref={veRef}
            veId={veId}
            veName={veName}
            theme={theme}
            lang={lang}
            initialOnlineStatus={initialOnlineStatus}
            existingSessionId={savedSessionId}
            initialMessages={savedMessages}
            initialInputText={savedInputText}
        />
    );
}

// --- Live Group Tab Wrapper ---
// Renders a real-time group chat view (upgraded from VE 1:1 conversation).
// Shows the VE conversation on the left and a participant panel on the right.

interface LiveGroupTabWrapperProps {
    tab: AITab;
    theme: any;
    lang: string;
    getTabState?: (tabId: string) => AITabState | undefined;
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void;
}

function LiveGroupTabWrapper({ tab, theme, lang, getTabState, saveTabState }: LiveGroupTabWrapperProps) {
    const veRef = useVEStatePersistence(tab.id, saveTabState);

    const savedState = getTabState?.(tab.id);
    const savedMessages = savedState?.history as VEMessage[] | undefined;
    const savedSessionId = savedState?.sessionId || tab.discussionId;
    const savedInputText = savedState?.inputText;

    // External @mention insert state (triggered by right-click "Talk to" in participant panel)
    const [externalMentionInsert, setExternalMentionInsert] = useState<{ name: string; timestamp: number } | null>(null);

    // Handle "Talk to" from participant panel right-click menu
    const handleTalkTo = useCallback((participant: Participant) => {
        const displayName = participant.isLocal
            ? (lang?.startsWith("zh") || !lang ? "本机AI" : "Local AI")
            : participant.name;
        setExternalMentionInsert({ name: displayName, timestamp: Date.now() });
    }, [lang]);

    // Memoize participant list to avoid unnecessary re-renders of GroupParticipantPanel.
    // Only recompute when the participants array reference or tab title changes.
    const participants: Participant[] = useMemo(() =>
        (tab.participants || []).map((pid) => ({
            id: pid,
            name: pid === "local-maclaw"
                ? "" // GroupParticipantPanel uses isLocal flag for display name
                : (pid === tab.veId ? tab.title : pid),
            online: true, // Real-time status updated via ve:status_change events inside the panel
            isLocal: pid === "local-maclaw",
        })),
        [tab.participants, tab.veId, tab.title]
    );

    // Participants for @mention popover — needs display names for all participants
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
            {/* Left: Conversation area */}
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
                    participants={mentionParticipants}
                    externalMentionInsert={externalMentionInsert}
                />
            </div>
            {/* Right: Participant panel */}
            <GroupParticipantPanel
                participants={participants}
                theme={theme}
                lang={lang}
                sessionId={savedSessionId}
                onTalkTo={handleTalkTo}
            />
        </div>
    );
}
