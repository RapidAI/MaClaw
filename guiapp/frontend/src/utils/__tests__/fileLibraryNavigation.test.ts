import { afterEach, describe, expect, it } from "vitest";
import { consumePendingFileLibraryOpen, OPEN_FILE_LIBRARY_EVENT, openFileLibrary } from "../fileLibraryNavigation";

describe("fileLibraryNavigation", () => {
    afterEach(() => {
        consumePendingFileLibraryOpen();
    });

    it("stashes the pending open and notifies the shell", () => {
        const seen: unknown[] = [];
        const listener = (event: Event) => seen.push((event as CustomEvent).detail);
        window.addEventListener(OPEN_FILE_LIBRARY_EVENT, listener);
        try {
            openFileLibrary({ documentId: " doc-1 ", query: "report" });
            expect(seen).toEqual([{ documentId: "doc-1", query: "report" }]);
            expect(consumePendingFileLibraryOpen()).toEqual({ documentId: "doc-1", query: "report" });
            expect(consumePendingFileLibraryOpen()).toBeNull();
        } finally {
            window.removeEventListener(OPEN_FILE_LIBRARY_EVENT, listener);
        }
    });
});
