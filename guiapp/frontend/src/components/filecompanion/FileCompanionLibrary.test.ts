import { describe, expect, it, vi } from "vitest";

// The library module only calls Wails from event handlers; the parser under
// test is pure. Stub every bound method so importing it is harmless.
vi.mock("../../../wailsjs/go/main/App", () => ({
    AddFileCompanionGroupMember: vi.fn(),
    CreateFileCompanionGroup: vi.fn(),
    ImportFileCompanionShare: vi.fn(),
    DeleteFileCompanionGroup: vi.fn(),
    DeleteFileCompanionProject: vi.fn(),
    DeleteFileCompanionShare: vi.fn(),
    LeaveFileCompanionGroup: vi.fn(),
    ListFileCompanionGroups: vi.fn(),
    ListFileCompanionLibrary: vi.fn(),
    ListFileCompanionCatalog: vi.fn(),
    CreateFileCompanionFolder: vi.fn(),
    RenameFileCompanionFolder: vi.fn(),
    DeleteFileCompanionFolder: vi.fn(),
    MoveFileCompanionLibraryItem: vi.fn(),
    SetFileCompanionDefaultFolder: vi.fn(),
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

import { companionFolderNameError, companionInstalledSharePath, companionLibraryApplyLocalEdits, companionLibraryFolderID, companionLibraryHideRow, companionLibraryNameMatch, companionLibraryRowKey, companionLibraryShowRestored, companionLibrarySync, companionLibraryWithoutRemoved, companionShareChatBehind, companionShareDocumentCatchUp, companionShareMarksBehind, companionShouldPullShareChat, companionShouldPullShareMarks, companionZipKeepsAppliedMarks, companionWithdrawnNotice, resetCompanionLibraryList, takeCompanionLibraryList } from "./FileCompanionLibrary";

describe("companionInstalledSharePath", () => {
    it("recognizes a received copy by the share folder", () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        expect(companionInstalledSharePath(`C:/Users/me/.maclaw/data/file-companion/library-files/${token}/shared.pdf`)).toBe(true);
        expect(companionInstalledSharePath(`C:\\Users\\me\\.maclaw\\data\\file-companion\\library-files\\${token}\\shared.pdf`)).toBe(true);
        expect(companionInstalledSharePath("C:/Users/me/Desktop/paper.pdf")).toBe(false);
    });
});

describe("companionShouldPullShareMarks", () => {
    const token = "cd_0123456789abcdef0123456789abcdef";
    const cache = `C:/Users/me/.maclaw/data/file-companion/library-files/${token}/shared.pdf`;
    it("follows marks onto another sign-in of the host", () => {
        expect(companionShouldPullShareMarks({ mine: true, path: cache, marksRevision: 3 })).toBe(true);
        expect(companionShouldPullShareMarks({ mine: false, path: cache, marksRevision: 3 })).toBe(true);
    });
    it("leaves the authoring paper to the machine that painted the marks", () => {
        expect(companionShouldPullShareMarks({ mine: true, path: "C:/Users/me/Desktop/paper.pdf", marksRevision: 3 })).toBe(false);
        expect(companionShouldPullShareMarks({ mine: true, path: cache, marksRevision: 0 })).toBe(false);
        expect(companionShouldPullShareChat({ mine: false, path: "C:/library/shared.pdf", chatRevision: 2 })).toBe(true);
        expect(companionShouldPullShareChat({ mine: true, path: cache, chatRevision: 2 })).toBe(true);
        expect(companionShouldPullShareChat({ mine: true, path: "C:/Users/me/Desktop/paper.pdf", chatRevision: 2 })).toBe(false);
        expect(companionShouldPullShareChat({ mine: false, path: cache, chatRevision: 0 })).toBe(false);
        expect(companionShareChatBehind(cache, 4, 2)).toBe(true);
        expect(companionShareChatBehind(cache, 2, 2)).toBe(false);
        expect(companionShareChatBehind("C:/Users/me/Desktop/paper.pdf", 4, 0)).toBe(false);
    });
});

describe("companionZipKeepsAppliedMarks", () => {
    it("keeps highlights a newer marks fetch already showed", () => {
        expect(companionZipKeepsAppliedMarks(6, 5)).toBe(true);
        expect(companionZipKeepsAppliedMarks(5, 5)).toBe(false);
        expect(companionZipKeepsAppliedMarks(0, 5)).toBe(false);
        expect(companionZipKeepsAppliedMarks(4, 0)).toBe(false);
        expect(companionZipKeepsAppliedMarks(0, 0, true)).toBe(true);
    });
});

describe("companionShareMarksBehind", () => {
    it("holds a question on a downloaded host copy that has not applied marks", () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        const cache = `C:/Users/me/.maclaw/data/file-companion/library-files/${token}/shared.pdf`;
        expect(companionShareMarksBehind(cache, 4, 0)).toBe(true);
        expect(companionShareMarksBehind(cache, 4, 4)).toBe(false);
        expect(companionShareMarksBehind(cache, 0, 0)).toBe(false);
        expect(companionShareMarksBehind("C:/Users/me/Desktop/paper.pdf", 4, 0)).toBe(false);
        expect(companionShareMarksBehind(cache, 4, 4, { listed: 5, applied: 4, known: true })).toBe(true);
        expect(companionShareMarksBehind(cache, 4, 4, { listed: 5, applied: 5, known: true })).toBe(false);
        expect(companionShareMarksBehind(cache, 0, 0, { listed: 0, applied: 0, known: false })).toBe(true);
        expect(companionShareMarksBehind("C:/Users/me/Desktop/paper.pdf", 0, 0, { listed: 5, applied: 0, known: false })).toBe(false);
    });
});

describe("companionShareDocumentCatchUp", () => {
    it("keeps a quiet stored list and waits for the hub index download", () => {
        const stored = companionShareDocumentCatchUp({ listed: 0, applied: 0 }, 4, true, false);
        expect(stored).toEqual({ listed: 4, applied: 4 });
        const index = companionShareDocumentCatchUp(stored, 5, true, true);
        expect(index).toEqual({ listed: 5, applied: 4 });
        const finished = companionShareDocumentCatchUp(index, 5, false, false);
        expect(finished).toEqual({ listed: 5, applied: 5 });
        const failed = companionShareDocumentCatchUp(index, 4, false, false);
        expect(failed.applied).toBe(4);
        expect(failed.listed).toBe(5);
    });
});

describe("companionLibrarySync", () => {
    it("reads the withdrawn paths and the hosts to ask", () => {
        const synced = companionLibrarySync({
            removed: ["C:\\library\\a.pdf"],
            removedHosts: { "C:\\library\\a.pdf": "王老师" },
        });
        expect(synced.removed).toEqual(["C:\\library\\a.pdf"]);
        expect(synced.removedHosts).toEqual({ "C:\\library\\a.pdf": "王老师" });
    });

    it("reads the PascalCase shape a Wails payload uses", () => {
        const synced = companionLibrarySync({
            Removed: ["C:\\library\\a.pdf"],
            RemovedHosts: { "C:\\library\\a.pdf": "李老师" },
        });
        expect(synced.removed).toEqual(["C:\\library\\a.pdf"]);
        expect(synced.removedHosts).toEqual({ "C:\\library\\a.pdf": "李老师" });
    });

    it("keeps one entry per path and drops blank or non-string values", () => {
        const synced = companionLibrarySync({
            removed: ["C:\\a.pdf", "C:\\a.pdf", 7, "", "C:\\b.pdf"],
            removedHosts: { "C:\\a.pdf": " 王老师 ", "C:\\b.pdf": "   ", "C:\\c.pdf": "赵老师" },
        });
        expect(synced.removed).toEqual(["C:\\a.pdf", "C:\\b.pdf"]);
        // A blank label is not a name, and a path that did not close keeps no
        // name; the window falls back to its generic notice for those.
        expect(synced.removedHosts).toEqual({ "C:\\a.pdf": "王老师" });
    });

    it("matches a host label when the path spelling differs", () => {
        const synced = companionLibrarySync({
            removed: ["C:/library/a.pdf"],
            removedHosts: { "C:\\library\\a.pdf": " 王老师 " },
        });
        expect(synced.removedHosts).toEqual({ "C:/library/a.pdf": "王老师" });
    });

    it("tells the reader the article was unshared and who to ask", () => {
        expect(companionWithdrawnNotice(1, ["王老师"])).toBe("该文章已经取消分享，如果需要继续阅读，请联系主持人 王老师");
        expect(companionWithdrawnNotice(2, ["王老师", " 李老师 ", "王老师"])).toBe("这些文章已经取消分享，如果需要继续阅读，请联系主持人 王老师、李老师");
        expect(companionWithdrawnNotice(1, ["", "  "])).toBe("该文章已经取消分享，如果需要继续阅读，请联系分享的主持人");
    });

    it("has nothing to sync without a payload", () => {
        const empty = { updates: [], removed: [], removedHosts: {} };
        expect(companionLibrarySync(null)).toEqual(empty);
        expect(companionLibrarySync("removed")).toEqual(empty);
        expect(companionLibrarySync({})).toEqual(empty);
    });
});

describe("companion library folders", () => {
    it("rejects a blank, reserved, or duplicate folder name", () => {
        expect(companionFolderNameError("  ")).toBe("请填写文件夹名称");
        expect(companionFolderNameError("好\n名")).toBe("请填写文件夹名称");
        expect(companionFolderNameError("未分类")).toBe("全部和未分类不能用作文件夹名称");
        expect(companionFolderNameError("论文", [{ id: "fld_a", name: "论文" }])).toBe("已有同名文件夹");
        expect(companionFolderNameError("论文", [{ id: "fld_a", name: "论文" }], "fld_a")).toBe("");
    });

    it("matches a file name without case and keeps an empty query", () => {
        expect(companionLibraryNameMatch("Alpha.MD", "alpha")).toBe(true);
        expect(companionLibraryNameMatch("beta.md", "  ")).toBe(true);
        expect(companionLibraryNameMatch("beta.md", "alpha")).toBe(false);
    });

    it("prefers the item placement over the path placement", () => {
        const item = { id: "local:a", path: "C:/notes/a.md" };
        const placements = { "item:local:a": "fld_item", "path:c:/notes/a.md": "fld_path" };
        expect(companionLibraryFolderID(item, placements)).toBe("fld_item");
        expect(companionLibraryFolderID({ id: "local:b", path: "C:/notes/a.md" }, placements)).toBe("fld_path");
    });

    it("reads a Windows path key and keeps an explicit uncategorized row", () => {
        const backslash = { "path:c:\\notes\\a.md": "fld_path" };
        expect(companionLibraryFolderID({ id: "local:b", path: "C:/notes/a.md" }, backslash)).toBe("fld_path");
        expect(companionLibraryFolderID(
            { id: "local:a", path: "C:/notes/a.md" },
            { "item:local:a": "none", "path:c:\\notes\\a.md": "fld_path" },
        )).toBe("");
    });
});

describe("takeCompanionLibraryList", () => {
    const local = { id: "local:a", name: "a.pdf", source: "local" };
    const cloud = { id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", source: "cloud", token: "cd_0123456789abcdef0123456789abcdef" };

    it("paints a delete that shares the refresh generation", () => {
        resetCompanionLibraryList();
        expect(takeCompanionLibraryList({ items: [local, cloud], syncing: true, syncGen: 4, libraryRev: 2 })).toBe(true);
        // The same generation is the disk read for this refresh. A second copy
        // with nothing saved must not replace the one already on screen.
        expect(takeCompanionLibraryList({ items: [local, cloud], syncing: true, syncGen: 4, libraryRev: 2 })).toBe(false);
        // The reader confirmed 删除. The library revision moved; the card goes.
        expect(takeCompanionLibraryList({ items: [cloud], syncing: true, syncGen: 4, libraryRev: 3 })).toBe(true);
        // The hub index from before that delete has a newer sync generation and
        // the old revision. It must not put the local project back.
        expect(takeCompanionLibraryList({ items: [local, cloud], syncing: true, catalog: true, syncGen: 5, libraryRev: 2 })).toBe(false);
        expect(takeCompanionLibraryList({ items: [cloud], syncing: false, syncGen: 6, libraryRev: 3 })).toBe(true);
    });

    it("a confirmed delete rejects the catalog that was already built", () => {
        resetCompanionLibraryList();
        expect(takeCompanionLibraryList({ items: [local, cloud], syncing: true, syncGen: 4, libraryRev: 2 })).toBe(true);
        companionLibraryHideRow(local, 4, 2);
        expect(takeCompanionLibraryList({ items: [local, cloud], syncing: true, catalog: true, syncGen: 5, libraryRev: 2 })).toBe(false);
        expect(takeCompanionLibraryList({ items: [cloud], syncing: false, syncGen: 6, libraryRev: 4 })).toBe(true);
    });
});

describe("companionLibraryWithoutRemoved", () => {
    const local = { id: "local:a", source: "local", token: "" };
    const cloud = { id: "cd_0123456789abcdef0123456789abcdef", source: "cloud", token: "cd_0123456789abcdef0123456789abcdef" };

    it("hides a removed card until a later library save names it again", () => {
        resetCompanionLibraryList();
        companionLibraryHideRow(local, 4, 3);
        expect(companionLibraryRowKey(local)).toBe("id:local:a");
        expect(companionLibraryRowKey(cloud)).toBe(`cloud:${cloud.token}`);
        expect(companionLibraryWithoutRemoved([local, cloud], 3).map((item) => item.id)).toEqual([cloud.id]);
        expect(companionLibraryWithoutRemoved([local, cloud], 4).map((item) => item.id)).toEqual([local.id, cloud.id]);
    });
});

describe("companionLibraryApplyLocalEdits", () => {
    const cloud = {
        id: "cd_0123456789abcdef0123456789abcdef",
        name: "paper.pdf",
        path: "D:/paper.pdf",
        source: "cloud",
        token: "cd_0123456789abcdef0123456789abcdef",
        openedAt: "",
        owner: "",
        shareUrl: "",
        expiresAt: "",
        visibility: "",
        size: 5,
        permanent: true,
        mine: true,
        revision: 1,
        target: "",
        groupId: "",
        expired: false,
        highlights: "",
        notes: [],
    };

    it("shows the owner's file while the refresh that would list it is still copying", () => {
        resetCompanionLibraryList();
        const local = { ...cloud, id: "local:paper", source: "local", token: "", mine: false, permanent: false };
        companionLibraryShowRestored(local, 6, 5);
        companionLibraryHideRow(cloud, 6, 5);
        expect(companionLibraryApplyLocalEdits([cloud], 5).map((item) => item.id)).toEqual(["local:paper"]);
        expect(companionLibraryApplyLocalEdits([local], 6).map((item) => item.id)).toEqual(["local:paper"]);
    });

    it("keeps a share a later save brought back", () => {
        resetCompanionLibraryList();
        companionLibraryHideRow(cloud, 6, 5);
        expect(companionLibraryApplyLocalEdits([cloud], 8).map((item) => item.id)).toEqual([cloud.id]);
    });
});
