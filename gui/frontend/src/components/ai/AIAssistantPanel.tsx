import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { OpenFileOrShowInFolder, ShowItemInFolder, SearchProjects, ResumeProject, RenameTask, PinTask, HideTask, GetTTSEnabled, SetTTSEnabled, SelectProjectDir, SetWorkflowWorkingDir } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { ChatMessage, CancelAIAssistantResult, ChatAction, AIAssistantInitStatus, ChatConfirmation, ChatUnfinishedSlot } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { useWorkflowState } from "./useWorkflowState";
import { WorkflowDocPreview } from "./WorkflowDocPreview";
import { useCodePreviewState } from "./useCodePreviewState";
import { CodePreviewPanel, darkCodePreviewTheme, lightCodePreviewTheme } from "./CodePreviewPanel";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { BufferQueuePanel } from "./BufferQueuePanel";

interface AIAssistantPanelStateProps {
    messages: ChatMessage[];
    progressMessages?: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    visualBusy?: boolean;
    ready: boolean;
    initStatus?: AIAssistantInitStatus;
    selectedFilePath?: string;
    selectedFilePaths?: string[];
    submittedPrompts?: string[];
    draftInputValue?: string;
    trialReflectEnabled?: boolean;
    scrollToTopSeq?: number;
    onboardingIncomplete?: boolean;
    showTraceEntry?: boolean;
}

interface AIAssistantPanelActionProps {
    browseFile?: () => Promise<void>;
    clearSelectedFile?: () => void;
    removeSelectedFile?: (index: number) => void;
    sendMessage: (text: string) => Promise<void>;
    sendMessageInBackground?: (text: string) => Promise<void>;
    clearHistory: () => Promise<void>;
    recordSubmittedPrompt?: (text: string) => void;
    setDraftInputValue?: (text: string) => void;
    executeAction: (command: string) => Promise<void>;
    refreshNews: () => void;
    onOpenOnboarding?: () => void;
    cancelSession?: () => Promise<CancelAIAssistantResult>;
    onOpenTutorial?: () => void;
    onTaskPrefsChanged?: () => void;
}

interface AIAssistantPanelWindowProps {
    inline?: boolean;
    maximized?: boolean;
    onToggleMaximize?: () => void;
    onHideWindow?: () => void;
}

interface GroupDiscussionPanelStatus {
    enabled?: boolean;
    discoverable?: boolean;
    profile?: { agent_id?: string; display_name?: string } | null;
    experts?: Array<unknown>;
    discussions?: Array<unknown>;
    pending_invites?: Array<any>;
    active_discussion_count?: number;
    ready_discussion_count?: number;
    waiting_discussion_count?: number;
    stale_discussion_count?: number;
    error?: string;
}

interface GroupDiscussionPanelControl {
    config?: any;
    status?: GroupDiscussionPanelStatus | null;
    onRefreshStatus?: () => void | Promise<void>;
    onPublishProfile?: () => void | Promise<void>;
    onAcceptInvite?: (inviteId: string) => void | Promise<void>;
    onRejectInvite?: (inviteId: string) => void | Promise<void>;
}

interface AIAssistantPanelProps {
    onClose: () => void;
    lang: string; // 'zh-Hans' | 'zh-Hant' | 'en'
    chatFontSize?: number;
    state: AIAssistantPanelStateProps;
    actions: AIAssistantPanelActionProps;
    window?: AIAssistantPanelWindowProps;
    groupDiscussion?: GroupDiscussionPanelControl;
    onThemeModeChange?: (mode: 'light' | 'dark') => void;
    audioInputDeviceId?: string;
    audioOutputDeviceId?: string;
    petVoiceStartSeq?: number;
    petFocusInputSeq?: number;
}

/* Theme definitions */

interface Theme {
    bg: string;
    titleBarBg: string;
    titleBarBorder: string;
    titleText: string;
    text: string;
    textMuted: string;
    inputBarBg: string;
    inputBarBorder: string;
    inputText: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    codeBlockLang: string;
    borderLeft: string;
    responseBorderLeft: string;
    headingColor: string;
    linkColor: string;
    pathColor: string;
    promptColor: string;
    userColor: string;
    divider: string;
    fieldBg: string;
    fieldBorder: string;
    fieldLabel: string;
    errorText: string;
    errorBg: string;
    errorBorder: string;
    emptyHint: string;
    boldColor: string;
    italicColor: string;
    bulletColor: string;
    quoteBorder: string;
    quoteText: string;
    btnColor: string;
    btnBorder: string;
    actionBtnColor: string;
    closeBtnColor: string;
    sendBtnColor: string;
    sendBtnBorder: string;
}

const overlayTheme: Theme = {
    bg: "#eef0f5",
    titleBarBg: "#e2e4ea",
    titleBarBorder: "#c8cad0",
    titleText: "#555",
    text: "#2d2d2d",
    textMuted: "#777",
    inputBarBg: "#ffffff",
    inputBarBorder: "#6366f1",
    inputText: "#222",
    codeBg: "#e4e6ec",
    codeText: "#b5314a",
    codeBlockBg: "#e8eaf0",
    codeBlockBorder: "#c8cad0",
    codeBlockLang: "#999",
    borderLeft: "#c8cad0",
    responseBorderLeft: "#b4b6d4",
    headingColor: "#5558d6",
    linkColor: "#5558d6",
    pathColor: "#059669",
    promptColor: "#5558d6",
    userColor: "#5558d6",
    divider: "#d0d2d8",
    fieldBg: "#e8eaf0",
    fieldBorder: "#c8cad0",
    fieldLabel: "#777",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#999",
    boldColor: "#1a1a1a",
    italicColor: "#333",
    bulletColor: "#888",
    quoteBorder: "#b4b6d4",
    quoteText: "#666",
    btnColor: "#5558d6",
    btnBorder: "#5558d6",
    actionBtnColor: "#777",
    closeBtnColor: "#888",
    sendBtnColor: "#fff",
    sendBtnBorder: "#5558d6",
};

const lightTheme: Theme = {
    bg: "#fafbff",
    titleBarBg: "#f0f1f5",
    titleBarBorder: "#ddd",
    titleText: "#666",
    text: "#333",
    textMuted: "#888",
    inputBarBg: "#f5f6fa",
    inputBarBorder: "#ddd",
    inputText: "#333",
    codeBg: "#f0f0f5",
    codeText: "#c7254e",
    codeBlockBg: "#f5f6fa",
    codeBlockBorder: "#ddd",
    codeBlockLang: "#aaa",
    borderLeft: "#ddd",
    responseBorderLeft: "#d4d4f7",
    headingColor: "#6366f1",
    linkColor: "#6366f1",
    pathColor: "#059669",
    promptColor: "#6366f1",
    userColor: "#6366f1",
    divider: "#e5e7eb",
    fieldBg: "#f5f6fa",
    fieldBorder: "#ddd",
    fieldLabel: "#888",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#aaa",
    boldColor: "#222",
    italicColor: "#444",
    bulletColor: "#999",
    quoteBorder: "#d4d4f7",
    quoteText: "#777",
    btnColor: "#6366f1",
    btnBorder: "#6366f1",
    actionBtnColor: "#888",
    closeBtnColor: "#999",
    sendBtnColor: "#6366f1",
    sendBtnBorder: "#6366f1",
};


const darkTheme: Theme = {
    ...lightTheme,
    bg: "#0b1220",
    titleBarBg: "#111827",
    titleBarBorder: "#334155",
    titleText: "#f1f5f9",
    text: "#f1f5f9",
    textMuted: "#cbd5e1",
    inputBarBg: "#0f172a",
    inputBarBorder: "#334155",
    inputText: "#e5e7eb",
    codeBg: "#0f172a",
    codeText: "#f87171",
    codeBlockBg: "#111827",
    codeBlockBorder: "#334155",
    borderLeft: "#334155",
    responseBorderLeft: "#475569",
    headingColor: "#a5b4fc",
    linkColor: "#93c5fd",
    pathColor: "#34d399",
    promptColor: "#a5b4fc",
    userColor: "#c4b5fd",
    divider: "#1e293b",
    fieldBg: "#111827",
    fieldBorder: "#334155",
    fieldLabel: "#94a3b8",
    emptyHint: "#64748b",
    boldColor: "#f8fafc",
    italicColor: "#cbd5e1",
    bulletColor: "#94a3b8",
    quoteBorder: "#475569",
    quoteText: "#cbd5e1",
    actionBtnColor: "#cbd5e1",
    closeBtnColor: "#cbd5e1",
    sendBtnColor: "#ffffff",
    sendBtnBorder: "#818cf8",
};

const AI_THEME_MODE_STORAGE_KEY = "ai_assistant_theme_mode";
/* Style constants */

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: overlayTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.08)",
};

const maximizedInlineStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 12000,
    display: "flex",
    flexDirection: "column",
    width: "100vw",
    height: "100vh",
    minHeight: 0,
    background: lightTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.12)",
    overflow: "hidden",
};

const dotBase: React.CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    display: "inline-block",
    cursor: "pointer",
};

const baseInputBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid",
    borderRadius: "8px",
    padding: 0,
    fontSize: "14px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "34px",
    minWidth: "36px",
    width: "36px",
    height: "34px",
    flexShrink: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "transform 120ms ease, box-shadow 120ms ease, background 120ms ease, border-color 120ms ease, opacity 120ms ease",
};

type AssistantInputIconName = "paperclip" | "mic" | "cornerDownLeft" | "stop";

function AssistantInputIcon({ name, size = 17 }: { name: AssistantInputIconName; size?: number }) {
    const common = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 2,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
            {name === "paperclip" && (
                <path {...common} d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 1 1-2.83-2.83l8.49-8.48" />
            )}
            {name === "mic" && (
                <>
                    <path {...common} d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                    <path {...common} d="M19 10v2a7 7 0 0 1-14 0v-2" />
                    <path {...common} d="M12 19v3" />
                </>
            )}
            {name === "cornerDownLeft" && (
                <path {...common} d="M9 10 4 15l5 5M20 4v7a4 4 0 0 1-4 4H4" />
            )}
            {name === "stop" && (
                <rect {...common} x="7" y="7" width="10" height="10" rx="1.8" />
            )}
        </svg>
    );
}

function getInputActionButtonStyle(
    t: Theme,
    themeMode: 'light' | 'dark',
    tone: "neutral" | "attach" | "voice" | "voiceHold" | "send" | "cancel",
    disabled = false,
): React.CSSProperties {
    const dark = themeMode === 'dark';
    const palette = {
        neutral: { color: t.textMuted, border: dark ? "rgba(148, 163, 184, 0.28)" : "rgba(99, 102, 241, 0.16)", bg: dark ? "rgba(15, 23, 42, 0.72)" : "rgba(255, 255, 255, 0.86)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.82)" },
        attach: { color: dark ? "#67e8f9" : "#0891b2", border: dark ? "rgba(34, 211, 238, 0.38)" : "rgba(8, 145, 178, 0.28)", bg: dark ? "rgba(8, 145, 178, 0.10)" : "rgba(236, 254, 255, 0.86)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(8,145,178,0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
        voice: { color: dark ? "#cbd5e1" : "#475569", border: dark ? "rgba(148, 163, 184, 0.30)" : "rgba(71, 85, 105, 0.20)", bg: dark ? "rgba(15, 23, 42, 0.74)" : "rgba(248, 250, 252, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.86)" },
        voiceHold: { color: dark ? "#fecaca" : "#dc2626", border: dark ? "rgba(248, 113, 113, 0.56)" : "rgba(220, 38, 38, 0.34)", bg: dark ? "rgba(127, 29, 29, 0.28)" : "rgba(254, 242, 242, 0.96)", shadow: dark ? "0 0 0 2px rgba(248, 113, 113, 0.10), inset 0 1px 0 rgba(255,255,255,0.05)" : "0 0 0 2px rgba(248, 113, 113, 0.12), inset 0 1px 0 rgba(255,255,255,0.9)" },
        send: { color: "#ffffff", border: dark ? "rgba(129, 140, 248, 0.78)" : "rgba(79, 70, 229, 0.72)", bg: dark ? "linear-gradient(180deg, #818cf8 0%, #6366f1 100%)" : "linear-gradient(180deg, #6366f1 0%, #4f46e5 100%)", shadow: dark ? "0 8px 18px rgba(79,70,229,0.28), inset 0 1px 0 rgba(255,255,255,0.18)" : "0 8px 18px rgba(79,70,229,0.20), inset 0 1px 0 rgba(255,255,255,0.22)" },
        cancel: { color: dark ? "#ddd6fe" : "#4f46e5", border: dark ? "rgba(129, 140, 248, 0.56)" : "rgba(79, 70, 229, 0.34)", bg: dark ? "rgba(99, 102, 241, 0.16)" : "rgba(238, 242, 255, 0.94)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(79,70,229,0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
    }[tone];
    return { ...baseInputBtnStyle, color: palette.color, borderColor: palette.border, background: palette.bg, boxShadow: palette.shadow, opacity: disabled ? 0.45 : 1, cursor: disabled ? "default" : "pointer" };
}

const baseWindowControlBtnStyle: React.CSSProperties = {
    width: "36px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    background: "transparent",
    border: "none",
    borderRadius: "4px",
    cursor: "pointer",
    padding: 0,
    lineHeight: 1,
    flexShrink: 0,
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    transition: "background 120ms ease, color 120ms ease",
};


const baseActionBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "none",
    fontSize: "12px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    padding: 0,
    borderRadius: "4px",
    lineHeight: 1,
    minHeight: "28px",
    minWidth: "28px",
    width: "32px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "background 120ms ease, color 120ms ease",
};

const AI_PANEL_STATIC_STYLE_ID = "ai-panel-static-style";
const AI_PANEL_STATIC_STYLE_TEXT = `
    @keyframes blink { 50% { opacity: 0; } }
    @keyframes ai-spinner-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
    @keyframes maclaw-spin { to { transform: rotate(360deg); } }
    .pinned-news-card > div { margin-top: 0 !important; margin-bottom: 0 !important; }
    .ai-window-control:hover { background: var(--ai-window-control-hover-bg, rgba(148, 163, 184, 0.14)) !important; }
    .ai-window-control:active { filter: brightness(0.96); }
    .ai-window-control:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.55);
        outline-offset: 1px;
    }
    .ai-titlebar-tool:hover { background: var(--ai-titlebar-tool-hover-bg, rgba(148, 163, 184, 0.12)) !important; }
    .ai-titlebar-tool:active { filter: brightness(0.96); }
    .ai-titlebar-tool:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.4);
        outline-offset: 1px;
    }
`;

function getTitleBarToolButtonStyle(t: Theme, variant: "default" | "danger" | "active" = "default"): React.CSSProperties {
    const isDanger = variant === "danger";
    const isActive = variant === "active";
    return {
        ...baseActionBtnStyle,
        color: isDanger ? "#b91c1c" : (isActive ? t.text : t.actionBtnColor),
        background: isDanger ? "rgba(220, 38, 38, 0.04)" : (isActive ? t.divider : "transparent"),
        boxShadow: isActive ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-titlebar-tool-hover-bg' as any]: isDanger ? "rgba(220, 38, 38, 0.12)" : (isActive ? t.divider : "rgba(148, 163, 184, 0.12)"),
    };
}


function localizeText(lang: string, en: string, zhHans: string, zhHant?: string): string {
    if (lang === "en") return en;
    if (lang === "zh-Hant") return zhHant || zhHans;
    return zhHans;
}

interface ProjectSearchItem {
    id: string;
    name: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    tags?: string[];
    last_activity?: string;
    entry_count?: number;
    pinned?: boolean;
}

function formatWorkflowType(type: string | undefined, lang: string): string {
    if (!type) return "";
    const labels: Record<string, { en: string; zh: string }> = {
        coding: { en: "Coding", zh: "\u7f16\u7a0b" },
        product_design: { en: "Product Design", zh: "\u4ea7\u54c1\u8bbe\u8ba1" },
        research: { en: "Research", zh: "\u7814\u7a76" },
        writing: { en: "Writing", zh: "\u5199\u4f5c" },
    };
    const hit = labels[type];
    return hit ? (lang === "en" ? hit.en : hit.zh) : type.replace(/_/g, " ");
}

function useProjectSearch(lang: string) {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [results, setResults] = useState<ProjectSearchItem[]>([]);
    const [loading, setLoading] = useState(false);
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const doSearch = useCallback((q: string) => {
        setLoading(true);
        SearchProjects(q, 10)
            .then(r => setResults((r || []) as ProjectSearchItem[]))
            .catch(() => setResults([]))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (open && query === "") doSearch("");
    }, [open, query, doSearch]);

    useEffect(() => () => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
    }, []);

    const onQueryChange = useCallback((value: string) => {
        setQuery(value);
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => doSearch(value), 250);
    }, [doSearch]);

    const close = useCallback(() => { setOpen(false); setQuery(""); }, []);
    const toggle = useCallback(() => { setOpen(v => !v); }, []);
    const refresh = useCallback(() => doSearch(query), [doSearch, query]);

    const formatTime = useCallback((iso?: string): string => {
        if (!iso) return "";
        try {
            const d = new Date(iso);
            const diffH = Math.floor((Date.now() - d.getTime()) / 3600000);
            if (diffH < 1) return localizeText(lang, "just now", "\u521a\u521a");
            if (diffH < 24) return `${diffH}${localizeText(lang, "h ago", "\u5c0f\u65f6\u524d")}`;
            const diffD = Math.floor(diffH / 24);
            if (diffD < 7) return `${diffD}${localizeText(lang, "d ago", "\u5929\u524d")}`;
            return d.toLocaleDateString();
        } catch {
            return "";
        }
    }, [lang]);

    return { open, query, results, loading, toggle, close, onQueryChange, refresh, formatTime };
}

function ProjectSearchPanel({ search, lang, theme: t, inline, onProjectSwitch, onTaskPrefsChanged }: {
    search: ReturnType<typeof useProjectSearch>;
    lang: string;
    theme: Theme;
    inline: boolean;
    onProjectSwitch: (displayMsg: string) => Promise<void> | void;
    onTaskPrefsChanged?: () => void;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; item: ProjectSearchItem } | null>(null);
    const [renamingPath, setRenamingPath] = useState<string | null>(null);
    const [renameVal, setRenameVal] = useState("");

    useEffect(() => { if (search.open) inputRef.current?.focus(); }, [search.open]);

    useEffect(() => {
        if (!search.open) return;
        const handler = (event: MouseEvent) => {
            if (panelRef.current && !panelRef.current.contains(event.target as Node)) {
                search.close();
                setCtxMenu(null);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [search.open, search.close]);

    const refreshResults = useCallback(() => {
        search.refresh();
        onTaskPrefsChanged?.();
    }, [search, onTaskPrefsChanged]);

    const onSelect = useCallback(async (item: ProjectSearchItem) => {
        if (renamingPath) return;
        search.close();
        try {
            const msg = await ResumeProject(item.project_path);
            if (msg) await onProjectSwitch(msg);
        } catch (error) {
            console.error("[ProjectSearch] ResumeProject failed:", error);
        }
    }, [renamingPath, search, onProjectSwitch]);

    if (!search.open) return null;

    return (
        <div ref={panelRef} style={{ flexShrink: 0, borderBottom: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, zIndex: 30000, position: "relative", overflow: "visible" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "6px 12px" }}>
                <span style={{ fontSize: "13px", opacity: 0.55, flexShrink: 0 }}>{"\u{1F50D}"}</span>
                <input
                    ref={inputRef}
                    type="text"
                    value={search.query}
                    onChange={event => search.onQueryChange(event.target.value)}
                    onKeyDown={event => { if (event.key === "Escape") search.close(); }}
                    placeholder={localizeText(lang, "Search tasks...", "\u641c\u7d22\u4efb\u52a1...")}
                    style={{ flex: 1, border: "none", outline: "none", background: "transparent", color: t.text, fontSize: "13px", fontFamily: "inherit", padding: "4px 0", minWidth: 0 }}
                />
                <button
                    {...(inline ? { onMouseDown: (event: React.MouseEvent) => { event.preventDefault(); event.stopPropagation(); search.close(); } } : { onClick: () => search.close() })}
                    style={{ background: "none", border: "none", cursor: "pointer", color: t.text, opacity: 0.5, fontSize: "12px", padding: "2px 4px", lineHeight: 1, flexShrink: 0 }}
                    title={localizeText(lang, "Close", "\u5173\u95ed")}
                >{"x"}</button>
            </div>
            <div style={{ maxHeight: "320px", overflowY: "auto", padding: "0 4px 4px" }}>
                {search.loading && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{localizeText(lang, "Searching...", "\u641c\u7d22\u4e2d...")}</div>}
                {!search.loading && search.results.length === 0 && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{search.query ? localizeText(lang, "No tasks found", "\u672a\u627e\u5230\u4efb\u52a1") : localizeText(lang, "No recent tasks", "\u6682\u65e0\u6700\u8fd1\u4efb\u52a1")}</div>}
                {!search.loading && search.results.map(item => (
                    <div
                        key={item.id || item.project_path}
                        onClick={() => onSelect(item)}
                        onContextMenu={event => { event.preventDefault(); setCtxMenu({ x: event.clientX, y: event.clientY, item }); }}
                        style={{ padding: "8px 10px", cursor: "pointer", borderRadius: "6px", transition: "background 0.15s" }}
                        onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)}
                        onMouseLeave={event => (event.currentTarget.style.background = "transparent")}
                    >
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "2px" }}>
                            <span style={{ fontSize: "13px", flexShrink: 0 }}>{item.pinned ? "\u{1F4CC}" : "\u{1F516}"}</span>
                            {renamingPath === item.project_path ? (
                                <input
                                    autoFocus
                                    value={renameVal}
                                    onChange={event => setRenameVal(event.target.value)}
                                    onBlur={async () => {
                                        const trimmed = renameVal.trim();
                                        if (trimmed && trimmed !== item.name) {
                                            await RenameTask(item.project_path, trimmed);
                                            refreshResults();
                                        }
                                        setRenamingPath(null);
                                    }}
                                    onKeyDown={event => { if (event.key === "Enter") (event.target as HTMLInputElement).blur(); if (event.key === "Escape") setRenamingPath(null); }}
                                    onClick={event => event.stopPropagation()}
                                    style={{ flex: 1, fontSize: "13px", fontWeight: 600, color: t.text, background: t.codeBlockBg, border: `1px solid ${t.headingColor}`, borderRadius: "3px", padding: "2px 6px", outline: "none", minWidth: 0, fontFamily: "inherit" }}
                                />
                            ) : (
                                <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{item.name || item.project_path}</span>
                            )}
                            {item.workflow_type && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(99,102,241,0.12)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{formatWorkflowType(item.workflow_type, lang)}</span>}
                            <button type="button" onClick={event => { event.stopPropagation(); void onSelect(item); }} style={{ border: "none", background: "rgba(99,102,241,0.12)", color: t.headingColor, borderRadius: "999px", width: "22px", height: "22px", cursor: "pointer", flexShrink: 0 }} title={localizeText(lang, "Resume task", "\u7ee7\u7eed\u4efb\u52a1")}>{">"}</button>
                        </div>
                        <div style={{ fontSize: "11px", color: t.text, opacity: 0.45, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px" }}>{item.project_path}</div>
                        {item.preview && <div style={{ fontSize: "11px", color: t.text, opacity: 0.35, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px", marginTop: "1px" }}>{item.preview}</div>}
                        {item.last_activity && <div style={{ fontSize: "10px", color: t.text, opacity: 0.32, paddingLeft: "21px", marginTop: "1px" }}>{search.formatTime(item.last_activity)}</div>}
                    </div>
                ))}
            </div>
            {ctxMenu && (<>
                <div style={{ position: "fixed", inset: 0, zIndex: 9998 }} onClick={() => setCtxMenu(null)} />
                <div style={{ position: "fixed", left: ctxMenu.x, top: ctxMenu.y, zIndex: 9999, background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: "6px", boxShadow: "0 4px 12px rgba(0,0,0,0.15)", padding: "4px 0", minWidth: "120px" }}>
                    {[
                        { label: localizeText(lang, "Rename", "\u91cd\u547d\u540d"), icon: "edit", action: () => { setRenamingPath(ctxMenu.item.project_path); setRenameVal(ctxMenu.item.name || ""); setCtxMenu(null); } },
                        { label: ctxMenu.item.pinned ? localizeText(lang, "Unpin", "\u53d6\u6d88\u7f6e\u9876") : localizeText(lang, "Pin", "\u7f6e\u9876"), icon: "pin", action: async () => { await PinTask(ctxMenu.item.project_path, !ctxMenu.item.pinned); refreshResults(); setCtxMenu(null); } },
                        { label: localizeText(lang, "Remove", "\u79fb\u9664"), icon: "x", action: async () => { await HideTask(ctxMenu.item.project_path); refreshResults(); setCtxMenu(null); } },
                    ].map(item => (
                        <div key={item.label} onClick={item.action} style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 12px", cursor: "pointer", fontSize: "12px", color: t.text, transition: "background 0.1s" }} onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}>
                            <span style={{ fontSize: "13px" }}>{item.icon}</span><span>{item.label}</span>
                        </div>
                    ))}
                </div>
            </>)}
        </div>
    );
}
const miniActionButtonStyle: React.CSSProperties = {
    flex: 1,
    minWidth: 0,
    border: "1px solid #cbd5e1",
    borderRadius: "8px",
    background: "white",
    color: "#334155",
    fontSize: "11px",
    fontWeight: 600,
    padding: "5px 8px",
    cursor: "pointer",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
};

const NUM_BARS = 8;

function VoiceLevelVisualizer({ onAudioLevelRef, isSpeaking, themeColor, speakingColor }: {
    onAudioLevelRef: React.MutableRefObject<((level: number) => void) | null>;
    isSpeaking: boolean;
    themeColor: string;
    speakingColor: string;
}) {
    const barsRef = useRef<HTMLDivElement | null>(null);
    const levelsRef = useRef(new Float32Array(NUM_BARS));
    const colorRef = useRef(themeColor);
    colorRef.current = isSpeaking ? speakingColor : themeColor;

    useEffect(() => {
        let frameId = 0;
        const levels = levelsRef.current;
        onAudioLevelRef.current = (level: number) => {
            for (let i = 0; i < NUM_BARS - 1; i++) levels[i] = levels[i + 1];
            levels[NUM_BARS - 1] = level;
            if (!frameId) {
                frameId = requestAnimationFrame(() => {
                    frameId = 0;
                    const container = barsRef.current;
                    if (!container) return;
                    const bars = container.children;
                    const color = colorRef.current;
                    for (let i = 0; i < bars.length && i < NUM_BARS; i++) {
                        const el = bars[i] as HTMLElement;
                        el.style.height = `${Math.max(2, Math.min(14, levels[i] * 14))}px`;
                        el.style.background = color;
                    }
                });
            }
        };
        return () => {
            onAudioLevelRef.current = null;
            if (frameId) cancelAnimationFrame(frameId);
            levels.fill(0);
        };
    }, [onAudioLevelRef]);

    return (
        <div ref={barsRef} style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", gap: "1px", width: "22px", height: "18px", overflow: "hidden" }} aria-hidden="true">
            {Array.from({ length: NUM_BARS }, (_, i) => <div key={i} style={{ width: "2px", height: "2px", flex: "0 0 2px", borderRadius: "1px", background: themeColor, transition: "height 0.08s ease-out" }} />)}
        </div>
    );
}

function getWindowControlButtonStyle(t: Theme, variant: "hide" | "fullscreen", active = false): React.CSSProperties {
    const hoverBg = variant === "hide" ? "rgba(148, 163, 184, 0.14)" : "rgba(99, 102, 241, 0.12)";
    return {
        ...baseWindowControlBtnStyle,
        color: active ? "#1f2937" : t.actionBtnColor,
        background: active ? "rgba(99, 102, 241, 0.16)" : "transparent",
        boxShadow: active ? "inset 0 0 0 1px rgba(99, 102, 241, 0.08)" : "none",
        ['--ai-window-control-hover-bg' as any]: hoverBg,
    };
}

/* Themed inline markdown rendering */

function looksLikeFilePath(s: string): boolean {
    if (/^[A-Za-z]:\\/.test(s)) return true;
    if (/^(~|\/(?:Users|home|tmp|var|opt|etc|usr))[/\\]/.test(s)) return true;
    return false;
}

function renderPathLink(filePath: string, key: number, t: Theme, trimTrailing = false): React.ReactNode {
    const display = trimTrailing
        ? filePath.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "")
        : filePath;
    return (
        <a key={key}
           href="#"
           onClick={(event) => openFileInFolder(event, display)}
           style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
           title={display}
        >{"\uD83D\uDCC2 "}{display}</a>
    );
}

function renderInlineMarkdownRestored(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[^\n\r*?"<>|:]+\.\w+)|([A-Za-z]:\\[\w\\.\-]+\\?)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))\/[^\n\r*?"<>|:]+\.\w+)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))[\w/.\-]+)/g;
    let lastIndex = 0;
    let idx = 0;
    while (true) {
        const match = re.exec(text);
        if (!match) break;
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const m = match[0];
        if (match[1]) {
            const inner = m.slice(1, -1);
            parts.push(looksLikeFilePath(inner) ? renderPathLink(inner, idx++, t) : <code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{inner}</code>);
        } else if (match[2]) {
            const inner = m.slice(2, -2);
            parts.push(looksLikeFilePath(inner) ? renderPathLink(inner, idx++, t) : <strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{inner}</strong>);
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else if (looksLikeFilePath(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(event) => openFileInFolder(event, href)} style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }} title={href}>{"\uD83D\uDCC2 "}{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            const filePath = m.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
            if (filePath.length !== m.length) re.lastIndex -= (m.length - filePath.length);
            parts.push(renderPathLink(filePath, idx++, t));
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts.length > 0 ? parts : ["\u00A0"];
}

function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    return renderInlineMarkdownRestored(text, t);
}

function renderInlineMarkdownLegacyUnused(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Path matching: two strategies per platform
    // 1. Broad match for paths with CJK/spaces - requires .ext ending as boundary anchor
    // 2. Original ASCII-only match - works without .ext
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[^\n\r*?"<>|:]+\.\w+)|([A-Za-z]:\\[\w\\.\-]+\\?)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))\/[^\n\r*?"<>|:]+\.\w+)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))[\w/.\-]+)/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let idx = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const m = match[0];
        if (match[1]) {
            const inner = m.slice(1, -1);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else {
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{inner}</code>);
            }
        } else if (match[2]) {
            const inner = m.slice(2, -2);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else {
                parts.push(<strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{inner}</strong>);
            }
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else if (looksLikeFilePath(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(event) => openFileInFolder(event, href)} style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }} title={href}>{"\uD83D\uDCC2 "}{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            // Trim trailing punctuation/whitespace that isn't part of the path
            const filePath = m.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
            if (filePath.length !== m.length) {
                // Rewind regex lastIndex so trimmed chars are re-processed
                re.lastIndex -= (m.length - filePath.length);
            }
            parts.push(
                <a key={idx++}
                   href="#"
                   onClick={(event) => openFileInFolder(event, filePath)}
                   style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
                   title={filePath}
                >{"\u{1F4C4}"} {filePath}</a>
            );
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length > 0 ? parts : ["\u00A0"];
}

function renderMarkdownLine(text: string, key: string | number, t: Theme): React.ReactNode {
    const trimmed = text.trimStart();

    const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (headingMatch) {
        const level = headingMatch[1].length;
        const sizes: Record<number, string> = { 1: "1.2em", 2: "1.1em", 3: "1.0em", 4: "0.95em" };
        return (
            <div key={key} style={{ fontSize: sizes[level] || "1em", fontWeight: 700, color: t.headingColor, margin: "0.4em 0 0.2em" }}>
                {renderInlineMarkdown(headingMatch[2], t)}
            </div>
        );
    }

    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{ borderLeft: `2px solid ${t.quoteBorder}`, paddingLeft: "8px", color: t.quoteText, fontStyle: "italic", minHeight: "1.4em" }}>
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    if (/^[-*_]{3,}\s*$/.test(trimmed)) {
        return <hr key={key} style={{ border: "none", borderTop: `1px solid ${t.divider}`, margin: "8px 0" }} />;
    }

    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2], t)}
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em" }}>
            {renderInlineMarkdown(text, t) || "\u00A0"}
        </div>
    );
}

/* Structured response rendering */

function isTableRow(line: string): boolean {
    const trimmed = line.trim();
    return trimmed.startsWith("|") && trimmed.length > 1;
}

function isSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

function parseTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith("|")) trimmed = trimmed.slice(1);
    if (trimmed.endsWith("|")) trimmed = trimmed.slice(0, -1);
    return trimmed.split("|").map(c => c.trim());
}

function renderTable(tableLines: string[], key: string, t: Theme): React.ReactNode {
    const dataRows = tableLines.filter(line => !isSeparatorRow(line));
    if (tableLines.length < 2 || dataRows.length === 0) return null;
    const headerCells = parseTableCells(dataRows[0]);
    const bodyRows = dataRows.slice(1);
    const cellStyle: React.CSSProperties = { border: `1px solid ${t.divider}`, padding: "4px 8px", textAlign: "left", fontSize: "0.9em", lineHeight: 1.5 };
    return (
        <div key={key} style={{ overflowX: "auto", margin: "4px 0" }}>
            <table style={{ borderCollapse: "collapse", width: "100%", color: t.text, whiteSpace: "normal", wordBreak: "normal" }}>
                <thead><tr>{headerCells.map((cell, ci) => <th key={ci} style={{ ...cellStyle, fontWeight: 600, background: t.fieldBg }}>{renderInlineMarkdown(cell, t)}</th>)}</tr></thead>
                {bodyRows.length > 0 && <tbody>{bodyRows.map((row, ri) => { const cells = parseTableCells(row); return <tr key={ri}>{headerCells.map((_, ci) => <td key={ci} style={cellStyle}>{renderInlineMarkdown(cells[ci] || "", t)}</td>)}</tr>; })}</tbody>}
            </table>
        </div>
    );
}

function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
    const elements: React.ReactNode[] = [];
    const lines = content.split("\n");
    let inCodeBlock = false;
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
    let tableLines: string[] = [];
    let lineIdx = 0;

    const flushCodeBlock = () => {
        if (codeBlockLines.length > 0) {
            elements.push(
                <pre key={`code-${elements.length}`} style={{
                    background: t.codeBlockBg,
                    border: `1px solid ${t.codeBlockBorder}`,
                    borderRadius: "4px",
                    padding: "8px 10px",
                    margin: "4px 0",
                    fontSize: "0.9em",
                    overflowX: "auto",
                    color: t.codeText,
                    lineHeight: 1.5,
                }}>
                    {codeBlockLang && <div style={{ color: t.codeBlockLang, fontSize: "0.85em", marginBottom: "4px" }}>{codeBlockLang}</div>}
                    <code>{codeBlockLines.join("\n")}</code>
                </pre>
            );
        }
        codeBlockLines = [];
        codeBlockLang = "";
    };

    const flushTable = () => {
        if (tableLines.length === 0) return;
        const rendered = renderTable(tableLines, `tbl-${elements.length}`, t);
        if (rendered) {
            elements.push(rendered);
        } else {
            for (const tableLine of tableLines) {
                elements.push(renderMarkdownLine(tableLine, `md-fallback-${elements.length}`, t));
            }
        }
        tableLines = [];
    };

    for (const line of lines) {
        if (/^```/.test(line.trimStart())) {
            flushTable();
            if (inCodeBlock) {
                flushCodeBlock();
                inCodeBlock = false;
            } else {
                inCodeBlock = true;
                codeBlockLang = line.trimStart().slice(3).trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
        } else if (isTableRow(line)) {
            tableLines.push(line);
        } else {
            flushTable();
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`, t));
        }
        lineIdx++;
    }
    if (inCodeBlock) flushCodeBlock();
    flushTable();
    return elements;
}

function renderFields(fields: Array<{ label: string; value: string }>, t: Theme): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {fields.map((f, i) => {
                const isRecovery = f.label === "Recovery";
                const recoveryTone = String(f.value || '').toLowerCase();
                const recoveryStyle: React.CSSProperties = isRecovery
                    ? {
                        display: "inline-flex",
                        alignItems: "center",
                        padding: "2px 8px",
                        borderRadius: "999px",
                        fontWeight: 700,
                        background: recoveryTone.includes('failed')
                            ? "rgba(220, 38, 38, 0.12)"
                            : recoveryTone.includes('partial')
                                ? "rgba(245, 158, 11, 0.16)"
                                : "rgba(34, 197, 94, 0.14)",
                        color: recoveryTone.includes('failed')
                            ? "#b91c1c"
                            : recoveryTone.includes('partial')
                                ? "#b45309"
                                : "#166534",
                    }
                    : { color: t.text };
                return (
                    <div key={`field-${i}`} data-testid="field-card" style={{
                        background: t.fieldBg,
                        border: `1px solid ${t.fieldBorder}`,
                        borderRadius: "4px",
                        padding: "4px 8px",
                        fontSize: "12px",
                    }}>
                        <span style={{ color: t.fieldLabel, marginRight: "6px" }}>{f.label}:</span>
                        <span data-testid={isRecovery ? 'recovery-badge' : undefined} style={recoveryStyle}>{f.value}</span>
                    </div>
                );
            })}
        </div>
    );
}

function renderActions(
    actions: ChatAction[],
    executeAction: (command: string) => void,
    t: Theme,
): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {actions.map((a, i) => (
                <button
                    key={`action-${i}`}
                    data-testid="action-button"
                    onClick={() => executeAction(a.command)}
                    style={{
                        ...baseInputBtnStyle,
                        color: a.style === "danger" ? t.errorText : t.btnColor,
                        borderColor: a.style === "danger" ? t.errorText : t.btnBorder,
                        fontSize: "12px",
                        padding: "4px 10px",
                        minHeight: "28px",
                    }}
                >
                    {a.label}
                </button>
            ))}
        </div>
    );
}

function renderConfirmationList(testId: string, title: string, items: string[], t: Theme): React.ReactNode {
    if (items.length === 0) return null;
    return (
        <div data-testid={testId} style={{ marginTop: "8px" }}>
            <div style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "4px" }}>{title}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                {items.map((item, index) => (
                    <div key={`${testId}-${index}`} style={{ minHeight: "1.4em", color: t.text }}>
                        <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                        {renderInlineMarkdown(item, t)}
                    </div>
                ))}
            </div>
        </div>
    );
}

function renderConfirmationCard(
    confirmation: ChatConfirmation,
    actions: ChatAction[] | undefined,
    executeAction: (command: string) => void,
    t: Theme,
): React.ReactNode {
    const targetPaths = confirmation.targetPaths || [];
    const plannedActions = confirmation.plannedActions || [];
    const riskFlags = confirmation.riskFlags || [];
    const revisionHints = confirmation.revisionHints || [];
    const taskType = confirmation.taskType?.trim() || '';
    const status = confirmation.status?.trim() || '';
    return (
        <div
            data-testid="confirmation-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(99,102,241,0.02))",
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {taskType ? `\u6267\u884c\u524d\u786e\u8ba4 - ${taskType}` : "\u6267\u884c\u524d\u786e\u8ba4"}
            </div>
            {status && (
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {"\u72b6\u6001"}: {status}
                </div>
            )}
            <div data-testid="confirmation-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                {renderContentWithCodeBlocks(confirmation.summary, t)}
            </div>
            {renderConfirmationList("confirmation-target-paths", "\u76ee\u6807\u8def\u5f84", targetPaths, t)}
            {renderConfirmationList("confirmation-planned-actions", "\u8ba1\u5212\u64cd\u4f5c", plannedActions, t)}
            {renderConfirmationList("confirmation-risk-flags", "\u98ce\u9669\u6807\u8bb0", riskFlags, t)}
            {renderConfirmationList("confirmation-revision-hints", "\u4fee\u8ba2\u63d0\u793a", revisionHints, t)}
            {actions && actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function renderUnfinishedSlotCard(
    slot: ChatUnfinishedSlot,
    executeAction: (command: string) => void,
    t: Theme,
): React.ReactNode {
    const actions = slot.actions || [];
    return (
        <div
            data-testid="unfinished-slot-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: "linear-gradient(135deg, rgba(245,158,11,0.08), rgba(245,158,11,0.03))",
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {"Unfinished item"}
            </div>
            {slot.status && (
                <div data-testid="unfinished-slot-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {"\u72b6\u6001"}: {slot.status}
                </div>
            )}
            {slot.title && (
                <div data-testid="unfinished-slot-title" style={{ color: t.text, fontWeight: 600, marginBottom: "4px" }}>
                    {slot.title}
                </div>
            )}
            {slot.summary && (
                <div data-testid="unfinished-slot-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                    {renderContentWithCodeBlocks(slot.summary, t)}
                </div>
            )}
            {slot.projectPath && (
                <div data-testid="unfinished-slot-project" style={{ color: t.pathColor, marginTop: "6px", wordBreak: "break-all" }}>
                    {"\u{1F4C1}"} {slot.projectPath}
                </div>
            )}
            {actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function openFileInFolder(event: React.MouseEvent, filePath: string) {
    event.preventDefault();
    void OpenFileOrShowInFolder(filePath).catch(() => ShowItemInFolder(filePath));
}

/* Render a single ChatMessage */

function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void, t: Theme, isLastAssistant: boolean, savedFileLabel: string): React.ReactNode {
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: `1px solid ${t.divider}`, margin: "8px 0 4px 0" }} />
                    <div style={{ color: t.userColor, fontWeight: 600, padding: "3px 0 3px 1.2em", overflowWrap: "break-word", whiteSpace: "pre-wrap", textIndent: "-1.2em" }}>
                        {">"} {msg.content}
                    </div>
                </div>
            );
        case "assistant":
            return (
                <div key={msg.id} style={{
                    padding: "4px 0 4px 8px",
                    borderLeft: `2px solid ${t.responseBorderLeft}`,
                    margin: "2px 0",
                    color: t.text,
                }}>
                    {/* Streaming: show blinking cursor only on the last assistant message */}
                    {isLastAssistant && !msg.content && !msg.fields && !msg.thumbnailBase64 && !msg.localFilePaths?.length && (
                        <span style={{ opacity: 0.5, animation: "blink 1s step-end infinite" }}>{"|"}</span>
                    )}
                    {msg.thumbnailBase64 && msg.localFilePath && (
                        <div style={{ margin: "4px 0 6px 0" }}>
                            <a href="#" onClick={(event) => openFileInFolder(event, msg.localFilePath!)}
                               style={{ display: "inline-block", cursor: "pointer" }}
                               title={msg.localFilePath}>
                                <img
                                    src={`data:image/png;base64,${msg.thumbnailBase64}`}
                                    alt="screenshot"
                                    style={{
                                        maxWidth: "180px", maxHeight: "120px",
                                        borderRadius: "4px", border: `1px solid ${t.borderLeft}`,
                                        objectFit: "contain",
                                    }}
                                />
                            </a>
                        </div>
                    )}
                    {renderContentWithCodeBlocks(msg.content, t)}
                    {msg.confirmation && renderConfirmationCard(msg.confirmation, msg.actions, executeAction, t)}
                    {msg.unfinishedSlot && renderUnfinishedSlotCard(msg.unfinishedSlot, executeAction, t)}
                    {msg.localFilePaths && msg.localFilePaths.length > 0 && (
                        <div style={{ margin: "4px 0" }}>
                            {msg.localFilePaths.map((fp, i) => (
                                <div key={i} style={{ padding: "2px 0" }}>
                                    <a href="#"
                                       onClick={(event) => openFileInFolder(event, fp)}
                                       style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                                       title={fp}>
                                        {"\u{1F4BE}"} {savedFileLabel}: {"\u{1F4C1}"} {fp}
                                    </a>
                                </div>
                            ))}
                        </div>
                    )}
                    {msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {!msg.confirmation && msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction, t)}
                </div>
            );
        case "progress":
            return (
                <div key={msg.id} style={{ color: t.textMuted, fontSize: "11px", padding: "1px 0", fontStyle: "italic" }}>
                        {">"} {msg.content}
                </div>
            );
        case "system":
            return (
                <div key={msg.id} style={{
                    padding: "8px 12px",
                    margin: "4px 0",
                    borderRadius: "6px",
                    background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                    borderLeft: `3px solid ${t.promptColor}`,
                    color: t.text,
                    fontSize: "12px",
                    lineHeight: "1.6",
                }}>
                    {msg.kind === 'trace' && msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {renderContentWithCodeBlocks(msg.content, t)}
                </div>
            );
        case "error":
            return (
                <div key={msg.id} style={{
                    color: t.errorText,
                    background: t.errorBg,
                    borderLeft: `2px solid ${t.errorBorder}`,
                    padding: "4px 8px",
                    margin: "2px 0",
                    borderRadius: "2px",
                    fontSize: "12px",
                }}>
                        {">"} {msg.content}
                </div>
            );
        default:
            return null;
    }
}

/* Inject static panel styles once at module level */
if (typeof document !== "undefined" && !document.getElementById(AI_PANEL_STATIC_STYLE_ID)) {
    const style = document.createElement("style");
    style.id = AI_PANEL_STATIC_STYLE_ID;
    style.textContent = AI_PANEL_STATIC_STYLE_TEXT;
    document.head.appendChild(style);
}

/* Main component */

async function savePastedImage(base64: string, ext: string): Promise<string> {
    const w = window as any;
    if (typeof window !== "undefined" && w.go?.main?.App?.SavePastedImage) {
        return w.go.main.App.SavePastedImage(base64, ext);
    }
    throw new Error("SavePastedImage binding not available");
}

export function AIAssistantPanel(props: any) {
    const { onClose, lang, chatFontSize = 14, groupDiscussion, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0 } = props;
    const state = props.state || props;
    const actions = props.actions || props;
    const panelWindow = props.window || props;
    const {
        messages,
        progressMessages = [],
        sending,
        streaming,
        visualBusy,
        ready,
        initStatus,
        selectedFilePath: selectedFilePathFromState = "",
        submittedPrompts = [],
        draftInputValue = "",
        trialReflectEnabled = false,
        scrollToTopSeq,
        onboardingIncomplete,
        showTraceEntry = false,
    } = state;
    const {
        browseFile,
        clearSelectedFile,
        removeSelectedFile,
        sendMessage,
        clearHistory,
        recordSubmittedPrompt,
        setDraftInputValue,
        executeAction,
        refreshNews,
        onOpenOnboarding,
        cancelSession,
        onOpenTutorial,
        onTaskPrefsChanged,
    } = actions;
    const selectedFilePaths = Array.isArray(state.selectedFilePaths) ? state.selectedFilePaths : (selectedFilePathFromState ? [selectedFilePathFromState] : []);
    const selectedFilePath = selectedFilePaths[0] || "";
    const {
        inline,
        maximized = false,
        onToggleMaximize,
        onHideWindow,
    } = panelWindow || {};
    const [localDraftInputValue, setLocalDraftInputValue] = useState(draftInputValue);
    const [composing, setComposing] = useState(false);
    const [historyIndex, setHistoryIndex] = useState(-1);
    const [draftBeforeHistory, setDraftBeforeHistory] = useState<string | null>(null);
    const [historyEdits, setHistoryEdits] = useState<Record<number, string>>({});
    const [groupDiscussionOpen, setGroupDiscussionOpen] = useState(false);
    const [groupDiscussionBusy, setGroupDiscussionBusy] = useState("");
    const [cancelPending, setCancelPending] = useState(false);
    const [pendingAttachments, setPendingAttachments] = useState<AttachmentInfo[]>([]);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const scrollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const prevReadyRef = useRef(ready);
    const [themeMode, setThemeMode] = useState<'light' | 'dark'>(() => {
        if (typeof window === 'undefined') return 'light';
        try {
            return window.localStorage.getItem(AI_THEME_MODE_STORAGE_KEY) === 'dark' ? 'dark' : 'light';
        } catch {
            return 'light';
        }
    });
    const [ttsEnabled, setTtsEnabled] = useState(false);
    const ttsEnabledRef = useRef(false);
    const ttsAudioQueueRef = useRef<string[]>([]);
    const ttsAudioPlayingRef = useRef(false);
    const ttsCurrentAudioRef = useRef<HTMLAudioElement | null>(null);

    useEffect(() => { ttsEnabledRef.current = ttsEnabled; }, [ttsEnabled]);
    useEffect(() => { GetTTSEnabled().then(v => setTtsEnabled(!!v)).catch(() => {}); }, []);
    useEffect(() => {
        try { window.localStorage.setItem(AI_THEME_MODE_STORAGE_KEY, themeMode); } catch {}
        onThemeModeChange?.(themeMode);
    }, [themeMode, onThemeModeChange]);

    const playNextTTSAudio = useCallback(() => {
        if (ttsAudioPlayingRef.current) return;
        if (!ttsEnabledRef.current) {
            ttsAudioQueueRef.current = [];
            return;
        }
        const b64wav = ttsAudioQueueRef.current.shift();
        if (!b64wav) return;
        ttsAudioPlayingRef.current = true;
        try {
            const audio = new Audio("data:audio/wav;base64," + b64wav);
            ttsCurrentAudioRef.current = audio;
            if (audioOutputDeviceId && typeof (audio as any).setSinkId === "function") {
                void (audio as any).setSinkId(audioOutputDeviceId).catch(() => {});
            }
            const finish = () => {
                audio.onended = null;
                audio.onerror = null;
                if (ttsCurrentAudioRef.current === audio) ttsCurrentAudioRef.current = null;
                ttsAudioPlayingRef.current = false;
                playNextTTSAudio();
            };
            audio.onended = finish;
            audio.onerror = finish;
            void audio.play().catch(finish);
        } catch {
            ttsAudioPlayingRef.current = false;
            playNextTTSAudio();
        }
    }, [audioOutputDeviceId]);

    useEffect(() => {
        if (ttsEnabled) return;
        ttsAudioQueueRef.current = [];
        ttsAudioPlayingRef.current = false;
        const audio = ttsCurrentAudioRef.current;
        if (audio) {
            audio.pause();
            audio.src = "";
            ttsCurrentAudioRef.current = null;
        }
    }, [ttsEnabled]);

    useEffect(() => {
        const handler = (b64wav: string) => {
            if (!ttsEnabledRef.current) return;
            ttsAudioQueueRef.current.push(b64wav);
            playNextTTSAudio();
        };
        EventsOn("tts:audio", handler);
        return () => { EventsOff("tts:audio"); };
    }, [playNextTTSAudio]);

    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, mergeAndFire } = useBufferQueue();
    const [inputAreaHeight, setInputAreaHeight] = useState<number | null>(null);


    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;

    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion, dismissDocsBar } = useWorkflowState();
    const { state: codePreviewState, closePanel: closeCodePreview, selectFile: selectCodeFile } = useCodePreviewState(workflowState.splitMode);
    const showWorkflowPreview = workflowState.splitMode;
    const showCodePreview = !showWorkflowPreview && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview;
    const splitRatio = anySplitActive ? workflowState.splitRatio : 1;
    const startPreviewResize = useCallback(() => {
        const container = document.querySelector('[data-testid="ai-panel-root"]') as HTMLElement | null;
        if (!container) return;
        const onMouseMove = (e: MouseEvent) => {
            const rect = container.getBoundingClientRect();
            if (rect.width <= 0) return;
            const nextRatio = Math.max(0.2, Math.min(0.8, (e.clientX - rect.left) / rect.width));
            setWorkflowSplitRatio(nextRatio);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [setWorkflowSplitRatio]);

    const title = lang === "en" ? "AI Assistant" : "AI \u52a9\u624b";
    const thinkingText = lang === "en" ? "Thinking..." : "\u6b63\u5728\u601d\u8003...";
    const processingText = lang === "en" ? "Executing tools and finishing task..." : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u5e76\u5b8c\u6210\u4efb\u52a1...";
    const idlePlaceholderText = lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...";
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const isBusy = sending;
    const inputLocked = isBusy || cancelPending;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;
    const projectSearch = useProjectSearch(lang);
    const handleProjectSearchSwitch = useCallback(async (msg: string) => {
        if (isBusy && cancelSession) {
            const ok = window.confirm(localizeText(lang, "A task is running. Stop it and switch tasks?", "\u5f53\u524d\u6709\u4efb\u52a1\u6b63\u5728\u6267\u884c\u3002\u662f\u5426\u4e2d\u6b62\u5f53\u524d\u4efb\u52a1\u5e76\u5207\u6362\uff1f"));
            if (!ok) return;
            await cancelSession();
        }
        await sendMessage(msg);
    }, [cancelSession, isBusy, lang, sendMessage]);

    const submitRecognizedVoiceText = useCallback(async (text: string, _source?: VoiceInputSource) => {
        const trimmed = text.trim();
        if (!trimmed || !ready || inputLocked) return;
        recordSubmittedPrompt?.(trimmed);
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setHistoryEdits({});
        setLocalDraftInputValue("");
        setDraftInputValue?.("");
        await sendMessage(trimmed);
    }, [inputLocked, ready, recordSubmittedPrompt, sendMessage, setDraftInputValue]);

    const voiceInput = useVoiceInput(submitRecognizedVoiceText, audioInputDeviceId || '');
    const voiceHoldTimerRef = useRef<number | null>(null);
    const voiceHoldActiveRef = useRef(false);
    const voiceSuppressClickRef = useRef(false);

    useEffect(() => {
        if (!petVoiceStartSeq || !ready || voiceInput.state !== 'idle') return;
        void voiceInput.toggle();
    }, [petVoiceStartSeq, ready, voiceInput]);

    useEffect(() => {
        if (!petFocusInputSeq) return;
        inputRef.current?.focus();
    }, [petFocusInputSeq]);

    const handleVoiceClick = useCallback(() => {
        if (voiceSuppressClickRef.current) {
            voiceSuppressClickRef.current = false;
            return;
        }
        if (!ready || voiceInput.state === 'transcribing') return;
        void voiceInput.toggle();
    }, [ready, voiceInput]);

    const finishVoicePointer = useCallback(() => {
        if (voiceHoldTimerRef.current) {
            clearTimeout(voiceHoldTimerRef.current);
            voiceHoldTimerRef.current = null;
        }
        if (voiceHoldActiveRef.current) {
            voiceHoldActiveRef.current = false;
            voiceSuppressClickRef.current = true;
            voiceInput.stopHold();
            window.setTimeout(() => { voiceSuppressClickRef.current = false; }, 250);
        }
    }, [voiceInput]);

    const handleVoicePointerDown = useCallback((event: React.PointerEvent<HTMLButtonElement>) => {
        if (event.button !== 0 || !ready || !voiceInput.asrReady || voiceInput.state !== "idle") return;
        voiceHoldTimerRef.current = window.setTimeout(() => {
            voiceHoldTimerRef.current = null;
            voiceHoldActiveRef.current = true;
            voiceSuppressClickRef.current = true;
            try { event.currentTarget.setPointerCapture(event.pointerId); } catch {}
            void voiceInput.startHold();
        }, 180);
    }, [ready, voiceInput]);

    const handleVoicePointerLeave = useCallback(() => {
        if (!voiceHoldActiveRef.current && voiceHoldTimerRef.current) {
            clearTimeout(voiceHoldTimerRef.current);
            voiceHoldTimerRef.current = null;
        }
    }, []);

    const initStatusLabels: Record<AIAssistantInitStatus, { en: string; zh: string }> = {
        connecting: { en: "Connecting to Hub...", zh: "\u6b63\u5728\u8fde\u63a5 Hub..." },
        loading:    { en: "Loading components...", zh: "\u6b63\u5728\u52a0\u8f7d\u7ec4\u4ef6..." },
        warming:    { en: "Warming up...", zh: "\u6b63\u5728\u9884\u70ed..." },
        ready:      { en: "Ready", zh: "\u5c31\u7eea" },
    };
    const groupDiscussionStatus = groupDiscussion?.status;
    const groupDiscussionConfig = groupDiscussion?.config || {};
    const groupDiscussionEnabled = groupDiscussionStatus?.enabled ?? groupDiscussionConfig.enabled ?? groupDiscussionConfig.group_discussion_enabled ?? false;
    const groupDiscussionDiscoverable = groupDiscussionStatus?.discoverable ?? groupDiscussionConfig.discoverable ?? groupDiscussionConfig.group_discussion_discoverable ?? false;
    const groupPendingInvites = groupDiscussionStatus?.pending_invites || [];
    const groupReadyTalks = groupDiscussionStatus?.ready_discussion_count ?? 0;
    const groupWaitingTalks = groupDiscussionStatus?.waiting_discussion_count ?? 0;
    const groupActiveTalks = groupDiscussionStatus?.active_discussion_count ?? 0;
    const groupStaleTalks = groupDiscussionStatus?.stale_discussion_count ?? 0;
    const groupDiscussionLabel = lang === "en"
        ? (groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "Group Listed" : "Group Private") : "Group Off")
        : (groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "\u7fa4\u7ec4\u53ef\u89c1" : "\u7fa4\u7ec4\u79c1\u5bc6") : "\u7fa4\u7ec4\u5173\u95ed");
    const groupDiscussionScopeText = lang === "en" ? "Current Hub only" : "\u4ec5\u5f53\u524d Hub";
    const statusKey = (initStatus ?? "connecting") as AIAssistantInitStatus;
    const initLabel = initStatusLabels[statusKey as keyof typeof initStatusLabels][lang === "en" ? "en" : "zh"];

    const placeholderText = !ready
        ? initLabel
        : showThinkingState
            ? thinkingText
            : showProcessingState
                ? processingText
                : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        setDraftInputValue?.(nextValue);
    }, [setDraftInputValue]);
    const canSend = ready && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
    const selectedFileName = selectedFilePath ? selectedFilePath.split(/[/\\]/).pop() || selectedFilePath : "";
    const { pinnedNews, otherMessages } = useMemo(() => {
        const pinned: ChatMessage[] = [];
        const other: ChatMessage[] = [];
        for (const m of messages) {
            if (isPinnedNewsMessage(m)) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [messages]);
    const hasConversation = otherMessages.length + progressMessages.length > 0;

    const resizeInput = useCallback(() => {
        if (!inputRef.current) return;
        const maxHeight = inputAreaHeight ?? 120;
        inputRef.current.style.height = "auto";
        inputRef.current.style.height = Math.min(inputRef.current.scrollHeight, maxHeight) + "px";
    }, [inputAreaHeight]);

    // Sync local draft from parent-owned draft state.
    useEffect(() => {
        setLocalDraftInputValue(draftInputValue);
    }, [draftInputValue]);

    // Reset scroll flag when component mounts (panel opened/re-shown)
    useEffect(() => {
        userScrolledUpRef.current = false;
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, []);

    // Debounced auto-scroll: coalesce rapid token updates into a single scroll
    useEffect(() => {
        if (userScrolledUpRef.current) {
            prevMsgCountRef.current = messages.length;
            return;
        }
        // New message added - scroll immediately
        if (messages.length !== prevMsgCountRef.current) {
            prevMsgCountRef.current = messages.length;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
            return;
        }
        // Content update on existing message (streaming tokens) - debounce 80ms
        if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        scrollTimerRef.current = setTimeout(() => {
            outputEndRef.current?.scrollIntoView({ behavior: "auto" });
        }, 80);
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        };
    }, [messages]);

    useEffect(() => {
        if (!inputRef.current) return;
        resizeInput();
    }, [inputValue, resizeInput]);

    // Track user scroll position
    const handleScroll = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return;
        const threshold = 80;
        userScrolledUpRef.current =
            container.scrollHeight - container.scrollTop - container.clientHeight > threshold;
    }, []);

    useEffect(() => {
        const becameReady = !prevReadyRef.current && ready;
        prevReadyRef.current = ready;
        if (!becameReady || userScrolledUpRef.current || !hasConversation) return;
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, [ready, hasConversation]);

    // Scroll to top when pinned news are (re)loaded only if there is no
    // existing conversation yet, so reopening maclaw still shows the latest chat.
    useEffect(() => {
        if (!scrollToTopSeq || hasConversation) return;
        const container = outputContainerRef.current;
        if (container) {
            container.scrollTo({ top: 0, behavior: "smooth" });
            userScrolledUpRef.current = true; // prevent auto-scroll-to-bottom from overriding
        }
    }, [scrollToTopSeq, hasConversation]);

    // Focus input on mount
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);

    // Escape key closes overlay mode, or exits maximized inline mode.
    useEffect(() => {
        if (!maximized && inline) return;
        const handler = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (inline && maximized) {
                onToggleMaximize?.();
                return;
            }
            if (!inline) onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose, inline, maximized, onToggleMaximize]);

    const startInputResize = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        const startY = e.clientY;
        const startHeight = inputAreaHeight ?? 120;
        const onMouseMove = (moveEvent: MouseEvent) => {
            const next = Math.max(56, Math.min(260, startHeight - (moveEvent.clientY - startY)));
            setInputAreaHeight(next);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "ns-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [inputAreaHeight]);
    const handleSelectWorkflowDir = useCallback(async () => {
        try {
            const dir = await SelectProjectDir();
            if (dir) SetWorkflowWorkingDir(dir);
        } catch (err) {
            console.error("Failed to set workflow working directory:", err);
        }
    }, []);
    const handleSend = useCallback(async () => {
        const text = inputValue.trim();
        if (submitLocked) {
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) return;
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = "." + (fileName.split(".").pop() || "").toLowerCase();
                attachments.push({ filePath: fp, isImage: isImageFilePath(fp), fileName, extension: ext });
            }
            addEntry(inputValue, attachments);
            recordSubmittedPrompt?.(inputValue);
            setHistoryIndex(-1);
            setDraftBeforeHistory(null);
            setHistoryEdits({});
            updateInputValue("");
            if (inputRef.current) inputRef.current.style.height = "auto";
            setPendingAttachments([]);
            clearSelectedFile?.();
            requestAnimationFrame(() => inputRef.current?.focus());
            return;
        }
        if (!text && selectedFilePaths.length === 0 && pendingAttachments.length === 0) return;
        const allFilePaths: string[] = [...selectedFilePaths];
        for (const att of pendingAttachments) {
            if (att.filePath.trim()) allFilePaths.push(att.filePath.trim());
        }
        recordSubmittedPrompt?.(text);
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setHistoryEdits({});
        updateInputValue("");
        if (inputRef.current) inputRef.current.style.height = "auto";
        setPendingAttachments([]);
        clearSelectedFile?.();
        userScrolledUpRef.current = false;
        const outgoing = allFilePaths.length > 0 ? buildOutgoingMessageMulti(text, allFilePaths) : text;
        await sendMessage(outgoing);
    }, [inputValue, submitLocked, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, updateInputValue, clearSelectedFile, sendMessage]);

    const applyInputValue = useCallback((nextValue: string) => {
        updateInputValue(nextValue);
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = nextValue.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [resizeInput, updateInputValue]);

    const isSelectionCollapsedAtBoundary = useCallback((direction: 'up' | 'down') => {
        const input = inputRef.current;
        if (!input) return false;
        const { selectionStart, selectionEnd, value } = input;
        if (selectionStart !== selectionEnd) return false;
        if (selectionStart == null || selectionEnd == null) return false;
        if (direction === 'up') {
            return !value.slice(0, selectionStart).includes("\n");
        }
        return !value.slice(selectionEnd).includes("\n");
    }, []);

    const recallHistory = useCallback((direction: 'up' | 'down') => {
        if (submittedPrompts.length === 0) return false;

        const currentEdits = historyEdits;
        const currentHistoryIndex = historyIndex;
        const currentInputValue = inputValue;

        const rememberCurrentEntry = () => {
            if (currentHistoryIndex < 0) return;
            setHistoryEdits(prev => ({ ...prev, [currentHistoryIndex]: currentInputValue }));
        };

        if (direction === 'up') {
            if (currentHistoryIndex >= 0) {
                rememberCurrentEntry();
            } else {
                setDraftBeforeHistory(currentInputValue);
            }
            const nextIndex = currentHistoryIndex < 0 ? submittedPrompts.length - 1 : Math.max(0, currentHistoryIndex - 1);
            setHistoryIndex(nextIndex);
            applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
            return true;
        }

        if (currentHistoryIndex < 0) return false;
        rememberCurrentEntry();
        if (currentHistoryIndex >= submittedPrompts.length - 1) {
            setHistoryIndex(-1);
            applyInputValue(draftBeforeHistory ?? "");
            return true;
        }
        const nextIndex = currentHistoryIndex + 1;
        setHistoryIndex(nextIndex);
        applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
        return true;
    }, [submittedPrompts, historyIndex, inputValue, draftBeforeHistory, historyEdits, applyInputValue]);

    const exitHistoryBrowsing = useCallback(() => {
        if (historyIndex < 0) return false;
        setHistoryIndex(-1);
        setHistoryEdits({});
        applyInputValue(draftBeforeHistory ?? "");
        setDraftBeforeHistory(null);
        return true;
    }, [historyIndex, draftBeforeHistory, applyInputValue]);

    const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
        const items = e.clipboardData?.items;
        if (!items) return;
        for (const item of Array.from(items)) {
            if (!item.type.startsWith("image/")) continue;
            e.preventDefault();
            const blob = item.getAsFile();
            if (!blob) continue;
            const ext = blob.type === "image/png" ? "png" : "jpg";
            try {
                const base64 = await new Promise<string>((resolve, reject) => {
                    const reader = new FileReader();
                    reader.onload = () => resolve(String(reader.result || "").split(",")[1] || "");
                    reader.onerror = reject;
                    reader.readAsDataURL(blob);
                });
                const filePath = await savePastedImage(base64, ext);
                const thumbnailDataUrl = URL.createObjectURL(blob);
                const fileName = filePath.split(/[/\\]/).pop() || "paste." + ext;
                setPendingAttachments(prev => [...prev, { filePath, thumbnailDataUrl, isImage: true, fileName, extension: "." + ext }]);
            } catch (err) {
                console.error("Failed to save pasted image:", err);
            }
            return;
        }
    }, []);

    useEffect(() => {
        if (prevSubmitLockedRef.current && !submitLocked && queue.length > 0) {
            const result = mergeAndFire();
            if (result) {
                const outgoing = buildOutgoingMessageMulti(result.mergedText, result.allFilePaths);
                recordSubmittedPrompt?.(result.mergedText);
                sendMessage(outgoing).catch(() => {});
            }
        }
        prevSubmitLockedRef.current = submitLocked;
    }, [submitLocked, queue.length, mergeAndFire, sendMessage, recordSubmittedPrompt]);

    const handleEditEntry = useCallback((id: string) => setEditingEntryId(id), []);
    const handleCancelEdit = useCallback(() => setEditingEntryId(null), []);
    const handleSaveEdit = useCallback((id: string, text: string, attachments: AttachmentInfo[]) => {
        updateEntry(id, text, attachments);
        setEditingEntryId(null);
    }, [updateEntry]);
    const handleCancel = useCallback(async () => {
        if (!cancelSession || cancelPending) return;
        const restoreSeq = ++cancelRestoreSeqRef.current;
        const previousInputValue = inputValue;
        setCancelPending(true);
        try {
            const { canceledText } = await cancelSession();
            if (cancelRestoreSeqRef.current !== restoreSeq) return;
            if (draftInputValue === previousInputValue) {
                updateInputValue(canceledText);
            }
            setHistoryIndex(-1);
            setDraftBeforeHistory(null);
            setHistoryEdits({});
            requestAnimationFrame(() => {
                resizeInput();
                inputRef.current?.focus();
            });
        } finally {
            setCancelPending(false);
        }
    }, [cancelPending, cancelSession, inputValue, resizeInput, updateInputValue]);

    const runGroupDiscussionAction = useCallback(async (name: string, action?: () => void | Promise<void>) => {
        if (!action || groupDiscussionBusy) return;
        setGroupDiscussionBusy(name);
        try {
            await action();
        } finally {
            setGroupDiscussionBusy("");
        }
    }, [groupDiscussionBusy]);

    const bindGroupDiscussionPress = useCallback((handler: () => void) => {
        if (!inline) return { onClick: handler };
        return {
            onMouseDown: (event: React.MouseEvent) => {
                event.preventDefault();
                event.stopPropagation();
                handler();
            },
        };
    }, [inline]);

    const lastAssistantIdx = useMemo(() => findLastIndex(otherMessages, m => m.role === 'assistant'), [otherMessages]);
    const renderedOtherMessages = useMemo(() => {
        return otherMessages.map((msg: ChatMessage, idx: number) => renderMessage(msg, executeAction, t, idx === lastAssistantIdx, savedFileLabel));
    }, [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel]);

    const renderedProgressMessages = useMemo(() => {
        return progressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel));
    }, [progressMessages, executeAction, t, savedFileLabel]);

    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : { display: "flex", flexDirection: "column", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" })
        : overlayStyle;

    return (
        <div data-testid="ai-panel-root" style={{ ...containerStyle, flexDirection: "row" }}>
            <div style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, height: "100%" }}>
            {/* Drag overlay (inline mode) */}
            {inline && !maximized && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            {/* Title bar */}
            <div
                data-testid="ai-title-bar"
                onDoubleClick={() => { if (inline) onToggleMaximize?.(); }}
                style={{
                display: "flex", alignItems: "center", justifyContent: "space-between",
                padding: "0 12px 0 10px", height: "38px",
                background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`,
                flexShrink: 0, gap: "8px",
                position: "relative", zIndex: 30000, overflow: "visible",
                ...(inline && !maximized ? { '--wails-draggable': 'drag' } as any : {}),
            }}>
                <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: 1 }}>
                    {!inline && (
                        <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}>
                            <span
                                style={{ ...dotBase, background: "#ff5f57" }}
                                onClick={onClose}
                                title={lang === "en" ? "Close" : "\u5173\u95ed"}
                            />
                        </div>
                    )}
                    <span style={{
                        color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em",
                        fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif",
                        overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                        transform: "translateY(-0.5px)",
                    }}>{title}</span>
                    {trialReflectEnabled && (
                        <span style={{
                            fontSize: "10px",
                            lineHeight: 1,
                            padding: "3px 6px",
                            borderRadius: "999px",
                            background: "rgba(99, 102, 241, 0.12)",
                            color: t.headingColor,
                            border: `1px solid ${t.titleBarBorder}`,
                            flexShrink: 0,
                        }}>
                            {lang === "en" ? "Trial+Reflect" : "\u8bd5\u9519\u53cd\u601d"}
                        </span>
                    )}
                </div>
                <div style={{ display: "flex", alignItems: "center", flexShrink: 0, paddingRight: inline ? 0 : 2, ...(inline ? { '--wails-draggable': 'no-drag', position: 'relative', zIndex: 30010 } as any : {}) }}>
                    <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "4px", alignItems: "center", paddingTop: 1 }}>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); projectSearch.toggle(); } } : { onClick: () => projectSearch.toggle() })}
                        style={getTitleBarToolButtonStyle(t, projectSearch.open ? "active" : "default")}
                        title={localizeText(lang, "Search tasks", "\u641c\u7d22\u4efb\u52a1")}
                    >
                        <span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F50D}"}</span>
                    </button>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); const next = !ttsEnabled; setTtsEnabled(next); SetTTSEnabled(next).catch(() => {}); } } : { onClick: () => { const next = !ttsEnabled; setTtsEnabled(next); SetTTSEnabled(next).catch(() => {}); } })}
                        style={getTitleBarToolButtonStyle(t, ttsEnabled ? "active" : "default")}
                        title={ttsEnabled ? localizeText(lang, "Voice readback ON - click to disable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5f00\u542f\uff0c\u70b9\u51fb\u5173\u95ed") : localizeText(lang, "Voice readback OFF - click to enable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5173\u95ed\uff0c\u70b9\u51fb\u5f00\u542f")}
                    >
                        <span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{ttsEnabled ? "\u{1F50A}" : "\u{1F507}"}</span>
                    </button>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); setThemeMode(themeMode === 'dark' ? 'light' : 'dark'); } } : { onClick: () => setThemeMode(themeMode === 'dark' ? 'light' : 'dark') })}
                        style={getTitleBarToolButtonStyle(t)}
                        title={themeMode === 'dark' ? localizeText(lang, "Switch to light mode", "\u5207\u6362\u5230\u666e\u901a\u6a21\u5f0f") : localizeText(lang, "Switch to dark mode", "\u5207\u6362\u5230\u6697\u9ed1\u6a21\u5f0f")}
                    >
                        <span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{themeMode === 'dark' ? "\u{1F319}" : "\u2600\uFE0F"}</span>
                    </button>
                    {groupDiscussion && (
                    <div style={{ position: "relative", zIndex: 30010 }}>
                        <button
                            className="ai-titlebar-tool"
                            {...(inline ? { onMouseDown: (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); setGroupDiscussionOpen(v => !v); } } : { onClick: () => setGroupDiscussionOpen(v => !v) })}
                            style={{
                                ...getTitleBarToolButtonStyle(t),
                                width: "auto",
                                minWidth: "72px",
                                padding: "0 8px",
                                gap: "5px",
                                color: groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "#047857" : "#92400e") : t.actionBtnColor,
                                borderColor: groupPendingInvites.length > 0 ? "#f59e0b" : undefined,
                            }}
                            title={lang === "en" ? "Group discussion" : "\u7fa4\u7ec4\u8ba8\u8bba"}
                        >
                            <span aria-hidden="true" style={{ fontSize: "13px", lineHeight: 1 }}>GD</span>
                            <span style={{ fontSize: "10px", lineHeight: 1, whiteSpace: "nowrap" }}>{groupDiscussionLabel}</span>
                            {groupPendingInvites.length > 0 && (
                                <span style={{
                                    minWidth: "14px",
                                    height: "14px",
                                    borderRadius: "999px",
                                    background: "#f59e0b",
                                    color: "white",
                                    fontSize: "9px",
                                    lineHeight: "14px",
                                    textAlign: "center",
                                    fontWeight: 700,
                                }}>{groupPendingInvites.length > 9 ? "9+" : groupPendingInvites.length}</span>
                            )}
                        </button>
                        {groupDiscussionOpen && (
                            <div style={{
                                position: "absolute",
                                right: 0,
                                top: "30px",
                                width: "min(280px, calc(100vw - 96px))",
                                maxWidth: "calc(100vw - 96px)",
                                padding: "12px",
                                borderRadius: "12px",
                                border: `1px solid ${t.titleBarBorder}`,
                                background: themeMode === 'dark' ? "#0f172a" : t.bg,
                                boxShadow: themeMode === 'dark' ? "0 22px 60px rgba(0, 0, 0, 0.72), 0 0 0 1px rgba(148, 163, 184, 0.16)" : "0 18px 45px rgba(15, 23, 42, 0.18)",
                                color: t.text,
                                zIndex: 30020,
                                '--wails-draggable': 'no-drag',
                            } as any}>
                                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px", marginBottom: "8px" }}>
                                    <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: "2px" }}>
                                        <strong style={{ fontSize: "12px" }}>{lang === "en" ? "Group Discussion" : "\u7fa4\u7ec4\u8ba8\u8bba"}</strong>
                                        <span style={{ fontSize: "10px", color: t.textMuted }}>{groupDiscussionScopeText}</span>
                                    </div>
                                    <button
                                        type="button"
                                        {...bindGroupDiscussionPress(() => setGroupDiscussionOpen(false))}
                                        aria-label={lang === "en" ? "Close group discussion panel" : "\u5173\u95ed\u7fa4\u7ec4\u8ba8\u8bba\u9762\u677f"}
                                        title={lang === "en" ? "Close" : "\u5173\u95ed"}
                                        style={{
                                            width: "22px",
                                            height: "22px",
                                            minWidth: "22px",
                                            borderRadius: "999px",
                                            border: `1px solid ${themeMode === 'dark' ? "rgba(148, 163, 184, 0.28)" : "rgba(148, 163, 184, 0.24)"}`,
                                            background: themeMode === 'dark' ? "rgba(15, 23, 42, 0.88)" : "rgba(255, 255, 255, 0.9)",
                                            color: t.textMuted,
                                            cursor: "pointer",
                                            display: "inline-flex",
                                            alignItems: "center",
                                            justifyContent: "center",
                                            padding: 0,
                                            fontSize: "14px",
                                            lineHeight: 1,
                                            fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
                                            flexShrink: 0,
                                            '--wails-draggable': 'no-drag',
                                        } as any}
                                    >
                                        <span aria-hidden="true">&times;</span>
                                    </button>
                                </div>
                                <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: "6px", marginBottom: "10px" }}>
                                    {[
                                        [lang === "en" ? "Experts" : "\u4e13\u5bb6", groupDiscussionStatus?.experts?.length ?? 0],
                                        [lang === "en" ? "Talks" : "\u8ba8\u8bba", groupDiscussionStatus?.discussions?.length ?? 0],
                                        [lang === "en" ? "Invites" : "\u9080\u8bf7", groupPendingInvites.length],
                                    ].map(([label, value]) => (
                                        <div key={String(label)} style={{ padding: "7px", borderRadius: "9px", background: themeMode === 'dark' ? "rgba(148, 163, 184, 0.14)" : "rgba(148, 163, 184, 0.10)", textAlign: "center", minWidth: 0 }}>
                                            <div style={{ fontSize: "14px", fontWeight: 700 }}>{value}</div>
                                            <div style={{ fontSize: "10px", color: t.textMuted }}>{label}</div>
                                        </div>
                                    ))}
                                </div>
                                {(groupActiveTalks > 0 || groupReadyTalks > 0 || groupWaitingTalks > 0 || groupStaleTalks > 0) && (
                                    <div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "8px", padding: "7px", borderRadius: "9px", background: themeMode === 'dark' ? "rgba(148, 163, 184, 0.12)" : "rgba(15, 23, 42, 0.04)" }}>
                                        {lang === "en" ? `Active ${groupActiveTalks} \u00b7 Ready ${groupReadyTalks} \u00b7 Waiting ${groupWaitingTalks} \u00b7 Stale ${groupStaleTalks}` : `\u8fdb\u884c\u4e2d ${groupActiveTalks} \u00b7 \u53ef\u6536\u5c3e ${groupReadyTalks} \u00b7 \u7b49\u5f85 ${groupWaitingTalks} \u00b7 \u8d85\u65f6 ${groupStaleTalks}`}
                                    </div>
                                )}
                                {groupDiscussionStatus?.error && (
                                    <div style={{ fontSize: "11px", color: "#b91c1c", marginBottom: "8px" }}>{String(groupDiscussionStatus.error)}</div>
                                )}
                                {groupPendingInvites.slice(0, 2).map((invite: any) => (
                                    <div key={invite.invite_id || invite.id} style={{ padding: "8px 0", borderTop: `1px solid ${t.divider}` }}>
                                        <div style={{ fontSize: "11px", fontWeight: 600, marginBottom: "2px" }}>{invite.topic || invite.consultation_id || (lang === "en" ? "Discussion invite" : "\u8ba8\u8bba\u9080\u8bf7")}</div>
                                        <div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "6px" }}>{invite.from_name || invite.from_id || "MaClaw"}</div>
                                        <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "6px" }}>
                                            <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: themeMode === 'dark' ? "#86efac" : "#047857", borderColor: themeMode === 'dark' ? "rgba(134, 239, 172, 0.45)" : "#86efac" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("accept", () => groupDiscussion.onAcceptInvite?.(invite.invite_id || invite.id)))}>{lang === "en" ? "Accept" : "\u63a5\u53d7"}</button>
                                            <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: themeMode === 'dark' ? "#fca5a5" : "#b91c1c", borderColor: themeMode === 'dark' ? "rgba(252, 165, 165, 0.45)" : "#fecaca" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("reject", () => groupDiscussion.onRejectInvite?.(invite.invite_id || invite.id)))}>{lang === "en" ? "Reject" : "\u62d2\u7edd"}</button>
                                        </div>
                                    </div>
                                ))}
                                <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "6px", marginTop: "10px" }}>
                                    <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder, opacity: groupDiscussionBusy ? 0.68 : 1, cursor: groupDiscussionBusy ? "default" : "pointer" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("refresh", groupDiscussion.onRefreshStatus))}>{groupDiscussionBusy === "refresh" ? (lang === "en" ? "Refreshing..." : "\u5237\u65b0\u4e2d...") : (lang === "en" ? "Refresh" : "\u5237\u65b0")}</button>
                                    <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder, opacity: groupDiscussionBusy ? 0.68 : (groupDiscussionEnabled ? 1 : 0.55), cursor: (groupDiscussionBusy || !groupDiscussionEnabled) ? "default" : "pointer" }} disabled={!!groupDiscussionBusy || !groupDiscussionEnabled} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("publish", groupDiscussion.onPublishProfile))}>{groupDiscussionBusy === "publish" ? (lang === "en" ? "Publishing..." : "\u53d1\u5e03\u4e2d...") : (lang === "en" ? "Publish" : "\u53d1\u5e03\u8eab\u4efd")}</button>
                                </div>
                            </div>
                        )}
                    </div>
                    )}
                    {onOpenTutorial && (
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })}
                        style={getTitleBarToolButtonStyle(t)}
                        title={lang === "en" ? "Tutorial" : "\u6559\u7a0b"}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            {"\u{1F4D6}"}
                        </span>
                    </button>
                    )}
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })}
                        style={getTitleBarToolButtonStyle(t)}
                        title={lang === "en" ? "Refresh news" : "\u5237\u65b0\u6d88\u606f"}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            {"\u21bb"}
                        </span>
                    </button>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })}
                        style={getTitleBarToolButtonStyle(t, "danger")}
                        title={lang === "en" ? "Clear history" : "\u6e05\u7a7a\u5386\u53f2"}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            {"\u{1F5D1}"}
                        </span>
                    </button>
                    </div>
                    <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && (
                    <button
                        className="ai-window-control"
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); onHideWindow(); }}
                        data-testid="ai-hide-toggle"
                        aria-label={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}
                        style={getWindowControlButtonStyle(t, "hide")}
                        title={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}
                    >
                        <span style={{ width: "10px", borderTop: "1.5px solid currentColor", transform: "translateY(4px)" }} />
                    </button>
                    )}
                    {showMaximizeToggle && (
                    <button
                        className="ai-window-control"
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); onToggleMaximize?.(); }}
                        data-testid="ai-maximize-toggle"
                        aria-label={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}
                        style={getWindowControlButtonStyle(t, "fullscreen", maximized)}
                        title={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}
                    >
                        <span style={{
                            position: "relative",
                            width: "12px",
                            height: "12px",
                            display: "inline-block",
                        }}>
                            <span style={{
                                position: "absolute",
                                inset: maximized ? "2px 0 0 2px" : 0,
                                border: "1.5px solid currentColor",
                                borderRadius: "1px",
                                background: "transparent",
                            }} />
                            {maximized && (
                                <span style={{
                                    position: "absolute",
                                    inset: "0 2px 2px 0",
                                    border: "1.5px solid currentColor",
                                    borderRadius: "1px",
                                    background: t.titleBarBg,
                                }} />
                            )}
                        </span>
                    </button>
                    )}
                    {!inline && (
                    <button
                        className="ai-window-control"
                        onClick={onClose}
                        style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor, fontSize: "14px" }}
                        title={lang === "en" ? "Close" : "\u5173\u95ed"}
                    >
                        {"x"}
                    </button>
                    )}
                    </div>
                </div>
            </div>

            {/* Chat area */}
            {workflowState.suggestMaximize && !maximized && inline && onToggleMaximize && (
                <div data-testid="ai-workflow-maximize-suggestion" style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "10px", padding: "8px 14px", background: themeMode === 'dark' ? "rgba(99,102,241,0.16)" : "linear-gradient(90deg, rgba(99,102,241,0.08), rgba(59,130,246,0.08))", borderBottom: `1px solid ${t.titleBarBorder}`, fontSize: "13px", flexShrink: 0 }}>
                    <span style={{ color: t.text }}>{localizeText(lang, "Workflow is starting. Fullscreen is recommended.", "\u6d41\u7a0b\u5373\u5c06\u5f00\u59cb\uff0c\u5168\u5c4f\u6a21\u5f0f\u4f53\u9a8c\u66f4\u597d")}</span>
                    <div style={{ display: "flex", gap: "6px", flexShrink: 0 }}>
                        <button type="button" onClick={() => { onToggleMaximize(); dismissMaximizeSuggestion(); }} style={{ padding: "4px 12px", fontSize: "12px", border: `1px solid ${t.inputBarBorder}`, borderRadius: "4px", background: t.fieldBg, color: t.headingColor, cursor: "pointer", fontWeight: 500 }}>{localizeText(lang, "Fullscreen", "\u5168\u5c4f")}</button>
                        <button type="button" onClick={dismissMaximizeSuggestion} style={{ padding: "4px 8px", fontSize: "12px", border: "none", background: "transparent", color: t.textMuted, cursor: "pointer" }}>{localizeText(lang, "Dismiss", "\u5ffd\u7565")}</button>
                    </div>
                </div>
            )}

            <BufferQueuePanel
                queue={queue}
                lang={lang}
                theme={{ bg: t.bg, text: t.text, textMuted: t.textMuted, headingColor: t.headingColor, inputBarBg: t.inputBarBg, inputBarBorder: t.inputBarBorder, codeBlockBg: t.codeBlockBg, codeBlockBorder: t.codeBlockBorder, divider: t.divider }}
                editingEntryId={editingEntryId}
                onEdit={handleEditEntry}
                onCancelEdit={handleCancelEdit}
                onSaveEdit={handleSaveEdit}
                onDelete={removeEntry}
                onReorder={reorderEntry}
            />
            <ProjectSearchPanel
                search={projectSearch}
                lang={lang}
                theme={t}
                inline={!!inline}
                onProjectSwitch={handleProjectSearchSwitch}
                onTaskPrefsChanged={onTaskPrefsChanged}
            />

            <div
                ref={outputContainerRef}
                style={{
                    flex: 1, minHeight: 0, maxHeight: "none",
                    padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5,
                    overflowY: "auto", overflowX: "hidden", textAlign: "left",
                    color: t.text, background: t.bg,
                    fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
                    whiteSpace: "pre-wrap", wordBreak: "break-all",
                }}
                onScroll={handleScroll}
            >
                {onboardingIncomplete ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "16px" }}>
                        <div style={{ color: t.textMuted, fontSize: "13px" }}>
                            {lang === "en" ? "Setup not completed" : "\u8bbe\u7f6e\u672a\u5b8c\u6210"}
                        </div>
                        <button
                            onClick={onOpenOnboarding}
                            style={{
                                padding: "10px 28px", fontSize: "15px", fontWeight: 600,
                                background: "linear-gradient(135deg, #6366f1, #8b5cf6)",
                                color: "#fff", border: "none", borderRadius: "8px",
                                cursor: "pointer", transition: "opacity 0.2s",
                            }}
                            onMouseEnter={e => (e.currentTarget.style.opacity = "0.85")}
                            onMouseLeave={e => (e.currentTarget.style.opacity = "1")}
                        >
                            {lang === "en" ? "Complete Setup" : "\u5b8c\u6210\u8bbe\u7f6e"}
                        </button>
                    </div>
                ) : !ready ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "12px" }}>
                        <div style={{
                            border: `3px solid ${t.inputBarBorder}`,
                            borderTop: `3px solid ${t.promptColor}`,
                            borderRadius: "50%",
                            animation: "maclaw-spin 0.8s linear infinite",
                        }} />
                        <div style={{ color: t.textMuted, fontSize: "12px" }}>
                            {initLabel}
                        </div>
                    </div>
                ) : messages.length === 0 ? (
                    <span style={{ color: t.emptyHint }}>
                        {lang === "en" ? "Ask me anything..." : "\u6709\u4ec0\u4e48\u53ef\u4ee5\u5e2e\u4f60\u7684\uff1f"}
                    </span>
                ) : (
                    <>
                        {pinnedNews.length > 0 && (
                            <div style={{
                                display: 'grid',
                                gridTemplateColumns: pinnedNews.length >= 2 ? '1fr 1fr' : '1fr',
                                gap: '6px',
                                marginBottom: '6px',
                            }}>
                                {pinnedNews.map(msg => {
                                    const news = msg.news;
                                    if (!news) return null;
                                    const tooltipText = news.title + (news.body ? '\n' + news.body : '');
                                    return (
                                    <div key={msg.id} className="pinned-news-card" title={tooltipText} style={{
                                        padding: "6px 8px",
                                        borderRadius: "6px",
                                        background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                                        borderLeft: `3px solid ${t.promptColor}`,
                                        color: t.text,
                                        fontSize: "11px",
                                        lineHeight: "1.4",
                                        overflow: "hidden",
                                    }}>
                                        <div style={{
                                            overflow: "hidden",
                                            textOverflow: "ellipsis",
                                            whiteSpace: "nowrap",
                                            fontWeight: 600,
                                        }}>
                                            <span>{news.icon} </span>
                                            {renderInlineMarkdown(news.title, t)}
                                        </div>
                                        {news.body && (
                                        <div style={{
                                            overflow: "hidden",
                                            display: "-webkit-box",
                                            WebkitLineClamp: 2,
                                            WebkitBoxOrient: "vertical" as any,
                                            marginTop: "2px",
                                            color: t.textMuted,
                                        }}>
                                            {renderInlineMarkdown(news.body, t)}
                                        </div>
                                        )}
                                    </div>
                                    );
                                })}
                            </div>
                        )}
                        {renderedOtherMessages}
                        {renderedProgressMessages}
                    </>
                )}
                {showThinkingState && (
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                {showProcessingState && (
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {processingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* Input bar */}
            <div data-testid="ai-input-resize-handle" onMouseDown={startInputResize} style={{ height: "6px", cursor: "ns-resize", background: "transparent", borderTop: `1px solid ${t.divider}`, flexShrink: 0 }} />
            {workflowState.phaseDocuments.size > 0 && !workflowState.docsBarDismissed && (
                <div data-testid="ai-workflow-docs-bar" style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 14px", borderTop: `1px solid ${t.divider}`, background: t.bg, flexShrink: 0, flexWrap: "wrap" }}>
                    <span style={{ fontSize: "11px", color: t.textMuted, flexShrink: 0 }}>{"docs"}</span>
                    {Array.from(workflowState.phaseDocuments.keys()).map(pid => (
                        <button key={pid} type="button" onClick={() => openDocPreview(pid)} style={{ padding: "3px 8px", fontSize: "11px", borderRadius: "999px", border: `1px solid ${workflowState.splitMode && workflowState.currentPhaseID === pid ? t.headingColor : t.titleBarBorder}`, background: workflowState.splitMode && workflowState.currentPhaseID === pid ? (themeMode === 'dark' ? "rgba(99,102,241,0.18)" : "rgba(99,102,241,0.08)") : "transparent", color: workflowState.splitMode && workflowState.currentPhaseID === pid ? t.headingColor : t.textMuted, cursor: "pointer" }}>
                            {pid}
                        </button>
                    ))}
                    <button type="button" onClick={handleSelectWorkflowDir} style={{ padding: "3px 8px", fontSize: "11px", borderRadius: "999px", border: `1px solid ${t.titleBarBorder}`, background: "transparent", color: t.textMuted, cursor: "pointer" }} title={workflowState.workingDir || undefined}>{workflowState.workingDir ? workflowState.workingDir.split(/[/\\]/).pop() : localizeText(lang, "Working dir", "\u5de5\u4f5c\u76ee\u5f55")}</button>
                    <button type="button" onClick={dismissDocsBar} style={{ marginLeft: "auto", border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", fontSize: "12px" }}>{"x"}</button>
                </div>
            )}
            <div style={{
                display: "flex", flexDirection: "column", gap: "6px",
                padding: "8px 12px", paddingBottom: "max(8px, env(safe-area-inset-bottom))",
                background: t.inputBarBg, borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
                flexShrink: 0,
                ...(inline ? {} : { margin: "0 10px 10px 10px", borderRadius: "8px", border: `1.5px solid ${t.inputBarBorder}` }),
            }}>
                {pendingAttachments.length > 0 && (
                    <div data-testid="ai-pending-attachments" style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
                        {pendingAttachments.map((att, index) => {
                            const showImageOnly = !!att.thumbnailDataUrl && att.isImage;
                            return (
                                <div
                                    key={`${att.filePath}-${index}`}
                                    style={{
                                        display: "flex",
                                        alignItems: "center",
                                        gap: showImageOnly ? "5px" : "6px",
                                        maxWidth: showImageOnly ? "72px" : "220px",
                                        padding: showImageOnly ? "4px 5px" : "5px 7px",
                                        borderRadius: "7px",
                                        background: t.codeBlockBg,
                                        border: `1px solid ${t.codeBlockBorder}`,
                                        color: t.text,
                                        fontSize: "11px",
                                    }}
                                    title={att.filePath}
                                >
                                    {att.thumbnailDataUrl ? (
                                        <img
                                            src={att.thumbnailDataUrl}
                                            alt={att.fileName || "pasted image"}
                                            style={{ width: "34px", height: "34px", objectFit: "cover", borderRadius: "4px", flexShrink: 0 }}
                                        />
                                    ) : (
                                        <span>{"file"}</span>
                                    )}
                                    {!showImageOnly && (
                                        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{att.fileName}</span>
                                    )}
                                    <button type="button" onClick={() => setPendingAttachments(prev => prev.filter((_, i) => i !== index))} style={{ border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", padding: showImageOnly ? "0 2px" : undefined }}>{"x"}</button>
                                </div>
                            );
                        })}
                    </div>
                )}
                {selectedFilePaths.length > 0 && (
                    <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                        {selectedFilePaths.map((filePath: string, index: number) => {
                            const fileName = filePath.split(/[/\\]/).pop() || filePath;
                            return (
                                <div key={filePath + index} style={{
                                    display: "flex",
                                    alignItems: "center",
                                    gap: "8px",
                                    minWidth: 0,
                                    padding: "6px 8px",
                                    borderRadius: "6px",
                                    background: t.codeBlockBg,
                                    border: `1px solid ${t.codeBlockBorder}`,
                                    color: t.text,
                                    fontSize: "12px",
                                }}>
                                    <span style={{ color: t.pathColor, flexShrink: 0 }}>{isImageFilePath(filePath) ? "img" : "file"}</span>
                                    <div style={{ minWidth: 0, flex: 1 }} title={filePath}>
                                        <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", fontWeight: 600 }}>{fileName}</div>
                                        <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", color: t.textMuted, fontSize: "11px" }}>{filePath}</div>
                                    </div>
                                    <button
                                        type="button"
                                        onClick={() => removeSelectedFile ? removeSelectedFile(index) : clearSelectedFile?.()}
                                        disabled={cancelPending}
                                        style={{
                                            ...baseActionBtnStyle,
                                            color: t.errorText,
                                            border: `1px solid ${t.errorBorder}`,
                                            background: "transparent",
                                            opacity: cancelPending ? 0.5 : 1,
                                        }}
                                        title={lang === "en" ? "Clear selected file" : "\u6e05\u9664\u5df2\u9009\u6587\u4ef6"}
                                    >
                                        {"x"}
                                    </button>
                                </div>
                            );
                        })}
                    </div>
                )}
                <div style={{
                    display: "flex", alignItems: "flex-end", gap: "8px",
                }}>
                    <span style={{
                        color: t.promptColor, fontFamily: "Consolas, monospace",
                        fontSize: "13px", flexShrink: 0, userSelect: "none",
                        paddingBottom: "8px",
                    }}>{">"}</span>
                    <textarea
                        ref={inputRef}
                        data-testid="ai-input"
                        disabled={!ready || cancelPending}
                        readOnly={cancelPending}
                        aria-readonly={cancelPending}
                        style={{
                            flex: 1, minWidth: 0, background: "transparent",
                            border: "none", outline: "none", color: t.inputText,
                            fontFamily: "Consolas, 'Courier New', monospace",
                            fontSize: "14px", padding: "8px 0",
                            resize: "none", overflow: "auto",
                            minHeight: "36px", maxHeight: inputAreaHeight ? `${inputAreaHeight}px` : "120px",
                            lineHeight: 1.4,
                            opacity: (!ready || cancelPending) ? 0.5 : 1,
                            cursor: cancelPending ? "default" : "text",
                        }}
                        rows={1}
                        value={inputValue}
                        onChange={(e) => {
                            if (historyIndex >= 0) {
                                setHistoryEdits(prev => ({ ...prev, [historyIndex]: e.target.value }));
                            }
                            updateInputValue(e.target.value);
                            resizeInput();
                        }}
                        onPaste={handlePaste}
                        onCompositionStart={() => setComposing(true)}
                        onCompositionEnd={() => setComposing(false)}
                        onKeyDown={(e) => {
                            if (composing) return;
                            if (e.key === "ArrowUp" && isSelectionCollapsedAtBoundary('up')) {
                                if (recallHistory('up')) {
                                    e.preventDefault();
                                    return;
                                }
                            }
                            if (e.key === "ArrowDown" && isSelectionCollapsedAtBoundary('down')) {
                                if (recallHistory('down')) {
                                    e.preventDefault();
                                    return;
                                }
                            }
                            if (e.key === "Escape") {
                                if (exitHistoryBrowsing()) {
                                    e.preventDefault();
                                }
                                return;
                            }
                            if (e.key === "Enter" && !e.shiftKey) {
                                e.preventDefault();
                                handleSend();
                            }
                        }}
                        placeholder={placeholderText}
                        autoCapitalize="off"
                        autoCorrect="off"
                        spellCheck={false}
                    />
                    <button
                        type="button"
                        onClick={browseFile}
                        disabled={!ready || inputLocked}
                        style={{
                            ...getInputActionButtonStyle(t, themeMode, "attach", !ready || inputLocked),
                            marginBottom: "4px",
                        }}
                        title={localizeText(lang, "Choose file", "\u9009\u62e9\u6587\u4ef6")}
                    >
                        <AssistantInputIcon name="paperclip" />
                    </button>
                    <button
                        type="button"
                        onClick={handleVoiceClick}
                        onPointerDown={handleVoicePointerDown}
                        onPointerUp={finishVoicePointer}
                        onPointerCancel={finishVoicePointer}
                        onPointerLeave={handleVoicePointerLeave}
                        onContextMenu={(e) => e.preventDefault()}
                        disabled={!ready || voiceInput.state === "transcribing" || !voiceInput.asrReady}
                        data-testid="ai-voice-input"
                        style={{
                            ...getInputActionButtonStyle(
                                t,
                                themeMode,
                                voiceInput.state === "listening" ? "voiceHold" : "voice",
                                !ready || !voiceInput.asrReady || voiceInput.state === "transcribing",
                            ),
                            position: "relative",
                            marginBottom: "4px",
                            touchAction: "none",
                            overflow: "hidden",
                        }}
                        title={
                            !voiceInput.asrReady
                                ? localizeText(lang, "Voice input unavailable - enable ASR model first", "\u8bed\u97f3\u8f93\u5165\u4e0d\u53ef\u7528\uff0c\u8bf7\u5148\u542f\u7528 ASR \u6a21\u578b")
                                : voiceInput.state === "listening"
                                ? localizeText(lang, "Listening - click to stop", "\u76d1\u542c\u4e2d\uff0c\u70b9\u51fb\u505c\u6b62")
                                : voiceInput.state === "transcribing"
                                ? localizeText(lang, "Transcribing...", "\u8bc6\u522b\u4e2d...")
                                : localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")
                        }
                        aria-label={localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")}
                    >
                        {voiceInput.state === "transcribing" ? (
                            <span aria-hidden="true" style={{ display: "inline-block", width: "14px", height: "14px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} />
                        ) : voiceInput.state === "listening" ? (
                            <VoiceLevelVisualizer
                                onAudioLevelRef={voiceInput.onAudioLevelRef}
                                isSpeaking={voiceInput.isSpeaking}
                                themeColor="#ffffff"
                                speakingColor={themeMode === 'dark' ? "#fecaca" : "#dc2626"}
                            />
                        ) : (
                            <AssistantInputIcon name="mic" />
                        )}
                    </button>
                    {voiceInput.error && (
                        <span style={{ color: t.errorText, fontSize: "11px", alignSelf: "center", maxWidth: "160px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={voiceInput.error}>{voiceInput.error}</span>
                    )}
                    {isBusy && cancelSession ? (
                        <button
                            type="button"
                            onClick={handleCancel}
                            data-testid="ai-cancel-progress"
                            style={{
                                ...getInputActionButtonStyle(t, themeMode, "cancel"),
                                marginBottom: "4px",
                            }}
                            title={localizeText(lang, "Cancel", "\u53d6\u6d88")}
                            aria-label={localizeText(lang, "Cancel", "\u53d6\u6d88")}
                        >
                            {showBusySpinner ? (
                                <span
                                    aria-hidden="true"
                                    style={{
                                        width: "18px",
                                        height: "18px",
                                        borderRadius: "50%",
                                        border: `2px solid ${themeMode === 'dark' ? "rgba(221, 214, 254, 0.24)" : "rgba(79, 70, 229, 0.18)"}`,
                                        borderTopColor: themeMode === 'dark' ? "#ddd6fe" : "#4f46e5",
                                        borderRightColor: themeMode === 'dark' ? "#ddd6fe" : "#4f46e5",
                                        animation: "ai-spinner-spin 0.8s linear infinite",
                                    }}
                                />
                            ) : (
                                <AssistantInputIcon name="stop" />
                            )}
                            <span style={{ position: "absolute", opacity: 0, pointerEvents: "none" }}>
                                {localizeText(lang, "Cancel", "\u53d6\u6d88")}
                            </span>
                        </button>
                    ) : (
                        <button
                            type="button"
                            onClick={handleSend}
                            disabled={!canSend}
                            style={{
                                ...getInputActionButtonStyle(t, themeMode, canSend ? "send" : "neutral", !canSend),
                                marginBottom: "4px",
                            }}
                            title={localizeText(lang, "Send", "\u53d1\u9001")}
                        >
                            {isBusy ? (
                                <span style={{ width: "16px", height: "16px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} />
                            ) : (
                                <AssistantInputIcon name="cornerDownLeft" />
                            )}
                        </button>
                    )}
                </div>
            </div>
            </div>
            {showWorkflowPreview && (
                <div style={{ flex: Math.max(0.2, 1 - splitRatio), minWidth: 0, height: "100%" }}>
                    <WorkflowDocPreview
                        phaseDocuments={workflowState.phaseDocuments}
                        currentPhaseID={workflowState.currentPhaseID}
                        gateResults={workflowState.gateResults}
                        onClose={closeDocPreview}
                        onToggleMaximize={inline ? onToggleMaximize : undefined}
                        onResizeStart={startPreviewResize}
                        theme={{
                            bg: t.bg,
                            text: t.text,
                            textMuted: t.textMuted,
                            border: t.divider,
                            headerBg: t.titleBarBg,
                            accentColor: t.headingColor,
                            accentBg: themeMode === 'dark' ? "rgba(99,102,241,0.15)" : "rgba(99,102,241,0.08)",
                            codeBg: t.codeBg,
                            codeText: t.codeText,
                            codeBlockBg: t.codeBlockBg,
                            codeBlockBorder: t.codeBlockBorder,
                            headingColor: t.headingColor,
                            linkColor: t.linkColor,
                            quoteBorder: t.quoteBorder,
                            quoteText: t.quoteText,
                            quoteBg: themeMode === 'dark' ? "rgba(99,102,241,0.08)" : "rgba(99,102,241,0.04)",
                        }}
                    />
                </div>
            )}
            {showCodePreview && (
                <div style={{ flex: Math.max(0.2, 1 - splitRatio), minWidth: 0, height: "100%" }}>
                    <CodePreviewPanel
                        files={codePreviewState.files}
                        activeFilePath={codePreviewState.activeFilePath}
                        onSelectFile={selectCodeFile}
                        onClose={closeCodePreview}
                        onResizeStart={startPreviewResize}
                        onToggleMaximize={inline ? onToggleMaximize : undefined}
                        theme={themeMode === 'dark' ? darkCodePreviewTheme : lightCodePreviewTheme}
                    />
                </div>
            )}
        </div>
    );
}
