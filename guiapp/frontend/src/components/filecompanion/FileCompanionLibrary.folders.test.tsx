// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DialogProvider } from "../CustomDialog";
import { CompanionLibraryView } from "./FileCompanionLibrary";

const listLibrary = vi.fn();
const listCatalog = vi.fn();
const createFolder = vi.fn();
const renameFolder = vi.fn();
const deleteFolder = vi.fn();
const moveItem = vi.fn();

vi.mock("../../../wailsjs/go/main/App", () => ({
    AddFileCompanionGroupMember: vi.fn(),
    CreateFileCompanionGroup: vi.fn(),
    ImportFileCompanionShare: vi.fn(),
    DeleteFileCompanionGroup: vi.fn(),
    DeleteFileCompanionProject: vi.fn(),
    DeleteFileCompanionShare: vi.fn(),
    LeaveFileCompanionGroup: vi.fn(),
    ListFileCompanionGroups: vi.fn(),
    ListFileCompanionLibrary: (...args: unknown[]) => listLibrary(...args),
    ListFileCompanionCatalog: (...args: unknown[]) => listCatalog(...args),
    CreateFileCompanionFolder: (...args: unknown[]) => createFolder(...args),
    RenameFileCompanionFolder: (...args: unknown[]) => renameFolder(...args),
    DeleteFileCompanionFolder: (...args: unknown[]) => deleteFolder(...args),
    MoveFileCompanionLibraryItem: (...args: unknown[]) => moveItem(...args),
    RemoveFileCompanionGroupMember: vi.fn(),
    ShareFileCompanionDocument: vi.fn(),
    ShowItemInFolder: vi.fn(),
    SyncFileCompanionShare: vi.fn(),
    UpdateFileCompanionGroup: vi.fn(),
    UpdateFileCompanionShare: vi.fn(),
    TransferFileCompanionShareHost: vi.fn(),
    RevokeFileCompanionShareHost: vi.fn(),
    FileCompanionExportProject: vi.fn(),
}));

const papers = [
    { id: "local:a", name: "alpha.md", path: "C:/notes/alpha.md", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 12 },
    { id: "local:b", name: "beta.md", path: "C:/notes/beta.md", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 8 },
];

function renderLibrary() {
    listLibrary.mockResolvedValue({ items: papers, notice: "" });
    return render(
        <DialogProvider>
            <CompanionLibraryView
                lang="zh-Hans"
                activePath=""
                showToast={() => undefined}
                onOpen={() => undefined}
                onExit={() => undefined}
                onShareProject={() => undefined}
                onEditShare={() => undefined}
                onImportLink={() => undefined}
            />
        </DialogProvider>,
    );
}

afterEach(() => {
    cleanup();
    listLibrary.mockReset();
    listCatalog.mockReset();
    createFolder.mockReset();
    renameFolder.mockReset();
    deleteFolder.mockReset();
    moveItem.mockReset();
});

describe("companion library folders and file-name filter", () => {
    it("filters the list by file name and clears it", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [], placements: {} });
        renderLibrary();
        await screen.findByTestId("file-companion-library-card-local:a");
        expect(screen.getByTestId("file-companion-library-card-local:b")).toBeTruthy();
        fireEvent.change(screen.getByTestId("file-companion-library-filter"), { target: { value: "alpha" } });
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-library-card-local:b")).toBeNull();
        expect(screen.getByTestId("file-companion-library-filter-count").textContent).toContain("1");
        fireEvent.click(screen.getByTestId("file-companion-library-filter-clear"));
        expect(screen.getByTestId("file-companion-library-card-local:b")).toBeTruthy();
    });

    it("shows one folder level and the documents placed in it", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-fld_paper");
        expect(screen.getByTestId("file-companion-folder-all").textContent).toContain("全部");
        fireEvent.click(screen.getByTestId("file-companion-folder-fld_paper"));
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-library-card-local:b")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-folder-none"));
        expect(screen.queryByTestId("file-companion-library-card-local:a")).toBeNull();
        expect(screen.getByTestId("file-companion-library-card-local:b")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-folder-all"));
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        expect(screen.getByTestId("file-companion-library-card-local:b")).toBeTruthy();
    });

    it("asks before deleting a folder that still holds documents", async () => {
        listCatalog.mockResolvedValue({
            rev: 3,
            folders: [
                { id: "fld_paper", name: "论文" },
                { id: "fld_notes", name: "笔记" },
            ],
            placements: { "item:local:a": "fld_paper" },
        });
        deleteFolder.mockResolvedValue({
            rev: 4,
            folders: [{ id: "fld_notes", name: "笔记" }],
            placements: { "item:local:a": "fld_notes" },
        });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-delete-fld_paper");
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-fld_paper"));
        const prompt = await screen.findByTestId("file-companion-folder-delete");
        expect(prompt.textContent).toContain("论文");
        expect(prompt.textContent).toContain("合并到其他文件夹");
        expect(prompt.textContent).toContain("删除这些文档");
        expect(deleteFolder).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-cancel"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-folder-delete")).toBeNull());
        expect(deleteFolder).not.toHaveBeenCalled();

        fireEvent.click(screen.getByTestId("file-companion-folder-delete-fld_paper"));
        await screen.findByTestId("file-companion-folder-delete");
        fireEvent.change(screen.getByTestId("file-companion-folder-delete-target"), { target: { value: "fld_notes" } });
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-confirm"));
        await waitFor(() => expect(deleteFolder).toHaveBeenCalledWith("fld_paper", "merge", "fld_notes"));
    });

    it("deletes an empty folder only after confirmation", async () => {
        listCatalog.mockResolvedValue({
            rev: 5,
            folders: [{ id: "fld_empty", name: "空文件夹" }],
            placements: {},
        });
        deleteFolder.mockResolvedValue({ rev: 6, folders: [], placements: {} });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-delete-fld_empty");
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-fld_empty"));
        const confirm = await screen.findByRole("dialog", { name: "删除文件夹" });
        expect(confirm.textContent).toContain("空文件夹");
        expect(deleteFolder).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "取消" }));
        await waitFor(() => expect(screen.queryByRole("dialog", { name: "删除文件夹" })).toBeNull());
        expect(deleteFolder).not.toHaveBeenCalled();
    });
});
