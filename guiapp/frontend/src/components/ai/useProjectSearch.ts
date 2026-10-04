import { useCallback, useEffect, useRef, useState } from "react";
import { KnowledgeSearch, ListExperts, ListMobileLibraryItems, SearchTasks } from "../../../wailsjs/go/main/App";
import { formatProjectSearchTime } from "./projectSearchTime";
import { isVisibleTaskRow } from "./codingTaskMode";
import { parseExpertListJSON } from "./expertTypes";
import type { ProjectSearchArtifact } from "./ProjectSceneDetailPanel";
import { HEADER_SEARCH_CLOUD_LIMIT, searchCloudWorkspaceContent, type HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";
import { HEADER_SEARCH_DATA_DIR_LIMIT, searchDataDirectoryWorkspaces, type HeaderDataDirectoryHit } from "./dataDirectoryWorkspaceSearch";
import { HEADER_SEARCH_TASK_LIMIT, headerLibrarySearchJobs, type HeaderExpertSearchHit, type HeaderFileSearchHit, type HeaderKnowledgeSearchHit } from "./unifiedHeaderSearch";

export interface ProjectSearchItem {
    id: string;
    name: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    tags?: string[];
    last_activity?: string;
    entry_count?: number;
    pinned?: boolean;
    archived?: boolean;
    has_output?: boolean;
    source_urls?: string[];
    recent_artifacts?: ProjectSearchArtifact[];
}

export function useProjectSearch(lang: string) {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [results, setResults] = useState<ProjectSearchItem[]>([]);
    const [fileResults, setFileResults] = useState<HeaderFileSearchHit[]>([]);
    const [knowledgeResults, setKnowledgeResults] = useState<HeaderKnowledgeSearchHit[]>([]);
    const [expertResults, setExpertResults] = useState<HeaderExpertSearchHit[]>([]);
    const [cloudResults, setCloudResults] = useState<HeaderCloudWorkspaceHit[]>([]);
    const [dataDirResults, setDataDirResults] = useState<HeaderDataDirectoryHit[]>([]);
    const [loading, setLoading] = useState(false);
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const requestIdRef = useRef(0);
    const queryRef = useRef(query);
    queryRef.current = query;
    const skipOpenSearchRef = useRef(false);

    const doSearch = useCallback((q: string) => {
        const requestId = ++requestIdRef.current;
        setLoading(true);
        setFileResults([]);
        setKnowledgeResults([]);
        setExpertResults([]);
        setCloudResults([]);
        setDataDirResults([]);
        const tasksPromise = SearchTasks(q, HEADER_SEARCH_TASK_LIMIT)
            .then(r => {
                if (requestId !== requestIdRef.current) return;
                setResults(((r || []) as ProjectSearchItem[]).filter(isVisibleTaskRow));
            })
            .catch(() => { if (requestId !== requestIdRef.current) return; setResults([]); });
        const jobs = headerLibrarySearchJobs(q, {
            listMobileLibraryItems: ListMobileLibraryItems,
            knowledgeSearch: (opts) => KnowledgeSearch(opts),
            listExperts: async () => parseExpertListJSON(await ListExperts()),
        });
        const applyIfCurrent = <T,>(setter: (value: T) => void) => (value: T) => {
            if (requestId === requestIdRef.current) setter(value);
        };
        Promise.all([
            tasksPromise,
            jobs.files.then(applyIfCurrent(setFileResults)),
            jobs.knowledge.then(applyIfCurrent(setKnowledgeResults)),
            jobs.experts.then(applyIfCurrent(setExpertResults)),
            searchCloudWorkspaceContent(q, HEADER_SEARCH_CLOUD_LIMIT).then(applyIfCurrent(setCloudResults)).catch(() => {
                if (requestId === requestIdRef.current) setCloudResults([]);
            }),
            searchDataDirectoryWorkspaces(q, HEADER_SEARCH_DATA_DIR_LIMIT).then(applyIfCurrent(setDataDirResults)).catch(() => {
                if (requestId === requestIdRef.current) setDataDirResults([]);
            }),
        ]).finally(() => {
            if (requestId === requestIdRef.current) setLoading(false);
        });
    }, []);

    useEffect(() => {
        if (!open) {
            skipOpenSearchRef.current = false;
            return;
        }
        if (skipOpenSearchRef.current) {
            skipOpenSearchRef.current = false;
            return;
        }
        doSearch(queryRef.current);
    }, [open, doSearch]);
    useEffect(() => () => { if (debounceRef.current) clearTimeout(debounceRef.current); }, []);

    const onQueryDraft = useCallback((value: string) => { setQuery(value); }, []);
    const onQueryChange = useCallback((value: string) => {
        setQuery(value);
        if (debounceRef.current) clearTimeout(debounceRef.current);
        if (!value.trim()) {
            doSearch("");
            return;
        }
        debounceRef.current = setTimeout(() => doSearch(value), 250);
    }, [doSearch]);

    const close = useCallback(() => {
        requestIdRef.current += 1;
        skipOpenSearchRef.current = false;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = null;
        setLoading(false);
        setOpen(false);
        setQuery("");
        setResults([]);
        setFileResults([]);
        setKnowledgeResults([]);
        setExpertResults([]);
        setCloudResults([]);
        setDataDirResults([]);
    }, []);
    const toggle = useCallback(() => { setOpen(v => !v); }, []);
    const openWithQuery = useCallback((value = "") => {
        if (!open) {
            if (debounceRef.current) clearTimeout(debounceRef.current);
            debounceRef.current = null;
            skipOpenSearchRef.current = true;
            setOpen(true);
            const next = value.trim();
            setQuery(next);
            doSearch(next);
            return;
        }
        onQueryChange(value);
    }, [doSearch, onQueryChange, open]);
    const refresh = useCallback(() => doSearch(query), [doSearch, query]);

    const formatTime = useCallback((iso?: string) => formatProjectSearchTime(lang, iso), [lang]);

    return { open, query, results, fileResults, knowledgeResults, expertResults, cloudResults, dataDirResults, loading, toggle, close, openWithQuery, onQueryDraft, onQueryChange, refresh, formatTime };
}
