// @vitest-environment jsdom
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DialogProvider } from "../CustomDialog";
import { CompanionLibraryView, CompanionPlaceDialog, companionLibraryDocTitle, companionLibraryDragType } from "./FileCompanionLibrary";

const listLibrary = vi.fn();
const listCatalog = vi.fn();
const createFolder = vi.fn();
const renameFolder = vi.fn();
const deleteFolder = vi.fn();
const moveItem = vi.fn();
const setDefault = vi.fn();
const folderForPath = vi.fn();

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
    SetFileCompanionDefaultFolder: (...args: unknown[]) => setDefault(...args),
    FileCompanionFolderForPath: (...args: unknown[]) => folderForPath(...args),
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

vi.mock("../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => () => undefined),
    EventsOff: vi.fn(),
}));

const papers = [
    { id: "local:a", name: "alpha.md", path: "C:/notes/alpha.md", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 12 },
    { id: "local:b", name: "beta.md", path: "C:/notes/beta.md", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 8 },
];

function renderLibrary(items: ReadonlyArray<Record<string, unknown>> = papers) {
    listLibrary.mockResolvedValue({ items, notice: "" });
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
    setDefault.mockReset();
    folderForPath.mockReset();
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

    it("filters the filename list to local or cloud projects", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [], placements: {} });
        renderLibrary([
            ...papers,
            { id: "cloud:c", name: "cloud.pdf", path: "C:/notes/cloud.pdf", source: "cloud", openedAt: "2026-10-25T00:00:00Z", size: 20 },
        ]);
        await screen.findByTestId("file-companion-library-card-local:a");
        expect(screen.getByTestId("file-companion-library-card-cloud:c")).toBeTruthy();
        expect(screen.getByTestId("file-companion-library-source-all").getAttribute("aria-pressed")).toBe("true");
        expect(screen.getByTestId("file-companion-library-source-local").textContent).toBe("本地");
        expect(screen.getByTestId("file-companion-library-source-cloud").textContent).toBe("云端");

        fireEvent.click(screen.getByTestId("file-companion-library-source-local"));
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-library-cloud")).toBeNull();
        expect(screen.queryByTestId("file-companion-library-card-cloud:c")).toBeNull();

        fireEvent.click(screen.getByTestId("file-companion-library-source-cloud"));
        expect(screen.queryByTestId("file-companion-library-local")).toBeNull();
        expect(screen.getByTestId("file-companion-library-card-cloud:c")).toBeTruthy();

        fireEvent.change(screen.getByTestId("file-companion-library-filter"), { target: { value: "alpha" } });
        expect(screen.getByTestId("file-companion-library-filter-empty").textContent).toContain("没有匹配的文件");

        fireEvent.click(screen.getByTestId("file-companion-library-source-all"));
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-library-card-cloud:c")).toBeNull();
    });

    it("does not call a folder empty when it only holds the other source", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:cloud:c": "fld_paper" },
        });
        renderLibrary([
            ...papers,
            { id: "cloud:c", name: "cloud.pdf", path: "C:/notes/cloud.pdf", source: "cloud", openedAt: "2026-10-25T00:00:00Z", size: 20 },
        ]);
        await screen.findByTestId("file-companion-folder-fld_paper");
        fireEvent.click(screen.getByTestId("file-companion-folder-fld_paper"));
        expect(screen.getByTestId("file-companion-library-card-cloud:c")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-library-source-local"));
        expect(screen.getByTestId("file-companion-library-filter-empty").textContent).toContain("没有匹配的文件");
        expect(screen.queryByText("这个文件夹里还没有文档")).toBeNull();
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
        expect(screen.queryByText("本机文档会清除伴读数据。你的云端分享会取消，文档回到未分类。收到的分享会从列表移除。")).toBeNull();
        expect(screen.getByTestId("file-companion-folder-delete-confirm").classList.contains("file-companion-folder-danger")).toBe(false);
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

    it("selects a folder after creating it and refuses a reserved name", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [], placements: {} });
        createFolder.mockResolvedValue({ rev: 2, folders: [{ id: "fld_new", name: "论文" }], placements: {} });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-all");
        fireEvent.change(screen.getByTestId("file-companion-folder-name"), { target: { value: "未分类" } });
        fireEvent.click(screen.getByTestId("file-companion-folder-add"));
        expect(createFolder).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId("file-companion-folder-name"), { target: { value: "论文" } });
        fireEvent.click(screen.getByTestId("file-companion-folder-add"));
        await waitFor(() => expect(createFolder).toHaveBeenCalledWith("论文"));
        const created = await screen.findByTestId("file-companion-folder-fld_new");
        expect(created.closest("[role=treeitem]")?.getAttribute("aria-selected")).toBe("true");
    });

    it("removes the documents when that choice is confirmed", async () => {
        listCatalog.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        deleteFolder.mockResolvedValue({ rev: 4, folders: [], placements: {} });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-delete-fld_paper");
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-fld_paper"));
        await screen.findByTestId("file-companion-folder-delete");
        expect(screen.getByText("本机文档会清除伴读数据。你的云端分享会取消，文档回到未分类。收到的分享会从列表移除。")).toBeTruthy();
        expect(screen.getByTestId("file-companion-folder-delete-confirm").classList.contains("file-companion-folder-danger")).toBe(true);
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-remove"));
        fireEvent.click(screen.getByTestId("file-companion-folder-delete-confirm"));
        await waitFor(() => expect(deleteFolder).toHaveBeenCalledWith("fld_paper", "remove", ""));
    });

    it("escape while renaming leaves the library open", async () => {
        const onExit = vi.fn();
        listCatalog.mockResolvedValue({ rev: 1, folders: [{ id: "fld_paper", name: "论文" }], placements: {} });
        listLibrary.mockResolvedValue({ items: papers, notice: "" });
        render(
            <DialogProvider>
                <CompanionLibraryView
                    lang="zh-Hans"
                    activePath=""
                    showToast={() => undefined}
                    onOpen={() => undefined}
                    onExit={onExit}
                    onShareProject={() => undefined}
                    onEditShare={() => undefined}
                    onImportLink={() => undefined}
                />
            </DialogProvider>,
        );
        fireEvent.click(await screen.findByTestId("file-companion-folder-rename-fld_paper"));
        const input = await screen.findByTestId("file-companion-folder-rename-input-fld_paper");
        fireEvent.keyDown(input, { key: "Escape" });
        expect(screen.queryByTestId("file-companion-folder-rename-input-fld_paper")).toBeNull();
        expect(onExit).not.toHaveBeenCalled();
    });

    it("blur while renaming cancels without saving", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [{ id: "fld_paper", name: "论文" }], placements: {} });
        renderLibrary();
        fireEvent.click(await screen.findByTestId("file-companion-folder-rename-fld_paper"));
        const input = await screen.findByTestId("file-companion-folder-rename-input-fld_paper");
        fireEvent.change(input, { target: { value: "改名" } });
        fireEvent.blur(input);
        expect(screen.queryByTestId("file-companion-folder-rename-input-fld_paper")).toBeNull();
        expect(renameFolder).not.toHaveBeenCalled();
    });

    it("saves a rename from the folder button", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [{ id: "fld_paper", name: "论文" }], placements: {} });
        renameFolder.mockResolvedValue({ rev: 2, folders: [{ id: "fld_paper", name: "新名字" }], placements: {} });
        renderLibrary();
        fireEvent.click(await screen.findByTestId("file-companion-folder-rename-fld_paper"));
        const input = await screen.findByTestId("file-companion-folder-rename-input-fld_paper");
        fireEvent.change(input, { target: { value: "新名字" } });
        const button = screen.getByTestId("file-companion-folder-rename-fld_paper");
        expect(button.textContent).toBe("保存");
        fireEvent.mouseDown(button);
        fireEvent.click(button);
        await waitFor(() => expect(renameFolder).toHaveBeenCalledWith("fld_paper", "新名字"));
    });

    it("keeps a new folder when an older catalog list returns", async () => {
        let resolveList: (value: unknown) => void = () => undefined;
        listCatalog.mockReturnValue(new Promise((resolve) => {
            resolveList = resolve;
        }));
        createFolder.mockResolvedValue({ rev: 2, folders: [{ id: "fld_new", name: "论文" }], placements: {} });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-name");
        fireEvent.change(screen.getByTestId("file-companion-folder-name"), { target: { value: "论文" } });
        fireEvent.click(screen.getByTestId("file-companion-folder-add"));
        expect(await screen.findByTestId("file-companion-folder-fld_new")).toBeTruthy();
        await act(async () => {
            resolveList({ rev: 0, folders: [], placements: {} });
        });
        expect(screen.getByTestId("file-companion-folder-fld_new")).toBeTruthy();
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

    it("keeps the folder name field focused when the document list arrives", async () => {
        let resolveLibrary: (value: unknown) => void = () => undefined;
        listLibrary.mockReturnValue(new Promise((resolve) => {
            resolveLibrary = resolve;
        }));
        listCatalog.mockResolvedValue({ rev: 0, folders: [], placements: {} });
        render(
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
        const name = await screen.findByTestId("file-companion-folder-name");
        name.focus();
        await act(async () => {
            resolveLibrary({ items: papers, notice: "" });
        });
        await screen.findByTestId("file-companion-library-card-local:a");
        expect(document.activeElement).toBe(name);
    });

    it("drops a document onto a folder and onto 未分类", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        moveItem.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper", "item:local:b": "fld_paper" },
        });
        renderLibrary();
        const card = await screen.findByTestId("file-companion-library-card-local:b");
        const handle = screen.getByTestId("file-companion-library-drag-local:b");
        const meta = screen.getByTestId("file-companion-library-meta-local:b");
        expect(card.hasAttribute("draggable")).toBe(false);
        expect(handle.getAttribute("draggable")).toBe("true");
        expect(handle.getAttribute("title")).toBe("拖到左侧文件夹以分类");
        expect(meta.getAttribute("draggable")).not.toBe("true");
        expect(screen.getByTestId("file-companion-library-item-local:b").hasAttribute("draggable")).toBe(false);
        const data = libraryDragTransfer();
        card.getBoundingClientRect = () => ({ top: 100, left: 40, bottom: 160, right: 340, width: 300, height: 60, x: 40, y: 100, toJSON: () => ({}) });
        const start = createEvent.dragStart(handle, { dataTransfer: data });
        Object.defineProperty(start, "clientX", { configurable: true, value: 70 });
        Object.defineProperty(start, "clientY", { configurable: true, value: 128 });
        fireEvent(handle, start);
        expect(data.imageX).toBe(30);
        expect(data.imageY).toBe(28);
        expect(card.style.contentVisibility).toBe("visible");
        expect(document.querySelector(".file-companion-library-dialog")?.getAttribute("data-library-drag")).toBe("true");
        await act(async () => {
            await Promise.resolve();
        });
        expect(data.getData(companionLibraryDragType)).toBe("local:b");
        expect(card.getAttribute("data-dragging")).toBe("true");
        expect(card.style.contentVisibility).toBe("visible");

        const paper = folderRow("file-companion-folder-fld_paper");
        fireEvent.dragOver(paper, { dataTransfer: data });
        expect(paper.getAttribute("data-drop")).toBe("over");
        fireEvent.drop(paper, { dataTransfer: data });
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("local:b", "C:/notes/beta.md", "fld_paper"));
        expect(paper.getAttribute("data-drop")).toBeNull();
        expect(card.style.contentVisibility).toBe("");
        expect(card.getAttribute("data-dragging")).toBeNull();
        expect(document.querySelector(".file-companion-library-dialog")?.hasAttribute("data-library-drag")).toBe(false);

        moveItem.mockClear();
        moveItem.mockResolvedValue({
            rev: 4,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:b": "fld_paper" },
        });
        const back = libraryDragTransfer();
        const handleA = screen.getByTestId("file-companion-library-drag-local:a");
        fireEvent.dragStart(handleA, { dataTransfer: back });
        await act(async () => {
            await Promise.resolve();
        });
        expect(back.getData(companionLibraryDragType)).toBe("local:a");
        expect(screen.getByTestId("file-companion-library-card-local:a").getAttribute("data-dragging")).toBe("true");
        const none = folderRow("file-companion-folder-none");
        fireEvent.dragOver(none, { dataTransfer: back });
        expect(none.getAttribute("data-drop")).toBe("over");
        fireEvent.drop(none, { dataTransfer: back });
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("local:a", "C:/notes/alpha.md", ""));
    });

    it("does not drop onto 全部, the current folder, or the title", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        renderLibrary();
        await screen.findByTestId("file-companion-library-drag-local:a");
        const data = libraryDragTransfer();
        fireEvent.dragStart(screen.getByTestId("file-companion-library-drag-local:a"), { dataTransfer: data });
        const paper = folderRow("file-companion-folder-fld_paper");
        fireEvent.dragOver(paper, { dataTransfer: data });
        expect(paper.getAttribute("data-drop")).toBeNull();
        fireEvent.drop(paper, { dataTransfer: data });
        expect(moveItem).not.toHaveBeenCalled();

        const all = folderRow("file-companion-folder-all");
        fireEvent.dragOver(all, { dataTransfer: data });
        fireEvent.drop(all, { dataTransfer: data });
        expect(all.getAttribute("data-drop")).toBeNull();
        expect(moveItem).not.toHaveBeenCalled();

        const blocked = libraryDragTransfer();
        fireEvent.dragStart(screen.getByTestId("file-companion-library-item-local:b"), { dataTransfer: blocked });
        fireEvent.dragStart(screen.getByTestId("file-companion-library-delete-local:b"), { dataTransfer: blocked });
        expect(blocked.getData(companionLibraryDragType)).toBe("");
        expect(screen.getByTestId("file-companion-library-card-local:b").getAttribute("data-dragging")).toBeNull();
    });

    it("does not paste a library drag into the search box or a new folder name", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        renderLibrary();
        const handle = await screen.findByTestId("file-companion-library-drag-local:b");
        const data = libraryDragTransfer();
        data.dropEffect = "copy";
        fireEvent.dragStart(handle, { dataTransfer: data });
        const filter = screen.getByTestId("file-companion-library-filter") as HTMLInputElement;
        fireEvent.dragOver(filter, { dataTransfer: data });
        expect(data.dropEffect).toBe("none");
        const dropped = createEvent.drop(filter, { dataTransfer: data });
        fireEvent(filter, dropped);
        expect(dropped.defaultPrevented).toBe(true);
        expect(filter.value).toBe("");
        expect(moveItem).not.toHaveBeenCalled();
        const name = screen.getByTestId("file-companion-folder-name") as HTMLInputElement;
        const named = createEvent.drop(name, { dataTransfer: data });
        fireEvent(name, named);
        expect(named.defaultPrevented).toBe(true);
        expect(name.value).toBe("");
        expect(moveItem).not.toHaveBeenCalled();
    });

    it("hides the drag handle when there is no folder to drop on", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [], placements: {} });
        renderLibrary();
        const card = await screen.findByTestId("file-companion-library-card-local:a");
        expect(card.hasAttribute("draggable")).toBe(false);
        expect(screen.queryByTestId("file-companion-library-drag-local:a")).toBeNull();
        expect(screen.getByTestId("file-companion-library-meta-local:a").getAttribute("draggable")).not.toBe("true");
    });

    it("scrolls the folder rail when a drag sits on its edge", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        renderLibrary();
        const handle = await screen.findByTestId("file-companion-library-drag-local:b");
        const data = libraryDragTransfer();
        fireEvent.dragStart(handle, { dataTransfer: data });
        const rail = screen.getByTestId("file-companion-library-folders");
        vi.spyOn(rail, "scrollHeight", "get").mockReturnValue(400);
        vi.spyOn(rail, "clientHeight", "get").mockReturnValue(100);
        rail.getBoundingClientRect = () => ({ top: 0, bottom: 100, left: 0, right: 80, width: 80, height: 100, x: 0, y: 0, toJSON: () => ({}) });
        rail.scrollTop = 20;
        const paper = folderRow("file-companion-folder-fld_paper");
        dragOverAt(paper, data, 90);
        expect(rail.scrollTop).toBe(20);
        dragOverAt(rail, data, 96);
        expect(rail.scrollTop).toBe(28);
        dragOverAt(rail, data, 4);
        expect(rail.scrollTop).toBe(20);
        fireEvent.dragEnd(handle);
        await act(async () => {
            await Promise.resolve();
        });
        rail.scrollTop = 20;
        dragOverAt(rail, data, 96);
        expect(rail.scrollTop).toBe(20);
    });

    it("files a document when dragend arrives before the drop", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        moveItem.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:b": "fld_paper" },
        });
        renderLibrary();
        const handle = await screen.findByTestId("file-companion-library-drag-local:b");
        const card = screen.getByTestId("file-companion-library-card-local:b");
        const data = libraryDragTransfer();
        fireEvent.dragStart(handle, { dataTransfer: data });
        expect(card.style.contentVisibility).toBe("visible");
        fireEvent.dragEnd(handle);
        expect(card.style.contentVisibility).toBe("");
        fireEvent.drop(folderRow("file-companion-folder-fld_paper"), { dataTransfer: data });
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("local:b", "C:/notes/beta.md", "fld_paper"));
    });

    it("clears a library drag when dragend reaches the window", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        renderLibrary();
        const handle = await screen.findByTestId("file-companion-library-drag-local:b");
        const card = screen.getByTestId("file-companion-library-card-local:b");
        fireEvent.dragStart(handle, { dataTransfer: libraryDragTransfer() });
        expect(card.style.contentVisibility).toBe("visible");
        window.dispatchEvent(new Event("dragend"));
        expect(card.style.contentVisibility).toBe("");
        expect(document.querySelector(".file-companion-library-dialog")?.hasAttribute("data-library-drag")).toBe(false);
    });

    it("marks a folder as the default category", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        setDefault.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
            defaultId: "fld_paper",
        });
        renderLibrary();
        await screen.findByTestId("file-companion-folder-fld_paper");
        expect(screen.getByTestId("file-companion-folder-default-none").textContent).toBe("默认");
        fireEvent.click(screen.getByTestId("file-companion-folder-default-fld_paper"));
        await waitFor(() => expect(setDefault).toHaveBeenCalledWith("fld_paper"));
        expect(screen.getByTestId("file-companion-folder-default-fld_paper").textContent).toBe("默认");
        expect(screen.getByTestId("file-companion-folder-default-none").textContent).toBe("设为默认");
    });

    it("refreshes an open library after the place dialog moves a document", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        moveItem.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        renderLibrary();
        fireEvent.click(await screen.findByTestId("file-companion-folder-fld_paper"));
        expect(screen.queryByTestId("file-companion-library-card-local:a")).toBeNull();
        listCatalog.mockResolvedValue({
            rev: 3,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: { "item:local:a": "fld_paper" },
        });
        render(
            <CompanionPlaceDialog path="C:/notes/alpha.md" onClose={() => undefined} />,
        );
        const paper = await screen.findByTestId("file-companion-place-fld_paper");
        fireEvent.click(paper);
        fireEvent.click(screen.getByTestId("file-companion-place-confirm"));
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("", "C:/notes/alpha.md", "fld_paper"));
        await screen.findByTestId("file-companion-library-card-local:a");
    });
});

describe("CompanionPlaceDialog", () => {
    it("preselects the default category and leaves the file uncategorized on cancel", async () => {
        listCatalog.mockResolvedValue({
            rev: 2,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
            defaultId: "fld_paper",
        });
        const onClose = vi.fn();
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={onClose} />);
        expect(await screen.findByTestId("file-companion-place-name")).toHaveProperty("textContent", "2510.16079.pdf");
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_paper") as HTMLInputElement).checked).toBe(true));
        fireEvent.click(screen.getByTestId("file-companion-place-cancel"));
        expect(onClose).toHaveBeenCalledOnce();
        expect(moveItem).not.toHaveBeenCalled();
    });

    it("does not move the file until the folder list has loaded", async () => {
        let release: (value: unknown) => void = () => undefined;
        const gate = new Promise((resolve) => { release = resolve; });
        listCatalog.mockReturnValue(gate);
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        const confirm = screen.getByTestId("file-companion-place-confirm");
        expect((confirm as HTMLButtonElement).disabled).toBe(true);
        expect(screen.queryByRole("radiogroup")).toBeNull();
        fireEvent.click(confirm);
        const form = confirm.closest("form");
        if (!form) throw new Error("missing place form");
        fireEvent.submit(form);
        expect(moveItem).not.toHaveBeenCalled();
        await act(async () => {
            release({
                rev: 2,
                folders: [{ id: "fld_paper", name: "论文" }],
                placements: {},
                defaultId: "fld_paper",
            });
        });
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_paper") as HTMLInputElement).checked).toBe(true));
        expect((screen.getByTestId("file-companion-place-confirm") as HTMLButtonElement).disabled).toBe(false);
        expect(document.activeElement).toBe(screen.getByTestId("file-companion-place-fld_paper"));
        screen.getByTestId("file-companion-place-confirm").focus();
        fireEvent.keyDown(window, { key: "Tab" });
        expect(document.activeElement).toBe(screen.getByRole("radio", { name: "未分类" }));
        fireEvent.click(screen.getByTestId("file-companion-place-confirm"));
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("", "C:/papers/2510.16079.pdf", "fld_paper"));
    });

    it("offers the folder the file is already in, not the default", async () => {
        listCatalog.mockResolvedValue({
            rev: 4,
            folders: [
                { id: "fld_paper", name: "论文" },
                { id: "fld_notes", name: "笔记" },
            ],
            placements: { "path:c:\\papers\\2510.16079.pdf": "fld_notes" },
            defaultId: "fld_paper",
        });
        const onClose = vi.fn();
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" name="2510.16079.PDF" onClose={onClose} />);
        expect(await screen.findByTestId("file-companion-place-name")).toHaveProperty("textContent", "2510.16079.PDF");
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_notes") as HTMLInputElement).checked).toBe(true));
        expect((screen.getByTestId("file-companion-place-fld_paper") as HTMLInputElement).checked).toBe(false);
        expect(screen.getByText("取消后这篇文档仍留在「笔记」。")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-place-cancel"));
        expect(onClose).toHaveBeenCalledOnce();
        expect(moveItem).not.toHaveBeenCalled();
        cleanup();
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_notes") as HTMLInputElement).checked).toBe(true));
        fireEvent.click(screen.getByTestId("file-companion-place-confirm"));
        await waitFor(() => expect(moveItem).toHaveBeenCalledWith("", "C:/papers/2510.16079.pdf", "fld_notes"));
    });

    it("does not inherit a sibling path key when this row is uncategorized", async () => {
        listCatalog.mockResolvedValue({
            rev: 5,
            folders: [
                { id: "fld_paper", name: "论文" },
                { id: "fld_notes", name: "笔记" },
            ],
            placements: { "path:c:\\papers\\2510.16079.pdf": "fld_notes" },
            defaultId: "fld_paper",
        });
        folderForPath.mockResolvedValue("");
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_paper") as HTMLInputElement).checked).toBe(true));
        expect((screen.getByTestId("file-companion-place-fld_notes") as HTMLInputElement).checked).toBe(false);
        expect(screen.getByText("取消后这篇文档留在未分类。")).toBeTruthy();
        expect(folderForPath).toHaveBeenCalledWith("C:/papers/2510.16079.pdf");
    });

    it("does not file the document when the folder list cannot be read", async () => {
        listCatalog.mockRejectedValue(new Error("文档列表无法读取"));
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        expect(await screen.findByText("文档列表无法读取")).toBeTruthy();
        expect(screen.queryByText("正在读取…")).toBeNull();
        expect(screen.queryByRole("radiogroup")).toBeNull();
        expect(screen.queryByText("取消后这篇文档留在未分类。")).toBeNull();
        const confirm = screen.getByTestId("file-companion-place-confirm");
        expect((confirm as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(confirm);
        const form = confirm.closest("form");
        if (!form) throw new Error("missing place form");
        fireEvent.submit(form);
        expect(moveItem).not.toHaveBeenCalled();
    });

    it("does not claim the file is uncategorized when its folder cannot be read", async () => {
        listCatalog.mockResolvedValue({
            rev: 7,
            folders: [
                { id: "fld_paper", name: "论文" },
                { id: "fld_notes", name: "笔记" },
            ],
            placements: { "path:c:\\papers\\2510.16079.pdf": "fld_notes" },
            defaultId: "fld_paper",
        });
        folderForPath.mockRejectedValue(new Error("文档列表无法读取"));
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_paper") as HTMLInputElement).checked).toBe(true));
        expect((screen.getByTestId("file-companion-place-fld_notes") as HTMLInputElement).checked).toBe(false);
        expect(screen.getByText("文档列表无法读取")).toBeTruthy();
        expect(screen.queryByText("取消后这篇文档留在未分类。")).toBeNull();
        expect(screen.queryByText("取消后这篇文档仍留在「笔记」。")).toBeNull();
        expect((screen.getByTestId("file-companion-place-confirm") as HTMLButtonElement).disabled).toBe(false);
    });

    it("offers the row folder when the path key is absent", async () => {
        listCatalog.mockResolvedValue({
            rev: 6,
            folders: [
                { id: "fld_paper", name: "论文" },
                { id: "fld_notes", name: "笔记" },
            ],
            placements: {},
            defaultId: "fld_paper",
        });
        folderForPath.mockResolvedValue("fld_notes");
        render(<CompanionPlaceDialog path="C:/papers/2510.16079.pdf" onClose={() => undefined} />);
        await waitFor(() => expect((screen.getByTestId("file-companion-place-fld_notes") as HTMLInputElement).checked).toBe(true));
        expect(screen.getByText("取消后这篇文档仍留在「笔记」。")).toBeTruthy();
    });

    it("shows the document title on one line under the filename", async () => {
        listCatalog.mockResolvedValue({ rev: 1, folders: [], placements: {} });
        const longTitle = "Attention Is All You Need and then a second sentence that the card must keep on a single line";
        renderLibrary([
            { ...papers[0], title: `${longTitle}\nmore` },
            { ...papers[1], title: "beta.md" },
            { id: "local:c", name: "gamma.pdf", path: "C:/notes/gamma.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 4, title: "gamma" },
        ]);
        const line = await screen.findByTestId("file-companion-library-doctitle-local:a");
        expect(line.textContent).toBe(`${longTitle} more`);
        expect(line.tagName).toBe("P");
        expect(line.getAttribute("draggable")).toBeNull();
        expect(line.closest(".file-companion-project-meta")).toBeNull();
        expect(screen.getByTestId("file-companion-library-item-local:a").textContent).toBe("alpha.md");
        expect(screen.queryByTestId("file-companion-library-doctitle-local:b")).toBeNull();
        expect(screen.queryByTestId("file-companion-library-doctitle-local:c")).toBeNull();
        expect(companionLibraryDocTitle({ name: "alpha.md", title: longTitle })).toBe(longTitle);
    });

    it("opens a document from the card surface and leaves action controls alone", async () => {
        listCatalog.mockResolvedValue({
            rev: 1,
            folders: [{ id: "fld_paper", name: "论文" }],
            placements: {},
        });
        const onOpen = vi.fn();
        const onImportLink = vi.fn();
        const onShareProject = vi.fn();
        listLibrary.mockResolvedValue({
            items: [
                { ...papers[0], title: "Alpha notes" },
                papers[1],
                { id: "cloud:pending", name: "pending.pdf", path: "", source: "cloud", shareUrl: "https://hub.example/s/1", openedAt: "2026-10-25T00:00:00Z", size: 4 },
                { id: "cloud:live", name: "live.pdf", path: "C:/notes/live.pdf", source: "cloud", shareUrl: "https://hub.example/s/live", openedAt: "2026-10-25T00:00:00Z", size: 8, title: "A shared paper" },
            ],
            notice: "",
        });
        render(
            <DialogProvider>
                <CompanionLibraryView
                    lang="zh-Hans"
                    activePath=""
                    showToast={() => undefined}
                    onOpen={onOpen}
                    onExit={() => undefined}
                    onShareProject={onShareProject}
                    onEditShare={() => undefined}
                    onImportLink={onImportLink}
                />
            </DialogProvider>,
        );
        const card = await screen.findByTestId("file-companion-library-card-local:a");
        fireEvent.click(screen.getByTestId("file-companion-library-drag-local:a"));
        expect(onOpen).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-library-meta-local:a"));
        fireEvent.click(screen.getByTestId("file-companion-library-doctitle-local:a"));
        fireEvent.click(card);
        fireEvent.click(screen.getByTestId("file-companion-library-item-local:a"));
        expect(onOpen).toHaveBeenCalledTimes(4);
        expect(onOpen).toHaveBeenCalledWith("C:/notes/alpha.md");
        expect(screen.getByTestId("file-companion-library-meta-local:a").getAttribute("draggable")).not.toBe("true");
        expect(screen.getByTestId("file-companion-library-item-local:a").getAttribute("title")).toBe("打开 alpha.md");

        onOpen.mockClear();
        fireEvent.click(screen.getByTestId("file-companion-library-share-action-local:a"));
        fireEvent.click(screen.getByTestId("file-companion-library-export-local:a"));
        fireEvent.click(screen.getByTestId("file-companion-library-move-local:a"));
        fireEvent.click(screen.getByTestId("file-companion-library-shared-mark-cloud:live"));
        expect(onShareProject).toHaveBeenCalledWith("C:/notes/alpha.md");
        expect(onOpen).not.toHaveBeenCalled();

        const url = screen.getByTestId("file-companion-library-url-cloud:live");
        fireEvent.mouseDown(url, { button: 0, clientX: 4, clientY: 4 });
        fireEvent.click(url, { button: 0, clientX: 40, clientY: 4, detail: 1 });
        expect(onOpen).not.toHaveBeenCalled();
        fireEvent.mouseDown(url, { button: 0, clientX: 4, clientY: 4 });
        fireEvent.click(url, { button: 0, clientX: 6, clientY: 5, detail: 1 });
        fireEvent.click(screen.getByTestId("file-companion-library-doctitle-cloud:live"));
        expect(onOpen).toHaveBeenCalledTimes(2);
        expect(onOpen).toHaveBeenCalledWith("C:/notes/live.pdf");

        fireEvent.click(screen.getByTestId("file-companion-library-meta-cloud:pending"));
        expect(onImportLink).toHaveBeenCalledWith("https://hub.example/s/1");
        expect(onOpen).toHaveBeenCalledTimes(2);
    });
});

function folderRow(testId: string): HTMLElement {
    const node = screen.getByTestId(testId).closest("[role=treeitem]");
    if (!(node instanceof HTMLElement)) throw new Error("missing folder row");
    return node;
}

function dragOverAt(node: HTMLElement, data: ReturnType<typeof libraryDragTransfer>, clientY: number) {
    const event = createEvent.dragOver(node, { clientY, dataTransfer: data });
    Object.defineProperty(event, "clientY", { configurable: true, value: clientY });
    fireEvent(node, event);
}

function libraryDragTransfer() {
    const store = new Map<string, string>();
    return {
        effectAllowed: "all",
        dropEffect: "none",
        imageX: 0,
        imageY: 0,
        setData(type: string, value: string) {
            store.set(type, value);
        },
        getData(type: string) {
            return store.get(type) ?? "";
        },
        setDragImage(_image: unknown, x: number, y: number) {
            this.imageX = x;
            this.imageY = y;
        },
    };
}
