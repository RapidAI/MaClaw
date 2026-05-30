import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { SendAIAssistantMessage, SendBtwQuery, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession, CancelAIAssistantSessionForSession, CancelAIAssistantTask, SelectAIAssistantFiles, StartAIAssistantBackgroundTask, GetTrialReflectEnabled, GetAIAssistantTrace, LoadConfig, ListRemoteSessions, ResolveCriticalConfirm, InjectAIAssistantSupplementary, InjectAIAssistantGuideReference, InjectAIAssistantGuideReferenceForSession, SubmitAgentView, DismissAgentView } from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";
import { EventsOn, EventsOff, EventsEmit } from "../../../wailsjs/runtime";
import type { AgentView } from "./agentViewTypes";
import type { AIAssistantPanelHookState, AIAssistantPanelHookActions } from "./aiAssistantPanelTypes";
import { localizeText } from "./aiAssistantI18n";

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
    unfinished_slot?: AIAssistantResponseUnfinishedSlot;
    UnfinishedSlot?: AIAssistantResponseUnfinishedSlot;
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
}

interface AIAssistantContextMessage {
    role: 'user' | 'assistant';
    content: string;
}

interface SendMessageOptions {
    resumeSlotID?: string;
    startNewTask?: boolean;
    dismissSlotID?: string;
    uiAction?: boolean;
    displayText?: string;
    markConfirmationRunning?: boolean;
    /** Project path to include when sending from a Project Tab */
    project_path?: string;
    /** Tab ID (informational, not used for routing) */
    tabId?: string;
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

export type ChatActionStyle = 'default' | 'danger';

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
    localFilePath?: string;
    localFilePaths?: string[];
    thumbnailBase64?: string;
    imageKey?: string;
    requestId?: string;
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

// Module-level active session key. Updated by AIAssistantPanel when the active
// tab changes. The useAIAssistant hook reads this to filter events by session.
// This avoids prop-drilling through App.tsx -> AIAssistantPanel -> useAIAssistant.
let _activeSessionKey = '';
export function setActiveSessionKey(key: string) { _activeSessionKey = key; }
export function getActiveSessionKey(): string { return _activeSessionKey; }

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
const HIDDEN_PROGRESS_PATTERNS = [
    /^__heartbeat__$/,
];

function shouldHideProgressText(progressText: string): boolean {
    const trimmed = progressText.trim();
    if (!trimmed) return true;
    if (trimmed.includes("命令仍在执行中")) return true;
    return HIDDEN_PROGRESS_PATTERNS.some(pattern => pattern.test(trimmed));
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
        ) as ChatMessage[];
    } catch {
        return [];
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
        if (messageID && messageID.trim()) {
            localStorage.setItem(AI_ASSISTANT_CONTEXT_BOUNDARY_STORAGE_KEY, messageID);
        } else {
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

function buildResetConfirmationMessages(response: any): ChatMessage[] {
    const rawResetText = typeof response?.text === 'string'
        ? response.text
        : (typeof response?.error === 'string' ? response.error : '');
    const resetText = stripRolePrefixFrontend(rawResetText);
    logRolePrefixDiagnostic('reset-confirmation', rawResetText, resetText, {
        requestId: response?.request_id,
        responseSource: response?.response_source,
    });
    if (!resetText) return [];
    return [{
        id: nextId(),
        role: 'assistant' as const,
        content: resetText,
        timestamp: Date.now(),
    }];
}

function serializePersistedMessages(msgs: ChatMessage[]): string | null {
    // Only persist meaningful messages; skip progress, system, and empty content.
    // Strip image payloads to avoid blowing up localStorage (5MB limit).
    const toSave = msgs
        .filter(m => m.role !== 'progress' && m.role !== 'system' && m.content !== '')
        .slice(-MAX_PERSISTED_MESSAGES)
        .map(m => {
            if (!m.thumbnailBase64 && !m.imageKey) return m;
            const { thumbnailBase64: _, imageKey: __, ...rest } = m;
            return rest;
        });
    return toSave.length === 0 ? null : JSON.stringify(toSave);
}

function buildClientContextMessages(messages: ChatMessage[], startIndex = 0): AIAssistantContextMessage[] {
    return messages
        .slice(Math.max(0, startIndex))
        .filter((message): message is ChatMessage & { role: 'user' | 'assistant' } =>
            (message.role === 'user' || message.role === 'assistant') && message.content.trim() !== ''
        )
        .slice(-MAX_CONTEXT_MESSAGES_TO_SEND)
        .map(message => ({
            role: message.role,
            content: message.content,
        }));
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
    if (options?.uiAction !== undefined) payload.ui_action = options.uiAction;
    if (options?.project_path) payload.project_path = options.project_path;
    return payload;
}

function persistMessages(msgs: ChatMessage[]) {
    try {
        const serialized = serializePersistedMessages(msgs);
        if (serialized === null) {
            localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
            return;
        }
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, serialized);
    } catch {
        // localStorage full or unavailable - silently ignore
    }
}

function appendSubmittedPrompt(prompts: string[], prompt: string): string[] {
    const trimmed = prompt.trim();
    if (!trimmed) return prompts;
    if (prompts[prompts.length - 1] === trimmed) return prompts;
    return [...prompts, trimmed].slice(-MAX_PERSISTED_PROMPTS);
}

function persistPrompts(prompts: string[]) {
    try {
        const normalized = prompts
            .map(prompt => prompt.trim())
            .filter(Boolean)
            .slice(-MAX_PERSISTED_PROMPTS);
        if (normalized.length === 0) {
            localStorage.removeItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
            return;
        }
        localStorage.setItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY, JSON.stringify(normalized));
    } catch {
        // localStorage full or unavailable - silently ignore
    }
}

interface ActiveRound {
    generation: number;
    phase: 'idle' | 'requesting' | 'streaming';
    assistantMessageId: string | null;
    requestId: string;
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
        && left.requestId === right.requestId;
}

const IDLE_ROUND: ActiveRound = createIdleRound(0);

function isAssistantPlaceholder(msg: ChatMessage): boolean {
    return msg.role === 'assistant' && msg.content === '' && !msg.fields?.length && !msg.thumbnailBase64 && !msg.localFilePaths?.length && !msg.localFilePath;
}

function appendAssistantPlaceholder(messages: ChatMessage[], assistantMessageId: string, requestId = ''): ChatMessage[] {
    const index = messages.findIndex(msg => msg.id === assistantMessageId);
    if (index >= 0) return messages;
    return [...messages, {
        id: assistantMessageId,
        role: 'assistant',
        content: '',
        requestId: requestId || undefined,
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
        const nextReasoning = message.reasoning ? message.reasoning + reasoningDelta : reasoningDelta;
        if (nextReasoning === message.reasoning) return message;
        return { ...message, reasoning: nextReasoning };
    }

    const rawNextContent = message.content ? message.content + delta : delta;
    const nextContent = stripRolePrefixFrontend(rawNextContent);
    logRolePrefixDiagnostic('append-token', rawNextContent, nextContent, {
        messageId: message.id,
        requestId: message.requestId,
        deltaLen: delta.length,
    });
    if (nextContent === message.content) return message;
    return {
        ...message,
        content: nextContent,
    };
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

function normalizeConfirmation(raw: AIAssistantResponseConfirmation | null | undefined): ChatConfirmation | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const id = typeof raw.id === 'string' ? raw.id.trim() : (typeof raw.ID === 'string' ? raw.ID.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    if (!summary) return undefined;
    const taskType = typeof raw.task_type === 'string' ? raw.task_type.trim() : (typeof raw.TaskType === 'string' ? raw.TaskType.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    const rawLabels = (raw as Record<string, unknown>).labels ?? (raw as Record<string, unknown>).Labels;
    const labels = (rawLabels && typeof rawLabels === 'object') ? rawLabels as ChatConfirmationLabels : undefined;
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

function normalizeSendResponse(response: AIAssistantSendResult | null | undefined, showDetailEntry = false): AIAssistantSendResult {
    const raw = response || {};
    const normalizedFields = normalizeResponseFields(raw.fields ?? raw.Fields, showDetailEntry);
    const counterFields = tokenUsageCounterFields(raw, showDetailEntry, normalizedFields);
    return {
        ...raw,
        text: typeof raw.text === 'string' ? raw.text : (typeof raw.Text === 'string' ? raw.Text : ''),
        error: typeof raw.error === 'string' ? raw.error : (typeof raw.Error === 'string' ? raw.Error : ''),
        fields: mergeResponseFields(normalizedFields, counterFields),
        actions: raw.actions ?? raw.Actions,
        confirmation: normalizeConfirmation(raw.confirmation ?? raw.Confirmation),
        unfinished_slot: normalizeUnfinishedSlot((raw as any).unfinished_slot ?? (raw as any).UnfinishedSlot),
        local_file_path: typeof raw.local_file_path === 'string' ? raw.local_file_path : (typeof raw.LocalFilePath === 'string' ? raw.LocalFilePath : ''),
        local_file_paths: Array.isArray(raw.local_file_paths) ? raw.local_file_paths : (Array.isArray(raw.LocalFilePaths) ? raw.LocalFilePaths : undefined),
        thumbnail_base64: typeof raw.thumbnail_base64 === 'string' ? raw.thumbnail_base64 : (typeof raw.ThumbnailBase64 === 'string' ? raw.ThumbnailBase64 : ''),
        image_key: typeof raw.image_key === 'string' ? raw.image_key : (typeof raw.ImageKey === 'string' ? raw.ImageKey : ''),
        request_id: typeof raw.request_id === 'string' ? raw.request_id : (typeof raw.RequestID === 'string' ? raw.RequestID : ''),
        response_source: typeof raw.response_source === 'string' ? raw.response_source : (typeof raw.ResponseSource === 'string' ? raw.ResponseSource : ''),
        trace_status: typeof raw.trace_status === 'string' ? raw.trace_status : (typeof raw.TraceStatus === 'string' ? raw.TraceStatus : ''),
        trace_summary: typeof raw.trace_summary === 'string' ? raw.trace_summary : (typeof raw.TraceSummary === 'string' ? raw.TraceSummary : ''),
        trace_event_count: typeof raw.trace_event_count === 'number' ? raw.trace_event_count : (typeof raw.TraceEventCount === 'number' ? raw.TraceEventCount : undefined),
        evidence_count: typeof raw.evidence_count === 'number' ? raw.evidence_count : (typeof raw.EvidenceCount === 'number' ? raw.EvidenceCount : undefined),
        deferred: typeof raw.deferred === 'boolean' ? raw.deferred : (typeof raw.Deferred === 'boolean' ? raw.Deferred : false),
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

// rolePrefixPattern matches hallucinated role prefixes (e.g. "Browser: ..." or
// "Tool: ...") at the start of a line, with optional Markdown block-level markers.
// This is the frontend equivalent of the Go-side rolePrefixRe / rolePrefixLineRe.
const rolePrefixPattern = /^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)/m;

type RolePrefixDiagnosticMeta = Record<string, string | number | boolean | null | undefined>;
type RolePrefixDiagnosticInfo = {
    hasRolePrefix: boolean;
    rolePrefixKind?: string;
    rolePrefixIndex?: number;
    rolePrefixAtStart?: boolean;
};

function hasRolePrefixLeakCandidate(text: string): boolean {
    if (!text) return false;
    if (!text.includes('Browser') && !text.includes('Tool')) return false;
    if (!text.includes(':') && !text.includes('\uff1a')) return false;
    return rolePrefixPattern.test(text);
}

function rolePrefixDiagnosticInfo(text: string): RolePrefixDiagnosticInfo {
    if (!text || (!text.includes('Browser') && !text.includes('Tool'))) {
        return { hasRolePrefix: false };
    }
    if (!text.includes(':') && !text.includes('\uff1a')) {
        return { hasRolePrefix: false };
    }
    const match = rolePrefixPattern.exec(text);
    if (!match || match.index === undefined) {
        return { hasRolePrefix: false };
    }
    return {
        hasRolePrefix: true,
        rolePrefixKind: match[1],
        rolePrefixIndex: match.index,
        rolePrefixAtStart: text.slice(0, match.index).trim().length === 0,
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
    if (!text) return text;
    // Fast path: no known prefix keyword present.
    if (!text.includes('Browser') && !text.includes('Tool')) return text;
    if (!text.includes(':') && !text.includes('\uff1a')) return text;

    // Split into code-block-aware segments.
    const parts: Array<{ text: string; isCode: boolean }> = [];
    let rest = text;
    while (rest.length > 0) {
        const idx = rest.indexOf('```');
        if (idx < 0) {
            parts.push({ text: rest, isCode: false });
            break;
        }
        if (idx > 0) {
            parts.push({ text: rest.slice(0, idx), isCode: false });
        }
        const closeIdx = rest.indexOf('```', idx + 3);
        if (closeIdx < 0) {
            parts.push({ text: rest.slice(idx), isCode: true });
            break;
        }
        const end = closeIdx + 3;
        parts.push({ text: rest.slice(idx, end), isCode: true });
        rest = rest.slice(end);
    }

    // Scan non-code segments for role prefix.
    let absOffset = 0;
    for (const part of parts) {
        if (!part.isCode) {
            const match = rolePrefixPattern.exec(part.text);
            if (match && match.index !== undefined) {
                const matchAbsStart = absOffset + match.index;
                const prefixEnd = matchAbsStart + match[0].length;
                const before = text.slice(0, matchAbsStart).trimEnd();
                if (!before) {
                    // Case 1: prefix at start - strip it, keep everything after.
                    return text.slice(prefixEnd).trimStart();
                }
                // Case 2: prefix in middle. Match backend behavior and drop
                // the role-prefixed tail instead of keeping a duplicate block.
                return before;
            }
        }
        absOffset += part.text.length;
    }
    return text;
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
    return buildEmptyTerminalFallback(response);
}

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, response: any, preferences: AIAssistantPreferences): ChatMessage[] {
    const finalizeMessage = (message: ChatMessage): ChatMessage | null => {
        const nextContent = resolveFinalRoundContent(message, response);
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
        if (!nextContent && !nextFields?.length && !nextActions?.length && !nextThumbnailBase64 && !nextImageKey && !nextLocalFilePaths?.length) {
            return null;
        }
        return {
            ...message,
            content: nextContent,
            fields: nextFields,
            actions: nextActions.length > 0 ? nextActions : undefined,
            confirmation: response.confirmation,
            unfinishedSlot: (response as any).unfinished_slot,
            localFilePath: nextLocalFilePath || undefined,
            localFilePaths: nextLocalFilePaths,
            thumbnailBase64: nextThumbnailBase64,
            imageKey: nextImageKey || undefined,
        };
    };
    return updateRoundMessage(messages, assistantMessageId, requestId, finalizeMessage);
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
    return style === 'danger' ? 'danger' : 'default';
}

function normalizeInitStatus(status: unknown): AIAssistantInitStatus {
    return status === 'loading' || status === 'warming' || status === 'ready' || status === 'degraded' ? status : 'connecting';
}

function normalizeActions(actions: any): ChatAction[] | undefined {
    if (!Array.isArray(actions) || actions.length === 0) return undefined;
    return actions
        .filter(action => action && typeof action.label === 'string' && typeof action.command === 'string')
        .map(action => ({
            label: action.label,
            command: action.command,
            style: normalizeActionStyle(action.style),
        }));
}

function normalizeNewsCategory(category: unknown): NewsCategory {
    return category === 'notice' || category === 'update' || category === 'tip' || category === 'alert' ? category : '';
}

function normalizeStreamEvent(raw: unknown): AIAssistantStreamEvent {
    if (raw && typeof raw === 'object') {
        const event = raw as AIAssistantStreamEvent;
        return {
            request_id: typeof event.request_id === 'string' ? event.request_id : '',
            text: typeof event.text === 'string' ? event.text : '',
            session_key: typeof event.session_key === 'string' ? event.session_key : '',
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

function eventSequenceValue(payload: unknown): number | undefined {
    if (!payload || typeof payload !== 'object') return undefined;
    const data = payload as Record<string, unknown>;
    return typeof data.seq === 'number' ? data.seq : (typeof data.sequence === 'number' ? data.sequence : undefined);
}

function subscribeEvent(eventName: string, handler: (...args: any[]) => void): () => void {
    const unsubscribe = EventsOn(eventName, handler);
    return typeof unsubscribe === 'function' ? unsubscribe : () => EventsOff(eventName);
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

function createSystemMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'system',
        content,
        timestamp: Date.now(),
    };
}

function createTraceMessage(content: string, fields: Array<{ label: string; value: string }>): ChatMessage {
    return {
        id: nextId(),
        role: 'system',
        kind: 'trace',
        content,
        fields,
        timestamp: Date.now(),
    };
}

function createUserMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'user',
        content,
        timestamp: Date.now(),
    };
}

function createErrorMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'error',
        content,
        timestamp: Date.now(),
    };
}

function appendBackgroundLaunchMessages(messages: ChatMessage[], outgoingText: string, result: AIAssistantBackgroundLaunchResult): ChatMessage[] {
    return [
        ...messages,
        createUserMessage(outgoingText),
        createSystemMessage(buildBackgroundLaunchNotice(result)),
    ];
}

function createNewsMessage(article: any): ChatMessage {
    const iconByCategory: Record<Exclude<NewsCategory, ''>, string> = { notice: '📙', update: '🚀', tip: '💡', alert: '⚠️' };
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
            icon: category ? iconByCategory[category] : '📰',
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
        return confirmed ? '\n\n✓ Confirmed. Installing...' : '\n\n✕ Installation rejected.';
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
    const [agentView, setAgentView] = useState<AgentView | null>(null);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const pendingTaskRef = useRef<AIAssistantPendingTask | null>(null);
    const foregroundSendTailRef = useRef<Promise<boolean>>(Promise.resolve(true));
    const idleWaitersRef = useRef<Set<() => void>>(new Set());
    const initStatusRef = useRef<AIAssistantInitStatus>("connecting");
    const latestNewsPayloadRef = useRef<string>("[]");
    const progressTailRef = useRef<string | null>(null);
    const streamTokenBufferRef = useRef<StreamTokenBuffer | null>(null);
    const responseTimeoutControllerRef = useRef<ResponseTimeoutController | null>(null);
    const agentViewLifecycleSeqRef = useRef(0);
    const scrollOnNextNewsRef = useRef(true);
    const refreshSessionsOnlyRef = useRef(options?.refreshSessionsOnly);
    const lastPetSpeakingEmitAtRef = useRef(0);

    const stopResponseTimeout = useCallback(() => {
        const controller = responseTimeoutControllerRef.current;
        responseTimeoutControllerRef.current = null;
        controller?.stop();
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

    const notifyForegroundIdle = useCallback(() => {
        if (activeRoundRef.current.phase !== 'idle' || pendingTaskRef.current) return;
        const waiters = Array.from(idleWaitersRef.current);
        idleWaitersRef.current.clear();
        waiters.forEach(resolve => resolve());
    }, []);

    const waitForForegroundIdle = useCallback(() => {
        if (activeRoundRef.current.phase === 'idle' && !pendingTaskRef.current) {
            return Promise.resolve();
        }
        return new Promise<void>(resolve => {
            idleWaitersRef.current.add(resolve);
        });
    }, []);

    const setPendingTaskState = useCallback((nextTask: AIAssistantPendingTask | null) => {
        pendingTaskRef.current = nextTask;
        setPendingTask(current => {
            const same = current?.requestId === nextTask?.requestId
                && current?.sessionID === nextTask?.sessionID
                && current?.jobID === nextTask?.jobID
                && current?.runID === nextTask?.runID;
            return same ? current : nextTask;
        });
        if (!nextTask) notifyForegroundIdle();
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

    const sending = activeRound.phase !== 'idle' || !!pendingTask;
    const streaming = activeRound.phase === 'streaming';
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
            if (cancelled || initStatusRef.current === 'ready' || pollTimer) return;
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

    const setSelectedFiles = useCallback((nextPaths: string[]) => {
        const normalized = nextPaths.map(normalizeSelectedFilePath).filter(Boolean);
        setSelectedFilePaths(normalized);
        return normalized;
    }, []);

    const addSelectedFiles = useCallback((newPaths: string[]) => {
        const normalized = newPaths.map(normalizeSelectedFilePath).filter(Boolean);
        if (normalized.length === 0) return;
        setSelectedFilePaths(prev => {
            const existing = new Set(prev);
            return [...prev, ...normalized.filter(p => !existing.has(p))];
        });
    }, []);

    const removeSelectedFile = useCallback((index: number) => {
        setSelectedFilePaths(prev => prev.filter((_, i) => i !== index));
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
            });
        }
        setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId, current.requestId));
        return assistantMessageId;
    }, [setRoundState]);

    const clearTransientProgress = useCallback(() => {
        progressTailRef.current = null;
        setProgressMessages([]);
    }, []);

    const finalizeRound = useCallback((generation: number) => {
        if (activeRoundRef.current.generation !== generation) return;
        clearTransientProgress();
        resetActiveRound();
        emitPetStateForAssistant('idle', 'ai:round-done');
    }, [clearTransientProgress, emitPetStateForAssistant, resetActiveRound]);

    const startResponseTimeout = useCallback((round: { generation: number; assistantMessageId: string; requestId: string; source: string }) => {
        stopResponseTimeout();
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
            if (responseTimeoutControllerRef.current !== controller) return;
            const currentRound = activeRoundRef.current;
            if (currentRound.generation !== round.generation) return;
            if (currentRound.phase === 'idle') return;
            if (currentRound.phase === 'streaming') return;
            const activeRequestId = currentRound.requestId || round.requestId;
            if (pendingTaskRef.current?.requestId === activeRequestId) return;
            responseTimeoutControllerRef.current = null;
            setMessages(prev => replaceRoundWithError(prev, round.assistantMessageId, activeRequestId,
                `⏱️ 请求超时（${responseActivityTimeoutSec}秒无响应），请重试。`, true));
            clearTransientProgress();
            resetActiveRound(round.generation);
            emitPetStateForAssistant('idle', `${round.source}:timeout`);
        };

        const resetController = () => {
            if (responseTimeoutControllerRef.current !== controller) return;
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
        responseTimeoutControllerRef.current = controller;
        controller.reset();
        return controller;
    }, [clearTransientProgress, emitPetStateForAssistant, resetActiveRound, responseActivityTimeoutSec, stopResponseTimeout]);

    const resetResponseTimeoutForActiveRound = useCallback(() => {
        const controller = responseTimeoutControllerRef.current;
        if (!controller) return;
        const currentRound = activeRoundRef.current;
        if (currentRound.generation !== controller.generation) return;
        controller.reset();
    }, []);

    const appendTokenToAssistantMessage = useCallback((assistantMessageId: string, text: string) => {
        if (!assistantMessageId || !text) return;
        setMessages(prev => updateTailMessage(prev, assistantMessageId, message => appendTokenToMessage(message, text))
            ?? updateMessageById(prev, assistantMessageId, message => appendTokenToMessage(message, text)));
    }, []);

    const clearStreamTokenFlushTimer = useCallback(() => {
        const buffer = streamTokenBufferRef.current;
        if (!buffer?.flushTimer) return;
        clearTimeout(buffer.flushTimer);
        buffer.flushTimer = null;
    }, []);

    const flushStreamTokenBuffer = useCallback(() => {
        const buffer = streamTokenBufferRef.current;
        if (!buffer) return;
        clearStreamTokenFlushTimer();
        const text = buffer.text;
        if (!text) return;
        buffer.text = '';
        appendTokenToAssistantMessage(buffer.assistantMessageId, text);
    }, [appendTokenToAssistantMessage, clearStreamTokenFlushTimer]);

    const resetStreamTokenBuffer = useCallback(() => {
        clearStreamTokenFlushTimer();
        streamTokenBufferRef.current = null;
    }, [clearStreamTokenFlushTimer]);

    const queueStreamToken = useCallback((round: ActiveRound, text: string) => {
        if (!round.assistantMessageId || !text) return;

        // Reasoning tokens (\x01 prefix) are rendered immediately without
        // buffering. They display in a collapsed "thinking" area - the DOM
        // update cost is minimal (hidden content), and immediate rendering
        // ensures the first reasoning token triggers the "thinking" UI state
        // without delay.
        if (text.startsWith('\x01')) {
            appendTokenToAssistantMessage(round.assistantMessageId, text);
            return;
        }

        let buffer = streamTokenBufferRef.current;
        if (!buffer || buffer.requestId !== round.requestId || buffer.assistantMessageId !== round.assistantMessageId) {
            clearStreamTokenFlushTimer();
            buffer = {
                requestId: round.requestId,
                assistantMessageId: round.assistantMessageId,
                text: '',
                flushTimer: null,
                hasRenderedFirstToken: false,
            };
            streamTokenBufferRef.current = buffer;
        }

        if (!buffer.hasRenderedFirstToken) {
            buffer.hasRenderedFirstToken = true;
            appendTokenToAssistantMessage(round.assistantMessageId, text);
            return;
        }

        buffer.text += text;
        if (!buffer.flushTimer) {
            buffer.flushTimer = setTimeout(flushStreamTokenBuffer, STREAM_TOKEN_FLUSH_MS);
        }
    }, [appendTokenToAssistantMessage, clearStreamTokenFlushTimer, flushStreamTokenBuffer]);

    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const latestMessagesRef = useRef(messages);
    const latestPromptsRef = useRef(submittedPrompts);
    const lastPersistedPayloadRef = useRef<string | null>(null);
    const lastPersistedPromptsPayloadRef = useRef<string | null>(null);
    const persistOnUnmountRef = useRef(true);
    const contextBoundaryMessageIDRef = useRef<string | null>(loadPersistedContextBoundaryMessageID());
    useEffect(() => {
        latestMessagesRef.current = messages;
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
            lastPersistedPayloadRef.current = payload;
            persistMessages(latestMessagesRef.current);
        }, 300);
    }, [messages]);
    useEffect(() => {
        latestPromptsRef.current = submittedPrompts;
        const nextPayload = JSON.stringify(submittedPrompts);
        if (nextPayload === lastPersistedPromptsPayloadRef.current) {
            return;
        }
        lastPersistedPromptsPayloadRef.current = nextPayload;
        persistPrompts(submittedPrompts);
    }, [submittedPrompts]);
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
            if (payload !== lastPersistedPayloadRef.current) {
                lastPersistedPayloadRef.current = payload;
                persistMessages(latestMessagesRef.current);
            }
            const promptPayload = JSON.stringify(latestPromptsRef.current);
            if (promptPayload !== lastPersistedPromptsPayloadRef.current) {
                lastPersistedPromptsPayloadRef.current = promptPayload;
                persistPrompts(latestPromptsRef.current);
            }
        };
    }, []);

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
            const currentTask = pendingTaskRef.current;
            if (!currentTask) return;
            try {
                const sessions = await ListRemoteSessions() as AIAssistantRemoteSessionView[];
                const stillActive = (Array.isArray(sessions) ? sessions : []).some(session => {
                    if (normalizeLaunchSource(session?.launch_source) !== 'ai') return false;
                    if (isTerminalRemoteStatus(session?.status)) return false;
                    return matchesPendingTaskSession(session, currentTask);
                });
                if (!stillActive && pendingTaskRef.current?.requestId === currentTask.requestId) {
                    setPendingTaskState(null);
                    const currentRound = activeRoundRef.current;
                    if (currentRound.requestId === currentTask.requestId) {
                        stopResponseTimeout();
                        clearTransientProgress();
                        resetActiveRound(currentRound.generation);
                    }
                }
            } catch {
            }
        };

        void refreshPendingTask();
        if (!pendingTask) return;

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
    }, [clearTransientProgress, pendingTask, resetActiveRound, setPendingTaskState, stopResponseTimeout]);

    useEffect(() => {
        const tokenHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (event.request_id && matchesActiveRequest(currentRound, event)) {
                resetResponseTimeoutForActiveRound();
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
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            if (!matchesActiveRequest(currentRound, event)) return;
            resetResponseTimeoutForActiveRound();
            emitPetStateForAssistant('thinking', 'ai:new-round', 10000);
            ensureRoundPlaceholder(currentRound.generation);
        };

        const streamDoneHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            if (!matchesActiveRequest(currentRound, event)) return;
            resetResponseTimeoutForActiveRound();
            emitPetStateForAssistant('thinking', 'ai:stream-done', 2500);
            flushStreamTokenBuffer();
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
            resetStreamTokenBuffer();
        };
    }, [activeSessionKeyForEvents, emitPetStateForAssistant, ensureRoundPlaceholder, flushStreamTokenBuffer, queueStreamToken, resetResponseTimeoutForActiveRound, resetStreamTokenBuffer, transitionRound]);

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
            if (!matchesActiveResponseRequest(currentRound, responseRequestId)) return;
            flushStreamTokenBuffer();
            const assistantMessageId = currentRound.assistantMessageId || '';
            const effectiveRequestId = responseRequestId || currentRound.requestId;
            const userText = currentRound.userText || '';

            // Handle explicit history reset (/new, /reset, /clear)
            const explicitReset = normalized.clear_ui && isExplicitHistoryResetCommand(userText);
            if (explicitReset) {
                const resetMessages = buildResetConfirmationMessages(normalized);
                const nextBoundary = resetMessages[0]
                    ? `${AI_ASSISTANT_CONTEXT_BOUNDARY_AFTER_PREFIX}${resetMessages[0].id}`
                    : AI_ASSISTANT_CONTEXT_BOUNDARY_END;
                if (persistTimerRef.current) {
                    clearTimeout(persistTimerRef.current);
                    persistTimerRef.current = null;
                }
                latestMessagesRef.current = resetMessages;
                lastPersistedPayloadRef.current = null;
                contextBoundaryMessageIDRef.current = nextBoundary;
                persistContextBoundaryMessageID(nextBoundary);
                localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
                clearTransientProgress();
                setMessages(resetMessages);
            } else {
                setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, normalized, preferences));
                if (normalized.clear_ui) {
                    contextBoundaryMessageIDRef.current = '';
                    persistContextBoundaryMessageID('');
                    clearTransientProgress();
                }
            }
            setPendingTaskState(null);
            stopResponseTimeout();
            resetStreamTokenBuffer();
            finalizeRound(currentRound.generation);
        };
        const off = subscribeEvent(RESPONSE_EVENT, handler);
        return () => { off(); };
    }, [clearTransientProgress, finalizeRound, flushStreamTokenBuffer, preferences, resetStreamTokenBuffer, stopResponseTimeout]);

    const sendMessageNow = useCallback(async (text: string, options?: SendMessageOptions): Promise<boolean> => {
        // Callers (e.g. handleSend in AIAssistantPanel) are responsible for
        // embedding file paths into `text` via buildOutgoingMessageMulti before calling here.
        const outgoingText = text.trim();
        if (outgoingText === "") return false;
        await waitForForegroundIdle();

        const generation = activeRoundRef.current.generation + 1;
        const assistantMessageId = nextId();
        const requestId = createForegroundRequestID();
        const userMsg: ChatMessage = {
            id: nextId(),
            role: 'user',
            content: options?.displayText || outgoingText,
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMessageId,
            role: 'assistant',
            content: '',
            requestId,
            timestamp: Date.now(),
        };
        const approvalMessage = options?.markConfirmationRunning === true;
        const contextStartIndex = resolveContextStartIndex(latestMessagesRef.current, contextBoundaryMessageIDRef.current);
        const recentMessages = buildClientContextMessages(latestMessagesRef.current, contextStartIndex);

        stopResponseTimeout();
        resetStreamTokenBuffer();
        clearTransientProgress();
        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId,
            requestId,
            userText: outgoingText,
        });
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
            if (responseTimeoutControllerRef.current !== responseTimeoutController) return;
            responseTimeoutControllerRef.current = null;
            responseTimeoutController.stop();
        };

        let deferredAsync = false;
        try {
            // All messages go through the async path - the binding returns
            // immediately with {deferred: true}. The actual response arrives
            // via the "ai-assistant-response" event and is processed by the
            // event handler above.
            const rawResponse = await SendAIAssistantMessage(
                buildAIAssistantSendPayload(outgoingText, requestId, recentMessages, options)
            ) as AIAssistantSendResult;
            const response = normalizeSendResponse(rawResponse, preferences.showTraceEntry);
            const responseRequestId = resolveSendRequestID(response);
            const currentRound = activeRoundRef.current;
            if (currentRound.generation !== generation || currentRound.phase === 'idle') {
                return true;
            }
            // Update requestId if the backend assigned a different one.
            if (responseRequestId && responseRequestId !== requestId) {
                setRoundState({
                    generation,
                    phase: activeRoundRef.current.phase,
                    assistantMessageId,
                    requestId: responseRequestId,
                    userText: outgoingText,
                });
            }
            if (response.deferred) {
                // Async mode: response will arrive via event. If there's a
                // live run_id/job_id, track that remote coding session. When
                // the backend reports deferred but the session already ended,
                // finalize this foreground round instead of leaving the input locked.
                if (response.run_id || response.job_id) {
                    const effectiveRequestId = responseRequestId || requestId;
                    const pending = await resolvePendingAITask(effectiveRequestId, response);
                    if (pending) {
                        setPendingTaskState(pending);
                        deferredAsync = true;
                    } else {
                        setPendingTaskState(null);
                        deferredAsync = false;
                    }
                } else {
                    deferredAsync = true;
                }
            } else {
                // Synchronous response (e.g. handleAgentViewControlMessage,
                // TryHandlePassthroughSlashCommand). Process immediately.
                clearResponseTimeout();
                flushStreamTokenBuffer();
                const effectiveRequestId = responseRequestId || requestId;
                const explicitReset = response.clear_ui && isExplicitHistoryResetCommand(outgoingText);
                if (explicitReset) {
                    const resetMessages = buildResetConfirmationMessages(response);
                    const nextBoundary = resetMessages[0]
                        ? `${AI_ASSISTANT_CONTEXT_BOUNDARY_AFTER_PREFIX}${resetMessages[0].id}`
                        : AI_ASSISTANT_CONTEXT_BOUNDARY_END;
                    if (persistTimerRef.current) {
                        clearTimeout(persistTimerRef.current);
                        persistTimerRef.current = null;
                    }
                    latestMessagesRef.current = resetMessages;
                    lastPersistedPayloadRef.current = null;
                    contextBoundaryMessageIDRef.current = nextBoundary;
                    persistContextBoundaryMessageID(nextBoundary);
                    localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
                    clearTransientProgress();
                    setMessages(resetMessages);
                } else {
                    setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, response, preferences));
                    if (response.clear_ui) {
                        contextBoundaryMessageIDRef.current = userMsg.id;
                        persistContextBoundaryMessageID(userMsg.id);
                        clearTransientProgress();
                    }
                }
                setPendingTaskState(null);
            }
        } catch (err: any) {
            clearResponseTimeout();
            resetStreamTokenBuffer();
            const currentRound = activeRoundRef.current;
            if (currentRound.generation !== generation || currentRound.phase === 'idle') {
                return true;
            }
            setMessages(prev => resolveSendResult(prev, assistantMessageId, requestId, null, preferences, err?.message || String(err)));
            setPendingTaskState(null);
        } finally {
            if (!deferredAsync) {
                clearResponseTimeout();
                resetStreamTokenBuffer();
                finalizeRound(generation);
            }
        }
        return true;
    }, [clearTransientProgress, emitPetStateForAssistant, finalizeRound, flushStreamTokenBuffer, preferences, resetActiveRound, resetStreamTokenBuffer, setRoundState, startResponseTimeout, stopResponseTimeout, waitForForegroundIdle]);

    const sendMessage = useCallback((text: string, options?: SendMessageOptions): Promise<boolean> => {
        const outgoingText = text.trim();
        if (outgoingText === "") return Promise.resolve(false);
        const run = foregroundSendTailRef.current
            .catch(() => true)
            .then(() => sendMessageNow(outgoingText, options));
        foregroundSendTailRef.current = run.catch(() => false);
        return run;
    }, [sendMessageNow]);

    const sendMessageInBackground = useCallback(async (text: string) => {
        // Callers are responsible for embedding file paths into `text` before calling here.
        const outgoingText = text.trim();
        if (outgoingText === "" || activeRoundRef.current.phase !== 'idle') return;

        const generation = activeRoundRef.current.generation + 1;
        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId: null,
            requestId: '',
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
            setMessages(prev => appendBackgroundLaunchMessages(prev, outgoingText, launchResult));
            await refreshSessionsOnlyRef.current?.();
        } catch (err: any) {
            setMessages(prev => [...prev, createUserMessage(outgoingText), createErrorMessage(err?.message || String(err))]);
        } finally {
            finalizeRound(generation);
        }
    }, [emitPetStateForAssistant, finalizeRound, setRoundState]);

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
        if (!trimmedQuery) {
            // Show usage help as a local message - no backend call needed.
            setMessages(prev => [...prev,
                { id: nextId(), role: 'user' as const, content: '/btw', timestamp: Date.now() },
                { id: nextId(), role: 'assistant' as const, content: 'Usage: /btw <query>\n\nExamples:\n  /btw latest Go changes\n  /btw React 19 major changes\n  /btw what framework does this project use?', timestamp: Date.now() },
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
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMsgId,
            role: 'assistant',
            content: '',
            requestId,
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
                m.id === assistantMsgId ? { ...m, content: `❌ /btw 查询失败: ${err?.message || String(err)}` } : m
            ));
        } finally {
            offBtwToken();
            offBtwProgress();
            emitPetStateForAssistant('idle', 'ai:btw-done');
        }
    }, [emitPetStateForAssistant]);

    const browseFile = useCallback(async () => {
        try {
            const selected = await SelectAIAssistantFiles();
            if (!selected || selected.length === 0) return; // User cancelled or no files selected
            const validPaths = selected.filter(p => p && p.trim());
            if (validPaths.length > 0) {
                addSelectedFiles(validPaths);
            }
        } catch (err) {
            console.error('Failed to select files:', err);
        }
    }, [addSelectedFiles]);

    const clearSelectedFile = useCallback(() => {
        setSelectedFiles([]);
    }, [setSelectedFiles]);

    const clearHistory = useCallback(async () => {
        stopResponseTimeout();
        resetActiveRound();
        setPendingTaskState(null);
        setSelectedFiles([]);
        if (persistTimerRef.current) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
        latestMessagesRef.current = [];
        lastPersistedPayloadRef.current = null;
        persistOnUnmountRef.current = false;
        foregroundSendTailRef.current = Promise.resolve(true);
        contextBoundaryMessageIDRef.current = null;
        persistContextBoundaryMessageID(null);
        localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        setMessages([]);
        setDraftInputValue("");
        clearTransientProgress();
        latestNewsPayloadRef.current = '[]';
        scrollOnNextNewsRef.current = true;
        doFetchNews();
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
        } finally {
            persistOnUnmountRef.current = true;
        }
    }, [clearTransientProgress, doFetchNews, resetActiveRound, setSelectedFiles, stopResponseTimeout]);

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
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err))]);
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
                setMessages(prev => [...prev, createTraceMessage(buildTraceDetailMessage(view, runID), buildTraceDetailFields(view, runID))]);
            } catch (err: any) {
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err))]);
            }
            return;
        }
        const resumeMatch = command.match(/^__resume_unfinished__\s+(\S+)$/);
        if (resumeMatch) {
            const resumeText = localizeText(uiLang, "Continue previous unfinished task", "\u7ee7\u7eed\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u7e7c\u7e8c\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9");
            return sendMessage(resumeText, {
                resumeSlotID: resumeMatch[1]?.trim() || '',
                displayText: resumeText,
            });
        }
        // Backward compat: __start_new_task__ is no longer emitted by the
        // backend (merged into __dismiss_unfinished__), but keep the handler
        // in case older backend versions are still in use.
        if (command === '__start_new_task__') {
            const startNewText = localizeText(uiLang, "Start a new task", "\u5f00\u59cb\u4e00\u4e2a\u65b0\u4efb\u52a1", "\u958b\u59cb\u4e00\u500b\u65b0\u4efb\u52d9");
            return sendMessage(startNewText, {
                startNewTask: true,
                uiAction: true,
                displayText: startNewText,
            });
        }
        const dismissMatch = command.match(/^__dismiss_unfinished__\s+(\S+)$/);
        if (dismissMatch) {
            const dismissText = localizeText(uiLang, "Dismiss previous unfinished task", "\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9");
            return sendMessage(dismissText, {
                dismissSlotID: dismissMatch[1]?.trim() || '',
                startNewTask: true,
                uiAction: true,
                displayText: dismissText,
            });
        }
        return sendMessage(command);
    }, [sendMessage, uiLang]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            const currentRound = activeRoundRef.current;
            if (!isMatchingSessionOrActiveRequest(event, currentRound, activeSessionKeyForEvents())) return;
            const progressText = event.text || (typeof payload === 'string' ? payload : '');
            if (!progressText) return;
            if (!matchesActiveProgressRequest(currentRound, event)) {
                return;
            }
            // Reset the sliding-window activity timeout - backend is alive.
            resetResponseTimeoutForActiveRound();
            if (shouldHideProgressText(progressText)) {
                return;
            }
            if (progressTailRef.current === progressText) {
                return;
            }
            progressTailRef.current = progressText;
            setProgressMessages(prev => appendProgressText(prev, progressText));
        };
        const offProgress = subscribeEvent(PROGRESS_EVENT, handler);
        return () => {
            offProgress();
        };
    }, [activeSessionKeyForEvents, resetResponseTimeoutForActiveRound]);

    useEffect(() => {
        // agent-view:lifecycle is the single source of truth for view state.
        // The legacy "agent-view" / "agent-view-clear" events are ignored when
        // lifecycle events are available, preventing double-setState on the same
        // render cycle. Legacy events are retained only for external consumers
        // that haven't migrated to the lifecycle protocol.
        let lifecycleActive = false;
        const acceptAgentViewSequence = (payload: unknown): boolean => {
            const seq = eventSequenceValue(payload);
            if (typeof seq !== 'number' || seq <= 0) return true;
            if (seq < agentViewLifecycleSeqRef.current) return false;
            agentViewLifecycleSeqRef.current = seq;
            return true;
        };
        const offView = subscribeEvent(AGENT_VIEW_EVENT, (payload: unknown) => {
            if (lifecycleActive) return; // lifecycle is authoritative
            if (!acceptAgentViewSequence(payload)) return;
            const nextView = normalizeAgentView(payload);
            if (nextView) setAgentView(nextView);
        });
        const offClear = subscribeEvent(AGENT_VIEW_CLEAR_EVENT, (payload: unknown) => {
            if (lifecycleActive) return;
            if (!acceptAgentViewSequence(payload)) return;
            const data = payload && typeof payload === 'object' ? payload as Record<string, unknown> : {};
            const event: AgentViewLifecyclePayload = {
                action: 'dismiss',
                view_id: typeof data.view_id === 'string' ? data.view_id : undefined,
                workflow_id: typeof data.workflow_id === 'string' ? data.workflow_id : undefined,
                workflow_phase: typeof data.workflow_phase === 'string' ? data.workflow_phase : undefined,
                workflow_user_id: typeof data.workflow_user_id === 'string' ? data.workflow_user_id : undefined,
            };
            setAgentView(current => agentViewMatchesLifecycle(current, event) ? null : current);
        });
        const offLifecycle = subscribeEvent(AGENT_VIEW_LIFECYCLE_EVENT, (payload: unknown) => {
            const event = normalizeAgentViewLifecycle(payload);
            if (!event) return;
            if (!acceptAgentViewSequence(payload)) return;
            lifecycleActive = true;
            switch (event.action) {
                case "open":
                case "update":
                    if (event.view) setAgentView(event.view);
                    break;
                case "dismiss":
                    setAgentView(current => agentViewMatchesLifecycle(current, event) ? null : current);
                    break;
                case "complete":
                    if (event.view) setAgentView(event.view);
                    else setAgentView(current => agentViewMatchesLifecycle(current, event) ? null : current);
                    break;
                case "error":
                    if (event.error) {
                        setMessages(prev => [...prev, createErrorMessage(event.error || "Task panel error")]);
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
    }, []);

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
        const canceledRound = activeRoundRef.current;
        const pendingTaskAtCancel = pendingTaskRef.current;
        const sessionKeyAtCancel = activeSessionKeyForEvents();
        const nextGeneration = canceledRound.generation + 1;
        if (!canceledRound.assistantMessageId && isRoundIdle(canceledRound) && !pendingTaskAtCancel) {
            return { canceledText: "" };
        }
        if (canceledRound.assistantMessageId) {
            flushStreamTokenBuffer();
            resetStreamTokenBuffer();
            setMessages(prev => markRoundCancelled(prev, canceledRound.assistantMessageId, canceledRound.requestId));
        }
        stopResponseTimeout();
        resetActiveRound(nextGeneration);
        clearTransientProgress();
        setPendingTaskState(null);
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
                    canceledText: sessionKeyAtCancel && sessionKeyAtCancel !== 'desktop-user'
                        ? (await CancelAIAssistantSessionForSession(sessionKeyAtCancel)) || ""
                        : (await CancelAIAssistantSession()) || "",
                };
            } catch {
                return { canceledText: "" };
            }
        })();
        foregroundSendTailRef.current = cancelBackend.then(() => true).catch(() => true);
        return await cancelBackend;
    }, [activeSessionKeyForEvents, clearTransientProgress, emitPetStateForAssistant, flushStreamTokenBuffer, resetActiveRound, resetStreamTokenBuffer, setPendingTaskState, stopResponseTimeout]);

    // injectSupplementary sends a supplementary message into the running
    // agent loop without cancelling it. Returns true if the injection was
    // accepted (a loop is active), false if no loop is running (caller
    // should fall back to normal sendMessage).
    // On success, a user message bubble is added to the chat so the user
    // sees visual confirmation of what was injected.
    const injectSupplementary = useCallback(async (text: string): Promise<boolean> => {
        try {
            const accepted = await InjectAIAssistantSupplementary(text);
            if (accepted) {
                // Show the injected text as a user message in the chat area
                // so the user has visual confirmation.
                setMessages(prev => [...prev, createUserMessage("💬 " + text)]);
            }
            return accepted;
        } catch {
            return false;
        }
    }, []);

    const guideLaunchReference = useCallback(async (text: string, sessionKey?: string): Promise<boolean> => {
        try {
            const normalizedSessionKey = sessionKey?.trim() || '';
            const accepted = normalizedSessionKey
                ? await InjectAIAssistantGuideReferenceForSession(text, normalizedSessionKey)
                : await InjectAIAssistantGuideReference(text);
            if (accepted && (!normalizedSessionKey || normalizedSessionKey === 'desktop-user')) {
                setMessages(prev => [...prev, createSystemMessage("引导已注入下一轮：\n" + text)]);
            }
            return accepted;
        } catch {
            return false;
        }
    }, []);

    const submitAgentView = useCallback(async (viewId: string | undefined, data: Record<string, unknown>) => {
        const isWorkflowFormSubmit = typeof viewId === 'string' && viewId.startsWith('workflow:form:');
        if (!isWorkflowFormSubmit) setAgentView(null);
        const payload = JSON.stringify({ view_id: viewId || "", data });
        let workflowSubmitRound: { generation: number; assistantMessageId: string; requestId: string } | null = null;
        const startAgentViewSubmitRound = (requestId: string) => {
            const generation = activeRoundRef.current.generation + 1;
            const assistantMessageId = nextId();
            stopResponseTimeout();
            resetStreamTokenBuffer();
            clearTransientProgress();
            setRoundState({ generation, phase: 'requesting', assistantMessageId, requestId, userText: '' });
            setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId, requestId));
            emitPetStateForAssistant('thinking', 'agent-view:submit', 15000);

            startResponseTimeout({ generation, assistantMessageId, requestId, source: 'agent-view' });
            return { generation, assistantMessageId, requestId };
        };
        try {
            if (isWorkflowFormSubmit) {
                await waitForForegroundIdle();
                workflowSubmitRound = startAgentViewSubmitRound(createForegroundRequestID());
            }
            const rawResponse = await SubmitAgentView({ view_id: viewId || "", data, request_id: workflowSubmitRound?.requestId || undefined }) as AIAssistantSendResult | null | undefined;
            const response = normalizeSendResponse(rawResponse, preferences.showTraceEntry);
            if (response?.deferred && !workflowSubmitRound) {
                const requestId = resolveSendRequestID(response) || createForegroundRequestID();
                startAgentViewSubmitRound(requestId);
            }
            if (workflowSubmitRound && !response?.deferred) {
                const round = workflowSubmitRound;
                stopResponseTimeout();
                flushStreamTokenBuffer();
                setMessages(prev => resolveSendResult(prev, round.assistantMessageId, round.requestId, response, preferences));
                resetActiveRound(round.generation);
                emitPetStateForAssistant('idle', 'agent-view:done');
            }
            return;
        } catch (err: any) {
            if (workflowSubmitRound) {
                const round = workflowSubmitRound;
                stopResponseTimeout();
                setMessages(prev => resolveSendResult(prev, round.assistantMessageId, round.requestId, null, preferences, err?.message || String(err)));
                resetActiveRound(round.generation);
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
    }, [clearTransientProgress, emitPetStateForAssistant, flushStreamTokenBuffer, injectSupplementary, preferences, resetActiveRound, resetStreamTokenBuffer, sendMessage, setRoundState, startResponseTimeout, stopResponseTimeout, uiLang, waitForForegroundIdle]);

    const dismissAgentView = useCallback(async (viewId: string | undefined, data?: Record<string, unknown>) => {
        const isWorkflowFormDismiss = typeof viewId === 'string' && viewId.startsWith('workflow:form:');
        if (!isWorkflowFormDismiss) setAgentView(null);
        const payload = JSON.stringify({ view_id: viewId || "", data: data || {} });
        try {
            await DismissAgentView({ view_id: viewId || "", data: data || {} });
            return;
        } catch (err: any) {
            if (isWorkflowFormDismiss) {
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err))]);
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
    }, [injectSupplementary, sendMessage, uiLang]);

    // panelState / panelActions: pre-built objects matching AIAssistantPanelStateProps
    // and AIAssistantPanelActionProps. App.tsx spreads these directly into
    // <AIAssistantPanel state={...panelState} actions={...panelActions} />,
    // eliminating the manual field-by-field mapping that caused #agentView-missing.
    // When useAIAssistant adds a new field, it flows through automatically.
    const panelState: AIAssistantPanelHookState = useMemo(() => ({
        messages,
        progressMessages,
        sending,
        streaming,
        visualBusy,
        ready,
        initStatus,
        selectedFilePaths,
        submittedPrompts,
        draftInputValue,
        trialReflectEnabled,
        scrollToTopSeq,
        agentView,
    }), [messages, progressMessages, sending, streaming, visualBusy, ready, initStatus, selectedFilePaths, submittedPrompts, draftInputValue, trialReflectEnabled, scrollToTopSeq, agentView]);

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

    return { messages, submittedPrompts, draftInputValue, progressMessages, sending, streaming, visualBusy, ready, initStatus, selectedFilePaths, trialReflectEnabled, agentView, browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, sendMessageInBackground, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession, injectSupplementary, guideLaunchReference, submitAgentView, dismissAgentView, panelState, panelActions };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
