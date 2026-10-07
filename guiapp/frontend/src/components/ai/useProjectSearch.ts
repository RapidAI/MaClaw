import { useCallback, useEffect, useRef, useState } from "react";
import { KnowledgeSearch, ListExperts, ListMobileLibraryItems, SearchTasks } from "../../../wailsjs/go/main/App";
import { formatProjectSearchTime } from "./projectSearchTime";
import { isVisibleTaskRow } from "./codingTaskMode";
import { commitProjectSearchBatch, refreshProjectSearchCaches, resetProjectSearchOutputs, type ExpertCache, type LibraryCache } from "./projectSearchCaches";
import type { ProjectSearchItem } from "./projectSearchTypes";
import { HEADER_SEARCH_CLOUD_LIMIT, searchCloudWorkspaceContent, type HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";
import { HEADER_SEARCH_DATA_DIR_LIMIT, searchDataDirectoryWorkspaces, type HeaderDataDirectoryHit } from "./dataDirectoryWorkspaceSearch";
import { HEADER_SEARCH_TASK_LIMIT, headerLibrarySearchJobs, type HeaderExpertSearchHit, type HeaderFileSearchHit, type HeaderKnowledgeSearchHit } from "./unifiedHeaderSearch";

export type { ProjectSearchItem } from "./projectSearchTypes";

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
    const [pendingQuery, setPendingQuery] = useState("");
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const requestIdRef = useRef(0);
    const queryRef = useRef(query);
    queryRef.current = query;
    const openRef = useRef(false);
    const searchEpochRef = useRef(0);
    const skipOpenSearchRef = useRef(false);
    const issuedQueryRef = useRef("");
    const libraryCacheRef = useRef<LibraryCache>(null);
    const expertCacheRef = useRef<ExpertCache>(null);

    const doSearch = useCallback((q: string) => {
        const queryText = q.trim();
        const requestId = ++requestIdRef.current;
        issuedQueryRef.current = queryText;
        setLoading(true);
        setPendingQuery(queryText);
        const { libraryItems, expertList } = refreshProjectSearchCaches(queryText, Date.now(), libraryCacheRef, expertCacheRef, ListMobileLibraryItems, ListExperts);
        const tasksPromise = SearchTasks(queryText, HEADER_SEARCH_TASK_LIMIT)
            .then(r => ((r || []) as ProjectSearchItem[]).filter(isVisibleTaskRow))
            .catch(() => [] as ProjectSearchItem[]);
        const jobs = headerLibrarySearchJobs(queryText, {
            listMobileLibraryItems: () => libraryItems as Promise<unknown[] | null | undefined>,
            knowledgeSearch: (opts) => KnowledgeSearch(opts),
            listExperts: () => expertList,
        });
        const cloudPromise = searchCloudWorkspaceContent(queryText, HEADER_SEARCH_CLOUD_LIMIT).catch(() => [] as HeaderCloudWorkspaceHit[]);
        const dataDirPromise = searchDataDirectoryWorkspaces(queryText, HEADER_SEARCH_DATA_DIR_LIMIT).catch(() => [] as HeaderDataDirectoryHit[]);
        // Commit every section with the header together. A fast task response must
        // not replace the list while cloud or knowledge hits still belong to the previous query.
        commitProjectSearchBatch(requestId, requestIdRef, Promise.all([tasksPromise, jobs.files, jobs.knowledge, jobs.experts, cloudPromise, dataDirPromise]), ([tasks, files, knowledge, experts, cloud, dataDir]) => {
            setResults(tasks);
            setFileResults(files);
            setKnowledgeResults(knowledge);
            setExpertResults(experts);
            setCloudResults(cloud);
            setDataDirResults(dataDir);
            setQuery(queryText);
        }, () => { setPendingQuery(""); setLoading(false); });
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
        const next = value.trim();
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = null;
        if (!next) {
            doSearch("");
            return;
        }
        if (next === issuedQueryRef.current) return;
        debounceRef.current = setTimeout(() => doSearch(next), 250);
    }, [doSearch]);

    const close = useCallback(() => {
        const wasOpen = openRef.current;
        openRef.current = false;
        requestIdRef.current += 1;
        skipOpenSearchRef.current = false;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = null;
        setLoading(false);
        setOpen(false);
        setQuery("");
        setPendingQuery("");
        issuedQueryRef.current = "";
        resetProjectSearchOutputs({ setResults, setFileResults, setKnowledgeResults, setExpertResults, setCloudResults, setDataDirResults, libraryCacheRef, expertCacheRef });
        // A close while the panel is already shut must not wipe a query the
        // task pane published after this session ended.
        if (wasOpen && typeof window !== "undefined") {
            window.dispatchEvent(new CustomEvent("maclaw:task-search-closed", { detail: { epoch: searchEpochRef.current } }));
        }
    }, []);
    const toggle = useCallback(() => {
        openRef.current = !openRef.current;
        setOpen(openRef.current);
    }, []);
    const openWithQuery = useCallback((value = "", epoch?: number) => {
        if (typeof epoch === "number") searchEpochRef.current = epoch;
        const next = value.trim();
        if (!openRef.current) {
            openRef.current = true;
            if (debounceRef.current) clearTimeout(debounceRef.current);
            debounceRef.current = null;
            skipOpenSearchRef.current = true;
            setOpen(true);
            doSearch(next);
            return;
        }
        onQueryChange(value);
    }, [doSearch, onQueryChange]);
    const refresh = useCallback(() => doSearch(issuedQueryRef.current), [doSearch]);
    const formatTime = useCallback((iso?: string) => formatProjectSearchTime(lang, iso), [lang]);

    return { open, query, pendingQuery, results, fileResults, knowledgeResults, expertResults, cloudResults, dataDirResults, loading, toggle, close, openWithQuery, onQueryDraft, onQueryChange, refresh, formatTime };
}
