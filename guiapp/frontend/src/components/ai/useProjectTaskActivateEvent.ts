import { useEffect } from "react";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { EVENT_PROJECT_TASK_ACTIVATE } from "../../constants/events";
import { cloudWorkspaceIdFromPath } from "./codingTaskMode";
import { normalizeProjectSessionPath } from "./aiAssistantPanelSessionUtils";
import type { AITab } from "./AITabTypes";

/**
 * Focus-only activation from the task list: never re-runs restore/rehydrate,
 * just selects the matching open tab (project by path/cloud id, expert by id).
 */
export function useProjectTaskActivateEvent(opts: {
    activateTab: (tabId: string) => void;
    getTabs: () => AITab[];
}) {
    const { activateTab, getTabs } = opts;
    useEffect(() => {
        const offActivate = EventsOn(EVENT_PROJECT_TASK_ACTIVATE, (payload: { projectPath?: string; cloudWorkspaceId?: string; expertId?: string; local?: boolean } | string) => {
            const detail = typeof payload === "string" ? { projectPath: payload } : payload || {};
            if (detail.local) {
                const localTab = getTabs().find(t => t.type === "local");
                if (localTab) activateTab(localTab.id);
                return;
            }
            const expertId = String(detail.expertId || "").trim();
            const cloudWorkspaceId = String(detail.cloudWorkspaceId || "").trim();
            const normalizedPath = normalizeProjectSessionPath(String(detail.projectPath || ""));
            const tab = getTabs().find(t => {
                if (expertId) return t.type === "expert" && t.expertId === expertId;
                if (t.type !== "project") return false;
                if (cloudWorkspaceId && (t.cloudWorkspaceId === cloudWorkspaceId || cloudWorkspaceIdFromPath(t.projectPath) === cloudWorkspaceId)) return true;
                return !!normalizedPath && normalizeProjectSessionPath(t.projectPath) === normalizedPath;
            });
            if (tab) activateTab(tab.id);
        });
        return () => {
            if (typeof offActivate === "function") offActivate();
            else EventsOff(EVENT_PROJECT_TASK_ACTIVATE);
        };
    }, [activateTab, getTabs]);
}
