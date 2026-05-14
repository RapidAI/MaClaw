import type { AITab } from "./AITabTypes";
import { HistoryGroupDiscussionTab } from "./HistoryGroupDiscussionTab";
import { VEConversationView } from "./VEConversationView";

type AssistantActiveTabContentProps = {
    activeTab: AITab;
    isLocalTabActive: boolean;
    lang: string;
    theme: any;
};

export function AssistantActiveTabContent({ activeTab, isLocalTabActive, lang, theme }: AssistantActiveTabContentProps) {
    if (isLocalTabActive) return null;

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