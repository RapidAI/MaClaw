export const HEADER_SEARCH_CLOUD_LIMIT = 8;

export type HeaderCloudWorkspaceHit = {
    id: string;
    workspaceId: string;
    workspaceName: string;
    projectPath: string;
    relativePath: string;
    title: string;
    preview: string;
    match: "name" | "content";
    tags: string[];
};

type CloudWorkspaceSearchBinding = (query: string, limit: number) => Promise<unknown>;

function cloudWorkspaceSearchBinding(): CloudWorkspaceSearchBinding | null {
    const app = (window as unknown as {
        go?: { main?: { App?: { SearchCloudWorkspaceContent?: CloudWorkspaceSearchBinding } } };
    }).go?.main?.App;
    const fn = app?.SearchCloudWorkspaceContent;
    return typeof fn === "function" ? fn : null;
}

export function normalizeCloudWorkspaceHits(raw: unknown): HeaderCloudWorkspaceHit[] {
    const list = Array.isArray(raw) ? raw : [];
    const hits: HeaderCloudWorkspaceHit[] = [];
    for (const row of list) {
        if (!row || typeof row !== "object") continue;
        const rec = row as Record<string, unknown>;
        const relativePath = String(rec.relative_path || rec.RelativePath || "").trim();
        const projectPath = String(rec.project_path || rec.ProjectPath || "").trim();
        const workspaceId = String(rec.workspace_id || rec.WorkspaceID || "").trim();
        const title = String(rec.title || rec.Title || relativePath).trim();
        const id = String(rec.id || rec.ID || (workspaceId && relativePath ? `${workspaceId}/${relativePath}` : "")).trim();
        if (!id || !projectPath || !relativePath || !title) continue;
        const tagsRaw = rec.tags || rec.Tags;
        const tags = Array.isArray(tagsRaw) ? tagsRaw.map((tag) => String(tag || "").trim()).filter(Boolean) : [];
        hits.push({
            id,
            workspaceId,
            workspaceName: String(rec.workspace_name || rec.WorkspaceName || "").trim(),
            projectPath,
            relativePath,
            title,
            preview: String(rec.preview || rec.Preview || "").trim(),
            match: String(rec.match || rec.Match || "") === "content" ? "content" : "name",
            tags,
        });
    }
    return hits;
}

/** Local cloud-workspace file search. A desktop build without the binding returns no hits. */
export async function searchCloudWorkspaceContent(query: string, limit = HEADER_SEARCH_CLOUD_LIMIT): Promise<HeaderCloudWorkspaceHit[]> {
    const trimmed = query.trim();
    if (!trimmed) return [];
    const fn = cloudWorkspaceSearchBinding();
    if (!fn) return [];
    return normalizeCloudWorkspaceHits(await fn(trimmed, limit));
}
