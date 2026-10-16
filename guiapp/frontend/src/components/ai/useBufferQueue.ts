import { useState, useCallback, useEffect, useMemo, useRef } from "react";

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

export interface AttachmentInfo {
    filePath: string;
    thumbnailDataUrl?: string;
    isImage: boolean;
    fileName: string;
    extension: string;
}

export interface BufferEntry {
    id: string;
    text: string;
    attachments: AttachmentInfo[];
    createdAt: number;
    autoDrain?: boolean;
    /** Try to attach this entry to the active turn immediately, Codex-style. */
    steerWhenBusy?: boolean;
    sessionKey?: string;
}

export interface UseBufferQueueReturn {
    queue: BufferEntry[];
    addEntry: (text: string, attachments: AttachmentInfo[], options?: { autoDrain?: boolean; steerWhenBusy?: boolean }) => BufferEntry | null;
    /** Remove an entry and report whether this call actually owned it. */
    removeEntry: (id: string) => boolean;
    updateEntry: (id: string, text: string, attachments: AttachmentInfo[], options?: { autoDrain?: boolean; steerWhenBusy?: boolean }) => void;
    reorderEntry: (fromIndex: number, toIndex: number) => void;
    /** Extract a single entry from the queue by id, removing it. Returns null if not found. */
    extractEntry: (id: string) => BufferEntry | null;
    clearQueue: () => void;
    restoreQueue: () => BufferEntry[];
}

// ---------------------------------------------------------------------------
// Module-level counter for unique ID generation
// ---------------------------------------------------------------------------

let _bufIdCounter = 0;

/** Visible for testing — reset the counter between test runs. */
export function _resetIdCounter(): void {
    _bufIdCounter = 0;
}

function nextBufferId(): string {
    // Date + a module counter can repeat after a renderer restart, which would
    // make the backend's idempotency key mistake a new interjection for an old
    // accepted one. Add per-entry entropy while keeping the readable prefix.
    const entropy = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : Math.random().toString(36).slice(2);
    return `buf-${Date.now()}-${_bufIdCounter++}-${entropy}`;
}

function normalizePersistedAttachment(attachment: any): AttachmentInfo | null {
    if (!attachment || typeof attachment.filePath !== "string") return null;
    const filePath = attachment.filePath.trim();
    if (!filePath) return null;
    const fileName = typeof attachment.fileName === "string" && attachment.fileName.trim()
        ? attachment.fileName.trim()
        : filePath.split(/[/\\]/).pop() || filePath;
    const extension = typeof attachment.extension === "string" && attachment.extension.trim()
        ? attachment.extension.trim()
        : `.${(fileName.split(".").pop() || "").toLowerCase()}`;
    return {
        filePath,
        isImage: !!attachment.isImage,
        fileName,
        extension,
    };
}

function normalizePersistedEntry(entry: any): BufferEntry | null {
    if (!entry || typeof entry.id !== "string" || typeof entry.text !== "string") return null;
    const attachments = Array.isArray(entry.attachments)
        ? entry.attachments.map(normalizePersistedAttachment).filter((attachment: AttachmentInfo | null): attachment is AttachmentInfo => !!attachment)
        : [];
    if (!entry.text.trim() && attachments.length === 0) return null;
    return {
        id: entry.id,
        text: entry.text,
        attachments,
        createdAt: typeof entry.createdAt === "number" ? entry.createdAt : Date.now(),
        autoDrain: entry.autoDrain === false ? false : true,
        // A steer attempt is tied to the in-memory turn that was active when
        // the user pressed Enter. Never revive it after a reload and risk
        // attaching stale text to an unrelated turn; restore it as a durable
        // next-turn message instead.
        steerWhenBusy: false,
        sessionKey: normalizeSessionKey(entry.sessionKey),
    };
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

/**
 * Returns the original string if ≤80 chars, or the first 80 chars + "..."
 * if longer.
 */
export function getTextPreview(text: string): string {
    if (text.length <= 80) return text;
    return text.slice(0, 80) + "...";
}

// ---------------------------------------------------------------------------
// localStorage persistence
// ---------------------------------------------------------------------------

export const BUFFER_QUEUE_STORAGE_KEY = "ai_assistant_buffer_queue";
const DEFAULT_BUFFER_QUEUE_SESSION_KEY = "desktop-user";
const FORGET_SESSION_STATE_EVENT = "ai-assistant:forget-session-rounds";

function normalizeSessionKey(sessionKey?: string): string {
    const trimmed = typeof sessionKey === "string" ? sessionKey.trim() : "";
    return trimmed || DEFAULT_BUFFER_QUEUE_SESSION_KEY;
}

function persistQueue(queue: BufferEntry[]): void {
    try {
        const serializable = queue.map(e => ({
            ...e,
            attachments: e.attachments.map(a => {
                const { thumbnailDataUrl, ...rest } = a;
                return rest;
            }),
        }));
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify(serializable));
    } catch {
        console.warn("Failed to persist buffer queue to localStorage");
    }
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useBufferQueue(sessionKey = DEFAULT_BUFFER_QUEUE_SESSION_KEY): UseBufferQueueReturn {
    const activeSessionKey = normalizeSessionKey(sessionKey);
    // Restore persisted queue from localStorage on first mount.
    const [queue, setQueue] = useState<BufferEntry[]>(() => {
        try {
            const raw = localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY);
            if (!raw) return [];
            const parsed = JSON.parse(raw);
            if (!Array.isArray(parsed)) return [];
            return parsed.map(normalizePersistedEntry).filter((entry): entry is BufferEntry => !!entry);
        } catch {
            return [];
        }
    });
    const queueRef = useRef(queue);
    const activeSessionKeyRef = useRef(activeSessionKey);
    activeSessionKeyRef.current = activeSessionKey;

    const visibleQueue = useMemo(
        () => queue.filter(entry => normalizeSessionKey(entry.sessionKey) === activeSessionKey),
        [activeSessionKey, queue],
    );

    const commitQueue = useCallback((next: BufferEntry[]) => {
        // Persist before publishing the render state. Queue acceptance/removal
        // often follows an awaited backend handoff; deferring this write to an
        // effect leaves a reload window where accepted input can be replayed or
        // newly queued input can disappear.
        persistQueue(next);
        queueRef.current = next;
        setQueue(next);
    }, []);

    const addEntry = useCallback((text: string, attachments: AttachmentInfo[], options?: { autoDrain?: boolean; steerWhenBusy?: boolean }): BufferEntry | null => {
        // Reject whitespace-only text with no attachments
        if (!text.trim() && attachments.length === 0) return null;

        const entry: BufferEntry = {
            id: nextBufferId(),
            text,
            attachments,
            createdAt: Date.now(),
            autoDrain: options?.autoDrain === false ? false : options?.autoDrain || undefined,
            steerWhenBusy: options?.steerWhenBusy === true || undefined,
            sessionKey: activeSessionKeyRef.current,
        };
        commitQueue([...queueRef.current, entry]);
        return entry;
    }, [commitQueue]);

    const removeEntry = useCallback((id: string) => {
		const current = queueRef.current;
		if (!current.some(e => e.id === id)) return false;
		commitQueue(current.filter(e => e.id !== id));
		return true;
    }, [commitQueue]);

    const updateEntry = useCallback(
        (id: string, text: string, attachments: AttachmentInfo[], options?: { autoDrain?: boolean; steerWhenBusy?: boolean }) => {
            // If both empty, remove the entry
            if (!text.trim() && attachments.length === 0) {
                commitQueue(queueRef.current.filter(e => e.id !== id));
                return;
            }
            const autoDrain = options?.autoDrain === false ? false : options?.autoDrain || undefined;
            const steerWhenBusy = options?.steerWhenBusy === true || undefined;
            commitQueue(queueRef.current.map(e => (e.id === id ? { ...e, text, attachments, autoDrain, steerWhenBusy, sessionKey: normalizeSessionKey(e.sessionKey) } : e)));
        },
        [commitQueue],
    );

    const reorderEntry = useCallback((fromIndex: number, toIndex: number) => {
        const visible = queueRef.current.filter(e => normalizeSessionKey(e.sessionKey) === activeSessionKeyRef.current);
        const moving = visible[fromIndex];
        const target = visible[toIndex];
        if (!moving || !target) return;
        const current = queueRef.current;
        const fromAllIndex = current.findIndex(e => e.id === moving.id);
        const toAllIndex = current.findIndex(e => e.id === target.id);
        if (
            fromAllIndex < 0 ||
            fromAllIndex >= current.length ||
            toAllIndex < 0 ||
            toAllIndex >= current.length
        ) {
            return;
        }
        const next = [...current];
        const [moved] = next.splice(fromAllIndex, 1);
        next.splice(toAllIndex, 0, moved);
        commitQueue(next);
    }, [commitQueue]);

    const clearQueue = useCallback(() => {
        commitQueue(queueRef.current.filter(e => normalizeSessionKey(e.sessionKey) !== activeSessionKeyRef.current));
    }, [commitQueue]);

    const extractEntry = useCallback((id: string): BufferEntry | null => {
        const result = queueRef.current.find(e => e.id === id) || null;
        if (!result) return null;

        const next = queueRef.current.filter(e => e.id !== id);
        commitQueue(next);
        return result;
    }, [commitQueue]);

    const restoreQueue = useCallback((): BufferEntry[] => {
        try {
            const raw = localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY);
            if (!raw) return [];
            const parsed = JSON.parse(raw);
            if (!Array.isArray(parsed)) {
                console.warn("Corrupted buffer queue data in localStorage — expected array");
                return [];
            }
            const entries: BufferEntry[] = parsed.map(normalizePersistedEntry).filter((entry): entry is BufferEntry => !!entry);
            commitQueue(entries);
            return entries.filter(entry => normalizeSessionKey(entry.sessionKey) === activeSessionKeyRef.current);
        } catch {
            console.warn("Failed to restore buffer queue from localStorage");
            return [];
        }
    }, [commitQueue]);

    useEffect(() => {
        const handler = (event: Event) => {
            const rawSessionKey = String((event as CustomEvent)?.detail?.sessionKey || '').trim();
            if (!rawSessionKey) return;
            const forgottenSessionKey = normalizeSessionKey(rawSessionKey);
            const next = queueRef.current.filter(entry => normalizeSessionKey(entry.sessionKey) !== forgottenSessionKey);
            if (next.length === queueRef.current.length) return;
            console.info("[useBufferQueue] clearing queued input for forgotten session", { sessionKey: forgottenSessionKey });
            commitQueue(next);
        };
        window.addEventListener(FORGET_SESSION_STATE_EVENT, handler);
        return () => window.removeEventListener(FORGET_SESSION_STATE_EVENT, handler);
    }, [commitQueue]);

    return {
        queue: visibleQueue,
        addEntry,
        removeEntry,
        updateEntry,
        reorderEntry,
        extractEntry,
        clearQueue,
        restoreQueue,
    };
}
