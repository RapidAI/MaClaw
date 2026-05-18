import { useState, useCallback, useEffect, useRef } from "react";

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
}

export interface UseBufferQueueReturn {
    queue: BufferEntry[];
    addEntry: (text: string, attachments: AttachmentInfo[]) => void;
    removeEntry: (id: string) => void;
    updateEntry: (id: string, text: string, attachments: AttachmentInfo[]) => void;
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
    return `buf-${Date.now()}-${_bufIdCounter++}`;
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

export function useBufferQueue(): UseBufferQueueReturn {
    // Restore persisted queue from localStorage on first mount.
    const [queue, setQueue] = useState<BufferEntry[]>(() => {
        try {
            const raw = localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY);
            if (!raw) return [];
            const parsed = JSON.parse(raw);
            if (!Array.isArray(parsed)) return [];
            return parsed.filter(
                (e: any) => e && typeof e.id === "string" && typeof e.text === "string",
            ) as BufferEntry[];
        } catch {
            return [];
        }
    });
    // Skip persisting on the very first render — the lazy initializer already
    // loaded from localStorage, so writing it back would be a no-op.
    // Subsequent queue mutations set initializedRef via their callbacks.
    const initializedRef = useRef(false);
    const queueRef = useRef(queue);

    const commitQueue = useCallback((next: BufferEntry[]) => {
        initializedRef.current = true;
        queueRef.current = next;
        setQueue(next);
    }, []);

    const addEntry = useCallback((text: string, attachments: AttachmentInfo[]) => {
        // Reject whitespace-only text with no attachments
        if (!text.trim() && attachments.length === 0) return;

        const entry: BufferEntry = {
            id: nextBufferId(),
            text,
            attachments,
            createdAt: Date.now(),
        };
        commitQueue([...queueRef.current, entry]);
    }, [commitQueue]);

    const removeEntry = useCallback((id: string) => {
        commitQueue(queueRef.current.filter(e => e.id !== id));
    }, [commitQueue]);

    const updateEntry = useCallback(
        (id: string, text: string, attachments: AttachmentInfo[]) => {
            // If both empty, remove the entry
            if (!text.trim() && attachments.length === 0) {
                commitQueue(queueRef.current.filter(e => e.id !== id));
                return;
            }
            commitQueue(queueRef.current.map(e => (e.id === id ? { ...e, text, attachments } : e)));
        },
        [commitQueue],
    );

    const reorderEntry = useCallback((fromIndex: number, toIndex: number) => {
        const current = queueRef.current;
        if (
            fromIndex < 0 ||
            fromIndex >= current.length ||
            toIndex < 0 ||
            toIndex >= current.length
        ) {
            return;
        }
        const next = [...current];
        const [moved] = next.splice(fromIndex, 1);
        next.splice(toIndex, 0, moved);
        commitQueue(next);
    }, [commitQueue]);

    const clearQueue = useCallback(() => {
        commitQueue([]);
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
            const entries: BufferEntry[] = parsed.filter(
                (e: any) => e && typeof e.id === "string" && typeof e.text === "string",
            );
            commitQueue(entries);
            return entries;
        } catch {
            console.warn("Failed to restore buffer queue from localStorage");
            return [];
        }
    }, [commitQueue]);

    // Persist queue to localStorage after every mutation.
    // Skip the first run (mount) — the lazy initializer already loaded from
    // localStorage, writing it back would be a no-op.
    useEffect(() => {
        if (!initializedRef.current) {
            initializedRef.current = true;
            return;
        }
        persistQueue(queue);
    }, [queue]);

    return {
        queue,
        addEntry,
        removeEntry,
        updateEntry,
        reorderEntry,
        extractEntry,
        clearQueue,
        restoreQueue,
    };
}
