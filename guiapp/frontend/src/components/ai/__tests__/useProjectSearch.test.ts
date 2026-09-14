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
