// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useProjectSearch } from "../useProjectSearch";
import { KnowledgeSearch, ListExperts, ListMobileLibraryItems, SearchTasks } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SearchTasks: vi.fn().mockResolvedValue([]),
    ListMobileLibraryItems: vi.fn().mockResolvedValue([]),
    KnowledgeSearch: vi.fn().mockResolvedValue([]),
    ListExperts: vi.fn().mockResolvedValue("[]"),
}));

describe("useProjectSearch", () => {
    beforeEach(() => {
        vi.mocked(SearchTasks).mockClear().mockResolvedValue([]);
        vi.mocked(ListMobileLibraryItems).mockClear().mockResolvedValue([]);
        vi.mocked(KnowledgeSearch).mockClear().mockResolvedValue([]);
        vi.mocked(ListExperts).mockClear().mockResolvedValue("[]");
        delete (window as unknown as { go?: unknown }).go;
    });

    it("puts cloud workspace file hits into cloudResults", async () => {
        const searchCloud = vi.fn().mockResolvedValue([{
            id: "cws_1/notes/预算说明.txt",
            workspace_id: "cws_1",
            workspace_name: "经营分析",
            project_path: "C:/cloud/cws_1",
            relative_path: "notes/预算说明.txt",
            title: "预算说明.txt",
            preview: "经营分析 · notes/预算说明.txt",
            match: "name",
            tags: ["cloud_workspace:cws_1"],
        }]);
        (window as unknown as { go: unknown }).go = { main: { App: { SearchCloudWorkspaceContent: searchCloud } } };

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("预算");
        });

        await waitFor(() => expect(result.current.cloudResults).toHaveLength(1));
        expect(searchCloud).toHaveBeenCalledWith("预算", 8);
        expect(result.current.cloudResults[0]).toMatchObject({
            workspaceId: "cws_1",
            relativePath: "notes/预算说明.txt",
            title: "预算说明.txt",
            match: "name",
        });
    });

    it("puts data directory workspace hits into dataDirResults", async () => {
        const searchData = vi.fn().mockResolvedValue([{
            id: "notes-1/papers/brief.md",
            task_name: "经营材料",
            project_path: "D:/data/tasks/notes-1",
            relative_path: "papers/brief.md",
            title: "brief.md",
            preview: "经营材料 · 竞品分析",
            match: "content",
            tags: ["task_management"],
        }]);
        (window as unknown as { go: unknown }).go = { main: { App: { SearchDataDirectoryWorkspaces: searchData } } };

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("竞品");
        });

        await waitFor(() => expect(result.current.dataDirResults).toHaveLength(1));
        expect(searchData).toHaveBeenCalledWith("竞品", 8);
        expect(result.current.dataDirResults[0]).toMatchObject({
            taskName: "经营材料",
            projectPath: "D:/data/tasks/notes-1",
            relativePath: "papers/brief.md",
            match: "content",
        });
    });

    it("refreshes to the task list when the header sends an empty query while open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("report", 20));
        vi.mocked(SearchTasks).mockClear();
        vi.mocked(ListMobileLibraryItems).mockClear();
        vi.mocked(KnowledgeSearch).mockClear();
        vi.mocked(ListExperts).mockClear();

        await act(async () => {
            result.current.openWithQuery("   ");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("", 20));
        expect(ListMobileLibraryItems).not.toHaveBeenCalled();
        expect(KnowledgeSearch).not.toHaveBeenCalled();
        expect(ListExperts).not.toHaveBeenCalled();
    });

    it("still searches after a header open is cancelled before the panel stays open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
            result.current.close();
        });
        vi.mocked(SearchTasks).mockClear();

        await act(async () => {
            result.current.toggle();
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("", 20));
    });

    it("debounces header query updates while search is already open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("report", 20));
        vi.mocked(SearchTasks).mockClear();

        await act(async () => {
            result.current.openWithQuery("repo");
            result.current.openWithQuery("reports");
        });
        expect(SearchTasks).not.toHaveBeenCalled();
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("reports", 20));
        expect(SearchTasks).toHaveBeenCalledTimes(1);
    });

    it("updates the draft query during IME composition without searching", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalled());
        vi.mocked(SearchTasks).mockClear();
        vi.mocked(ListMobileLibraryItems).mockClear();

        await act(async () => {
            result.current.onQueryDraft("zhuan");
        });
        expect(result.current.query).toBe("zhuan");
        expect(SearchTasks).not.toHaveBeenCalled();
        expect(ListMobileLibraryItems).not.toHaveBeenCalled();
    });
});
