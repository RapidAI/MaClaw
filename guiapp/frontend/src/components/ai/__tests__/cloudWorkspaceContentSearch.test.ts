import { describe, expect, it } from "vitest";
import { normalizeCloudWorkspaceHits } from "../cloudWorkspaceContentSearch";
import {
    clearParkedCloudWorkspaceFileOpen,
    cloudWorkspaceFileOpenMatches,
    parkCloudWorkspaceFileOpen,
    peekParkedCloudWorkspaceFileOpen,
    takeParkedCloudWorkspaceFileOpen,
} from "../cloudWorkspaceFileOpen";

describe("normalizeCloudWorkspaceHits", () => {
    it("keeps file hits and drops rows that cannot be opened", () => {
        const hits = normalizeCloudWorkspaceHits([
            {
                id: "cws_a/papers/brief.md",
                workspace_id: "cws_a",
                workspace_name: "经营材料",
                project_path: "D:/tasks/notes",
                relative_path: "papers/brief.md",
                title: "brief.md",
                preview: "经营材料 · 竞品分析",
                match: "content",
                tags: ["cloud_workspace:cws_a", ""],
            },
            { title: "missing path" },
            null,
        ]);
        expect(hits).toEqual([
            expect.objectContaining({
                id: "cws_a/papers/brief.md",
                workspaceId: "cws_a",
                workspaceName: "经营材料",
                projectPath: "D:/tasks/notes",
                relativePath: "papers/brief.md",
                match: "content",
                tags: ["cloud_workspace:cws_a"],
            }),
        ]);
    });
});

describe("cloudWorkspaceFileOpenMatches", () => {
    const request = {
        projectPath: "D:/tasks/notes",
        relativePath: "papers/brief.md",
        workspaceId: "cws_a",
        fileName: "brief.md",
    };

    it("matches the task path or the same workspace id", () => {
        expect(cloudWorkspaceFileOpenMatches(request, { projectPath: "D:\\tasks\\notes\\" })).toBe(true);
        expect(cloudWorkspaceFileOpenMatches(request, {
            projectPath: "C:/other",
            workspaceId: "cws_a",
        })).toBe(true);
        expect(cloudWorkspaceFileOpenMatches(request, {
            projectPath: "C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_a",
        })).toBe(true);
        expect(cloudWorkspaceFileOpenMatches(request, { projectPath: "D:/tasks/other", workspaceId: "cws_b" })).toBe(false);
        expect(cloudWorkspaceFileOpenMatches(null, { projectPath: "D:/tasks/notes" })).toBe(false);
    });

    it("parks one file and replaces it with the next choice", () => {
        clearParkedCloudWorkspaceFileOpen();
        parkCloudWorkspaceFileOpen(request);
        expect(peekParkedCloudWorkspaceFileOpen()).toEqual(request);
        parkCloudWorkspaceFileOpen({ ...request, relativePath: "papers/other.md", fileName: "other.md" });
        expect(takeParkedCloudWorkspaceFileOpen()?.relativePath).toBe("papers/other.md");
        expect(peekParkedCloudWorkspaceFileOpen()).toBeNull();
    });
});
