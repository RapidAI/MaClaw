import { describe, expect, it } from "vitest";
import { normalizeDataDirectoryHits } from "../dataDirectoryWorkspaceSearch";

describe("normalizeDataDirectoryHits", () => {
    it("keeps sandbox file hits and drops rows that cannot be opened", () => {
        const hits = normalizeDataDirectoryHits([
            {
                id: "notes-1/papers/brief.md",
                task_name: "经营材料",
                project_path: "D:/data/tasks/notes-1",
                relative_path: "papers/brief.md",
                title: "brief.md",
                preview: "经营材料 · 竞品分析",
                match: "content",
                tags: ["task_management", ""],
            },
            { title: "missing path" },
            null,
        ]);
        expect(hits).toEqual([
            expect.objectContaining({
                id: "notes-1/papers/brief.md",
                taskName: "经营材料",
                projectPath: "D:/data/tasks/notes-1",
                relativePath: "papers/brief.md",
                match: "content",
                tags: ["task_management"],
            }),
        ]);
    });
});
