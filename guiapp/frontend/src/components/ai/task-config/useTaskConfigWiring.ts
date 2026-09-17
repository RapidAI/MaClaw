import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn } from "../../../../wailsjs/runtime";
import {
    CloudWorkspaceEntitlement,
    CreateCloudWorkspace,
    CreateTaskUnified,
    EnsureCodingWorkbenchArmed,
    ListExperts,
    ListManagedIndustryExperts,
    ListWorkflowTemplateSummaries,
    SelectWorkingDir,
} from "../../../../wailsjs/go/main/App";
import {
    EVENT_EXPERTS_CHANGED,
    EVENT_OPEN_NEW_TASK_WIZARD,
    EVENT_OPEN_TASK_LAUNCH,
    type OpenTaskLaunchDetail,
} from "../../../constants/events";
import { openExpertConversation } from "../../../utils/expertConversationNavigation";
import type { WelcomePromptSubmitMeta } from "../AssistantWelcomeView";
import { isCloudWorkspacePath } from "../codingTaskMode";
import { parseExpertListJSON, parseInstalledManagedIndustryExpertsJSON } from "../expertTypes";
import { extractErrorMessage } from "../participantAddError";
import { defaultTaskDraft, isDraftDefault, type TaskDraft } from "./taskDraft";
import { runTaskConfigSend } from "./taskConfigSend";
import type { CloudWorkspaceOption } from "./WorkspacePickerPopover";
import type { ExpertOption, WorkflowOption } from "./TaskConfigBar";

/** Human-readable size for the cloud workspace row subtitle. */
function formatCloudWorkspaceSize(bytes: number): string {
    const value = Number.isFinite(bytes) ? Math.max(0, bytes) : 0;
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/** Localized status text for the entitlement workspace `status` field. */
function cloudWorkspaceStateLabel(status: unknown, isZh: boolean): string {
    const raw = (typeof status === "string" ? status : "").trim().toLowerCase();
    switch (raw) {
        case "running": return isZh ? "运行中" : "Running";
        case "stopped": return isZh ? "已停止" : "Stopped";
        case "provisioning": return isZh ? "创建中" : "Provisioning";
        case "error":
        case "failed": return isZh ? "异常" : "Error";
        default: return raw || (isZh ? "未知" : "Unknown");
    }
}

/**
 * Everything the panel must hand in. Field names match the panel's own scope
 * one-to-one so the call site stays a plain shorthand object literal.
 */
export interface TaskConfigWiringOptions {
    activeTab: { id: string; type?: string };
    isLocalTabActive: boolean;
    lang?: string | null;
    taskListProp?: Array<{ working_dir?: unknown }> | null;
    inputRef: { current: HTMLTextAreaElement | null };
    inputValue: string;
    composeAction: unknown;
    inputLocked: boolean;
    messages: unknown[];
    handleSend: () => void;
    handleWelcomePromptSend: (text: string, meta?: WelcomePromptSubmitMeta) => unknown;
    clearComposerDraft: (options?: { clearAttachments?: boolean; focus?: boolean }) => void;
    clearActiveHistory: () => unknown;
    getTabs: () => Array<{ id: string; type?: string }>;
    getTabState: (tabId: string) => { history?: unknown[]; newTaskWizard?: boolean } | null | undefined;
    saveTabState: (tabId: string, patch: { newTaskWizard: boolean }) => void;
    activateTab: (tabId: string) => void;
    setQueueInteractionStarted: (value: boolean) => void;
    setQueueEditDraftActive: (value: boolean) => void;
    setEditingEntryId: (value: string | null) => void;
}

/** The object AssistantWelcomeView consumes through its `taskConfig` prop. */
export interface TaskConfigPanelModel {
    draft: TaskDraft;
    onDraftChange: (draft: TaskDraft) => void;
    experts: ExpertOption[];
    workflows: WorkflowOption[];
    cloudWorkspaces?: CloudWorkspaceOption[];
    recentLocalPaths: string[];
    onBrowseLocal: () => Promise<string | null>;
    onCreateCloud?: () => void;
    disabled: boolean;
    sending: boolean;
    error: string;
    defaultExpanded: boolean;
}

export interface TaskConfigWiring {
    taskConfig: TaskConfigPanelModel;
    handleSendWithTaskConfig: () => void;
    handleWelcomePromptSendWithTaskConfig: (text: string, meta?: WelcomePromptSubmitMeta) => Promise<unknown>;
}

/**
 * Owns new-task wizard (TaskConfigBar) state plus the send orchestration that
 * both the composer Enter path and the welcome-card path funnel through.
 * Extracted out of AIAssistantPanel to keep the panel under its line budget.
 */
export function useTaskConfigWiring(options: TaskConfigWiringOptions): TaskConfigWiring {
    const {
        activeTab, isLocalTabActive, lang, taskListProp,
        inputRef, inputValue, composeAction, inputLocked, messages,
        handleSend, handleWelcomePromptSend, clearComposerDraft, clearActiveHistory,
        getTabs, getTabState, saveTabState, activateTab,
        setQueueInteractionStarted, setQueueEditDraftActive, setEditingEntryId,
    } = options;

    // --- New-task wizard (TaskConfigBar) wiring ---
    // Draft lives here — above both the composer Enter path and the welcome
    // card click path — so a non-default draft intercepts every send.
    const [taskConfigDraft, setTaskConfigDraft] = useState<TaskDraft>(() => defaultTaskDraft());
    const taskConfigDraftRef = useRef(taskConfigDraft);
    taskConfigDraftRef.current = taskConfigDraft;
    const [taskConfigExperts, setTaskConfigExperts] = useState<ExpertOption[]>([]);
    const [taskConfigWorkflows, setTaskConfigWorkflows] = useState<WorkflowOption[]>([]);
    const [taskConfigCloudWorkspaces, setTaskConfigCloudWorkspaces] = useState<CloudWorkspaceOption[]>([]);
    const [taskConfigError, setTaskConfigError] = useState("");
    const [taskConfigSending, setTaskConfigSending] = useState(false);

    // Experts + workflows load once per panel mount; failures degrade to empty
    // lists (the bar itself tolerates missing sources). Experts re-load when
    // the backend broadcasts experts:changed (market install/uninstall,
    // local save/delete, managed industry install).
    const loadTaskConfigExperts = useCallback(async () => {
        try {
            const [personalRaw, managedRaw] = await Promise.all([
                Promise.resolve().then(() => ListExperts()).catch(() => ""),
                Promise.resolve().then(() => ListManagedIndustryExperts()).catch(() => ""),
            ]);
            const personal = parseExpertListJSON(personalRaw);
            const seen = new Set(personal.map(e => e.id));
            const managed = parseInstalledManagedIndustryExpertsJSON(managedRaw).filter(e => !seen.has(e.id));
            setTaskConfigExperts([...personal, ...managed].map(e => ({
                id: e.id,
                name: e.name || e.id,
                description: e.description || "",
                icon: e.icon || "",
            })));
        } catch {
            setTaskConfigExperts([]);
        }
    }, []);

    useEffect(() => {
        void loadTaskConfigExperts();
    }, [loadTaskConfigExperts]);

    useEffect(() => {
        const off = EventsOn(EVENT_EXPERTS_CHANGED, () => { void loadTaskConfigExperts(); });
        return () => { if (typeof off === "function") off(); };
    }, [loadTaskConfigExperts]);

    // Cloud workspaces load once per panel mount and re-load after the bar's
    // 「新建云端工作区…」 entry creates one (the entitlement response doubles
    // as the list); failures degrade to an empty list.
    const loadTaskConfigCloudWorkspaces = useCallback(async () => {
        const isZh = !lang?.startsWith("en");
        try {
            const ent = await CloudWorkspaceEntitlement();
            const rows = Array.isArray(ent?.workspaces) ? ent.workspaces : [];
            setTaskConfigCloudWorkspaces(rows
                .map((row) => {
                    const id = String(row?.id || "").trim();
                    return {
                        id,
                        name: String(row?.name || "").trim() || id,
                        spec: formatCloudWorkspaceSize(Number(row?.retained_bytes) || Number(row?.used_bytes) || 0),
                        state: cloudWorkspaceStateLabel(row?.status, isZh),
                    };
                })
                .filter((row) => row.id));
        } catch {
            setTaskConfigCloudWorkspaces([]);
        }
    }, [lang]);

    useEffect(() => {
        void loadTaskConfigCloudWorkspaces();
    }, [loadTaskConfigCloudWorkspaces]);

    // SidebarTaskManagement's create dialog provisions workspaces through the
    // same binding with an empty name (backend picks a default); the popover
    // stays open so the fresh workspace can be picked from the refreshed list.
    const handleTaskConfigCreateCloud = useCallback(() => {
        void (async () => {
            try {
                await CreateCloudWorkspace("");
                setTaskConfigError("");
            } catch (err) {
                setTaskConfigError(extractErrorMessage(err) || (!lang?.startsWith("en") ? "新建云端工作区失败" : "Failed to create cloud workspace"));
            } finally {
                await loadTaskConfigCloudWorkspaces();
            }
        })();
    }, [lang, loadTaskConfigCloudWorkspaces]);

    useEffect(() => {
        let cancelled = false;
        void (async () => {
            try {
                const list = await ListWorkflowTemplateSummaries();
                if (cancelled) return;
                setTaskConfigWorkflows((Array.isArray(list) ? list : [])
                    .filter(w => !w.semanticOnly)
                    .map(w => ({
                        id: w.id,
                        title: w.title || w.id,
                        category: w.category || "",
                        phaseCount: w.phaseCount || 0,
                        requiresWorkingDir: !!w.requiresWorkingDir,
                        hasParamSlots: !!w.hasParamSlots,
                    })));
            } catch {
                if (!cancelled) setTaskConfigWorkflows([]);
            }
        })();
        return () => { cancelled = true; };
    }, []);

    // Recent local directories come from the existing task list (most recent
    // first); directories picked through the native browse dialog are
    // prepended so a fresh pick is immediately re-selectable.
    const [taskConfigBrowsedPaths, setTaskConfigBrowsedPaths] = useState<string[]>([]);
    const taskConfigRecentPaths = useMemo(() => {
        const dirs: string[] = [];
        const seen = new Set<string>();
        for (const path of taskConfigBrowsedPaths) {
            if (!path || seen.has(path)) continue;
            seen.add(path);
            dirs.push(path);
        }
        for (const task of taskListProp || []) {
            const dir = String(task?.working_dir || "").trim();
            if (!dir || seen.has(dir)) continue;
            if (isCloudWorkspacePath(dir)) continue;
            seen.add(dir);
            dirs.push(dir);
            if (dirs.length >= 8) break;
        }
        return dirs.slice(0, 8);
    }, [taskListProp, taskConfigBrowsedPaths]);

    // Native directory picker (same binding as the task-management create
    // dialog). Empty string = cancelled; cloud-cache picks are refused like
    // the sidebar dialog does.
    const handleTaskConfigBrowseLocal = useCallback(async () => {
        try {
            const dir = await SelectWorkingDir();
            const path = String(dir || "").trim();
            if (!path) return null;
            if (isCloudWorkspacePath(path)) {
                setTaskConfigError(!lang?.startsWith("en")
                    ? "该目录是云端工作区缓存目录，请选择其他目录"
                    : "That folder is a cloud workspace cache. Pick another folder.");
                return null;
            }
            setTaskConfigError("");
            setTaskConfigBrowsedPaths(prev => [path, ...prev.filter(p => p !== path)]);
            return path;
        } catch {
            return null;
        }
    }, [lang]);

    const runConfiguredTaskSend = useCallback(async (rawText: string, sendOptions?: { force?: boolean }): Promise<boolean> => {
        const draft = taskConfigDraftRef.current;
        const force = sendOptions?.force === true;
        // In-flight guard covers the force path too: a wizard tab with an
        // all-default draft would otherwise double-create tasks on a quick
        // double-Enter (default drafts reach this function only via force).
        if (taskConfigSending) return false;
        setTaskConfigSending(true);
        setTaskConfigError("");
        try {
            const result = await runTaskConfigSend({
                text: rawText,
                draft,
                force,
                isZh: !lang?.startsWith("en"),
                bindings: {
                    createTaskUnified: (opts) => CreateTaskUnified(opts as unknown as Parameters<typeof CreateTaskUnified>[0]),
                    ensureCodingArmed: (projectPath) => EnsureCodingWorkbenchArmed(projectPath),
                    openTaskLaunch: (nav) => {
                        const detail: OpenTaskLaunchDetail = {
                            projectPath: nav.projectPath,
                            taskTitle: nav.taskTitle,
                            initialMessage: nav.initialMessage,
                            agentMode: nav.agentMode,
                            cloudWorkspaceId: nav.cloudWorkspaceId,
                            remoteHost: nav.remoteHost,
                            remoteSafety: nav.remoteSafety,
                            remoteNeedsReconnect: nav.remoteNeedsReconnect,
                            warning: nav.warning,
                            noWorkflowInterception: nav.noWorkflowInterception,
                        };
                        window.dispatchEvent(new CustomEvent(EVENT_OPEN_TASK_LAUNCH, { detail }));
                    },
                    openExpert: (nav) => {
                        openExpertConversation({ id: nav.id, name: nav.name, description: nav.description }, nav.initialMessage);
                    },
                },
            });
            if (!result.ok) {
                setTaskConfigError(result.error || (!lang?.startsWith("en") ? "创建任务失败" : "Failed to create task"));
                return false;
            }
            // Success: the launch/expert handoff owns the first message; reset
            // the composer and the wizard draft, and retire the wizard marker
            // on the local tab (it is an ordinary assistant page again).
            clearComposerDraft({ clearAttachments: true });
            setTaskConfigDraft(defaultTaskDraft());
            if (isLocalTabActive && getTabState(activeTab.id)?.newTaskWizard) {
                saveTabState(activeTab.id, { newTaskWizard: false });
            }
            return true;
        } catch (err) {
            setTaskConfigError(extractErrorMessage(err) || (!lang?.startsWith("en") ? "创建任务失败" : "Failed to create task"));
            return false;
        } finally {
            setTaskConfigSending(false);
        }
    }, [activeTab.id, clearComposerDraft, getTabState, isLocalTabActive, lang, saveTabState, taskConfigSending]);

    // True while the active local tab is a new-task wizard page: its first
    // send always creates a task (even with an all-default draft).
    const isNewTaskWizardTabActive = useCallback((): boolean => {
        return isLocalTabActive && !!getTabState(activeTab.id)?.newTaskWizard;
    }, [activeTab.id, getTabState, isLocalTabActive]);

    // Composer Enter interception: a default draft keeps the legacy path
    // untouched (hard compatibility requirement) — except on a wizard tab,
    // where the first send must create the task.
    const handleSendWithTaskConfig = useCallback(() => {
        const draft = taskConfigDraftRef.current;
        const wizardActive = isNewTaskWizardTabActive();
        if ((isDraftDefault(draft) && !wizardActive) || composeAction) {
            handleSend();
            return;
        }
        const rawInputValue = inputRef.current?.value ?? inputValue;
        const text = (rawInputValue || "").trim();
        // Slash commands (/btw, installs, resets) keep their dedicated paths.
        if (!text || text.startsWith("/")) {
            handleSend();
            return;
        }
        void runConfiguredTaskSend(text, { force: wizardActive });
    }, [composeAction, handleSend, inputValue, isNewTaskWizardTabActive, runConfiguredTaskSend]);

    // Welcome card "send now" path respects the wizard draft too.
    const handleWelcomePromptSendWithTaskConfig = useCallback(async (text: string, meta?: WelcomePromptSubmitMeta) => {
        const draft = taskConfigDraftRef.current;
        const wizardActive = isNewTaskWizardTabActive();
        const trimmed = (text || "").trim();
        if ((!isDraftDefault(draft) || wizardActive) && trimmed && !trimmed.startsWith("/")) {
            await runConfiguredTaskSend(trimmed, { force: wizardActive });
            return;
        }
        return handleWelcomePromptSend(text, meta);
    }, [handleWelcomePromptSend, isNewTaskWizardTabActive, runConfiguredTaskSend]);

    // --- New-task wizard page opening (task-pane "新建任务" button) ---
    // Phase 1 activates the fixed local tab; phase 2 runs one tick later so
    // its closures see the local tab as active (clear/mark target the local
    // session, not whatever tab was active when the button was clicked).
    const openNewTaskWizardRef = useRef<() => void>(() => {});
    openNewTaskWizardRef.current = () => {
        const localTab = getTabs().find(tab => tab.type === "local");
        if (!localTab) return;
        const state = getTabState(localTab.id);
        const hasConversation = (Array.isArray(state?.history) ? state.history : []).some((m) => {
            const msg = m as { role?: unknown };
            return !!msg && typeof msg === "object" && (msg.role === "user" || msg.role === "assistant");
        }) || messages.length > 0;
        if (hasConversation && !inputLocked) {
            // Same reset as the title-bar "New conversation" control.
            void Promise.resolve(clearActiveHistory()).catch(() => {});
        }
        setQueueInteractionStarted(false);
        setQueueEditDraftActive(false);
        setEditingEntryId(null);
        clearComposerDraft({ clearAttachments: true });
        saveTabState(localTab.id, { newTaskWizard: true });
        requestAnimationFrame(() => inputRef.current?.focus());
    };
    const openNewTaskWizard = useCallback(() => {
        const localTab = getTabs().find(tab => tab.type === "local");
        if (!localTab) return;
        if (activeTab.id !== localTab.id) activateTab(localTab.id);
        window.setTimeout(() => openNewTaskWizardRef.current(), 0);
    }, [activateTab, activeTab.id, getTabs]);
    useEffect(() => {
        const handler = () => openNewTaskWizard();
        window.addEventListener(EVENT_OPEN_NEW_TASK_WIZARD, handler);
        return () => window.removeEventListener(EVENT_OPEN_NEW_TASK_WIZARD, handler);
    }, [openNewTaskWizard]);

    const taskConfig = useMemo<TaskConfigPanelModel>(() => ({
        draft: taskConfigDraft,
        onDraftChange: setTaskConfigDraft,
        experts: taskConfigExperts,
        workflows: taskConfigWorkflows,
        cloudWorkspaces: taskConfigCloudWorkspaces,
        recentLocalPaths: taskConfigRecentPaths,
        onBrowseLocal: handleTaskConfigBrowseLocal,
        onCreateCloud: handleTaskConfigCreateCloud,
        disabled: inputLocked,
        sending: taskConfigSending,
        error: taskConfigError,
        defaultExpanded: isNewTaskWizardTabActive(),
    }), [
        taskConfigDraft, taskConfigExperts, taskConfigWorkflows, taskConfigCloudWorkspaces, taskConfigRecentPaths,
        handleTaskConfigBrowseLocal, handleTaskConfigCreateCloud, inputLocked, taskConfigSending, taskConfigError,
        isNewTaskWizardTabActive,
    ]);

    return { taskConfig, handleSendWithTaskConfig, handleWelcomePromptSendWithTaskConfig };
}
