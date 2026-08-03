import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type React from "react";
import { EVENT_OPEN_CREATE_CODING_TASK, type OpenCreateCodingTaskDetail } from "../../constants/events";
import type { Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { AssistantInputComposer } from "./AssistantInputComposer";
import { AssistantPinnedNewsCards } from "./AssistantPinnedNewsCards";
import {
    WelcomePromptParamDialog,
    type WelcomeCodingSubmitEnv,
    type WelcomePromptParamAction,
    type WelcomePromptParamSubmitMode,
} from "./WelcomePromptParamDialog";
import { getComposeActionPlaceholder, type ComposeAction, type FireSlashCommand, type PlusMenuActionId } from "./composeAction";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";
import { CODING_TASK_COMMAND_MAX_LEN, type PureCodingAgentMode } from "./codingTaskMode";
import {
    getWelcomeOpsPrompt,
    getWelcomeOpsPrompts,
    SCENARIO_TABS,
    type WelcomeOpsMode,
    type WelcomePrompt,
} from "./welcomeScenarioTasks";
import {
    matchWelcomeTasksFromClipboard,
    pickClipboardPrefillLabel,
    type WelcomeClipboardHit,
} from "./welcomeClipboardSuggest";
import { resolveWelcomeQuickHints } from "./welcomeQuickHints";
import {
    customTemplateToWelcomePrompt,
    deleteWelcomeCustomTemplate,
    filterWelcomeRecentForQuickAccess,
    importWelcomeCustomTemplates,
    loadWelcomeCloudRevision,
    loadWelcomeCustomTemplates,
    loadWelcomeRecentEntries,
    loadWelcomeUserRole,
    moveWelcomeCustomTemplate,
    previewWelcomeTemplatesImport,
    recordWelcomeRecent,
    renameWelcomeCustomTemplate,
    resolveWelcomeDefaultTab,
    resolveWelcomeRecentPrompts,
    saveWelcomeCloudRevision,
    saveWelcomeCustomTemplate,
    saveWelcomeUserRole,
    stringifyWelcomeTemplatesExport,
    touchWelcomeCustomTemplate,
    welcomePromptKey,
    welcomeTemplatesExportFilename,
    WELCOME_CUSTOM_TEMPLATES_MAX,
    WELCOME_ROLE_DEFAULT_TAB,
    type WelcomeCustomTemplate,
    type WelcomeRecentEntry,
    type WelcomeStoredCodingEnv,
    type WelcomeTemplatesImportPreview,
    type WelcomeUserRole,
} from "./welcomeTaskMemory";
import { WelcomeTemplatesImportPreviewPanel } from "./WelcomeTemplatesImportPreview";
import {
	UpdateLocalStartMenuTemplates,
    WelcomeSyncDelete,
    WelcomeSyncPull,
    WelcomeSyncPush,
    WelcomeSyncStatus,
} from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";

// Keep standalone IM shortcuts in the desktop process. This is deliberately
// separate from cloud sync: LANXIN local mode must not depend on Hub.
type LocalStartMenuTemplateSnapshot = Array<{
    title: string;
    body: string;
    agentMode?: "coding_dev" | "remote_coding_dev";
    remoteSafety?: "diagnosis";
    codingEnv?: {
        workingDir?: string;
        remote?: { host?: string; port?: number; user?: string; workDir?: string };
    };
}>;

let pendingLocalStartMenuSnapshot: LocalStartMenuTemplateSnapshot | null = null;
let localStartMenuSyncRunning = false;
let localStartMenuSyncRetryTimer: ReturnType<typeof setTimeout> | null = null;
let localStartMenuSyncRetryDelayMs = 250;
const LOCAL_STARTMENU_SYNC_RETRY_MAX_MS = 5_000;

function localStartMenuSnapshot(templates: WelcomeCustomTemplate[]): LocalStartMenuTemplateSnapshot {
    // Deliberately enumerate safe fields. `WelcomeStoredCodingEnv.remote.password`
    // stays in browser storage and can never enter the Wails payload.
    return templates.map((t) => ({
        title: t.title,
        body: t.body,
        agentMode: t.agentMode,
        remoteSafety: t.remoteSafety,
        codingEnv: t.codingEnv ? {
            workingDir: t.codingEnv.workingDir,
            remote: t.codingEnv.remote ? {
                host: t.codingEnv.remote.host,
                port: t.codingEnv.remote.port,
                user: t.codingEnv.remote.user,
                workDir: t.codingEnv.remote.workDir,
            } : undefined,
        } : undefined,
    }));
}

export function syncLocalStartMenuTemplates(templates: WelcomeCustomTemplate[]): void {
    pendingLocalStartMenuSnapshot = localStartMenuSnapshot(templates);
    if (localStartMenuSyncRetryTimer !== null) {
        clearTimeout(localStartMenuSyncRetryTimer);
        localStartMenuSyncRetryTimer = null;
    }
    if (localStartMenuSyncRunning) return;
    localStartMenuSyncRunning = true;

    const flush = async () => {
        let failedSnapshot: LocalStartMenuTemplateSnapshot | null = null;
        try {
            // Coalesce any number of saves/deletes into ordered snapshots. If an
            // edit occurs while a Wails call is pending, its newer snapshot is
            // sent immediately afterwards, so an old response cannot win.
            while (pendingLocalStartMenuSnapshot) {
                const snapshot = pendingLocalStartMenuSnapshot;
                pendingLocalStartMenuSnapshot = null;
                try {
                    await UpdateLocalStartMenuTemplates(snapshot as main.LocalStartMenuTemplate[]);
                } catch (error) {
                    failedSnapshot = snapshot;
                    throw error;
                }
            }
        } catch {
            // Do not drop a just-saved common task if Wails is temporarily
            // unavailable during startup. Keep the newest coalesced snapshot
            // and retry with a bounded backoff; a newer edit replaces it.
            // Preserve a newer edit if it arrived while the failed call was in
            // flight; otherwise put the failed snapshot back for retry.
            if (!pendingLocalStartMenuSnapshot && failedSnapshot) {
                pendingLocalStartMenuSnapshot = failedSnapshot;
            }
            if (pendingLocalStartMenuSnapshot && localStartMenuSyncRetryTimer === null) {
                const delay = localStartMenuSyncRetryDelayMs;
                localStartMenuSyncRetryDelayMs = Math.min(
                    localStartMenuSyncRetryDelayMs * 2,
                    LOCAL_STARTMENU_SYNC_RETRY_MAX_MS,
                );
                localStartMenuSyncRetryTimer = setTimeout(() => {
                    localStartMenuSyncRetryTimer = null;
                    if (pendingLocalStartMenuSnapshot && !localStartMenuSyncRunning) {
                        localStartMenuSyncRunning = true;
                        void flush();
                    }
                }, delay);
            }
        } finally {
            localStartMenuSyncRunning = false;
            if (!pendingLocalStartMenuSnapshot) {
                localStartMenuSyncRetryDelayMs = 250;
            }
            if (pendingLocalStartMenuSnapshot) {
                // A retry timer is intentionally allowed to own the next try.
                // Without it, a persistent IPC failure would spin a hot loop.
                if (localStartMenuSyncRetryTimer === null) {
                    localStartMenuSyncRunning = true;
                    void flush();
                }
            }
        }
    };
    void flush();
}
import {
    classifyWelcomeCloudError,
    formatWelcomeCloudUpdatedAt,
    loadWelcomeCloudAutoSync,
    parseWelcomeCloudStatus,
    saveWelcomeCloudAutoSync,
    shouldAutoPushWelcomeCloud,
    shouldPushWelcomeFromStorageAfterPull,
    welcomeCloudConflictPhaseLabel,
    welcomeCloudConflictResolveButtonLabel,
    welcomeCloudErrorMessage,
    welcomeCloudLocalFingerprint,
    welcomeCloudPayloadText,
    welcomeCloudStatusLabel,
    welcomeCloudUserNote,
    WELCOME_CLOUD_AUTO_SYNC_DEBOUNCE_MS,
    type WelcomeCloudConflictPhase,
} from "./welcomeCloudSync";

/** Open/create pure coding task via sidebar (optionally auto-create with env). */
function requestCreateCodingTask(
    mode: PureCodingAgentMode,
    name: string,
    codingEnv?: WelcomeCodingSubmitEnv,
) {
    const command = (name || "").slice(0, CODING_TASK_COMMAND_MAX_LEN);
    if (!command.trim()) return;
    const detail: OpenCreateCodingTaskDetail = {
        mode,
        name: command,
        workingDir: codingEnv?.workingDir,
        remote: codingEnv?.remote,
        remoteSafety: codingEnv?.remoteSafety,
        autoCreate: codingEnv?.autoCreate === true,
    };
    window.dispatchEvent(new CustomEvent(EVENT_OPEN_CREATE_CODING_TASK, { detail }));
}

/** Multi-path professional icons (24×24) for scenario cards — not single-stroke “AI slop” glyphs. */
function WelcomePromptIcon({ name, color }: { name: string; color: string }) {
    const s = { fill: "none", stroke: color, strokeWidth: 1.55, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    // Keys must stay in sync with WELCOME_PROMPT_ICON_NAMES (welcomeScenarioCatalogGuards).
    const paths: Record<string, React.ReactNode> = {
        ppt: (<><rect {...s} x="4" y="4" width="16" height="12" rx="1.5" /><path {...s} d="M8 20h8" /><path {...s} d="M12 16v4" /><path {...s} d="M8 8h4v4H8z" /><path {...s} d="M14 9h3M14 12h2" /></>),
        plan: (<><path {...s} d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" /><path {...s} d="M14 3v4h4" /><path {...s} d="M9 11h7M9 14h7M9 17h4" /></>),
        contract: (<><path {...s} d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" /><path {...s} d="M14 3v4h4" /><path {...s} d="m9 14 1.5 1.5L14 12" /><path {...s} d="M9 18h6" /></>),
        code: (<><path {...s} d="m8 8-3.5 4L8 16" /><path {...s} d="m16 8 3.5 4L16 16" /><path {...s} d="m13.2 6-2.4 12" /></>),
        bug: (<><path {...s} d="M9 9v5a3 3 0 0 0 6 0V9a3 3 0 0 0-6 0Z" /><path {...s} d="M7 10H4M7 13H4M7 16H5" /><path {...s} d="M17 10h3M17 13h3M17 16h2" /><path {...s} d="m8.5 7.5-1.5-2M15.5 7.5 17 5.5" /><path {...s} d="M12 9v8" /></>),
        docker: (<><path {...s} d="M3 13h18" /><path {...s} d="M6 13V10h2.5v3M10 13V10h2.5v3M14 13V10h2.5v3" /><path {...s} d="M10 10V7h2.5v3M6 10V7h2.5v3" /><path {...s} d="M5 16c1.2 2.8 4.5 4 8.5 3 2.2-.6 3.8-1.8 4.8-3.2" /></>),
        server: (<><rect {...s} x="4" y="4" width="16" height="6" rx="1.2" /><rect {...s} x="4" y="14" width="16" height="6" rx="1.2" /><path {...s} d="M8 7h.01M8 17h.01" /><path {...s} d="M12 7h4M12 17h4" /></>),
        install: (<><path {...s} d="M12 4v10" /><path {...s} d="m8 11 4 3 4-3" /><path {...s} d="M5 18h14" /><path {...s} d="M7 18v2h10v-2" /></>),
        deploy: (<><path {...s} d="M12 4v9" /><path {...s} d="m8 9 4-5 4 5" /><path {...s} d="M6 15h12" /><path {...s} d="m7 15 5 5 5-5" /></>),
        search: (<><circle {...s} cx="11" cy="11" r="6" /><path {...s} d="m16 16 3.5 3.5" /><path {...s} d="M8.5 11h5M11 8.5v5" /></>),
        translate: (<><path {...s} d="M4 5h7M7.5 5v2.5" /><path {...s} d="M5.5 11c1.5 2 3.5 3.5 5.5 4.5" /><path {...s} d="M11 5c-.8 2.5-2.5 4.5-4.5 6" /><path {...s} d="m14 10 3 9M15.2 15.5h4.3" /><path {...s} d="M18 7V5h-3" /></>),
        chart: (<><path {...s} d="M4 19V10M9 19V6M14 19v-7M19 19V8" /><path {...s} d="M3 19h18" /></>),
        award: (<><path {...s} d="m12 3 1.9 3.9 4.3.6-3.1 3 .7 4.3L12 12.8 8.2 14.8l.7-4.3-3.1-3 4.3-.6L12 3Z" /><path {...s} d="M9 16v4l3-1.5L15 20v-4" /></>),
        write: (<><path {...s} d="M5 19h4L19 9l-4-4L5 15v4Z" /><path {...s} d="m13.5 6.5 4 4" /><path {...s} d="M14 19h5" /></>),
        mail: (<><rect {...s} x="3.5" y="5.5" width="17" height="13" rx="1.5" /><path {...s} d="m3.5 7 8.5 6.5L20.5 7" /></>),
        meeting: (<><rect {...s} x="4" y="4" width="16" height="16" rx="1.5" /><path {...s} d="M8 9h8M8 12h8M8 15h5" /></>),
        knowledge: (<><path {...s} d="M6 4h8l4 4v12H6V4Z" /><path {...s} d="M14 4v4h4" /><path {...s} d="M9 12h7M9 15h7M9 18h4" /></>),
        qa: (<><path {...s} d="M6 5h12v8H10l-4 3.5V5Z" /><path {...s} d="M10 9.5h4" /><path {...s} d="M12 9.5v.2a2 2 0 0 1-1.2 1.8V12.2" /><circle {...s} cx="12" cy="14.2" r="0.6" fill={color} stroke="none" /></>),
        checklist: (<><path {...s} d="m5 7 1.5 1.5L9.5 5.5" /><path {...s} d="M12 7h7" /><path {...s} d="m5 12 1.5 1.5L9.5 10.5" /><path {...s} d="M12 12h7" /><path {...s} d="m5 17 1.5 1.5L9.5 15.5" /><path {...s} d="M12 17h7" /></>),
        workflow: (<><rect {...s} x="3.5" y="4" width="6" height="5" rx="1" /><rect {...s} x="14.5" y="15" width="6" height="5" rx="1" /><path {...s} d="M9.5 6.5h2.8a3 3 0 0 1 3 3V15" /><path {...s} d="M14.5 17.5H12a3 3 0 0 1-3-3V9" /></>),
        form: (<><rect {...s} x="5" y="3.5" width="14" height="17" rx="1.5" /><path {...s} d="M8 8h8M8 12h8M8 16h5" /></>),
        schedule: (<><rect {...s} x="4" y="5" width="16" height="15" rx="1.5" /><path {...s} d="M8 3.5v3M16 3.5v3M4 10h16" /><path {...s} d="M8 14h2M12 14h2M8 17h2" /></>),
        strategy: (<><path {...s} d="M4 19h16" /><path {...s} d="M6 19V12l3-3 3 3 5-6" /><path {...s} d="M15 6h3v3" /></>),
        review: (<><path {...s} d="M6 3.5h9l4 4V20a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1Z" /><path {...s} d="M15 3.5V8h4" /><path {...s} d="M8 12h8M8 15h5" /><path {...s} d="m14 17 1.2 1.2L18 15.5" /></>),
        monitor: (<><path {...s} d="M4 16h3l2-7 2.5 11 2-5h5" /><path {...s} d="M3 19h18" /></>),
        diagram: (<><rect {...s} x="3.5" y="3.5" width="6" height="4.5" rx="1" /><rect {...s} x="14.5" y="3.5" width="6" height="4.5" rx="1" /><rect {...s} x="3.5" y="16" width="6" height="4.5" rx="1" /><rect {...s} x="14.5" y="16" width="6" height="4.5" rx="1" /><path {...s} d="M9.5 5.8h5" /><path {...s} d="M6.5 8v3.5h11V8" /><path {...s} d="M6.5 16v-4.5M17.5 16v-4.5" /></>),
        target: (<><circle {...s} cx="12" cy="12" r="8" /><circle {...s} cx="12" cy="12" r="4.5" /><circle {...s} cx="12" cy="12" r="1.3" fill={color} stroke="none" /></>),
        users: (<><circle {...s} cx="9" cy="8" r="2.5" /><path {...s} d="M3.5 19c.7-3 2.7-4.5 5.5-4.5S14 16 14.5 19" /><circle {...s} cx="17" cy="9" r="2.1" /><path {...s} d="M15.5 14.5c1.8.3 3.2 1.4 3.8 3.5" /></>),
        shield: (<><path {...s} d="M12 3.5 19 6v5c0 4.5-2.9 7.8-7 9.5-4.1-1.7-7-5-7-9.5V6l7-2.5Z" /><path {...s} d="m9 12 2 2 4-4.5" /></>),
        spark: (<><path {...s} d="m12 3 1.2 4.2L17.5 8.5 13.2 9.8 12 14l-1.2-4.2L6.5 8.5l4.3-1.3L12 3Z" /><path {...s} d="m6 15 .7 2.2L9 18l-2.3.6L6 20.8l-.7-2.2L3 18l2.3-.8L6 15Z" /><path {...s} d="m17 15 .7 2.2 2.3.8-2.3.8-.7 2.2-.7-2.2-2.3-.8 2.3-.8.7-2.2Z" /></>),
    };
    return (
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true" style={{ flexShrink: 0, marginTop: 1 }}>
            {paths[name] || paths.plan}
        </svg>
    );
}

const STORAGE_KEY = "maclaw:welcome-scenario-tab";
/** Retired pre-scenario industry tab key (purged once on welcome mount). */
const RETIRED_INDUSTRY_TAB_KEY = "maclaw:welcome-industry-tab";
const SCENARIO_TAB_IDS = new Set(SCENARIO_TABS.map(tab => tab.id));
const SCENARIO_TAB_BY_ID = new Map(SCENARIO_TABS.map(tab => [tab.id, tab]));
const isScenarioTabId = (value: string | null): value is string => !!value && SCENARIO_TAB_IDS.has(value);

/** Max width for the main content column (input, tabs, cards). */
const CONTENT_MAX_WIDTH = "720px";

const ROLE_LABELS_ZH: Record<WelcomeUserRole, string> = {
    auto: "自动",
    dev: "研发",
    ops: "运维",
    business: "经营",
    research: "科研",
    writing: "写作",
    general: "通用",
};

const ROLE_LABELS_EN: Record<WelcomeUserRole, string> = {
    auto: "Auto",
    dev: "Dev",
    ops: "Ops",
    business: "Business",
    research: "Research",
    writing: "Writing",
    general: "General",
};

async function readClipboardTextSafe(): Promise<string> {
    try {
        if (typeof navigator === "undefined" || !navigator.clipboard?.readText) return "";
        const text = await navigator.clipboard.readText();
        return (text || "").trim();
    } catch {
        // Permission denied / not focused / insecure context — ignore.
        return "";
    }
}

// --- Component ---

/** Props subset needed by AssistantInputComposer inside the welcome view. */
export interface WelcomeComposerProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    composeAction?: ComposeAction | null;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
    handleClearInput: () => void;
    handleDragOver: (event: React.DragEvent<HTMLElement>) => void;
    handleDrop: (event: React.DragEvent<HTMLElement>) => void;
    handlePaste: (event: React.ClipboardEvent<HTMLTextAreaElement>) => void;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: React.PointerEvent<HTMLButtonElement>) => void;
    inputLocked: boolean;
    inputRef: React.Ref<HTMLTextAreaElement>;
    inputValue: string;
    isBusy: boolean;
    isSelectionCollapsedAtBoundary: (direction: "up" | "down") => boolean;
    onComposeActionChange?: (action: ComposeAction | null) => void;
    onFireSlashCommand?: (command: FireSlashCommand) => void;
    onInsertTemplate?: (template: string) => void;
    onPlusMenuAction?: (actionId: PlusMenuActionId) => void;
    pendingAttachments: AttachmentInfo[];
    permissionMode?: AssistantPermissionMode;
    showWorkspacePermissionOption?: boolean;
    onPermissionModeChange?: (mode: AssistantPermissionMode) => void;
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeSelectedFile?: (filePath: string) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: React.Dispatch<React.SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
    submittedPrompts?: string[];
    updateInputValue: (value: string) => void;
    voiceInput: UseVoiceInputResult;
}

/** Metadata for post-send "save as template" offers. */
export type WelcomePromptSubmitMeta = {
    title: string;
    tabId?: string;
    /** Built-in scenario key when available. */
    taskKey?: string;
};

interface AssistantWelcomeViewProps {
    lang: string;
    theme: Theme;
    themeMode: "light" | "dark";
    /** Whether the assistant page is visible. Hidden retained panels must not own document-level dialogs. */
    active?: boolean;
    /** Insert filled prompt into the composer (do not send). */
    onPromptSelect: (text: string, meta?: WelcomePromptSubmitMeta) => void;
    /** Insert and immediately send (chat tasks only). */
    onPromptSend?: (text: string, meta?: WelcomePromptSubmitMeta) => void;
    pinnedNews?: ChatMessage[];
    composer: WelcomeComposerProps;
}

export function AssistantWelcomeView({
    lang,
    theme: t,
    themeMode,
    active = true,
    onPromptSelect,
    onPromptSend,
    pinnedNews,
    composer: cp,
}: AssistantWelcomeViewProps) {
    const isZh = !lang?.startsWith("en");

    // Drop retired industry-tab key once (no longer read).
    useEffect(() => {
        try {
            localStorage.removeItem(RETIRED_INDUSTRY_TAB_KEY);
        } catch { /* ignore */ }
    }, []);

    type ParamDialogState = {
        title: string;
        description: string;
        template: string;
        submitMode: WelcomePromptParamSubmitMode;
        taskKey: string;
        tabId: string;
        textEn: string;
        clipboardPrefill?: string;
        clipboardPrefillLabel?: string | null;
        /** Per-template coding env (from "save as favorite"); may include local password. */
        initialCodingEnv?: WelcomeStoredCodingEnv;
        remoteSafety?: "diagnosis";
    };
    const [paramDialog, setParamDialog] = useState<ParamDialogState | null>(null);
    const [recentEntries, setRecentEntries] = useState<WelcomeRecentEntry[]>(() => loadWelcomeRecentEntries());
    const [customTemplates, setCustomTemplates] = useState<WelcomeCustomTemplate[]>(() => loadWelcomeCustomTemplates());
    const [opsMode, setOpsMode] = useState<WelcomeOpsMode>("local");

    useEffect(() => {
        if (!active) setParamDialog(null);
    }, [active]);

    // Initial bridge for templates that existed before this version. Subsequent
    // CRUD paths sync immediately below; this also repairs a missing local file.
    useEffect(() => {
        syncLocalStartMenuTemplates(customTemplates);
    // The bridge is intentionally keyed to the snapshot, not cloud state.
    }, [customTemplates]);
    /** When set, that custom template chip is in rename mode. */
    const [renamingTemplateId, setRenamingTemplateId] = useState<string | null>(null);
    const [renameDraft, setRenameDraft] = useState("");
    const [templateIoNote, setTemplateIoNote] = useState("");
    const [importPreview, setImportPreview] = useState<WelcomeTemplatesImportPreview | null>(null);
    const importFileInputRef = useRef<HTMLInputElement | null>(null);
    const templateIoNoteTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    /** Selected import strategy for the next file pick. */
    const importModeRef = useRef<"merge" | "replace">("merge");
    const [userRole, setUserRole] = useState<WelcomeUserRole>(() => loadWelcomeUserRole());
    const [cloudSync, setCloudSync] = useState<{
        loggedIn: boolean;
        hasDocument: boolean;
        revision: string;
        templateCount: number;
        updatedAt: string;
        busy: boolean;
        unsupported: boolean;
    }>({
        loggedIn: false,
        hasDocument: false,
        revision: loadWelcomeCloudRevision(),
        templateCount: 0,
        updatedAt: "",
        busy: false,
        unsupported: false,
    });
    const [cloudAutoSync, setCloudAutoSync] = useState(() => loadWelcomeCloudAutoSync());
    /** Show one-click conflict resolution strip after push/auto-sync revision conflict. */
    const [cloudConflictOpen, setCloudConflictOpen] = useState(false);
    const [cloudConflictPhase, setCloudConflictPhase] = useState<WelcomeCloudConflictPhase>("idle");
    const cloudBusyRef = useRef(false);
    const cloudConflictOpenRef = useRef(false);
    cloudConflictOpenRef.current = cloudConflictOpen;
    const cloudConflictPhaseRef = useRef<WelcomeCloudConflictPhase>("idle");
    /** Keep phase ref in lock-step with setState so re-entry guards work before re-render. */
    const setConflictPhase = useCallback((phase: WelcomeCloudConflictPhase) => {
        cloudConflictPhaseRef.current = phase;
        setCloudConflictPhase(phase);
    }, []);
    /** After a revision conflict, the next push overwrites cloud without if-match. */
    const forceNextCloudPushRef = useRef(false);
    /** After empty-local / rich-cloud warning, next push proceeds. */
    const confirmEmptyCloudPushRef = useRef(false);
    /** Second click confirms cloud delete. */
    const confirmCloudDeleteRef = useRef(false);
    /** Skip one auto-push cycle after pull apply (avoid immediate re-upload noise). */
    const suppressAutoPushRef = useRef(false);
    const autoPushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    /** Last successfully auto-pushed payload fingerprint (skip unchanged uploads). */
    const lastAutoPushFingerprintRef = useRef("");
    /** Latest cloud status for debounced auto-push without putting busy/count in effect deps. */
    const cloudSyncRef = useRef(cloudSync);
    cloudSyncRef.current = cloudSync;
    /** Latest push options available to debounced auto-sync without stale closures. */
    const pushWelcomeToCloudRef = useRef<(opts?: {
        quiet?: boolean;
        skipEmptyGuard?: boolean;
        force?: boolean;
        fromStorage?: boolean;
        clearConflict?: boolean;
    }) => Promise<boolean>>(async () => false);

    const showTemplateIoNote = useCallback((note: string) => {
        setTemplateIoNote(note);
        if (templateIoNoteTimerRef.current != null) {
            clearTimeout(templateIoNoteTimerRef.current);
        }
        if (!note) return;
        templateIoNoteTimerRef.current = setTimeout(() => {
            templateIoNoteTimerRef.current = null;
            setTemplateIoNote("");
            // Expire double-confirm gestures with the note.
            confirmCloudDeleteRef.current = false;
            confirmEmptyCloudPushRef.current = false;
            // Keep forceNext while the conflict strip is visible so "push again to force"
            // still works after the toast disappears. Cleared on resolve / pull / dismiss.
        }, 6_000);
    }, []);

    useEffect(() => () => {
        if (templateIoNoteTimerRef.current != null) {
            clearTimeout(templateIoNoteTimerRef.current);
        }
        if (autoPushTimerRef.current != null) {
            clearTimeout(autoPushTimerRef.current);
            autoPushTimerRef.current = null;
        }
    }, []);

    const toggleCloudAutoSync = useCallback(() => {
        confirmCloudDeleteRef.current = false;
        confirmEmptyCloudPushRef.current = false;
        setCloudAutoSync((prev) => {
            const next = !prev;
            saveWelcomeCloudAutoSync(next);
            return next;
        });
    }, []);

    const applyCloudStatus = useCallback((
        status: Record<string, unknown> | null | undefined,
        opts?: { keepBusy?: boolean },
    ) => {
        if (!status) return;
        const parsed = parseWelcomeCloudStatus(status);
        if (parsed.unsupported) {
            // Drop stale local if-match so a later upgraded hub is not blocked by an old rev.
            saveWelcomeCloudRevision("");
        } else if (parsed.revision) {
            saveWelcomeCloudRevision(parsed.revision);
        }
        setCloudSync((prev) => ({
            ...prev,
            loggedIn: parsed.loggedIn,
            hasDocument: parsed.hasDocument,
            revision: parsed.unsupported ? "" : (parsed.revision || prev.revision),
            templateCount: parsed.templateCount,
            updatedAt: parsed.updatedAt,
            unsupported: parsed.unsupported,
            // Mid-flight status updates (e.g. pull headers) must not clear busy early.
            busy: opts?.keepBusy ? prev.busy : false,
        }));
    }, []);

    const isWailsWelcomeSyncAvailable = useCallback((): boolean => {
        try {
            const go = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go;
            return typeof go?.main?.App?.WelcomeSyncStatus === "function";
        } catch {
            return false;
        }
    }, []);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            if (!isWailsWelcomeSyncAvailable()) return;
            try {
                const status = await WelcomeSyncStatus({});
                if (!cancelled) applyCloudStatus(status as unknown as Record<string, unknown>);
            } catch {
                if (!cancelled) {
                    setCloudSync((prev) => ({
                        ...prev,
                        loggedIn: false,
                        unsupported: false,
                        busy: false,
                    }));
                }
            }
        })();
        return () => { cancelled = true; };
    }, [applyCloudStatus, isWailsWelcomeSyncAvailable]);
    const [clipboardHits, setClipboardHits] = useState<WelcomeClipboardHit[]>([]);
    const [clipboardSnippet, setClipboardSnippet] = useState("");
    const [clipboardBusy, setClipboardBusy] = useState(false);
    const deferredCodingCreateTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const lastClipboardFingerprintRef = useRef("");
    /** Full clipboard text for param prefill (UI only shows a short snippet). */
    const clipboardFullTextRef = useRef("");

    useEffect(() => () => {
        if (deferredCodingCreateTimerRef.current != null) {
            clearTimeout(deferredCodingCreateTimerRef.current);
            deferredCodingCreateTimerRef.current = null;
        }
    }, []);

    const touchRecent = useCallback((tabId: string, textEn: string) => {
        setRecentEntries(recordWelcomeRecent(tabId, textEn));
    }, []);

    const openPrompt = useCallback((
        prompt: WelcomePrompt,
        tabId: string,
        options?: {
            clipboardPrefill?: string;
            taskKeyOverride?: string;
            initialCodingEnv?: WelcomeStoredCodingEnv;
        },
    ) => {
        // Quick hints, clipboard matches and recent entries bypass the card
        // grid. Keep them in the active remote-ops context rather than
        // accidentally downgrading them to an ordinary local chat prompt.
        const resolvedPrompt = tabId === "ops" ? getWelcomeOpsPrompt(prompt, opsMode) : prompt;
        const text = isZh ? (resolvedPrompt.template || resolvedPrompt.text) : (resolvedPrompt.templateEn || resolvedPrompt.textEn);
        const title = isZh ? resolvedPrompt.text : resolvedPrompt.textEn;
        const description = isZh ? resolvedPrompt.desc : resolvedPrompt.descEn;
        const taskKey = options?.taskKeyOverride
            || (tabId === "custom" ? `custom::${resolvedPrompt.textEn}` : welcomePromptKey(tabId, resolvedPrompt.textEn));
        const submitMode: WelcomePromptParamSubmitMode =
            resolvedPrompt.agentMode === "coding_dev" || resolvedPrompt.agentMode === "remote_coding_dev"
                ? resolvedPrompt.agentMode
                : "chat";
        const clip = (options?.clipboardPrefill || "").trim();
        // Always open the param dialog — never dump raw [placeholder] templates into the composer.
        // Coding cards also collect workdir/SSH here; chat templates collect field values.
        setParamDialog({
            title,
            description,
            template: text,
            submitMode,
            taskKey,
            tabId,
            textEn: resolvedPrompt.textEn,
            clipboardPrefill: clip || undefined,
            clipboardPrefillLabel: clip ? pickClipboardPrefillLabel(text) : null,
            initialCodingEnv: options?.initialCodingEnv,
            remoteSafety: resolvedPrompt.remoteSafety,
        });
    }, [isZh, opsMode]);

    const clipboardScanInFlightRef = useRef(false);
    const clipboardFocusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const refreshClipboardSuggestions = useCallback(async (opts?: { force?: boolean }) => {
        if (clipboardScanInFlightRef.current) return;
        // Avoid background/hidden windows probing the clipboard.
        if (typeof document !== "undefined" && document.hidden && !opts?.force) return;
        clipboardScanInFlightRef.current = true;
        setClipboardBusy(true);
        try {
            const text = await readClipboardTextSafe();
            if (!text) {
                if (opts?.force) {
                    setClipboardHits([]);
                    setClipboardSnippet("");
                    clipboardFullTextRef.current = "";
                    lastClipboardFingerprintRef.current = "";
                }
                return;
            }
            const fingerprint = `${text.length}:${text.slice(0, 80)}:${text.slice(-40)}`;
            if (!opts?.force && fingerprint === lastClipboardFingerprintRef.current) return;
            lastClipboardFingerprintRef.current = fingerprint;
            clipboardFullTextRef.current = text;
            setClipboardSnippet(text.length > 160 ? `${text.slice(0, 160)}…` : text);
            setClipboardHits(matchWelcomeTasksFromClipboard(text, 3));
        } finally {
            clipboardScanInFlightRef.current = false;
            setClipboardBusy(false);
        }
    }, []);

    // Soft auto-scan on mount / focus / tab visible (debounced; never throws).
    useEffect(() => {
        void refreshClipboardSuggestions();
        const scheduleScan = () => {
            if (clipboardFocusTimerRef.current != null) {
                clearTimeout(clipboardFocusTimerRef.current);
            }
            clipboardFocusTimerRef.current = setTimeout(() => {
                clipboardFocusTimerRef.current = null;
                void refreshClipboardSuggestions();
            }, 180);
        };
        const onFocus = () => scheduleScan();
        const onVisibility = () => {
            if (!document.hidden) scheduleScan();
        };
        window.addEventListener("focus", onFocus);
        document.addEventListener("visibilitychange", onVisibility);
        return () => {
            window.removeEventListener("focus", onFocus);
            document.removeEventListener("visibilitychange", onVisibility);
            if (clipboardFocusTimerRef.current != null) {
                clearTimeout(clipboardFocusTimerRef.current);
                clipboardFocusTimerRef.current = null;
            }
        };
    }, [refreshClipboardSuggestions]);

    const handleParamSubmit = useCallback((
        filled: string,
        mode: WelcomePromptParamSubmitMode,
        action: WelcomePromptParamAction,
        codingEnv?: WelcomeCodingSubmitEnv,
    ) => {
        const text = (filled || "").trim();
        // Keep the dialog open if there is nothing to submit.
        if (!text) return;
        const meta = paramDialog;
        setParamDialog(null);
        if (meta && meta.tabId !== "custom") {
            touchRecent(meta.tabId, meta.textEn);
        }
        if (mode === "coding_dev" || mode === "remote_coding_dev") {
            // Defer so the param portal unmounts before create-task dialog / create runs.
            if (deferredCodingCreateTimerRef.current != null) {
                clearTimeout(deferredCodingCreateTimerRef.current);
            }
            deferredCodingCreateTimerRef.current = setTimeout(() => {
                deferredCodingCreateTimerRef.current = null;
                requestCreateCodingTask(mode, text, codingEnv);
            }, 0);
            return;
        }
        const submitMeta: WelcomePromptSubmitMeta | undefined = meta
            ? {
                title: meta.title,
                tabId: meta.tabId,
                taskKey: meta.tabId === "custom" ? undefined : meta.taskKey,
            }
            : undefined;
        if (action === "send" && onPromptSend) {
            onPromptSend(text, submitMeta);
            return;
        }
        onPromptSelect(text, submitMeta);
    }, [paramDialog, onPromptSelect, onPromptSend, touchRecent]);

    const handleSaveTemplate = useCallback((
        filledPrompt: string,
        title: string,
        codingEnv?: WelcomeStoredCodingEnv,
    ) => {
        const meta = paramDialog;
        const { templates, saved } = saveWelcomeCustomTemplate({
            title: title || (isZh ? "未命名模板" : "Untitled"),
            body: filledPrompt,
            sourceKey: meta?.taskKey,
            sourceTabId: meta?.tabId === "custom" ? undefined : meta?.tabId,
            agentMode:
                meta?.submitMode === "coding_dev" || meta?.submitMode === "remote_coding_dev"
                    ? meta.submitMode
                    : undefined,
            remoteSafety: meta?.remoteSafety,
            codingEnv,
        });
        setCustomTemplates(templates);
		syncLocalStartMenuTemplates(templates);
        return !!saved;
    }, [paramDialog, isZh]);

    const openCustomTemplate = useCallback((tpl: WelcomeCustomTemplate) => {
        if (renamingTemplateId === tpl.id) return;
        const templates = touchWelcomeCustomTemplate(tpl.id);
        setCustomTemplates(templates);
        syncLocalStartMenuTemplates(templates);
        const prompt = customTemplateToWelcomePrompt(tpl);
        openPrompt(prompt, "custom", {
            taskKeyOverride: `custom-id::${tpl.id}`,
            initialCodingEnv: tpl.codingEnv,
        });
    }, [openPrompt, renamingTemplateId]);

    const removeCustomTemplate = useCallback((id: string, event: React.MouseEvent) => {
        event.stopPropagation();
        if (renamingTemplateId === id) {
            setRenamingTemplateId(null);
            setRenameDraft("");
        }
        const templates = deleteWelcomeCustomTemplate(id);
        setCustomTemplates(templates);
        syncLocalStartMenuTemplates(templates);
    }, [renamingTemplateId]);

    const beginRenameCustomTemplate = useCallback((
        tpl: WelcomeCustomTemplate,
        event?: React.MouseEvent | React.KeyboardEvent,
    ) => {
        event?.stopPropagation();
        event?.preventDefault();
        setRenamingTemplateId(tpl.id);
        setRenameDraft(tpl.title);
    }, []);

    const commitRenameCustomTemplate = useCallback(() => {
        if (!renamingTemplateId) return;
        const templates = renameWelcomeCustomTemplate(renamingTemplateId, renameDraft);
        setCustomTemplates(templates);
        syncLocalStartMenuTemplates(templates);
        setRenamingTemplateId(null);
        setRenameDraft("");
    }, [renamingTemplateId, renameDraft]);

    const cancelRenameCustomTemplate = useCallback(() => {
        setRenamingTemplateId(null);
        setRenameDraft("");
    }, []);

    const moveCustomTemplate = useCallback((
        id: string,
        direction: "up" | "down",
        event?: React.MouseEvent | React.KeyboardEvent,
    ) => {
        event?.stopPropagation();
        event?.preventDefault();
        const templates = moveWelcomeCustomTemplate(id, direction);
        setCustomTemplates(templates);
        syncLocalStartMenuTemplates(templates);
    }, []);

    const [activeTab, setActiveTab] = useState<string>(() => {
        let saved: string | null = null;
        try {
            saved = localStorage.getItem(STORAGE_KEY);
        } catch { /* ignore */ }
        return resolveWelcomeDefaultTab(
            isScenarioTabId(saved) ? saved : null,
            loadWelcomeUserRole(),
            loadWelcomeRecentEntries(),
        );
    });

    useEffect(() => {
        try {
            localStorage.setItem(STORAGE_KEY, activeTab);
        } catch { /* ignore */ }
    }, [activeTab]);

    const applyUserRole = useCallback((role: WelcomeUserRole) => {
        setUserRole(role);
        saveWelcomeUserRole(role);
        // Switching persona should jump to that role's default tab (auto keeps current
        // unless it is invalid).
        if (role !== "auto") {
            const next = WELCOME_ROLE_DEFAULT_TAB[role];
            if (isScenarioTabId(next)) setActiveTab(next);
        }
    }, []);

    const exportCustomTemplates = useCallback(() => {
        // Full backup is useful even with zero templates (role / recent still pack).
        try {
            const json = stringifyWelcomeTemplatesExport(customTemplates, {
                includeExtras: true,
                userRole,
                recent: recentEntries,
                lastScenarioTab: activeTab,
            });
            const blob = new Blob([json], { type: "application/json;charset=utf-8" });
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = welcomeTemplatesExportFilename();
            a.rel = "noopener";
            document.body.appendChild(a);
            a.click();
            a.remove();
            URL.revokeObjectURL(url);
            showTemplateIoNote(
                isZh
                    ? `已导出备份（模板 ${customTemplates.length} + 角色/最近）`
                    : `Backup exported (${customTemplates.length} template(s) + role/recent)`,
            );
        } catch {
            showTemplateIoNote(isZh ? "导出失败" : "Export failed");
        }
    }, [customTemplates, isZh, userRole, recentEntries, activeTab, showTemplateIoNote]);

    const applyImportedWelcomePayload = useCallback((
        raw: string,
        mode: "merge" | "replace",
        opts?: { fromCloud?: boolean },
    ) => {
        const result = importWelcomeCustomTemplates(raw, {
            mode,
            restoreExtras: true,
        });
        if (result.error) {
            return { ok: false as const, error: result.error };
        }
        // Cloud pull already matches Hub — suppress auto-push echo.
        // Local file import should still auto-upload when auto-sync is on.
        if (opts?.fromCloud) {
            suppressAutoPushRef.current = true;
        }
        setCustomTemplates(result.templates);
		syncLocalStartMenuTemplates(result.templates);
        if (result.restoredExtras) {
            setUserRole(loadWelcomeUserRole());
            setRecentEntries(loadWelcomeRecentEntries());
            try {
                const tab = localStorage.getItem(STORAGE_KEY);
                if (isScenarioTabId(tab)) setActiveTab(tab);
            } catch { /* ignore */ }
        }
        return {
            ok: true as const,
            added: result.added,
            skipped: result.skipped,
            restoredExtras: result.restoredExtras,
            mode,
        };
    }, []);

    const applyImportPreview = useCallback((preview: WelcomeTemplatesImportPreview) => {
        // File/JSON import — allow auto-sync to push the merged result.
        const result = applyImportedWelcomePayload(preview.raw, preview.mode, { fromCloud: false });
        if (!result.ok) {
            showTemplateIoNote(isZh ? "导入失败" : "Import failed");
            setImportPreview(null);
            return;
        }
        const modeLabel = result.mode === "replace"
            ? (isZh ? "替换" : "replace")
            : (isZh ? "合并" : "merge");
        const extras = result.restoredExtras
            ? (isZh ? "，已恢复角色/最近" : ", restored role/recent")
            : "";
        showTemplateIoNote(
            isZh
                ? `${modeLabel}导入：新增 ${result.added}，跳过 ${result.skipped}${extras}`
                : `${modeLabel}: +${result.added}, skipped ${result.skipped}${extras}`,
        );
        setImportPreview(null);
    }, [applyImportedWelcomePayload, isZh, showTemplateIoNote]);

    const importCustomTemplatesFromFile = useCallback((file: File | null) => {
        if (!file) return;
        const mode = importModeRef.current;
        const reader = new FileReader();
        reader.onload = () => {
            const text = typeof reader.result === "string" ? reader.result : "";
            const previewed = previewWelcomeTemplatesImport(text, mode, customTemplates);
            if (!previewed.ok) {
                const msg =
                    previewed.error === "invalid_json" ? (isZh ? "JSON 无效" : "Invalid JSON")
                    : previewed.error === "empty" ? (isZh ? "文件中没有有效模板" : "No valid templates in file")
                    : previewed.error === "unknown_kind" ? (isZh ? "不是 MaClaw 备份文件" : "Not a MaClaw backup file")
                    : (isZh ? "导入失败" : "Import failed");
                showTemplateIoNote(msg);
                setImportPreview(null);
                return;
            }
            setTemplateIoNote("");
            setImportPreview(previewed.preview);
        };
        reader.onerror = () => {
            showTemplateIoNote(isZh ? "读取文件失败" : "Failed to read file");
        };
        reader.readAsText(file, "utf-8");
    }, [isZh, customTemplates, showTemplateIoNote]);

    const pushWelcomeToCloud = useCallback(async (opts?: {
        quiet?: boolean;
        skipEmptyGuard?: boolean;
        force?: boolean;
        /**
         * Read templates/role/recent from localStorage instead of React state.
         * Required after pull-merge where setState has not flushed yet (conflict resolve).
         */
        fromStorage?: boolean;
        /** When false, leave conflict strip open (multi-step resolve). Default true. */
        clearConflict?: boolean;
    }): Promise<boolean> => {
        if (cloudBusyRef.current) return false;
        if (!isWailsWelcomeSyncAvailable()) {
            if (!opts?.quiet) {
                showTemplateIoNote(isZh ? "当前环境不支持云同步" : "Cloud sync unavailable in this environment");
            }
            return false;
        }
        if (cloudSync.unsupported) {
            if (!opts?.quiet) showTemplateIoNote(welcomeCloudUserNote("unsupported", isZh));
            return false;
        }
        const templates = opts?.fromStorage ? loadWelcomeCustomTemplates() : customTemplates;
        const role = opts?.fromStorage ? loadWelcomeUserRole() : userRole;
        const recent = opts?.fromStorage ? loadWelcomeRecentEntries() : recentEntries;
        let tab = activeTab;
        if (opts?.fromStorage) {
            try {
                const saved = localStorage.getItem(STORAGE_KEY);
                if (isScenarioTabId(saved)) tab = saved;
            } catch { /* keep activeTab */ }
        }
        // Guard: empty local overwriting a rich cloud copy needs a second click (manual only).
        if (
            !opts?.skipEmptyGuard
            && !opts?.quiet
            && templates.length === 0
            && cloudSync.hasDocument
            && cloudSync.templateCount > 0
            && !confirmEmptyCloudPushRef.current
            && !forceNextCloudPushRef.current
            && !opts?.force
        ) {
            confirmEmptyCloudPushRef.current = true;
            showTemplateIoNote(
                isZh
                    ? `云端有 ${cloudSync.templateCount} 个模板，本地为空。再点一次上传将覆盖云端。`
                    : `Cloud has ${cloudSync.templateCount} template(s); local is empty. Click upload again to overwrite.`,
            );
            return false;
        }
        cloudBusyRef.current = true;
        setCloudSync((prev) => ({ ...prev, busy: true }));
        // Quiet auto-sync must never inherit the manual "force next" flag — that would
        // silently overwrite cloud after a conflict without an explicit second click.
        const force = opts?.force === true || (!opts?.quiet && forceNextCloudPushRef.current);
        const fp = welcomeCloudLocalFingerprint({
            templates,
            userRole: role,
            recent,
        });
        try {
            const json = stringifyWelcomeTemplatesExport(templates, {
                includeExtras: true,
                userRole: role,
                recent,
                lastScenarioTab: tab,
            });
            const status = await WelcomeSyncPush({
                payload_json: json,
                if_match_revision: force ? "" : (loadWelcomeCloudRevision() || undefined),
            }) as unknown as Record<string, unknown>;
            forceNextCloudPushRef.current = false;
            confirmEmptyCloudPushRef.current = false;
            confirmCloudDeleteRef.current = false;
            applyCloudStatus({ ...status, logged_in: true });
            if (opts?.clearConflict !== false) {
                setCloudConflictOpen(false);
                setConflictPhase("idle");
            }
            // Remember fingerprint so auto-sync won't re-upload the same payload.
            lastAutoPushFingerprintRef.current = fp;
            const count = Number(status.template_count ?? status.templateCount ?? templates.length) || templates.length;
            if (!opts?.quiet) {
                showTemplateIoNote(
                    isZh
                        ? `已上传云端（模板 ${count}）`
                        : `Uploaded to cloud (${count} template(s))`,
                );
            }
            return true;
        } catch (err) {
            const kind = classifyWelcomeCloudError(err);
            const msg = welcomeCloudErrorMessage(err);
            if (kind === "conflict" && !force) {
                setCloudConflictOpen(true);
                setConflictPhase("idle");
                if (opts?.quiet) {
                    // Stop retrying the same payload on every debounce tick.
                    lastAutoPushFingerprintRef.current = fp;
                } else {
                    forceNextCloudPushRef.current = true;
                }
            }
            if (kind === "unsupported") {
                setCloudSync((prev) => ({ ...prev, unsupported: true, hasDocument: false }));
            } else if (kind === "login") {
                setCloudSync((prev) => ({ ...prev, loggedIn: false }));
            }
            // Quiet auto-sync: only surface conflict / login / unsupported.
            if (!opts?.quiet || kind === "conflict" || kind === "login" || kind === "unsupported") {
                const noteKind = kind === "conflict" && force ? "generic" : kind;
                showTemplateIoNote(welcomeCloudUserNote(noteKind, isZh, msg, "push", !!opts?.quiet));
            }
            return false;
        } finally {
            cloudBusyRef.current = false;
            setCloudSync((prev) => (prev.busy ? { ...prev, busy: false } : prev));
        }
    }, [
        customTemplates,
        cloudSync.hasDocument,
        cloudSync.templateCount,
        cloudSync.unsupported,
        userRole,
        recentEntries,
        activeTab,
        isZh,
        applyCloudStatus,
        showTemplateIoNote,
        isWailsWelcomeSyncAvailable,
        setConflictPhase,
    ]);

    pushWelcomeToCloudRef.current = pushWelcomeToCloud;

    // Debounced auto-upload when *local* welcome memory changes.
    // Do not depend on activeTab (browsing tabs shouldn't upload) or cloud busy/count
    // (those update after push and would loop).
    useEffect(() => {
        if (!cloudAutoSync || !cloudSync.loggedIn || cloudSync.unsupported) {
            return;
        }
        if (suppressAutoPushRef.current) {
            suppressAutoPushRef.current = false;
            return;
        }
        // Pause auto-upload while the user is resolving a conflict.
        if (cloudConflictOpenRef.current) {
            return;
        }
        const fingerprint = welcomeCloudLocalFingerprint({
            templates: customTemplates,
            userRole,
            recent: recentEntries,
        });
        if (fingerprint && fingerprint === lastAutoPushFingerprintRef.current) {
            return;
        }
        if (autoPushTimerRef.current != null) {
            clearTimeout(autoPushTimerRef.current);
        }
        autoPushTimerRef.current = setTimeout(() => {
            autoPushTimerRef.current = null;
            if (cloudConflictOpenRef.current) return;
            const cs = cloudSyncRef.current;
            if (!shouldAutoPushWelcomeCloud({
                autoSync: true,
                loggedIn: cs.loggedIn,
                unsupported: cs.unsupported,
                busy: cs.busy || cloudBusyRef.current,
                localTemplateCount: customTemplates.length,
                cloudHasDocument: cs.hasDocument,
                cloudTemplateCount: cs.templateCount,
            })) {
                return;
            }
            const fp = welcomeCloudLocalFingerprint({
                templates: customTemplates,
                userRole,
                recent: recentEntries,
            });
            if (fp && fp === lastAutoPushFingerprintRef.current) {
                return;
            }
            void pushWelcomeToCloudRef.current({ quiet: true, skipEmptyGuard: true });
        }, WELCOME_CLOUD_AUTO_SYNC_DEBOUNCE_MS);
        return () => {
            if (autoPushTimerRef.current != null) {
                clearTimeout(autoPushTimerRef.current);
                autoPushTimerRef.current = null;
            }
        };
    }, [
        cloudAutoSync,
        cloudSync.loggedIn,
        cloudSync.unsupported,
        customTemplates,
        userRole,
        recentEntries,
    ]);

    /** Pull cloud backup and apply immediately (merge by default; Alt/Shift+click = replace). */
    const pullWelcomeFromCloud = useCallback(async (
        mode: "merge" | "replace" = "merge",
        opts?: {
            /** Suppress success toast (used by conflict resolve before push). */
            quiet?: boolean;
            /** When false, leave the conflict strip open during multi-step resolve. Default true. */
            clearConflict?: boolean;
        },
    ): Promise<boolean> => {
        if (cloudBusyRef.current) return false;
        if (!isWailsWelcomeSyncAvailable()) {
            showTemplateIoNote(isZh ? "当前环境不支持云同步" : "Cloud sync unavailable in this environment");
            return false;
        }
        if (cloudSync.unsupported) {
            showTemplateIoNote(welcomeCloudUserNote("unsupported", isZh));
            return false;
        }
        confirmCloudDeleteRef.current = false;
        confirmEmptyCloudPushRef.current = false;
        cloudBusyRef.current = true;
        setCloudSync((prev) => ({ ...prev, busy: true }));
        try {
            const result = await WelcomeSyncPull({}) as unknown as Record<string, unknown>;
            // Keep busy=true until apply finishes so auto-sync cannot interleave.
            applyCloudStatus({ ...result, logged_in: true }, { keepBusy: true });
            const text = welcomeCloudPayloadText(result);
            if (!text) {
                showTemplateIoNote(welcomeCloudUserNote("empty", isZh));
                return false;
            }
            // Validate first so we don't half-apply junk.
            const previewed = previewWelcomeTemplatesImport(text, mode, customTemplates);
            if (!previewed.ok) {
                showTemplateIoNote(welcomeCloudUserNote("invalid", isZh));
                return false;
            }
            const applied = applyImportedWelcomePayload(text, mode, { fromCloud: true });
            if (!applied.ok) {
                showTemplateIoNote(isZh ? "导入失败" : "Import failed");
                return false;
            }
            forceNextCloudPushRef.current = false;
            confirmEmptyCloudPushRef.current = false;
            confirmCloudDeleteRef.current = false;
            if (opts?.clearConflict !== false) {
                setCloudConflictOpen(false);
                setConflictPhase("idle");
            }
            setImportPreview(null);
            // Mark post-import local snapshot as already synced (skip auto-push echo).
            lastAutoPushFingerprintRef.current = welcomeCloudLocalFingerprint({
                templates: loadWelcomeCustomTemplates(),
                userRole: loadWelcomeUserRole(),
                recent: loadWelcomeRecentEntries(),
            });
            if (!opts?.quiet) {
                const modeLabel = mode === "replace"
                    ? (isZh ? "替换" : "replace")
                    : (isZh ? "合并" : "merge");
                const extras = applied.restoredExtras
                    ? (isZh ? "，已恢复角色/最近" : ", restored role/recent")
                    : "";
                showTemplateIoNote(
                    isZh
                        ? `已拉取并${modeLabel}：新增 ${applied.added}，跳过 ${applied.skipped}${extras}`
                        : `Pulled (${modeLabel}): +${applied.added}, skipped ${applied.skipped}${extras}`,
                );
            }
            return true;
        } catch (err) {
            const kind = classifyWelcomeCloudError(err);
            const msg = welcomeCloudErrorMessage(err);
            if (kind === "unsupported") {
                setCloudSync((prev) => ({ ...prev, unsupported: true, hasDocument: false, loggedIn: true }));
            } else if (kind === "login") {
                setCloudSync((prev) => ({ ...prev, loggedIn: false }));
            }
            showTemplateIoNote(welcomeCloudUserNote(kind, isZh, msg, "pull"));
            return false;
        } finally {
            cloudBusyRef.current = false;
            setCloudSync((prev) => (prev.busy ? { ...prev, busy: false } : prev));
        }
    }, [
        customTemplates,
        cloudSync.unsupported,
        isZh,
        applyCloudStatus,
        applyImportedWelcomePayload,
        showTemplateIoNote,
        isWailsWelcomeSyncAvailable,
        setConflictPhase,
    ]);

    const deleteWelcomeCloud = useCallback(async () => {
        if (cloudBusyRef.current) return;
        if (!isWailsWelcomeSyncAvailable()) {
            showTemplateIoNote(isZh ? "当前环境不支持云同步" : "Cloud sync unavailable in this environment");
            return;
        }
        if (cloudSync.unsupported) {
            showTemplateIoNote(welcomeCloudUserNote("unsupported", isZh));
            return;
        }
        if (!cloudSync.loggedIn) {
            showTemplateIoNote(welcomeCloudUserNote("login", isZh));
            return;
        }
        if (!cloudSync.hasDocument) {
            showTemplateIoNote(welcomeCloudUserNote("empty", isZh));
            return;
        }
        if (!confirmCloudDeleteRef.current) {
            confirmCloudDeleteRef.current = true;
            confirmEmptyCloudPushRef.current = false;
            showTemplateIoNote(
                isZh
                    ? "再点一次「删除云端」将永久删除 Hub 上的引导页备份。"
                    : "Click Delete cloud again to permanently remove the Hub backup.",
            );
            return;
        }
        confirmCloudDeleteRef.current = false;
        cloudBusyRef.current = true;
        setCloudSync((prev) => ({ ...prev, busy: true }));
        try {
            const status = await WelcomeSyncDelete({}) as unknown as Record<string, unknown>;
            saveWelcomeCloudRevision("");
            lastAutoPushFingerprintRef.current = "";
            setCloudConflictOpen(false);
            setConflictPhase("idle");
            applyCloudStatus({
                ...status,
                logged_in: true,
                has_document: false,
                template_count: 0,
                revision: "",
            });
            showTemplateIoNote(isZh ? "已删除云端引导页备份" : "Cloud welcome backup deleted");
        } catch (err) {
            const kind = classifyWelcomeCloudError(err);
            const msg = welcomeCloudErrorMessage(err);
            if (kind === "login") {
                setCloudSync((prev) => ({ ...prev, loggedIn: false }));
            } else if (kind === "unsupported") {
                setCloudSync((prev) => ({ ...prev, unsupported: true }));
            }
            showTemplateIoNote(
                isZh
                    ? `删除失败：${msg.slice(0, 80) || "未知错误"}`
                    : `Delete failed: ${msg.slice(0, 80) || "unknown error"}`,
            );
        } finally {
            cloudBusyRef.current = false;
            setCloudSync((prev) => (prev.busy ? { ...prev, busy: false } : prev));
        }
    }, [
        cloudSync.unsupported,
        cloudSync.loggedIn,
        cloudSync.hasDocument,
        isZh,
        applyCloudStatus,
        showTemplateIoNote,
        isWailsWelcomeSyncAvailable,
        setConflictPhase,
    ]);

    /**
     * One-click conflict resolve: merge-pull cloud into local, then upload the result.
     * Clears force-overwrite state so a normal if-match push follows the new revision.
     * Push reads localStorage (fromStorage) because React state may not have flushed yet.
     */
    const resolveWelcomeCloudConflict = useCallback(async () => {
        if (cloudBusyRef.current || cloudConflictPhaseRef.current !== "idle") return;
        forceNextCloudPushRef.current = false;
        confirmEmptyCloudPushRef.current = false;
        confirmCloudDeleteRef.current = false;
        setCloudConflictOpen(true);
        setConflictPhase("pulling");
        showTemplateIoNote(
            isZh ? "正在拉取合并并回传…" : "Merging from cloud, then uploading…",
        );
        const pulled = await pullWelcomeFromCloud("merge", {
            quiet: true,
            clearConflict: false,
        });
        if (!pulled) {
            setCloudConflictOpen(true);
            setConflictPhase("idle");
            return;
        }
        // After merge, storage holds the union — push must not use stale React state.
        lastAutoPushFingerprintRef.current = "";
        setConflictPhase("pushing");
        const useStorage = shouldPushWelcomeFromStorageAfterPull(pulled);
        // Keep conflict strip visible through push; clear only after full success.
        const pushed = await pushWelcomeToCloud({
            skipEmptyGuard: true,
            fromStorage: useStorage,
            clearConflict: false,
        });
        if (pushed) {
            setCloudConflictOpen(false);
            setConflictPhase("idle");
            showTemplateIoNote(
                isZh ? "冲突已解决：已合并并上传云端" : "Conflict resolved: merged and uploaded",
            );
        } else {
            setCloudConflictOpen(true);
            setConflictPhase("idle");
        }
    }, [isZh, showTemplateIoNote, pullWelcomeFromCloud, pushWelcomeToCloud, setConflictPhase]);

    const currentTab = SCENARIO_TAB_BY_ID.get(activeTab) || SCENARIO_TABS[0];
    const visiblePrompts = currentTab.id === "ops"
        ? getWelcomeOpsPrompts(opsMode)
        : currentTab.prompts;
    const recentPrompts = useMemo(() => {
        const resolved = resolveWelcomeRecentPrompts(recentEntries);
        return filterWelcomeRecentForQuickAccess(resolved, customTemplates, 4);
    }, [recentEntries, customTemplates]);
    /** Cap custom chips so the quick row does not dominate the welcome screen. */
    const visibleCustomTemplates = useMemo(
        () => customTemplates.slice(0, Math.min(6, WELCOME_CUSTOM_TEMPLATES_MAX)),
        [customTemplates],
    );
    const quickHints = useMemo(() => resolveWelcomeQuickHints(), []);
    const showQuickHints = !(cp.inputValue || "").trim();
    const roleLabels = isZh ? ROLE_LABELS_ZH : ROLE_LABELS_EN;
    const hasQuickAccess = visibleCustomTemplates.length > 0 || recentPrompts.length > 0;
    /** Always show the quick section header so users can import even with zero templates. */
    const showQuickSection = true;
    const handleScenarioTabKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
        const key = event.key;
        if (!["ArrowRight", "ArrowDown", "ArrowLeft", "ArrowUp", "Home", "End"].includes(key)) return;
        event.preventDefault();

        const lastIndex = SCENARIO_TABS.length - 1;
        const nextIndex =
            key === "Home" ? 0 :
            key === "End" ? lastIndex :
            key === "ArrowRight" || key === "ArrowDown" ? (index + 1) % SCENARIO_TABS.length :
            (index - 1 + SCENARIO_TABS.length) % SCENARIO_TABS.length;
        const nextTabId = SCENARIO_TABS[nextIndex].id;
        setActiveTab(nextTabId);
        requestAnimationFrame(() => {
            const el = document.getElementById(`welcome-tab-${nextTabId}`);
            el?.focus();
            el?.scrollIntoView({ block: "nearest", inline: "nearest" });
        });
    };

    const hasNews = pinnedNews && pinnedNews.length > 0;

    return (
        <div
            role="region"
            aria-label={isZh ? "工作台任务入口" : "Workbench task entry"}
            style={{
                display: "flex",
                flexDirection: "column",
                height: "100%",
                boxSizing: "border-box",
                overflowY: "auto",
            }}
        >
            {/* Pinned news cards pinned to top */}
            {hasNews && (
                <div style={{ flexShrink: 0, padding: "12px 16px 0", display: "flex", justifyContent: "center" }}>
                    <div style={{ width: "100%", maxWidth: "520px" }}>
                        <AssistantPinnedNewsCards messages={pinnedNews} theme={t} />
                    </div>
                </div>
            )}

            {/* Main content centered in remaining space.
                Uses margin:auto instead of justifyContent:center to avoid
                top-clipping when content overflows a short panel. */}
            <div style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                padding: "16px 16px 16px",
                gap: "14px",
                margin: "auto 0",
                flexShrink: 0,
            }}>

            {/* Title — invite the user to pick a starter task */}
            <h2 style={{
                margin: 0,
                fontSize: "13px",
                fontWeight: 600,
                color: t.textMuted,
                textAlign: "center",
                fontFamily: "system-ui, -apple-system, sans-serif",
                letterSpacing: "0.01em",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                gap: "6px",
            }}>
                <WelcomePromptIcon name="checklist" color={t.textMuted} />
                {isZh ? "选择一个任务开始吧！" : "Pick a task to get started!"}
            </h2>

            {/* Toolbar: role + clipboard scan on one row */}
            <div
                data-testid="welcome-toolbar"
                style={{
                    width: "100%",
                    maxWidth: CONTENT_MAX_WIDTH,
                    display: "flex",
                    flexWrap: "wrap",
                    alignItems: "center",
                    justifyContent: "center",
                    gap: 10,
                }}
            >
                <label
                    data-testid="welcome-role-picker"
                    style={{
                        display: "inline-flex",
                        alignItems: "center",
                        gap: 8,
                        fontSize: 11,
                        color: t.textMuted,
                        fontFamily: "system-ui, -apple-system, sans-serif",
                    }}
                >
                    <span>{isZh ? "角色" : "Role"}</span>
                    <select
                        aria-label={isZh ? "工作角色" : "Workbench role"}
                        data-testid="welcome-role-select"
                        value={userRole}
                        onChange={(e) => applyUserRole(e.target.value as WelcomeUserRole)}
                        style={{
                            padding: "4px 8px",
                            borderRadius: 6,
                            border: `1px solid ${t.fieldBorder}`,
                            background: t.fieldBg,
                            color: t.text,
                            fontSize: 12,
                            fontFamily: "system-ui, -apple-system, sans-serif",
                            cursor: "pointer",
                            maxWidth: 160,
                        }}
                    >
                        {(Object.keys(ROLE_LABELS_ZH) as WelcomeUserRole[]).map((role) => (
                            <option key={role} value={role} data-testid={`welcome-role-${role}`}>
                                {roleLabels[role]}
                            </option>
                        ))}
                    </select>
                </label>
                <button
                    type="button"
                    data-testid="welcome-clipboard-refresh"
                    title={isZh ? "根据剪贴板内容推荐任务" : "Suggest tasks from clipboard"}
                    onClick={() => void refreshClipboardSuggestions({ force: true })}
                    disabled={clipboardBusy}
                    style={{
                        padding: "4px 10px",
                        borderRadius: 6,
                        border: `1px solid ${t.fieldBorder}`,
                        background: t.fieldBg,
                        color: t.textMuted,
                        fontSize: 11,
                        cursor: clipboardBusy ? "wait" : "pointer",
                        fontFamily: "system-ui, -apple-system, sans-serif",
                    }}
                >
                    {clipboardBusy
                        ? (isZh ? "识别中…" : "Scanning…")
                        : (isZh ? "剪贴板识别" : "Scan clipboard")}
                </button>
            </div>

            {/* Centered input composer — workbench field, not chat bubble */}
            <div style={{
                width: "100%",
                maxWidth: CONTENT_MAX_WIDTH,
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.inputBarBg,
                // Visible so history autocomplete can paint above the composer.
                overflow: "visible",
            }}>
                <AssistantInputComposer
                    active={active}
                    browseFile={cp.browseFile}
                    canSend={cp.canSend}
                    cancelPending={cp.cancelPending}
                    cancelSession={cp.cancelSession}
                    clearSelectedFile={cp.clearSelectedFile}
                    composeAction={cp.composeAction}
                    exitHistoryBrowsing={cp.exitHistoryBrowsing}
                    finishVoicePointer={cp.finishVoicePointer}
                    handleCancel={cp.handleCancel}
                    handleClearInput={cp.handleClearInput}
                    handleDragOver={cp.handleDragOver}
                    handleDrop={cp.handleDrop}
                    handlePaste={cp.handlePaste}
                    handleSend={cp.handleSend}
                    handleVoiceClick={cp.handleVoiceClick}
                    handleVoicePointerDown={cp.handleVoicePointerDown}
                    handleVoicePointerLeave={cp.handleVoicePointerLeave}
                    inputAreaHeight={null}
                    inputLocked={cp.inputLocked}
                    inputRef={cp.inputRef}
                    inputValue={cp.inputValue}
                    inline={true}
                    isBusy={cp.isBusy}
                    isSelectionCollapsedAtBoundary={cp.isSelectionCollapsedAtBoundary}
                    lang={lang}
                    onComposeActionChange={cp.onComposeActionChange}
                    onFireSlashCommand={cp.onFireSlashCommand}
                    onInsertTemplate={cp.onInsertTemplate}
                    onPlusMenuAction={cp.onPlusMenuAction}
                    pendingAttachments={cp.pendingAttachments}
                    permissionMode={cp.permissionMode}
                    showWorkspacePermissionOption={cp.showWorkspacePermissionOption}
                    onPermissionModeChange={cp.onPermissionModeChange}
                    placeholderText={
                        getComposeActionPlaceholder(cp.composeAction, isZh)
                            || (isZh ? "输入任务或指令…" : "Enter a task or command...")
                    }
                    ready={cp.ready}
                    recallHistory={cp.recallHistory}
                    rememberHistoryEdit={cp.rememberHistoryEdit}
                    removeSelectedFile={cp.removeSelectedFile}
                    resizeInput={cp.resizeInput}
                    selectedFilePaths={cp.selectedFilePaths}
                    setPendingAttachments={cp.setPendingAttachments}
                    showBusySpinner={cp.showBusySpinner}
                    showMemoryUsage={false}
                    showVoiceInput={true}
                    submittedPrompts={cp.submittedPrompts}
                    theme={t}
                    themeMode={themeMode}
                    updateInputValue={cp.updateInputValue}
                    voiceInput={cp.voiceInput}
                />
            </div>

            {/* Empty-input light hints */}
            {showQuickHints && quickHints.length > 0 && (
                <div
                    data-testid="welcome-quick-hints"
                    style={{
                        width: "100%",
                        maxWidth: CONTENT_MAX_WIDTH,
                        display: "flex",
                        flexWrap: "wrap",
                        alignItems: "center",
                        gap: 6,
                    }}
                >
                    <span style={{
                        fontSize: 11,
                        color: t.textMuted,
                        fontFamily: "system-ui, -apple-system, sans-serif",
                    }}>
                        {isZh ? "试试：" : "Try:"}
                    </span>
                    {quickHints.map((hint) => (
                        <button
                            key={hint.id}
                            type="button"
                            data-testid={`welcome-quick-hint-${hint.id}`}
                            onClick={() => openPrompt(hint.prompt, hint.tabId)}
                            style={{
                                padding: "3px 9px",
                                borderRadius: 999,
                                border: `1px dashed ${t.fieldBorder}`,
                                background: "transparent",
                                color: t.textMuted,
                                fontSize: 11,
                                cursor: "pointer",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                        >
                            {isZh ? hint.label : hint.labelEn}
                        </button>
                    ))}
                </div>
            )}

            {/* Clipboard hits only — scan control lives in the toolbar above */}
            {clipboardHits.length > 0 && (
                <div
                    data-testid="welcome-clipboard-suggest"
                    style={{
                        width: "100%",
                        maxWidth: CONTENT_MAX_WIDTH,
                        display: "flex",
                        flexDirection: "column",
                        gap: 8,
                    }}
                >
                    <div style={{
                        fontSize: 11,
                        fontWeight: 600,
                        color: t.textMuted,
                        fontFamily: "system-ui, -apple-system, sans-serif",
                    }}>
                        {isZh ? "剪贴板推荐" : "From clipboard"}
                    </div>
                    {clipboardSnippet && (
                        <p style={{
                            margin: 0,
                            fontSize: 11,
                            lineHeight: 1.4,
                            color: t.textMuted,
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            display: "-webkit-box",
                            WebkitLineClamp: 2,
                            WebkitBoxOrient: "vertical",
                            fontFamily: "system-ui, -apple-system, sans-serif",
                        }}>
                            {clipboardSnippet}
                        </p>
                    )}
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                        {clipboardHits.map((hit) => (
                            <button
                                key={hit.key}
                                type="button"
                                data-testid={`welcome-clipboard-hit-${hit.key}`}
                                title={isZh ? hit.prompt.desc : hit.prompt.descEn}
                                onClick={() => openPrompt(hit.prompt, hit.tabId, {
                                    clipboardPrefill: clipboardFullTextRef.current || undefined,
                                })}
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    gap: 6,
                                    maxWidth: "100%",
                                    padding: "6px 10px",
                                    borderRadius: 8,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.text,
                                    cursor: "pointer",
                                    fontSize: 12,
                                    fontWeight: 500,
                                    lineHeight: 1.3,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                    textAlign: "left",
                                }}
                            >
                                <WelcomePromptIcon name={hit.prompt.icon} color={t.textMuted} />
                                <span style={{
                                    display: "flex",
                                    flexDirection: "column",
                                    gap: 2,
                                    minWidth: 0,
                                }}>
                                    <span style={{
                                        overflow: "hidden",
                                        textOverflow: "ellipsis",
                                        whiteSpace: "nowrap",
                                        maxWidth: 220,
                                    }}>
                                        {isZh ? hit.prompt.text : hit.prompt.textEn}
                                    </span>
                                    <span style={{ fontSize: 10, color: t.textMuted }}>
                                        {isZh ? hit.reason : hit.reasonEn}
                                    </span>
                                </span>
                            </button>
                        ))}
                    </div>
                </div>
            )}

            {/* Quick access: saved templates + recently used + export/import */}
            {showQuickSection && (
                <div
                    data-testid="welcome-quick-access"
                    style={{
                        width: "100%",
                        maxWidth: CONTENT_MAX_WIDTH,
                        display: "flex",
                        flexDirection: "column",
                        gap: 8,
                    }}
                >
                    <div style={{
                        display: "flex",
                        flexWrap: "wrap",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 8,
                    }}>
                        <div style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 8,
                            flexWrap: "wrap",
                            minWidth: 0,
                        }}>
                            <div style={{
                                fontSize: 11,
                                fontWeight: 600,
                                color: t.textMuted,
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? "快捷" : "Quick access"}
                            </div>
                            {(cloudSync.loggedIn || cloudSync.unsupported) && (
                                <span
                                    data-testid="welcome-cloud-status"
                                    title={
                                        cloudSync.unsupported
                                            ? (isZh ? "Hub 需升级后才支持引导页云同步" : "Upgrade Hub for welcome cloud sync")
                                            : cloudSync.hasDocument
                                            ? (isZh
                                                ? `云端已备份 ${cloudSync.templateCount} 个模板${cloudSync.updatedAt ? ` · ${formatWelcomeCloudUpdatedAt(cloudSync.updatedAt, true)}` : ""}`
                                                : `Cloud backup: ${cloudSync.templateCount} template(s)${cloudSync.updatedAt ? ` · ${formatWelcomeCloudUpdatedAt(cloudSync.updatedAt, false)}` : ""}`)
                                            : (isZh ? "已登录 Hub，云端尚无备份" : "Hub signed in · no cloud backup yet")
                                    }
                                    style={{
                                        fontSize: 10,
                                        color: t.textMuted,
                                        fontFamily: "system-ui, -apple-system, sans-serif",
                                        opacity: 0.9,
                                    }}
                                >
                                    {welcomeCloudStatusLabel(cloudSync, isZh)}
                                </span>
                            )}
                        </div>
                        <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                            <button
                                type="button"
                                data-testid="welcome-cloud-auto-sync"
                                aria-pressed={cloudAutoSync}
                                disabled={cloudSync.unsupported}
                                onClick={toggleCloudAutoSync}
                                title={
                                    isZh
                                        ? (cloudAutoSync
                                            ? "自动同步已开：本地变更将防抖上传（不会用空本地覆盖云端）"
                                            : "自动同步已关：点击开启，本地变更后自动上传云端")
                                        : (cloudAutoSync
                                            ? "Auto-sync on: local changes upload after a short debounce (never wipes rich cloud with empty local)"
                                            : "Auto-sync off: click to upload local changes automatically")
                                }
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${cloudAutoSync ? (t.sendBtnBorder || t.fieldBorder) : t.fieldBorder}`,
                                    background: cloudAutoSync ? (t.fieldBg) : t.fieldBg,
                                    color: cloudAutoSync && cloudSync.loggedIn ? t.text : t.textMuted,
                                    fontSize: 11,
                                    fontWeight: cloudAutoSync ? 600 : 500,
                                    cursor: cloudSync.unsupported ? "default" : "pointer",
                                    opacity: cloudSync.unsupported ? 0.55 : 1,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh
                                    ? (cloudAutoSync ? "自动开" : "自动关")
                                    : (cloudAutoSync ? "Auto on" : "Auto off")}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-cloud-push"
                                disabled={cloudSync.busy || cloudSync.unsupported}
                                aria-busy={cloudSync.busy || undefined}
                                onClick={() => {
                                    confirmCloudDeleteRef.current = false;
                                    void pushWelcomeToCloud();
                                }}
                                title={
                                    cloudSync.unsupported
                                        ? (isZh ? "Hub 需升级" : "Upgrade Hub")
                                        : cloudSync.loggedIn
                                        ? (isZh ? "手动上传到 Hub 云端（按账号保存一份）" : "Manual upload to Hub cloud (one copy per account)")
                                        : (isZh ? "需先登录 Hub" : "Hub login required")
                                }
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: cloudSync.loggedIn && !cloudSync.unsupported ? t.text : t.textMuted,
                                    fontSize: 11,
                                    cursor: cloudSync.busy || cloudSync.unsupported ? "default" : "pointer",
                                    opacity: cloudSync.busy || cloudSync.unsupported ? 0.55 : 1,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "上传云端" : "Cloud ↑"}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-cloud-pull"
                                disabled={cloudSync.busy || cloudSync.unsupported}
                                aria-busy={cloudSync.busy || undefined}
                                onClick={(e) => {
                                    const replace = e.altKey || e.shiftKey;
                                    void pullWelcomeFromCloud(replace ? "replace" : "merge");
                                }}
                                title={
                                    cloudSync.unsupported
                                        ? (isZh ? "Hub 需升级" : "Upgrade Hub")
                                        : isZh
                                        ? "从 Hub 拉取并直接合并写入本地（Alt/Shift = 替换写入）"
                                        : "Pull from Hub and merge into local immediately (Alt/Shift = replace)"
                                }
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: cloudSync.hasDocument && !cloudSync.unsupported ? t.text : t.textMuted,
                                    fontSize: 11,
                                    cursor: cloudSync.busy || cloudSync.unsupported ? "default" : "pointer",
                                    opacity: cloudSync.busy || cloudSync.unsupported ? 0.55 : 1,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "拉取云端" : "Cloud ↓"}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-cloud-delete"
                                disabled={cloudSync.busy || cloudSync.unsupported || !cloudSync.hasDocument}
                                aria-busy={cloudSync.busy || undefined}
                                onClick={() => { void deleteWelcomeCloud(); }}
                                title={
                                    cloudSync.unsupported
                                        ? (isZh ? "Hub 需升级" : "Upgrade Hub")
                                        : !cloudSync.hasDocument
                                        ? (isZh ? "云端暂无备份" : "No cloud backup")
                                        : (isZh ? "删除 Hub 上的引导页备份（需点两次确认）" : "Delete Hub welcome backup (click twice to confirm)")
                                }
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: cloudSync.hasDocument && !cloudSync.unsupported ? t.text : t.textMuted,
                                    fontSize: 11,
                                    cursor: cloudSync.busy || cloudSync.unsupported || !cloudSync.hasDocument ? "default" : "pointer",
                                    opacity: cloudSync.busy || cloudSync.unsupported || !cloudSync.hasDocument ? 0.55 : 1,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "删除云端" : "Delete cloud"}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-templates-export"
                                onClick={exportCustomTemplates}
                                title={isZh ? "导出完整备份（模板+角色+最近）" : "Export full backup (templates+role+recent)"}
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.textMuted,
                                    fontSize: 11,
                                    cursor: "pointer",
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "导出" : "Export"}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-templates-import"
                                onClick={() => {
                                    importModeRef.current = "merge";
                                    importFileInputRef.current?.click();
                                }}
                                title={isZh ? "合并导入（保留本地，跳过相同正文）" : "Merge import (keep local, skip same body)"}
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.textMuted,
                                    fontSize: 11,
                                    cursor: "pointer",
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "合并导入" : "Merge"}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-templates-import-replace"
                                onClick={() => {
                                    importModeRef.current = "replace";
                                    importFileInputRef.current?.click();
                                }}
                                title={isZh ? "替换导入（用文件覆盖本地模板列表）" : "Replace import (overwrite local templates)"}
                                style={{
                                    padding: "2px 8px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.textMuted,
                                    fontSize: 11,
                                    cursor: "pointer",
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {isZh ? "替换导入" : "Replace"}
                            </button>
                            <input
                                ref={importFileInputRef}
                                data-testid="welcome-templates-import-input"
                                type="file"
                                accept="application/json,.json"
                                style={{ display: "none" }}
                                onChange={(e) => {
                                    const file = e.target.files?.[0] || null;
                                    importCustomTemplatesFromFile(file);
                                    e.target.value = "";
                                }}
                            />
                        </div>
                    </div>
                    {templateIoNote && (
                        <div
                            data-testid="welcome-templates-io-note"
                            role="status"
                            aria-live="polite"
                            style={{
                                fontSize: 11,
                                color: t.textMuted,
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                        >
                            {templateIoNote}
                        </div>
                    )}
                    {cloudConflictOpen && (
                        <div
                            data-testid="welcome-cloud-conflict"
                            role="status"
                            aria-live="polite"
                            style={{
                                display: "flex",
                                flexWrap: "wrap",
                                alignItems: "center",
                                gap: 8,
                                padding: "8px 10px",
                                borderRadius: 8,
                                border: `1px solid ${t.fieldBorder}`,
                                background: t.fieldBg,
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                        >
                            <span
                                data-testid="welcome-cloud-conflict-phase"
                                style={{ fontSize: 11, color: t.text, flex: "1 1 160px", lineHeight: 1.4 }}
                            >
                                {welcomeCloudConflictPhaseLabel(cloudConflictPhase, isZh)}
                            </span>
                            <button
                                type="button"
                                data-testid="welcome-cloud-conflict-resolve"
                                disabled={cloudSync.busy || cloudSync.unsupported || cloudConflictPhase !== "idle"}
                                aria-busy={cloudConflictPhase !== "idle" || cloudSync.busy || undefined}
                                onClick={() => { void resolveWelcomeCloudConflict(); }}
                                style={{
                                    padding: "4px 10px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.sendBtnBorder || t.fieldBorder}`,
                                    background: t.sendBtnBg || t.fieldBg,
                                    color: t.sendBtnColor || t.text,
                                    fontSize: 11,
                                    fontWeight: 600,
                                    cursor: cloudSync.busy || cloudConflictPhase !== "idle" ? "default" : "pointer",
                                    opacity: cloudSync.busy || cloudConflictPhase !== "idle" ? 0.55 : 1,
                                }}
                            >
                                {welcomeCloudConflictResolveButtonLabel(cloudConflictPhase, isZh)}
                            </button>
                            <button
                                type="button"
                                data-testid="welcome-cloud-conflict-dismiss"
                                disabled={cloudSync.busy || cloudConflictPhase !== "idle"}
                                onClick={() => {
                                    setCloudConflictOpen(false);
                                    setConflictPhase("idle");
                                    forceNextCloudPushRef.current = false;
                                }}
                                style={{
                                    padding: "4px 10px",
                                    borderRadius: 6,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: "transparent",
                                    color: t.textMuted,
                                    fontSize: 11,
                                    cursor: cloudSync.busy || cloudConflictPhase !== "idle" ? "default" : "pointer",
                                }}
                            >
                                {isZh ? "忽略" : "Dismiss"}
                            </button>
                        </div>
                    )}
                    {importPreview && (
                        <WelcomeTemplatesImportPreviewPanel
                            lang={lang}
                            theme={t}
                            preview={importPreview}
                            onConfirm={() => applyImportPreview(importPreview)}
                            onCancel={() => {
                                setImportPreview(null);
                                showTemplateIoNote(isZh ? "已取消导入" : "Import cancelled");
                            }}
                        />
                    )}
                    {!hasQuickAccess && !importPreview && (
                        <div style={{
                            fontSize: 11,
                            color: t.textMuted,
                            fontFamily: "system-ui, -apple-system, sans-serif",
                        }}>
                            {isZh
                                ? "暂无模板。可从任务表单「保存为常用」，或导入 JSON 备份。"
                                : "No templates yet. Save from a task form, or import a JSON backup."}
                        </div>
                    )}
                    <div
                        data-testid="welcome-custom-templates"
                        role="list"
                        aria-label={isZh ? "我的模板与最近任务" : "My templates and recent tasks"}
                        style={{ display: "flex", flexWrap: "wrap", gap: 8 }}
                    >
                        {visibleCustomTemplates.map((tpl) => {
                            const isRenaming = renamingTemplateId === tpl.id;
                            const fullIndex = customTemplates.findIndex((c) => c.id === tpl.id);
                            const canMoveLeft = fullIndex > 0;
                            const canMoveRight = fullIndex >= 0 && fullIndex < customTemplates.length - 1;
                            const chipBtnStyle: React.CSSProperties = {
                                border: "none",
                                borderLeft: `1px solid ${t.fieldBorder}`,
                                background: "transparent",
                                color: t.textMuted,
                                cursor: "pointer",
                                padding: "6px 7px",
                                fontSize: 12,
                                lineHeight: 1,
                            };
                            return (
                                <div
                                    key={tpl.id}
                                    role="listitem"
                                    data-testid={`welcome-custom-chip-${tpl.id}`}
                                    style={{
                                        display: "inline-flex",
                                        alignItems: "center",
                                        maxWidth: "100%",
                                        borderRadius: 999,
                                        border: `1px solid ${t.sendBtnBorder || t.fieldBorder}`,
                                        background: t.fieldBg,
                                        overflow: "hidden",
                                    }}
                                >
                                    {isRenaming ? (
                                        <input
                                            data-testid={`welcome-custom-rename-input-${tpl.id}`}
                                            value={renameDraft}
                                            autoFocus
                                            maxLength={80}
                                            aria-label={isZh ? "重命名模板" : "Rename template"}
                                            onChange={(e) => setRenameDraft(e.target.value)}
                                            onClick={(e) => e.stopPropagation()}
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault();
                                                    commitRenameCustomTemplate();
                                                } else if (e.key === "Escape") {
                                                    e.preventDefault();
                                                    cancelRenameCustomTemplate();
                                                }
                                            }}
                                            onBlur={() => commitRenameCustomTemplate()}
                                            style={{
                                                width: 140,
                                                margin: "4px 6px 4px 10px",
                                                padding: "2px 6px",
                                                borderRadius: 6,
                                                border: `1px solid ${t.fieldBorder}`,
                                                background: t.inputBarBg || t.bg,
                                                color: t.text,
                                                fontSize: 12,
                                                fontFamily: "system-ui, -apple-system, sans-serif",
                                                outline: "none",
                                            }}
                                        />
                                    ) : (
                                        <button
                                            type="button"
                                            data-testid={`welcome-custom-${tpl.id}`}
                                            aria-label={
                                                isZh
                                                    ? `打开模板 ${tpl.title}。F2 或铅笔重命名，Alt+左右键排序`
                                                    : `Open template ${tpl.title}. F2 or pencil to rename, Alt+arrows reorder`
                                            }
                                            title={
                                                isZh
                                                    ? `${tpl.body.slice(0, 100)}\n（单击打开 · F2/✎ 重命名 · Alt+←/→ 排序）`
                                                    : `${tpl.body.slice(0, 100)}\n(Click to open · F2/✎ rename · Alt+←/→ reorder)`
                                            }
                                            // Single click opens. Rename via F2 or ✎ only —
                                            // double-click used to race open+rename and looked like "data lost".
                                            onClick={() => openCustomTemplate(tpl)}
                                            onKeyDown={(e) => {
                                                if (e.key === "F2") {
                                                    beginRenameCustomTemplate(tpl, e);
                                                    return;
                                                }
                                                if (e.altKey && e.key === "ArrowLeft" && canMoveLeft) {
                                                    moveCustomTemplate(tpl.id, "up", e);
                                                    return;
                                                }
                                                if (e.altKey && e.key === "ArrowRight" && canMoveRight) {
                                                    moveCustomTemplate(tpl.id, "down", e);
                                                }
                                            }}
                                            style={{
                                                display: "inline-flex",
                                                alignItems: "center",
                                                gap: 6,
                                                padding: "6px 8px 6px 10px",
                                                border: "none",
                                                background: "transparent",
                                                color: t.text,
                                                cursor: "pointer",
                                                fontSize: 12,
                                                fontWeight: 500,
                                                fontFamily: "system-ui, -apple-system, sans-serif",
                                                maxWidth: 160,
                                            }}
                                        >
                                            <WelcomePromptIcon name="spark" color={t.textMuted} />
                                            <span style={{
                                                overflow: "hidden",
                                                textOverflow: "ellipsis",
                                                whiteSpace: "nowrap",
                                            }}>
                                                {tpl.title}
                                            </span>
                                        </button>
                                    )}
                                    {!isRenaming && (
                                        <button
                                            type="button"
                                            aria-label={isZh ? `重命名 ${tpl.title}` : `Rename ${tpl.title}`}
                                            data-testid={`welcome-custom-rename-${tpl.id}`}
                                            onClick={(e) => beginRenameCustomTemplate(tpl, e)}
                                            style={chipBtnStyle}
                                        >
                                            ✎
                                        </button>
                                    )}
                                    <button
                                        type="button"
                                        aria-label={isZh ? `左移 ${tpl.title}` : `Move ${tpl.title} left`}
                                        data-testid={`welcome-custom-move-up-${tpl.id}`}
                                        disabled={!canMoveLeft}
                                        onClick={(e) => moveCustomTemplate(tpl.id, "up", e)}
                                        style={{
                                            ...chipBtnStyle,
                                            opacity: canMoveLeft ? 1 : 0.35,
                                            cursor: canMoveLeft ? "pointer" : "default",
                                        }}
                                    >
                                        ‹
                                    </button>
                                    <button
                                        type="button"
                                        aria-label={isZh ? `右移 ${tpl.title}` : `Move ${tpl.title} right`}
                                        data-testid={`welcome-custom-move-down-${tpl.id}`}
                                        disabled={!canMoveRight}
                                        onClick={(e) => moveCustomTemplate(tpl.id, "down", e)}
                                        style={{
                                            ...chipBtnStyle,
                                            opacity: canMoveRight ? 1 : 0.35,
                                            cursor: canMoveRight ? "pointer" : "default",
                                        }}
                                    >
                                        ›
                                    </button>
                                    <button
                                        type="button"
                                        aria-label={isZh ? `删除 ${tpl.title}` : `Delete ${tpl.title}`}
                                        data-testid={`welcome-custom-delete-${tpl.id}`}
                                        onClick={(e) => removeCustomTemplate(tpl.id, e)}
                                        style={chipBtnStyle}
                                    >
                                        ×
                                    </button>
                                </div>
                            );
                        })}
                        {recentPrompts.map(({ tabId, prompt, key }) => (
                            <button
                                type="button"
                                role="listitem"
                                key={key}
                                data-testid={`welcome-recent-${key}`}
                                title={isZh ? prompt.desc : prompt.descEn}
                                aria-label={isZh ? `最近：${prompt.text}` : `Recent: ${prompt.textEn}`}
                                onClick={() => openPrompt(prompt, tabId)}
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    gap: "6px",
                                    maxWidth: "100%",
                                    padding: "6px 10px",
                                    borderRadius: "999px",
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.text,
                                    cursor: "pointer",
                                    fontSize: "12px",
                                    fontWeight: 500,
                                    lineHeight: 1.3,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                    textAlign: "left",
                                }}
                            >
                                <WelcomePromptIcon name={prompt.icon} color={t.textMuted} />
                                <span style={{
                                    overflow: "hidden",
                                    textOverflow: "ellipsis",
                                    whiteSpace: "nowrap",
                                    maxWidth: 200,
                                }}>
                                    {isZh ? prompt.text : prompt.textEn}
                                </span>
                            </button>
                        ))}
                    </div>
                </div>
            )}

            {/* Scenario tabs — outer scroll container + inner centering wrapper.
                Using a wrapper div with margin:auto to center tabs when they fit,
                while allowing left-aligned overflow scroll when they don't.
                justify-content:center on overflow would clip left-side tabs. */}
            <div
                role="tablist"
                aria-label={isZh ? "场景分类" : "Scenario categories"}
                className="no-scrollbar"
                style={{
                    width: "100%",
                    maxWidth: CONTENT_MAX_WIDTH,
                    overflowX: "auto",
                    scrollbarWidth: "none",
                }}
            >
                <div style={{
                    display: "flex",
                    flexWrap: "nowrap",
                    gap: "6px",
                    width: "fit-content",
                    margin: "0 auto",
                }}>
                {SCENARIO_TABS.map((tab, index) => {
                    const isActive = tab.id === activeTab;
                        return (
                        <button
                            type="button"
                            key={tab.id}
                            id={`welcome-tab-${tab.id}`}
                            role="tab"
                            aria-selected={isActive}
                            aria-controls={`welcome-tabpanel-${tab.id}`}
                            tabIndex={isActive ? 0 : -1}
                            onClick={() => setActiveTab(tab.id)}
                            onKeyDown={event => handleScenarioTabKeyDown(event, index)}
                            style={{
                                padding: "4px 10px",
                                fontSize: "12px",
                                fontWeight: isActive ? 600 : 400,
                                lineHeight: 1.3,
                                color: isActive ? t.text : t.textMuted,
                                background: isActive ? t.fieldBg : "transparent",
                                border: `1px solid ${isActive ? t.fieldBorder : "transparent"}`,
                                borderRadius: "6px",
                                boxSizing: "border-box",
                                cursor: "pointer",
                                transition: "background 0.12s ease, border-color 0.12s ease, color 0.12s ease",
                                whiteSpace: "nowrap",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                            onMouseEnter={e => {
                                if (!isActive) {
                                    e.currentTarget.style.color = t.text;
                                    e.currentTarget.style.background = t.fieldBg;
                                }
                            }}
                            onMouseLeave={e => {
                                if (!isActive) {
                                    e.currentTarget.style.color = t.textMuted;
                                    e.currentTarget.style.background = "transparent";
                                }
                            }}
                        >
                            {isZh ? tab.label : tab.labelEn}
                        </button>
                    );
                })}
                </div>
            </div>

            {currentTab.id === "ops" && (
                <div
                    aria-label={isZh ? "运维执行位置" : "Ops execution location"}
                    style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: "12px",
                        width: "100%",
                        maxWidth: CONTENT_MAX_WIDTH,
                        padding: "8px 10px",
                        background: t.fieldBg,
                        border: `1px solid ${t.fieldBorder}`,
                        borderRadius: "8px",
                        boxSizing: "border-box",
                    }}
                >
                    <div style={{ minWidth: 0 }}>
                        <div style={{ color: t.text, fontSize: "12px", fontWeight: 600, lineHeight: 1.35 }}>
                            {isZh ? "选择运维任务类型" : "Choose an ops task type"}
                        </div>
                        <div style={{ color: t.textMuted, fontSize: "11px", lineHeight: 1.35, marginTop: "2px" }}>
                            {opsMode === "remote"
                                ? (isZh ? "需填写 SSH 服务器连接信息；首轮仅收集证据，不执行变更。" : "SSH connection details are required; the first turn gathers evidence only.")
                                : (isZh ? "仅分析当前本机提供的日志、配置或现象，不连接服务器。" : "Analyze local evidence only; no server connection is opened.")}
                        </div>
                    </div>
                    <div
                        role="group"
                        aria-label={isZh ? "本地或远程运维" : "Local or remote operations"}
                        style={{
                            display: "inline-flex",
                            flexShrink: 0,
                            padding: "2px",
                            background: t.bg,
                            border: `1px solid ${t.fieldBorder}`,
                            borderRadius: "6px",
                        }}
                    >
                        {([
                            ["local", isZh ? "本地运维" : "Local ops"],
                            ["remote", isZh ? "远程运维" : "Remote ops"],
                        ] as const).map(([mode, label]) => {
                            const selected = opsMode === mode;
                            return (
                                <button
                                    type="button"
                                    key={mode}
                                    aria-pressed={selected}
                                    data-testid={`welcome-ops-mode-${mode}`}
                                    onClick={() => setOpsMode(mode)}
                                    style={{
                                        padding: "4px 8px",
                                        color: selected ? t.sendBtnColor : t.textMuted,
                                        background: selected ? t.sendBtnBg : "transparent",
                                        border: 0,
                                        borderRadius: "4px",
                                        cursor: "pointer",
                                        fontSize: "11px",
                                        fontWeight: selected ? 600 : 500,
                                        lineHeight: 1.35,
                                        fontFamily: "system-ui, -apple-system, sans-serif",
                                        transition: "background 0.12s ease, color 0.12s ease",
                                    }}
                                >
                                    {label}
                                </button>
                            );
                        })}
                    </div>
                </div>
            )}

            {/* Prompt cards */}
            <div
                role="tabpanel"
                id={`welcome-tabpanel-${currentTab.id}`}
                aria-labelledby={`welcome-tab-${currentTab.id}`}
                style={{
                    display: "grid",
                    gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
                    gap: "10px",
                    width: "100%",
                    maxWidth: CONTENT_MAX_WIDTH,
                }}
            >
                {visiblePrompts.map((prompt, promptIndex) => (
                    <button
                        type="button"
                        key={`${currentTab.id}-${prompt.textEn}`}
                        data-agent-mode={prompt.agentMode || undefined}
                        data-testid={prompt.agentMode ? `welcome-coding-card-${prompt.agentMode}-${promptIndex}` : undefined}
                        title={isZh ? prompt.desc : prompt.descEn}
                        aria-label={
                            prompt.agentMode === "coding_dev"
                                ? (isZh ? `${prompt.text}（本地编程）` : `${prompt.textEn} (local coding)`)
                                : prompt.agentMode === "remote_coding_dev"
                                    ? (prompt.remoteSafety === "diagnosis"
                                        ? (isZh ? `${prompt.text}（远程维护）` : `${prompt.textEn} (remote maintenance)`)
                                        : (isZh ? `${prompt.text}（远程编程）` : `${prompt.textEn} (remote coding)`))
                                    : undefined
                        }
                        onClick={() => openPrompt(prompt, currentTab.id)}
                        style={{
                            display: "flex",
                            alignItems: "flex-start",
                            gap: "10px",
                            padding: "10px 12px",
                            background: t.fieldBg,
                            border: `1px solid ${t.fieldBorder}`,
                            borderRadius: "6px",
                            boxSizing: "border-box",
                            cursor: "pointer",
                            textAlign: "left",
                            transition: "border-color 0.12s ease, background 0.12s ease",
                            width: "100%",
                            minWidth: 0,
                            minHeight: "64px",
                        }}
                        onMouseEnter={e => {
                            e.currentTarget.style.borderColor = t.inputBarBorder;
                            e.currentTarget.style.background = t.inputBarBg;
                        }}
                        onMouseLeave={e => {
                            e.currentTarget.style.borderColor = t.fieldBorder;
                            e.currentTarget.style.background = t.fieldBg;
                        }}
                        onFocus={e => {
                            e.currentTarget.style.borderColor = t.sendBtnBorder || t.sendBtnBg;
                        }}
                        onBlur={e => {
                            e.currentTarget.style.borderColor = t.fieldBorder;
                        }}
                    >
                        <WelcomePromptIcon name={prompt.icon} color={t.textMuted} />
                        <div style={{ display: "flex", flexDirection: "column", gap: "3px", minWidth: 0 }}>
                            <span style={{
                                fontSize: "13px",
                                fontWeight: 500,
                                lineHeight: 1.35,
                                color: t.text,
                                overflowWrap: "anywhere",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? prompt.text : prompt.textEn}
                            </span>
                            <span style={{
                                fontSize: "11px",
                                lineHeight: 1.35,
                                color: t.textMuted,
                                overflowWrap: "anywhere",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? prompt.desc : prompt.descEn}
                            </span>
                        </div>
                    </button>
                ))}
            </div>
            </div>

            <WelcomePromptParamDialog
                open={active && !!paramDialog}
                onClose={() => setParamDialog(null)}
                lang={lang}
                theme={t}
                title={paramDialog?.title || ""}
                description={paramDialog?.description}
                template={paramDialog?.template || ""}
                taskKey={paramDialog?.taskKey}
                clipboardPrefill={paramDialog?.clipboardPrefill}
                clipboardPrefillLabel={paramDialog?.clipboardPrefillLabel}
                submitMode={paramDialog?.submitMode || "chat"}
                initialCodingEnv={paramDialog?.initialCodingEnv}
                remoteSafety={paramDialog?.remoteSafety}
                canSend={cp.ready && !cp.inputLocked && !!onPromptSend}
                onSubmit={handleParamSubmit}
                onSaveTemplate={handleSaveTemplate}
            />
        </div>
    );
}
