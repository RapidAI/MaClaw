import { normalizeCloudWorkspaceHits, type HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";

function asRecord(value: unknown): Record<string, unknown> | null {
    if (!value || typeof value !== "object") return null;
    return value as Record<string, unknown>;
}

function localSearchHits(raw: unknown): Record<string, unknown>[] {
    const record = asRecord(raw);
    const list = Array.isArray(raw) ? raw : record?.hits;
    if (!Array.isArray(list)) return [];
    const hits: Record<string, unknown>[] = [];
    for (const row of list) {
        const hit = asRecord(row);
        if (hit) hits.push(hit);
    }
    return hits;
}

/** Citation rows carried on knowledge hits. Header display reuses knowledgeSearchHits. */
export function knowledgePayloadsFromLocalSearch(raw: unknown): Record<string, unknown>[] {
    const payloads: Record<string, unknown>[] = [];
    for (const hit of localSearchHits(raw)) {
        const knowledge = asRecord(hit.knowledge);
        if (knowledge) payloads.push(knowledge);
    }
    return payloads;
}

/** Cloud-workspace scan hits use the same openable file shape as the header column. */
export function headerCloudHitsFromLocalSearch(raw: unknown): HeaderCloudWorkspaceHit[] {
    return normalizeCloudWorkspaceHits(localSearchHits(raw).map((hit) => ({
        id: hit.locator,
        workspace_id: hit.workspace_id,
        workspace_name: hit.workspace_name,
        project_path: hit.project_path || hit.path,
        relative_path: hit.relative_path,
        title: hit.title,
        preview: hit.snippet,
        match: hit.match,
        tags: hit.tags,
    })));
}
