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

import { companionLibrarySync, companionWithdrawnNotice } from "./FileCompanionLibrary";

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
