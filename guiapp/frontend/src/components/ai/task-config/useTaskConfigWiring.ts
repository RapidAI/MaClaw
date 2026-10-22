import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn } from "../../../../wailsjs/runtime";
import {
    AbandonUnopenedFreshLatexTask,
    CloudWorkspaceEntitlement,
    CreateCloudWorkspace,
    CreateRunWindowTask,
    CreateTaskUnified,
    RenameCloudWorkspace,
    SuggestNewTaskType,
    EnsureCodingWorkbenchArmed,
    ListExperts,
    ListLatexTemplates,
    ListManagedIndustryExperts,
    ListWorkflowTemplateSummaries,
    SelectWorkingDir,
    SetTabWorkingDir,
} from "../../../../wailsjs/go/main/App";
import {
    EVENT_EXPERTS_CHANGED,
    EVENT_NEW_TASK_WIZARD_BLOCKED,
    EVENT_OPEN_NEW_TASK_WIZARD,
    EVENT_OPEN_TASK_LAUNCH,
    type OpenTaskLaunchDetail,
} from "../../../constants/events";
import { openExpertConversation } from "../../../utils/expertConversationNavigation";
import type { WelcomePromptSubmitMeta } from "../AssistantWelcomeView";
import { isCloudWorkspacePath } from "../codingTaskMode";
import { expertTabId, parseExpertListJSON, parseInstalledManagedIndustryExpertsJSON } from "../expertTypes";
import { extractErrorMessage } from "../participantAddError";
import { applyComposeActionToText, goalCreateCommand, isBtwCommandText, isHistoryResetCommandText, normalizeInstallCommandText, type ComposeAction } from "../composeAction";
import { buildOutgoingMessageMulti } from "../useAIAssistant";
import { defaultTaskDraft, draftFromWizardSeed, isDraftDefault, withCloudWorkspace, withTaskType, type NewTaskWizardSeed, type TaskDraft } from "./taskDraft";
import { mismatchedTaskType, taskTypeMismatchCopy, type NewTaskTypeSuggestion } from "./taskTypeSuggest";
import { isRunWindowScheduled, runWindowCreatedNotice, runWindowCreateRequest, validateRunWindow } from "./runWindow";
import { dispatchDueRunWindows, launchRunWindowTask } from "./runWindowDispatch";
import { setNewTaskCoverHeld } from "./newTaskCoverHold";
import {
    createLatexDocumentForTask,
    isLatexExpertId,
    LATEX_EXPERT_ID,
    latexPaperTaskMessage,
    parseLatexTemplateLibrary,
    type LatexTemplate,
} from "../../../utils/latexTemplates";
import { runTaskConfigSend } from "./taskConfigSend";

function sameLatexTemplateList(left: LatexTemplate[], right: LatexTemplate[]): boolean {
    if (left.length !== right.length) return false;
    for (let i = 0; i < left.length; i += 1) {
        if (left[i]?.id !== right[i]?.id || left[i]?.name !== right[i]?.name) return false;
    }
    return true;
}
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
        case "active": return isZh ? "可用" : "Active";
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
    /** True only while an agent turn is actually executing (busy or cancelling). A running turn no longer blocks the wizard: the welcome page opens over that conversation and the turn keeps running. */
    assistantBusy?: boolean;
    /** Live mic or an ACP mirror must keep the execution surface. The wizard does not cover those. */
    keepExecutionSurface?: boolean;
    /** Empty local startup guide. Sends from that page create a task, same as 新建任务. */
    localGuideVisible?: boolean;
    /** Attachments on the shared composer, snapshotted when a running turn is covered. */
    pendingAttachments?: readonly unknown[];
    /** Files chosen from the picker. They ride along on the new task's first message. */
    selectedFilePaths?: readonly string[];
    /** Puts composer text back on screen (local tab still visible). */
    showComposerText?: (text: string) => void;
    /** New-task screen draft. It is not the conversation composer's text. */
    guideInputValue?: string;
    guideInputRef?: { current: HTMLTextAreaElement | null };
    /** Files the new-task screen itself is holding. Empty unless that screen added them. */
    guidePendingAttachments?: readonly unknown[];
    guideSelectedFilePaths?: readonly string[];
    /** Clears the new-task draft only. */
    clearGuideDraft?: () => void;
    replacePendingAttachments?: (items: unknown[]) => void;
    /** Picker files, restored with the covered conversation's composer. */
    replaceSelectedFilePaths?: (paths: string[]) => void;
    /**
     * The active tab's own transcript has a user or assistant turn.
     * Raw session messages are not a signal: other tabs' chats stay in that list.
     */
    activeChatVisible?: boolean;
    handleSend: () => void;
    handleWelcomePromptSend: (text: string, meta?: WelcomePromptSubmitMeta) => unknown;
    clearComposerDraft: (options?: { clearAttachments?: boolean; focus?: boolean }) => void;
    getTabs: () => Array<{ id: string; type?: string }>;
    getTabState: (tabId: string) => { history?: unknown[]; newTaskWizard?: boolean } | null | undefined;
    saveTabState: (tabId: string, patch: { newTaskWizard: boolean }) => void;
    activateTab: (tabId: string) => void;
    setQueueInteractionStarted: (value: boolean) => void;
    setQueueEditDraftActive: (value: boolean) => void;
    setEditingEntryId: (value: string | null) => void;
    /** App dialog. A scheduled create confirms the start time here. */
    showAlert?: (message: string, title?: string) => Promise<void> | void;
    /**
     * Asks whether a command that does not match the selected type should
     * switch. Confirm switches. Cancel keeps the selected type. Both still
     * create the task. Missing means keep the selected type.
     */
    showConfirm?: (message: string, title?: string, options?: { confirmText?: string; cancelText?: string }) => Promise<boolean> | boolean;
}

/** The object AssistantWelcomeView consumes through its `taskConfig` prop. */
export interface TaskConfigPanelModel {
    draft: TaskDraft;
    onDraftChange: (draft: TaskDraft) => void;
    experts: ExpertOption[];
    workflows: WorkflowOption[];
    latexTemplates?: LatexTemplate[];
    onPrepareLatexTemplates?: () => void;
    cloudWorkspaces?: CloudWorkspaceOption[];
    recentLocalPaths: string[];
    onBrowseLocal: () => Promise<string | null>;
    onCreateCloud?: (name: string) => void | Promise<void>;
    onRenameCloud?: (id: string, name: string) => void | Promise<void>;
    disabled: boolean;
    sending: boolean;
    error: string;
    defaultExpanded: boolean;
}

export interface TaskConfigWiring {
    taskConfig: TaskConfigPanelModel;
    handleSendWithTaskConfig: () => void;
    handleWelcomePromptSendWithTaskConfig: (text: string, meta?: WelcomePromptSubmitMeta) => Promise<unknown>;
    /** Welcome page is covering a live conversation so a new task can be created without stopping it. */
    wizardOverlay: boolean;
    dismissWizardOverlay: () => void;
}

/**
 * Owns new-task wizard (TaskConfigBar) state plus the send orchestration that
 * both the composer Enter path and the welcome-card path funnel through.
 * Extracted out of AIAssistantPanel to keep the panel under its line budget.
 */
export function useTaskConfigWiring(options: TaskConfigWiringOptions): TaskConfigWiring {
    const {
        activeTab, isLocalTabActive, lang, taskListProp,
        inputRef, inputValue, composeAction, inputLocked, assistantBusy = false, keepExecutionSurface = false, localGuideVisible = false,
        pendingAttachments, selectedFilePaths, showComposerText, guideInputValue = "", guideInputRef, guidePendingAttachments, guideSelectedFilePaths, clearGuideDraft, replacePendingAttachments, replaceSelectedFilePaths, activeChatVisible = false,
        handleSend, handleWelcomePromptSend, clearComposerDraft,
        getTabs, getTabState, saveTabState,
        setQueueInteractionStarted, setQueueEditDraftActive, setEditingEntryId,
        showAlert, showConfirm,
    } = options;

    // --- New-task wizard (TaskConfigBar) wiring ---
    // Draft lives here — above both the composer Enter path and the welcome
    // card click path — so a non-default draft intercepts every send.
    const [taskConfigDraft, setTaskConfigDraft] = useState<TaskDraft>(() => defaultTaskDraft());
    const taskConfigDraftRef = useRef(taskConfigDraft);
    taskConfigDraftRef.current = taskConfigDraft;
    const [taskConfigExperts, setTaskConfigExperts] = useState<ExpertOption[]>([]);
    const [taskConfigWorkflows, setTaskConfigWorkflows] = useState<WorkflowOption[]>([]);
    const [taskConfigLatexTemplates, setTaskConfigLatexTemplates] = useState<LatexTemplate[]>([]);
    const latexListGenRef = useRef(0);
    const [taskConfigCloudWorkspaces, setTaskConfigCloudWorkspaces] = useState<CloudWorkspaceOption[]>([]);
    const cloudListGenRef = useRef(0);
    const [taskConfigError, setTaskConfigError] = useState("");
    const [taskConfigSending, setTaskConfigSending] = useState(false);
    const taskConfigSendingRef = useRef(false);
    // The new-task screen is showing. The conversation underneath stays mounted.
    const [wizardOverlay, setWizardOverlay] = useState(false);
    const wizardOverlayRef = useRef(false);
    wizardOverlayRef.current = wizardOverlay;
    /** Unsent composer captured when the welcome page covers a live turn. */
    const composerStashRef = useRef<{ text: string; attachments: unknown[]; selectedPaths: string[] } | null>(null);

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
    const loadTaskConfigCloudWorkspaces = useCallback(async (): Promise<CloudWorkspaceOption[] | null> => {
        const gen = ++cloudListGenRef.current;
        const isZh = !lang?.startsWith("en");
        try {
            const ent = await CloudWorkspaceEntitlement();
            if (gen !== cloudListGenRef.current) return null;
            const rows = (Array.isArray(ent?.workspaces) ? ent.workspaces : [])
                .map((row) => {
                    const id = String(row?.id || "").trim();
                    return {
                        id,
                        name: String(row?.name || "").trim() || id,
                        spec: formatCloudWorkspaceSize(Number(row?.retained_bytes) || Number(row?.used_bytes) || 0),
                        state: cloudWorkspaceStateLabel(row?.status, isZh),
                    };
                })
                .filter((row) => row.id);
            setTaskConfigCloudWorkspaces(rows);
            return rows;
        } catch {
            if (gen !== cloudListGenRef.current) return null;
            setTaskConfigCloudWorkspaces([]);
            return [];
        }
    }, [lang]);

    useEffect(() => {
        void loadTaskConfigCloudWorkspaces();
    }, [loadTaskConfigCloudWorkspaces]);

    // The new-task picker collects a name before creating, so Hub does not
    // allocate a placeholder 工作区 N. The popover stays open; the new
    // workspace is selected on the draft once the list reloads.
    const handleTaskConfigCreateCloud = useCallback(async (name: string) => {
        const trimmed = name.trim();
        if (!trimmed) {
            throw new Error(!lang?.startsWith("en") ? "请填写工作区名称" : "Workspace name is required");
        }
        try {
            const created = await CreateCloudWorkspace(trimmed);
            setTaskConfigError("");
            const loaded = await loadTaskConfigCloudWorkspaces();
            const id = String(created?.id || "").trim();
            const createdName = String(created?.name || trimmed).trim() || trimmed;
            if (id) {
                if (loaded) {
                    const isZh = !lang?.startsWith("en");
                    const merged = loaded.some((row) => row.id === id)
                        ? loaded.map((row) => row.id === id ? { ...row, name: createdName } : row)
                        : [{ id, name: createdName, spec: "0 B", state: isZh ? "可用" : "Active" }, ...loaded];
                    setTaskConfigCloudWorkspaces(merged);
                }
                setTaskConfigDraft((prev) => withCloudWorkspace(prev, id, createdName));
            }
        } catch (err) {
            setTaskConfigError(extractErrorMessage(err) || (!lang?.startsWith("en") ? "新建云端工作区失败" : "Failed to create cloud workspace"));
            throw err;
        }
    }, [lang, loadTaskConfigCloudWorkspaces]);

    const handleTaskConfigRenameCloud = useCallback(async (id: string, name: string) => {
        const workspaceId = id.trim();
        const trimmed = name.trim();
        if (!workspaceId || !trimmed) {
            throw new Error(!lang?.startsWith("en") ? "请填写工作区名称" : "Workspace name is required");
        }
        try {
            const renamed = await RenameCloudWorkspace(workspaceId, trimmed);
            setTaskConfigError("");
            const loaded = await loadTaskConfigCloudWorkspaces();
            const nextName = String(renamed?.name || trimmed).trim() || trimmed;
            if (loaded) {
                setTaskConfigCloudWorkspaces(loaded.map((row) => row.id === workspaceId ? { ...row, name: nextName } : row));
            }
            setTaskConfigDraft((prev) => {
                if (prev.workspace.kind !== "cloud" || prev.workspace.cloudWorkspaceId !== workspaceId) return prev;
                return withCloudWorkspace(prev, workspaceId, nextName);
            });
        } catch (err) {
            setTaskConfigError(extractErrorMessage(err) || (!lang?.startsWith("en") ? "云端工作区改名失败" : "Failed to rename cloud workspace"));
            throw err;
        }
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

    // A cancelled or failed read must not stick. Opening the template menu
    // loads again, so a template imported after the first look shows up.
    const loadTaskConfigLatexTemplates = useCallback(async () => {
        const gen = ++latexListGenRef.current;
        try {
            const library = parseLatexTemplateLibrary(await ListLatexTemplates());
            if (gen !== latexListGenRef.current) return;
            setTaskConfigLatexTemplates((prev) => (sameLatexTemplateList(prev, library.templates) ? prev : library.templates));
        } catch {
            if (gen !== latexListGenRef.current) return;
        }
    }, []);

    useEffect(() => {
        if (!isLatexExpertId(taskConfigDraft.expertId)) return;
        void loadTaskConfigLatexTemplates();
    }, [loadTaskConfigLatexTemplates, taskConfigDraft.expertId]);

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

    const retireLocalWizardFlag = useCallback(() => {
        const localTab = getTabs().find(tab => tab.type === "local");
        if (localTab && getTabState(localTab.id)?.newTaskWizard) {
            saveTabState(localTab.id, { newTaskWizard: false });
        }
    }, [getTabState, getTabs, saveTabState]);

    const handoffComposer = useCallback((options?: { focus?: boolean }) => {
        const next = defaultTaskDraft();
        // Publish before the next render. A repeated Enter in this turn must
        // not still see the scheduled draft and create another copy.
        taskConfigDraftRef.current = next;
        // The new-task screen over another tab has its own draft. Clearing the
        // shared composer would erase the conversation the user is coming back to.
        if (wizardOverlayRef.current && !isLocalTabActive) clearGuideDraft?.();
        else clearComposerDraft({ clearAttachments: true, focus: options?.focus !== false });
        setTaskConfigDraft(next);
        const coveredTurn = wizardOverlayRef.current;
        setWizardOverlay(false);
        if (coveredTurn) retireLocalWizardFlag();
        const covered = composerStashRef.current;
        composerStashRef.current = null;
        if (covered?.text) showComposerText?.(covered.text);
        if (covered?.attachments?.length) replacePendingAttachments?.(covered.attachments);
        if (covered?.selectedPaths?.length) replaceSelectedFilePaths?.(covered.selectedPaths);
    }, [clearComposerDraft, clearGuideDraft, isLocalTabActive, replacePendingAttachments, replaceSelectedFilePaths, retireLocalWizardFlag, showComposerText]);

    // A confident local or LLM reading that disagrees with the selected type
    // asks once. Agreeing updates the draft (coding drops a cloud workspace).
    // Refusing, a classifier failure, or no dialog keeps the selected type.
    // Experts ignore the type, and a remote workspace already runs as remote
    // coding for both types, so those creates are not asked.
    const alignTaskTypeWithCommand = useCallback(async (command: string, draft: TaskDraft): Promise<TaskDraft> => {
        const text = (command || "").trim();
        if (!text || (draft.expertId || "").trim() || draft.workspace.kind === "remote") return draft;
        let suggestion: NewTaskTypeSuggestion | null = null;
        try {
            suggestion = await SuggestNewTaskType(text) as NewTaskTypeSuggestion;
        } catch (err) {
            console.warn("[task-type] classify failed", err);
            return draft;
        }
        const next = mismatchedTaskType(draft.taskType, suggestion);
        if (!next) return draft;
        const isZh = !lang?.startsWith("en");
        const copy = taskTypeMismatchCopy({
            current: draft.taskType,
            suggested: next,
            dropsCloud: draft.workspace.kind === "cloud" && next === "coding",
            isZh,
        });
        let accepted = false;
        try {
            accepted = await Promise.resolve(showConfirm?.(copy.message, copy.title, {
                confirmText: copy.confirmText,
                cancelText: copy.cancelText,
            }) ?? false);
        } catch (err) {
            console.warn("[task-type] confirm failed", err);
            accepted = false;
        }
        if (!accepted) return draft;
        const updated = withTaskType(draft, next);
        taskConfigDraftRef.current = updated;
        setTaskConfigDraft(updated);
        return updated;
    }, [lang, showConfirm]);

    const runConfiguredTaskSend = useCallback(async (rawText: string, sendOptions?: { force?: boolean; initialMessage?: string; classifyText?: string }): Promise<boolean> => {
        const force = sendOptions?.force === true;
        const initialMessage = (sendOptions?.initialMessage || "").trim();
        // In-flight guard covers the force path too: a wizard tab with an
        // all-default draft would otherwise double-create tasks on a quick
        // double-Enter (default drafts reach this function only via force).
        // Ref, not the state closure: a second Enter before re-render must not
        // start another create.
        if (taskConfigSendingRef.current) return false;
        taskConfigSendingRef.current = true;
        setTaskConfigSending(true);
        setTaskConfigError("");
        try {
            const draft = await alignTaskTypeWithCommand(sendOptions?.classifyText ?? rawText, taskConfigDraftRef.current);
            const result = await runTaskConfigSend({
                text: rawText,
                draft,
                force,
                initialMessage: initialMessage || undefined,
                isZh: !lang?.startsWith("en"),
                bindings: {
                    createTaskUnified: (opts) => CreateTaskUnified(opts as unknown as Parameters<typeof CreateTaskUnified>[0]),
                    abandonFreshLatexTask: (projectPath) => AbandonUnopenedFreshLatexTask(projectPath),
                    ensureCodingArmed: (projectPath) => EnsureCodingWorkbenchArmed(projectPath),
                    openTaskLaunch: (nav) => {
                        const detail: OpenTaskLaunchDetail = {
                            projectPath: nav.projectPath,
                            taskTitle: nav.taskTitle,
                            initialMessage: nav.initialMessage,
                            agentMode: nav.agentMode,
                            cloudWorkspaceId: nav.cloudWorkspaceId,
                            fileWorkspaceId: nav.fileWorkspaceId,
                            remoteHost: nav.remoteHost,
                            remoteSafety: nav.remoteSafety,
                            remoteNeedsReconnect: nav.remoteNeedsReconnect,
                            warning: nav.warning,
                            noWorkflowInterception: nav.noWorkflowInterception,
                            deliverInitialMessage: true,
                        };
                        window.dispatchEvent(new CustomEvent(EVENT_OPEN_TASK_LAUNCH, { detail }));
                    },
                    openExpert: (nav) => {
                        openExpertConversation(
                            { id: nav.id, name: nav.name, description: nav.description },
                            nav.initialMessage,
                            nav.latexDocument,
                            nav.projectPath,
                        );
                    },
                    materializeLatexDocument: async ({ projectPath, templateId, templateName, userText }) => {
                        let failure = "";
                        const document = await createLatexDocumentForTask(projectPath, templateId, "", lang || "zh-Hans", (message) => {
                            failure = message;
                        });
                        if (!document?.relative_path) {
                            throw new Error(failure || (!lang?.startsWith("en") ? "无法创建 LaTeX 文档" : "The LaTeX document could not be created"));
                        }
                        // Reusing a paper that is already in the chosen folder does
                        // not retarget the expert tab. Point the tools at that
                        // folder before the first message, or they stay on the
                        // directory the expert used last time.
                        const workspacePath = String(document.workspace_path || "").trim();
                        if (workspacePath) {
                            await SetTabWorkingDir(expertTabId(LATEX_EXPERT_ID), workspacePath);
                        }
                        return {
                            relativePath: document.relative_path,
                            initialMessage: latexPaperTaskMessage(lang || "zh-Hans", {
                                id: document.template_id || templateId,
                                name: document.template_name || templateName,
                            }, document, userText),
                        };
                    },
                },
            });
            if (!result.ok) {
                setTaskConfigError(result.error || (!lang?.startsWith("en") ? "创建任务失败" : "Failed to create task"));
                return false;
            }
            // Success: the launch/expert handoff owns the first message. Reset
            // the composer and the wizard draft. The local guide stays the
            // new-task page — there is no default task to fall back to.
            // The empty guide stays a new-task page, so the next send creates
            // another task. A cover over a live conversation does not: that
            // chat's later sends must stay in the conversation.
            handoffComposer();
            return true;
        } catch (err) {
            setTaskConfigError(extractErrorMessage(err) || (!lang?.startsWith("en") ? "创建任务失败" : "Failed to create task"));
            return false;
        } finally {
            taskConfigSendingRef.current = false;
            setTaskConfigSending(false);
        }
    }, [alignTaskTypeWithCommand, handoffComposer, lang]);

    // The local assistant guide is the new-task page. There is no default
    // task: a send from that page creates a task even when every chip stays
    // on its default. A live local conversation, and project-tab welcomes,
    // keep the legacy path.
    const isNewTaskWizardTabActive = useCallback((): boolean => {
        // The cover paints before the local tab is active. Enter in that gap
        // creates a task; it must not send into the conversation underneath.
        if (wizardOverlay) return true;
        if (!isLocalTabActive) return false;
        return localGuideVisible || !!getTabState(activeTab.id)?.newTaskWizard;
    }, [activeTab.id, getTabState, isLocalTabActive, localGuideVisible, wizardOverlay]);

    const collectGuideFilePaths = useCallback((): string[] => {
        // Over another tab, the new-task screen must not send that tab's files.
        const separate = wizardOverlay && !isLocalTabActive;
        const paths = [
            ...(separate ? (guideSelectedFilePaths ?? []) : (selectedFilePaths ?? [])),
            ...(separate ? (guidePendingAttachments ?? []) : (pendingAttachments ?? [])).map((item) => {
                if (!item || typeof item !== "object") return "";
                return String((item as { filePath?: unknown }).filePath || "").trim();
            }),
        ];
        return paths.map((path) => path.trim()).filter((path, index, all) => path.length > 0 && all.indexOf(path) === index);
    }, [guidePendingAttachments, guideSelectedFilePaths, isLocalTabActive, pendingAttachments, selectedFilePaths, wizardOverlay]);

    const scheduleConfiguredTask = useCallback(async (named: string, initialMessage: string, classifyText = ""): Promise<boolean> => {
        const isZh = !lang?.startsWith("en");
        const check = validateRunWindow(taskConfigDraftRef.current.runWindow, new Date(), isZh);
        if (!check.ok) {
            setTaskConfigError(check.error);
            return false;
        }
        if (typeof CreateRunWindowTask !== "function") {
            setTaskConfigError(isZh ? "运行时段不可用" : "Run windows are unavailable");
            return false;
        }
        if (taskConfigSendingRef.current) return false;
        taskConfigSendingRef.current = true;
        setTaskConfigSending(true);
        setTaskConfigError("");
        let created = false;
        let startAt = "";
        let endAt: string | undefined;
        try {
            const draft = await alignTaskTypeWithCommand(classifyText, taskConfigDraftRef.current);
            startAt = draft.runWindow?.startAt || "";
            endAt = draft.runWindow?.endAt;
            await CreateRunWindowTask(runWindowCreateRequest(named, named, initialMessage, draft) as unknown as Parameters<typeof CreateRunWindowTask>[0]);
            created = true;
        } catch (err) {
            setTaskConfigError(extractErrorMessage(err) || (isZh ? "加入计划任务失败" : "Could not schedule the task"));
            return false;
        } finally {
            // Hold the send lock through the confirmation. Releasing it here
            // lets a repeated Enter create a second task before the dialog opens.
            if (!created) {
                taskConfigSendingRef.current = false;
                setTaskConfigSending(false);
            }
        }
        const notice = runWindowCreatedNotice(named, startAt, endAt, isZh);
        try {
            await Promise.resolve(showAlert?.(notice.message, notice.title));
        } catch (err) {
            console.warn("[run-window] created notice failed", err);
        } finally {
            try {
                handoffComposer();
            } finally {
                taskConfigSendingRef.current = false;
                setTaskConfigSending(false);
            }
        }
        // A start time that has already arrived should open now, not on the
        // next 15s poll. Future windows are left pending by the claim check.
        void dispatchDueRunWindows((task) => launchRunWindowTask(task, lang || "zh-Hans")).catch((err) => {
            console.warn("[run-window] dispatch after create failed", err);
        });
        return true;
    }, [alignTaskTypeWithCommand, handoffComposer, lang, showAlert]);

    // Name stays the typed text. A file with no text uses the filename so the
    // guide still creates a task instead of falling into a nameless session.
    const startGuideTask = useCallback((text: string, wizardActive: boolean, taskName?: string) => {
        const filePaths = collectGuideFilePaths();
        const fileName = filePaths[0] ? (filePaths[0].split(/[/\\]/).pop() || filePaths[0]) : "";
        const typed = (text || "").trim();
        const named = (taskName || typed || fileName).trim();
        if (!named) return Promise.resolve(false);
        const initialMessage = filePaths.length > 0 ? buildOutgoingMessageMulti(text, filePaths) : text || named;
        // Classify the instruction, not a filename standing in for an empty composer.
        const classifyText = (taskName || typed).trim();
        if (isRunWindowScheduled(taskConfigDraftRef.current)) {
            return scheduleConfiguredTask(named, initialMessage, classifyText);
        }
        return runConfiguredTaskSend(named, { force: wizardActive, initialMessage, classifyText });
    }, [collectGuideFilePaths, runConfiguredTaskSend, scheduleConfiguredTask]);

    // Composer Enter on the local guide always creates a task. A default
    // draft on any other surface keeps the legacy send path.
    const handleSendWithTaskConfig = useCallback(() => {
        if (taskConfigSendingRef.current) return;
        const draft = taskConfigDraftRef.current;
        const wizardActive = isNewTaskWizardTabActive();
        // Compose mode on any other page keeps its dedicated send path.
        // On the guide it is still a new task; the prefix rides on the first message.
        if (!wizardActive && !isRunWindowScheduled(draft) && (isDraftDefault(draft) || composeAction)) {
            handleSend();
            return;
        }
        const separateScreen = wizardOverlayRef.current && !isLocalTabActive;
        const rawInputValue = separateScreen
            ? (guideInputRef?.current?.value ?? guideInputValue)
            : (inputRef.current?.value ?? inputValue);
        const raw = (rawInputValue || "").trim();
        const text = applyComposeActionToText(raw, (composeAction as ComposeAction | null) ?? null);
        // /goal <objective> is a new task. Session verbs and other slash
        // commands stay on the conversation that is already open.
        const createdGoal = goalCreateCommand(raw);
        if (wizardActive && createdGoal) {
            void startGuideTask(createdGoal.command, wizardActive, createdGoal.objective);
            return;
        }
        // A slash the user typed (including fullwidth ／) stays on the command
        // path. A prefix added by compose mode, such as /goal, still creates a task.
        const typedSlash = raw.startsWith("/") || raw.startsWith("／");
        if (typedSlash || isHistoryResetCommandText(text) || isBtwCommandText(text) || normalizeInstallCommandText(text)) {
            handleSend();
            return;
        }
        if (!text && collectGuideFilePaths().length === 0) {
            // Empty Enter must not dismiss the guide and reveal an old chat.
            if (!wizardActive) handleSend();
            return;
        }
        if (!text && !wizardActive) {
            handleSend();
            return;
        }
        void startGuideTask(text, wizardActive, composeAction ? raw : undefined);
    }, [collectGuideFilePaths, composeAction, guideInputRef, guideInputValue, handleSend, inputValue, isLocalTabActive, isNewTaskWizardTabActive, startGuideTask]);

    // Welcome card "send now" path uses the same task create, including files
    // already sitting on the composer.
    const handleWelcomePromptSendWithTaskConfig = useCallback(async (text: string, meta?: WelcomePromptSubmitMeta) => {
        if (taskConfigSendingRef.current) return;
        const draft = taskConfigDraftRef.current;
        const wizardActive = isNewTaskWizardTabActive();
        const trimmed = (text || "").trim();
        // Fullwidth ／ from a Chinese IME is the same slash as "/".
        const commandText = trimmed.startsWith("／") ? `/${trimmed.slice(1)}` : trimmed;
        const composed = applyComposeActionToText(commandText, (composeAction as ComposeAction | null) ?? null);
        const createdGoal = goalCreateCommand(commandText);
        if (wizardActive && createdGoal) {
            await startGuideTask(createdGoal.command, wizardActive, createdGoal.objective);
            return;
        }
        const typedSlash = commandText.startsWith("/");
        if (typedSlash || isHistoryResetCommandText(composed) || isBtwCommandText(composed) || normalizeInstallCommandText(composed)) {
            return handleWelcomePromptSend(typedSlash ? commandText : composed, meta);
        }
        const hasFiles = collectGuideFilePaths().length > 0;
        if ((!isDraftDefault(draft) || wizardActive || isRunWindowScheduled(draft)) && (composed || (wizardActive && hasFiles))) {
            await startGuideTask(composed, wizardActive, composeAction ? commandText : undefined);
            return;
        }
        return handleWelcomePromptSend(composed || text, meta);
    }, [collectGuideFilePaths, composeAction, handleWelcomePromptSend, isNewTaskWizardTabActive, startGuideTask]);

    // --- New-task wizard page opening (task-pane "新建任务" button) ---
    // Opening the screen does not switch tabs and does not unmount the
    // transcript. Another tab keeps its composer. The local tab has one draft
    // slot, so that text moves into the stash and comes back on Back.
    const pendingWizardSeedRef = useRef<NewTaskWizardSeed | null>(null);
    const openNewTaskWizard = useCallback(() => {
        const seed = pendingWizardSeedRef.current;
        pendingWizardSeedRef.current = null;
        const localTab = getTabs().find(tab => tab.type === "local");
        if (!localTab) return;
        const separate = !isLocalTabActive;
        const applySeed = () => {
            if (!String(seed?.expertId || "").trim() && !String(seed?.workflowTemplateId || "").trim()) return;
            setTaskConfigDraft(draftFromWizardSeed(seed));
            setTaskConfigError("");
            // A template must not keep leftover guide text as its first message.
            // The other tab's composer is a different box and stays put.
            if (separate) clearGuideDraft?.();
            else clearComposerDraft({ clearAttachments: true });
        };
        const focusGuide = () => {
            window.requestAnimationFrame(() => {
                const node = (separate ? guideInputRef?.current : inputRef.current) ?? inputRef.current;
                node?.focus();
            });
        };
        if (wizardOverlayRef.current) {
            saveTabState(localTab.id, { newTaskWizard: true });
            applySeed();
            focusGuide();
            return;
        }
        if (localGuideVisible) {
            if (assistantBusy) setWizardOverlay(true);
            saveTabState(localTab.id, { newTaskWizard: true });
            applySeed();
            focusGuide();
            return;
        }
        const chatTurn = (entry: unknown) => {
            const msg = entry as { role?: unknown };
            return !!msg && typeof msg === "object" && (msg.role === "user" || msg.role === "assistant");
        };
        const state = getTabState(localTab.id);
        const localHasConversation = (Array.isArray(state?.history) ? state.history : []).some(chatTurn)
            || (isLocalTabActive && activeChatVisible);
        if (keepExecutionSurface && localHasConversation) {
            if (assistantBusy) window.dispatchEvent(new CustomEvent(EVENT_NEW_TASK_WIZARD_BLOCKED));
            return;
        }
        // One draft slot. Move the follow-up out so the new task starts empty.
        const coverLocalConversation = isLocalTabActive && (localHasConversation || assistantBusy);
        if (coverLocalConversation) {
            if (!composerStashRef.current) {
                composerStashRef.current = {
                    text: inputRef.current?.value ?? inputValue,
                    attachments: [...(pendingAttachments ?? [])],
                    selectedPaths: [...(selectedFilePaths ?? [])],
                };
                setTaskConfigDraft(defaultTaskDraft());
                setTaskConfigError("");
                clearComposerDraft({ clearAttachments: true });
            }
            setWizardOverlay(true);
            saveTabState(localTab.id, { newTaskWizard: true });
            applySeed();
            focusGuide();
            return;
        }
        if (!isLocalTabActive) {
            setWizardOverlay(true);
            saveTabState(localTab.id, { newTaskWizard: true });
            applySeed();
            focusGuide();
            return;
        }
        setQueueInteractionStarted(false);
        setQueueEditDraftActive(false);
        setEditingEntryId(null);
        clearComposerDraft({ clearAttachments: true });
        saveTabState(localTab.id, { newTaskWizard: true });
        applySeed();
        focusGuide();
    }, [activeChatVisible, assistantBusy, clearComposerDraft, clearGuideDraft, getTabState, getTabs, guideInputRef, inputRef, inputValue, isLocalTabActive, keepExecutionSurface, localGuideVisible, pendingAttachments, saveTabState, selectedFilePaths, setEditingEntryId, setQueueEditDraftActive, setQueueInteractionStarted]);
    useEffect(() => {
        const handler = (event: Event) => {
            const detail = (event as CustomEvent<NewTaskWizardSeed | undefined>).detail;
            const expertId = String(detail?.expertId || "").trim();
            const workflowId = String(detail?.workflowTemplateId || "").trim();
            pendingWizardSeedRef.current = (expertId || workflowId) ? {
                expertId: expertId || null,
                expertName: detail?.expertName ?? null,
                workflowTemplateId: workflowId || null,
                latexTemplateId: detail?.latexTemplateId ?? null,
                latexTemplateName: detail?.latexTemplateName ?? null,
            } : null;
            openNewTaskWizard();
        };
        window.addEventListener(EVENT_OPEN_NEW_TASK_WIZARD, handler);
        return () => window.removeEventListener(EVENT_OPEN_NEW_TASK_WIZARD, handler);
    }, [openNewTaskWizard]);
    useEffect(() => {
        let stopped = false;
        let ticking = false;
        const tick = async () => {
            if (stopped || ticking) return;
            ticking = true;
            try {
                await dispatchDueRunWindows((task) => launchRunWindowTask(task, lang || "zh-Hans"));
            } catch (err) {
                console.warn("[run-window] dispatch failed", err);
            } finally {
                ticking = false;
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, 15000);
        const onFocus = () => { void tick(); };
        window.addEventListener("focus", onFocus);
        return () => {
            stopped = true;
            window.clearInterval(timer);
            window.removeEventListener("focus", onFocus);
        };
    }, [lang]);
    // The startup guide is the new-task page even before the sidebar button.
    // A live conversation is not: leaving the guide drops the marker so the
    // next message stays in that chat.
    useEffect(() => {
        if (!isLocalTabActive || wizardOverlay) return;
        const marked = !!getTabState(activeTab.id)?.newTaskWizard;
        if (localGuideVisible) {
            if (!marked) saveTabState(activeTab.id, { newTaskWizard: true });
            return;
        }
        if (marked) saveTabState(activeTab.id, { newTaskWizard: false });
    }, [activeTab.id, getTabState, isLocalTabActive, localGuideVisible, saveTabState, wizardOverlay]);
    useEffect(() => {
        setNewTaskCoverHeld(wizardOverlay);
        return () => setNewTaskCoverHeld(false);
    }, [wizardOverlay]);

    const dismissWizardOverlay = useCallback(() => {
        const covered = composerStashRef.current;
        composerStashRef.current = null;
        setWizardOverlay(false);
        retireLocalWizardFlag();
        if (!covered) return;
        showComposerText?.(covered.text);
        replacePendingAttachments?.(covered.attachments);
        if (covered.selectedPaths.length) replaceSelectedFilePaths?.(covered.selectedPaths);
    }, [replacePendingAttachments, replaceSelectedFilePaths, retireLocalWizardFlag, showComposerText]);

    const taskConfig = useMemo<TaskConfigPanelModel>(() => ({
        draft: taskConfigDraft,
        onDraftChange: setTaskConfigDraft,
        experts: taskConfigExperts,
        workflows: taskConfigWorkflows,
        latexTemplates: taskConfigLatexTemplates,
        onPrepareLatexTemplates: loadTaskConfigLatexTemplates,
        cloudWorkspaces: taskConfigCloudWorkspaces,
        recentLocalPaths: taskConfigRecentPaths,
        onBrowseLocal: handleTaskConfigBrowseLocal,
        onCreateCloud: handleTaskConfigCreateCloud,
        onRenameCloud: handleTaskConfigRenameCloud,
        // The cover creates a different task, so the running turn's lock does
        // not disable its chips — including the paint before the local tab is active.
        disabled: inputLocked && !wizardOverlay,
        sending: taskConfigSending,
        error: taskConfigError,
        defaultExpanded: isNewTaskWizardTabActive(),
    }), [
        taskConfigDraft, taskConfigExperts, taskConfigWorkflows, taskConfigLatexTemplates, loadTaskConfigLatexTemplates, taskConfigCloudWorkspaces, taskConfigRecentPaths,
        handleTaskConfigBrowseLocal, handleTaskConfigCreateCloud, handleTaskConfigRenameCloud, inputLocked, wizardOverlay, taskConfigSending, taskConfigError,
        isNewTaskWizardTabActive,
    ]);

    return { taskConfig, handleSendWithTaskConfig, handleWelcomePromptSendWithTaskConfig, wizardOverlay, dismissWizardOverlay };
}
