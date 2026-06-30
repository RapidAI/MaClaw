import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { SendAIAssistantMessage, SendBtwQuery, ClearAIAssistantHistoryForSession, ClearAIAssistantUIState, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSessionForSession, CancelAIAssistantTask, SelectAIAssistantFiles, StartAIAssistantBackgroundTask, GetTrialReflectEnabled, GetAIAssistantTrace, LoadAIAssistantUIState, LoadConfig, ListRemoteSessions, ResolveCriticalConfirm, InjectAIAssistantSupplementaryForSession, InjectAIAssistantGuideReferenceForSession, SaveAIAssistantUIState, SubmitAgentView, DismissAgentView } from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";
import { EventsOn, EventsOff, EventsEmit } from "../../../wailsjs/runtime";
import type { AgentView } from "./agentViewTypes";
import type { AIAssistantPanelHookState, AIAssistantPanelHookActions } from "./aiAssistantPanelTypes";
import { localizeText } from "./aiAssistantI18n";
import { normalizeAssistantSessionKey, normalizeProjectSessionPath, projectPathFromSessionKey as normalizedProjectPathFromSessionKey, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { findRolePrefixForDisplay, stripRolePrefixForDisplay, truncateRolePrefixForDisplay } from "./rolePrefixDisplay";

export interface CancelAIAssistantResult {
    canceledText: string;
}

export interface StartAIAssistantBackgroundTaskResult {
    accepted?: boolean;
    mode?: string;
    session_id?: string;
    sessionID?: string;
    job_id?: string;
    jobID?: string;
    run_id?: string;
    runID?: string;
    error?: string;
}

export interface AIAssistantBackgroundLaunchResult {
    sessionID: string;
    jobID?: string;
    runID?: string;
}

interface AIAssistantSendResult {
    text?: string;
    Text?: string;
    error?: string;
    Error?: string;
    deferred?: boolean;
    Deferred?: boolean;
    keep_panel?: boolean;
    KeepPanel?: boolean;
    clear_ui?: boolean;
    ClearUI?: boolean;
    confirmed_resume?: boolean;
    ConfirmedResume?: boolean;
    fields?: any;
    Fields?: any;
    actions?: any;
    Actions?: any;
    confirmation?: AIAssistantResponseConfirmation;
    Confirmation?: AIAssistantResponseConfirmation;
    unfinished_task?: AIAssistantResponseUnfinishedSlot;
    UnfinishedTask?: AIAssistantResponseUnfinishedSlot;
    unfinished_slot?: AIAssistantResponseUnfinishedSlot;
    UnfinishedSlot?: AIAssistantResponseUnfinishedSlot;
    recoverable_session?: AIAssistantResponseRecoverableSession;
    RecoverableSession?: AIAssistantResponseRecoverableSession;
    local_file_path?: string;
    LocalFilePath?: string;
    local_file_paths?: string[];
    LocalFilePaths?: string[];
    thumbnail_base64?: string;
    ThumbnailBase64?: string;
    image_key?: string;
    ImageKey?: string;
    request_id?: string;
    RequestID?: string;
    session_key?: string;
    SessionKey?: string;
    response_source?: string;
    ResponseSource?: string;
    trace_status?: string;
    TraceStatus?: string;
    trace_summary?: string;
    TraceSummary?: string;
    trace_event_count?: number;
    TraceEventCount?: number;
    evidence_count?: number;
    EvidenceCount?: number;
    trial_reflect_summary?: string;
    TrialReflectSummary?: string;
    trial_reflect_status?: string;
    TrialReflectStatus?: string;
    trial_reflect_failures?: number;
    TrialReflectFailures?: number;
    job_id?: string;
    JobID?: string;
    run_id?: string;
    RunID?: string;
    input_tokens?: number;
    InputTokens?: number;
    output_tokens?: number;
    OutputTokens?: number;
    total_tokens?: number;
    TotalTokens?: number;
    cache_read_tokens?: number;
    CacheReadTokens?: number;
    cache_write_tokens?: number;
    CacheWriteTokens?: number;
    reasoning?: string;
    Reasoning?: string;
}

interface AIAssistantContextMessage {
    role: 'user' | 'assistant';
    content: string;
}

interface SendMessageOptions {
    resumeSlotID?: string;
    startNewTask?: boolean;
    dismissSlotID?: string;
    resumeSessionID?: string;
    dismissRecoverableSessionID?: string;
    lang?: string;
    uiAction?: boolean;
    displayText?: string;
    markConfirmationRunning?: boolean;
    /** Project path to include when sending from a Project Tab */
    project_path?: string;
    /** Tab ID (informational, not used for routing) */
    tabId?: string;
    /** Explicit context window for non-local tabs. Prevents local history bleed. */
    recentMessages?: AIAssistantContextMessage[];
}

function projectPathFromSessionKey(sessionKey?: string): string {
    return normalizedProjectPathFromSessionKey(sessionKey);
}

function deriveSendSessionKey(options?: SendMessageOptions): string {
    const sessionKey = projectSessionKey(options?.project_path);
    return sessionKey || 'desktop-user';
}

interface AIAssistantRemoteSessionView {
    id?: string;
    launch_source?: string;
    job_id?: string;
    run_id?: string;
    status?: string;
}

interface AIAssistantPendingTask {
    requestId: string;
    sessionKey?: string;
    sessionID?: string;
    jobID?: string;
    runID?: string;
}

interface AIAssistantStreamEvent {
    request_id?: string;
    text?: string;
    session_key?: string;
}

const AGENT_VIEW_EVENT = "agent-view";
const AGENT_VIEW_CLEAR_EVENT = "agent-view-clear";
const AGENT_VIEW_LIFECYCLE_EVENT = "agent-view:lifecycle";

type AgentViewLifecycleAction = "open" | "update" | "submit" | "dismiss" | "error" | "complete";

interface AgentViewLifecyclePayload {
    action: AgentViewLifecycleAction;
    view?: AgentView;
    view_id?: string;
    seq?: number;
    workflow_id?: string;
    workflow_phase?: string;
    workflow_user_id?: string;
    error?: string;
}

type DesktopPetState = 'idle' | 'listening' | 'thinking' | 'speaking';

function emitDesktopPetState(state: DesktopPetState, source: string, ttlMs?: number) {
    try {
        EventsEmit('pet:state', { state, source, ttlMs });
    } catch {
        // The desktop pet window may not be open; AI assistant flow should continue unaffected.
    }
}

export interface NewsCardData {
    articleId: string;
    category: NewsCategory;
    title: string;
    body: string;
    icon: string;
}

export type NewsCategory = 'notice' | 'update' | 'tip' | 'alert' | '';
export type AIAssistantInitStatus = 'connecting' | 'loading' | 'warming' | 'ready' | 'degraded';

export type ChatActionStyle = 'default' | 'primary' | 'secondary' | 'danger';

export interface ChatAction {
    label: string;
    command: string;
    style: ChatActionStyle;
}

export interface ChatConfirmation {
    id: string;
    summary: string;
    taskType?: string;
    targetPaths?: string[];
    plannedActions?: string[];
    riskFlags?: string[];
    revisionHints?: string[];
    status?: string;
    labels?: ChatConfirmationLabels;
}

export interface ChatConfirmationLabels {
    title?: string;
    status?: string;
    target_paths?: string;
    planned_actions?: string;
    risk_flags?: string;
    revision_hints?: string;
}

export interface ChatUnfinishedSlot {
    slotID?: string;
    title?: string;
    summary?: string;
    projectPath?: string;
    status?: string;
    actions?: ChatAction[];
}

export interface ChatRecoverableSession {
    sessionID?: string;
    tool?: string;
    title?: string;
    summary?: string;
    projectPath?: string;
    status?: string;
    exitReason?: string;
    resumeSessionID?: string;
    resumeCount?: number;
    lastProgress?: string;
    actions?: ChatAction[];
}

interface AIAssistantTraceView {
    job_id?: string;
    run_id?: string;
    status?: string;
    summary?: string;
    event_count?: number;
    evidence_count?: number;
    trial_reflect_summary?: {
        attempt_count?: number;
        attempted_tools?: string[];
        failure_count?: number;
        failure_categories?: string[];
        recovered?: boolean;
        final_outcome?: string;
        strategy_note?: string;
    };
    events?: Array<{ kind?: string; summary?: string }>;
    evidence?: Array<{ source_kind?: string; category?: string; summary?: string }>;
}

interface AIAssistantResponseConfirmation {
    id?: string;
    ID?: string;
    summary?: string;
    Summary?: string;
    task_type?: string;
    TaskType?: string;
    target_paths?: string[];
    TargetPaths?: string[];
    planned_actions?: string[];
    PlannedActions?: string[];
    risk_flags?: string[];
    RiskFlags?: string[];
    revision_hints?: string[];
    RevisionHints?: string[];
    status?: string;
    Status?: string;
    labels?: AIAssistantResponseConfirmationLabels;
    Labels?: AIAssistantResponseConfirmationLabels;
}

interface AIAssistantResponseConfirmationLabels {
    title?: string;
    Title?: string;
    status?: string;
    Status?: string;
    target_paths?: string;
    TargetPaths?: string;
    planned_actions?: string;
    PlannedActions?: string;
    risk_flags?: string;
    RiskFlags?: string;
    revision_hints?: string;
    RevisionHints?: string;
}

interface AIAssistantResponseUnfinishedSlot {
    slot_id?: string;
    SlotID?: string;
    title?: string;
    Title?: string;
    summary?: string;
    Summary?: string;
    project_path?: string;
    ProjectPath?: string;
    status?: string;
    Status?: string;
    actions?: ChatAction[];
    Actions?: ChatAction[];
}

interface AIAssistantResponseRecoverableSession {
    session_id?: string;
    SessionID?: string;
    tool?: string;
    Tool?: string;
    title?: string;
    Title?: string;
    summary?: string;
    Summary?: string;
    project_path?: string;
    ProjectPath?: string;
    status?: string;
    Status?: string;
    exit_reason?: string;
    ExitReason?: string;
    resume_session_id?: string;
    ResumeSessionID?: string;
    resume_count?: number;
    ResumeCount?: number;
    last_progress?: string;
    LastProgress?: string;
    actions?: ChatAction[];
    Actions?: ChatAction[];
}

interface AIAssistantPreferences {
    showTraceEntry: boolean;
}

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error' | 'system';
    kind?: 'news' | 'trace';
    content: string;
    /** Reasoning/thinking content from reasoning models (displayed as collapsed gray text). */
    reasoning?: string;
    news?: NewsCardData;
    fields?: Array<{ label: string; value: string }>;
    actions?: ChatAction[];
    confirmation?: ChatConfirmation;
    unfinishedSlot?: ChatUnfinishedSlot;
    recoverableSession?: ChatRecoverableSession;
    localFilePath?: string;
    localFilePaths?: string[];
    thumbnailBase64?: string;
    imageKey?: string;
    requestId?: string;
    /** Runtime owner. Project-tab messages must never be treated as local chat context. */
    sessionKey?: string;
    /** UI tab that initiated the round, used only for tab-local recovery/display. */
    tabId?: string;
    timestamp: number;
    /** Workflow document link - phase ID for opening doc preview. */
    workflowPhaseID?: string;
    /** Workflow document link label. */
    workflowDocLabel?: string;
}

// Auto-incrementing ID to avoid collisions from rapid messages / progress events.
let _nextMsgId = 1;
function nextId(): string {
    return `msg-${Date.now()}-${_nextMsgId++}`;
}

const STREAM_TOKEN_EVENT = "ai-assistant-token";
const NEW_ROUND_EVENT = "ai-assistant-new-round";
const STREAM_DONE_EVENT = "ai-assistant-stream-done";
const INIT_PROGRESS_EVENT = "ai-assistant-init-progress";
const PROGRESS_EVENT = "ai-assistant-progress";
const RESPONSE_EVENT = "ai-assistant-response";
const LOCAL_FORGET_SESSION_ROUNDS_EVENT = "ai-assistant:forget-session-rounds";
const LOCAL_ACTIVE_SESSION_CHANGED_EVENT = "ai-assistant:active-session-changed";
const MIN_REASONING_DEDUP_OVERLAP = 8;

// Module-level active session key. Updated by AIAssistantPanel when the active
// tab changes. The useAIAssistant hook reads this to filter events by session.
// This avoids prop-drilling through App.tsx -> AIAssistantPanel -> useAIAssistant.
let _activeSessionKey = '';
export function setActiveSessionKey(key: string) {
    const normalizedKey = String(key || '').trim();
    if (_activeSessionKey === normalizedKey) return;
    _activeSessionKey = normalizedKey;
    if (typeof window !== 'undefined') {
        window.dispatchEvent(new CustomEvent(LOCAL_ACTIVE_SESSION_CHANGED_EVENT, { detail: { sessionKey: normalizedKey } }));
    }
}
export function getActiveSessionKey(): string { return _activeSessionKey; }
export function forgetAIAssistantSessionRounds(sessionKey: string) {
    const normalizedSessionKey = String(sessionKey || '').trim();
    if (!normalizedSessionKey) return;
    window.dispatchEvent(new CustomEvent(LOCAL_FORGET_SESSION_ROUNDS_EVENT, { detail: { sessionKey: normalizedSessionKey } }));
}

// ---------------------------------------------------------------------------
// localStorage persistence for chat history across app restarts
// ---------------------------------------------------------------------------
export const AI_ASSISTANT_HISTORY_STORAGE_KEY = "ai-assistant-history";
export const AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY = "ai-assistant-prompt-history";
const AI_ASSISTANT_CONTEXT_BOUNDARY_STORAGE_KEY = "ai-assistant-context-boundary-message-id";
const AI_ASSISTANT_CONTEXT_BOUNDARY_END = "__maclaw_context_boundary_end__";
const AI_ASSISTANT_CONTEXT_BOUNDARY_AFTER_PREFIX = "__maclaw_context_boundary_after__:";
const MAX_PERSISTED_MESSAGES = 200;
const MAX_CONTEXT_MESSAGES_TO_SEND = 80;
const MAX_PERSISTED_PROMPTS = 100;
const FILE_PATH_PROMPT_PREFIX = "[用户选择的本地文件路径]";
const IMAGE_FILE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff"]);
const MAX_LIVE_PROGRESS_MESSAGES = 30;
const HEARTBEAT_PROGRESS_TEXT = "__heartbeat__";

function isHeartbeatProgressText(progressText: unknown): boolean {
    return String(progressText || '').trim() === HEARTBEAT_PROGRESS_TEXT;
}

function shouldHideProgressText(progressText: string): boolean {
    const trimmed = progressText.trim();
    if (!trimmed) return true;
    if (trimmed.includes("命令仍在执行中")) return true;
    return isHeartbeatProgressText(trimmed);
}

function removeMessageAtIndex(messages: ChatMessage[], index: number): ChatMessage[] {
    if (index < 0 || index >= messages.length) return messages;
    if (index === messages.length - 1) return messages.slice(0, -1);
    return [...messages.slice(0, index), ...messages.slice(index + 1)];
}

function appendProgressText(messages: ChatMessage[], progressText: string): ChatMessage[] {
    const lastMessage = messages[messages.length - 1];
    if (lastMessage?.content === progressText) {
        return messages;
    }
    if (lastMessage && progressSemanticKey(lastMessage.content) === progressSemanticKey(progressText)) {
        return messages;
    }
    const next = [...messages, {
        id: nextId(),
        role: 'progress' as const,
        content: progressText,
        timestamp: Date.now(),
    }];
    if (next.length <= MAX_LIVE_PROGRESS_MESSAGES) return next;
    return next.slice(-MAX_LIVE_PROGRESS_MESSAGES);
}

function progressSemanticKey(progressText: string): string {
    const trimmed = progressText.trim();
    const prefix = "Coding Agent Event:";
    if (!trimmed.startsWith(prefix)) return trimmed;
    try {
        const raw = JSON.parse(trimmed.slice(prefix.length).trim()) as Record<string, unknown>;
        if (raw.agent !== "coding") return trimmed;
        const stable = {
            agent: raw.agent,
            count: raw.count,
            detail: raw.detail,
            duration_ms: raw.duration_ms,
            event: raw.event,
            files: raw.files,
            outcome: raw.outcome,
            phase: raw.phase,
            run_id: raw.run_id,
            summary: raw.summary,
            task_id: raw.task_id,
            title: raw.title,
            turn_id: raw.turn_id,
            version: raw.version,
        };
        return `${prefix} ${JSON.stringify(stable)}`;
    } catch {
        return trimmed;
    }
}

function normalizeLocalFilePaths(localFilePath: unknown, localFilePaths: unknown): string[] | undefined {
    const normalized = new Set<string>();
    if (typeof localFilePath === 'string' && localFilePath.trim()) {
        normalized.add(localFilePath.trim());
    }
    if (Array.isArray(localFilePaths)) {
        for (const entry of localFilePaths) {
            if (typeof entry !== 'string') continue;
            const trimmed = entry.trim();
            if (!trimmed) continue;
            normalized.add(trimmed);
        }
    }
    return normalized.size > 0 ? Array.from(normalized) : undefined;
}

function firstStringValue(...values: unknown[]): string {
    for (const value of values) {
        if (typeof value !== 'string') continue;
        const trimmed = value.trim();
        if (trimmed) return trimmed;
    }
    return '';
}

function firstStringArrayValue(...values: unknown[]): string[] | undefined {
    for (const value of values) {
        if (Array.isArray(value)) return value;
    }
    return undefined;
}

function responseArtifactPayload(response: any): { localFilePath: string; localFilePaths?: string[]; thumbnailBase64: string; imageKey: string } {
    const localFilePath = firstStringValue(response?.local_file_path, response?.LocalFilePath);
    const localFilePaths = normalizeLocalFilePaths(
        localFilePath,
        firstStringArrayValue(response?.local_file_paths, response?.LocalFilePaths),
    );
    const thumbnailBase64 = firstStringValue(response?.thumbnail_base64, response?.ThumbnailBase64);
    const imageKey = firstStringValue(response?.image_key, response?.ImageKey);
    return { localFilePath, localFilePaths, thumbnailBase64, imageKey };
}

export function isImageFilePath(filePath: string): boolean {
    const normalized = filePath.trim().toLowerCase();
    if (!normalized) return false;
    const queryIndex = normalized.search(/[?#]/);
    const pathname = queryIndex >= 0 ? normalized.slice(0, queryIndex) : normalized;
    return Array.from(IMAGE_FILE_EXTENSIONS).some(ext => pathname.endsWith(ext));
}

export function buildOutgoingMessage(text: string, selectedFilePath: string): string {
    const trimmedText = text.trim();
    const trimmedPath = selectedFilePath.trim();
    if (!trimmedPath) return trimmedText;
    const pathInstructions = isImageFilePath(trimmedPath)
        ? "User provided this local image file. Do not call screenshot or re-capture it; use the path directly and prefer read_file or open to inspect it before answering."
        : "Use this path directly; if content inspection is needed, use read_file, open, or related tools.";
    const fileBlock = [
        FILE_PATH_PROMPT_PREFIX,
        trimmedPath,
        pathInstructions,
    ].join("\n");
    return trimmedText ? `${trimmedText}\n\n${fileBlock}` : fileBlock;
}

export function buildOutgoingMessageMulti(text: string, filePaths: string[]): string {
    const trimmedText = text.trim();
    const validPaths = filePaths.map(p => p.trim()).filter(Boolean);
    if (validPaths.length === 0) return trimmedText;

    const hasImages = validPaths.some(isImageFilePath);
    const pathInstructions = hasImages
        ? "User provided these local files. For image files, do not call screenshot or re-capture them; use the paths directly."
        : "Use these paths directly; if content inspection is needed, use read_file, open, or related tools.";

    const fileBlock = [
        FILE_PATH_PROMPT_PREFIX,
        ...validPaths,
        pathInstructions,
    ].join("\n");

    return trimmedText ? `${trimmedText}\n\n${fileBlock}` : fileBlock;
}

function normalizeSelectedFilePath(filePath: string): string {
    return filePath.trim();
}

function loadPersistedMessages(): ChatMessage[] {
    try {
        const raw = localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        // Only restore user / assistant / error messages
        // Skip transient progress and system (news) messages
        return parsed.filter(
            (m: any) => m && m.id && m.role && m.role !== 'progress' && m.role !== 'system'
        ).map(sanitizeChatMessageForDisplay) as ChatMessage[];
    } catch {
        return [];
    }
}

function stripRolePrefixReasoning(text: string): string {
    return truncateRolePrefixForDisplay(text);
}

function sanitizeChatMessageForDisplay(message: ChatMessage): ChatMessage {
    if (message.role !== 'assistant') return message;
    const nextContent = stripRolePrefixFrontend(stripAssistantProtocolArtifactsFrontend(message.content || ''));
    const nextReasoning = message.reasoning ? stripRolePrefixReasoning(message.reasoning) : message.reasoning;
    if (nextContent === message.content && nextReasoning === message.reasoning) return message;
    return {
        ...message,
        content: nextContent,
        reasoning: nextReasoning || undefined,
    };
}

function stripAssistantProtocolArtifactsFrontend(text: string): string {
    if (!text) return text;
    let out = text
        .replace(/<details\b[\s\S]*?<\/details>/gi, '')
        .replace(/<think\b[^>]*>[\s\S]*?<\/think>/gi, '')
        .replace(/<\|FunctionCallBegin\|>[\s\S]*?(?:<\|FunctionCallEnd\|>|$)/g, '')
        .replace(/<turn:\s*tool_call\b[\s\S]*?(?:<\/turn>|$)/gi, '');

    const lower = out.toLowerCase();
    const markers = ['<tool_call', '<functioncall', '<|functioncallbegin|>'];
    let cut = -1;
    for (const marker of markers) {
        const idx = lower.indexOf(marker);
        if (idx >= 0 && (cut < 0 || idx < cut)) cut = idx;
    }
    if (cut >= 0) {
        out = out.slice(0, cut);
    }

    const trimmed = out.trim();
    if (looksLikeStandaloneToolCallJSON(trimmed)) {
        return '';
    }
    return trimmed;
}

function looksLikeStandaloneToolCallJSON(text: string): boolean {
    if (!text || text[0] !== '{') return false;
    if (!/"(?:name|tool_name|function)"\s*:/.test(text)) return false;
    if (!/"(?:arguments|args|input)"\s*:/.test(text)) return false;
    try {
        const parsed = JSON.parse(text);
        return !!parsed && typeof parsed === 'object' && !Array.isArray(parsed);
    } catch {
        return false;
    }
}

function loadPersistedPrompts(): string[] {
    try {
        const raw = localStorage.getItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter((value: unknown): value is string => typeof value === 'string')
            .map(value => value.trim())
            .filter(Boolean)
            .slice(-MAX_PERSISTED_PROMPTS);
    } catch {
        return [];
    }
}

function loadPersistedContextBoundaryMessageID(): string | null {
    try {
        const value = localStorage.getItem(AI_ASSISTANT_CONTEXT_BOUNDARY_STORAGE_KEY);
        return value && value.trim() ? value : null;
    } catch {
        return null;
    }
}

function persistContextBoundaryMessageID(messageID: string | null) {
    try {
        if (!messageID || !messageID.trim()) {
            localStorage.removeItem(AI_ASSISTANT_CONTEXT_BOUNDARY_STORAGE_KEY);
        }
    } catch {
        // localStorage full or unavailable - silently ignore
    }
}

function resolveContextStartIndex(messages: ChatMessage[], boundaryMessageID: string | null): number {
    if (!boundaryMessageID) return 0;
    if (boundaryMessageID === AI_ASSISTANT_CONTEXT_BOUNDARY_END) return messages.length;
    const afterPrefix = AI_ASSISTANT_CONTEXT_BOUNDARY_AFTER_PREFIX;
    const startAfterBoundary = boundaryMessageID.startsWith(afterPrefix);
    const targetMessageID = startAfterBoundary ? boundaryMessageID.slice(afterPrefix.length) : boundaryMessageID;
    const index = messages.findIndex(message => message.id === targetMessageID);
    if (index < 0) return 0;
    return startAfterBoundary ? index + 1 : index;
}

function isExplicitHistoryResetCommand(text: string): boolean {
    const trimmed = text.trim().toLowerCase();
    return trimmed === "/new" || trimmed === "/reset" || trimmed === "/clear";
}


function serializePersistedMessages(msgs: ChatMessage[]): string | null {
    // Only persist meaningful messages; skip progress/system and blank shells.
    // Strip image payloads to avoid blowing up localStorage (5MB limit).
    const toSave = msgs
        .filter(hasPersistableMessageContent)
        .filter(isLocalChatMessage)
        .slice(-MAX_PERSISTED_MESSAGES)
        .map(sanitizeChatMessageForDisplay)
        .map(m => {
            if (!m.thumbnailBase64 && !m.imageKey) return m;
            const { thumbnailBase64: _, imageKey: __, ...rest } = m;
            return rest;
        });
    return toSave.length === 0 ? null : JSON.stringify(toSave);
}

function isLocalChatMessage(message: ChatMessage): boolean {
    const sessionKey = typeof message.sessionKey === 'string' ? message.sessionKey.trim() : '';
    return !sessionKey || sessionKey === 'desktop-user';
}

function hasPersistableMessageContent(message: ChatMessage): boolean {
    if (message.role === 'progress' || message.role === 'system') return false;
    if (message.content.trim() !== '') return true;
    if (message.reasoning?.trim()) return true;
    if (message.fields?.length) return true;
    if (message.actions?.length) return true;
    if (message.confirmation) return true;
    if (message.unfinishedSlot) return true;
    if (message.recoverableSession) return true;
    if (message.localFilePath || message.localFilePaths?.length || message.thumbnailBase64 || message.imageKey) return true;
    return false;
}

function buildClientContextMessages(messages: ChatMessage[], startIndex = 0): AIAssistantContextMessage[] {
    return messages
        .slice(Math.max(0, startIndex))
        .filter(isLocalChatMessage)
        .map(message => {
            if (message.role !== 'user' && message.role !== 'assistant') return null;
            const content = buildClientContextContent(message);
            if (!content) return null;
            return { role: message.role, content };
        })
        .filter((message): message is AIAssistantContextMessage => message !== null)
        .slice(-MAX_CONTEXT_MESSAGES_TO_SEND)
}

function buildClientContextContent(message: ChatMessage): string {
    const parts: string[] = [];
    const text = message.content.trim();
    if (text) parts.push(text);
    if (message.unfinishedSlot) {
        const slot = message.unfinishedSlot;
        const detail = [
            slot.title ? `title=${slot.title}` : '',
            slot.status ? `status=${slot.status}` : '',
            slot.summary ? `summary=${slot.summary}` : '',
            slot.projectPath ? `project=${slot.projectPath}` : '',
        ].filter(Boolean).join('; ');
        parts.push(`Assistant showed unfinished task card${detail ? `: ${detail}` : '.'}`);
    }
    if (message.recoverableSession) {
        const session = message.recoverableSession;
        const progress = session.summary || session.lastProgress || '';
        const detail = [
            session.title ? `title=${session.title}` : '',
            session.status ? `status=${session.status}` : '',
            progress ? `progress=${progress}` : '',
            session.projectPath ? `project=${session.projectPath}` : '',
        ].filter(Boolean).join('; ');
        parts.push(`Assistant showed recoverable session card${detail ? `: ${detail}` : '.'}`);
    }
    return parts.join('\n').trim();
}

function buildAIAssistantSendPayload(
    outgoingText: string,
    requestId: string,
    recentMessages: AIAssistantContextMessage[],
    options?: SendMessageOptions,
) {
    const payload: Record<string, unknown> = {
        text: outgoingText,
        request_id: requestId,
    };
    if (recentMessages.length > 0) payload.recent_messages = recentMessages;
    if (options?.resumeSlotID) payload.resume_slot_id = options.resumeSlotID;
    if (options?.startNewTask !== undefined) payload.start_new_task = options.startNewTask;
    if (options?.dismissSlotID) payload.dismiss_slot_id = options.dismissSlotID;
    if (options?.resumeSessionID) payload.resume_session_id = options.resumeSessionID;
    if (options?.dismissRecoverableSessionID) payload.dismiss_recoverable_session_id = options.dismissRecoverableSessionID;
    if (options?.lang) payload.lang = options.lang;
    if (options?.uiAction !== undefined) payload.ui_action = options.uiAction;
    if (options?.project_path) payload.project_path = normalizeProjectSessionPath(options.project_path);
    if (options?.tabId) payload.event_scope_id = options.tabId;
    return payload;
}

function appendSubmittedPrompt(prompts: string[], prompt: string): string[] {
    const trimmed = prompt.trim();
    if (!trimmed) return prompts;
    if (prompts[prompts.length - 1] === trimmed) return prompts;
    return [...prompts, trimmed].slice(-MAX_PERSISTED_PROMPTS);
}

function normalizePersistedPrompts(prompts: string[]): string[] {
    return prompts
        .map(prompt => prompt.trim())
        .filter(Boolean)
        .slice(-MAX_PERSISTED_PROMPTS);
}

function parseSerializedMessages(serialized: string | null): ChatMessage[] {
    if (!serialized) return [];
    try {
        const parsed = JSON.parse(serialized);
        return Array.isArray(parsed) ? parsed.map(sanitizeChatMessageForDisplay) as ChatMessage[] : [];
    } catch {
        return [];
    }
}

function legacyClearAIAssistantUIState() {
    try {
        localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        localStorage.removeItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
        localStorage.removeItem(AI_ASSISTANT_CONTEXT_BOUNDARY_STORAGE_KEY);
    } catch {
        // localStorage unavailable - ignore
    }
}

function backendStateMessages(state: any): ChatMessage[] {
    const raw = state?.messages || state?.Messages || [];
    if (!Array.isArray(raw)) return [];
    return raw
        .filter((m: any) => m && m.id && m.role && m.role !== 'progress' && m.role !== 'system')
        .map(sanitizeChatMessageForDisplay) as ChatMessage[];
}

function backendStatePrompts(state: any): string[] {
    const raw = state?.prompts || state?.Prompts || [];
    return Array.isArray(raw) ? normalizePersistedPrompts(raw.filter((value: unknown): value is string => typeof value === 'string')) : [];
}

function backendStateBoundary(state: any): string | null {
    const value = String(state?.context_boundary_message_id || state?.ContextBoundaryMessageID || '').trim();
    return value || null;
}

interface ActiveRound {
    generation: number;
    phase: 'idle' | 'requesting' | 'streaming';
    assistantMessageId: string | null;
    requestId: string;
    sessionKey?: string;
    /** Original user text, used by the response event handler for clear_ui decisions. */
    userText?: string;
}

interface StreamTokenBuffer {
    requestId: string;
    assistantMessageId: string;
    text: string;
    flushTimer: ReturnType<typeof setTimeout> | null;
    hasRenderedFirstToken: boolean;
}

const STREAM_SNAPSHOT_DEDUP_MIN_CHARS = 16;
const STREAM_SNAPSHOT_OVERLAP_MIN_CHARS = 24;

interface StreamAppendResult {
    delta: string;
    snapshotMode: boolean;
}

interface StreamAppendState {
    content: string;
    reasoning: string;
    contentSnapshotMode: boolean;
    reasoningSnapshotMode: boolean;
}

interface ResponseTimeoutController {
    generation: number;
    requestId: string;
    assistantMessageId: string;
    reset: () => void;
    stop: () => void;
}

const STREAM_TOKEN_FLUSH_MS = 33;

function createIdleRound(generation: number): ActiveRound {
    return {
        generation,
        phase: 'idle',
        assistantMessageId: null,
        requestId: '',
    };
}

function isRoundIdle(round: ActiveRound): boolean {
    return round.phase === 'idle' && round.assistantMessageId === null;
}

function sameActiveRound(left: ActiveRound, right: ActiveRound): boolean {
    return left.generation === right.generation
        && left.phase === right.phase
        && left.assistantMessageId === right.assistantMessageId
        && left.requestId === right.requestId
        && (left.sessionKey || '') === (right.sessionKey || '');
}

const IDLE_ROUND: ActiveRound = createIdleRound(0);

function isAssistantPlaceholder(msg: ChatMessage): boolean {
    return msg.role === 'assistant' && msg.content === '' && !msg.reasoning && !msg.fields?.length && !msg.thumbnailBase64 && !msg.localFilePaths?.length && !msg.localFilePath;
}

function appendAssistantPlaceholder(messages: ChatMessage[], assistantMessageId: string, requestId = '', sessionKey?: string): ChatMessage[] {
    const index = messages.findIndex(msg => msg.id === assistantMessageId);
    if (index >= 0) return messages;
    const ownerSessionKey = normalizeRuntimeSessionKey(sessionKey || getActiveSessionKey() || 'desktop-user');
    return [...messages, {
        id: assistantMessageId,
        role: 'assistant',
        content: '',
        requestId: requestId || undefined,
        sessionKey: ownerSessionKey,
        timestamp: Date.now(),
    }];
}

function updateTailMessage(messages: ChatMessage[], messageId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] | null {
    if (!messageId || messages.length === 0) return null;
    const lastIndex = messages.length - 1;
    if (messages[lastIndex].id !== messageId) return null;
    const updated = updater(messages[lastIndex]);
    if (updated === messages[lastIndex]) return messages;
    if (updated === null) {
        return messages.slice(0, -1);
    }
    const next = [...messages];
    next[lastIndex] = updated;
    return next;
}

function updateMessageById(messages: ChatMessage[], messageId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    if (!messageId) return messages;
    const index = findLastIndex(messages, msg => msg.id === messageId);
    if (index < 0) return messages;
    const updated = updater(messages[index]);
    if (updated === messages[index]) return messages;
    if (updated === null) {
        return removeMessageAtIndex(messages, index);
    }
    const next = [...messages];
    next[index] = updated;
    return next;
}

function updateMessageByRequestId(messages: ChatMessage[], requestId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
    if (!normalizedRequestId) return messages;
    const index = findLastIndex(messages, msg => msg.role === 'assistant' && msg.requestId === normalizedRequestId);
    if (index < 0) return messages;
    const updated = updater(messages[index]);
    if (updated === messages[index]) return messages;
    if (updated === null) {
        return removeMessageAtIndex(messages, index);
    }
    const next = [...messages];
    next[index] = updated;
    return next;
}

function hasRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null): boolean {
    if (assistantMessageId && findLastIndex(messages, msg => msg.id === assistantMessageId) >= 0) {
        return true;
    }
    const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
    return !!normalizedRequestId && findLastIndex(messages, msg => msg.role === 'assistant' && msg.requestId === normalizedRequestId) >= 0;
}

function updateRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    const updatedTail = updateTailMessage(messages, assistantMessageId, updater);
    if (updatedTail) return updatedTail;
    const updatedById = updateMessageById(messages, assistantMessageId, updater);
    if (updatedById !== messages) return updatedById;
    return updateMessageByRequestId(messages, requestId, updater);
}

function markLatestConfirmationAsRunning(messages: ChatMessage[]): ChatMessage[] {
    const index = findLastIndex(messages, message => message.role === 'assistant' && !!message.confirmation);
    if (index < 0) return messages;
    const target = messages[index];
    const confirmation = target.confirmation;
    if (!confirmation) return messages;
    const nextStatus = 'running';
    if (confirmation.status === nextStatus) return messages;
    const next = [...messages];
    next[index] = {
        ...target,
        confirmation: {
            ...confirmation,
            status: nextStatus,
        },
    };
    return next;
}

function findLatestConfirmationAction(messages: ChatMessage[], command: string): ChatAction | undefined {
    const index = findLastIndex(messages, message => message.role === 'assistant' && !!message.confirmation);
    if (index < 0) return undefined;
    return messages[index].actions?.find(action => action.command === command);
}

function isConfirmationApprovalAction(action: ChatAction | undefined): boolean {
    if (!action) return false;
    const text = `${action.label} ${action.command}`.toLocaleLowerCase();
    if (text.includes('cancel') || text.includes('reject')) return false;
    return true;
}

function localizedExecutionActionText(command: string, label: string | undefined, lang: string): string | undefined {
    const normalizedLabel = (label || '').trim().toLowerCase();
    if (/^__confirm_execution__\s+\S+$/.test(command)) {
        if (!normalizedLabel || normalizedLabel === 'confirm and start') {
            return localizeText(lang, "Confirm and start", "\u786e\u8ba4\u5e76\u5f00\u59cb", "\u78ba\u8a8d\u4e26\u958b\u59cb");
        }
        return label;
    }
    if (/^__cancel_execution__\s+\S+$/.test(command)) {
        if (!normalizedLabel || normalizedLabel === 'cancel') {
            return localizeText(lang, "Cancel", "\u53d6\u6d88", "\u53d6\u6d88");
        }
        return label;
    }
    return label;
}

function appendTokenToMessage(message: ChatMessage, delta: string): ChatMessage {
    // Reasoning tokens are prefixed with \x01 by the backend to distinguish
    // them from content tokens. They represent the model's thinking phase.
    if (delta.startsWith('\x01')) {
        const reasoningDelta = delta.slice(1);
        if (!reasoningDelta) return message;
        const rawNextReasoning = message.reasoning ? message.reasoning + reasoningDelta : reasoningDelta;
        const nextReasoning = stripRolePrefixReasoning(rawNextReasoning);
        if (nextReasoning === message.reasoning) return message;
        return { ...message, reasoning: nextReasoning || undefined };
    }

    const contentDelta = delta;
    if (!contentDelta) return message;
    const rawNextContent = message.content ? message.content + contentDelta : contentDelta;
    const nextContent = stripRolePrefixFrontend(rawNextContent);
    logRolePrefixDiagnostic('append-token', rawNextContent, nextContent, {
        messageId: message.id,
        requestId: message.requestId,
        deltaLen: contentDelta.length,
    });
    if (nextContent === message.content) return message;
    return {
        ...message,
        content: nextContent,
    };
}

function streamAppendDelta(existing: string, incoming: string, snapshotMode = false): StreamAppendResult {
    if (!existing || !incoming) return { delta: incoming, snapshotMode: false };
    const existingLen = Array.from(existing.trim()).length;
    const incomingLen = Array.from(incoming.trim()).length;
    if (incoming === existing && incomingLen >= STREAM_SNAPSHOT_DEDUP_MIN_CHARS) {
        return snapshotMode ? { delta: '', snapshotMode: true } : { delta: incoming, snapshotMode: false };
    }
    if (incoming.startsWith(existing) && existingLen >= STREAM_SNAPSHOT_DEDUP_MIN_CHARS) {
        return { delta: incoming.slice(existing.length), snapshotMode: true };
    }
    if (existing.endsWith(incoming) && incomingLen >= STREAM_SNAPSHOT_OVERLAP_MIN_CHARS) {
        return snapshotMode ? { delta: '', snapshotMode: true } : { delta: incoming, snapshotMode: false };
    }
    const existingChars = Array.from(existing);
    const incomingChars = Array.from(incoming);
    const maxOverlap = Math.min(existingChars.length, incomingChars.length);
    for (let overlap = maxOverlap; overlap >= STREAM_SNAPSHOT_OVERLAP_MIN_CHARS; overlap--) {
        const incomingPrefix = incomingChars.slice(0, overlap).join('');
        if (existing.endsWith(incomingPrefix)) {
            return { delta: incomingChars.slice(overlap).join(''), snapshotMode: true };
        }
    }
    return { delta: incoming, snapshotMode: false };
}

function normalizeStreamDeltaWithState(streamState: StreamAppendState, incoming: string): string {
    if (!incoming) return '';
    if (incoming.startsWith('\x01')) {
        const incomingReasoning = stripRolePrefixReasoning(incoming.slice(1));
        const result = streamAppendDelta(streamState.reasoning, incomingReasoning, streamState.reasoningSnapshotMode);
        if (result.snapshotMode) streamState.reasoningSnapshotMode = true;
        if (!result.delta) return '';
        streamState.reasoning = stripRolePrefixReasoning(streamState.reasoning + result.delta);
        return `\x01${result.delta}`;
    }
    const incomingContent = stripRolePrefixFrontend(incoming);
    const result = streamAppendDelta(streamState.content, incomingContent, streamState.contentSnapshotMode);
    if (result.snapshotMode) streamState.contentSnapshotMode = true;
    if (!result.delta) return '';
    // Keep state aligned with display text, but return the raw delta. Split
    // prefixes such as "Brow" + "ser: ..." need the second raw piece so the
    // message write can strip the full reconstructed prefix from the UI text.
    streamState.content = stripRolePrefixFrontend(streamState.content + result.delta);
    return result.delta;
}

function appendTokenToRound(messages: ChatMessage[], assistantMessageId: string | null, delta: string): ChatMessage[] {
    if (!assistantMessageId || !delta || messages.length === 0) return messages;
    const lastIndex = messages.length - 1;
    const tail = messages[lastIndex];
    if (tail.id === assistantMessageId) {
        const updatedTail = appendTokenToMessage(tail, delta);
        if (updatedTail === tail) return messages;
        const next = [...messages];
        next[lastIndex] = updatedTail;
        return next;
    }
    const index = findLastIndex(messages, message => message.id === assistantMessageId);
    if (index < 0) return messages;
    const updatedMessage = appendTokenToMessage(messages[index], delta);
    if (updatedMessage === messages[index]) return messages;
    const next = [...messages];
    next[index] = updatedMessage;
    return next;
}

function checkInitReadiness(): Promise<{ ready: boolean; status: AIAssistantInitStatus }> {
    return Promise.allSettled([IsAIAssistantReady(), GetAIAssistantInitStatus()]).then(([readyResult, statusResult]) => ({
        ready: readyResult.status === 'fulfilled' && !!readyResult.value,
        status: statusResult.status === 'fulfilled' ? normalizeInitStatus(statusResult.value) : 'connecting',
    }));
}

function replaceNewsMessages(messages: ChatMessage[], newsMessages: ChatMessage[]): ChatMessage[] {
    const existingNews = messages.filter(isPinnedNewsMessage);
    if (existingNews.length === newsMessages.length && existingNews.every((msg, idx) => samePinnedNews(msg, newsMessages[idx]))) {
        return messages;
    }
    const filtered = messages.filter(message => !isPinnedNewsMessage(message));
    return [...newsMessages, ...filtered];
}

function serializeNewsMessages(newsMessages: ChatMessage[]): string {
    return JSON.stringify(newsMessages.map(message => ({
        id: message.id,
        content: message.content,
        news: message.news,
    })));
}

function normalizeTraceFields(response: any, showTraceEntry: boolean): Array<{ label: string; value: string }> {
    if (!showTraceEntry) return [];
    const fields: Array<{ label: string; value: string }> = [];
    const traceSummary = typeof response?.trace_summary === 'string' ? response.trace_summary.trim() : '';
    const trialReflectSummary = typeof response?.trial_reflect_summary === 'string' ? response.trial_reflect_summary.trim() : '';
    const trialReflectStatus = typeof response?.trial_reflect_status === 'string' ? response.trial_reflect_status.trim() : '';
    const trialReflectFailures = typeof response?.trial_reflect_failures === 'number' ? response.trial_reflect_failures : 0;
    const traceEventCount = typeof response?.trace_event_count === 'number' ? response.trace_event_count : 0;
    const evidenceCount = typeof response?.evidence_count === 'number' ? response.evidence_count : 0;
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    const jobID = typeof response?.job_id === 'string' ? response.job_id.trim() : '';
    if (traceSummary) {
        fields.push({ label: 'Trace', value: traceSummary });
    }
    if (trialReflectStatus) {
        fields.push({ label: 'Recovery', value: formatRecoveryStatus(trialReflectStatus) });
    }
    if (trialReflectFailures > 0) {
        fields.push({ label: 'Failures', value: String(trialReflectFailures) });
    }
    if (trialReflectSummary) {
        fields.push({ label: 'Trial reflect', value: trialReflectSummary });
    }
    if (traceEventCount > 0) {
        fields.push({ label: 'Trace events', value: String(traceEventCount) });
    }
    if (evidenceCount > 0) {
        fields.push({ label: 'Evidence', value: String(evidenceCount) });
    }
    if (runID) {
        fields.push({ label: 'Run ID', value: runID });
    }
    if (jobID) {
        fields.push({ label: 'Job ID', value: jobID });
    }
    return fields;
}

function isTokenFieldLabel(label: string): boolean {
    const normalized = label.trim().toLowerCase();
    return normalized === 'input tokens'
        || normalized === 'output tokens'
        || normalized === 'total tokens'
        || normalized === 'cache read tokens'
        || normalized === 'cache write tokens';
}

function numericTokenFieldValue(...values: unknown[]): number | undefined {
    for (const value of values) {
        if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value;
    }
    return undefined;
}

function tokenUsageCounterFields(raw: AIAssistantSendResult, showDetailEntry: boolean, existingFields?: Array<{ label: string; value: string }>): Array<{ label: string; value: string }> {
    if (!showDetailEntry) return [];
    const existingLabels = new Set((existingFields || []).map(field => field.label.trim().toLowerCase()));
    const fields: Array<{ label: string; value: string }> = [];
    const add = (label: string, value?: number) => {
        if (!value || existingLabels.has(label.toLowerCase())) return;
        fields.push({ label, value: String(value) });
    };
    const input = numericTokenFieldValue(raw.input_tokens, raw.InputTokens);
    const output = numericTokenFieldValue(raw.output_tokens, raw.OutputTokens);
    const total = numericTokenFieldValue(raw.total_tokens, raw.TotalTokens) ?? ((input || output) ? (input || 0) + (output || 0) : undefined);
    add('Input tokens', input);
    add('Output tokens', output);
    add('Total tokens', total);
    add('Cache read tokens', numericTokenFieldValue(raw.cache_read_tokens, raw.CacheReadTokens));
    add('Cache write tokens', numericTokenFieldValue(raw.cache_write_tokens, raw.CacheWriteTokens));
    return fields;
}
function normalizeResponseFields(fields: any, showDetailEntry = false): Array<{ label: string; value: string }> | undefined {
    if (!Array.isArray(fields) || fields.length === 0) return undefined;
    const normalized = fields
        .filter(field => field && typeof field === 'object')
        .map(field => ({
            label: typeof field.label === 'string' ? field.label : (typeof field.Label === 'string' ? field.Label : ''),
            value: typeof field.value === 'string' ? field.value : (typeof field.Value === 'string' ? field.Value : ''),
        }))
        .filter(field => field.label && field.value)
        .filter(field => showDetailEntry || !isTokenFieldLabel(field.label));
    return normalized.length > 0 ? normalized : undefined;
}

function normalizeStringArray(values: unknown): string[] | undefined {
    if (!Array.isArray(values)) return undefined;
    const normalized = values
        .filter((value): value is string => typeof value === 'string')
        .map(value => value.trim())
        .filter(Boolean);
    return normalized.length > 0 ? normalized : undefined;
}

function optionalTrimmedString(value: unknown): string | undefined {
    if (typeof value !== 'string') return undefined;
    const trimmed = value.trim();
    return trimmed || undefined;
}

function normalizeConfirmationLabels(raw: unknown): ChatConfirmationLabels | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const labels = raw as Record<string, unknown>;
    const normalized: ChatConfirmationLabels = {
        title: optionalTrimmedString(labels.title ?? labels.Title),
        status: optionalTrimmedString(labels.status ?? labels.Status),
        target_paths: optionalTrimmedString(labels.target_paths ?? labels.TargetPaths),
        planned_actions: optionalTrimmedString(labels.planned_actions ?? labels.PlannedActions),
        risk_flags: optionalTrimmedString(labels.risk_flags ?? labels.RiskFlags),
        revision_hints: optionalTrimmedString(labels.revision_hints ?? labels.RevisionHints),
    };
    return Object.values(normalized).some(Boolean) ? normalized : undefined;
}

function normalizeConfirmation(raw: AIAssistantResponseConfirmation | null | undefined): ChatConfirmation | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const id = typeof raw.id === 'string' ? raw.id.trim() : (typeof raw.ID === 'string' ? raw.ID.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    if (!summary) return undefined;
    const taskType = typeof raw.task_type === 'string' ? raw.task_type.trim() : (typeof raw.TaskType === 'string' ? raw.TaskType.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    const labels = normalizeConfirmationLabels(raw.labels ?? raw.Labels);
    return {
        id,
        summary,
        taskType: taskType || undefined,
        targetPaths: normalizeStringArray(raw.target_paths ?? raw.TargetPaths),
        plannedActions: normalizeStringArray(raw.planned_actions ?? raw.PlannedActions),
        riskFlags: normalizeStringArray(raw.risk_flags ?? raw.RiskFlags),
        revisionHints: normalizeStringArray(raw.revision_hints ?? raw.RevisionHints),
        status: status || undefined,
        labels,
    };
}

function normalizeUnfinishedSlot(raw: AIAssistantResponseUnfinishedSlot | null | undefined): ChatUnfinishedSlot | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const slotID = typeof raw.slot_id === 'string' ? raw.slot_id.trim() : (typeof raw.SlotID === 'string' ? raw.SlotID.trim() : '');
    const title = typeof raw.title === 'string' ? raw.title.trim() : (typeof raw.Title === 'string' ? raw.Title.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    const projectPath = typeof raw.project_path === 'string' ? raw.project_path.trim() : (typeof raw.ProjectPath === 'string' ? raw.ProjectPath.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    const actions = normalizeActions(raw.actions ?? raw.Actions);
    if (!slotID && !title && !summary) return undefined;
    return {
        slotID: slotID || undefined,
        title: title || undefined,
        summary: summary || undefined,
        projectPath: projectPath || undefined,
        status: status || undefined,
        actions,
    };
}

function normalizeRecoverableSession(raw: AIAssistantResponseRecoverableSession | null | undefined): ChatRecoverableSession | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const sessionID = typeof raw.session_id === 'string' ? raw.session_id.trim() : (typeof raw.SessionID === 'string' ? raw.SessionID.trim() : '');
    const tool = typeof raw.tool === 'string' ? raw.tool.trim() : (typeof raw.Tool === 'string' ? raw.Tool.trim() : '');
    const title = typeof raw.title === 'string' ? raw.title.trim() : (typeof raw.Title === 'string' ? raw.Title.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    const projectPath = typeof raw.project_path === 'string' ? raw.project_path.trim() : (typeof raw.ProjectPath === 'string' ? raw.ProjectPath.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    const exitReason = typeof raw.exit_reason === 'string' ? raw.exit_reason.trim() : (typeof raw.ExitReason === 'string' ? raw.ExitReason.trim() : '');
    const resumeSessionID = typeof raw.resume_session_id === 'string' ? raw.resume_session_id.trim() : (typeof raw.ResumeSessionID === 'string' ? raw.ResumeSessionID.trim() : '');
    const resumeCount = typeof raw.resume_count === 'number' ? raw.resume_count : (typeof raw.ResumeCount === 'number' ? raw.ResumeCount : undefined);
    const lastProgress = typeof raw.last_progress === 'string' ? raw.last_progress.trim() : (typeof raw.LastProgress === 'string' ? raw.LastProgress.trim() : '');
    const actions = normalizeActions(raw.actions ?? raw.Actions);
    if (!sessionID && !title && !summary && !lastProgress) return undefined;
    return {
        sessionID: sessionID || undefined,
        tool: tool || undefined,
        title: title || undefined,
        summary: summary || undefined,
        projectPath: projectPath || undefined,
        status: status || undefined,
        exitReason: exitReason || undefined,
        resumeSessionID: resumeSessionID || undefined,
        resumeCount,
        lastProgress: lastProgress || undefined,
        actions,
    };
}

function normalizeSendResponse(response: AIAssistantSendResult | null | undefined, showDetailEntry = false): AIAssistantSendResult {
    const raw = response || {};
    const normalizedFields = normalizeResponseFields(raw.fields ?? raw.Fields, showDetailEntry);
    const counterFields = tokenUsageCounterFields(raw, showDetailEntry, normalizedFields);
    return {
        ...raw,
        text: typeof raw.text === 'string' ? raw.text : (typeof raw.Text === 'string' ? raw.Text : ''),
        reasoning: typeof raw.reasoning === 'string' ? raw.reasoning : (typeof raw.Reasoning === 'string' ? raw.Reasoning : ''),
        error: typeof raw.error === 'string' ? raw.error : (typeof raw.Error === 'string' ? raw.Error : ''),
        fields: mergeResponseFields(normalizedFields, counterFields),
        actions: raw.actions ?? raw.Actions,
        confirmation: normalizeConfirmation(raw.confirmation ?? raw.Confirmation),
        unfinished_slot: normalizeUnfinishedSlot((raw as any).unfinished_slot ?? (raw as any).UnfinishedSlot ?? (raw as any).unfinished_task ?? (raw as any).UnfinishedTask),
        recoverable_session: normalizeRecoverableSession((raw as any).recoverable_session ?? (raw as any).RecoverableSession),
        local_file_path: typeof raw.local_file_path === 'string' ? raw.local_file_path : (typeof raw.LocalFilePath === 'string' ? raw.LocalFilePath : ''),
        local_file_paths: Array.isArray(raw.local_file_paths) ? raw.local_file_paths : (Array.isArray(raw.LocalFilePaths) ? raw.LocalFilePaths : undefined),
        thumbnail_base64: typeof raw.thumbnail_base64 === 'string' ? raw.thumbnail_base64 : (typeof raw.ThumbnailBase64 === 'string' ? raw.ThumbnailBase64 : ''),
        image_key: typeof raw.image_key === 'string' ? raw.image_key : (typeof raw.ImageKey === 'string' ? raw.ImageKey : ''),
        request_id: typeof raw.request_id === 'string' ? raw.request_id : (typeof raw.RequestID === 'string' ? raw.RequestID : ''),
        session_key: typeof raw.session_key === 'string' ? raw.session_key : (typeof raw.SessionKey === 'string' ? raw.SessionKey : ''),
        response_source: typeof raw.response_source === 'string' ? raw.response_source : (typeof raw.ResponseSource === 'string' ? raw.ResponseSource : ''),
        trace_status: typeof raw.trace_status === 'string' ? raw.trace_status : (typeof raw.TraceStatus === 'string' ? raw.TraceStatus : ''),
        trace_summary: typeof raw.trace_summary === 'string' ? raw.trace_summary : (typeof raw.TraceSummary === 'string' ? raw.TraceSummary : ''),
        trace_event_count: typeof raw.trace_event_count === 'number' ? raw.trace_event_count : (typeof raw.TraceEventCount === 'number' ? raw.TraceEventCount : undefined),
        evidence_count: typeof raw.evidence_count === 'number' ? raw.evidence_count : (typeof raw.EvidenceCount === 'number' ? raw.EvidenceCount : undefined),
        deferred: typeof raw.deferred === 'boolean' ? raw.deferred : (typeof raw.Deferred === 'boolean' ? raw.Deferred : false),
        keep_panel: typeof raw.keep_panel === 'boolean' ? raw.keep_panel : (typeof raw.KeepPanel === 'boolean' ? raw.KeepPanel : false),
        clear_ui: typeof raw.clear_ui === 'boolean' ? raw.clear_ui : (typeof raw.ClearUI === 'boolean' ? raw.ClearUI : false),
        confirmed_resume: typeof raw.confirmed_resume === 'boolean' ? raw.confirmed_resume : (typeof raw.ConfirmedResume === 'boolean' ? raw.ConfirmedResume : false),
        trial_reflect_summary: typeof raw.trial_reflect_summary === 'string' ? raw.trial_reflect_summary : (typeof raw.TrialReflectSummary === 'string' ? raw.TrialReflectSummary : ''),
        trial_reflect_status: typeof raw.trial_reflect_status === 'string' ? raw.trial_reflect_status : (typeof raw.TrialReflectStatus === 'string' ? raw.TrialReflectStatus : ''),
        trial_reflect_failures: typeof raw.trial_reflect_failures === 'number' ? raw.trial_reflect_failures : (typeof raw.TrialReflectFailures === 'number' ? raw.TrialReflectFailures : undefined),
        job_id: typeof raw.job_id === 'string' ? raw.job_id : (typeof raw.JobID === 'string' ? raw.JobID : ''),
        run_id: typeof raw.run_id === 'string' ? raw.run_id : (typeof raw.RunID === 'string' ? raw.RunID : ''),
    };
}

function formatRecoveryStatus(status: string): string {
    const normalized = status.trim().toLowerCase();
    switch (normalized) {
        case 'recovered_success':
            return 'Recovered after retry';
        case 'partial_success':
            return 'Partial success';
        case 'success':
            return 'Success';
        case 'failed':
            return 'Failed';
        default:
            return status.trim();
    }
}

function mergeResponseFields(responseFields: any, traceFields: Array<{ label: string; value: string }>): Array<{ label: string; value: string }> | undefined {
    const baseFields = Array.isArray(responseFields) ? responseFields : [];
    const merged = [...baseFields, ...traceFields];
    return merged.length > 0 ? merged : undefined;
}

function buildTraceDetailCommand(runID: string): string {
    return `__view_trace__ ${runID}`;
}

function buildTraceDetailFields(view: AIAssistantTraceView, runID: string): Array<{ label: string; value: string }> {
    const fields: Array<{ label: string; value: string }> = [
        { label: 'Run ID', value: runID },
    ];
    const jobID = typeof view.job_id === 'string' ? view.job_id.trim() : '';
    const status = typeof view.status === 'string' ? view.status.trim() : '';
    const trialReflect = view.trial_reflect_summary;
    if (trialReflect?.final_outcome) {
        fields.push({ label: 'Recovery', value: formatRecoveryStatus(trialReflect.final_outcome) });
    }
    if (typeof trialReflect?.failure_count === 'number' && trialReflect.failure_count > 0) {
        fields.push({ label: 'Failures', value: String(trialReflect.failure_count) });
    }
    if (jobID) {
        fields.push({ label: 'Job ID', value: jobID });
    }
    if (typeof view.event_count === 'number' && view.event_count > 0) {
        fields.push({ label: 'Trace events', value: String(view.event_count) });
    }
    if (typeof view.evidence_count === 'number' && view.evidence_count > 0) {
        fields.push({ label: 'Evidence', value: String(view.evidence_count) });
    }
    if (status) {
        fields.push({ label: 'Status', value: status });
    }
    return fields;
}

function buildTraceDetailMessage(view: AIAssistantTraceView, runID: string): string {
    const lines: string[] = [`Trace details for ${runID}`];
    if (typeof view.summary === 'string' && view.summary.trim()) {
        lines.push(`Summary: ${view.summary.trim()}`);
    }
    const trialReflect = view.trial_reflect_summary;
    if (typeof trialReflect?.final_outcome === 'string' && trialReflect.final_outcome.trim()) {
        lines.push(`Recovery status: ${formatRecoveryStatus(trialReflect.final_outcome)}`);
    }
    if (typeof trialReflect?.strategy_note === 'string' && trialReflect.strategy_note.trim()) {
        lines.push(`Trial-reflect: ${trialReflect.strategy_note.trim()}`);
    }
    if (typeof trialReflect?.final_outcome === 'string' && trialReflect.final_outcome.trim()) {
        lines.push(`Recovery: ${trialReflect.final_outcome.trim()}`);
    }
    if (Array.isArray(trialReflect?.failure_categories) && trialReflect.failure_categories.length > 0) {
        lines.push(`Failure categories: ${trialReflect.failure_categories.join(', ')}`);
    }
    if (typeof view.event_count === 'number' && view.event_count > 0) {
        lines.push(`Events: ${view.event_count}`);
    }
    if (typeof view.evidence_count === 'number' && view.evidence_count > 0) {
        lines.push(`Evidence: ${view.evidence_count}`);
    }
    const eventKinds = Array.isArray(view.events)
        ? view.events.map(event => String(event?.kind || '').trim()).filter(Boolean).slice(0, 4)
        : [];
    if (eventKinds.length > 0) {
        lines.push(`Event kinds: ${eventKinds.join(', ')}`);
    }
    const evidenceKinds = Array.isArray(view.evidence)
        ? view.evidence
            .map(item => {
                const source = String(item?.source_kind || '').trim();
                const category = String(item?.category || '').trim();
                return [source, category].filter(Boolean).join('/');
            })
            .filter(Boolean)
            .slice(0, 4)
        : [];
    if (evidenceKinds.length > 0) {
        lines.push(`Evidence kinds: ${evidenceKinds.join(', ')}`);
    }
    return lines.join('\n');
}

function buildTraceDetailAction(response: any, showTraceEntry: boolean): ChatAction[] | undefined {
    if (!showTraceEntry) return undefined;
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    if (!runID) return undefined;
    return [{ label: 'View trace', command: buildTraceDetailCommand(runID), style: 'default' }];
}

const shortChitChatEdgePunctuationPattern = /^[\s"'`()[\]{}<>.,!?;:，。！？；：、-]+|[\s"'`()[\]{}<>.,!?;:，。！？；：、-]+$/g;
const shortChitChatChineseIdlePattern = /^(没事|没有|不用|无)$/;
const shortChitChatChineseThanksPattern = /^(谢谢|谢了|多谢)$/;
const shortChitChatChineseGreetingPattern = /^(你好|您好|hello|hi)$/;

function normalizeShortChitChatToken(text: string): string {
    let cleaned = text.trim().toLowerCase();
    if (!cleaned) return '';
    while (true) {
        const next = cleaned.replace(shortChitChatEdgePunctuationPattern, '').trim();
        if (next === cleaned) break;
        cleaned = next;
    }
    cleaned = cleaned.split(/\s+/).filter(Boolean).join(' ');
    if (!cleaned) return '';
    if (shortChitChatChineseIdlePattern.test(cleaned)) return '没事';
    if (shortChitChatChineseThanksPattern.test(cleaned)) return '谢谢';
    if (shortChitChatChineseGreetingPattern.test(cleaned)) return '你好';
    return new Set([
        '没事',
        'nothing',
        'none',
        'hi',
        'hello',
        'ok',
        'okay',
        'thanks',
        'thank you',
        '谢谢',
    ]).has(cleaned) ? cleaned : '';
}

function selectVisibleEmptyResultSummary(response: any): string {
    const candidates = [response?.trace_summary, response?.trial_reflect_summary]
        .filter((value): value is string => typeof value === 'string')
        .map(value => value.trim())
        .filter(Boolean);
    return candidates.find(isVisibleEmptyResultSummary) || '';
}

function isVisibleEmptyResultSummary(summary: string): boolean {
    const trimmed = summary.trim();
    if (!trimmed) return false;

    const normalized = trimmed.toLowerCase();
    const normalizedEcho = normalizeShortChitChatToken(normalized.replace(/^(summary|result|trace summary|结果|摘要)\s*[:：-]?\s*/i, '').trim());
    if (normalizedEcho) {
        return false;
    }

    const promptLikeMarkers = [
        '当前工作目录',
        'primary working directory',
        'current working directory',
        'project directory',
        'default directory',
        'continue the conversation',
        'resume directly',
        'user:',
        'assistant:',
        'task:',
        '任务',
        '请帮我',
        '帮我',
        '请实现',
        '请修复',
        '请重构',
        'you are',
    ];
    if (promptLikeMarkers.some(marker => normalized.includes(marker))) {
        return false;
    }

    const executionSignals = [
        'failed',
        'failure',
        'error',
        'stopped',
        'timeout',
        'cancel',
        'killed',
        'retry',
        'recovered',
        'generated',
        'created',
        'saved',
        'wrote',
        'written',
        'exported',
        'uploaded',
        'downloaded',
        'prepared',
        'delivered',
        'found',
        'produced',
        '执行',
        '失败',
        '错误',
        '超时',
        '取消',
        '停止',
        '重试',
        '恢复',
        '生成',
        '创建',
        '保存',
        '写入',
        '导出',
        '上传',
        '下载',
        '准备',
        '找到',
        '文件',
    ];
    return executionSignals.some(signal => normalized.includes(signal));
}

function buildEmptyTerminalFallback(response: any): string {
    const traceStatus = typeof response?.trace_status === 'string' ? response.trace_status.trim().toLowerCase() : '';
    const fallback = selectVisibleEmptyResultSummary(response);
    if (traceStatus === 'failed' || traceStatus === 'timeout' || traceStatus === 'cancelled' || traceStatus === 'stopped') {
        return fallback ? `任务未完成可交付结果。${fallback}` : '任务未完成可交付结果。可查看 Trace 了解失败位置。';
    }
    return fallback ? `任务已结束，但没有生成可展示的结果。${fallback}` : '任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。';
}

function isFailedTerminalTraceStatus(status: unknown): boolean {
    const normalized = typeof status === 'string' ? status.trim().toLowerCase() : '';
    return normalized === 'failed' || normalized === 'timeout' || normalized === 'cancelled' || normalized === 'stopped';
}

function hasVisibleTerminalPayload(response: any): boolean {
    const text = typeof response?.text === 'string' ? response.text.trim() : '';
    const { localFilePaths, thumbnailBase64, imageKey } = responseArtifactPayload(response);
    return !!text || !!thumbnailBase64 || !!imageKey || !!localFilePaths?.length;
}

function hasRenderableTerminalPayload(response: any): boolean {
    if (!response || typeof response !== 'object') return false;
    if (typeof response.error === 'string' && response.error.trim()) return true;
    if (hasVisibleTerminalPayload(response)) return true;
    if (hasStructuredResponsePayload(response)) return true;
    const fields = Array.isArray(response.fields) ? response.fields : [];
    if (fields.length > 0) return true;
    return isFailedTerminalTraceStatus(response.trace_status);
}

/** Sources whose response.text is semantically unrelated to streamed content. */
const SPECIAL_RESPONSE_SOURCES: ReadonlySet<string> = new Set([
    'ask_user', 'cancel', 'file_delivery', 'screenshot',
]);

function canonicalResponseSource(source: unknown): string {
    if (typeof source !== 'string') return '';
    const trimmed = source.trim();
    if (!trimmed) return '';
    const token = trimmed.toLowerCase().replace(/[_\-\s]/g, '');
    switch (token) {
        case 'filedelivery':
            return 'file_delivery';
        case 'screenshot':
        case 'screenshotcapture':
            return 'screenshot';
        case 'askuser':
            return 'ask_user';
        case 'cancel':
        case 'cancelled':
        case 'canceled':
            return 'cancel';
        case 'agentloop':
            return 'agent_loop';
        case 'agentviewsubmit':
            return 'agent_view_submit';
        case 'agentviewdismiss':
            return 'agent_view_dismiss';
        default:
            return trimmed.toLowerCase();
    }
}

function resolveResponseSource(response: any): string {
    const explicitSource = canonicalResponseSource(response?.response_source || response?.ResponseSource);
    const { localFilePaths, thumbnailBase64, imageKey } = responseArtifactPayload(response);
    if (!explicitSource || explicitSource === 'agent_loop') {
        if (thumbnailBase64 || imageKey) return 'screenshot';
        if (localFilePaths?.length) return 'file_delivery';
    }
    if (explicitSource) return explicitSource;
    return '';
}

type RolePrefixDiagnosticMeta = Record<string, string | number | boolean | null | undefined>;
type RolePrefixDiagnosticInfo = {
    hasRolePrefix: boolean;
    rolePrefixKind?: string;
    rolePrefixIndex?: number;
    rolePrefixAtStart?: boolean;
};

function hasRolePrefixLeakCandidate(text: string): boolean {
    return !!findRolePrefixForDisplay(text);
}

function rolePrefixDiagnosticInfo(text: string): RolePrefixDiagnosticInfo {
    const match = findRolePrefixForDisplay(text);
    if (!match) {
        return { hasRolePrefix: false };
    }
    return {
        hasRolePrefix: true,
        rolePrefixKind: match.kind,
        rolePrefixIndex: match.index,
        rolePrefixAtStart: match.atStart,
    };
}

function logRolePrefixDiagnostic(stage: string, before: string, after?: string, meta: RolePrefixDiagnosticMeta = {}): void {
    const beforeText = before || '';
    const afterText = after ?? beforeText;
    const stripped = after !== undefined && afterText !== beforeText;
    if (!stripped && !hasRolePrefixLeakCandidate(beforeText) && !hasRolePrefixLeakCandidate(afterText)) {
        return;
    }
    const beforeInfo = rolePrefixDiagnosticInfo(beforeText);
    const afterInfo = rolePrefixDiagnosticInfo(afterText);
    const payload: RolePrefixDiagnosticMeta & {
        stage: string;
        stripped: boolean;
        beforeLen: number;
        afterLen: number;
        beforeHasRolePrefix: boolean;
        beforeRolePrefixKind?: string;
        beforeRolePrefixIndex?: number;
        beforeRolePrefixAtStart?: boolean;
        afterHasRolePrefix: boolean;
        afterRolePrefixKind?: string;
        afterRolePrefixIndex?: number;
        afterRolePrefixAtStart?: boolean;
    } = {
        stage,
        stripped,
        beforeLen: beforeText.length,
        afterLen: afterText.length,
        beforeHasRolePrefix: beforeInfo.hasRolePrefix,
        afterHasRolePrefix: afterInfo.hasRolePrefix,
        ...meta,
    };
    if (beforeInfo.hasRolePrefix) {
        payload.beforeRolePrefixKind = beforeInfo.rolePrefixKind;
        payload.beforeRolePrefixIndex = beforeInfo.rolePrefixIndex;
        payload.beforeRolePrefixAtStart = beforeInfo.rolePrefixAtStart;
    }
    if (afterInfo.hasRolePrefix) {
        payload.afterRolePrefixKind = afterInfo.rolePrefixKind;
        payload.afterRolePrefixIndex = afterInfo.rolePrefixIndex;
        payload.afterRolePrefixAtStart = afterInfo.rolePrefixAtStart;
    }
    // Diagnostic only: catches display-layer role prefix leaks without logging
    // assistant/user content.
    console.warn('[ai-role-prefix]', payload);
    const logFrontendDiagnostic = typeof window !== 'undefined'
        ? (window as any).go?.main?.App?.LogFrontendDiagnostic
        : undefined;
    if (typeof logFrontendDiagnostic === 'function') {
        try {
            void Promise.resolve(logFrontendDiagnostic({
                tag: 'ai-role-prefix',
                ...payload,
            })).catch(() => { });
        } catch {
            // Diagnostics must never affect chat rendering.
        }
    }
}

/**
 * Strip hallucinated role prefixes from LLM output text.
 * Frontend safety net - catches anything the backend streaming filter missed.
 *
 * Case 1: Prefix at the start of text -> strip prefix, keep content after it.
 * Case 2: Prefix in the middle -> truncate at the role-prefixed tail.
 *         This mirrors the backend post-processor and the stream filter:
 *         once valid content has been emitted, a later Browser:/Tool: line is
 *         treated as hallucinated continuation output, not user-facing text.
 *
 * Code blocks (``` fenced) are excluded to avoid false positives.
 */
function stripRolePrefixFrontend(text: string): string {
    return stripRolePrefixForDisplay(text);
}

export function resolveFinalRoundContent(message: ChatMessage, response: any): string {
    const rawFinalText = typeof response?.text === 'string' ? response.text : '';
    const rawStreamedContent = message.content || '';
    const finalText = stripRolePrefixFrontend(rawFinalText);
    const streamedContent = stripRolePrefixFrontend(rawStreamedContent);
    const responseSource = resolveResponseSource(response);
    logRolePrefixDiagnostic('final-response:text', rawFinalText, finalText, {
        messageId: message.id,
        requestId: message.requestId || response?.request_id,
        responseSource,
    });
    logRolePrefixDiagnostic('final-response:streamed', rawStreamedContent, streamedContent, {
        messageId: message.id,
        requestId: message.requestId || response?.request_id,
        responseSource,
    });

    // --- Layer 1: Source check ---
    // Special handler paths produce text semantically unrelated to streamed
    // content (e.g. ask_user structured questions, cancel messages, file
    // delivery notices, screenshot captions). Always use finalText for these.
    if (SPECIAL_RESPONSE_SOURCES.has(responseSource)) {
        return finalText;
    }
    const { localFilePaths } = responseArtifactPayload(response);
    if (!finalText && localFilePaths?.length) {
        return '';
    }
    if (!finalText && !streamedContent && hasStructuredResponsePayload(response)) {
        return '';
    }

    // --- Layer 2: Length comparison ---
    // When streamed content is significantly longer than finalText (>= 2x),
    // the response text is just the last iteration's fragment from a
    // multi-round agent loop. Preserve the complete accumulated output.
    const finalTextLen = finalText.trim().length;
    if (streamedContent && finalText && finalTextLen > 0
        && streamedContent.length >= finalTextLen * 2
        && (!responseSource || responseSource === 'agent_loop')) {
        // Apply role prefix stripping - streamedContent bypasses backend
        // post-processing (stripRolePrefixHallucination) because it comes
        // from the streaming token path, not from resp.Text.
        return stripRolePrefixFrontend(streamedContent);
    }

    // --- Layer 3: endsWith fallback ---
    // Original improvement #19: if streamed content ends with the response
    // text, the response text is the final iteration's tail - keep the full
    // streamed content.
    if (streamedContent && finalText && streamedContent.length > finalText.length) {
        if (streamedContent.endsWith(finalText)) {
            return stripRolePrefixFrontend(streamedContent);
        }
    }

    // --- Subsequent fallbacks (unchanged) ---
    if (finalText) {
        return finalText;
    }
    if (hasVisibleTerminalPayload(response)) {
        return stripRolePrefixFrontend(streamedContent);
    }
    if (isFailedTerminalTraceStatus(response?.trace_status)) {
        return buildEmptyTerminalFallback(response);
    }
    if (streamedContent) {
        return stripRolePrefixFrontend(streamedContent);
    }
    if (typeof response?.reasoning === 'string' && response.reasoning.trim()) {
        return '';
    }
    return buildEmptyTerminalFallback(response);
}

function hasStructuredResponsePayload(response: any): boolean {
    if (!response || typeof response !== 'object') return false;
    if (response.confirmation || response.unfinished_slot || response.unfinished_task || response.recoverable_session) return true;
    if (response.Confirmation || response.UnfinishedSlot || response.UnfinishedTask || response.RecoverableSession) return true;
    if (Array.isArray(response.actions) && response.actions.length > 0) return true;
    if (Array.isArray(response.Actions) && response.Actions.length > 0) return true;
    return false;
}

function resolveFinalRoundReasoning(message: ChatMessage, response: any): string | undefined {
    const rawFinalReasoning = typeof response?.reasoning === 'string' ? response.reasoning : '';
    const finalReasoning = stripRolePrefixReasoning(rawFinalReasoning);
    const streamedReasoning = stripRolePrefixReasoning(message.reasoning || '');
    if (streamedReasoning && finalReasoning) {
        return mergeReasoningText(streamedReasoning, finalReasoning) || undefined;
    }
    return (streamedReasoning || finalReasoning || '').trim() || undefined;
}

function mergeReasoningText(streamedReasoning: string, finalReasoning: string): string {
    const streamed = streamedReasoning.trim();
    const final = finalReasoning.trim();
    if (!streamed) return final;
    if (!final) return streamed;
    if (streamed === final) return streamed;
    if (streamed.length >= MIN_REASONING_DEDUP_OVERLAP && final.startsWith(streamed)) return final;
    if (final.length >= MIN_REASONING_DEDUP_OVERLAP && streamed.endsWith(final)) return streamed;
    const maxOverlap = Math.min(streamed.length, final.length);
    if (maxOverlap < MIN_REASONING_DEDUP_OVERLAP) {
        return `${streamed}${streamed.endsWith('\n') || final.startsWith('\n') ? '' : '\n'}${final}`.trim();
    }
    for (let size = maxOverlap; size >= MIN_REASONING_DEDUP_OVERLAP; size--) {
        if (streamed.endsWith(final.slice(0, size))) {
            return `${streamed}${final.slice(size)}`.trim();
        }
    }
    return `${streamed}${streamed.endsWith('\n') || final.startsWith('\n') ? '' : '\n'}${final}`.trim();
}

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, response: any, preferences: AIAssistantPreferences): ChatMessage[] {
    const finalizeMessage = (message: ChatMessage): ChatMessage | null => {
        const nextContent = resolveFinalRoundContent(message, response);
        const nextReasoning = resolveFinalRoundReasoning(message, response);
        const nextFields = mergeResponseFields(response.fields, normalizeTraceFields(response, preferences.showTraceEntry));
        const responseActions = normalizeActions(response.actions) || [];
        const traceActions = buildTraceDetailAction(response, preferences.showTraceEntry) || [];
        const nextActions = [...responseActions, ...traceActions];
        const {
            localFilePath: nextLocalFilePath,
            localFilePaths: nextLocalFilePaths,
            thumbnailBase64: nextThumbnailBase64,
            imageKey: nextImageKey,
        } = responseArtifactPayload(response);
        const nextUnfinishedSlot = (response as any).unfinished_slot;
        const nextRecoverableSession = (response as any).recoverable_session;
        if (!nextContent && !nextReasoning && !nextFields?.length && !nextActions?.length && !nextUnfinishedSlot && !nextRecoverableSession && !nextThumbnailBase64 && !nextImageKey && !nextLocalFilePaths?.length) {
            return null;
        }
        return {
            ...message,
            content: nextContent,
            reasoning: nextReasoning,
            fields: nextFields,
            actions: nextActions.length > 0 ? nextActions : undefined,
            confirmation: response.confirmation,
            unfinishedSlot: nextUnfinishedSlot,
            recoverableSession: nextRecoverableSession,
            localFilePath: nextLocalFilePath || undefined,
            localFilePaths: nextLocalFilePaths,
            thumbnailBase64: nextThumbnailBase64,
            imageKey: nextImageKey || undefined,
        };
    };
    return updateRoundMessage(messages, assistantMessageId, requestId, finalizeMessage);
}

function removeActionCommandFromMessage(message: ChatMessage, command: string): ChatMessage {
    let changed = false;
    const filterActions = (actions: ChatAction[] | undefined): ChatAction[] | undefined => {
        if (!actions?.length) return actions;
        const next = actions.filter(action => action.command !== command);
        if (next.length === actions.length) return actions;
        changed = true;
        return next.length > 0 ? next : undefined;
    };
    const nextActions = filterActions(message.actions);
    const nextUnfinishedActions = filterActions(message.unfinishedSlot?.actions);
    const nextRecoverableActions = filterActions(message.recoverableSession?.actions);
    if (!changed) return message;
    return {
        ...message,
        actions: nextActions,
        unfinishedSlot: message.unfinishedSlot ? { ...message.unfinishedSlot, actions: nextUnfinishedActions } : message.unfinishedSlot,
        recoverableSession: message.recoverableSession ? { ...message.recoverableSession, actions: nextRecoverableActions } : message.recoverableSession,
    };
}

function removeActionCommandFromMessages(messages: ChatMessage[], command: string): ChatMessage[] {
    return messages.map(message => removeActionCommandFromMessage(message, command));
}

/**
 * Remove ALL actions from the message that contains the given command.
 * Used for one-shot, mutually exclusive button groups (ask_user choices,
 * workflow choice panels) where clicking any button should disable the
 * entire group — not just the clicked button.
 */
function disableActionsForCommand(messages: ChatMessage[], command: string): ChatMessage[] {
    return messages.map(message => {
        if (!message.actions?.some(a => a.command === command)) return message;
        return { ...message, actions: undefined };
    });
}

function replaceRoundWithError(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, errorText: string, preserveExistingContent = false): ChatMessage[] {
    if (!assistantMessageId && !requestId) {
        return [...messages, {
            id: nextId(),
            role: 'error',
            content: errorText,
            timestamp: Date.now(),
        }];
    }
    const index = findLastIndex(messages, msg =>
        (assistantMessageId && msg.id === assistantMessageId)
        || (!!requestId && msg.role === 'assistant' && msg.requestId === requestId)
    );
    if (preserveExistingContent && index >= 0 && messages[index].content.trim()) {
        return [...messages, {
            id: nextId(),
            role: 'error',
            content: errorText,
            timestamp: Date.now(),
        }];
    }
    const replaceWithError = (message: ChatMessage): ChatMessage => ({
        ...message,
        role: 'error',
        content: errorText,
        timestamp: Date.now(),
    });
    const nextMessages = updateRoundMessage(messages, assistantMessageId, requestId, replaceWithError);
    return hasRoundMessage(messages, assistantMessageId, requestId)
        ? nextMessages
        : [...messages, {
            id: nextId(),
            role: 'error',
            content: errorText,
            timestamp: Date.now(),
        }];
}

function isTimeoutErrorText(errorText: unknown): boolean {
    const normalized = String(errorText || '').toLowerCase();
    return normalized.includes('timeout')
        || normalized.includes('timed out')
        || normalized.includes('time out')
        || normalized.includes('deadline exceeded')
        || normalized.includes('\u8bf7\u6c42\u8d85\u65f6')
        || normalized.includes('\u8d85\u65f6');
}

export const CANCELED_BY_USER_LINE = "任务已经应用户要求取消";

export function markRoundCancelled(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null): ChatMessage[] {
    const markCancelled = (message: ChatMessage): ChatMessage => {
        const content = message.content || "";
        if (content.includes(CANCELED_BY_USER_LINE)) return message;
        return {
            ...message,
            content: content.trimEnd()
                ? `${content.trimEnd()}\n${CANCELED_BY_USER_LINE}`
                : CANCELED_BY_USER_LINE,
            timestamp: Date.now(),
        };
    };
    return updateRoundMessage(messages, assistantMessageId, requestId, markCancelled);
}

function resolveSendResult(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, response: any, preferences: AIAssistantPreferences, errorText?: string): ChatMessage[] {
    return errorText
        ? replaceRoundWithError(messages, assistantMessageId, requestId, errorText, isTimeoutErrorText(errorText))
        : response?.error
            ? replaceRoundWithError(messages, assistantMessageId, requestId, response.error, isTimeoutErrorText(response.error))
            : finalizeRoundMessage(messages, assistantMessageId, requestId, response, preferences);
}

function normalizeActionStyle(style: unknown): ChatActionStyle {
    return style === 'primary' || style === 'secondary' || style === 'danger' ? style : 'default';
}

function normalizeInitStatus(status: unknown): AIAssistantInitStatus {
    return status === 'loading' || status === 'warming' || status === 'ready' || status === 'degraded' ? status : 'connecting';
}

function normalizeActions(actions: any): ChatAction[] | undefined {
    if (!Array.isArray(actions) || actions.length === 0) return undefined;
    const normalized = actions
        .map(action => {
            if (!action || typeof action !== 'object') return null;
            const raw = action as Record<string, unknown>;
            const label = optionalTrimmedString(raw.label ?? raw.Label);
            const command = optionalTrimmedString(raw.command ?? raw.Command);
            if (!label || !command) return null;
            return {
                label,
                command,
                style: normalizeActionStyle(raw.style ?? raw.Style),
            };
        })
        .filter((action): action is ChatAction => action !== null);
    return normalized.length > 0 ? normalized : undefined;
}

function normalizeNewsCategory(category: unknown): NewsCategory {
    return category === 'notice' || category === 'update' || category === 'tip' || category === 'alert' ? category : '';
}

function normalizeStreamEvent(raw: unknown): AIAssistantStreamEvent {
    if (raw && typeof raw === 'object') {
        const event = raw as Record<string, unknown>;
        const stringField = (...keys: string[]) => {
            for (const key of keys) {
                const value = event[key];
                if (typeof value === 'string') return value;
            }
            return '';
        };
        return {
            request_id: stringField('request_id', 'requestId', 'RequestID'),
            text: stringField('text', 'Text'),
            session_key: stringField('session_key', 'sessionKey', 'SessionKey'),
        };
    }
    if (typeof raw === 'string') {
        const trimmed = raw.trim();
        if (!trimmed) return { request_id: '', text: '', session_key: '' };
        if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
            try {
                return normalizeStreamEvent(JSON.parse(trimmed));
            } catch {
                return { request_id: '', text: raw, session_key: '' };
            }
        }
        return { request_id: '', text: raw, session_key: '' };
    }
    return { request_id: '', text: '', session_key: '' };
}

/** Returns true if the event belongs to the local (main) session.
 * Local session events have session_key="" or "desktop-user" (no project path suffix). */
function isLocalSessionEvent(event: AIAssistantStreamEvent): boolean {
    const key = event.session_key || '';
    if (!key) return true;
    if (key === 'desktop-user') return true;
    return false;
}

/** Returns true if the event belongs to the given active session key.
 * Used when the hook is serving a project tab; accepts events matching that session. */
function isMatchingSessionEvent(event: AIAssistantStreamEvent, activeSessionKey: string): boolean {
    const eventKey = event.session_key || '';
    if (!activeSessionKey || activeSessionKey === 'desktop-user') {
        return isLocalSessionEvent(event);
    }
    return eventKey === activeSessionKey;
}

function matchesActiveRequest(round: ActiveRound, event: AIAssistantStreamEvent): boolean {
    if (!round.requestId || round.generation === 0) return false;
    return !event.request_id || event.request_id === round.requestId;
}

function matchesActiveResponseRequest(round: ActiveRound, requestId: string): boolean {
    if (!round.requestId || round.generation === 0 || round.phase === 'idle') return false;
    return !!requestId && requestId === round.requestId;
}

function matchesActiveProgressRequest(round: ActiveRound, event: AIAssistantStreamEvent): boolean {
    if (!round.requestId || round.generation === 0 || round.phase === 'idle') return false;
    return !!event.request_id && event.request_id === round.requestId;
}

function matchesActiveProgressActivity(round: ActiveRound, event: AIAssistantStreamEvent, activeSessionKey: string): boolean {
    if (matchesActiveProgressRequest(round, event)) return true;
    if (event.request_id) return false;
    if (!isHeartbeatProgressText(event.text)) return false;
    return isMatchingSessionEvent(event, activeSessionKey);
}

function isLocalRoundSession(round: ActiveRound | { sessionKey?: string } | null | undefined): boolean {
    const sessionKey = round?.sessionKey || 'desktop-user';
    return sessionKey === 'desktop-user';
}

function isMatchingSessionOrActiveRequest(event: AIAssistantStreamEvent, round: ActiveRound, activeSessionKey: string): boolean {
    if (matchesActiveProgressRequest(round, event)) return true;
    return isMatchingSessionEvent(event, activeSessionKey);
}

function normalizeAgentView(raw: unknown): AgentView | null {
    if (!raw || typeof raw !== 'object') return null;
    const data = raw as Record<string, unknown>;
    const candidate = data.view && typeof data.view === 'object'
        ? data.view as Record<string, unknown>
        : data;
    if (typeof candidate.type !== 'string' || typeof candidate.title !== 'string') {
        return null;
    }
    return candidate as unknown as AgentView;
}

function normalizeAgentViewLifecycle(raw: unknown): AgentViewLifecyclePayload | null {
    if (!raw || typeof raw !== 'object') return null;
    const data = raw as Record<string, unknown>;
    const action = typeof data.action === 'string' ? data.action.trim() : '';
    if (!["open", "update", "submit", "dismiss", "error", "complete"].includes(action)) {
        return null;
    }
    return {
        action: action as AgentViewLifecycleAction,
        view: normalizeAgentView(data) || undefined,
        view_id: typeof data.view_id === 'string' ? data.view_id : undefined,
        seq: typeof data.seq === 'number' ? data.seq : (typeof data.sequence === 'number' ? data.sequence : undefined),
        workflow_id: typeof data.workflow_id === 'string' ? data.workflow_id : undefined,
        workflow_phase: typeof data.workflow_phase === 'string' ? data.workflow_phase : undefined,
        workflow_user_id: typeof data.workflow_user_id === 'string' ? data.workflow_user_id : undefined,
        error: typeof data.error === 'string' ? data.error : undefined,
    };
}

function agentViewFieldValue(view: AgentView | null | undefined, fieldName: string): string {
    if (!view || !('fields' in view) || !Array.isArray((view as any).fields)) return '';
    const field = (view as any).fields.find((item: any) => item && item.name === fieldName);
    const value = field?.value;
    return typeof value === 'string' ? value.trim() : '';
}

function agentViewMatchesLifecycle(current: AgentView | null, event: AgentViewLifecyclePayload): boolean {
    if (!current) return true;
    if (event.view_id && current.id !== event.view_id) return false;
    const currentWorkflowID = agentViewFieldValue(current, '_workflow_id');
    const currentWorkflowPhase = agentViewFieldValue(current, '_workflow_phase');
    const currentWorkflowUserID = agentViewFieldValue(current, '_workflow_user_id');
    const currentViewID = current.id || '';
    const currentIsWorkflowForm = currentViewID.startsWith('workflow:form:') || !!currentWorkflowID || !!currentWorkflowPhase || !!currentWorkflowUserID;
    const workflowID = event.workflow_id?.trim() || '';
    if (currentIsWorkflowForm && currentWorkflowID && !workflowID) return false;
    if (workflowID) {
        if (currentWorkflowID && currentWorkflowID !== workflowID) return false;
    }
    const workflowPhase = event.workflow_phase?.trim() || '';
    if (currentIsWorkflowForm && currentWorkflowPhase && !workflowPhase) return false;
    if (workflowPhase) {
        if (currentWorkflowPhase && currentWorkflowPhase !== workflowPhase) return false;
    }
    const workflowUserID = event.workflow_user_id?.trim() || '';
    if (currentIsWorkflowForm && currentWorkflowUserID && !workflowUserID) return false;
    if (workflowUserID) {
        if (currentWorkflowUserID && currentWorkflowUserID !== workflowUserID) return false;
    }
    return true;
}

function normalizeRuntimeSessionKey(sessionKey?: string): string {
    return normalizeAssistantSessionKey(sessionKey) || 'desktop-user';
}

function isTaskManagedSessionKey(sessionKey: string): boolean {
    return normalizeRuntimeSessionKey(sessionKey).replace(/\\/g, '/').toLowerCase().includes('/.maclaw/data/tasks/');
}

function agentViewSessionKey(view: AgentView | null | undefined, fallbackSessionKey = 'desktop-user'): string {
    return normalizeRuntimeSessionKey(agentViewFieldValue(view, '_workflow_user_id') || fallbackSessionKey);
}

function agentViewLifecycleSessionKey(event: AgentViewLifecyclePayload, fallbackSessionKey = 'desktop-user'): string {
    return normalizeRuntimeSessionKey(event.workflow_user_id || agentViewSessionKey(event.view, fallbackSessionKey));
}

function eventSequenceValue(payload: unknown): number | undefined {
    if (!payload || typeof payload !== 'object') return undefined;
    const data = payload as Record<string, unknown>;
    return typeof data.seq === 'number' ? data.seq : (typeof data.sequence === 'number' ? data.sequence : undefined);
}

function subscribeEvent(eventName: string, handler: (...args: any[]) => void): () => void {
    const unsubscribe = EventsOn(eventName, handler);
    return typeof unsubscribe === 'function' ? unsubscribe : () => EventsOff(eventName);
}

function logAIAssistantDiagnostic(payload: Record<string, unknown>) {
    const logFrontendDiagnostic = typeof window !== 'undefined'
        ? (window as any).go?.main?.App?.LogFrontendDiagnostic
        : undefined;
    if (typeof logFrontendDiagnostic !== 'function') return;
    try {
        void Promise.resolve(logFrontendDiagnostic({ tag: 'ai-assistant', ...payload })).catch(() => {});
    } catch {
        // diagnostics only
    }
}

function resolveSendRequestID(response: AIAssistantSendResult | null | undefined): string {
    return typeof response?.request_id === 'string' ? response.request_id.trim() : '';
}

function createForegroundRequestID(): string {
    return `desktop-ai-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

function resolveBackgroundSessionID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.session_id || result?.sessionID || "").trim();
}

function resolveBackgroundJobID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.job_id || result?.jobID || "").trim();
}

function resolveBackgroundRunID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.run_id || result?.runID || "").trim();
}

function normalizeSessionID(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
}

function normalizeStatusKey(value: unknown): string {
    return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function normalizeLaunchSource(value: unknown): string {
    return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function isTerminalRemoteStatus(status: unknown): boolean {
    const key = normalizeStatusKey(status);
    return key === 'stopped'
        || key === 'finished'
        || key === 'failed'
        || key === 'killed'
        || key === 'exited'
        || key === 'closed'
        || key === 'done'
        || key === 'error'
        || key === 'completed'
        || key === 'terminated';
}

function matchesPendingTaskSession(session: AIAssistantRemoteSessionView, pendingTask: AIAssistantPendingTask | null): boolean {
    if (!pendingTask) return false;
    const sessionID = normalizeSessionID(session?.id);
    const jobID = normalizeSessionID(session?.job_id);
    const runID = normalizeSessionID(session?.run_id);
    if (pendingTask.sessionID && sessionID === pendingTask.sessionID) return true;
    if (pendingTask.jobID && jobID === pendingTask.jobID) return true;
    if (pendingTask.runID && runID === pendingTask.runID) return true;
    return false;
}

async function resolvePendingAITask(requestId: string, response: AIAssistantSendResult | null | undefined): Promise<AIAssistantPendingTask | null> {
    const runID = normalizeSessionID(response?.run_id);
    const jobID = normalizeSessionID(response?.job_id);
    if (!response?.deferred && !runID && !jobID) {
        return null;
    }
    try {
        const sessions = await ListRemoteSessions() as AIAssistantRemoteSessionView[];
        for (const session of Array.isArray(sessions) ? sessions : []) {
            if (normalizeLaunchSource(session?.launch_source) !== 'ai') continue;
            if (isTerminalRemoteStatus(session?.status)) continue;
            const sessionRunID = normalizeSessionID(session?.run_id);
            const sessionJobID = normalizeSessionID(session?.job_id);
            if (runID && sessionRunID && sessionRunID === runID) {
                return {
                    requestId,
                    sessionID: normalizeSessionID(session?.id) || undefined,
                    jobID: sessionJobID || jobID || undefined,
                    runID: sessionRunID || runID || undefined,
                };
            }
            if (jobID && sessionJobID && sessionJobID === jobID) {
                return {
                    requestId,
                    sessionID: normalizeSessionID(session?.id) || undefined,
                    jobID: sessionJobID || jobID || undefined,
                    runID: sessionRunID || runID || undefined,
                };
            }
        }
    } catch {
        return null;
    }
    return null;
}

function buildBackgroundLaunchNotice(result: AIAssistantBackgroundLaunchResult): string {
    const detailLines = [
        "已转到后台运行。",
        "任务会显示在“任务管理”里的后台列表。",
        `session_id: ${result.sessionID}`,
    ];
    if (result.jobID) detailLines.push(`job_id: ${result.jobID}`);
    if (result.runID) detailLines.push(`run_id: ${result.runID}`);
    return detailLines.join("\n");
}

function createSystemMessage(content: string, sessionKey?: string): ChatMessage {
    const ownerSessionKey = normalizeRuntimeSessionKey(sessionKey || getActiveSessionKey() || 'desktop-user');
    return {
        id: nextId(),
        role: 'system',
        content,
        sessionKey: ownerSessionKey,
        timestamp: Date.now(),
    };
}

function createTraceMessage(content: string, fields: Array<{ label: string; value: string }>, sessionKey?: string): ChatMessage {
    const ownerSessionKey = normalizeRuntimeSessionKey(sessionKey || getActiveSessionKey() || 'desktop-user');
    return {
        id: nextId(),
        role: 'system',
        kind: 'trace',
        content,
        fields,
        sessionKey: ownerSessionKey,
        timestamp: Date.now(),
    };
}

function createUserMessage(content: string, sessionKey?: string): ChatMessage {
    const ownerSessionKey = normalizeRuntimeSessionKey(sessionKey || getActiveSessionKey() || 'desktop-user');
    return {
        id: nextId(),
        role: 'user',
        content,
        sessionKey: ownerSessionKey,
        timestamp: Date.now(),
    };
}

function createErrorMessage(content: string, sessionKey?: string): ChatMessage {
    const ownerSessionKey = normalizeRuntimeSessionKey(sessionKey || getActiveSessionKey() || 'desktop-user');
    return {
        id: nextId(),
        role: 'error',
        content,
        sessionKey: ownerSessionKey,
        timestamp: Date.now(),
    };
}

function appendBackgroundLaunchMessages(messages: ChatMessage[], outgoingText: string, result: AIAssistantBackgroundLaunchResult, sessionKey?: string): ChatMessage[] {
    return [
        ...messages,
        createUserMessage(outgoingText, sessionKey),
        createSystemMessage(buildBackgroundLaunchNotice(result), sessionKey),
    ];
}

function createNewsMessage(article: any): ChatMessage {
    const iconByCategory: Record<Exclude<NewsCategory, ''>, string> = { notice: 'INFO', update: 'NEW', tip: 'TIP', alert: 'ALERT' };
    const category = normalizeNewsCategory(article?.category);
    const title = typeof article?.title === 'string' ? article.title : '';
    const body = typeof article?.content === 'string' ? article.content : '';
    const articleId = String(article?.id ?? nextId());
    return {
        id: `news-${articleId}`,
        role: 'system',
        kind: 'news',
        content: body,
        news: {
            articleId,
            category,
            title,
            body,
            icon: category ? iconByCategory[category] : 'INFO',
        },
        timestamp: Date.now(),
    };
}

export function isPinnedNewsMessage(message: ChatMessage): boolean {
    return message.kind === 'news' && !!message.news;
}

function samePinnedNews(left: ChatMessage, right: ChatMessage): boolean {
    if (!isPinnedNewsMessage(left) || !isPinnedNewsMessage(right)) return false;
    const leftNews = left.news;
    const rightNews = right.news;
    if (!leftNews || !rightNews) return false;
    return leftNews.articleId === rightNews.articleId
        && leftNews.category === rightNews.category
        && leftNews.title === rightNews.title
        && leftNews.body === rightNews.body
        && leftNews.icon === rightNews.icon;
}

function isTraditionalSkillConfirmLang(lang: string): boolean {
    const normalized = lang.trim().toLowerCase();
    return normalized.startsWith('zh-hant') || normalized.startsWith('zh-tw') || normalized.startsWith('zh-hk');
}

function criticalConfirmFeedback(lang: string, confirmed: boolean): string {
    const normalized = lang.trim().toLowerCase();
    if (normalized === 'en' || normalized.startsWith('en-')) {
        return confirmed ? '\n\nConfirmed. Installing...' : '\n\nInstallation rejected.';
    }
    if (isTraditionalSkillConfirmLang(normalized)) {
        return confirmed ? '\n\nConfirmed. Installing...' : '\n\nInstallation rejected.';
    }
    return confirmed ? '\n\nConfirmed. Installing...' : '\n\nInstallation rejected.';
}

function inferCriticalConfirmLangFromMessage(message: ChatMessage): string {
    const content = message.content || '';
    if (content.includes('Security warning') || content.includes('Risk factors')) return 'en';
    if (content.includes('風險') || content.includes('確認安裝') || content.includes('拒絕安裝')) return 'zh-Hant';
    return 'zh-Hans';
}

const MIN_AGENT_TIMEOUT_SEC = 240;
const DEFAULT_AGENT_TIMEOUT_SEC = 600;
const MAX_AGENT_TIMEOUT_SEC = 600;
const LOCAL_CONFIG_CHANGED_EVENT = 'maclaw-config-changed';

function normalizeAgentTimeoutSeconds(value: unknown): number {
    const seconds = Number(value || 0);
    if (!Number.isFinite(seconds) || seconds <= 0) return DEFAULT_AGENT_TIMEOUT_SEC;
    return Math.min(MAX_AGENT_TIMEOUT_SEC, Math.max(MIN_AGENT_TIMEOUT_SEC, Math.floor(seconds)));
}

export function useAIAssistant(options?: { refreshSessionsOnly?: () => Promise<void>; activeSessionKey?: string; lang?: string }) {
    const uiLang = options?.lang || "en";
    const activeSessionKeyForEvents = useCallback(() => options?.activeSessionKey || getActiveSessionKey(), [options?.activeSessionKey]);
    const [messages, setMessages] = useState<ChatMessage[]>(loadPersistedMessages);
    const [submittedPrompts, setSubmittedPrompts] = useState<string[]>(loadPersistedPrompts);
    const [draftInputValue, setDraftInputValue] = useState("");
    const [progressMessages, setProgressMessages] = useState<ChatMessage[]>([]);
    const [selectedFilePaths, setSelectedFilePaths] = useState<string[]>([]);
    const [trialReflectEnabled, setTrialReflectEnabled] = useState(false);
    const [preferences, setPreferences] = useState<AIAssistantPreferences>({ showTraceEntry: false });
    const [responseActivityTimeoutSec, setResponseActivityTimeoutSec] = useState(DEFAULT_AGENT_TIMEOUT_SEC);
    const [initStatus, setInitStatus] = useState<AIAssistantInitStatus>("connecting");
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    const [activeRound, setActiveRound] = useState<ActiveRound>(IDLE_ROUND);
    const [pendingTask, setPendingTask] = useState<AIAssistantPendingTask | null>(null);
    const [inFlightRoundVersion, setInFlightRoundVersion] = useState(0);
    const [pendingTaskVersion, setPendingTaskVersion] = useState(0);
    const [agentView, setAgentView] = useState<AgentView | null>(null);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const pendingTaskRef = useRef<AIAssistantPendingTask | null>(null);
    const pendingTasksByRequestRef = useRef<Map<string, AIAssistantPendingTask>>(new Map());
    const foregroundSendTailRef = useRef<Promise<boolean>>(Promise.resolve(true));
    const foregroundSendTailsBySessionRef = useRef<Map<string, Promise<boolean>>>(new Map());
    const inFlightRoundsByRequestRef = useRef<Map<string, ActiveRound>>(new Map());
    const idleWaitersBySessionRef = useRef<Map<string, Set<() => void>>>(new Map());
    const initStatusRef = useRef<AIAssistantInitStatus>("connecting");
    const latestNewsPayloadRef = useRef<string>("[]");
    const progressMessagesBySessionRef = useRef<Map<string, ChatMessage[]>>(new Map());
    const progressTailBySessionRef = useRef<Map<string, string>>(new Map());
    const activeProgressSessionKeyRef = useRef(normalizeRuntimeSessionKey(options?.activeSessionKey || getActiveSessionKey()));
    const selectedFilePathsBySessionRef = useRef<Map<string, string[]>>(new Map());
    const activeSelectedFilesSessionKeyRef = useRef(normalizeRuntimeSessionKey(options?.activeSessionKey || getActiveSessionKey()));
    const streamTokenBuffersByRequestRef = useRef<Map<string, StreamTokenBuffer>>(new Map());
    const streamAppendStatesByMessageRef = useRef<Map<string, StreamAppendState>>(new Map());
    const responseTimeoutControllersByRequestRef = useRef<Map<string, ResponseTimeoutController>>(new Map());
    const agentViewLifecycleSeqBySessionRef = useRef<Map<string, number>>(new Map());
    const agentViewsBySessionRef = useRef<Map<string, AgentView>>(new Map());
    const initialActiveSessionKey = options?.activeSessionKey || getActiveSessionKey();
    const activeAgentViewSessionKeyRef = useRef(normalizeRuntimeSessionKey(initialActiveSessionKey));
    const hasExplicitAgentViewSessionRef = useRef(!!String(initialActiveSessionKey || '').trim());
    const scrollOnNextNewsRef = useRef(true);
    const refreshSessionsOnlyRef = useRef(options?.refreshSessionsOnly);
    const lastPetSpeakingEmitAtRef = useRef(0);

    const stopResponseTimeout = useCallback((requestId?: string) => {
        const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
        if (normalizedRequestId) {
            const controller = responseTimeoutControllersByRequestRef.current.get(normalizedRequestId);
            responseTimeoutControllersByRequestRef.current.delete(normalizedRequestId);
            controller?.stop();
            return;
        }
        const activeRequestId = activeRoundRef.current.requestId;
        if (activeRequestId) {
            const controller = responseTimeoutControllersByRequestRef.current.get(activeRequestId);
            responseTimeoutControllersByRequestRef.current.delete(activeRequestId);
            controller?.stop();
        }
    }, []);

    const stopAllResponseTimeouts = useCallback(() => {
        for (const controller of responseTimeoutControllersByRequestRef.current.values()) {
            controller.stop();
        }
        responseTimeoutControllersByRequestRef.current.clear();
    }, []);

    const emitPetStateForAssistant = useCallback((state: DesktopPetState, source: string, ttlMs?: number) => {
        if (state === 'speaking') {
            const now = Date.now();
            if (now - lastPetSpeakingEmitAtRef.current < 900) return;
            lastPetSpeakingEmitAtRef.current = now;
        } else if (state === 'idle') {
            lastPetSpeakingEmitAtRef.current = 0;
        }
        emitDesktopPetState(state, source, ttlMs);
    }, []);

    useEffect(() => {
        refreshSessionsOnlyRef.current = options?.refreshSessionsOnly;
    }, [options?.refreshSessionsOnly]);

    const updateVisibleAgentViewForSession = useCallback((sessionKey: string, updater: AgentView | null | ((current: AgentView | null) => AgentView | null)) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey);
        const current = agentViewsBySessionRef.current.get(normalizedSessionKey) || null;
        const next = typeof updater === 'function' ? updater(current) : updater;
        if (next) {
            agentViewsBySessionRef.current.set(normalizedSessionKey, next);
        } else {
            agentViewsBySessionRef.current.delete(normalizedSessionKey);
        }
        if (activeAgentViewSessionKeyRef.current === normalizedSessionKey) {
            setAgentView(next || null);
        }
    }, []);

    const workflowFormAliasSessionKey = useCallback((ownerSessionKey: string, view: AgentView | null | undefined): string => {
        if (!view?.id?.startsWith('workflow:form:')) return '';
        const normalizedOwnerSessionKey = normalizeRuntimeSessionKey(ownerSessionKey);
        const activeSessionKey = activeAgentViewSessionKeyRef.current;
        if (!activeSessionKey || activeSessionKey === normalizedOwnerSessionKey) return '';
        if (isTaskManagedSessionKey(activeSessionKey)) return activeSessionKey;
        const currentRound = activeRoundRef.current;
        if (currentRound.phase !== 'idle' && normalizeRuntimeSessionKey(currentRound.sessionKey || 'desktop-user') === activeSessionKey) {
            return activeSessionKey;
        }
        for (const round of inFlightRoundsByRequestRef.current.values()) {
            if (round.phase !== 'idle' && normalizeRuntimeSessionKey(round.sessionKey || 'desktop-user') === activeSessionKey) {
                return activeSessionKey;
            }
        }
        for (const task of pendingTasksByRequestRef.current.values()) {
            if (normalizeRuntimeSessionKey(task.sessionKey || 'desktop-user') === activeSessionKey) {
                return activeSessionKey;
            }
        }
        return '';
    }, []);

    const updateWorkflowFormViewForSession = useCallback((sessionKey: string, view: AgentView | null) => {
        updateVisibleAgentViewForSession(sessionKey, view);
        const aliasSessionKey = workflowFormAliasSessionKey(sessionKey, view);
        if (aliasSessionKey) {
            updateVisibleAgentViewForSession(aliasSessionKey, view);
            logAIAssistantDiagnostic({
                event: 'agentview_workflow_form_alias_bound',
                ownerSessionKey: normalizeRuntimeSessionKey(sessionKey),
                aliasSessionKey,
                viewId: view?.id || '',
            });
        }
    }, [updateVisibleAgentViewForSession, workflowFormAliasSessionKey]);

    const clearWorkflowFormAliasForView = useCallback((viewId: string | undefined, ownerSessionKey: string) => {
        const normalizedOwnerSessionKey = normalizeRuntimeSessionKey(ownerSessionKey);
        for (const [sessionKey, view] of agentViewsBySessionRef.current) {
            if (sessionKey === normalizedOwnerSessionKey) continue;
            if (!view?.id?.startsWith('workflow:form:')) continue;
            if (viewId && view.id !== viewId) continue;
            const ownerFromView = agentViewFieldValue(view, '_workflow_user_id');
            if (ownerFromView && normalizeRuntimeSessionKey(ownerFromView) !== normalizedOwnerSessionKey) continue;
            updateVisibleAgentViewForSession(sessionKey, null);
        }
    }, [updateVisibleAgentViewForSession]);

    /** Force-reset agentView for the active session — used by session clear and /clear, /new, /reset.
     *  Advances lifecycle seq to reject in-flight stale events from before the reset. */
    const forceResetAgentViewForActiveSession = useCallback(() => {
        const key = activeAgentViewSessionKeyRef.current;
        agentViewsBySessionRef.current.delete(key);
        const prevSeq = agentViewLifecycleSeqBySessionRef.current.get(key) || 0;
        agentViewLifecycleSeqBySessionRef.current.set(key, prevSeq + 1);
        setAgentView(null);
    }, []);

    useEffect(() => {
        if (options?.activeSessionKey) {
            const normalizedSessionKey = normalizeRuntimeSessionKey(options.activeSessionKey);
            activeAgentViewSessionKeyRef.current = normalizedSessionKey;
            activeProgressSessionKeyRef.current = normalizedSessionKey;
            activeSelectedFilesSessionKeyRef.current = normalizedSessionKey;
            hasExplicitAgentViewSessionRef.current = true;
            setAgentView(agentViewsBySessionRef.current.get(normalizedSessionKey) || null);
            setProgressMessages(progressMessagesBySessionRef.current.get(normalizedSessionKey) || []);
            setSelectedFilePaths(selectedFilePathsBySessionRef.current.get(normalizedSessionKey) || []);
            return;
        }
        const handler = (event: Event) => {
            const rawSessionKey = String((event as CustomEvent)?.detail?.sessionKey || '').trim();
            const normalizedSessionKey = normalizeRuntimeSessionKey(rawSessionKey);
            activeAgentViewSessionKeyRef.current = normalizedSessionKey;
            activeProgressSessionKeyRef.current = normalizedSessionKey;
            activeSelectedFilesSessionKeyRef.current = normalizedSessionKey;
            hasExplicitAgentViewSessionRef.current = !!rawSessionKey;
            setAgentView(agentViewsBySessionRef.current.get(normalizedSessionKey) || null);
            setProgressMessages(progressMessagesBySessionRef.current.get(normalizedSessionKey) || []);
            setSelectedFilePaths(selectedFilePathsBySessionRef.current.get(normalizedSessionKey) || []);
        };
        window.addEventListener(LOCAL_ACTIVE_SESSION_CHANGED_EVENT, handler);
        return () => window.removeEventListener(LOCAL_ACTIVE_SESSION_CHANGED_EVENT, handler);
    }, [options?.activeSessionKey]);

    const adoptAgentViewSessionIfUnbound = useCallback((sessionKey: string) => {
        if (hasExplicitAgentViewSessionRef.current) return;
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey);
        activeAgentViewSessionKeyRef.current = normalizedSessionKey;
        hasExplicitAgentViewSessionRef.current = true;
    }, []);

    const isSessionIdle = useCallback((sessionKey = 'desktop-user') => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || 'desktop-user');
        const currentRound = activeRoundRef.current;
        const currentSessionKey = normalizeRuntimeSessionKey(currentRound.sessionKey || 'desktop-user');
        if (currentRound.phase !== 'idle' && currentSessionKey === normalizedSessionKey) return false;
        for (const round of inFlightRoundsByRequestRef.current.values()) {
            if (round.phase === 'idle') continue;
            if (normalizeRuntimeSessionKey(round.sessionKey || 'desktop-user') === normalizedSessionKey) return false;
        }
        for (const task of pendingTasksByRequestRef.current.values()) {
            if (normalizeRuntimeSessionKey(task.sessionKey || currentSessionKey || 'desktop-user') === normalizedSessionKey) return false;
        }
        return true;
    }, []);

    const notifyForegroundIdle = useCallback(() => {
        for (const [sessionKey, waitersForSession] of idleWaitersBySessionRef.current) {
            if (!isSessionIdle(sessionKey)) continue;
            idleWaitersBySessionRef.current.delete(sessionKey);
            for (const resolve of Array.from(waitersForSession)) resolve();
        }
    }, [isSessionIdle]);

    const waitForForegroundIdle = useCallback((sessionKey = 'desktop-user') => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || 'desktop-user');
        if (isSessionIdle(normalizedSessionKey)) return Promise.resolve();
        return new Promise<void>(resolve => {
            const waitersForSession = idleWaitersBySessionRef.current.get(normalizedSessionKey) || new Set<() => void>();
            waitersForSession.add(resolve);
            idleWaitersBySessionRef.current.set(normalizedSessionKey, waitersForSession);
        });
    }, [isSessionIdle]);

    const setPendingTaskState = useCallback((nextTask: AIAssistantPendingTask | null) => {
        const currentTask = pendingTaskRef.current;
        let changed = false;
        if (nextTask?.requestId) {
            const previous = pendingTasksByRequestRef.current.get(nextTask.requestId);
            const same = previous?.requestId === nextTask.requestId
                && previous?.sessionKey === nextTask.sessionKey
                && previous?.sessionID === nextTask.sessionID
                && previous?.jobID === nextTask.jobID
                && previous?.runID === nextTask.runID;
            pendingTasksByRequestRef.current.set(nextTask.requestId, nextTask);
            changed = !same;
        } else if (currentTask?.requestId && pendingTasksByRequestRef.current.delete(currentTask.requestId)) {
            changed = true;
        }
        pendingTaskRef.current = nextTask;
        setPendingTask(current => {
            const same = current?.requestId === nextTask?.requestId
                && current?.sessionKey === nextTask?.sessionKey
                && current?.sessionID === nextTask?.sessionID
                && current?.jobID === nextTask?.jobID
                && current?.runID === nextTask?.runID;
            return same ? current : nextTask;
        });
        if (changed) setPendingTaskVersion(version => version + 1);
        if (!nextTask && pendingTasksByRequestRef.current.size === 0) notifyForegroundIdle();
    }, [notifyForegroundIdle]);

    useEffect(() => {
        let cancelled = false;
        GetTrialReflectEnabled()
            .then(enabled => {
                if (!cancelled) setTrialReflectEnabled(!!enabled);
            })
            .catch(() => {
                if (!cancelled) setTrialReflectEnabled(false);
            });
        LoadConfig()
            .then(config => {
                if (!cancelled) {
                    const appConfig = new main.AppConfig(config || {});
                    setPreferences({ showTraceEntry: !!appConfig?.show_ai_trace_entry });
                    setTrialReflectEnabled(!!appConfig?.trial_reflect_enabled);
                    setResponseActivityTimeoutSec(normalizeAgentTimeoutSeconds(appConfig?.agent_response_timeout_sec));
                }
            })
            .catch(() => {
                if (!cancelled) {
                    setPreferences({ showTraceEntry: false });
                    setResponseActivityTimeoutSec(DEFAULT_AGENT_TIMEOUT_SEC);
                }
            });
        const handleConfigChanged = (cfg?: unknown) => {
            if (cancelled) return;
            const appConfig = new main.AppConfig(cfg || {});
            setPreferences({ showTraceEntry: !!appConfig?.show_ai_trace_entry });
            setTrialReflectEnabled(!!appConfig?.trial_reflect_enabled);
            setResponseActivityTimeoutSec(normalizeAgentTimeoutSeconds(appConfig?.agent_response_timeout_sec));
        };
        const cleanup = subscribeEvent("config-changed", handleConfigChanged);
        const handleLocalConfigChanged = (event: Event) => handleConfigChanged((event as CustomEvent).detail);
        window.addEventListener(LOCAL_CONFIG_CHANGED_EVENT, handleLocalConfigChanged);
        return () => {
            cancelled = true;
            cleanup();
            window.removeEventListener(LOCAL_CONFIG_CHANGED_EVENT, handleLocalConfigChanged);
        };
    }, []);

    const busySessionKeys = useMemo(() => {
        const keys = new Set<string>();
        if (activeRound.phase !== 'idle') keys.add(normalizeRuntimeSessionKey(activeRound.sessionKey || 'desktop-user'));
        for (const task of pendingTasksByRequestRef.current.values()) {
            keys.add(normalizeRuntimeSessionKey(task.sessionKey || activeRound.sessionKey || 'desktop-user'));
        }
        for (const round of inFlightRoundsByRequestRef.current.values()) {
            if (round.phase !== 'idle') keys.add(normalizeRuntimeSessionKey(round.sessionKey || 'desktop-user'));
        }
        return Array.from(keys);
    }, [activeRound, inFlightRoundVersion, pendingTaskVersion]);
    const streamingSessionKeys = useMemo(() => {
        const keys = new Set<string>();
        if (activeRound.phase === 'streaming') keys.add(normalizeRuntimeSessionKey(activeRound.sessionKey || 'desktop-user'));
        for (const round of inFlightRoundsByRequestRef.current.values()) {
            if (round.phase === 'streaming') keys.add(normalizeRuntimeSessionKey(round.sessionKey || 'desktop-user'));
        }
        return Array.from(keys);
    }, [activeRound, inFlightRoundVersion]);
    const sending = busySessionKeys.length > 0;
    const streaming = streamingSessionKeys.length > 0;
    const sendingSessionKey = normalizeRuntimeSessionKey((activeRound.phase !== 'idle' ? activeRound.sessionKey : pendingTask?.sessionKey) || busySessionKeys[0] || 'desktop-user');
    const streamingSessionKey = normalizeRuntimeSessionKey((activeRound.phase === 'streaming' ? activeRound.sessionKey : streamingSessionKeys[0]) || 'desktop-user');
    const visualBusy = streaming;
    const ready = initStatus === 'ready' || initStatus === 'degraded';

    const setInitStatusState = useCallback((nextStatus: AIAssistantInitStatus) => {
        if (initStatusRef.current === nextStatus) {
            return nextStatus;
        }
        initStatusRef.current = nextStatus;
        setInitStatus(current => current === nextStatus ? current : nextStatus);
        return nextStatus;
    }, []);

    useEffect(() => {
        let cancelled = false;
        let pollTimer: ReturnType<typeof setTimeout> | null = null;

        const clearPollTimer = () => {
            if (pollTimer) {
                clearTimeout(pollTimer);
                pollTimer = null;
            }
        };

        const scheduleCheck = () => {
            if (cancelled || initStatusRef.current === 'ready' || initStatusRef.current === 'degraded' || pollTimer) return;
            pollTimer = setTimeout(() => {
                pollTimer = null;
                void check();
            }, 1500);
        };

        const check = async () => {
            try {
                const { ready: isReady, status } = await checkInitReadiness();
                if (cancelled || initStatusRef.current === 'ready' || initStatusRef.current === 'degraded') return;
                if (isReady) {
                    clearPollTimer();
                    setInitStatusState('ready');
                    return;
                }
                setInitStatusState(status);
                scheduleCheck();
            } catch {
                if (!cancelled && initStatusRef.current !== 'ready' && initStatusRef.current !== 'degraded') {
                    scheduleCheck();
                }
            }
        };

        void check();

        const progressHandler = (status: string) => {
            const nextStatus = normalizeInitStatus(status);
            if (nextStatus === 'ready' || nextStatus === 'degraded') {
                clearPollTimer();
            }
            setInitStatusState(nextStatus);
        };
        const offInitProgress = subscribeEvent(INIT_PROGRESS_EVENT, progressHandler);

        return () => {
            cancelled = true;
            clearPollTimer();
            offInitProgress();
        };
    }, [setInitStatusState]);

    const setSelectedFiles = useCallback((nextPaths: string[], sessionKey?: string) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || activeSelectedFilesSessionKeyRef.current);
        const normalized = nextPaths.map(normalizeSelectedFilePath).filter(Boolean);
        if (normalized.length > 0) {
            selectedFilePathsBySessionRef.current.set(normalizedSessionKey, normalized);
        } else {
            selectedFilePathsBySessionRef.current.delete(normalizedSessionKey);
        }
        if (activeSelectedFilesSessionKeyRef.current === normalizedSessionKey) {
            setSelectedFilePaths(normalized);
        }
        return normalized;
    }, []);

    const addSelectedFiles = useCallback((newPaths: string[], sessionKey?: string) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || activeSelectedFilesSessionKeyRef.current);
        const normalized = newPaths.map(normalizeSelectedFilePath).filter(Boolean);
        if (normalized.length === 0) return;
        const current = selectedFilePathsBySessionRef.current.get(normalizedSessionKey) || [];
        const existing = new Set(current);
        const next = [...current, ...normalized.filter(p => !existing.has(p))];
        selectedFilePathsBySessionRef.current.set(normalizedSessionKey, next);
        if (activeSelectedFilesSessionKeyRef.current === normalizedSessionKey) {
            setSelectedFilePaths(next);
        }
    }, []);

    const removeSelectedFile = useCallback((index: number) => {
        const normalizedSessionKey = activeSelectedFilesSessionKeyRef.current;
        const current = selectedFilePathsBySessionRef.current.get(normalizedSessionKey) || [];
        const next = current.filter((_, i) => i !== index);
        if (next.length > 0) {
            selectedFilePathsBySessionRef.current.set(normalizedSessionKey, next);
        } else {
            selectedFilePathsBySessionRef.current.delete(normalizedSessionKey);
        }
        setSelectedFilePaths(next);
    }, []);

    const setRoundState = useCallback((next: ActiveRound) => {
        const current = activeRoundRef.current;
        if (sameActiveRound(current, next)) {
            return current;
        }
        activeRoundRef.current = next;
        setActiveRound(prev => sameActiveRound(prev, next) ? prev : next);
        if (next.phase === 'idle') notifyForegroundIdle();
        return next;
    }, [notifyForegroundIdle]);

    const rememberInFlightRound = useCallback((round: ActiveRound) => {
        if (!round.requestId) return;
        inFlightRoundsByRequestRef.current.set(round.requestId, round);
        setInFlightRoundVersion(version => version + 1);
    }, []);

    const forgetInFlightRound = useCallback((requestId: string) => {
        if (!requestId) return;
        if (inFlightRoundsByRequestRef.current.delete(requestId)) {
            setInFlightRoundVersion(version => version + 1);
            notifyForegroundIdle();
        }
    }, [notifyForegroundIdle]);

    const replaceRuntimeRequestId = useCallback((oldRequestId: string, nextRequestId: string) => {
        if (!oldRequestId || !nextRequestId || oldRequestId === nextRequestId) return;
        const controller = responseTimeoutControllersByRequestRef.current.get(oldRequestId);
        if (controller) {
            responseTimeoutControllersByRequestRef.current.delete(oldRequestId);
            controller.stop();
        }
        const buffer = streamTokenBuffersByRequestRef.current.get(oldRequestId);
        if (buffer) {
            streamTokenBuffersByRequestRef.current.delete(oldRequestId);
            streamTokenBuffersByRequestRef.current.set(nextRequestId, { ...buffer, requestId: nextRequestId });
        }
        const pendingTaskForOldRequest = pendingTasksByRequestRef.current.get(oldRequestId);
        if (pendingTaskForOldRequest) {
            pendingTasksByRequestRef.current.delete(oldRequestId);
            pendingTasksByRequestRef.current.set(nextRequestId, { ...pendingTaskForOldRequest, requestId: nextRequestId });
            if (pendingTaskRef.current?.requestId === oldRequestId) {
                const nextTask = { ...pendingTaskForOldRequest, requestId: nextRequestId };
                pendingTaskRef.current = nextTask;
                setPendingTask(nextTask);
            }
            setPendingTaskVersion(version => version + 1);
        }
    }, []);

    const replaceInFlightRoundRequestId = useCallback((oldRequestId: string, nextRound: ActiveRound) => {
        if (oldRequestId && oldRequestId !== nextRound.requestId) {
            inFlightRoundsByRequestRef.current.delete(oldRequestId);
            replaceRuntimeRequestId(oldRequestId, nextRound.requestId);
        }
        rememberInFlightRound(nextRound);
    }, [rememberInFlightRound, replaceRuntimeRequestId]);

    const updateInFlightRound = useCallback((requestId: string, updater: (round: ActiveRound) => ActiveRound) => {
        if (!requestId) return null;
        const current = inFlightRoundsByRequestRef.current.get(requestId);
        if (!current) return null;
        const next = updater(current);
        inFlightRoundsByRequestRef.current.set(requestId, next);
        if (!sameActiveRound(current, next)) {
            setInFlightRoundVersion(version => version + 1);
        }
        return next;
    }, []);

    const clearPendingTaskForRequest = useCallback((requestId: string) => {
        if (!requestId) return;
        let removed = false;
        if (pendingTasksByRequestRef.current.delete(requestId)) {
            removed = true;
            setPendingTaskVersion(version => version + 1);
        }
        if (pendingTaskRef.current?.requestId === requestId) {
            const nextTask = Array.from(pendingTasksByRequestRef.current.values()).at(-1) || null;
            pendingTaskRef.current = nextTask;
            setPendingTask(nextTask);
        }
        if (removed) notifyForegroundIdle();
    }, [notifyForegroundIdle]);

    const findInFlightRoundBySession = useCallback((sessionKey: string) => {
        const normalizedSessionKey = sessionKey || 'desktop-user';
        for (const round of inFlightRoundsByRequestRef.current.values()) {
            if (round.phase === 'idle') continue;
            if ((round.sessionKey || 'desktop-user') === normalizedSessionKey) return round;
        }
        return null;
    }, []);

    const findPendingTaskBySession = useCallback((sessionKey: string, fallbackSessionKey = 'desktop-user') => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || fallbackSessionKey || 'desktop-user');
        for (const task of pendingTasksByRequestRef.current.values()) {
            if (normalizeRuntimeSessionKey(task.sessionKey || fallbackSessionKey || 'desktop-user') === normalizedSessionKey) return task;
        }
        return null;
    }, []);

    const transitionRound = useCallback((updater: (current: ActiveRound) => ActiveRound) => {
        const next = updater(activeRoundRef.current);
        if (next === activeRoundRef.current || sameActiveRound(next, activeRoundRef.current)) {
            return activeRoundRef.current;
        }
        return setRoundState(next);
    }, [setRoundState]);

    const resetActiveRound = useCallback((generation?: number) => {
        const current = activeRoundRef.current;
        const next = generation === undefined
            ? (current.generation === 0 ? IDLE_ROUND : createIdleRound(current.generation))
            : (generation === 0 ? IDLE_ROUND : createIdleRound(generation));
        if (sameActiveRound(current, next)) {
            return current;
        }
        if (current.requestId && inFlightRoundsByRequestRef.current.delete(current.requestId)) {
            setInFlightRoundVersion(version => version + 1);
        }
        return setRoundState(next);
    }, [setRoundState]);

    const ensureRoundPlaceholder = useCallback((generation: number) => {
        const current = activeRoundRef.current;
        if (current.generation !== generation) {
            return null;
        }
        const assistantMessageId = current.assistantMessageId || nextId();
        if (current.phase !== 'streaming' || current.assistantMessageId !== assistantMessageId) {
            setRoundState({
                generation,
                phase: 'streaming',
                assistantMessageId,
                requestId: current.requestId,
                sessionKey: current.sessionKey,
                userText: current.userText,
            });
        }
        setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId, current.requestId, current.sessionKey));
        return assistantMessageId;
    }, [setRoundState]);

    const clearTransientProgress = useCallback((sessionKey?: string) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || activeRoundRef.current.sessionKey || activeProgressSessionKeyRef.current);
        progressTailBySessionRef.current.delete(normalizedSessionKey);
        progressMessagesBySessionRef.current.delete(normalizedSessionKey);
        if (activeProgressSessionKeyRef.current === normalizedSessionKey) {
            setProgressMessages([]);
        }
    }, []);

    const appendProgressForSession = useCallback((sessionKey: string, progressText: string) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey);
        if (progressTailBySessionRef.current.get(normalizedSessionKey) === progressText) {
            return;
        }
        progressTailBySessionRef.current.set(normalizedSessionKey, progressText);
        const nextMessages = appendProgressText(progressMessagesBySessionRef.current.get(normalizedSessionKey) || [], progressText);
        progressMessagesBySessionRef.current.set(normalizedSessionKey, nextMessages);
        if (activeProgressSessionKeyRef.current === normalizedSessionKey) {
            setProgressMessages(nextMessages);
        }
    }, []);

    const finalizeRound = useCallback((generation: number) => {
        if (activeRoundRef.current.generation !== generation) return;
        clearTransientProgress();
        resetActiveRound();
        emitPetStateForAssistant('idle', 'ai:round-done');
    }, [clearTransientProgress, emitPetStateForAssistant, resetActiveRound]);

    const startResponseTimeout = useCallback((round: { generation: number; assistantMessageId: string; requestId: string; source: string }) => {
        stopResponseTimeout(round.requestId);
        const activityTimeoutMs = responseActivityTimeoutSec * 1000;
        let activityTimer: ReturnType<typeof setTimeout> | null = null;

        const stopController = () => {
            if (activityTimer) {
                clearTimeout(activityTimer);
                activityTimer = null;
            }
        };

        const fireActivityTimeout = () => {
            activityTimer = null;
            if (responseTimeoutControllersByRequestRef.current.get(round.requestId) !== controller) return;
            const currentRound = activeRoundRef.current.requestId === round.requestId
                ? activeRoundRef.current
                : inFlightRoundsByRequestRef.current.get(round.requestId);
            if (!currentRound || currentRound.generation !== round.generation) return;
            if (currentRound.phase === 'idle') return;
            if (currentRound.phase === 'streaming') return;
            const activeRequestId = currentRound.requestId || round.requestId;
            if (pendingTasksByRequestRef.current.has(activeRequestId)) return;
            responseTimeoutControllersByRequestRef.current.delete(round.requestId);
            setMessages(prev => replaceRoundWithError(prev, round.assistantMessageId, activeRequestId,
                `⏱️ 请求超时（${responseActivityTimeoutSec}秒无响应），请重试。`, true));
            clearTransientProgress(currentRound.sessionKey || 'desktop-user');
            if (activeRoundRef.current.requestId === activeRequestId) resetActiveRound(round.generation);
            else forgetInFlightRound(activeRequestId);
            emitPetStateForAssistant('idle', `${round.source}:timeout`);
        };

        const resetController = () => {
            if (responseTimeoutControllersByRequestRef.current.get(round.requestId) !== controller) return;
            stopController();
            activityTimer = setTimeout(fireActivityTimeout, activityTimeoutMs);
        };

        const controller: ResponseTimeoutController = {
            generation: round.generation,
            requestId: round.requestId,
            assistantMessageId: round.assistantMessageId,
            reset: resetController,
            stop: stopController,
        };
        responseTimeoutControllersByRequestRef.current.set(round.requestId, controller);
        controller.reset();
        return controller;
    }, [clearTransientProgress, emitPetStateForAssistant, forgetInFlightRound, resetActiveRound, responseActivityTimeoutSec, stopResponseTimeout]);

    const resetResponseTimeoutForRound = useCallback((round: ActiveRound | null | undefined) => {
        if (!round?.requestId) return;
        const controller = responseTimeoutControllersByRequestRef.current.get(round.requestId);
        if (!controller) return;
        if (round.generation !== controller.generation) return;
        controller.reset();
    }, []);

    const resetResponseTimeoutForActiveRound = useCallback(() => {
        resetResponseTimeoutForRound(activeRoundRef.current);
    }, [resetResponseTimeoutForRound]);

    const streamAppendStateForMessage = useCallback((assistantMessageId: string): StreamAppendState => {
        let state = streamAppendStatesByMessageRef.current.get(assistantMessageId);
        if (!state) {
            state = { content: '', reasoning: '', contentSnapshotMode: false, reasoningSnapshotMode: false };
            streamAppendStatesByMessageRef.current.set(assistantMessageId, state);
        }
        return state;
    }, []);

    const resetStreamAppendStateForMessage = useCallback((assistantMessageId: string | null | undefined) => {
        if (assistantMessageId) streamAppendStatesByMessageRef.current.delete(assistantMessageId);
    }, []);

    const appendTokenToAssistantMessage = useCallback((assistantMessageId: string, text: string) => {
        if (!assistantMessageId || !text) return;
        setMessages(prev => updateTailMessage(prev, assistantMessageId, message => appendTokenToMessage(message, text))
            ?? updateMessageById(prev, assistantMessageId, message => appendTokenToMessage(message, text)));
    }, []);

    const appendTokenToDetachedRound = useCallback((round: ActiveRound, text: string) => {
        if (!round.requestId || !round.assistantMessageId || !text) return;
        const normalizedText = normalizeStreamDeltaWithState(streamAppendStateForMessage(round.assistantMessageId), text);
        if (!normalizedText) return;
        updateInFlightRound(round.requestId, current => ({ ...current, phase: 'streaming' }));
        setMessages(prev => appendTokenToRound(
            appendAssistantPlaceholder(prev, round.assistantMessageId || '', round.requestId, round.sessionKey),
            round.assistantMessageId,
            normalizedText,
        ));
    }, [streamAppendStateForMessage, updateInFlightRound]);

    const clearStreamTokenFlushTimer = useCallback((buffer: StreamTokenBuffer | null | undefined) => {
        if (!buffer?.flushTimer) return;
        clearTimeout(buffer.flushTimer);
        buffer.flushTimer = null;
    }, []);

    const flushStreamTokenBuffer = useCallback((requestId?: string) => {
        const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
        const buffer = streamTokenBuffersByRequestRef.current.get(normalizedRequestId || activeRoundRef.current.requestId);
        if (!buffer) return;
        clearStreamTokenFlushTimer(buffer);
        const text = buffer.text;
        if (!text) return;
        buffer.text = '';
        appendTokenToAssistantMessage(buffer.assistantMessageId, text);
    }, [appendTokenToAssistantMessage, clearStreamTokenFlushTimer]);

    const resetStreamTokenBuffer = useCallback((requestId?: string) => {
        const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
        const key = normalizedRequestId || activeRoundRef.current.requestId;
        if (!key) return;
        const buffer = streamTokenBuffersByRequestRef.current.get(key);
        clearStreamTokenFlushTimer(buffer);
        const round = activeRoundRef.current.requestId === key
            ? activeRoundRef.current
            : inFlightRoundsByRequestRef.current.get(key);
        resetStreamAppendStateForMessage(buffer?.assistantMessageId || round?.assistantMessageId);
        streamTokenBuffersByRequestRef.current.delete(key);
    }, [clearStreamTokenFlushTimer, resetStreamAppendStateForMessage]);

    const resetAllStreamTokenBuffers = useCallback(() => {
        for (const buffer of streamTokenBuffersByRequestRef.current.values()) {
            clearStreamTokenFlushTimer(buffer);
        }
        streamTokenBuffersByRequestRef.current.clear();
        streamAppendStatesByMessageRef.current.clear();
    }, [clearStreamTokenFlushTimer]);

    const forgetInFlightRoundsForSession = useCallback((sessionKey: string) => {
        const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey || 'desktop-user');
        let changed = false;
        for (const [requestId, round] of inFlightRoundsByRequestRef.current) {
            if (normalizeRuntimeSessionKey(round.sessionKey || 'desktop-user') === normalizedSessionKey) {
                stopResponseTimeout(requestId);
                resetStreamTokenBuffer(requestId);
                inFlightRoundsByRequestRef.current.delete(requestId);
                changed = true;
            }
        }
        for (const [requestId, task] of pendingTasksByRequestRef.current) {
            if (normalizeRuntimeSessionKey(task.sessionKey || 'desktop-user') === normalizedSessionKey) {
                stopResponseTimeout(requestId);
                resetStreamTokenBuffer(requestId);
                pendingTasksByRequestRef.current.delete(requestId);
                changed = true;
            }
        }
        if (normalizeRuntimeSessionKey(pendingTaskRef.current?.sessionKey || 'desktop-user') === normalizedSessionKey) {
            const nextTask = Array.from(pendingTasksByRequestRef.current.values()).at(-1) || null;
            pendingTaskRef.current = nextTask;
            setPendingTask(nextTask);
        }
        clearTransientProgress(normalizedSessionKey);
        selectedFilePathsBySessionRef.current.delete(normalizedSessionKey);
        if (activeSelectedFilesSessionKeyRef.current === normalizedSessionKey) {
            setSelectedFilePaths([]);
        }
        agentViewsBySessionRef.current.delete(normalizedSessionKey);
        agentViewLifecycleSeqBySessionRef.current.delete(normalizedSessionKey);
        if (activeAgentViewSessionKeyRef.current === normalizedSessionKey) {
            setAgentView(null);
        }
        if (changed) {
            setInFlightRoundVersion(version => version + 1);
            setPendingTaskVersion(version => version + 1);
            notifyForegroundIdle();
        }
        const currentRound = activeRoundRef.current;
        if (normalizeRuntimeSessionKey(currentRound.sessionKey || 'desktop-user') === normalizedSessionKey && currentRound.phase !== 'idle') {
            stopResponseTimeout(currentRound.requestId);
            resetStreamTokenBuffer(currentRound.requestId);
            resetActiveRound(currentRound.generation + 1);
        }
    }, [clearTransientProgress, notifyForegroundIdle, resetActiveRound, resetStreamTokenBuffer, stopResponseTimeout]);

    const queueStreamToken = useCallback((round: ActiveRound, text: string) => {
        if (!round.assistantMessageId || !text) return;
        const normalizedText = normalizeStreamDeltaWithState(streamAppendStateForMessage(round.assistantMessageId), text);
        if (!normalizedText) return;

        // Reasoning tokens (\x01 prefix) are rendered immediately without
        // buffering. They display in a collapsed "thinking" area - the DOM
        // update cost is minimal (hidden content), and immediate rendering
        // ensures the first reasoning token triggers the "thinking" UI state
        // without delay.
        if (normalizedText.startsWith('\x01')) {
            appendTokenToAssistantMessage(round.assistantMessageId, normalizedText);
            return;
        }

        let buffer = streamTokenBuffersByRequestRef.current.get(round.requestId);
        if (!buffer || buffer.requestId !== round.requestId || buffer.assistantMessageId !== round.assistantMessageId) {
            clearStreamTokenFlushTimer(buffer);
            buffer = {
                requestId: round.requestId,
                assistantMessageId: round.assistantMessageId,
                text: '',
                flushTimer: null,
                hasRenderedFirstToken: false,
            };
            streamTokenBuffersByRequestRef.current.set(round.requestId, buffer);
        }

        if (!buffer.hasRenderedFirstToken) {
            buffer.hasRenderedFirstToken = true;
            appendTokenToAssistantMessage(round.assistantMessageId, normalizedText);
            return;
        }

        buffer.text += normalizedText;
        if (!buffer.flushTimer) {
            buffer.flushTimer = setTimeout(() => flushStreamTokenBuffer(round.requestId), STREAM_TOKEN_FLUSH_MS);
        }
    }, [appendTokenToAssistantMessage, clearStreamTokenFlushTimer, flushStreamTokenBuffer, streamAppendStateForMessage]);

    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const latestMessagesRef = useRef(messages);
    const latestPromptsRef = useRef(submittedPrompts);
    const lastPersistedPayloadRef = useRef<string | null>(null);
    const lastPersistedPromptsPayloadRef = useRef<string | null>(null);
    const persistOnUnmountRef = useRef(true);
    const contextBoundaryMessageIDRef = useRef<string | null>(loadPersistedContextBoundaryMessageID());
    const uiStateLoadedRef = useRef(false);
    const persistQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));

    const persistUIState = useCallback((nextMessages: ChatMessage[], nextPrompts: string[], nextBoundary: string | null): Promise<boolean> => {
        const messagesToSave = parseSerializedMessages(serializePersistedMessages(nextMessages));
        const promptsToSave = normalizePersistedPrompts(nextPrompts);
        const payload = {
            messages: messagesToSave,
            prompts: promptsToSave,
            context_boundary_message_id: nextBoundary || '',
        };
        const save = () => SaveAIAssistantUIState(payload).then(() => true).catch((err) => {
            console.error('Failed to save AI assistant UI state:', err);
            return false;
        });
        const queued = persistQueueRef.current.then(save, save);
        persistQueueRef.current = queued;
        return queued;
    }, []);

    useEffect(() => {
        let cancelled = false;
        void LoadAIAssistantUIState().then((state) => {
            if (cancelled) return;
            const loadedMessages = backendStateMessages(state);
            const loadedPrompts = backendStatePrompts(state);
            const loadedBoundary = backendStateBoundary(state);
            const legacyMessages = latestMessagesRef.current;
            const legacyPrompts = latestPromptsRef.current;
            const legacyBoundary = contextBoundaryMessageIDRef.current;
            const hasBackendState = loadedMessages.length > 0 || loadedPrompts.length > 0 || !!loadedBoundary;
            const hasLegacyState = legacyMessages.length > 0 || legacyPrompts.length > 0 || !!legacyBoundary;
            if (hasBackendState) {
                const mergedMessages = loadedMessages.length > 0 ? loadedMessages : legacyMessages;
                const mergedPrompts = loadedPrompts.length > 0 ? loadedPrompts : legacyPrompts;
                const mergedBoundary = loadedBoundary || legacyBoundary;
                latestMessagesRef.current = mergedMessages;
                latestPromptsRef.current = mergedPrompts;
                contextBoundaryMessageIDRef.current = mergedBoundary;
                lastPersistedPayloadRef.current = serializePersistedMessages(mergedMessages);
                lastPersistedPromptsPayloadRef.current = JSON.stringify(mergedPrompts);
                setMessages(mergedMessages);
                setSubmittedPrompts(mergedPrompts);
                if (hasLegacyState && (loadedMessages.length === 0 || loadedPrompts.length === 0 || !loadedBoundary)) {
                    void persistUIState(mergedMessages, mergedPrompts, mergedBoundary)
                        .then((saved) => { if (saved) legacyClearAIAssistantUIState(); });
                } else {
                    legacyClearAIAssistantUIState();
                }
            } else if (hasLegacyState) {
                void persistUIState(legacyMessages, legacyPrompts, contextBoundaryMessageIDRef.current)
                    .then((saved) => { if (saved) legacyClearAIAssistantUIState(); });
            }
            uiStateLoadedRef.current = true;
        }).catch((err) => {
            console.error('Failed to load AI assistant UI state:', err);
            uiStateLoadedRef.current = true;
        });
        return () => {
            cancelled = true;
        };
    }, [persistUIState]);

    useEffect(() => {
        latestMessagesRef.current = messages;
        if (!uiStateLoadedRef.current) return;
        const nextPayload = serializePersistedMessages(messages);
        if (nextPayload === lastPersistedPayloadRef.current) {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
            return;
        }
        if (persistTimerRef.current) clearTimeout(persistTimerRef.current);
        persistTimerRef.current = setTimeout(() => {
            persistTimerRef.current = null;
            const payload = serializePersistedMessages(latestMessagesRef.current);
            if (payload === lastPersistedPayloadRef.current) return;
            const promptPayload = JSON.stringify(latestPromptsRef.current);
            void persistUIState(latestMessagesRef.current, latestPromptsRef.current, contextBoundaryMessageIDRef.current)
                .then((saved) => {
                    if (!saved) return;
                    lastPersistedPayloadRef.current = payload;
                    lastPersistedPromptsPayloadRef.current = promptPayload;
                });
        }, 300);
    }, [messages, persistUIState]);
    useEffect(() => {
        latestPromptsRef.current = submittedPrompts;
        if (!uiStateLoadedRef.current) return;
        const nextPayload = JSON.stringify(submittedPrompts);
        if (nextPayload === lastPersistedPromptsPayloadRef.current) {
            return;
        }
        const messagePayload = serializePersistedMessages(latestMessagesRef.current);
        void persistUIState(latestMessagesRef.current, submittedPrompts, contextBoundaryMessageIDRef.current)
            .then((saved) => {
                if (!saved) return;
                lastPersistedPayloadRef.current = messagePayload;
                lastPersistedPromptsPayloadRef.current = nextPayload;
            });
    }, [submittedPrompts, persistUIState]);
    useEffect(() => {
        lastPersistedPayloadRef.current = serializePersistedMessages(latestMessagesRef.current);
        lastPersistedPromptsPayloadRef.current = JSON.stringify(latestPromptsRef.current);
        return () => {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
            if (!persistOnUnmountRef.current) {
                return;
            }
            const payload = serializePersistedMessages(latestMessagesRef.current);
            const promptPayload = JSON.stringify(latestPromptsRef.current);
            if (payload !== lastPersistedPayloadRef.current || promptPayload !== lastPersistedPromptsPayloadRef.current) {
                void persistUIState(latestMessagesRef.current, latestPromptsRef.current, contextBoundaryMessageIDRef.current)
                    .then((saved) => {
                        if (!saved) return;
                        lastPersistedPayloadRef.current = payload;
                        lastPersistedPromptsPayloadRef.current = promptPayload;
                    });
            }
        };
    }, [persistUIState]);

    useEffect(() => {
        const handler = (event: Event) => {
            const sessionKey = String((event as CustomEvent)?.detail?.sessionKey || '').trim();
            if (!sessionKey) return;
            console.info('[useAIAssistant] forget in-flight rounds for session', { sessionKey });
            forgetInFlightRoundsForSession(sessionKey);
        };
        window.addEventListener(LOCAL_FORGET_SESSION_ROUNDS_EVENT, handler);
        return () => window.removeEventListener(LOCAL_FORGET_SESSION_ROUNDS_EVENT, handler);
    }, [forgetInFlightRoundsForSession]);

    // Fetch latest news from Hub Center and prepend as system messages.
    const doFetchNews = useCallback(() => {
        FetchNews().then((articles: any[]) => {
            const newsMessages = Array.isArray(articles) ? articles.map(createNewsMessage) : [];
            const nextPayload = serializeNewsMessages(newsMessages);
            if (nextPayload === latestNewsPayloadRef.current) {
                return;
            }
            latestNewsPayloadRef.current = nextPayload;
            setMessages(prev => replaceNewsMessages(prev, newsMessages));
            if (scrollOnNextNewsRef.current && newsMessages.length > 0) {
                scrollOnNextNewsRef.current = false;
                setScrollToTopSeq(s => s + 1);
            }
        }).catch(() => { /* silently ignore news fetch failures */ });
    }, []);

    // Fetch on mount + refresh every 6 hours.
    const newsFetchedRef = useRef(false);
    useEffect(() => {
        if (!newsFetchedRef.current) {
            newsFetchedRef.current = true;
            doFetchNews();
        }
        const SIX_HOURS = 6 * 60 * 60 * 1000;
        const timer = setInterval(doFetchNews, SIX_HOURS);
        return () => clearInterval(timer);
    }, [doFetchNews]);

    useEffect(() => {
        const refreshPendingTask = async () => {
            const pendingTasks = Array.from(pendingTasksByRequestRef.current.values());
            if (pendingTasks.length === 0) return;
            try {
                const sessions = await ListRemoteSessions() as AIAssistantRemoteSessionView[];
                const activeSessions = (Array.isArray(sessions) ? sessions : []).filter(session => normalizeLaunchSource(session?.launch_source) === 'ai' && !isTerminalRemoteStatus(session?.status));
                for (const currentTask of pendingTasks) {
                    if (!pendingTasksByRequestRef.current.has(currentTask.requestId)) continue;
                    const stillActive = activeSessions.some(session => matchesPendingTaskSession(session, currentTask));
                    if (stillActive) continue;
                    clearPendingTaskForRequest(currentTask.requestId);
                    const currentRound = activeRoundRef.current;
                    if (currentRound.requestId === currentTask.requestId) {
                        stopResponseTimeout(currentTask.requestId);
                        clearTransientProgress(currentTask.sessionKey || 'desktop-user');
                        resetActiveRound(currentRound.generation);
                    } else {
                        stopResponseTimeout(currentTask.requestId);
                        resetStreamTokenBuffer(currentTask.requestId);
                        forgetInFlightRound(currentTask.requestId);
                        clearTransientProgress(currentTask.sessionKey || 'desktop-user');
                    }
                }
            } catch {
            }
        };

        void refreshPendingTask();
        if (pendingTasksByRequestRef.current.size === 0) return;

        const handleRemoteStateChanged = () => {
            void refreshPendingTask();
        };
        const offRemoteStateChanged = subscribeEvent('remote-state-changed', handleRemoteStateChanged);
        const offRemoteSessionChanged = subscribeEvent('remote-session-changed', handleRemoteStateChanged);
        const timer = setInterval(() => {
            void refreshPendingTask();
        }, 2000);

        return () => {
            clearInterval(timer);
            offRemoteStateChanged();
            offRemoteSessionChanged();
        };
    }, [clearPendingTaskForRequest, clearTransientProgress, forgetInFlightRound, pendingTaskVersion, resetActiveRound, stopResponseTimeout]);

    useEffect(() => {
        const tokenHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (event.request_id && matchesActiveRequest(currentRound, event)) {
                resetResponseTimeoutForActiveRound();
            }
            const detachedRound = event.request_id ? inFlightRoundsByRequestRef.current.get(event.request_id) : undefined;
            if (detachedRound && !matchesActiveRequest(currentRound, event)) {
                resetResponseTimeoutForRound(detachedRound);
                logRolePrefixDiagnostic('stream-event-received', event.text || '', undefined, {
                    requestId: event.request_id,
                    activeRequestId: currentRound.requestId,
                    messageId: detachedRound.assistantMessageId,
                    matchedDetachedRequest: true,
                });
                appendTokenToDetachedRound(detachedRound, event.text || '');
                return;
            }
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            const isActiveRequest = matchesActiveRequest(currentRound, event);
            logRolePrefixDiagnostic('stream-event-received', event.text || '', undefined, {
                requestId: event.request_id,
                activeRequestId: currentRound.requestId,
                messageId: currentRound.assistantMessageId,
                matchedActiveRequest: isActiveRequest,
            });
            if (!isActiveRequest) return;
            if (!currentRound.assistantMessageId || !event.text) return;
            resetResponseTimeoutForActiveRound();
            emitPetStateForAssistant('speaking', 'ai:stream-token', 1800);
            queueStreamToken(currentRound, event.text);
        };

        const newRoundHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            const detachedRound = event.request_id ? inFlightRoundsByRequestRef.current.get(event.request_id) : undefined;
            if (detachedRound && !matchesActiveRequest(currentRound, event)) {
                resetResponseTimeoutForRound(detachedRound);
                updateInFlightRound(event.request_id || '', current => ({ ...current, phase: 'streaming' }));
                resetStreamAppendStateForMessage(detachedRound.assistantMessageId);
                if (detachedRound.assistantMessageId) {
                    setMessages(prev => appendAssistantPlaceholder(prev, detachedRound.assistantMessageId || '', detachedRound.requestId, detachedRound.sessionKey));
                }
                return;
            }
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            if (!matchesActiveRequest(currentRound, event)) return;
            resetResponseTimeoutForActiveRound();
            emitPetStateForAssistant('thinking', 'ai:new-round', 10000);
            flushStreamTokenBuffer(currentRound.requestId);
            ensureRoundPlaceholder(currentRound.generation);
        };

        const streamDoneHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            const detachedRound = event.request_id ? inFlightRoundsByRequestRef.current.get(event.request_id) : undefined;
            if (detachedRound && !matchesActiveRequest(currentRound, event)) {
                resetResponseTimeoutForRound(detachedRound);
                updateInFlightRound(event.request_id || '', current => ({ ...current, phase: 'requesting' }));
                return;
            }
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            if (!matchesActiveRequest(currentRound, event)) return;
            resetResponseTimeoutForActiveRound();
            emitPetStateForAssistant('thinking', 'ai:stream-done', 2500);
            flushStreamTokenBuffer(currentRound.requestId);
            resetStreamAppendStateForMessage(currentRound.assistantMessageId);
            transitionRound(current => {
                if (current.phase !== 'streaming') return current;
                return { ...current, phase: 'requesting' };
            });
        };

        const offStreamToken = subscribeEvent(STREAM_TOKEN_EVENT, tokenHandler);
        const offNewRound = subscribeEvent(NEW_ROUND_EVENT, newRoundHandler);
        const offStreamDone = subscribeEvent(STREAM_DONE_EVENT, streamDoneHandler);
        return () => {
            offStreamToken();
            offNewRound();
            offStreamDone();
            resetAllStreamTokenBuffers();
        };
    }, [activeSessionKeyForEvents, appendTokenToDetachedRound, emitPetStateForAssistant, ensureRoundPlaceholder, flushStreamTokenBuffer, queueStreamToken, resetAllStreamTokenBuffers, resetResponseTimeoutForActiveRound, resetResponseTimeoutForRound, resetStreamAppendStateForMessage, transitionRound, updateInFlightRound]);

        // Listen for the async response event. When SendAIAssistantMessage returns
    // {deferred: true} (non-blocking mode), the actual response arrives here.
    // This is the single source of truth for final response processing -
    // all messages (including /new, /reset, normal chat) are handled here.
    useEffect(() => {
        const handler = (payload: any) => {
            let resp: AIAssistantSendResult;
            try {
                resp = typeof payload === 'string' ? JSON.parse(payload) : payload;
            } catch {
                return;
            }
            // Response events are NOT filtered by session_key. The requestId
            // match (matchesActiveRequest below) is sufficient to ensure we only
            // process our own response. Filtering by session_key here would cause
            // the round to never finalize if the user switches tabs mid-stream,
            // permanently locking the input box.
            const normalized = normalizeSendResponse(resp, preferences.showTraceEntry);
            const responseRequestId = resolveSendRequestID(normalized) || '';
            const currentRound = activeRoundRef.current;
            // Final responses must carry the request id returned by SendAIAssistantMessage.
            // Treat missing ids as malformed terminal events instead of unlocking the wrong round.
            const activeMatch = matchesActiveResponseRequest(currentRound, responseRequestId);
            const round = activeMatch
                ? currentRound
                : inFlightRoundsByRequestRef.current.get(responseRequestId);
            const eventSessionKey = normalizeRuntimeSessionKey(normalized.session_key || '');
            if (!round || round.phase === 'idle') {
                console.info('[useAIAssistant] response ignored without active round', {
                    requestId: responseRequestId || undefined,
                    sessionKey: normalized.session_key || undefined,
                    hasRenderablePayload: hasRenderableTerminalPayload(normalized),
                });
                logAIAssistantDiagnostic({
                    event: 'response_ignored_no_round',
                    requestId: responseRequestId || '',
                    sessionKey: normalized.session_key || '',
                    hasRenderablePayload: hasRenderableTerminalPayload(normalized),
                });
                return;
            }
            if (!hasRoundMessage(latestMessagesRef.current, round.assistantMessageId, responseRequestId)) {
                if (!hasRenderableTerminalPayload(normalized)) {
                    stopResponseTimeout(responseRequestId);
                    resetStreamTokenBuffer(responseRequestId);
                    forgetInFlightRound(responseRequestId);
                    return;
                }
                const ownerSessionKey = normalizeRuntimeSessionKey(round.sessionKey || normalized.session_key || 'desktop-user');
                console.warn('[useAIAssistant] response round message missing; recreating terminal placeholder', {
                    requestId: responseRequestId || undefined,
                    roundSessionKey: round.sessionKey || undefined,
                    eventSessionKey: normalized.session_key || undefined,
                    active: activeMatch,
                });
                logAIAssistantDiagnostic({
                    event: 'response_recreate_placeholder',
                    requestId: responseRequestId || '',
                    roundSessionKey: round.sessionKey || '',
                    eventSessionKey: normalized.session_key || '',
                    active: activeMatch,
                });
                setMessages(prev => appendAssistantPlaceholder(prev, round.assistantMessageId || nextId(), responseRequestId || round.requestId, ownerSessionKey));
            }
            flushStreamTokenBuffer(responseRequestId);
            const assistantMessageId = round.assistantMessageId || '';
            const effectiveRequestId = responseRequestId || round.requestId;
            const userText = round.userText || '';
            console.info('[useAIAssistant] response matched foreground round', {
                requestId: effectiveRequestId,
                sessionKey: round.sessionKey || 'desktop-user',
                eventSessionKey,
                active: activeMatch,
            });
            logAIAssistantDiagnostic({
                event: 'response_matched_round',
                requestId: effectiveRequestId,
                sessionKey: round.sessionKey || 'desktop-user',
                eventSessionKey,
                active: activeMatch,
            });

            // Handle explicit history reset (/new, /reset, /clear)
            const canMutateLocalHistory = activeMatch && isLocalRoundSession(round);
            const explicitReset = canMutateLocalHistory && normalized.clear_ui && isExplicitHistoryResetCommand(userText);
            if (explicitReset) {
                // Clear messages completely (same as the clear button) so the
                // welcome/guide page reappears after /clear, /new, /reset.
                const nextBoundary = AI_ASSISTANT_CONTEXT_BOUNDARY_END;
                if (persistTimerRef.current) {
                    clearTimeout(persistTimerRef.current);
                    persistTimerRef.current = null;
                }
                latestMessagesRef.current = [];
                lastPersistedPayloadRef.current = null;
                contextBoundaryMessageIDRef.current = nextBoundary;
                persistContextBoundaryMessageID(nextBoundary);
                persistUIState([], latestPromptsRef.current, nextBoundary);
                legacyClearAIAssistantUIState();
                clearTransientProgress();
                setMessages([]);
                // Dismiss any visible agent view (including workflow forms).
                forceResetAgentViewForActiveSession();
            } else {
                setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, normalized, preferences));
                if (canMutateLocalHistory && normalized.clear_ui) {
                    contextBoundaryMessageIDRef.current = '';
                    persistContextBoundaryMessageID('');
                    clearTransientProgress();
                }
            }
            clearPendingTaskForRequest(effectiveRequestId);
            stopResponseTimeout(effectiveRequestId);
            resetStreamTokenBuffer(effectiveRequestId);
            clearTransientProgress(round.sessionKey || 'desktop-user');
            forgetInFlightRound(effectiveRequestId);
            if (activeMatch) {
                finalizeRound(round.generation);
            }
        };
        const off = subscribeEvent(RESPONSE_EVENT, handler);
        return () => { off(); };
    }, [clearPendingTaskForRequest, clearTransientProgress, finalizeRound, flushStreamTokenBuffer, forgetInFlightRound, preferences, resetStreamTokenBuffer, stopResponseTimeout]);

    const sendMessageNow = useCallback(async (text: string, options?: SendMessageOptions): Promise<boolean> => {
        // Callers (e.g. handleSend in AIAssistantPanel) are responsible for
        // embedding file paths into `text` via buildOutgoingMessageMulti before calling here.
        const outgoingText = text.trim();
        if (outgoingText === "") return false;
        const sessionKey = deriveSendSessionKey(options);
        const currentRoundBeforeWait = activeRoundRef.current;
        const currentSessionKeyBeforeWait = currentRoundBeforeWait.sessionKey || 'desktop-user';
        if (currentRoundBeforeWait.phase !== 'idle' && currentSessionKeyBeforeWait !== sessionKey) {
            console.info('[useAIAssistant] starting independent foreground send while another session is active', {
                sessionKey,
                activeSessionKey: currentSessionKeyBeforeWait,
                activeRequestId: currentRoundBeforeWait.requestId,
                activePhase: currentRoundBeforeWait.phase,
                projectPath: options?.project_path || '',
            });
        }
        await waitForForegroundIdle(sessionKey);

        const generation = activeRoundRef.current.generation + 1;
        const assistantMessageId = nextId();
        const requestId = createForegroundRequestID();
        const userMsg: ChatMessage = {
            id: nextId(),
            role: 'user',
            content: options?.displayText || outgoingText,
            sessionKey,
            tabId: options?.tabId,
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMessageId,
            role: 'assistant',
            content: '',
            requestId,
            sessionKey,
            tabId: options?.tabId,
            timestamp: Date.now(),
        };
        const approvalMessage = options?.markConfirmationRunning === true;
        const contextStartIndex = resolveContextStartIndex(latestMessagesRef.current, contextBoundaryMessageIDRef.current);
        const recentMessages = Array.isArray(options?.recentMessages)
            ? options.recentMessages
            : buildClientContextMessages(latestMessagesRef.current, contextStartIndex);

        clearTransientProgress(sessionKey);
        const nextRound = {
            generation,
            phase: 'requesting',
            assistantMessageId,
            requestId,
            sessionKey,
            userText: outgoingText,
        } as ActiveRound;
        rememberInFlightRound(nextRound);
        setRoundState(nextRound);
        emitPetStateForAssistant('thinking', 'ai:send', 15000);
        setMessages(prev => {
            const nextMessages = approvalMessage ? markLatestConfirmationAsRunning(prev) : prev;
            return [...nextMessages, userMsg, placeholderMsg];
        });

        // Sliding-window activity timeout: reset whenever the backend shows signs
        // of life (token, progress, new-round). Only fires when the backend is
        // completely silent for the configured timeout window - not from the initial send time.
        const responseTimeoutController = startResponseTimeout({ generation, assistantMessageId, requestId, source: 'ai' });
        const clearResponseTimeout = () => {
            if (responseTimeoutControllersByRequestRef.current.get(requestId) !== responseTimeoutController) return;
            responseTimeoutControllersByRequestRef.current.delete(requestId);
            responseTimeoutController.stop();
        };

        let deferredAsync = false;
        let cleanupRequestId = requestId;
        try {
            // All messages go through the async path - the binding returns
            // immediately with {deferred: true}. The actual response arrives
            // via the "ai-assistant-response" event and is processed by the
            // event handler above.
            const rawResponse = await SendAIAssistantMessage(
                buildAIAssistantSendPayload(outgoingText, requestId, recentMessages, { ...options, lang: options?.lang || uiLang })
            ) as AIAssistantSendResult;
            const response = normalizeSendResponse(rawResponse, preferences.showTraceEntry);
            const responseRequestId = resolveSendRequestID(response);
            const effectiveRequestId = responseRequestId || requestId;
            cleanupRequestId = effectiveRequestId;
            const currentRound = activeRoundRef.current;
            const activeStillMatches = currentRound.generation === generation && currentRound.phase !== 'idle';
            // Update requestId if the backend assigned a different one.
            if (responseRequestId && responseRequestId !== requestId) {
                const reassignedRound = {
                    generation,
                    phase: activeStillMatches ? activeRoundRef.current.phase : 'requesting',
                    assistantMessageId,
                    requestId: responseRequestId,
                    sessionKey,
                    userText: outgoingText,
                } as ActiveRound;
                replaceInFlightRoundRequestId(requestId, reassignedRound);
                if (response.deferred) {
                    startResponseTimeout({ generation, assistantMessageId, requestId: responseRequestId, source: 'ai' });
                }
                if (activeStillMatches) setRoundState(reassignedRound);
            }
            if (!activeStillMatches) {
                const detachedRound = inFlightRoundsByRequestRef.current.get(effectiveRequestId);
                if (!detachedRound || !hasRoundMessage(latestMessagesRef.current, assistantMessageId, effectiveRequestId)) {
                    stopResponseTimeout(effectiveRequestId);
                    resetStreamTokenBuffer(effectiveRequestId);
                    forgetInFlightRound(effectiveRequestId);
                    return true;
                }
                if (response.deferred) {
                    deferredAsync = true;
                    return true;
                }
                setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, response, preferences));
                stopResponseTimeout(effectiveRequestId);
                resetStreamTokenBuffer(effectiveRequestId);
                clearPendingTaskForRequest(effectiveRequestId);
                clearTransientProgress(detachedRound.sessionKey || 'desktop-user');
                forgetInFlightRound(effectiveRequestId);
                return true;
            }
            if (response.deferred) {
                // Async mode: response will arrive via event. If there's a
                // live run_id/job_id, track that remote coding session. When
                // the backend reports deferred but the session already ended,
                // finalize this foreground round instead of leaving the input locked.
                if (response.run_id || response.job_id) {
                    const pending = await resolvePendingAITask(effectiveRequestId, response);
                    if (pending) {
                        setPendingTaskState({ ...pending, sessionKey });
                        deferredAsync = true;
                    } else {
                        clearPendingTaskForRequest(effectiveRequestId);
                        deferredAsync = false;
                    }
                } else {
                    deferredAsync = true;
                }
            } else {
                // Synchronous response (e.g. handleAgentViewControlMessage,
                // TryHandlePassthroughSlashCommand). Process immediately.
                clearResponseTimeout();
                flushStreamTokenBuffer(effectiveRequestId);
                const canMutateLocalHistory = isLocalRoundSession({ sessionKey });
                const explicitReset = canMutateLocalHistory && response.clear_ui && isExplicitHistoryResetCommand(outgoingText);
                if (explicitReset) {
                    // Clear messages completely (same as the clear button) so the
                    // welcome/guide page reappears after /clear, /new, /reset.
                    const nextBoundary = AI_ASSISTANT_CONTEXT_BOUNDARY_END;
                    if (persistTimerRef.current) {
                        clearTimeout(persistTimerRef.current);
                        persistTimerRef.current = null;
                    }
                    latestMessagesRef.current = [];
                    lastPersistedPayloadRef.current = null;
                    contextBoundaryMessageIDRef.current = nextBoundary;
                    persistContextBoundaryMessageID(nextBoundary);
                    persistUIState([], latestPromptsRef.current, nextBoundary);
                    legacyClearAIAssistantUIState();
                    clearTransientProgress();
                    setMessages([]);
                    // Dismiss any visible agent view (including workflow forms).
                    forceResetAgentViewForActiveSession();
                } else {
                    setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, response, preferences));
                    if (canMutateLocalHistory && response.clear_ui) {
                        contextBoundaryMessageIDRef.current = userMsg.id;
                        persistContextBoundaryMessageID(userMsg.id);
                        clearTransientProgress();
                    }
                }
                clearPendingTaskForRequest(effectiveRequestId);
            }
        } catch (err: any) {
            clearResponseTimeout();
            resetStreamTokenBuffer(requestId);
            const currentRound = activeRoundRef.current;
            if (currentRound.generation !== generation || currentRound.phase === 'idle') {
                forgetInFlightRound(requestId);
                return true;
            }
            setMessages(prev => resolveSendResult(prev, assistantMessageId, requestId, null, preferences, err?.message || String(err)));
            clearPendingTaskForRequest(requestId);
        } finally {
            if (!deferredAsync) {
                clearResponseTimeout();
                resetStreamTokenBuffer(cleanupRequestId);
                forgetInFlightRound(cleanupRequestId);
                finalizeRound(generation);
            }
        }
        return true;
    }, [clearPendingTaskForRequest, clearTransientProgress, emitPetStateForAssistant, finalizeRound, flushStreamTokenBuffer, forgetInFlightRound, preferences, rememberInFlightRound, replaceInFlightRoundRequestId, resetActiveRound, resetStreamTokenBuffer, setRoundState, startResponseTimeout, stopResponseTimeout, uiLang, waitForForegroundIdle]);

    const optionsForActiveSession = useCallback((options?: SendMessageOptions): SendMessageOptions | undefined => {
        const explicitProjectPath = typeof options?.project_path === 'string' ? options.project_path.trim() : '';
        if (explicitProjectPath) return options;
        const isSessionScopedAction = !!options && (
            options.uiAction === true
            || !!options.resumeSlotID
            || options.startNewTask === true
            || !!options.dismissSlotID
            || !!options.resumeSessionID
            || !!options.dismissRecoverableSessionID
            || options.markConfirmationRunning === true
        );
        if (!isSessionScopedAction) return options;
        const activeProjectPath = projectPathFromSessionKey(activeSessionKeyForEvents());
        if (!activeProjectPath) return options;
        return { ...(options || {}), project_path: activeProjectPath };
    }, [activeSessionKeyForEvents]);

    const sendMessage = useCallback((text: string, options?: SendMessageOptions): Promise<boolean> => {
        const outgoingText = text.trim();
        if (outgoingText === "") return Promise.resolve(false);
        const routedOptions = optionsForActiveSession(options);
        const sessionKey = deriveSendSessionKey(routedOptions);
        const previousTail = foregroundSendTailsBySessionRef.current.get(sessionKey) || Promise.resolve(true);
        const run = previousTail
            .catch(() => true)
            .then(() => sendMessageNow(outgoingText, routedOptions));
        foregroundSendTailsBySessionRef.current.set(sessionKey, run.catch(() => false));
        foregroundSendTailRef.current = run.catch(() => false);
        return run;
    }, [optionsForActiveSession, sendMessageNow]);

    const sendMessageInBackground = useCallback(async (text: string) => {
        // Callers are responsible for embedding file paths into `text` before calling here.
        const outgoingText = text.trim();
        const sessionKey = 'desktop-user';
        if (outgoingText === "" || !isSessionIdle(sessionKey)) return;

        const generation = activeRoundRef.current.generation + 1;
        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId: null,
            requestId: '',
            sessionKey,
        });
        emitPetStateForAssistant('thinking', 'ai:background-send', 15000);

        try {
            const response = await StartAIAssistantBackgroundTask({
                text: outgoingText,
                force_background: true,
            }) as StartAIAssistantBackgroundTaskResult;
            const sessionID = resolveBackgroundSessionID(response);
            if (!response?.accepted || !sessionID) {
                throw new Error(response?.error || 'failed to start background task');
            }
            const launchResult: AIAssistantBackgroundLaunchResult = {
                sessionID,
                jobID: resolveBackgroundJobID(response) || undefined,
                runID: resolveBackgroundRunID(response) || undefined,
            };
            setMessages(prev => appendBackgroundLaunchMessages(prev, outgoingText, launchResult, sessionKey));
            await refreshSessionsOnlyRef.current?.();
        } catch (err: any) {
            setMessages(prev => [...prev, createUserMessage(outgoingText, sessionKey), createErrorMessage(err?.message || String(err), sessionKey)]);
        } finally {
            finalizeRound(generation);
        }
    }, [emitPetStateForAssistant, finalizeRound, isSessionIdle, setRoundState]);

    // sendBtwMessage sends a /btw side query via a dedicated backend binding.
    // Unlike sendMessage, this does NOT check activeRound.phase - it can run
    // while the main agent loop is active. It does NOT affect activeRound state,
    // so the main loop's streaming/progress continues uninterrupted.
    //
    // The result is appended as a pair of user+assistant messages to the chat.
    // Streaming tokens arrive on "ai-btw-token" events (separate from the main
    // "ai-assistant-token" channel).
    const sendBtwMessage = useCallback(async (query: string) => {
        const trimmedQuery = query.trim();
        const sessionKey = normalizeRuntimeSessionKey(activeSessionKeyForEvents() || 'desktop-user');
        if (!trimmedQuery) {
            // Show usage help as a local message - no backend call needed.
            setMessages(prev => [...prev,
                { id: nextId(), role: 'user' as const, content: '/btw', sessionKey, timestamp: Date.now() },
                { id: nextId(), role: 'assistant' as const, content: 'Usage: /btw <query>\n\nExamples:\n  /btw latest Go changes\n  /btw React 19 major changes\n  /btw what framework does this project use?', sessionKey, timestamp: Date.now() },
            ]);
            return;
        }

        const requestId = `btw-${Date.now()}`;
        const userMsgId = nextId();
        const assistantMsgId = nextId();

        // Add user message and placeholder immediately.
        const userMsg: ChatMessage = {
            id: userMsgId,
            role: 'user',
            content: '/btw ' + trimmedQuery,
            sessionKey,
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMsgId,
            role: 'assistant',
            content: '',
            requestId,
            sessionKey,
            timestamp: Date.now(),
        };
        setMessages(prev => [...prev, userMsg, placeholderMsg]);
        emitPetStateForAssistant('thinking', 'ai:btw-send', 10000);

        // Listen for streaming tokens on the /btw-specific channel.
        const tokenHandler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            if (event?.request_id !== requestId) return;
            const delta = event?.text || '';
            if (!delta) return;
            emitPetStateForAssistant('speaking', 'ai:btw-token', 1800);
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: m.content + delta } : m
            ));
        };
        const progressHandler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            if (event?.request_id !== requestId) return;
            // Progress is informational; the streaming tokens provide real-time feedback.
        };

        const offBtwToken = subscribeEvent("ai-btw-token", tokenHandler);
        const offBtwProgress = subscribeEvent("ai-btw-progress", progressHandler);

        try {
            const response = await SendBtwQuery(trimmedQuery, requestId) as any;
            const finalText = response?.text || response?.error || '查询失败';
            // Replace placeholder content with the final result.
            // If streaming already populated content, the final text is authoritative.
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: finalText } : m
            ));
        } catch (err: any) {
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: `ERROR /btw 查询失败: ${err?.message || String(err)}` } : m
            ));
        } finally {
            offBtwToken();
            offBtwProgress();
            emitPetStateForAssistant('idle', 'ai:btw-done');
        }
    }, [activeSessionKeyForEvents, emitPetStateForAssistant]);

    const browseFile = useCallback(async () => {
        const sessionKey = activeSelectedFilesSessionKeyRef.current;
        try {
            const selected = await SelectAIAssistantFiles();
            if (!selected || selected.length === 0) return; // User cancelled or no files selected
            const validPaths = selected.filter(p => p && p.trim());
            if (validPaths.length > 0) {
                addSelectedFiles(validPaths, sessionKey);
            }
        } catch (err) {
            console.error('Failed to select files:', err);
        }
    }, [addSelectedFiles]);

    const clearSelectedFile = useCallback(() => {
        setSelectedFiles([]);
    }, [setSelectedFiles]);

    const clearHistory = useCallback(async () => {
        stopAllResponseTimeouts();
        resetAllStreamTokenBuffers();
        resetActiveRound();
        setPendingTaskState(null);
        pendingTasksByRequestRef.current.clear();
        setPendingTaskVersion(version => version + 1);
        setSelectedFiles([]);
        // Unconditionally dismiss any visible agent view (including workflow forms)
        // so the right-side panel does not survive a session clear.
        // Only clear the active session's agentView — other tabs retain theirs.
        // Advance the lifecycle sequence so any in-flight stale events from
        // before the clear are rejected by acceptAgentViewSequence.
        forceResetAgentViewForActiveSession();
        if (persistTimerRef.current) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
        latestMessagesRef.current = [];
        lastPersistedPayloadRef.current = null;
        persistOnUnmountRef.current = false;
        foregroundSendTailRef.current = Promise.resolve(true);
        foregroundSendTailsBySessionRef.current.clear();
        inFlightRoundsByRequestRef.current.clear();
        contextBoundaryMessageIDRef.current = null;
        persistContextBoundaryMessageID(null);
        legacyClearAIAssistantUIState();
        setMessages([]);
        setDraftInputValue("");
        clearTransientProgress();
        latestNewsPayloadRef.current = '[]';
        scrollOnNextNewsRef.current = true;
        doFetchNews();
        try {
            await ClearAIAssistantUIState();
            // Use session-aware clear so that project tab workflows (V1/V2)
            // are properly cancelled. The backend resolves the session key to
            // the correct per-user owner ID and cancels the active agent loop,
            // clears conversation memory, and resets all session state including
            // active workflows.
            const sessionKey = activeSessionKeyForEvents() || 'desktop-user';
            await ClearAIAssistantHistoryForSession(sessionKey);
            const saved = await persistUIState([], latestPromptsRef.current, null);
            if (saved) {
                lastPersistedPayloadRef.current = serializePersistedMessages([]);
                lastPersistedPromptsPayloadRef.current = JSON.stringify(latestPromptsRef.current);
            }
        } catch (_) {
        } finally {
            persistOnUnmountRef.current = true;
        }
    }, [activeSessionKeyForEvents, clearTransientProgress, doFetchNews, persistUIState, resetActiveRound, resetAllStreamTokenBuffers, setSelectedFiles, stopAllResponseTimeouts]);

    const recordSubmittedPrompt = useCallback((prompt: string) => {
        setSubmittedPrompts(prev => appendSubmittedPrompt(prev, prompt));
    }, []);

    const executeAction = useCallback(async (command: string) => {
        // Handle critical-risk skill installation confirmation responses.
        const criticalConfirmMatch = command.match(/^__resolve_critical_confirm__\s+(\S+)\s+(confirm|reject)(?:\s+(\S+))?$/);
        if (criticalConfirmMatch) {
            const confirmID = criticalConfirmMatch[1] || '';
            const confirmed = criticalConfirmMatch[2] === 'confirm';
            const commandLang = criticalConfirmMatch[3] || '';
            // Remove buttons from the confirmation message immediately so the
            // user sees their click was registered. This is the standard UI
            // pattern for one-shot action buttons.
            setMessages(prev => prev.map(m => {
                if (!m.actions?.some(a => a.command.includes(confirmID))) return m;
                const feedbackLang = commandLang || inferCriticalConfirmLangFromMessage(m);
                return { ...m, actions: undefined, content: m.content + criticalConfirmFeedback(feedbackLang, confirmed) };
            }));
            try {
                await ResolveCriticalConfirm(confirmID, confirmed);
            } catch (err: any) {
                // Backend returned an error (e.g. confirmation expired).
                // Show the error to the user.
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err), activeSessionKeyForEvents() || 'desktop-user')]);
            }
            return;
        }
        const executionConfirmMatch = command.match(/^__confirm_execution__\s+(\S+)$/);
        if (executionConfirmMatch) {
            const action = findLatestConfirmationAction(latestMessagesRef.current, command);
            return sendMessage(command, {
                uiAction: true,
                displayText: localizedExecutionActionText(command, action?.label, uiLang),
                markConfirmationRunning: true,
            });
        }
        const executionCancelMatch = command.match(/^__cancel_execution__\s+(\S+)$/);
        if (executionCancelMatch) {
            const action = findLatestConfirmationAction(latestMessagesRef.current, command);
            return sendMessage(command, {
                uiAction: true,
                displayText: localizedExecutionActionText(command, action?.label, uiLang),
            });
        }
        const legacyConfirmationAction = findLatestConfirmationAction(latestMessagesRef.current, command);
        if (isConfirmationApprovalAction(legacyConfirmationAction)) {
            return sendMessage(command, { uiAction: true, displayText: legacyConfirmationAction?.label || command, markConfirmationRunning: true });
        }
        const traceMatch = command.match(/^__view_trace__\s+(\S+)$/);
        if (traceMatch) {
            const runID = traceMatch[1]?.trim() || '';
            if (!runID) return;
            try {
                const view = await GetAIAssistantTrace(runID) as AIAssistantTraceView;
                setMessages(prev => [...prev, createTraceMessage(buildTraceDetailMessage(view, runID), buildTraceDetailFields(view, runID), activeSessionKeyForEvents() || 'desktop-user')]);
            } catch (err: any) {
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err), activeSessionKeyForEvents() || 'desktop-user')]);
            }
            return;
        }
        const resumeMatch = command.match(/^__resume_unfinished__\s+(\S+)$/);
        if (resumeMatch) {
            setMessages(prev => removeActionCommandFromMessages(prev, command));
            const resumeText = localizeText(uiLang, "Continue previous unfinished task", "\u7ee7\u7eed\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u7e7c\u7e8c\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9");
            return sendMessage(resumeText, {
                resumeSlotID: resumeMatch[1]?.trim() || '',
                uiAction: true,
                displayText: resumeText,
            });
        }
        // Backward compat: __start_new_task__ is no longer emitted by the
        // backend (merged into __dismiss_unfinished__), but keep the handler
        // in case older backend versions are still in use.
        if (command === '__start_new_task__') {
            setMessages(prev => removeActionCommandFromMessages(prev, command));
            const startNewText = localizeText(uiLang, "Start a new task", "\u5f00\u59cb\u4e00\u4e2a\u65b0\u4efb\u52a1", "\u958b\u59cb\u4e00\u500b\u65b0\u4efb\u52d9");
            return sendMessage(startNewText, {
                startNewTask: true,
                uiAction: true,
                displayText: startNewText,
            });
        }
        const dismissMatch = command.match(/^__dismiss_unfinished__\s+(\S+)$/);
        if (dismissMatch) {
            setMessages(prev => removeActionCommandFromMessages(prev, command));
            const dismissText = localizeText(uiLang, "Dismiss previous unfinished task", "\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9");
            return sendMessage(dismissText, {
                dismissSlotID: dismissMatch[1]?.trim() || '',
                startNewTask: true,
                uiAction: true,
                displayText: dismissText,
            });
        }
        const resumeSessionMatch = command.match(/^__resume_session__\s+(\S+)$/);
        if (resumeSessionMatch) {
            setMessages(prev => removeActionCommandFromMessages(prev, command));
            const resumeSessionText = localizeText(uiLang, "Resume session", "\u6062\u590d\u4f1a\u8bdd", "\u6062\u5fa9\u6703\u8a71");
            return sendMessage(resumeSessionText, {
                resumeSessionID: resumeSessionMatch[1]?.trim() || '',
                uiAction: true,
                displayText: resumeSessionText,
            });
        }
        const dismissSessionMatch = command.match(/^__dismiss_recoverable_session__\s+(\S+)$/);
        if (dismissSessionMatch) {
            setMessages(prev => removeActionCommandFromMessages(prev, command));
            const dismissSessionText = localizeText(uiLang, "Dismiss session", "\u5ffd\u7565\u4f1a\u8bdd", "\u5ffd\u7565\u6703\u8a71");
            return sendMessage(dismissSessionText, {
                dismissRecoverableSessionID: dismissSessionMatch[1]?.trim() || '',
                uiAction: true,
                displayText: dismissSessionText,
            });
        }
        const workflowReviewMatch = command.match(/^__wf_review__\s+(\S+)$/);
        if (workflowReviewMatch) {
            const action = workflowReviewMatch[1];

            // "补充修改意见": pure frontend action — focus input box, don't send message
            if (action === 'supplement_focus') {
                setMessages(prev => disableActionsForCommand(prev, command));
                return;
            }

            // "中止": requires user confirmation before sending
            if (action === 'abort') {
                const confirmMsg = localizeText(uiLang,
                    "Are you sure you want to abort the current workflow?\n\nAll progress will be lost and you'll need to start over.",
                    "\u786e\u5b9a\u8981\u4e2d\u6b62\u5f53\u524d\u5de5\u4f5c\u6d41\u5417\uff1f\n\n\u4e2d\u6b62\u540e\u5f53\u524d\u8fdb\u5ea6\u5c06\u88ab\u6e05\u9664\uff0c\u65e0\u6cd5\u7ee7\u7eed\uff0c\u53ea\u80fd\u91cd\u65b0\u53d1\u8d77\u3002",
                    "\u78ba\u5b9a\u8981\u4e2d\u6b62\u7576\u524d\u5de5\u4f5c\u6d41\u55ce\uff1f\n\n\u4e2d\u6b62\u5f8c\u7576\u524d\u9032\u5ea6\u5c07\u88ab\u6e05\u9664\uff0c\u7121\u6cd5\u7e7c\u7e8c\uff0c\u53ea\u80fd\u91cd\u65b0\u767c\u8d77\u3002",
                );
                if (!window.confirm(confirmMsg)) {
                    return; // user cancelled — buttons stay clickable
                }
                setMessages(prev => disableActionsForCommand(prev, command));
                const displayText = localizeText(uiLang, "\ud83d\udeab Abort workflow", "\ud83d\udeab \u4e2d\u6b62\u5de5\u4f5c\u6d41", "\ud83d\udeab \u4e2d\u6b62\u5de5\u4f5c\u6d41");
                return sendMessage(command, { uiAction: true, displayText });
            }

            // "confirm": send to backend for fast-path processing
            const displayLabels: Record<string, string> = {
                confirm: localizeText(uiLang, "\u2705 Confirm and proceed", "\u2705 \u786e\u8ba4\u5e76\u63a8\u8fdb", "\u2705 \u78ba\u8a8d\u4e26\u63a8\u9032"),
            };
            setMessages(prev => disableActionsForCommand(prev, command));
            return sendMessage(command, { uiAction: true, displayText: displayLabels[action] || command });
        }
        const workflowChoiceMatch = command.match(/^__workflow_choice__\s+(complex|simple|skip|direct|alt_\S+)\s+(\S+)$/);
        if (workflowChoiceMatch) {
            const choice = workflowChoiceMatch[1];
            const choiceLabels: Record<string, string> = {
                complex: localizeText(uiLang, "Enter workflow", "\u8fdb\u5165\u5de5\u4f5c\u6d41", "\u9032\u5165\u5de5\u4f5c\u6d41"),
                simple: localizeText(uiLang, "Simple coding", "\u7b80\u5355\u7f16\u7a0b", "\u7c21\u55ae\u7de8\u7a0b"),
                skip: localizeText(uiLang, "Direct processing", "\u76f4\u63a5\u5904\u7406", "\u76f4\u63a5\u8655\u7406"),
                direct: localizeText(uiLang, "Direct processing", "\u76f4\u63a5\u5904\u7406", "\u76f4\u63a5\u8655\u7406"),
            };
            // alt_<type> choices from disambiguation panel: display as "进入工作流"
            const displayText = choice.startsWith('alt_')
                ? localizeText(uiLang, "Enter workflow", "\u8fdb\u5165\u5de5\u4f5c\u6d41", "\u9032\u5165\u5de5\u4f5c\u6d41")
                : (choiceLabels[choice] || command);
            // Persistently disable buttons so they stay grey across re-renders.
            // Remove ALL actions from the message (not just the clicked one)
            // because these are mutually exclusive choice buttons.
            setMessages(prev => disableActionsForCommand(prev, command));
            return sendMessage(command, { uiAction: true, displayText });
        }
        // Persistently disable action buttons after click. Component-local
        // firedIndex state may be lost on re-render; removing actions from the
        // message ensures buttons remain disabled even if React re-mounts the
        // ActionButtons component.
        // Remove ALL actions from the containing message — action buttons are
        // one-shot (mutually exclusive choices or single confirm/cancel).
        setMessages(prev => disableActionsForCommand(prev, command));
        return sendMessage(command, { uiAction: true });
    }, [activeSessionKeyForEvents, sendMessage, uiLang]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            const currentRound = activeRoundRef.current;
            const activeSessionKey = activeSessionKeyForEvents();
            const progressText = event.text || (typeof payload === 'string' ? payload : '');
            if (!progressText) return;
            const progressEvent = event.text === progressText ? event : { ...event, text: progressText };
            const detachedRound = event.request_id ? inFlightRoundsByRequestRef.current.get(event.request_id) : undefined;
            const matchesActiveRound = matchesActiveProgressRequest(currentRound, progressEvent);
            const matchesDetachedRound = !!detachedRound && !matchesActiveRound && matchesActiveProgressRequest(detachedRound, progressEvent);
            const eventSessionKey = normalizeRuntimeSessionKey(event.session_key || '');
            const hasExplicitEventSession = !!String(event.session_key || '').trim();
            const eventTargetsActiveRound = hasExplicitEventSession && normalizeRuntimeSessionKey(currentRound.sessionKey || 'desktop-user') === eventSessionKey;
            const sessionRound = hasExplicitEventSession
                ? (eventTargetsActiveRound ? null : findInFlightRoundBySession(eventSessionKey))
                : null;
            const matchesSessionRound = !!sessionRound && sessionRound.phase !== 'idle' && !event.request_id;
            const matchesPendingSession = hasExplicitEventSession && !event.request_id && !eventTargetsActiveRound && !!findPendingTaskBySession(eventSessionKey, currentRound.sessionKey || 'desktop-user');
            if (!matchesDetachedRound && !matchesSessionRound && !matchesPendingSession && !matchesActiveProgressActivity(currentRound, progressEvent, activeSessionKey)) {
                return;
            }
            // Reset the sliding-window activity timeout - backend is alive.
            if (matchesDetachedRound) resetResponseTimeoutForRound(detachedRound);
            else if (matchesSessionRound) resetResponseTimeoutForRound(sessionRound);
            else resetResponseTimeoutForActiveRound();
            if (shouldHideProgressText(progressText)) {
                return;
            }
            const progressSessionKey = normalizeRuntimeSessionKey(
                (matchesDetachedRound ? detachedRound?.sessionKey : undefined)
                || (matchesSessionRound ? sessionRound?.sessionKey : undefined)
                || (matchesPendingSession ? eventSessionKey : undefined)
                || currentRound.sessionKey
                || activeSessionKey,
            );
            appendProgressForSession(progressSessionKey, progressText);
        };
        const offProgress = subscribeEvent(PROGRESS_EVENT, handler);
        return () => {
            offProgress();
        };
    }, [activeSessionKeyForEvents, appendProgressForSession, findInFlightRoundBySession, findPendingTaskBySession, resetResponseTimeoutForActiveRound, resetResponseTimeoutForRound]);

    useEffect(() => {
        // agent-view:lifecycle is the single source of truth for view state.
        // The legacy "agent-view" / "agent-view-clear" events are ignored when
        // lifecycle events are available, preventing double-setState on the same
        // render cycle. Legacy events are retained only for external consumers
        // that haven't migrated to the lifecycle protocol.
        let lifecycleActive = false;
        const acceptAgentViewSequence = (sessionKey: string, payload: unknown): boolean => {
            const normalizedSessionKey = normalizeRuntimeSessionKey(sessionKey);
            const seq = eventSequenceValue(payload);
            if (typeof seq !== 'number' || seq <= 0) return true;
            const currentSeq = agentViewLifecycleSeqBySessionRef.current.get(normalizedSessionKey) || 0;
            if (seq < currentSeq) {
                // Detect backend restart epoch jump. After restart, the backend
                // initializes seq from Unix seconds (~1.7B+). Normal stale events
                // have a gap of at most a few dozen (events emitted in one interaction).
                // A gap > 100 between cached seq and incoming seq is impossible in
                // normal operation — it indicates the sequences belong to different
                // epoch series (old frontend cache vs new backend epoch, or vice versa).
                const gap = currentSeq - seq;
                if (gap > 100) {
                    // Large gap — backend restarted with a new epoch. Reset cache.
                    logAIAssistantDiagnostic({ event: 'agentview_seq_epoch_reset', seq, currentSeq, gap, sessionKey: normalizedSessionKey });
                    agentViewLifecycleSeqBySessionRef.current.set(normalizedSessionKey, seq);
                    return true;
                }
                logAIAssistantDiagnostic({ event: 'agentview_seq_rejected_stale', seq, currentSeq, gap, sessionKey: normalizedSessionKey });
                return false;
            }
            agentViewLifecycleSeqBySessionRef.current.set(normalizedSessionKey, seq);
            return true;
        };
        const offView = subscribeEvent(AGENT_VIEW_EVENT, (payload: unknown) => {
            if (lifecycleActive) return; // lifecycle is authoritative
            const nextView = normalizeAgentView(payload);
            const sessionKey = agentViewSessionKey(nextView, activeSessionKeyForEvents());
            if (!acceptAgentViewSequence(sessionKey, payload)) return;
            adoptAgentViewSessionIfUnbound(sessionKey);
            if (nextView) {
                if (nextView.id?.startsWith('workflow:form:')) updateWorkflowFormViewForSession(sessionKey, nextView);
                else updateVisibleAgentViewForSession(sessionKey, nextView);
            }
        });
        const offClear = subscribeEvent(AGENT_VIEW_CLEAR_EVENT, (payload: unknown) => {
            if (lifecycleActive) return;
            const data = payload && typeof payload === 'object' ? payload as Record<string, unknown> : {};
            const event: AgentViewLifecyclePayload = {
                action: 'dismiss',
                view_id: typeof data.view_id === 'string' ? data.view_id : undefined,
                workflow_id: typeof data.workflow_id === 'string' ? data.workflow_id : undefined,
                workflow_phase: typeof data.workflow_phase === 'string' ? data.workflow_phase : undefined,
                workflow_user_id: typeof data.workflow_user_id === 'string' ? data.workflow_user_id : undefined,
            };
            const sessionKey = agentViewLifecycleSessionKey(event, activeSessionKeyForEvents());
            if (!acceptAgentViewSequence(sessionKey, payload)) return;
            adoptAgentViewSessionIfUnbound(sessionKey);
            updateVisibleAgentViewForSession(sessionKey, current => agentViewMatchesLifecycle(current, event) ? null : current);
            clearWorkflowFormAliasForView(event.view_id, sessionKey);
        });
        const offLifecycle = subscribeEvent(AGENT_VIEW_LIFECYCLE_EVENT, (payload: unknown) => {
            const event = normalizeAgentViewLifecycle(payload);
            if (!event) return;
            const sessionKey = agentViewLifecycleSessionKey(event, activeSessionKeyForEvents());
            if (!acceptAgentViewSequence(sessionKey, payload)) return;
            adoptAgentViewSessionIfUnbound(sessionKey);
            lifecycleActive = true;
            switch (event.action) {
                case "open":
                case "update":
                    if (event.view) {
                        if (event.view.id?.startsWith('workflow:form:')) updateWorkflowFormViewForSession(sessionKey, event.view);
                        else updateVisibleAgentViewForSession(sessionKey, event.view);
                    }
                    break;
                case "dismiss":
                    updateVisibleAgentViewForSession(sessionKey, current => agentViewMatchesLifecycle(current, event) ? null : current);
                    clearWorkflowFormAliasForView(event.view_id, sessionKey);
                    break;
                case "complete":
                    if (event.view) {
                        if (event.view.id?.startsWith('workflow:form:')) updateWorkflowFormViewForSession(sessionKey, event.view);
                        else updateVisibleAgentViewForSession(sessionKey, event.view);
                    } else {
                        updateVisibleAgentViewForSession(sessionKey, current => agentViewMatchesLifecycle(current, event) ? null : current);
                        clearWorkflowFormAliasForView(event.view_id, sessionKey);
                    }
                    break;
                case "error":
                    if (event.error) {
                        setMessages(prev => [...prev, createErrorMessage(event.error || "Task panel error", sessionKey)]);
                    }
                    break;
                case "submit":
                    break;
            }
        });
        return () => {
            offView();
            offClear();
            offLifecycle();
        };
    }, [activeSessionKeyForEvents, adoptAgentViewSessionIfUnbound, clearWorkflowFormAliasForView, updateVisibleAgentViewForSession, updateWorkflowFormViewForSession]);

    // Listen for critical-risk skill installation confirmation events from the backend.
    useEffect(() => {
        const handler = (payload: unknown) => {
            if (!payload || typeof payload !== 'object') return;
            const data = payload as Record<string, unknown>;
            const confirmID = typeof data.confirm_id === 'string' ? data.confirm_id : '';
            const summary = typeof data.summary === 'string' ? data.summary : '';
            const eventLang = typeof data.lang === 'string' ? data.lang : '';
            const normalizedEventLang = eventLang.trim().toLowerCase();
            const actionPayload = Array.isArray(data.actions) ? data.actions as Array<Record<string, unknown>> : [];
            const fallbackConfirmLabel = normalizedEventLang === 'en' ? 'Confirm install' : (normalizedEventLang.startsWith('zh-hant') || normalizedEventLang.startsWith('zh-tw') || normalizedEventLang.startsWith('zh-hk')) ? '確認安裝' : '确认安装';
            const fallbackRejectLabel = normalizedEventLang === 'en' ? 'Reject install' : (normalizedEventLang.startsWith('zh-hant') || normalizedEventLang.startsWith('zh-tw') || normalizedEventLang.startsWith('zh-hk')) ? '拒絕安裝' : '拒绝安装';
            const confirmLabel = typeof actionPayload[0]?.label === 'string' ? actionPayload[0].label : fallbackConfirmLabel;
            const rejectLabel = typeof actionPayload[1]?.label === 'string' ? actionPayload[1].label : fallbackRejectLabel;
            if (!confirmID) return;
            const msg: ChatMessage = {
                id: nextId(),
                role: 'assistant',
                content: summary,
                actions: [
                    { label: confirmLabel, command: `__resolve_critical_confirm__ ${confirmID} confirm ${eventLang || 'zh-Hans'}`, style: 'default' as const },
                    { label: rejectLabel, command: `__resolve_critical_confirm__ ${confirmID} reject ${eventLang || 'zh-Hans'}`, style: 'danger' as const },
                ],
                sessionKey: 'desktop-user',
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, msg]);
        };
        const offCriticalConfirm = subscribeEvent('critical-risk-confirm', handler);
        return () => {
            offCriticalConfirm();
        };
    }, []);

    // Listen for skill installation result events (success/failure feedback after
    // the user confirms or the async install completes).
    useEffect(() => {
        const handler = (payload: unknown) => {
            if (!payload || typeof payload !== 'object') return;
            const data = payload as Record<string, unknown>;
            const message = typeof data.message === 'string' ? data.message : '';
            if (!message) return;
            const msg: ChatMessage = {
                id: nextId(),
                role: 'assistant',
                content: message,
                sessionKey: 'desktop-user',
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, msg]);
        };
        const offSkillResult = subscribeEvent('skill-install-result', handler);
        return () => {
            offSkillResult();
        };
    }, []);

    const cancelSession = useCallback(async (): Promise<CancelAIAssistantResult> => {
        const sessionKeyAtCancel = activeSessionKeyForEvents() || 'desktop-user';
        const activeRoundAtCancel = activeRoundRef.current;
        const activeRoundSessionKey = activeRoundAtCancel.sessionKey || 'desktop-user';
        const detachedRoundAtCancel = activeRoundSessionKey === sessionKeyAtCancel
            ? null
            : findInFlightRoundBySession(sessionKeyAtCancel);
        const canceledRound = detachedRoundAtCancel || activeRoundAtCancel;
        const cancelingDetachedRound = !!detachedRoundAtCancel;
        const pendingTaskAtCancel = findPendingTaskBySession(sessionKeyAtCancel, activeRoundSessionKey);
        const nextGeneration = canceledRound.generation + 1;
        if (!canceledRound.assistantMessageId && isRoundIdle(canceledRound) && !pendingTaskAtCancel) {
            return { canceledText: "" };
        }
        console.info('[useAIAssistant] cancel foreground session', {
            sessionKey: sessionKeyAtCancel || 'desktop-user',
            requestId: canceledRound.requestId,
            detached: cancelingDetachedRound,
            hasPendingTask: !!pendingTaskAtCancel,
        });
        if (canceledRound.assistantMessageId) {
            flushStreamTokenBuffer(canceledRound.requestId);
            resetStreamTokenBuffer(canceledRound.requestId);
            setMessages(prev => markRoundCancelled(prev, canceledRound.assistantMessageId, canceledRound.requestId));
        }
        stopResponseTimeout(canceledRound.requestId);
        // Do NOT resetActiveRound here — keep the spinner/lock visible until
        // the backend confirms the loop has fully exited (state.mu released).
        // Otherwise the user sees an unlocked input box but state.mu is still
        // held by the dying goroutine, causing "系统正在恢复中" on next send.
        forgetInFlightRound(canceledRound.requestId);
        if (pendingTaskAtCancel) {
            clearPendingTaskForRequest(pendingTaskAtCancel.requestId);
        }
        emitPetStateForAssistant('idle', 'ai:cancel');
        const cancelBackend = (async (): Promise<CancelAIAssistantResult> => {
            try {
                if (pendingTaskAtCancel?.sessionID) {
                    await CancelAIAssistantTask(pendingTaskAtCancel.sessionID);
                    return { canceledText: "" };
                }
                if (!canceledRound.assistantMessageId) {
                    const lastSessionMessage = [...latestMessagesRef.current]
                        .reverse()
                        .find((message) => message.role === 'system' && /session_id:\s*/i.test(message.content));
                    const sessionIDMatch = lastSessionMessage?.content.match(/session_id:\s*(\S+)/i);
                    const sessionID = sessionIDMatch?.[1]?.trim() || "";
                    if (sessionID) {
                        await CancelAIAssistantTask(sessionID);
                        return { canceledText: "" };
                    }
                }
                return {
                    canceledText: (await CancelAIAssistantSessionForSession(sessionKeyAtCancel || 'desktop-user')) || "",
                };
            } catch {
                return { canceledText: "" };
            }
        })();
        const cancelTail = cancelBackend.then(() => true).catch(() => true);
        foregroundSendTailRef.current = cancelTail;
        foregroundSendTailsBySessionRef.current.set(sessionKeyAtCancel || 'desktop-user', cancelTail);
        const result = await cancelBackend;
        // Backend confirmed loop exit — NOW it's safe to reset the round
        // (stop spinner, unlock input box). The state.mu is released.
        if (!cancelingDetachedRound) {
            resetActiveRound(nextGeneration);
            clearTransientProgress();
        }
        return result;
    }, [activeSessionKeyForEvents, clearPendingTaskForRequest, clearTransientProgress, emitPetStateForAssistant, findInFlightRoundBySession, findPendingTaskBySession, flushStreamTokenBuffer, forgetInFlightRound, resetActiveRound, resetStreamTokenBuffer, stopResponseTimeout]);

    // injectSupplementary sends a supplementary message into the running
    // agent loop without cancelling it. Returns true if the injection was
    // accepted (a loop is active), false if no loop is running (caller
    // should fall back to normal sendMessage).
    // On success, a user message bubble is added to the chat so the user
    // sees visual confirmation of what was injected.
    const injectSupplementary = useCallback(async (text: string): Promise<boolean> => {
        try {
            const sessionKey = activeSessionKeyForEvents().trim() || 'desktop-user';
            const accepted = await InjectAIAssistantSupplementaryForSession(text, sessionKey);
            if (accepted) {
                // Show the injected text as a user message in the chat area
                // so the user has visual confirmation.
                setMessages(prev => [...prev, createUserMessage(text, sessionKey || 'desktop-user')]);
            }
            return accepted;
        } catch {
            return false;
        }
    }, [activeSessionKeyForEvents]);

    const guideLaunchReference = useCallback(async (text: string, sessionKey?: string): Promise<boolean> => {
        try {
            const normalizedSessionKey = sessionKey?.trim() || 'desktop-user';
            const accepted = await InjectAIAssistantGuideReferenceForSession(text, normalizedSessionKey);
            if (accepted && normalizedSessionKey === 'desktop-user') {
                setMessages(prev => [...prev, createSystemMessage("引导已注入下一轮：\n" + text, 'desktop-user')]);
            }
            return accepted;
        } catch {
            return false;
        }
    }, []);

    const submitAgentView = useCallback(async (viewId: string | undefined, data: Record<string, unknown>) => {
        const isWorkflowFormSubmit = typeof viewId === 'string' && viewId.startsWith('workflow:form:');
        const activeSubmitSessionKey = normalizeRuntimeSessionKey(activeSessionKeyForEvents() || 'desktop-user');
        const workflowOwnerFromPayload = typeof data._workflow_user_id === 'string' ? data._workflow_user_id : '';
        const visibleAgentView = isWorkflowFormSubmit ? (agentViewsBySessionRef.current.get(activeAgentViewSessionKeyRef.current) || null) : null;
        const workflowOwnerFromVisibleView = visibleAgentView?.id === viewId ? agentViewFieldValue(visibleAgentView, '_workflow_user_id') : '';
        const workflowSubmitSessionKey = normalizeRuntimeSessionKey(workflowOwnerFromPayload || workflowOwnerFromVisibleView || activeSubmitSessionKey);
        const submitRoundSessionKey = isWorkflowFormSubmit ? workflowSubmitSessionKey : activeSubmitSessionKey;
        if (!isWorkflowFormSubmit) updateVisibleAgentViewForSession(activeSubmitSessionKey, null);
        const payload = JSON.stringify({ view_id: viewId || "", data });
        let workflowSubmitRound: { generation: number; assistantMessageId: string; requestId: string } | null = null;
        const startAgentViewSubmitRound = (requestId: string) => {
            const generation = activeRoundRef.current.generation + 1;
            const assistantMessageId = nextId();
            const sessionKey = submitRoundSessionKey;
            clearTransientProgress(sessionKey);
            const nextRound = { generation, phase: 'requesting' as const, assistantMessageId, requestId, sessionKey, userText: '' };
            rememberInFlightRound(nextRound);
            setRoundState(nextRound);
            setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId, requestId, sessionKey));
            emitPetStateForAssistant('thinking', 'agent-view:submit', 15000);

            startResponseTimeout({ generation, assistantMessageId, requestId, source: 'agent-view' });
            return { generation, assistantMessageId, requestId };
        };
        try {
            if (isWorkflowFormSubmit) {
                workflowSubmitRound = startAgentViewSubmitRound(createForegroundRequestID());
            }
            const rawResponse = await SubmitAgentView({ view_id: viewId || "", data, request_id: workflowSubmitRound?.requestId || undefined }) as AIAssistantSendResult | null | undefined;
            const response = normalizeSendResponse(rawResponse, preferences.showTraceEntry);
            const workflowSubmitAccepted = isWorkflowFormSubmit && !response.error;
            if (workflowSubmitAccepted && !response.keep_panel) {
                updateVisibleAgentViewForSession(workflowSubmitSessionKey, current => current?.id === viewId ? null : current);
                clearWorkflowFormAliasForView(viewId, workflowSubmitSessionKey);
            }
            if (response?.deferred && !workflowSubmitRound) {
                const requestId = resolveSendRequestID(response) || createForegroundRequestID();
                startAgentViewSubmitRound(requestId);
            }
            if (workflowSubmitRound && !response?.deferred) {
                const round = workflowSubmitRound;
                stopResponseTimeout(round.requestId);
                flushStreamTokenBuffer(round.requestId);
                setMessages(prev => resolveSendResult(prev, round.assistantMessageId, round.requestId, response, preferences));
                forgetInFlightRound(round.requestId);
                if (activeRoundRef.current.requestId === round.requestId) {
                    resetActiveRound(round.generation);
                }
                emitPetStateForAssistant('idle', 'agent-view:done');
            }
            return;
        } catch (err: any) {
            if (workflowSubmitRound) {
                const round = workflowSubmitRound;
                stopResponseTimeout(round.requestId);
                resetStreamTokenBuffer(round.requestId);
                setMessages(prev => resolveSendResult(prev, round.assistantMessageId, round.requestId, null, preferences, err?.message || String(err)));
                forgetInFlightRound(round.requestId);
                if (activeRoundRef.current.requestId === round.requestId) {
                    resetActiveRound(round.generation);
                }
                emitPetStateForAssistant('idle', 'agent-view:error');
            }
            if (isWorkflowFormSubmit) return;
            // Older desktop bindings may not expose the structured task-panel API yet.
        }
        const message = `__agent_view_submit__ ${payload}`;
        const injected = await injectSupplementary(message);
        if (!injected) {
            await sendMessage(message, {
                uiAction: true,
                displayText: localizeText(uiLang, "Submit structured data", "\u63d0\u4ea4\u7ed3\u6784\u5316\u6570\u636e", "\u63d0\u4ea4\u7d50\u69cb\u5316\u8cc7\u6599"),
            });
        }
    }, [activeSessionKeyForEvents, clearTransientProgress, clearWorkflowFormAliasForView, emitPetStateForAssistant, flushStreamTokenBuffer, forgetInFlightRound, injectSupplementary, preferences, rememberInFlightRound, resetActiveRound, resetStreamTokenBuffer, sendMessage, setRoundState, startResponseTimeout, stopResponseTimeout, uiLang, updateVisibleAgentViewForSession]);

    const dismissAgentView = useCallback(async (viewId: string | undefined, data?: Record<string, unknown>, options?: { force?: boolean }) => {
        // force: unconditionally clear frontend UI (even for workflow forms that
        // normally wait for backend lifecycle confirmation), swallow backend errors,
        // and skip the legacy sendMessage fallback. Used by session clear / /new / /reset.
        const isWorkflowFormDismiss = typeof viewId === 'string' && viewId.startsWith('workflow:form:');
        const isCancelWorkflow = !!(data && data.__cancel_workflow);
        if (!isWorkflowFormDismiss || options?.force || isCancelWorkflow) {
            updateVisibleAgentViewForSession(activeSessionKeyForEvents() || 'desktop-user', null);
        }
        const payload = JSON.stringify({ view_id: viewId || "", data: data || {} });
        try {
            const result = await DismissAgentView({ view_id: viewId || "", data: data || {} });
            // When cancelling a workflow, show the confirmation message to the user.
            if (isCancelWorkflow && result?.text) {
                const sessionKey = activeSessionKeyForEvents() || 'desktop-user';
                setMessages(prev => [...prev, { id: `wf-cancel-${Date.now()}`, role: 'assistant' as const, content: result.text, sessionKey, timestamp: Date.now() }]);
            }
            return;
        } catch (err: any) {
            if (options?.force) return; // Session clear — don't surface backend errors.
            if (isWorkflowFormDismiss) {
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err), activeSessionKeyForEvents() || 'desktop-user')]);
                return;
            }
            // Older desktop bindings may not expose the structured task-panel API yet.
        }
        const message = `__agent_view_dismiss__ ${payload}`;
        const injected = await injectSupplementary(message);
        if (!injected) {
            await sendMessage(message, {
                uiAction: true,
                displayText: localizeText(uiLang, "Close task panel", "\u5173\u95ed\u4efb\u52a1\u9762\u677f", "\u95dc\u9589\u4efb\u52d9\u9762\u677f"),
            });
        }
    }, [activeSessionKeyForEvents, injectSupplementary, sendMessage, uiLang, updateVisibleAgentViewForSession]);

    // panelState / panelActions: pre-built objects matching AIAssistantPanelStateProps
    // and AIAssistantPanelActionProps. App.tsx spreads these directly into
    // <AIAssistantPanel state={...panelState} actions={...panelActions} />,
    // eliminating the manual field-by-field mapping that caused #agentView-missing.
    // When useAIAssistant adds a new field, it flows through automatically.
    const panelState: AIAssistantPanelHookState = useMemo(() => ({
        messages,
        progressMessages,
        sending,
        sendingSessionKey,
        busySessionKeys,
        streaming,
        streamingSessionKey,
        streamingSessionKeys,
        visualBusy,
        ready,
        initStatus,
        selectedFilePaths,
        submittedPrompts,
        draftInputValue,
        trialReflectEnabled,
        scrollToTopSeq,
        agentView,
    }), [messages, progressMessages, sending, sendingSessionKey, busySessionKeys, streaming, streamingSessionKey, streamingSessionKeys, visualBusy, ready, initStatus, selectedFilePaths, submittedPrompts, draftInputValue, trialReflectEnabled, scrollToTopSeq, agentView]);

    const panelActions: AIAssistantPanelHookActions = useMemo(() => ({
        browseFile,
        clearSelectedFile,
        removeSelectedFile,
        sendMessage,
        sendBtwMessage,
        sendMessageInBackground,
        injectSupplementary,
        guideLaunchReference,
        clearHistory,
        recordSubmittedPrompt,
        setDraftInputValue,
        executeAction,
        refreshNews: doFetchNews,
        cancelSession,
        submitAgentView,
        dismissAgentView,
    }), [browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, sendMessageInBackground, injectSupplementary, guideLaunchReference, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, doFetchNews, cancelSession, submitAgentView, dismissAgentView]);

    return { messages, submittedPrompts, draftInputValue, progressMessages, sending, sendingSessionKey, busySessionKeys, streaming, streamingSessionKey, streamingSessionKeys, visualBusy, ready, initStatus, selectedFilePaths, trialReflectEnabled, agentView, browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, sendMessageInBackground, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession, injectSupplementary, guideLaunchReference, submitAgentView, dismissAgentView, panelState, panelActions };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
