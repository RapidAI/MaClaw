/**
 * useGroupSessionActions - single execution layer for group chat operations.
 *
 * The hook creates a session when needed, validates preconditions, executes the
 * Wails call, and returns canonical participant identity for local AI joins.
 */

import { useCallback, useRef, useState } from "react";
import { isLocalParticipantId, localExecutorDisplayName, localExecutorParticipantID, looksLikeRawParticipantId, type LocalGroupExecutorRegistration } from "./localAIIdentity";
import { addParticipantIdentityKeys } from "./participantIdentity";

export type ActionFeedbackLevel = "info" | "success" | "error";

export interface ActionFeedback {
    message: string;
    level: ActionFeedbackLevel;
    ts: number;
}

export interface GroupSessionContext {
    sessionId: string | null;
    veId?: string;
    participants: string[];
    localParticipantIds?: string[];
    maxParticipants: number;
}

export interface InviteResult {
    success: boolean;
    available: AvailableVE[];
    sessionId: string | null;
}

export interface AvailableVE {
    id: string;
    name: string;
    machineId?: string;
}


export interface AddResult {
    success: boolean;
    sessionId: string | null;
    participantId?: string;
    displayName?: string;
}

export interface UseGroupSessionActionsOptions {
    lang?: string;
    listVirtualEmployees?: () => Promise<any[]>;
    addVEToGroup?: (sessionId: string, veId: string) => Promise<void>;
    registerLocalExecutor?: (sessionId: string) => Promise<LocalGroupExecutorRegistration | void>;
    initiateConversation?: (veId: string) => Promise<{ session_id: string }>;
}

export interface UseGroupSessionActionsResult {
    feedback: ActionFeedback | null;
    clearFeedback: () => void;
    checkInviteAvailability: (ctx: GroupSessionContext) => Promise<InviteResult>;
    inviteVE: (ctx: GroupSessionContext, veId: string) => Promise<AddResult>;
    addLocalAI: (ctx: GroupSessionContext) => Promise<AddResult>;
}

function t(lang: string | undefined, zh: string, en: string): string {
    return (!lang || lang.startsWith("zh")) ? zh : en;
}

function hasParticipant(participants: string[], participantId: string): boolean {
    const current = new Set<string>();
    participants.forEach((id) => addParticipantIdentityKeys(current, id));
    const candidate = new Set<string>();
    addParticipantIdentityKeys(candidate, participantId);
    for (const key of candidate) {
        if (current.has(key)) return true;
    }
    return false;
}

function participantIdentitySet(values: unknown[]): Set<string> {
    const keys = new Set<string>();
    values.forEach((value) => addParticipantIdentityKeys(keys, value));
    return keys;
}

function distinctParticipantCount(values: unknown[]): number {
    const seen = new Set<string>();
    let count = 0;
    for (const value of values) {
        const before = seen.size;
        addParticipantIdentityKeys(seen, value);
        if (seen.size !== before) count++;
    }
    return count;
}

function hasLocalParticipant(ctx: GroupSessionContext): boolean {
    return (ctx.participants || []).some((id) => isLocalParticipantId(id, ctx.localParticipantIds));
}

function readableAvailableVEName(ve: any, index: number, lang: string | undefined): string {
    const name = String(ve?.name || "").trim();
    const id = String(ve?.id || "").trim();
    const machineId = String(ve?.machine_id || "").trim();
    if (name && name !== id && name !== machineId && !looksLikeRawParticipantId(name)) return name;
    const ordinal = index + 1;
    return t(lang, "数字员工 " + ordinal, "Digital employee " + ordinal);
}

export function useGroupSessionActions(options: UseGroupSessionActionsOptions = {}): UseGroupSessionActionsResult {
    const [feedback, setFeedback] = useState<ActionFeedback | null>(null);
    const optRef = useRef(options);
    optRef.current = options;

    const emit = useCallback((message: string, level: ActionFeedbackLevel) => {
        setFeedback({ message, level, ts: Date.now() });
    }, []);

    const clearFeedback = useCallback(() => setFeedback(null), []);

    const loadAppModule = useCallback(async () => {
        try {
            return await import("../../../wailsjs/go/main/App");
        } catch {
            emit(t(optRef.current.lang, "功能模块加载失败", "Failed to load module"), "error");
            return null;
        }
    }, [emit]);

    const ensureSession = useCallback(async (ctx: GroupSessionContext): Promise<string | null> => {
        if (ctx.sessionId) return ctx.sessionId;
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
            emit(t(optRef.current.lang, "建立会话失败：" + (err?.message || ""), "Session creation failed: " + (err?.message || "")), "error");
            return null;
        }
    }, [loadAppModule, emit]);

    const requireCapacity = useCallback((ctx: GroupSessionContext): boolean => {
        if (distinctParticipantCount(ctx.participants) >= ctx.maxParticipants) {
            emit(t(optRef.current.lang, "群聊人数已满（最大 " + ctx.maxParticipants + " 人）", "Group is full (max " + ctx.maxParticipants + ")"), "error");
            return false;
        }
        return true;
    }, [emit]);

    const checkInviteAvailability = useCallback(async (ctx: GroupSessionContext): Promise<InviteResult> => {
        if (!requireCapacity(ctx)) return { success: false, available: [], sessionId: ctx.sessionId };
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
            const currentIds = participantIdentitySet(ctx.participants);
            const available: AvailableVE[] = (employees || [])
                .filter((ve: any) => {
                    const candidateIds = participantIdentitySet([ve.id, ve.machine_id || ve.id]);
                    return ![...candidateIds].some((id) => currentIds.has(id)) && ve.online_status === "online";
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

    const inviteVE = useCallback(async (ctx: GroupSessionContext, veId: string): Promise<AddResult> => {
        if (!requireCapacity(ctx)) return { success: false, sessionId: ctx.sessionId };
        if (hasParticipant(ctx.participants, veId)) {
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
            emit(t(optRef.current.lang, "已邀请加入会话", "Invited to session"), "success");
            return { success: true, sessionId };
        } catch (err: any) {
            emit(t(optRef.current.lang, "邀请失败：" + (err?.message || "未知错误"), "Invite failed: " + (err?.message || "unknown error")), "error");
            return { success: false, sessionId };
        }
    }, [requireCapacity, ensureSession, loadAppModule, emit]);

    const addLocalAI = useCallback(async (ctx: GroupSessionContext): Promise<AddResult> => {
        if (hasLocalParticipant(ctx)) {
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
            const registered = await registerFn(sessionId);
            const participantId = localExecutorParticipantID(registered);
            if (!participantId) {
                emit(t(optRef.current.lang, "添加失败：缺少本机 AI 参与者 ID", "Failed to add: missing local AI participant ID"), "error");
                return { success: false, sessionId };
            }
            emit(t(optRef.current.lang, "本机 AI 助手已加入会话", "Local AI assistant added to session"), "success");
            return { success: true, sessionId, participantId, displayName: localExecutorDisplayName(registered) };
        } catch (err: any) {
            emit(t(optRef.current.lang, "添加失败：" + (err?.message || "未知错误"), "Failed to add: " + (err?.message || "unknown error")), "error");
            return { success: false, sessionId };
        }
    }, [requireCapacity, ensureSession, loadAppModule, emit]);

    return { feedback, clearFeedback, checkInviteAvailability, inviteVE, addLocalAI };
}
