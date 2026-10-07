export const HEADER_SEARCH_DATA_DIR_LIMIT = 8;

export type HeaderDataDirectoryHit = {
    id: string;
    taskName: string;
    projectPath: string;
    relativePath: string;
    title: string;
    preview: string;
    match: "name" | "content";
    tags: string[];
};

type DataDirectorySearchBinding = (query: string, limit: number) => Promise<unknown>;

function dataDirectorySearchBinding(): DataDirectorySearchBinding | null {
    const app = (window as unknown as {
        go?: { main?: { App?: { SearchDataDirectoryWorkspaces?: DataDirectorySearchBinding } } };
    }).go?.main?.App;
    const fn = app?.SearchDataDirectoryWorkspaces;
    return typeof fn === "function" ? fn : null;
}

export function normalizeDataDirectoryHits(raw: unknown): HeaderDataDirectoryHit[] {
    const list = Array.isArray(raw) ? raw : [];
    const hits: HeaderDataDirectoryHit[] = [];
    for (const row of list) {
        if (!row || typeof row !== "object") continue;
        const rec = row as Record<string, unknown>;
        const relativePath = String(rec.relative_path || rec.RelativePath || "").trim();
        const projectPath = String(rec.project_path || rec.ProjectPath || "").trim();
        const title = String(rec.title || rec.Title || relativePath).trim();
        const id = String(rec.id || rec.ID || (projectPath && relativePath ? `${projectPath}/${relativePath}` : "")).trim();
        if (!id || !projectPath || !relativePath || !title) continue;
        const tagsRaw = rec.tags || rec.Tags;
        const tags = Array.isArray(tagsRaw) ? tagsRaw.map((tag) => String(tag || "").trim()).filter(Boolean) : [];
        hits.push({
            id,
            taskName: String(rec.task_name || rec.TaskName || "").trim(),
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

/** Local data-directory workspace search. A desktop build without the binding returns no hits. */
export async function searchDataDirectoryWorkspaces(query: string, limit = HEADER_SEARCH_DATA_DIR_LIMIT): Promise<HeaderDataDirectoryHit[]> {
    const trimmed = query.trim();
    if (!trimmed) return [];
    const fn = dataDirectorySearchBinding();
    if (!fn) return [];
    return normalizeDataDirectoryHits(await fn(trimmed, limit));
}
