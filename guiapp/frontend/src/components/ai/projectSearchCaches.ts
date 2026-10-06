import type { MutableRefObject } from "react";
import type { HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";
import type { HeaderDataDirectoryHit } from "./dataDirectoryWorkspaceSearch";
import { parseExpertListJSON } from "./expertTypes";
import type { ProjectSearchItem } from "./projectSearchTypes";
import type { HeaderExpertSearchHit, HeaderFileSearchHit, HeaderKnowledgeSearchHit } from "./unifiedHeaderSearch";
import { HEADER_SEARCH_FILE_LIST_LIMIT } from "./unifiedHeaderSearch";

export type LibraryCache = { at: number; items: Promise<unknown> } | null;
export type ExpertCache = { at: number; experts: Promise<unknown> } | null;

export function refreshProjectSearchCaches(
    queryText: string,
    now: number,
    libraryCacheRef: MutableRefObject<LibraryCache>,
    expertCacheRef: MutableRefObject<ExpertCache>,
    listLibraryItems: (limit: number) => Promise<unknown>,
    listExperts: () => Promise<string>,
): { libraryItems: Promise<unknown>; expertList: Promise<unknown[] | string | null | undefined> } {
    if (queryText && (!libraryCacheRef.current || now - libraryCacheRef.current.at > 2000)) {
        try {
            const items = Promise.resolve(listLibraryItems(HEADER_SEARCH_FILE_LIST_LIMIT)).catch((error: unknown) => {
                libraryCacheRef.current = null;
                throw error;
            });
            libraryCacheRef.current = { at: now, items };
        } catch {
            libraryCacheRef.current = null;
        }
    }
    if (queryText && (!expertCacheRef.current || now - expertCacheRef.current.at > 2000)) {
        try {
            const experts = Promise.resolve(listExperts()).then(raw => parseExpertListJSON(raw)).catch((error: unknown) => {
                expertCacheRef.current = null;
                throw error;
            });
            expertCacheRef.current = { at: now, experts };
        } catch {
            expertCacheRef.current = null;
        }
    }
    return {
        libraryItems: libraryCacheRef.current?.items ?? Promise.resolve([]),
        expertList: (expertCacheRef.current?.experts ?? Promise.resolve([])) as Promise<unknown[] | string | null | undefined>,
    };
}

export function commitProjectSearchBatch<T>(
    requestId: number,
    requestIdRef: MutableRefObject<number>,
    batch: Promise<T>,
    apply: (parts: T) => void,
    finish: () => void,
) {
    void batch.then((parts) => {
        if (requestId !== requestIdRef.current) return;
        apply(parts);
        finish();
    }).catch(() => {
        if (requestId !== requestIdRef.current) return;
        finish();
    });
}

export function resetProjectSearchOutputs(args: {
    setResults: (value: ProjectSearchItem[]) => void;
    setFileResults: (value: HeaderFileSearchHit[]) => void;
    setKnowledgeResults: (value: HeaderKnowledgeSearchHit[]) => void;
    setExpertResults: (value: HeaderExpertSearchHit[]) => void;
    setCloudResults: (value: HeaderCloudWorkspaceHit[]) => void;
    setDataDirResults: (value: HeaderDataDirectoryHit[]) => void;
    libraryCacheRef: MutableRefObject<LibraryCache>;
    expertCacheRef: MutableRefObject<ExpertCache>;
}) {
    args.setResults([]);
    args.setFileResults([]);
    args.setKnowledgeResults([]);
    args.setExpertResults([]);
    args.setCloudResults([]);
    args.setDataDirResults([]);
    args.libraryCacheRef.current = null;
    args.expertCacheRef.current = null;
}
