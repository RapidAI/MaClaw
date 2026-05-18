/**
 * useGroupSessionActions — Single execution layer for all group chat operations.
 *
 * MECHANISM: "Ensure Session" pattern.
 * Operations do NOT require a pre-existing session. When sessionId is null,
 * the hook automatically creates a session via InitiateVEConversation/
 * InitiateGroupConversation before executing the requested operation.
 *
 * This eliminates the "click does nothing" problem — the user never needs to
 * "send a message first". Session creation is an implementation detail handled
 * transparently by the execution layer.
 *
 * Responsibilities:
 * 1. Session lifecycle (ensure exists, return new sessionId to caller)
 * 2. Precondition validation (duplicate check, capacity check)
 * 3. Operation execution (Wails binding calls)
 * 4. Unified feedback (success/error/empty-state notifications)
 */

import { useCallback, useRef, useState } from "react";

// --- Types ---

export type ActionFeedbackLevel = "info" | "success" | "error";

export interface ActionFeedback {
    message: string;
    level: ActionFeedbackLevel;
    ts: number;
}

export interface GroupSessionContext {
    /** A2A session ID (null = will be auto-created) */
    sessionId: string | null;
    /** The primary VE ID for this tab (used to auto-create session) */
    veId?: string;
    /** Current participant IDs in the group */
    participants: string[];
    /** Hub-configured max participants */
    maxParticipants: number;
}

export interface InviteResult {
    success: boolean;
    /** Available VEs that can be invited */
    available: AvailableVE[];
    /** Session ID (may be newly created) */
    sessionId: string | null;
}

export interface AvailableVE {
    id: string;
    name: string;
    machineId?: string;
}

export interface AddResult {
    success: boolean;
    /** Session ID (may be newly created) */
    sessionId: string | null;
}

export interface UseGroupSessionActionsOptions {
    lang?: string;
    /** Override for testing */
    listVirtualEmployees?: () => Promise<any[]>;
    /** Override for testing */
    addVEToGroup?: (sessionId: string, veId: string) => Promise<void>;
    /** Override for testing */
    registerLocalExecutor?: (sessionId: string) => Promise<void>;
    /** Override for testing */
    initiateConversation?: (veId: string) => Promise<{ session_id: string }>;
}

export interface UseGroupSessionActionsResult {
    feedback: ActionFeedback | null;
    clearFeedback: () => void;
    /**
     * Check availability and list invitable VEs.
     * Auto-creates session if needed. Returns sessionId for caller to persist.
     */
    checkInviteAvailability: (ctx: GroupSessionContext) => Promise<InviteResult>;
    /**
     * Invite a specific VE to the group.
     * Auto-creates session if needed. Returns sessionId for caller to persist.
     */
    inviteVE: (ctx: GroupSessionContext, veId: string) => Promise<AddResult>;
    /**
     * Add local maclaw AI to the group.
     * Auto-creates session if needed. Returns sessionId for caller to persist.
     */
    addLocalAI: (ctx: GroupSessionContext) => Promise<AddResult>;
}

// --- i18n (pure function, not a hook dependency) ---

function t(lang: string | undefined, zh: string, en: string): string {
    return (!lang || lang.startsWith("zh")) ? zh : en;
}

function readableAvailableVEName(ve: any, index: number, lang: string | undefined): string {
    const name = String(ve?.name || "").trim();
    const id = String(ve?.id || "").trim();
    const machineId = String(ve?.machine_id || "").trim();
    if (name && name !== id && name !== machineId && !/^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+)$/.test(name)) return name;
    const ordinal = index + 1;
    return t(lang, "数字员工 " + ordinal, "Digital employee " + ordinal);
}

// --- Hook ---

export function useGroupSessionActions(options: UseGroupSessionActionsOptions = {}): UseGroupSessionActionsResult {
    const [feedback, setFeedback] = useState<ActionFeedback | null>(null);
    const optRef = useRef(options);
    optRef.current = options;

    const emit = useCallback((message: string, level: ActionFeedbackLevel) => {
        setFeedback({ message, level, ts: Date.now() });
    }, []);

    const clearFeedback = useCallback(() => setFeedback(null), []);

    // --- Load Wails module (lazy, cached by bundler) ---
    const loadAppModule = useCallback(async () => {
        try {
            return await import("../../../wailsjs/go/main/App");
        } catch {
            emit(t(optRef.current.lang, "功能模块加载失败", "Failed to load module"), "error");
            return null;
        }
    }, [emit]);

    // --- CORE MECHANISM: Ensure session exists ---
    // If sessionId is null, auto-create via InitiateVEConversation.
    // Returns the (possibly new) sessionId, or null on failure.
    const ensureSession = useCallback(async (ctx: GroupSessionContext): Promise<string | null> => {
        if (ctx.sessionId) return ctx.sessionId;

        // Need veId to create a session
        if (!ctx.veId) {
            emit(t(optRef.current.lang, "无法建立会话：缺少目标员工信息", "Cannot create session: missing VE info"), "error");
            return null;
        }

        const mod = await loadAppModule();
        if (!mod) return null;

        const initFn = optRef.current.initiateConversation || (mod as any).InitiateVEConversation;
        if (!initFn) {
            emit(t(optRef.current.lang, "无法建立会话", "Cannot create session"), "error");
            return null;
        }

        try {
            const result = await initFn(ctx.veId);
            return result?.session_id || null;
        } catch (err: any) {
            emit(
                t(optRef.current.lang, `建立会话失败：${err?.message || ""}`, `Session creation failed: ${err?.message || ""}`),
                "error"
            );
            return null;
        }
    }, [loadAppModule, emit]);

    // --- Precondition: capacity ---
    const requireCapacity = useCallback((ctx: GroupSessionContext): boolean => {
        if (ctx.participants.length >= ctx.maxParticipants) {
            emit(
                t(optRef.current.lang, `群聊人数已满（最多 ${ctx.maxParticipants} 人）`, `Group is full (max ${ctx.maxParticipants})`),
                "error"
            );
            return false;
        }
        return true;
    }, [emit]);

    // --- checkInviteAvailability ---
    const checkInviteAvailability = useCallback(async (ctx: GroupSessionContext): Promise<InviteResult> => {
        if (!requireCapacity(ctx)) {
            return { success: false, available: [], sessionId: ctx.sessionId };
        }

        const sessionId = await ensureSession(ctx);
        if (!sessionId) return { success: false, available: [], sessionId: null };

        const mod = await loadAppModule();
        if (!mod) return { success: false, available: [], sessionId };

        const listFn = optRef.current.listVirtualEmployees || (mod as any).ListVirtualEmployees;
        if (!listFn) {
            emit(t(optRef.current.lang, "暂无可邀请的数字员工", "No digital employees available"), "info");
            return { success: false, available: [], sessionId };
        }

        try {
            const employees = await listFn();
            const currentIds = new Set(ctx.participants);
            const available: AvailableVE[] = (employees || [])
                .filter((ve: any) => {
                    const machineId = ve.machine_id || ve.id;
                    return !currentIds.has(ve.id) && !currentIds.has(machineId) && ve.online_status === "online";
                })
                .map((ve: any, index: number) => ({
                    id: ve.id,
                    name: readableAvailableVEName(ve, index, optRef.current.lang),
                    machineId: ve.machine_id,
                }));

            if (available.length === 0) {
                emit(t(optRef.current.lang, "暂无可邀请的数字员工", "No digital employees available to invite"), "info");
                return { success: false, available: [], sessionId };
            }

            return { success: true, available, sessionId };
        } catch {
            emit(t(optRef.current.lang, "获取数字员工列表失败", "Failed to load digital employee list"), "error");
            return { success: false, available: [], sessionId };
        }
    }, [requireCapacity, ensureSession, loadAppModule, emit]);

    // --- inviteVE ---
    const inviteVE = useCallback(async (ctx: GroupSessionContext, veId: string): Promise<AddResult> => {
        if (!requireCapacity(ctx)) return { success: false, sessionId: ctx.sessionId };
        if (ctx.participants.includes(veId)) {
            emit(t(optRef.current.lang, "该数字员工已在会话中", "This digital employee is already in the session"), "info");
            return { success: false, sessionId: ctx.sessionId };
        }

        const sessionId = await ensureSession(ctx);
        if (!sessionId) return { success: false, sessionId: null };

        const mod = await loadAppModule();
        if (!mod) return { success: false, sessionId };

        const addFn = optRef.current.addVEToGroup || (mod as any).AddVEToGroup;
        if (!addFn) {
            emit(t(optRef.current.lang, "此功能暂不可用", "This feature is not available"), "error");
            return { success: false, sessionId };
        }

        try {
            await addFn(sessionId, veId);
            emit(t(optRef.current.lang, "✓ 已邀请加入会话", "✓ Invited to session"), "success");
            return { success: true, sessionId };
        } catch (err: any) {
            emit(
                t(optRef.current.lang, `邀请失败：${err?.message || "未知错误"}`, `Invite failed: ${err?.message || "unknown error"}`),
                "error"
            );
            return { success: false, sessionId };
        }
    }, [requireCapacity, ensureSession, loadAppModule, emit]);

    // --- addLocalAI ---
    const addLocalAI = useCallback(async (ctx: GroupSessionContext): Promise<AddResult> => {
        if (ctx.participants.includes("local-maclaw")) {
            emit(t(optRef.current.lang, "本机 AI 助手已在会话中", "Local AI assistant is already in the session"), "info");
            return { success: false, sessionId: ctx.sessionId };
        }
        if (!requireCapacity(ctx)) return { success: false, sessionId: ctx.sessionId };

        const sessionId = await ensureSession(ctx);
        if (!sessionId) return { success: false, sessionId: null };

        const mod = await loadAppModule();
        if (!mod) return { success: false, sessionId };

        const registerFn = optRef.current.registerLocalExecutor || (mod as any).RegisterLocalExecutorInGroup;
        if (!registerFn) {
            emit(t(optRef.current.lang, "此功能暂不可用", "This feature is not available"), "error");
            return { success: false, sessionId };
        }

        try {
            await registerFn(sessionId);
            emit(t(optRef.current.lang, "✓ 本机 AI 助手已加入会话", "✓ Local AI assistant added to session"), "success");
            return { success: true, sessionId };
        } catch (err: any) {
            emit(
                t(optRef.current.lang, `添加失败：${err?.message || "未知错误"}`, `Failed to add: ${err?.message || "unknown error"}`),
                "error"
            );
            return { success: false, sessionId };
        }
    }, [requireCapacity, ensureSession, loadAppModule, emit]);

    return {
        feedback,
        clearFeedback,
        checkInviteAvailability,
        inviteVE,
        addLocalAI,
    };
}
