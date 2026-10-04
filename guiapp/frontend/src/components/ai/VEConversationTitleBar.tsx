import { useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import type { AITab } from "./AITabTypes";
import { getAITabDisplayTitle } from "./AITabItem";
import { localizeText } from "./aiAssistantI18n";
import { handleTaskExecutionHeaderDoubleClick } from "./assistantTaskExecutionChrome";
import { participantIdentityMatches } from "./participantIdentity";
import { TaskHideWindowButton } from "./TaskHideWindowButton";
import { TaskMaximizeWindowButton } from "./TaskMaximizeWindowButton";
import { TaskTabSwitcher } from "./TaskTabSwitcher";
import { veStatusEventInfo } from "./veStatusEvent";
import { windowDragHandleProps, windowNoDragRegionProps } from "../../utils/windowDrag";

export type VEConversationChrome = {
    inline?: boolean;
    maximized?: boolean;
    onHideWindow?: () => void;
    onToggleMaximize?: () => void;
    onActivateTab?: (tabId: string) => void;
    onCloseTab?: (tabId: string) => void;
};

type Props = {
    tab: AITab;
    tabs?: AITab[];
    lang: string;
    participantCount?: number;
    chrome?: VEConversationChrome;
};

function useEmployeeOnline(veId: string | undefined, initial: AITab["onlineStatus"], enabled: boolean): boolean {
    const [online, setOnline] = useState(initial !== "offline");
    useEffect(() => {
        setOnline(initial !== "offline");
    }, [initial, veId]);
    useEffect(() => {
        if (!enabled || !veId) return;
        const unsub = EventsOn("ve:status_change", (data: unknown) => {
            if (!data) return;
            const { ids, status } = veStatusEventInfo(data);
            if (!ids.some((id) => participantIdentityMatches(id, veId))) return;
            if (status === "online") setOnline(true);
            else if (status === "offline") setOnline(false);
        });
        return () => {
            if (typeof unsub === "function") unsub();
        };
    }, [enabled, veId]);
    return online;
}

function statusFor(tab: AITab, lang: string, participantCount: number, online: boolean): { label: string; tone: string } {
    if (tab.type === "group") {
        const count = Math.max(participantCount, tab.participants?.length || 0);
        const label = count > 0
            ? localizeText(lang, `${count} participants`, `${count} 位参与者`, `${count} 位參與者`)
            : localizeText(lang, "Group", "群聊", "群聊");
        return { label, tone: "" };
    }
    if (online) return { label: localizeText(lang, "Online", "在线", "在線"), tone: "completed" };
    return { label: localizeText(lang, "Offline", "离线", "離線"), tone: "pending" };
}

/** Title row for a digital-employee or group conversation. Task chats already have this region. */
export function VEConversationTitleBar({ tab, tabs, lang, participantCount = 0, chrome }: Props) {
    const title = getAITabDisplayTitle(tab, lang);
    const online = useEmployeeOnline(tab.veId, tab.onlineStatus, tab.type !== "group");
    const status = statusFor(tab, lang, participantCount, online);
    const skill = String(tab.veSkillDescription || "").trim();
    const showWindowControls = !!(chrome?.inline && (chrome.onHideWindow || chrome.onToggleMaximize));
    const showSwitcher = !!(chrome?.onActivateTab && chrome.onCloseTab);
    return (
        <div
            className="mc-task-execution-header"
            data-testid="ve-conversation-title"
            {...windowDragHandleProps(!!chrome?.inline, { minHeight: 72 })}
            onDoubleClick={(event) => handleTaskExecutionHeaderDoubleClick(event, chrome?.inline ? chrome.onToggleMaximize : undefined)}
        >
            <div className="mc-task-execution-heading">
                <strong className="mc-task-execution-title" role="heading" aria-level={2} title={title}>{title}</strong>
                <div className="mc-task-execution-subline">
                    <span className={`mc-task-execution-status${status.tone ? ` mc-task-execution-status--${status.tone}` : ""}`} data-status={status.tone || "group"} role="status"><i aria-hidden="true" />{status.label}</span>
                    {skill ? <div className="mc-task-execution-meta" title={skill}>{skill}</div> : null}
                </div>
            </div>
            {(showSwitcher || showWindowControls) ? (
            <div className="mc-task-execution-actions" data-testid="ve-conversation-title-actions" {...windowNoDragRegionProps()}>
                {showSwitcher ? (
                    <TaskTabSwitcher
                        tabs={tabs && tabs.length > 0 ? tabs : [tab]}
                        activeTabId={tab.id}
                        lang={lang}
                        onActivate={chrome.onActivateTab!}
                        onClose={chrome.onCloseTab!}
                    />
                ) : null}
                {showWindowControls ? (
                    <div className="mc-task-execution-window-controls" data-testid="ve-window-controls" role="group" aria-label={localizeText(lang, "Window controls", "窗口控制", "窗口控制")}>
                        {chrome?.onHideWindow ? <TaskHideWindowButton lang={lang} onHideWindow={chrome.onHideWindow} /> : null}
                        {chrome?.onToggleMaximize ? <TaskMaximizeWindowButton lang={lang} maximized={!!chrome.maximized} onToggleMaximize={chrome.onToggleMaximize} /> : null}
                    </div>
                ) : null}
            </div>
            ) : null}
        </div>
    );
}
