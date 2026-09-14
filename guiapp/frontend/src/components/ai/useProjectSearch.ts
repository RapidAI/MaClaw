import { useCallback, useEffect, useRef, useState } from "react";
import { KnowledgeSearch, ListExperts, ListMobileLibraryItems, SearchTasks } from "../../../wailsjs/go/main/App";
import { localizeText } from "./aiAssistantI18n";
import { isVisibleTaskRow } from "./codingTaskMode";
import { parseExpertListJSON } from "./expertTypes";
import type { ProjectSearchArtifact } from "./ProjectSceneDetailPanel";
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
        const tasksPromise = SearchTasks(q, HEADER_SEARCH_TASK_LIMIT)
            .then(r => {
                if (requestId !== requestIdRef.current) return;
                setResults(((r || []) as ProjectSearchItem[]).filter(isVisibleTaskRow));
            })
            .catch(() => {
                if (requestId !== requestIdRef.current) return;
                setResults([]);
            });
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

    const onQueryDraft = useCallback((value: string) => {
        setQuery(value);
    }, []);
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
    }, []);
    const toggle = useCallback(() => { setOpen(v => !v); }, []);
    const openWithQuery = useCallback((value = "") => {
        const next = value.trim();
        if (debounceRef.current) clearTimeout(debounceRef.current);
        if (!open) skipOpenSearchRef.current = true;
        setOpen(true);
        setQuery(next);
        doSearch(next);
    }, [doSearch, open]);
    const refresh = useCallback(() => doSearch(query), [doSearch, query]);

    const formatTime = useCallback((iso?: string): string => {
        if (!iso) return "";
        try {
            const d = new Date(iso);
            const diffH = Math.floor((Date.now() - d.getTime()) / 3600000);
            if (diffH < 1) return localizeText(lang, "just now", "\u521a\u521a");
            if (diffH < 24) return `${diffH}${localizeText(lang, "h ago", "\u5c0f\u65f6\u524d")}`;
            const diffD = Math.floor(diffH / 24);
            return diffD < 7 ? `${diffD}${localizeText(lang, "d ago", "\u5929\u524d")}` : d.toLocaleDateString();
        } catch { return ""; }
    }, [lang]);

    return { open, query, results, fileResults, knowledgeResults, expertResults, loading, toggle, close, openWithQuery, onQueryDraft, onQueryChange, refresh, formatTime };
}
