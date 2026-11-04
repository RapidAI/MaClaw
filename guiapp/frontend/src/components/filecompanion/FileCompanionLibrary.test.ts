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

import { companionInstalledSharePath, companionLibrarySync, companionShareDocumentCatchUp, companionShareMarksBehind, companionShouldPullShareMarks, companionZipKeepsAppliedMarks, companionWithdrawnNotice } from "./FileCompanionLibrary";

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
