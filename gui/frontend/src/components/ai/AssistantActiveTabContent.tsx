import type { AITab } from "./AITabTypes";
import { HistoryGroupDiscussionTab } from "./HistoryGroupDiscussionTab";
import { VEConversationView } from "./VEConversationView";

type AssistantActiveTabContentProps = {
    activeTab: AITab;
    isLocalTabActive: boolean;
    isProjectTabActive: boolean;
    lang: string;
    theme: any;
};

/**
 * Renders the content area for non-local, non-project tabs (VE and group).
 *
 * Project tabs are rendered inline in AIAssistantPanel alongside the local tab
 * (sharing the same AssistantConversationBody + AssistantInputStack layout but
 * with independent state). This component only handles VE and group tab types.
 */
export function AssistantActiveTabContent({ activeTab, isLocalTabActive, isProjectTabActive, lang, theme }: AssistantActiveTabContentProps) {
    // Local and project tabs are rendered by AIAssistantPanel directly
    if (isLocalTabActive || isProjectTabActive) return null;

    if (activeTab.type === "ve" && activeTab.veId) {
        return (
            <VEConversationView
                key={activeTab.id}
                veId={activeTab.veId}
                veName={activeTab.title}
                theme={theme}
                lang={lang}
            />
        );
    }

    if (activeTab.type === "group") {
        return (
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

    return null;
}