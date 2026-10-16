export const OPEN_FILE_LIBRARY_EVENT = "maclaw:open-files";

export type FileLibraryOpenDetail = {
    documentId?: string;
    query?: string;
};

let pendingFileLibraryOpen: FileLibraryOpenDetail | null = null;

export function openFileLibrary(detail: FileLibraryOpenDetail = {}): void {
    pendingFileLibraryOpen = {
        documentId: String(detail.documentId || "").trim() || undefined,
        query: detail.query == null ? undefined : String(detail.query),
    };
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(OPEN_FILE_LIBRARY_EVENT, { detail: { ...pendingFileLibraryOpen } }));
}

export function peekPendingFileLibraryOpen(): FileLibraryOpenDetail | null {
    return pendingFileLibraryOpen;
}

export function consumePendingFileLibraryOpen(): FileLibraryOpenDetail | null {
    const value = pendingFileLibraryOpen;
    pendingFileLibraryOpen = null;
    return value;
}
