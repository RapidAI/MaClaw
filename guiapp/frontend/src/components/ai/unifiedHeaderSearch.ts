import { localizeText } from "./aiAssistantI18n";

export const HEADER_SEARCH_TASK_LIMIT = 20;
export const HEADER_SEARCH_FILE_LIST_LIMIT = 200;
export const HEADER_SEARCH_FILE_LIMIT = 8;
export const HEADER_SEARCH_KNOWLEDGE_LIMIT = 8;
export const HEADER_SEARCH_EXPERT_LIMIT = 8;

/** Placeholder and title for the assistant header search box. */
export function headerSearchPlaceholder(lang: string, ellipsis = true): string {
    return localizeText(
        lang,
        ellipsis ? "Search tasks, cloud files, workspaces, knowledge, experts..." : "Search tasks, cloud files, workspaces, knowledge, experts",
        ellipsis ? "搜索任务、云端内容、数据目录、文件、知识、专家…" : "搜索任务、云端内容、数据目录、文件、知识、专家",
        ellipsis ? "搜尋任務、雲端內容、資料目錄、檔案、知識、專家…" : "搜尋任務、雲端內容、資料目錄、檔案、知識、專家",
    );
}

export type HeaderFileSearchHit = {
    id: string;
    title: string;
    preview: string;
    type: "document" | "audio";
    updatedAt?: string;
};

export type HeaderKnowledgeSearchHit = {
    id: string;
    title: string;
    preview: string;
    sourceId: string;
    sourceTitle: string;
    resultType: string;
};

export type HeaderExpertSearchHit = {
    id: string;
    title: string;
    preview: string;
    icon: string;
    expert: Record<string, unknown> & { id: string; name: string };
};

export function libraryItemHaystack(item: {
    title?: string;
    preview?: string;
    id?: string;
    source_filename?: string;
} | null | undefined): string {
    if (!item) return "";
    return `${item.title || ""} ${item.preview || ""} ${item.source_filename || ""} ${item.id || ""}`;
}

export function matchLibraryQuery(haystack: string, query: string): boolean {
    const needle = query.trim().toLowerCase();
    if (!needle) return true;
    return haystack.toLowerCase().includes(needle);
}

export function filterMobileLibraryHits(
    items: Array<{
        id?: string;
        title?: string;
        preview?: string;
        type?: string;
        updated_at?: string;
        source_filename?: string;
    }> | null | undefined,
    query: string,
    limit = HEADER_SEARCH_FILE_LIMIT,
): HeaderFileSearchHit[] {
    const list = Array.isArray(items) ? items : [];
    const matched = list.filter((item) => matchLibraryQuery(libraryItemHaystack(item), query));
    // Annotate the callback result: without it the `type` ternary widens to `string`.
    return matched.slice(0, Math.max(0, limit)).map((item): HeaderFileSearchHit => ({
        id: String(item.id || "").trim(),
        title: String(item.title || item.source_filename || item.id || "").trim(),
        preview: String(item.preview || item.source_filename || "").trim(),
        type: item.type === "audio" ? "audio" : "document",
        updatedAt: item.updated_at,
    })).filter((item) => item.id);
}

type KnowledgeHitText = {
    result_type?: string;
    card_title?: string;
    node_title?: string;
    claim?: string;
    summary?: string;
    snippet?: string;
    citation?: string;
    subject?: string;
    predicate?: string;
    object?: string;
    library_id?: string;
    source?: { title?: string; uri?: string };
};

const ENTERPRISE_MARK = "[企业]";

/** Enterprise hits carry library_id. Older rows may still have the mark on the source title. Show it once. */
export function knowledgeEnterpriseLabel(text: string, sourceTitle?: string, libraryID?: string): string {
    const body = String(text || "").trim();
    if (!body || body.startsWith(ENTERPRISE_MARK)) return body;
    const source = String(sourceTitle || "").trim();
    const library = String(libraryID || "").trim();
    if (library || source.startsWith(ENTERPRISE_MARK)) return `${ENTERPRISE_MARK} ${body}`;
    return body;
}

/** Fact rows copy the parent card title and summary. The body is the fact itself. */
function knowledgeFactText(result: KnowledgeHitText | null | undefined): string {
    if (!result) return "";
    const snippet = String(result.snippet || "").trim();
    if (snippet) return snippet;
    const triple = [result.subject, result.predicate, result.object]
        .map((part) => String(part || "").trim())
        .filter(Boolean)
        .join(" ");
    if (triple) return triple;
    return String(result.claim || "").trim();
}

function knowledgeContextTitle(result: KnowledgeHitText): string {
    return knowledgeEnterpriseLabel(String(
        result.card_title || result.node_title || result.source?.title || "",
    ), result.source?.title, result.library_id);
}

export function knowledgeHitTitle(result: KnowledgeHitText | null | undefined): string {
    if (!result) return "";
    if (String(result.result_type || "").trim() === "fact") {
        const context = knowledgeContextTitle(result);
        if (context) return context;
        const fact = knowledgeFactText(result);
        if (fact) return fact;
    }
    return knowledgeEnterpriseLabel(String(
        result.card_title
        || result.node_title
        || result.source?.title
        || result.claim
        || result.summary
        || result.citation
        || result.source?.uri
        || "",
    ), result.source?.title, result.library_id);
}

export function knowledgeHitPreview(result: KnowledgeHitText | null | undefined): string {
    if (!result) return "";
    if (String(result.result_type || "").trim() === "fact") {
        const fact = knowledgeFactText(result);
        if (fact) return fact;
    }
    return String(result.snippet || result.summary || result.claim || result.citation || "").trim();
}

type KnowledgeSearchRow = {
    node_id?: string;
    card_id?: string;
    fact_id?: string;
    row_id?: string;
    result_type?: string;
    card_title?: string;
    node_title?: string;
    claim?: string;
    summary?: string;
    snippet?: string;
    citation?: string;
    library_id?: string;
    source?: { id?: string; title?: string; uri?: string };
};

/** Body key. A row wins over a node. A card or fact with neither stays off the default list. */
export function knowledgeHitID(result: KnowledgeSearchRow | null | undefined): string {
    if (!result) return "";
    const rowID = String(result.row_id || "").trim();
    if (rowID) return `row:${rowID}`;
    const nodeID = String(result.node_id || "").trim();
    if (nodeID) return `node:${nodeID}`;
    return "";
}

/** Row key for a search result. The type stays on the key so two evidence ids can share a suffix. */
export function knowledgeResultKey(result: KnowledgeSearchRow | null | undefined, index = 0): string {
    const kind = String(result?.result_type || "result").trim() || "result";
    const id = knowledgeHitID(result);
    return `${kind}-${id || index}`;
}

export function knowledgeSearchHits(
    results: KnowledgeSearchRow[] | null | undefined,
    limit = HEADER_SEARCH_KNOWLEDGE_LIMIT,
): HeaderKnowledgeSearchHit[] {
    const list = Array.isArray(results) ? results : [];
    const hits: HeaderKnowledgeSearchHit[] = [];
    const seen = new Set<string>();
    for (const result of list) {
        const sourceId = String(result.source?.id || "").trim();
        const id = knowledgeHitID(result);
        if (!id || seen.has(id)) continue;
        seen.add(id);
        const title = knowledgeHitTitle(result);
        if (!title && !knowledgeHitPreview(result)) continue;
        hits.push({
            id,
            title: title || id,
            preview: knowledgeHitPreview(result),
            sourceId,
            sourceTitle: String(result.source?.title || "").trim(),
            resultType: String(result.result_type || "").trim(),
        });
        if (hits.length >= Math.max(0, limit)) break;
    }
    return hits;
}

export function isOpenableExpert(expert: {
    id?: string;
    managed_industry?: boolean;
    industry_installed?: boolean;
} | null | undefined): boolean {
    if (!String(expert?.id || "").trim()) return false;
    if (expert?.managed_industry && !expert.industry_installed) return false;
    return true;
}

export function expertHaystack(expert: {
    name?: string;
    description?: string;
    id?: string;
} | null | undefined): string {
    if (!expert) return "";
    return `${expert.name || ""} ${expert.description || ""} ${expert.id || ""}`;
}

export function parseExpertSearchList(raw: unknown): Array<Record<string, unknown> & { id?: string; name?: string }> {
    if (Array.isArray(raw)) return raw as Array<Record<string, unknown> & { id?: string; name?: string }>;
    if (typeof raw !== "string" || !raw.trim()) return [];
    try {
        const parsed = JSON.parse(raw);
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}

export function filterExpertHits(
    experts: Array<{
        id?: string;
        name?: string;
        description?: string;
        icon?: string;
        managed_industry?: boolean;
        industry_installed?: boolean;
        [key: string]: unknown;
    }> | string | null | undefined,
    query: string,
    limit = HEADER_SEARCH_EXPERT_LIMIT,
): HeaderExpertSearchHit[] {
    const list = parseExpertSearchList(experts);
    return list
        .filter((expert) => isOpenableExpert(expert) && matchLibraryQuery(expertHaystack(expert), query))
        .slice(0, Math.max(0, limit))
        .map((expert) => {
            const id = String(expert.id || "").trim();
            const title = String(expert.name || id).trim();
            return {
                id,
                title,
                preview: String(expert.description || "").trim(),
                icon: String(expert.icon || "").trim(),
                expert: { ...expert, id, name: title },
            };
        });
}

export type HeaderLibrarySearchDeps = {
    listMobileLibraryItems: (limit: number) => Promise<unknown[] | null | undefined>;
    knowledgeSearch: (opts: { query: string; limit: number }) => Promise<unknown[] | null | undefined>;
    listExperts?: () => Promise<unknown[] | string | null | undefined>;
};

export function headerLibrarySearchJobs(query: string, deps: HeaderLibrarySearchDeps): {
    files: Promise<HeaderFileSearchHit[]>;
    knowledge: Promise<HeaderKnowledgeSearchHit[]>;
    experts: Promise<HeaderExpertSearchHit[]>;
} {
    const trimmed = query.trim();
    if (!trimmed) {
        return {
            files: Promise.resolve([]),
            knowledge: Promise.resolve([]),
            experts: Promise.resolve([]),
        };
    }
    const files = Promise.resolve(deps.listMobileLibraryItems(HEADER_SEARCH_FILE_LIST_LIMIT))
        .then((items) => filterMobileLibraryHits(items as Parameters<typeof filterMobileLibraryHits>[0], trimmed, HEADER_SEARCH_FILE_LIMIT))
        .catch(() => [] as HeaderFileSearchHit[]);
    const knowledge = Promise.resolve(deps.knowledgeSearch({ query: trimmed, limit: HEADER_SEARCH_KNOWLEDGE_LIMIT }))
        .then((results) => knowledgeSearchHits(results as Parameters<typeof knowledgeSearchHits>[0], HEADER_SEARCH_KNOWLEDGE_LIMIT))
        .catch(() => [] as HeaderKnowledgeSearchHit[]);
    const experts = deps.listExperts
        ? Promise.resolve(deps.listExperts())
            .then((raw) => filterExpertHits(raw as Parameters<typeof filterExpertHits>[0], trimmed, HEADER_SEARCH_EXPERT_LIMIT))
            .catch(() => [] as HeaderExpertSearchHit[])
        : Promise.resolve([] as HeaderExpertSearchHit[]);
    return { files, knowledge, experts };
}

export async function searchHeaderLibraries(
    query: string,
    deps: HeaderLibrarySearchDeps,
): Promise<{ files: HeaderFileSearchHit[]; knowledge: HeaderKnowledgeSearchHit[]; experts: HeaderExpertSearchHit[] }> {
    const jobs = headerLibrarySearchJobs(query, deps);
    const [files, knowledge, experts] = await Promise.all([jobs.files, jobs.knowledge, jobs.experts]);
    return { files, knowledge, experts };
}
