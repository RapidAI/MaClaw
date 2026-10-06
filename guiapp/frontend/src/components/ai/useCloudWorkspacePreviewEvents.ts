import { useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import {
    CLOUD_WORKSPACE_FILES_CHANGED_EVENT,
    FOCUS_CLOUD_WORKSPACE_TREE_EVENT,
    REVEAL_CLOUD_WORKSPACE_FILES_EVENT,
    cloudWorkspaceIdFromPath,
    cloudWorkspaceRevealMatchesTab,
    isCloudWorkspacePath,
    nextTabWorkingDir,
    parseWailsEventObject,
    type CloudWorkspaceReveal,
    type TabWorkingDir,
} from "./codingTaskMode";
import { normalizeAssistantSessionKey, normalizeProjectSessionPath, projectSessionKey } from "./aiAssistantPanelSessionUtils";

export function useCloudWorkspacePreviewEvents(opts: {
    activeTab: { id: string; projectPath?: string };
    resolvedDirForActive: string;
    isCloudWorkspaceEnvironment: boolean;
    pendingCloudRevealRef: MutableRefObject<CloudWorkspaceReveal | null>;
    setCloudReveal: Dispatch<SetStateAction<TabWorkingDir | null>>;
    setResolvedWorkingDir: Dispatch<SetStateAction<TabWorkingDir | null>>;
    reopenCodePreview: () => void;
    bumpLocalWorkspaceRefresh: () => void;
    scheduleCloudTreeRefresh: () => void;
    clearCloudTreeRefreshTimer: () => void;
}) {
    const {
        activeTab,
        resolvedDirForActive,
        isCloudWorkspaceEnvironment,
        pendingCloudRevealRef,
        setCloudReveal,
        setResolvedWorkingDir,
        reopenCodePreview,
        bumpLocalWorkspaceRefresh,
        scheduleCloudTreeRefresh,
        clearCloudTreeRefreshTimer,
    } = opts;

    useEffect(() => {
        const tryReveal = (rawPath: string, workingDir = "") => {
            const want = normalizeProjectSessionPath(rawPath);
            if (!want) return;
            const detail = { projectPath: want, workingDir };
            const matches = cloudWorkspaceRevealMatchesTab(detail, {
                projectPath: activeTab.projectPath,
                workingDir: resolvedDirForActive,
            });
            if (matches) {
                pendingCloudRevealRef.current = null;
                setCloudReveal({ tabId: activeTab.id, path: want });
                if (isCloudWorkspacePath(workingDir)) {
                    setResolvedWorkingDir((prev) => nextTabWorkingDir(prev, activeTab.id, workingDir));
                }
                reopenCodePreview();
                bumpLocalWorkspaceRefresh();
                window.dispatchEvent(new CustomEvent(FOCUS_CLOUD_WORKSPACE_TREE_EVENT));
                return;
            }
            pendingCloudRevealRef.current = { projectPath: want, workingDir };
        };
        const handler = (e: Event) => {
            const detail = (e as CustomEvent<{ projectPath?: string; workingDir?: string }>).detail;
            tryReveal(String(detail?.projectPath || ""), String(detail?.workingDir || ""));
        };
        window.addEventListener(REVEAL_CLOUD_WORKSPACE_FILES_EVENT, handler);
        if (pendingCloudRevealRef.current) {
            tryReveal(
                String(pendingCloudRevealRef.current.projectPath || ""),
                String(pendingCloudRevealRef.current.workingDir || ""),
            );
        }
        return () => window.removeEventListener(REVEAL_CLOUD_WORKSPACE_FILES_EVENT, handler);
    }, [activeTab.id, activeTab.projectPath, bumpLocalWorkspaceRefresh, pendingCloudRevealRef, reopenCodePreview, resolvedDirForActive, setCloudReveal, setResolvedWorkingDir]);

    useEffect(() => {
        if (!isCloudWorkspaceEnvironment) return;
        const tab = { projectPath: activeTab.projectPath, workingDir: resolvedDirForActive };
        const tabWorkspaceId = cloudWorkspaceIdFromPath(resolvedDirForActive || activeTab.projectPath);
        const matchesActive = (path: string, workspaceId: string) => {
            if (workspaceId && tabWorkspaceId && workspaceId === tabWorkspaceId) return true;
            return cloudWorkspaceRevealMatchesTab({ projectPath: path, workingDir: path }, tab);
        };
        const offFiles = EventsOn(CLOUD_WORKSPACE_FILES_CHANGED_EVENT, (payload: unknown) => {
            const rec = parseWailsEventObject(payload);
            const path = String(rec.path || rec.Path || "");
            const workspaceId = String(rec.workspace_id || rec.workspaceId || rec.WorkspaceID || "");
            if (matchesActive(path, workspaceId)) scheduleCloudTreeRefresh();
        });
        const expectedSession = projectSessionKey(activeTab.projectPath || "");
        const offTurn = EventsOn("ai-assistant-response", (payload: unknown) => {
            const rec = parseWailsEventObject(payload);
            const sk = normalizeAssistantSessionKey(String(rec.session_key || rec.SessionKey || ""));
            if (!expectedSession || sk !== normalizeAssistantSessionKey(expectedSession)) return;
            scheduleCloudTreeRefresh();
        });
        return () => {
            clearCloudTreeRefreshTimer();
            if (typeof offFiles === "function") offFiles();
            if (typeof offTurn === "function") offTurn();
        };
    }, [isCloudWorkspaceEnvironment, activeTab.id, activeTab.projectPath, scheduleCloudTreeRefresh, clearCloudTreeRefreshTimer, resolvedDirForActive]);
}
