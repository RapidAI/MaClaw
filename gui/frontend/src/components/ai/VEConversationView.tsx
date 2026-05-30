import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { MessageContentRenderer } from "./MessageContentRenderer";
import type { Theme } from "./aiAssistantPanelTheme";
import { AssistantInputComposer } from "./AssistantInputComposer";
import { MentionPopover, useMentionKeyboard, type MentionParticipant } from "./MentionPopover";
import { getParticipantColor } from "./VEGroupChat";
import { LEGACY_LOCAL_AI_PARTICIPANT_ID, LOCAL_AI_DISPLAY_NAME_EN, LOCAL_AI_DISPLAY_NAME_ZH_HANS, LOCAL_AI_DISPLAY_NAME_ZH_HANT, isLocalAIName, looksLikeRawParticipantId, normalizeParticipantId } from "./localAIIdentity";
import { veStatusEventInfo } from "./veStatusEvent";
import { useAssistantInputHistory } from "./useAssistantInputHistory";
import { usePastedImageAttachments } from "./usePastedImageAttachments";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";

type WailsAppModule = typeof import("../../../wailsjs/go/main/App");

let wailsAppModulePromise: Promise<WailsAppModule> | null = null;

function getWailsAppModule(): Promise<WailsAppModule> {
    if (!wailsAppModulePromise) {
        wailsAppModulePromise = import("../../../wailsjs/go/main/App");
    }
    return wailsAppModulePromise;
}

// --- Types ---

export interface VEMessage {
    id: string;
    role: "user" | "assistant";
    content: string;
    timestamp: number;
    fromId?: string;
    fromName?: string;
    /** Marks a message as failed to send */
    sendFailed?: boolean;
    /** Attachment metadata for display */
    attachments?: VEMessageAttachment[];
}

export interface VEMessageAttachment {
    type: "text" | "image" | "file";
    filename: string;
    mimeType?: string;
    /** For images: URL to display */
    fileUrl?: string;
    /** Downloaded local path, when available */
    localPath?: string;
    /** For files: size in bytes */
    sizeBytes?: number;
}

type QueuedVEMessage = {
    id: string;
    content: string;
    message: VEMessage;
    filePaths?: string[];
    attachmentNames?: string[];
};

type VEHistoryAttachment = {
    filename?: string;
    Filename?: string;
    mime_type?: string;
    mimeType?: string;
    MimeType?: string;
    file_url?: string;
    fileUrl?: string;
    FileURL?: string;
    local_path?: string;
    localPath?: string;
    LocalPath?: string;
    size_bytes?: number | string;
    sizeBytes?: number | string;
    SizeBytes?: number | string;
};

type VEHistoryMessage = {
    id?: string;
    ID?: string;
    from_id?: string;
    fromId?: string;
    FromID?: string;
    from_name?: string;
    fromName?: string;
    FromName?: string;
    kind?: string;
    Kind?: string;
    content?: string;
    Content?: string;
    created_at?: string;
    createdAt?: string;
    CreatedAt?: string;
    attachments?: unknown;
    Attachments?: unknown;
    text_attachments?: VEHistoryAttachment[];
    textAttachments?: VEHistoryAttachment[];
    TextAttachments?: VEHistoryAttachment[];
    image_attachments?: VEHistoryAttachment[];
    imageAttachments?: VEHistoryAttachment[];
    ImageAttachments?: VEHistoryAttachment[];
    file_attachments?: VEHistoryAttachment[];
    fileAttachments?: VEHistoryAttachment[];
    FileAttachments?: VEHistoryAttachment[];
};

type VEHistoryDetail = {
    discussion?: {
        local_relation?: string;
    };
    session?: {
        participants?: Array<{ id?: string; ID?: string; role_code?: string; roleCode?: string; RoleCode?: string }>;
        messages?: VEHistoryMessage[];
        Messages?: VEHistoryMessage[];
    };
    messages?: VEHistoryMessage[];
    Messages?: VEHistoryMessage[];
};

export interface VEConversationState {
    sessionId: string | null;
    messages: VEMessage[];
    streaming: boolean;
    streamContent: string;
    streamFromId: string;
    streamFromName: string;
    streamAttachments: VEMessageAttachment[];
    error: VEConversationError | null;
    connectionState: "connected" | "disconnected" | "reconnecting";
    reconnectAttempt: number;
}

export type VEConversationError =
    | { type: "hub_disconnected"; message: string }
    | { type: "ve_offline"; message: string }
    | { type: "auth_pending"; message: string }
    | { type: "access_denied"; message: string }
    | { type: "send_failed"; message: string }
    | { type: "session_timeout"; message: string };

export interface VEConversationViewProps {
    veId: string;
    veName: string;
    avatarDataURL?: string;
    theme: Theme;
    lang?: string;
    /** Initial online status of the VE. Defaults to true (optimistic). Updated via ve:status_change events. */
    initialOnlineStatus?: "online" | "offline";
    /** Pre-existing session ID to resume (sticky session). Skips initiation if provided. */
    existingSessionId?: string;
    /** Pre-existing messages to restore on remount (tab switch). */
    initialMessages?: VEMessage[];
    /** Pre-existing input text to restore on remount (tab switch). */
    initialInputText?: string;
    /** View-only mode: history/invited sessions can render messages without allowing edits. */
    readOnly?: boolean;
    /** Incrementing signal from the tab manager to clear the current transcript and start fresh. */
    clearSignal?: number;
    /** Override for testing Wails bindings. */
    initiateConversation?: (veId: string) => Promise<{ session_id: string; ve_id: string; ve_name: string }>;
    initiateGroupConversation?: (veIds: string[]) => Promise<{ session_id: string; ve_id: string; ve_name: string }>;
    registerLocalExecutorInGroup?: (sessionId: string) => Promise<unknown>;
    sendMessage?: (sessionId: string, content: string) => Promise<void>;
    sendGroupMessage?: (sessionId: string, content: string, mentionedIds: string[]) => Promise<void>;
    sendMessageWithAttachments?: (sessionId: string, content: string, filePaths: string[]) => Promise<void>;
    sendGroupMessageWithAttachments?: (sessionId: string, content: string, mentionedIds: string[], filePaths: string[]) => Promise<void>;
    closeSession?: (sessionId: string) => Promise<void>;
    /** Group chat participants for @mention (empty array = no mention support) */
    participants?: MentionParticipant[];
    /** External @mention insert trigger (from right-click "Talk to" in participant panel) */
    externalMentionInsert?: { name: string; timestamp: number } | null;
    /** Notifies the parent as soon as a backend session is known. */
    onSessionIdChange?: (sessionId: string) => void;
    /** Notifies the parent that the visible transcript and cached session were explicitly cleared. */
    onConversationCleared?: () => void;
}

/**
 * Imperative handle exposed by VEConversationView via useImperativeHandle.
 * Used by the parent to snapshot conversation state on tab switch (unmount).
 */
export interface VEConversationHandle {
    getState: () => { messages: VEMessage[]; sessionId: string | null; inputText: string };
}

// --- Constants ---

const SESSION_TIMEOUT_MS = 5000;
const MIN_AGENT_TIMEOUT_SEC = 240;
const DEFAULT_AGENT_TIMEOUT_SEC = 600;
const MAX_AGENT_TIMEOUT_SEC = 600;
const LOCAL_CONFIG_CHANGED_EVENT = "maclaw-config-changed";
const RECONNECT_DELAYS = [2000, 4000, 8000, 16000, 30000]; // exponential backoff
const MAX_RECONNECT_RETRIES = 5;
const MENTION_TRIGGER_PATTERN = /(^|[^A-Za-z0-9_.-])@([^\s@]*)$/;
const VE_PROMPT_HISTORY_STORAGE_PREFIX = "ve-conversation-prompt-history:";
const MAX_VE_PROMPT_HISTORY = 100;

// --- Helpers ---

/** Generate a unique message ID */
function generateMsgId(): string {
    return `msg-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

/** Create a session with timeout */
function createSessionWithTimeout<T>(
    promise: Promise<T>,
    timeoutMs: number
): Promise<T> {
    return new Promise<T>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error("session_timeout")), timeoutMs);
        promise.then(
            (value) => {
                clearTimeout(timeout);
                resolve(value);
            },
            (error) => {
                clearTimeout(timeout);
                reject(error);
            }
        );
    });
}

function normalizeMentionParticipantId(value: string | undefined): string {
    return normalizeParticipantId(value);
}

function participantNameById(participants: MentionParticipant[] | undefined, id?: string): string {
    const normalized = normalizeMentionParticipantId(id);
    if (!normalized || !participants?.length) return "";
    return participants.find((p) => normalizeMentionParticipantId(p.id) === normalized)?.name || "";
}


function readableSpeakerName(
    candidate: string | undefined,
    fromId: string | undefined,
    participants: MentionParticipant[] | undefined,
    fallback: string,
): string {
    const name = String(candidate || "").trim();
    const id = String(fromId || "").trim();
    const participantName = participantNameById(participants, id);
    if (name && name !== id && !looksLikeRawParticipantId(name)) return name;
    return participantName || fallback;
}

function readableConversationPartnerName(name: string, id: string, isZh: boolean): string {
    const candidate = String(name || "").trim();
    const normalizedId = String(id || "").trim();
    if (candidate && candidate !== normalizedId && !looksLikeRawParticipantId(candidate)) return candidate;
    return isZh ? "\u6570\u5b57\u5458\u5de5" : "Digital employee";
}

function participantColorById(participants: MentionParticipant[] | undefined, id: string | undefined, fallback: string): string {
    const normalized = normalizeMentionParticipantId(id);
    const index = participants?.findIndex((p) => normalizeMentionParticipantId(p.id) === normalized) ?? -1;
    return index >= 0 ? getParticipantColor(index) : fallback;
}

function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isExplicitVEHistoryResetCommand(content: string): boolean {
    const trimmed = String(content || "").trim().toLowerCase();
    return trimmed === "/new" || trimmed === "/reset" || trimmed === "/clear";
}

function hasMention(content: string, label: string): boolean {
    const escaped = escapeRegExp(label);
    return new RegExp(`(^|[^A-Za-z0-9_.-])@${escaped}(?=$|[^A-Za-z0-9_.-])`).test(content);
}

function mentionLabelsForParticipant(participant: MentionParticipant): string[] {
    const labels = new Set<string>();
    const name = String(participant.name || "").trim();
    if (name) labels.add(name);
    if (normalizeMentionParticipantId(participant.id) === LEGACY_LOCAL_AI_PARTICIPANT_ID || isLocalAIName(name)) {
        labels.add(LOCAL_AI_DISPLAY_NAME_EN);
        labels.add(LOCAL_AI_DISPLAY_NAME_ZH_HANS);
        labels.add(LOCAL_AI_DISPLAY_NAME_ZH_HANT);
        labels.add("\u672c\u5730AI");
        labels.add("\u672c\u5730 AI");
        labels.add("\u672c\u673aAI");
        labels.add("\u672c\u673a AI");
        labels.add("\u672c\u6a5f AI");
        labels.add("\u672c\u5730\u667a\u80fd\u4f53");
        labels.add("\u672c\u673a\u667a\u80fd\u4f53");
    }
    return [...labels];
}

function mentionedParticipantIds(content: string, participants: MentionParticipant[] | undefined): string[] {
    if (!participants?.length) return [];
    const mentioned = new Set<string>();
    for (const participant of participants) {
        if (mentionLabelsForParticipant(participant).some((label) => hasMention(content, label))) {
            const id = String(participant.id || "").trim();
            if (id) mentioned.add(id);
        }
    }
    return [...mentioned];
}

function isLocalGroupParticipant(participant: MentionParticipant): boolean {
    const id = normalizeMentionParticipantId(participant.id);
    return id === LEGACY_LOCAL_AI_PARTICIPANT_ID || isLocalAIName(participant.name);
}

function groupSessionVEIds(primaryVEId: string, participants: MentionParticipant[] | undefined): string[] {
    const ids: string[] = [];
    const add = (value: string | undefined) => {
        const id = String(value || "").trim();
        if (!id) return;
        const normalized = normalizeMentionParticipantId(id);
        if (ids.some(existing => normalizeMentionParticipantId(existing) === normalized)) return;
        ids.push(id);
    };
    add(primaryVEId);
    for (const participant of participants || []) {
        if (isLocalGroupParticipant(participant)) continue;
        add(participant.id);
    }
    return ids;
}

// --- Component ---

export const VEConversationView = forwardRef<VEConversationHandle, VEConversationViewProps>(function VEConversationView({
    veId,
    veName,
    avatarDataURL,
    theme,
    lang,
    initialOnlineStatus,
    existingSessionId,
    initialMessages,
    initialInputText,
    readOnly = false,
    clearSignal = 0,
    initiateConversation,
    initiateGroupConversation,
    registerLocalExecutorInGroup,
    sendMessage,
    sendGroupMessage,
    sendMessageWithAttachments,
    sendGroupMessageWithAttachments,
    closeSession,
    participants,
    externalMentionInsert,
    onSessionIdChange,
    onConversationCleared,
}, ref) {
    const [state, setState] = useState<VEConversationState>({
        sessionId: existingSessionId || null,
        messages: initialMessages || [],
        streaming: false,
        streamContent: "",
        streamFromId: "",
        streamFromName: "",
        streamAttachments: [],
        error: null,
        connectionState: existingSessionId ? "connected" : "connected",
        reconnectAttempt: 0,
    });
    const [inputText, setInputText] = useState(initialInputText || "");
    const [mentionOpen, setMentionOpen] = useState(false);
    const [mentionQuery, setMentionQuery] = useState("");
    const [mentionStart, setMentionStart] = useState(-1);
    const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
    const [sending, setSending] = useState(false);
    const [awaitingReplyVisible, setAwaitingReplyVisible] = useState(false);
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments();
    const [visibleQueue, setVisibleQueue] = useState<QueuedVEMessage[]>([]);
    const [promptHistoryState, setPromptHistoryState] = useState(() => ({ veId, history: loadVEPromptHistory(veId) }));
    // Track VE online status; input is disabled when offline.
    const [veOnline, setVeOnline] = useState(initialOnlineStatus !== "offline");
    const [responseWatchdogTimeoutSec, setResponseWatchdogTimeoutSec] = useState(DEFAULT_AGENT_TIMEOUT_SEC);

    // Refs for imperative state access (avoids stale closure in useImperativeHandle)
    const stateRef = useRef(state);
    stateRef.current = state;
    const inputTextRef = useRef(inputText);
    inputTextRef.current = inputText;

    // Expose getState() to parent via ref; parent calls this before unmount to snapshot state
    useImperativeHandle(ref, () => ({
        getState: () => ({
            messages: stateRef.current.messages,
            sessionId: stateRef.current.sessionId,
            inputText: inputTextRef.current,
        }),
    }), []);

    const mountedRef = useRef(true);
    const messagesEndRef = useRef<HTMLDivElement>(null);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const focusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const queuedMessagesRef = useRef<QueuedVEMessage[]>([]);
    const sessionIdRef = useRef<string | null>(existingSessionId || null);
    const reconnectAttemptRef = useRef(0);
    const sendingRef = useRef(false);
    const awaitingReplyRef = useRef(false);
    const queueDrainRunningRef = useRef(false);
    const responseWatchdogRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const loadedHistorySessionRef = useRef<string>("");
    const sessionInitInFlightRef = useRef<Promise<boolean> | null>(null);
    const sessionInitGenerationRef = useRef(0);
    const [queueDrainSignal, setQueueDrainSignal] = useState(0);

    sendingRef.current = sending;

    const isZh = !lang || lang.startsWith("zh");
    const localSpeakerName = isZh ? "\u6211" : "Me";
    const assistantDisplayName = useMemo(() => readableConversationPartnerName(veName, veId, isZh), [isZh, veId, veName]);
    const safeAssistantAvatar = useMemo(() => safeAvatarDataURL(avatarDataURL), [avatarDataURL]);
    const canSend = veOnline && !readOnly && !sending && (!!inputText.trim() || pendingAttachments.length > 0);
    const inputReady = veOnline && !readOnly;
    const inputThemeMode: "light" | "dark" = isDarkHexColor(theme.bg) ? "dark" : "light";
    const voiceInputDisabledRef = useRef<((level: number) => void) | null>(null);
    const disabledVoiceInput = useMemo<UseVoiceInputResult>(() => ({
        state: "idle",
        asrReady: false,
        toggle: async () => {},
        startHold: async () => {},
        stopHold: () => {},
        holdRecording: false,
        duration: 0,
        isSpeaking: false,
        segmentCount: 0,
        error: null,
        onAudioLevelRef: voiceInputDisabledRef,
    }), []);

    const closeMentionPopover = useCallback(() => {
        setMentionOpen(false);
        setMentionQuery("");
        setMentionStart(-1);
        setMentionSelectedIndex(0);
    }, []);

    const showAwaitingReply = useCallback(() => {
        if (mountedRef.current) setAwaitingReplyVisible(true);
    }, []);

    const hideAwaitingReply = useCallback(() => {
        if (mountedRef.current) setAwaitingReplyVisible(false);
    }, []);

    const releaseResponseGate = useCallback(() => {
        awaitingReplyRef.current = false;
        hideAwaitingReply();
        if (responseWatchdogRef.current) {
            clearTimeout(responseWatchdogRef.current);
            responseWatchdogRef.current = null;
        }
        if (!mountedRef.current) return;
        setQueueDrainSignal((value) => value + 1);
    }, [hideAwaitingReply]);

    const armResponseWatchdog = useCallback(() => {
        if (responseWatchdogRef.current) clearTimeout(responseWatchdogRef.current);
        responseWatchdogRef.current = setTimeout(() => {
            responseWatchdogRef.current = null;
            releaseResponseGate();
        }, responseWatchdogTimeoutSec * 1000);
    }, [releaseResponseGate, responseWatchdogTimeoutSec]);

    const scheduleInputFocus = useCallback((position?: number) => {
        if (focusTimerRef.current) clearTimeout(focusTimerRef.current);
        focusTimerRef.current = setTimeout(() => {
            focusTimerRef.current = null;
            const target = position ?? inputRef.current?.value.length ?? 0;
            inputRef.current?.focus();
            inputRef.current?.setSelectionRange(target, target);
        }, 0);
    }, []);

    const resizeInput = useCallback(() => {
        const input = inputRef.current;
        if (!input) return;
        input.style.height = "auto";
        input.style.height = `${Math.min(input.scrollHeight, 180)}px`;
    }, []);

    const applyInputValue = useCallback((nextValue: string) => {
        setInputText(nextValue);
        requestAnimationFrame(() => {
            resizeInput();
            scheduleInputFocus(nextValue.length);
        });
    }, [resizeInput, scheduleInputFocus]);

    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({ applyInputValue, inputRef, inputValue: inputText, submittedPrompts: promptHistoryState.veId === veId ? promptHistoryState.history : [] });

    const recordSubmittedPrompt = useCallback((prompt: string) => {
        setPromptHistoryState(prev => {
            const history = prev.veId === veId ? prev.history : loadVEPromptHistory(veId);
            return { veId, history: appendVEPromptHistory(history, prompt) };
        });
    }, [veId]);

    const updateMentionState = useCallback((value: string, caret: number | null | undefined) => {
        if (readOnly || !participants?.length || caret == null) {
            closeMentionPopover();
            return;
        }
        const beforeCaret = value.slice(0, caret);
        const match = beforeCaret.match(MENTION_TRIGGER_PATTERN);
        if (!match) {
            closeMentionPopover();
            return;
        }
        const query = match[2] || "";
        const normalizedQuery = query.trim().toLowerCase();
        const hasMatches = !normalizedQuery || participants.some((p) =>
            p.name.toLowerCase().includes(normalizedQuery)
        );
        if (!hasMatches) {
            closeMentionPopover();
            return;
        }
        const atIndex = beforeCaret.length - query.length - 1;
        setMentionStart(atIndex);
        setMentionQuery(query);
        setMentionSelectedIndex(0);
        setMentionOpen(true);
    }, [closeMentionPopover, participants, readOnly]);

    const mentionFiltered = useMemo(() => {
        if (!participants?.length) return [];
        const query = mentionQuery.trim().toLowerCase();
        if (!query) return participants;
        return participants.filter((p) =>
            p.name.toLowerCase().includes(query)
        );
    }, [mentionQuery, participants]);

    const insertMentionParticipant = useCallback((participant: MentionParticipant) => {
        if (readOnly) return;
        const textarea = inputRef.current;
        const caret = textarea?.selectionStart ?? inputText.length;
        const start = mentionStart >= 0 ? mentionStart : caret;
        const mention = `@${participant.name} `;
        const next = `${inputText.slice(0, start)}${mention}${inputText.slice(caret)}`;
        const nextCaret = start + mention.length;
        setInputText(next);
        closeMentionPopover();
        scheduleInputFocus(nextCaret);
    }, [closeMentionPopover, inputText, mentionStart, readOnly, scheduleInputFocus]);

    const mentionKeyDown = useMentionKeyboard(mentionOpen, mentionFiltered, mentionSelectedIndex, setMentionSelectedIndex, insertMentionParticipant, closeMentionPopover);

    useEffect(() => {
        if (readOnly) closeMentionPopover();
    }, [closeMentionPopover, readOnly]);

    useEffect(() => {
        if (!readOnly) return;
        queuedMessagesRef.current = [];
        setVisibleQueue([]);
        sendingRef.current = false;
        awaitingReplyRef.current = false;
        queueDrainRunningRef.current = false;
        if (responseWatchdogRef.current) {
            clearTimeout(responseWatchdogRef.current);
            responseWatchdogRef.current = null;
        }
        setSending(false);
        hideAwaitingReply();
    }, [hideAwaitingReply, readOnly]);

    useEffect(() => {
        if (readOnly || !externalMentionInsert?.name) return;
        const mention = `@${externalMentionInsert.name} `;
        const textarea = inputRef.current;
        closeMentionPopover();
        setInputText((prev) => {
            const caret = textarea?.selectionStart ?? prev.length;
            const prefix = prev.slice(0, caret);
            const suffix = prev.slice(caret).replace(/^\s+/, "");
            const spacer = prefix && !prefix.endsWith(" ") ? " " : "";
            const next = `${prefix}${spacer}${mention}${suffix}`;
            scheduleInputFocus(prefix.length + spacer.length + mention.length);
            return next;
        });
    }, [closeMentionPopover, externalMentionInsert, readOnly, scheduleInputFocus]);

    // Keep sessionIdRef in sync
    useEffect(() => {
        sessionIdRef.current = state.sessionId;
        if (state.sessionId) onSessionIdChange?.(state.sessionId);
    }, [onSessionIdChange, state.sessionId]);

    useEffect(() => {
        setPromptHistoryState({ veId, history: loadVEPromptHistory(veId) });
    }, [veId]);

    useEffect(() => {
        persistVEPromptHistory(promptHistoryState.veId, promptHistoryState.history);
    }, [promptHistoryState]);

    useEffect(() => {
        const sessionId = String(state.sessionId || "").trim();
        if (!sessionId || loadedHistorySessionRef.current === sessionId || state.messages.length > 0) return;
        loadedHistorySessionRef.current = sessionId;
        let cancelled = false;
        void getWailsAppModule()
            .then((mod) => (mod as any).GroupDiscussionGetConsultationDetail?.(sessionId))
            .then((detail: VEHistoryDetail | undefined) => {
                if (cancelled || !detail) return;
                const history = veMessagesFromHistoryDetail(detail, veId, assistantDisplayName, localSpeakerName);
                if (!history.length) return;
                setState((prev) => {
                    if (prev.sessionId !== sessionId || prev.messages.length > 0) return prev;
                    return { ...prev, messages: history };
                });
            })
            .catch(() => {});
        return () => { cancelled = true; };
    }, [assistantDisplayName, localSpeakerName, state.messages.length, state.sessionId, veId]);


    // Keep reconnectAttemptRef in sync with state resets (e.g. successful session init)
    useEffect(() => {
        reconnectAttemptRef.current = state.reconnectAttempt;
    }, [state.reconnectAttempt]);

    // Scroll to bottom on new messages
    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [state.messages, state.streamContent]);

    // Track VE online/offline status via events
    useEffect(() => {
        const unsub = EventsOn("ve:status_change", (data: any) => {
            if (!data) return;
            const { ids, status } = veStatusEventInfo(data);
            if (!ids.includes(normalizeParticipantId(veId))) return;
            if (status === "online") setVeOnline(true);
            else if (status === "offline") setVeOnline(false);
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [veId]);

    useEffect(() => {
        let cancelled = false;
        getWailsAppModule()
            .then((mod) => (mod as any).LoadConfig?.())
            .then((cfg) => {
                if (!cancelled) setResponseWatchdogTimeoutSec(normalizeAgentTimeoutSeconds(cfg?.agent_response_timeout_sec));
            })
            .catch(() => {
                if (!cancelled) setResponseWatchdogTimeoutSec(DEFAULT_AGENT_TIMEOUT_SEC);
            });
        const handleConfigChanged = (cfg?: any) => {
            setResponseWatchdogTimeoutSec(normalizeAgentTimeoutSeconds(cfg?.agent_response_timeout_sec));
        };
        const unsub = EventsOn("config-changed", handleConfigChanged);
        const handleLocalConfigChanged = (event: Event) => handleConfigChanged((event as CustomEvent).detail);
        window.addEventListener(LOCAL_CONFIG_CHANGED_EVENT, handleLocalConfigChanged);
        return () => {
            cancelled = true;
            if (typeof unsub === "function") unsub();
            else EventsOff("config-changed");
            window.removeEventListener(LOCAL_CONFIG_CHANGED_EVENT, handleLocalConfigChanged);
        };
    }, []);

    // --- Session Management ---

    const initSession = useCallback(async () => {
        if (sessionInitInFlightRef.current) return sessionInitInFlightRef.current;
        const initGeneration = sessionInitGenerationRef.current;
        const veIds = participants?.length ? groupSessionVEIds(veId, participants) : [veId];
        const shouldRegisterLocalExecutor = !!participants?.some(isLocalGroupParticipant);
        const startSession = async () => {
            if (initiateGroupConversation && participants?.length) {
                return initiateGroupConversation(veIds);
            }
            if (initiateConversation) {
                return initiateConversation(veId);
            }
            const mod = await getWailsAppModule();
            if (participants?.length && typeof (mod as any).InitiateGroupConversation === "function") {
                return (mod as any).InitiateGroupConversation(veIds);
            }
            return (mod as any).InitiateVEConversation(veId);
        };

        const run = (async () => {
            try {
                const result = await createSessionWithTimeout<{ session_id: string; ve_id: string; ve_name: string }>(
                    startSession(),
                    SESSION_TIMEOUT_MS
                );
                const sessionId = String(result?.session_id || "").trim();
                let localRegistrationError: VEConversationError | null = null;
                if (sessionInitGenerationRef.current !== initGeneration) return false;
                if (shouldRegisterLocalExecutor && sessionId) {
                    try {
                        if (registerLocalExecutorInGroup) {
                            await registerLocalExecutorInGroup(sessionId);
                        } else {
                            const mod = await getWailsAppModule();
                            if (typeof (mod as any).RegisterLocalExecutorInGroup === "function") {
                                await (mod as any).RegisterLocalExecutorInGroup(sessionId);
                            }
                        }
                    } catch (err: any) {
                        localRegistrationError = {
                            type: "send_failed",
                            message: err?.message || "Local AI registration failed",
                        } as VEConversationError;
                    }
                }
                if (sessionInitGenerationRef.current !== initGeneration) return false;
                if (sessionId) sessionIdRef.current = sessionId;
                if (mountedRef.current) {
                    setState((prev) => ({
                        ...prev,
                        sessionId,
                        error: localRegistrationError,
                        connectionState: "connected",
                        reconnectAttempt: 0,
                    }));
                }
                return true;
            } catch (err: any) {
                if (sessionInitGenerationRef.current !== initGeneration) return false;
                if (mountedRef.current) {
                    const message = String(err?.message || "");
                    const errorType = message.includes("pending_confirmation")
                        ? "auth_pending"
                        : message.includes("VE_ACCESS_DENIED") || message.includes("VE_AUTH_BLOCKED") || message.includes("forbidden")
                        ? "access_denied"
                        : message.includes("session_timeout")
                        ? "session_timeout"
                        : message.includes("offline")
                        ? "ve_offline"
                        : "hub_disconnected";
                    setState((prev) => ({
                        ...prev,
                        error: {
                            type: errorType,
                            message: err?.message || "Connection failed",
                        } as VEConversationError,
                    }));
                }
                return false;
            }
        })();
        sessionInitInFlightRef.current = run;
        try {
            return await run;
        } finally {
            if (sessionInitInFlightRef.current === run) sessionInitInFlightRef.current = null;
        }
    }, [initiateConversation, initiateGroupConversation, participants, registerLocalExecutorInGroup, veId]);

    useEffect(() => {
        const nextSessionId = existingSessionId || null;
        if (!nextSessionId || sessionIdRef.current === nextSessionId) return;
        if (sessionIdRef.current) return;
        sessionIdRef.current = nextSessionId;
        setState((prev) => prev.sessionId ? prev : {
            ...prev,
            sessionId: nextSessionId,
            error: null,
            connectionState: "connected",
        });
    }, [existingSessionId]);

    // Initialize session on mount (skip if resuming an existing sticky session or VE is offline)
    useEffect(() => {
        mountedRef.current = true;
        if (!existingSessionId && initialOnlineStatus !== "offline") {
            initSession();
        }
        return () => {
            mountedRef.current = false;
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
            }
            if (focusTimerRef.current) {
                clearTimeout(focusTimerRef.current);
            }
            if (responseWatchdogRef.current) {
                clearTimeout(responseWatchdogRef.current);
            }
        };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // When VE comes online and we don't have a session yet, initiate one automatically.
    // Skip the initial render; mount effect handles the online-at-mount case.
    const didMountForAutoConnect = useRef(false);
    useEffect(() => {
        if (!didMountForAutoConnect.current) {
            didMountForAutoConnect.current = true;
            return;
        }
        if (veOnline && !sessionIdRef.current) {
            initSession();
        }
    }, [veOnline, initSession]);

    useEffect(() => {
        const handleAuthResult = (data: any) => {
            const payload = data?.payload && typeof data.payload === "object" ? data.payload : data;
            const normalizedVEIds = authResultCandidateIds(veId);
            const targetIds = [payload?.target_ve_id, payload?.ve_id, payload?.target_machine_id]
                .flatMap((value) => authResultCandidateIds(value));
            if (!targetIds.some((id) => normalizedVEIds.includes(id))) return;
            const decision = String(payload?.decision || "").trim();
            const status = String(payload?.status || "").trim();
            if (status === "allowed" || decision === "allow_once" || decision === "allow_long" || decision === "allow") {
                const hasSession = !!sessionIdRef.current;
                setState((prev) => ({ ...prev, error: null, connectionState: hasSession || prev.sessionId ? "connected" : "reconnecting" }));
                if (!hasSession) void initSession();
                return;
            }
            const blocked = status === "blocked" || decision === "block";
            sessionInitGenerationRef.current += 1;
            sessionInitInFlightRef.current = null;
            setState((prev) => ({
                ...prev,
                connectionState: "disconnected",
                error: {
                    type: "access_denied",
                    message: blocked ? "blocked" : "denied",
                } as VEConversationError,
            }));
        };
        const unsub = EventsOn("ve:auth_result", handleAuthResult);
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:auth_result");
        };
    }, [initSession, veId]);

    const clearConversation = useCallback(async () => {
        if (readOnly) return;
        const oldSessionId = sessionIdRef.current;
        sessionInitGenerationRef.current += 1;
        sessionInitInFlightRef.current = null;
            queuedMessagesRef.current = [];
            setVisibleQueue([]);
        if (reconnectTimerRef.current) {
            clearTimeout(reconnectTimerRef.current);
            reconnectTimerRef.current = null;
        }
        if (responseWatchdogRef.current) {
            clearTimeout(responseWatchdogRef.current);
            responseWatchdogRef.current = null;
        }
        awaitingReplyRef.current = false;
        hideAwaitingReply();
        queueDrainRunningRef.current = false;
        setPendingAttachments([]);
        setInputText("");
        resetHistoryBrowsing();
        setPromptHistoryState({ veId, history: [] });
        closeMentionPopover();
        sessionIdRef.current = null;
        setState((prev) => ({
            ...prev,
            sessionId: null,
            messages: [],
            streaming: false,
            streamContent: "",
            streamFromId: "",
            streamFromName: "",
            streamAttachments: [],
            error: null,
            connectionState: "connected",
            reconnectAttempt: 0,
        }));
        onConversationCleared?.();
        if (oldSessionId) {
            try {
                if (closeSession) {
                    await closeSession(oldSessionId);
                } else {
                    const mod = await getWailsAppModule();
                    if (typeof (mod as any).CloseVESession === "function") {
                        await (mod as any).CloseVESession(oldSessionId);
                    }
                }
            } catch {
                // Clearing the local UI should not be blocked by a stale Hub session.
            }
        }
        if (veOnline) void initSession();
    }, [closeMentionPopover, closeSession, hideAwaitingReply, initSession, onConversationCleared, readOnly, resetHistoryBrowsing, veOnline]);

    const lastClearSignalRef = useRef(clearSignal);
    useEffect(() => {
        if (clearSignal === lastClearSignalRef.current) return;
        lastClearSignalRef.current = clearSignal;
        void clearConversation();
    }, [clearConversation, clearSignal]);


    // --- Reconnection Logic ---

    const attemptReconnect = useCallback(() => {
        if (reconnectTimerRef.current) return;
        const currentAttempt = reconnectAttemptRef.current;

        if (currentAttempt >= MAX_RECONNECT_RETRIES) {
            setState((prev) => ({
                ...prev,
                connectionState: "disconnected",
                error: {
                    type: "hub_disconnected",
                    message: isZh
                        ? "\u91cd\u8fde\u5931\u8d25\uff0c\u5df2\u8fbe\u6700\u5927\u91cd\u8bd5\u6b21\u6570"
                        : "Reconnection failed after max retries",
                },
            }));
            return;
        }

        const nextAttempt = currentAttempt + 1;
        reconnectAttemptRef.current = nextAttempt;

        setState((prev) => ({
            ...prev,
            connectionState: "reconnecting",
            reconnectAttempt: nextAttempt,
        }));

        const delay = RECONNECT_DELAYS[Math.min(currentAttempt, RECONNECT_DELAYS.length - 1)];
        reconnectTimerRef.current = setTimeout(async () => {
            reconnectTimerRef.current = null;
            const connected = await initSession();
            if (!connected && mountedRef.current) {
                attemptReconnect();
            }
        }, delay);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [initSession, isZh]);

    // --- Streaming Event Listeners ---

    useEffect(() => {
        const handleStreamChunk = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            const content = data?.content || data?.chunk || "";
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            hideAwaitingReply();
            const senderId = String(data?.from_id || data?.fromId || data?.sender_id || data?.senderId || "");
            const senderName = String(data?.from_name || data?.fromName || data?.sender_name || data?.senderName || "");
            const attachments = normalizeVEMessageAttachments(data?.attachments);
            setState((prev) => {
                const hasPendingStream = !!prev.streamContent || prev.streamAttachments.length > 0;
                if (prev.streaming && hasPendingStream && senderId && prev.streamFromId && senderId !== prev.streamFromId) {
                    const completedMsg: VEMessage = {
                        id: generateMsgId(),
                        role: "assistant",
                        content: prev.streamContent,
                        timestamp: Date.now(),
                        fromId: prev.streamFromId,
                        fromName: prev.streamFromName || undefined,
                        attachments: prev.streamAttachments.length ? prev.streamAttachments : undefined,
                    };
                    return {
                        ...prev,
                        streaming: true,
                        streamContent: content,
                        streamFromId: senderId,
                        streamFromName: senderName,
                        streamAttachments: attachments,
                        messages: [...prev.messages, completedMsg],
                    };
                }
                return {
                    ...prev,
                    streaming: true,
                    streamContent: prev.streamContent + content,
                    streamFromId: senderId || prev.streamFromId,
                    streamFromName: senderName || prev.streamFromName,
                    streamAttachments: attachments.length ? mergeVEMessageAttachments(prev.streamAttachments, attachments) : prev.streamAttachments,
                };
            });
        };

        const handleStreamEnd = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            hideAwaitingReply();
            setState((prev) => {
                const finalContent = prev.streamContent;
                const endAttachments = normalizeVEMessageAttachments(data?.attachments);
                const attachments = endAttachments.length ? mergeVEMessageAttachments(prev.streamAttachments, endAttachments) : prev.streamAttachments;
                if (!finalContent && attachments.length === 0) return { ...prev, streaming: false, streamContent: "", streamFromId: "", streamFromName: "", streamAttachments: [] };
                const newMsg: VEMessage = {
                    id: generateMsgId(),
                    role: "assistant",
                    content: finalContent,
                    timestamp: Date.now(),
                    fromId: prev.streamFromId || undefined,
                    fromName: prev.streamFromName || undefined,
                    attachments: attachments.length ? attachments : undefined,
                };
                return {
                    ...prev,
                    streaming: false,
                    streamContent: "",
                    streamFromId: "",
                    streamFromName: "",
                    streamAttachments: [],
                    messages: [...prev.messages, newMsg],
                };
            });
            releaseResponseGate();
        };

        const handleDisconnect = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            if (reconnectTimerRef.current) return;
            hideAwaitingReply();
            releaseResponseGate();
            setState((prev) => ({
                ...prev,
                streaming: false,
                streamContent: "",
                streamFromId: "",
                streamFromName: "",
                streamAttachments: [],
                connectionState: "disconnected",
            }));
            attemptReconnect();
        };

        const unsub1 = EventsOn("ve:stream_chunk", handleStreamChunk);
        const unsub2 = EventsOn("ve:stream_end", handleStreamEnd);
        const unsub3 = EventsOn("ve:disconnected", handleDisconnect);

        return () => {
            if (typeof unsub1 === "function") unsub1();
            else EventsOff("ve:stream_chunk");
            if (typeof unsub2 === "function") unsub2();
            else EventsOff("ve:stream_end");
            if (typeof unsub3 === "function") unsub3();
            else EventsOff("ve:disconnected");
        };
    }, [attemptReconnect, hideAwaitingReply, releaseResponseGate]);

    // --- Message Sending ---

    const doSendMessage = useCallback(
        async (content: string, filePaths?: string[], messageId?: string): Promise<boolean> => {
            const sid = sessionIdRef.current;
            if (!sid) return false;

            try {
                if (filePaths && filePaths.length > 0 && participants?.length && sendGroupMessageWithAttachments) {
                    await sendGroupMessageWithAttachments(sid, content, mentionedParticipantIds(content, participants), filePaths);
                } else if (filePaths && filePaths.length > 0 && sendMessageWithAttachments) {
                    await sendMessageWithAttachments(sid, content, filePaths);
                } else if (sendGroupMessage && participants?.length) {
                    await sendGroupMessage(sid, content, mentionedParticipantIds(content, participants));
                } else if (sendMessage) {
                    await sendMessage(sid, content);
                } else {
                    const mod = await getWailsAppModule();
                    if (filePaths && filePaths.length > 0) {
                        if (participants?.length && typeof (mod as any).SendVEGroupMessageWithAttachments === "function") {
                            await (mod as any).SendVEGroupMessageWithAttachments(sid, content, mentionedParticipantIds(content, participants), filePaths);
                        } else {
                            await (mod as any).SendVEMessageWithAttachments(sid, content, filePaths);
                        }
                    } else if (participants?.length && typeof (mod as any).SendVEGroupMessage === "function") {
                        await (mod as any).SendVEGroupMessage(sid, content, mentionedParticipantIds(content, participants));
                    } else {
                        await (mod as any).SendVEMessage(sid, content);
                    }
                }
                return true;
            } catch (err: any) {
                if (mountedRef.current) {
                    // Mark last user message as failed
                    setState((prev) => {
                        const msgs = [...prev.messages];
                        const failedIdx = messageId
                            ? msgs.findIndex((m) => m.id === messageId)
                            : msgs.findLastIndex((m) => m.role === "user");
                        if (failedIdx >= 0) {
                            msgs[failedIdx] = { ...msgs[failedIdx], sendFailed: true };
                        }
                        return {
                            ...prev,
                            messages: msgs,
                            error: {
                                type: "send_failed",
                                message: extractErrorMessage(err) || "Send failed",
                            },
                        };
                    });
                }
                return false;
            }
        },
        [participants, sendGroupMessage, sendGroupMessageWithAttachments, sendMessage, sendMessageWithAttachments]
    );

    const drainQueuedMessages = useCallback(async () => {
        if (queueDrainRunningRef.current || awaitingReplyRef.current || !sessionIdRef.current || queuedMessagesRef.current.length === 0) return;
        const msg = queuedMessagesRef.current.shift();
        if (!msg) return;
        setVisibleQueue((prev) => prev.filter((item) => item.id !== msg.id));
        queueDrainRunningRef.current = true;
        awaitingReplyRef.current = true;
        showAwaitingReply();
        setState((prev) => ({
            ...prev,
            messages: prev.messages.some((item) => item.id === msg.message.id)
                ? prev.messages
                : [...prev.messages, msg.message],
            error: null,
        }));
        const sent = await doSendMessage(msg.content, msg.filePaths, msg.id);
        queueDrainRunningRef.current = false;
        if (sent) recordSubmittedPrompt(msg.content);
        if (sent && awaitingReplyRef.current) {
            armResponseWatchdog();
        } else {
            releaseResponseGate();
        }
    }, [armResponseWatchdog, doSendMessage, recordSubmittedPrompt, releaseResponseGate, showAwaitingReply]);

    useEffect(() => {
        if (readOnly || state.connectionState !== "connected" || !state.sessionId || state.streaming) return;
        void drainQueuedMessages();
    }, [drainQueuedMessages, queueDrainSignal, readOnly, state.connectionState, state.sessionId, state.streaming]);

    const handleSend = useCallback(async () => {
        const content = inputText.trim();
        if (readOnly) return;
        if (isExplicitVEHistoryResetCommand(content)) {
            void clearConversation();
            return;
        }
        if (!content && pendingAttachments.length === 0) return;
        if (sendingRef.current) return;
        sendingRef.current = true;

        const filePaths = pendingAttachments.length > 0
            ? pendingAttachments.map((f) => f.filePath || "").filter(Boolean)
            : undefined;

        const userMsg: VEMessage = {
            id: generateMsgId(),
            role: "user",
            content,
            timestamp: Date.now(),
            fromId: "user",
            fromName: localSpeakerName,
            attachments: pendingAttachments.map((f) => ({
                type: classifyAttachmentType(f.fileName),
                filename: f.fileName,
                localPath: f.filePath,
            })),
        };

        // If disconnected, still creating the session, or waiting for the prior
        // assistant turn to finish streaming, keep the draft in the visible
        // pre-input queue. It enters the transcript only when it is really sent.
        if (state.connectionState !== "connected" || !state.sessionId || state.streaming || awaitingReplyRef.current) {
            const queued = { id: userMsg.id, content, message: userMsg, filePaths, attachmentNames: pendingAttachments.map((f) => f.fileName) };
            queuedMessagesRef.current.push(queued);
            setVisibleQueue((prev) => [...prev, queued]);
            setInputText("");
            setPendingAttachments([]);
            window.setTimeout(() => { sendingRef.current = false; }, 0);
            return;
        }

        setSending(true);

        setState((prev) => ({
            ...prev,
            messages: [...prev.messages, userMsg],
            error: null,
        }));
        setInputText("");

        setPendingAttachments([]);

        try {
            awaitingReplyRef.current = true;
            showAwaitingReply();
            const sent = await doSendMessage(content, filePaths, userMsg.id);
            if (sent) recordSubmittedPrompt(content);
            if (sent && awaitingReplyRef.current) {
                armResponseWatchdog();
            } else {
                releaseResponseGate();
            }
        } finally {
            sendingRef.current = false;
            if (mountedRef.current) setSending(false);
        }
    }, [armResponseWatchdog, clearConversation, doSendMessage, inputText, localSpeakerName, pendingAttachments, readOnly, recordSubmittedPrompt, releaseResponseGate, showAwaitingReply, state.connectionState, state.sessionId, state.streaming]);

    // --- Attachment Handling ---

    const handleAttachmentSelect = useCallback(async () => {
        if (readOnly) return;
        try {
            const mod = await getWailsAppModule();
            const selected = await (mod as any).SelectAIAssistantFiles?.();
            if (Array.isArray(selected) && selected.length > 0) {
                setPendingAttachments((prev) => [
                    ...prev,
                    ...selected.map((filePath: string) => attachmentInfoFromPath(filePath)),
                ]);
                return;
            }
        } catch {
            // Fall through to browser file input for unit tests and non-Wails previews.
        }

        const input = document.createElement("input");
        input.type = "file";
        input.multiple = true;
        input.accept = ".txt,.md,.csv,.json,.xml,.yaml,.yml,.log,.go,.py,.js,.ts,.html,.css,.png,.jpg,.jpeg,.gif,.webp,.bmp,.pdf,.docx";
        input.onchange = () => {
            if (input.files) {
                setPendingAttachments((prev) => [
                    ...prev,
                    ...Array.from(input.files!).map((file) => ({
                        fileName: file.name,
                        filePath: (file as any).path || file.name,
                        extension: `.${(file.name.split(".").pop() || "").toLowerCase()}`,
                        isImage: classifyAttachmentType(file.name) === "image",
                    })),
                ]);
            }
        };
        input.click();
    }, [readOnly]);

    // --- Render ---

    return (
        <div
            data-testid="ve-conversation-view"
            style={{
                display: "flex",
                flexDirection: "column",
                height: "100%",
                background: theme.bg,
            }}
        >
            {/* Error / connection banner */}
            {(state.error || state.connectionState === "reconnecting") && (
                <div
                    data-testid="ve-error-banner"
                    style={{
                        padding: "8px 12px",
                        background: theme.errorBg || "#fef2f2",
                        color: theme.errorText || "#dc2626",
                        borderBottom: `1px solid ${theme.errorBorder || "#fecaca"}`,
                        fontSize: 12,
                        display: "flex",
                        alignItems: "center",
                        gap: 6,
                    }}
                >
                    <span>!</span>
                    <span>{state.error ? formatError(state.error, isZh) : (isZh ? "\u8fde\u63a5\u5df2\u65ad\u5f00\uff0c\u6b63\u5728\u91cd\u8fde" : "Connection lost, reconnecting")}</span>
                    {state.connectionState === "reconnecting" && (
                        <span style={{ marginLeft: "auto", fontSize: 11, opacity: 0.7 }}>
                            {isZh ? `\u91cd\u8fde\u4e2d (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...` : `Reconnecting (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...`}
                        </span>
                    )}
                </div>
            )}

            {/* Message List */}
            <div
                data-testid="ve-message-list"
                style={{
                    flex: 1,
                    overflowY: "auto",
                    padding: "12px 16px",
                }}
            >
                {state.messages.map((msg) => (
                    <MessageBubble
                        key={msg.id}
                        message={msg}
                        sessionId={state.sessionId || ""}
                        theme={theme}
                        isZh={isZh}
                        assistantName={readableSpeakerName(msg.fromName, msg.fromId, participants, assistantDisplayName)}
                        userName={readableSpeakerName(msg.fromName, msg.fromId, participants, localSpeakerName)}
                        assistantAvatarDataURL={safeAssistantAvatar}
                    />
                ))}

                {/* Awaiting first response chunk */}
                {awaitingReplyVisible && !state.streaming && (
                    <div data-testid="ve-thinking-indicator" style={{ marginTop: 8 }}>
                        <div style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "8px 12px", borderRadius: 8, background: theme.fieldBg, borderLeft: `3px solid ${theme.responseBorderLeft}`, fontSize: 13, color: theme.textMuted || theme.text }}>
                            <span>{isZh ? "思考中" : "Thinking"}</span>
                            <span className="ve-cursor-blink">...</span>
                        </div>
                    </div>
                )}

                {/* Streaming Indicator */}
                {state.streaming && (
                    <div data-testid="ve-streaming-indicator" style={{ marginTop: 8 }}>
                        <div
                            data-testid="ve-streaming-content"
                            style={{
                                padding: "8px 12px",
                                borderRadius: 8,
                                background: theme.fieldBg,
                                borderLeft: `3px solid ${theme.responseBorderLeft}`,
                                fontSize: 13,
                                color: theme.text,
                                wordBreak: "break-word",
                                overflowWrap: "anywhere",
                                whiteSpace: "pre-wrap",
                            }}
                        >
                            <div style={{ fontSize: 11, fontWeight: 600, color: participantColorById(participants, state.streamFromId, theme.responseBorderLeft), marginBottom: 2, whiteSpace: "normal" }}>
                                {readableSpeakerName(state.streamFromName, state.streamFromId, participants, assistantDisplayName)}
                            </div>
                            {state.streamContent && <MessageContentRenderer content={state.streamContent} theme={theme} />}
                            {state.streamAttachments.length > 0 && (
                                <div style={{ marginTop: state.streamContent ? 6 : 0, display: "flex", flexWrap: "wrap", gap: 4 }}>
                                    {state.streamAttachments.map((att, idx) => (
                                        <AttachmentDisplay key={`${att.type}-${att.filename}-${att.fileUrl || att.localPath || idx}`} attachment={att} sessionId={state.sessionId || sessionIdRef.current || ""} theme={theme} prefetchRemoteImage={false} />
                                    ))}
                                </div>
                            )}
                            <span className="ve-cursor-blink" style={{ opacity: 0.6 }}>{"|"}</span>
                        </div>
                    </div>
                )}

                <div ref={messagesEndRef} />
            </div>

            {visibleQueue.length > 0 && (
                <QueuedMessagePanel queue={visibleQueue} theme={theme} isZh={isZh} />
            )}

            <AssistantInputComposer
                attachButtonTestId="ve-attach-button"
                browseFile={handleAttachmentSelect}
                canSend={canSend}
                cancelPending={sending}
                clearSelectedFile={() => setPendingAttachments([])}
                exitHistoryBrowsing={exitHistoryBrowsing}
                finishVoicePointer={() => {}}
                handleCancel={() => {}}
                handlePaste={handlePaste}
                handleSend={handleSend}
                handleTextareaClick={(e) => updateMentionState(inputText, e.currentTarget.selectionStart)}
                handleTextareaKeyDownBefore={(e) => mentionKeyDown(e)}
                handleTextareaKeyUp={(e) => {
                    if (["ArrowDown", "ArrowUp", "Enter", "Escape"].includes(e.key)) return;
                    updateMentionState(e.currentTarget.value, e.currentTarget.selectionStart);
                }}
                handleVoiceClick={() => {}}
                handleVoicePointerDown={() => {}}
                handleVoicePointerLeave={() => {}}
                inputAreaHeight={null}
                inputBarTestId="ve-input-area"
                inputLocked={!inputReady}
                inputOverlay={mentionOpen ? (
                    <MentionPopover
                        filtered={mentionFiltered}
                        selectedIndex={mentionSelectedIndex}
                        onSelect={insertMentionParticipant}
                        onHover={setMentionSelectedIndex}
                        onClose={closeMentionPopover}
                        anchorRef={inputRef}
                        theme={theme}
                        lang={lang}
                    />
                ) : null}
                inputRef={inputRef}
                inputRowTestId="ve-input-row"
                inputValue={inputText}
                inline={false}
                isBusy={sending}
                isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary}
                lang={lang || "zh"}
                pendingAttachments={pendingAttachments}
                pendingAttachmentsTestId="ve-attachment-preview-bar"
                placeholderText={readOnly
                    ? (isZh ? "\u53ea\u8bfb\u4f1a\u8bdd\uff0c\u4e0d\u80fd\u7ee7\u7eed\u53d1\u8a00" : "Read-only session")
                    : !veOnline
                    ? (isZh ? `${assistantDisplayName} \u5f53\u524d\u79bb\u7ebf\uff0c\u4e0a\u7ebf\u540e\u53ef\u7ee7\u7eed\u5bf9\u8bdd` : `${assistantDisplayName} is offline`)
                    : (isZh ? `\u53d1\u9001\u6d88\u606f\u7ed9 ${assistantDisplayName}...` : `Message ${assistantDisplayName}...`)}
                ready={inputReady}
                recallHistory={recallHistory}
                rememberHistoryEdit={rememberHistoryEdit}
                resizeInput={resizeInput}
                selectedFilePaths={[]}
                sendButtonStyle={{ minWidth: 54, width: 54, flexShrink: 0 }}
                sendButtonTestId="ve-send-button"
                setPendingAttachments={setPendingAttachments}
                showBusySpinner={sending}
                showMemoryUsage={false}
                showVoiceInput={false}
                textareaTestId="ve-input-textarea"
                theme={theme}
                themeMode={inputThemeMode}
                toolbarTestId="ve-input-toolbar"
                updateInputValue={(value) => {
                    setInputText(value);
                    updateMentionState(value, inputRef.current?.selectionStart ?? value.length);
                }}
                voiceInput={disabledVoiceInput}
            />
        </div>
    );
});

// --- Sub-components ---

function vePromptHistoryKey(veId: string): string {
    return `${VE_PROMPT_HISTORY_STORAGE_PREFIX}${veId || "default"}`;
}

function loadVEPromptHistory(veId: string): string[] {
    try {
        const raw = localStorage.getItem(vePromptHistoryKey(veId));
        if (!raw) return [];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed.filter((value: unknown): value is string => typeof value === "string").map(value => value.trim()).filter(Boolean).slice(-MAX_VE_PROMPT_HISTORY);
    } catch {
        return [];
    }
}

function appendVEPromptHistory(history: string[], prompt: string): string[] {
    const trimmed = prompt.trim();
    if (!trimmed) return history;
    if (history[history.length - 1] === trimmed) return history;
    return [...history, trimmed].slice(-MAX_VE_PROMPT_HISTORY);
}

function persistVEPromptHistory(veId: string, history: string[]) {
    try {
        const normalized = history.map(item => item.trim()).filter(Boolean).slice(-MAX_VE_PROMPT_HISTORY);
        const key = vePromptHistoryKey(veId);
        if (normalized.length === 0) localStorage.removeItem(key);
        else localStorage.setItem(key, JSON.stringify(normalized));
    } catch {}
}

function attachmentInfoFromPath(filePath: string): AttachmentInfo {
    const fileName = fileNameFromPath(filePath);
    const extension = `.${(fileName.split(".").pop() || "").toLowerCase()}`;
    return { filePath, fileName, extension, isImage: classifyAttachmentType(fileName) === "image" };
}

function normalizeAgentTimeoutSeconds(value: unknown): number {
    const seconds = Number(value || 0);
    if (!Number.isFinite(seconds) || seconds <= 0) return DEFAULT_AGENT_TIMEOUT_SEC;
    return Math.min(MAX_AGENT_TIMEOUT_SEC, Math.max(MIN_AGENT_TIMEOUT_SEC, Math.floor(seconds)));
}

function AttachmentTypeBadge({ label, theme }: { label: string; theme: Theme }) {
    return (
        <span
            aria-hidden="true"
            style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: 34,
                height: 34,
                flexShrink: 0,
                borderRadius: 5,
                background: theme.codeBlockBorder || theme.divider,
                color: theme.pathColor || theme.text,
                fontSize: label.length > 3 ? 8 : 10,
                fontWeight: 800,
                letterSpacing: 0,
                lineHeight: 1,
                textTransform: "uppercase",
            }}
        >
            {label}
        </span>
    );
}

interface QueuedMessagePanelProps {
    queue: QueuedVEMessage[];
    theme: Theme;
    isZh: boolean;
}

function QueuedMessagePanel({ queue, theme, isZh }: QueuedMessagePanelProps) {
    if (queue.length === 0) return null;
    return (
        <div
            data-testid="ve-queued-message-panel"
            style={{
                display: "flex",
                flexDirection: "column",
                gap: 4,
                padding: "6px 12px",
                borderTop: `1px solid ${theme.divider}`,
                background: theme.inputBarBg,
                color: theme.textMuted,
                fontSize: 11,
            }}
        >
            <div data-testid="ve-queued-message-header" style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 600, color: theme.headingColor }}>
                <span>{isZh ? `${queue.length} \u6761\u5f85\u53d1\u9001` : `${queue.length} queued`}</span>
                <span style={{ fontWeight: 400, color: theme.textMuted }}>{isZh ? "\u56de\u590d\u7ed3\u675f\u540e\u81ea\u52a8\u53d1\u9001" : "Auto-sends after current reply"}</span>
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 3, maxHeight: 66, overflowY: "auto" }}>
                {queue.map((item, index) => (
                    <div key={item.id} data-testid={`ve-queued-message-${index}`} style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0 }}>
                        <span style={{ color: theme.textMuted, flexShrink: 0 }}>#{index + 1}</span>
                        <span style={{ color: theme.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                            {item.content || (isZh ? "\u4ec5\u9644\u4ef6" : "Attachments only")}
                        </span>
                        {!!item.attachmentNames?.length && (
                            <span style={{ flexShrink: 0, color: theme.pathColor }}>
                                {isZh ? `\u9644\u4ef6 ${item.attachmentNames.length}` : `${item.attachmentNames.length} files`}
                            </span>
                        )}
                    </div>
                ))}
            </div>
        </div>
    );
}

interface MessageBubbleProps {
    message: VEMessage;
    sessionId: string;
    theme: Theme;
    isZh: boolean;
    assistantName: string;
    userName: string;
    assistantAvatarDataURL?: string;
}

function MessageBubble({ message, sessionId, theme, isZh, assistantName, userName, assistantAvatarDataURL }: MessageBubbleProps) {
    const isUser = message.role === "user";
    const speakerName = isUser ? userName : assistantName;
    const hasAttachments = !!message.attachments?.length;
    const hasContent = message.content.trim().length > 0;
    const shouldRenderContent = hasContent || !hasAttachments || !!message.sendFailed;

    return (
        <div
            data-testid={`ve-msg-${message.id}`}
            style={{
                marginBottom: 10,
                display: "flex",
                flexDirection: "column",
                alignItems: isUser ? "flex-end" : "flex-start",
            }}
        >
            <div
                data-testid={`ve-msg-label-${message.id}`}
                style={{
                    maxWidth: "80%",
                    marginBottom: 2,
                    padding: "0 4px",
                    color: theme.textMuted,
                    fontSize: 11,
                    fontWeight: 600,
                    display: "flex",
                    alignItems: "center",
                    gap: 5,
                    flexDirection: isUser ? "row-reverse" : "row",
                }}
            >
                {!isUser && assistantAvatarDataURL && (
                    <img
                        data-testid={`ve-msg-avatar-${message.id}`}
                        src={assistantAvatarDataURL}
                        alt=""
                        style={{ width: 18, height: 18, borderRadius: "50%", objectFit: "cover", flexShrink: 0 }}
                    />
                )}
                <span>{speakerName}</span>
            </div>
            {shouldRenderContent && (
                <div
                    data-testid={`ve-msg-content-${message.id}`}
                    style={{
                        maxWidth: "80%",
                        padding: "8px 12px",
                        borderRadius: 8,
                        background: isUser ? theme.sendBtnBg + "15" : theme.fieldBg,
                        borderLeft: isUser ? "none" : `3px solid ${theme.responseBorderLeft}`,
                        borderRight: isUser ? `3px solid ${theme.borderLeft}` : "none",
                        fontSize: 13,
                        color: theme.text,
                        wordBreak: "break-word",
                        overflowWrap: "anywhere",
                        whiteSpace: "pre-wrap",
                    }}
                >
                    {hasContent && <MessageContentRenderer content={message.content} theme={theme} isUser={isUser} />}
                    {message.sendFailed && (
                        <span
                            data-testid={`ve-msg-failed-${message.id}`}
                            style={{ color: theme.errorText || "#dc2626", fontSize: 11, marginLeft: hasContent ? 6 : 0 }}
                        >
                            {isZh ? "\u53d1\u9001\u5931\u8d25" : "Failed"}
                        </span>
                    )}
                </div>
            )}

            {hasAttachments && (
                <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 4, maxWidth: "80%" }}>
                    {message.attachments?.map((att, idx) => (
                        <AttachmentDisplay key={`${att.type}-${att.filename}-${att.fileUrl || att.localPath || idx}`} attachment={att} sessionId={sessionId || ""} theme={theme} />
                    ))}
                </div>
            )}
        </div>
    );
}

interface AttachmentDisplayProps {
    attachment: VEMessageAttachment;
    sessionId: string;
    theme: Theme;
    prefetchRemoteImage?: boolean;
}

function AttachmentDisplay({ attachment, sessionId, theme, prefetchRemoteImage = true }: AttachmentDisplayProps) {
    const [localPath, setLocalPath] = useState(attachment.localPath || "");
    const [opening, setOpening] = useState(false);
    const [imageFailed, setImageFailed] = useState(false);
    const [previewDataUrl, setPreviewDataUrl] = useState("");
    const previewLocalPathRef = useRef(attachment.localPath || "");
    const canOpen = !!localPath || !!previewLocalPathRef.current || (!!sessionId && !!attachment.fileUrl);
    const imageSrc = isImageAttachment(attachment) ? previewDataUrl : "";
    const showThumbnail = !!imageSrc && !imageFailed;
    const typeLabel = attachmentKindLabel(attachment.filename, attachment.type);
    const attachmentTitle = localPath || attachment.filename;

    useEffect(() => {
        if (attachment.localPath) {
            previewLocalPathRef.current = attachment.localPath;
            setLocalPath(attachment.localPath);
        }
    }, [attachment.localPath]);

    useEffect(() => {
        let cancelled = false;
        setImageFailed(false);
        setPreviewDataUrl("");
        if (!isImageAttachment(attachment) || !sessionId) return () => { cancelled = true; };
        const loadPreview = async () => {
            const mod = await getWailsAppModule();
            let previewPath = localPath;
            if (!previewPath && prefetchRemoteImage && attachment.fileUrl) {
                const safeUrl = safeAttachmentFileURL(attachment.fileUrl);
                if (safeUrl) {
                    const result = await (mod as any).GroupDiscussionDownloadAttachment?.(sessionId, safeUrl, attachment.filename);
                    previewPath = result?.local_path || result?.LocalPath || result?.localPath || "";
                    if (!cancelled && previewPath) {
                        previewLocalPathRef.current = previewPath;
                    }
                }
            }
            if (!previewPath) return { dataUrl: "", previewPath: "" };
            const dataUrl = await (mod as any).GroupDiscussionAttachmentPreviewDataURL?.(sessionId, previewPath);
            return { dataUrl, previewPath };
        };
        void loadPreview()
            .then(({ dataUrl, previewPath }) => {
                if (!cancelled && typeof dataUrl === "string" && dataUrl.startsWith("data:image/")) {
                    if (previewPath) {
                        previewLocalPathRef.current = previewPath;
                    }
                    setPreviewDataUrl(dataUrl);
                }
            })
            .catch(() => {
                if (!cancelled) setPreviewDataUrl("");
            });
        return () => { cancelled = true; };
    }, [attachment.fileUrl, attachment.filename, attachment.mimeType, attachment.type, localPath, prefetchRemoteImage, sessionId]);

    const openAttachment = async () => {
        if (!canOpen || opening) return;
        setOpening(true);
        try {
            const cachedPath = localPath || previewLocalPathRef.current;
            if (cachedPath) {
                const mod = await getWailsAppModule();
                await (mod as any).OpenFileOrShowInFolder?.(cachedPath);
                return;
            }
            if (!sessionId || !attachment.fileUrl) return;
            const safeUrl = safeAttachmentFileURL(attachment.fileUrl);
            if (!safeUrl) return;
            const mod = await getWailsAppModule();
            const result = await (mod as any).GroupDiscussionDownloadAttachment?.(sessionId, safeUrl, attachment.filename);
            const downloadedPath = result?.local_path || result?.LocalPath || result?.localPath || "";
            if (downloadedPath) {
                setLocalPath(downloadedPath);
                await (mod as any).OpenFileOrShowInFolder?.(downloadedPath);
            }
        } catch (err) {
            console.warn("Failed to open VE attachment", err);
        } finally {
            setOpening(false);
        }
    };

    if (isImageAttachment(attachment)) {
        return (
            <button
                type="button"
                data-testid={`ve-att-chip-${attachment.filename}`}
                title={attachmentTitle}
                aria-label={attachment.filename}
                disabled={!canOpen || opening}
                style={{
                    display: "inline-flex",
                    flexDirection: "column",
                    alignItems: "stretch",
                    gap: 5,
                    width: 116,
                    padding: 5,
                    borderRadius: 7,
                    background: theme.fieldBg,
                    border: `1px solid ${theme.divider}`,
                    color: theme.text,
                    font: "inherit",
                    opacity: opening ? 0.65 : 1,
                    cursor: canOpen && !opening ? "pointer" : "default",
                }}
                onClick={openAttachment}
            >
                {showThumbnail ? (
                    <img
                        data-testid={`ve-att-image-thumb-${attachment.filename}`}
                        src={imageSrc}
                        alt={attachment.filename}
                        loading="lazy"
                        onError={() => setImageFailed(true)}
                        style={{ width: "100%", height: 76, objectFit: "cover", borderRadius: 5, background: theme.bg, flexShrink: 0 }}
                    />
                ) : (
                    <div style={{ width: "100%", height: 76, display: "flex", alignItems: "center", justifyContent: "center", borderRadius: 5, background: theme.codeBlockBg || theme.bg }}>
                        <AttachmentTypeBadge label="IMG" theme={theme} />
                    </div>
                )}
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 11, fontWeight: 600 }}>
                    {attachment.filename}
                </span>
                {formatFileSize(attachment.sizeBytes ?? 0) && (
                    <span style={{ color: theme.textMuted, fontSize: 10 }}>{formatFileSize(attachment.sizeBytes ?? 0)}</span>
                )}
                {opening && <span style={{ color: theme.textMuted, fontSize: 10 }}>...</span>}
            </button>
        );
    }

    return (
        <button
            type="button"
            data-testid={`ve-att-chip-${attachment.filename}`}
            title={attachmentTitle}
            aria-label={attachment.filename}
            disabled={!canOpen || opening}
            style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 8,
                padding: "6px 8px",
                borderRadius: 7,
                background: theme.fieldBg,
                border: `1px solid ${theme.divider}`,
                fontSize: 11,
                color: theme.text,
                font: "inherit",
                maxWidth: 240,
                minWidth: 0,
                opacity: opening ? 0.65 : 1,
                cursor: canOpen && !opening ? "pointer" : "default",
            }}
            onClick={openAttachment}
        >
            <AttachmentTypeBadge label={typeLabel} theme={theme} />
            <span style={{ minWidth: 0, display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 2 }}>
                <span style={{ maxWidth: 150, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>
                    {attachment.filename}
                </span>
                <span style={{ color: theme.textMuted, fontSize: 10 }}>
                    {[typeLabel, formatFileSize(attachment.sizeBytes ?? 0)].filter(Boolean).join(" / ")}
                </span>
            </span>
            {opening && <span style={{ color: theme.textMuted }}>...</span>}
        </button>
    );
}

// --- Utility Functions ---

function authResultCandidateIds(value: unknown): string[] {
    const normalized = normalizeParticipantId(String(value || ""));
    if (!normalized) return [];
    const paired = normalized.startsWith("ve_") ? normalized.slice(3) : `ve_${normalized}`;
    return paired && paired !== normalized ? [normalized, paired] : [normalized];
}

function formatError(error: VEConversationError, isZh: boolean): string {
    switch (error.type) {
        case "hub_disconnected":
            return isZh ? "Hub \u8fde\u63a5\u4e2d\u65ad" : "Hub disconnected";
        case "ve_offline":
            return isZh ? "\u8be5\u6570\u5b57\u5458\u5de5\u5f53\u524d\u4e0d\u5728\u7ebf" : "Digital employee is offline";
        case "auth_pending":
            return isZh ? "\u6b63\u5728\u8bf7\u6c42\u8bbf\u95ee\uff0c\u7b49\u5f85\u5bf9\u65b9\u786e\u8ba4" : "Waiting for access confirmation";
        case "access_denied":
            return error.message === "blocked"
                ? (isZh ? "\u5f53\u524d\u65e0\u6cd5\u8bbf\u95ee\u8be5\u6570\u5b57\u5458\u5de5" : "This digital employee is unavailable")
                : (isZh ? "\u8bbf\u95ee\u672a\u901a\u8fc7\uff0c\u5bf9\u65b9\u6682\u672a\u5141\u8bb8\u672c\u6b21\u8bbf\u95ee" : "Access was not approved");
        case "send_failed":
            return error.message ? (isZh ? `\u6d88\u606f\u53d1\u9001\u5931\u8d25\uff1a${error.message}` : `Message send failed: ${error.message}`) : (isZh ? "\u6d88\u606f\u53d1\u9001\u5931\u8d25" : "Message send failed");
        case "session_timeout":
            return isZh ? "\u4f1a\u8bdd\u521b\u5efa\u8d85\u65f6\uff085\u79d2\uff09" : "Session creation timed out (5s)";
        default:
            return (error as VEConversationError).message;
    }
}
function extractErrorMessage(err: unknown): string {
    if (typeof err === "string") return err;
    if (!err || typeof err !== "object") return "";
    const rec = err as Record<string, unknown>;
    for (const key of ["message", "error", "detail", "details"]) {
        const value = rec[key];
        if (typeof value === "string" && value.trim()) return value.trim();
    }
    try {
        const json = JSON.stringify(err);
        return json && json !== "{}" ? json : "";
    } catch {
        return String(err || "");
    }
}

function classifyAttachmentType(filename: string | null | undefined): "text" | "image" | "file" {
    const ext = String(filename || "").toLowerCase().split(".").pop() || "";
    const imageExts = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "avif"];
    const textExts = ["txt", "md", "csv", "json", "xml", "yaml", "yml", "log", "go", "py", "js", "ts", "html", "css"];
    if (imageExts.includes(ext)) return "image";
    if (textExts.includes(ext)) return "text";
    return "file";
}

function attachmentKindLabel(filename: string | null | undefined, type?: "text" | "image" | "file"): string {
    if (type === "image") return "IMG";
    const ext = String(filename || "").match(/\.([^./\\]+)$/)?.[1]?.trim();
    if (!ext) return type === "text" ? "TXT" : "FILE";
    return ext.slice(0, 4).toUpperCase();
}

function isImageAttachment(attachment: VEMessageAttachment): boolean {
    return attachment.type === "image" || String(attachment.mimeType || "").toLowerCase().startsWith("image/") || classifyAttachmentType(attachment.filename) === "image";
}

function safeAttachmentFileURL(value: string | undefined): string {
    const url = String(value || "").trim();
    return url.startsWith("http://") || url.startsWith("https://") || url.startsWith("/") ? url : "";
}

function isDarkHexColor(value: string): boolean {
    const hex = String(value || "").trim().replace(/^#/, "");
    if (!/^[0-9a-f]{3}([0-9a-f]{3})?$/i.test(hex)) return false;
    const full = hex.length === 3 ? hex.split("").map((char) => char + char).join("") : hex;
    const r = parseInt(full.slice(0, 2), 16);
    const g = parseInt(full.slice(2, 4), 16);
    const b = parseInt(full.slice(4, 6), 16);
    return (r * 299 + g * 587 + b * 114) / 1000 < 96;
}

function normalizeVEMessageAttachments(raw: unknown): VEMessageAttachment[] {
    if (!Array.isArray(raw)) return [];
    const out: VEMessageAttachment[] = [];
    for (const item of raw) {
        if (!item || typeof item !== "object") continue;
        const rec = item as Record<string, unknown>;
        const filename = attachmentStringField(rec.filename) || attachmentStringField(rec.Filename) || attachmentStringField(rec.name) || "attachment";
        const mimeType = attachmentStringField(rec.mimeType) || attachmentStringField(rec.mime_type) || attachmentStringField(rec.MimeType) || undefined;
        const rawType = attachmentStringField(rec.type) || classifyAttachmentType(filename);
        const type = rawType === "image" || (mimeType || "").toLowerCase().startsWith("image/")
            ? "image"
            : rawType === "text"
            ? "text"
            : "file";
        const sizeRaw = rec.sizeBytes ?? rec.size_bytes ?? rec.SizeBytes;
        const sizeBytes = typeof sizeRaw === "number" ? sizeRaw : Number(sizeRaw || 0);
        out.push({
            type,
            filename,
            mimeType,
            fileUrl: attachmentStringField(rec.fileUrl) || attachmentStringField(rec.file_url) || attachmentStringField(rec.FileURL) || undefined,
            localPath: attachmentStringField(rec.localPath) || attachmentStringField(rec.local_path) || attachmentStringField(rec.LocalPath) || undefined,
            sizeBytes: Number.isFinite(sizeBytes) && sizeBytes > 0 ? sizeBytes : undefined,
        });
    }
    return out;
}

function veMessagesFromHistoryDetail(detail: VEHistoryDetail, veId: string, assistantName: string, userName: string): VEMessage[] {
    const messages = firstHistoryList(detail.messages, detail.Messages, detail.session?.messages, detail.session?.Messages);
    if (!messages.length) return [];
    const localIds = new Set(["initiator", "me", "user"]);
    for (const participant of detail.session?.participants || []) {
        const id = String(participant.id || participant.ID || "").trim();
        const role = String(participant.role_code || participant.roleCode || participant.RoleCode || "").trim().toLowerCase();
        if (id && role === "initiator") localIds.add(normalizeParticipantId(id));
    }

    const normalizedVEId = normalizeParticipantId(veId);
    const out: VEMessage[] = [];
    let streamIndex = -1;
    let streamFromId = "";

    messages.forEach((message, index) => {
        const kind = String(message.kind || message.Kind || "").trim().toLowerCase();
        const fromId = String(message.from_id || message.fromId || message.FromID || "").trim();
        const attachments = normalizeVEHistoryAttachments(message);
        const content = String(message.content || message.Content || "");
        if (kind === "stream_end") {
            if (streamIndex >= 0 && (!fromId || streamFromId === fromId)) {
                const existing = out[streamIndex];
                if (content) existing.content += content;
                existing.attachments = mergeVEMessageAttachments(existing.attachments || [], attachments);
                streamIndex = -1;
                streamFromId = "";
                return;
            }
            if (!content && attachments.length === 0) {
                streamIndex = -1;
                streamFromId = "";
                return;
            }
        }

        if (kind === "stream_chunk" && !content && attachments.length === 0) return;

        if (kind === "stream_chunk" && streamIndex >= 0 && streamFromId === fromId) {
            const existing = out[streamIndex];
            existing.content += content;
            existing.attachments = mergeVEMessageAttachments(existing.attachments || [], attachments);
            return;
        }

        const normalizedFromId = normalizeParticipantId(fromId);
        const isLocalUser = localIds.has(normalizedFromId);
        const isAssistant = normalizedFromId === normalizedVEId || (!isLocalUser && normalizedFromId !== "");
        const fromName = String(message.from_name || message.fromName || message.FromName || "").trim();
        const createdAt = message.created_at || message.createdAt || message.CreatedAt;
        out.push({
            id: String(message.id || message.ID || `history-${index}`),
            role: isAssistant ? "assistant" : "user",
            content,
            timestamp: createdAt ? Date.parse(createdAt) || Date.now() : Date.now(),
            fromId,
            fromName: fromName || (isAssistant ? assistantName : userName),
            attachments,
        });

        if (kind === "stream_chunk") {
            streamIndex = out.length - 1;
            streamFromId = fromId;
        } else {
            streamIndex = -1;
            streamFromId = "";
        }
    });

    return out;
}

function normalizeVEHistoryAttachments(message: VEHistoryMessage): VEMessageAttachment[] {
    const attachments = normalizeVEMessageAttachments(message.attachments || message.Attachments);
    for (const att of [...historyAttachmentList(message.text_attachments), ...historyAttachmentList(message.textAttachments), ...historyAttachmentList(message.TextAttachments)]) {
        const rec = att as Record<string, unknown>;
        attachments.push({
            type: "text",
            filename: attachmentStringField(rec.filename) || attachmentStringField(rec.Filename) || "text",
            mimeType: attachmentStringField(rec.mime_type) || attachmentStringField(rec.mimeType) || attachmentStringField(rec.MimeType) || undefined,
            localPath: attachmentStringField(rec.local_path) || attachmentStringField(rec.localPath) || attachmentStringField(rec.LocalPath) || undefined,
        });
    }
    for (const att of [...historyAttachmentList(message.image_attachments), ...historyAttachmentList(message.imageAttachments), ...historyAttachmentList(message.ImageAttachments)]) {
        const rec = att as Record<string, unknown>;
        attachments.push({
            type: "image",
            filename: attachmentStringField(rec.filename) || attachmentStringField(rec.Filename) || "image",
            mimeType: attachmentStringField(rec.mime_type) || attachmentStringField(rec.mimeType) || attachmentStringField(rec.MimeType) || undefined,
            fileUrl: attachmentStringField(rec.file_url) || attachmentStringField(rec.fileUrl) || attachmentStringField(rec.FileURL) || undefined,
            localPath: attachmentStringField(rec.local_path) || attachmentStringField(rec.localPath) || attachmentStringField(rec.LocalPath) || undefined,
        });
    }
    for (const att of [...historyAttachmentList(message.file_attachments), ...historyAttachmentList(message.fileAttachments), ...historyAttachmentList(message.FileAttachments)]) {
        const rec = att as Record<string, unknown>;
        const sizeRaw = rec.size_bytes ?? rec.sizeBytes ?? rec.SizeBytes;
        const sizeBytes = typeof sizeRaw === "number" ? sizeRaw : Number(sizeRaw || 0);
        attachments.push({
            type: "file",
            filename: attachmentStringField(rec.filename) || attachmentStringField(rec.Filename) || "file",
            mimeType: attachmentStringField(rec.mime_type) || attachmentStringField(rec.mimeType) || attachmentStringField(rec.MimeType) || undefined,
            fileUrl: attachmentStringField(rec.file_url) || attachmentStringField(rec.fileUrl) || attachmentStringField(rec.FileURL) || undefined,
            localPath: attachmentStringField(rec.local_path) || attachmentStringField(rec.localPath) || attachmentStringField(rec.LocalPath) || undefined,
            sizeBytes: Number.isFinite(sizeBytes) && sizeBytes > 0 ? sizeBytes : undefined,
        });
    }
    return mergeVEMessageAttachments([], attachments);
}

function historyAttachmentList<T>(value: T[] | undefined): T[] {
    return Array.isArray(value) ? value : [];
}

function firstHistoryList<T>(...values: Array<T[] | undefined>): T[] {
    for (const value of values) {
        if (Array.isArray(value) && value.length > 0) return value;
    }
    return [];
}

function attachmentStringField(value: unknown): string {
    if (typeof value !== "string") return "";
    return value.trim();
}

function mergeVEMessageAttachments(existing: VEMessageAttachment[], incoming: VEMessageAttachment[]): VEMessageAttachment[] {
    const out = [...existing];
    for (const att of incoming) {
        const existingIndex = out.findIndex((item) => sameVEMessageAttachment(item, att));
        if (existingIndex >= 0) {
            out[existingIndex] = {
                ...out[existingIndex],
                ...att,
                fileUrl: att.fileUrl || out[existingIndex].fileUrl,
                localPath: att.localPath || out[existingIndex].localPath,
                mimeType: att.mimeType || out[existingIndex].mimeType,
                sizeBytes: att.sizeBytes || out[existingIndex].sizeBytes,
            };
            continue;
        }
        out.push(att);
    }
    return out;
}

function sameVEMessageAttachment(a: VEMessageAttachment, b: VEMessageAttachment): boolean {
    if (a.type !== b.type || a.filename !== b.filename) return false;
    if ((a.sizeBytes || 0) > 0 && (b.sizeBytes || 0) > 0 && a.sizeBytes !== b.sizeBytes) return false;
    if (a.fileUrl && b.fileUrl) return a.fileUrl === b.fileUrl;
    if (a.localPath && b.localPath) return a.localPath === b.localPath;
    if (a.fileUrl || b.fileUrl || a.localPath || b.localPath) return true;
    return (a.sizeBytes || 0) > 0 && a.sizeBytes === b.sizeBytes;
}

function fileNameFromPath(filePath: string): string {
    const normalized = String(filePath || "").replace(/\\/g, "/");
    return normalized.split("/").filter(Boolean).pop() || String(filePath || "attachment");
}

function formatFileSize(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return "";
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

export { formatError, classifyAttachmentType, fileNameFromPath, formatFileSize, createSessionWithTimeout };
