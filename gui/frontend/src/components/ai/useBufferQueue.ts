import { useState, useCallback, useEffect } from "react";

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
    mergeAndFire: () => { mergedText: string; allFilePaths: string[] } | null;
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
    const [queue, setQueue] = useState<BufferEntry[]>([]);
    // Track whether the queue has been initialized (restored or first mutation)
    // to avoid overwriting persisted data with an empty array on mount.
    const initializedRef = { current: false };

    const addEntry = useCallback((text: string, attachments: AttachmentInfo[]) => {
        // Reject whitespace-only text with no attachments
        if (!text.trim() && attachments.length === 0) return;

        const entry: BufferEntry = {
            id: nextBufferId(),
            text,
            attachments,
            createdAt: Date.now(),
        };
        initializedRef.current = true;
        setQueue(prev => [...prev, entry]);
    }, []);

    const removeEntry = useCallback((id: string) => {
        initializedRef.current = true;
        setQueue(prev => prev.filter(e => e.id !== id));
    }, []);

    const updateEntry = useCallback(
        (id: string, text: string, attachments: AttachmentInfo[]) => {
            initializedRef.current = true;
            // If both empty, remove the entry
            if (!text.trim() && attachments.length === 0) {
                setQueue(prev => prev.filter(e => e.id !== id));
                return;
            }
            setQueue(prev =>
                prev.map(e => (e.id === id ? { ...e, text, attachments } : e)),
            );
        },
        [],
    );

    const reorderEntry = useCallback((fromIndex: number, toIndex: number) => {
        initializedRef.current = true;
        setQueue(prev => {
            if (
                fromIndex < 0 ||
                fromIndex >= prev.length ||
                toIndex < 0 ||
                toIndex >= prev.length
            ) {
                return prev;
            }
            const next = [...prev];
            const [moved] = next.splice(fromIndex, 1);
            next.splice(toIndex, 0, moved);
            return next;
        });
    }, []);

    const mergeAndFire = useCallback(() => {
        let result: { mergedText: string; allFilePaths: string[] } | null = null;

        initializedRef.current = true;
        setQueue(prev => {
            if (prev.length === 0) {
                result = null;
                return prev;
            }

            const mergedText = prev.map(e => e.text).join("\n\n---\n\n");
            const allFilePaths = prev.flatMap(e =>
                e.attachments.map(a => a.filePath),
            );

            result = { mergedText, allFilePaths };
            return []; // clear queue
        });

        return result;
    }, []);

    const clearQueue = useCallback(() => {
        initializedRef.current = true;
        setQueue([]);
    }, []);

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
            initializedRef.current = true;
            setQueue(entries);
            return entries;
        } catch {
            console.warn("Failed to restore buffer queue from localStorage");
            return [];
        }
    }, []);

    // Persist queue to localStorage after every mutation (skip initial empty state)
    useEffect(() => {
        if (!initializedRef.current) return;
        persistQueue(queue);
    }, [queue]);

    return {
        queue,
        addEntry,
        removeEntry,
        updateEntry,
        reorderEntry,
        mergeAndFire,
        clearQueue,
        restoreQueue,
    };
}
