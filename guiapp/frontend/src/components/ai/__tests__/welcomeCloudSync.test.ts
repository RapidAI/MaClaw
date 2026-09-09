import { describe, expect, it } from "vitest";
import {
    classifyWelcomeCloudError,
    formatWelcomeCloudUpdatedAt,
    loadWelcomeCloudAutoSync,
    parseWelcomeCloudStatus,
    saveWelcomeCloudAutoSync,
    shouldAutoPushWelcomeCloud,
    welcomeCloudErrorMessage,
    welcomeCloudLocalFingerprint,
    welcomeCloudPayloadText,
    welcomeCloudStatusLabel,
    welcomeCloudUserNote,
    shouldPushWelcomeFromStorageAfterPull,
    welcomeCloudConflictPhaseLabel,
    welcomeCloudConflictResolveButtonLabel,
    WELCOME_CLOUD_AUTO_SYNC_KEY,
} from "../welcomeCloudSync";

describe("welcomeCloudSync helpers", () => {
    it("persists auto-sync preference", () => {
        localStorage.clear();
        expect(loadWelcomeCloudAutoSync()).toBe(false);
        saveWelcomeCloudAutoSync(true);
        expect(localStorage.getItem(WELCOME_CLOUD_AUTO_SYNC_KEY)).toBe("1");
        expect(loadWelcomeCloudAutoSync()).toBe(true);
        saveWelcomeCloudAutoSync(false);
        expect(loadWelcomeCloudAutoSync()).toBe(false);
    });

    it("blocks auto-push that would wipe a rich cloud backup", () => {
        expect(shouldAutoPushWelcomeCloud({
            autoSync: true,
            loggedIn: true,
            unsupported: false,
            busy: false,
            localTemplateCount: 0,
            cloudHasDocument: true,
            cloudTemplateCount: 3,
        })).toBe(false);
        expect(shouldAutoPushWelcomeCloud({
            autoSync: true,
            loggedIn: true,
            unsupported: false,
            busy: false,
            localTemplateCount: 2,
            cloudHasDocument: true,
            cloudTemplateCount: 3,
        })).toBe(true);
        expect(shouldAutoPushWelcomeCloud({
            autoSync: false,
            loggedIn: true,
            unsupported: false,
            busy: false,
            localTemplateCount: 2,
            cloudHasDocument: false,
            cloudTemplateCount: 0,
        })).toBe(false);
    });

    it("parses snake_case and camelCase status", () => {
        const a = parseWelcomeCloudStatus({
            logged_in: true,
            has_document: true,
            revision: "abc",
            template_count: 3,
            updated_at: "2026-07-14T00:00:00Z",
        });
        expect(a.loggedIn).toBe(true);
        expect(a.hasDocument).toBe(true);
        expect(a.revision).toBe("abc");
        expect(a.templateCount).toBe(3);

        const b = parseWelcomeCloudStatus({
            loggedIn: true,
            hasDocument: false,
            templateCount: 0,
        });
        expect(b.loggedIn).toBe(true);
        expect(b.hasDocument).toBe(false);
    });

    it("flags unsupported hub from error field", () => {
        const s = parseWelcomeCloudStatus({
            logged_in: true,
            has_document: false,
            error: "hub does not support welcome sync (upgrade hub)",
        });
        expect(s.unsupported).toBe(true);
        expect(s.hasDocument).toBe(false);
        expect(welcomeCloudStatusLabel(s, true)).toBe("需升级 Hub");
        expect(welcomeCloudStatusLabel(s, false)).toBe("Upgrade Hub");
    });

    it("classifies cloud errors", () => {
        expect(classifyWelcomeCloudError("cloud conflict: revision mismatch")).toBe("conflict");
        expect(classifyWelcomeCloudError("no cloud welcome document")).toBe("empty");
        expect(classifyWelcomeCloudError("hub login required (viewer token missing)")).toBe("login");
        expect(classifyWelcomeCloudError("hub does not support welcome sync (upgrade hub)")).toBe("unsupported");
        expect(classifyWelcomeCloudError("something else")).toBe("generic");
    });

    it("extracts error messages from varied shapes", () => {
        expect(welcomeCloudErrorMessage(new Error("boom"))).toBe("boom");
        expect(welcomeCloudErrorMessage("plain")).toBe("plain");
        expect(welcomeCloudErrorMessage({ message: "obj" })).toBe("obj");
    });

    it("reads payload json under several keys", () => {
        expect(welcomeCloudPayloadText({ payload_json: '{"a":1}' })).toContain('"a"');
        expect(welcomeCloudPayloadText({ payloadJson: '{"b":2}' })).toContain('"b"');
        expect(welcomeCloudPayloadText({})).toBe("");
    });

    it("labels signed-in empty vs filled cloud", () => {
        expect(welcomeCloudStatusLabel({ loggedIn: true, hasDocument: false, templateCount: 0, unsupported: false }, true))
            .toBe("云端空");
        expect(welcomeCloudStatusLabel({ loggedIn: true, hasDocument: true, templateCount: 4, unsupported: false }, false))
            .toBe("Cloud 4");
        expect(welcomeCloudStatusLabel({ loggedIn: false, hasDocument: false, templateCount: 0, unsupported: false }, true))
            .toBeNull();
    });

    it("formats updatedAt and user notes", () => {
        const formatted = formatWelcomeCloudUpdatedAt("2026-07-14T08:30:00.000Z", false);
        expect(formatted.length).toBeGreaterThan(0);
        expect(formatWelcomeCloudUpdatedAt("not-a-date", true)).toBe("not-a-date");
        expect(welcomeCloudUserNote("empty", true)).toContain("暂无");
        expect(welcomeCloudUserNote("conflict", false)).toMatch(/Merge then upload|拉取合并后再传/i);
        expect(welcomeCloudUserNote("conflict", true, "", "push", true)).toMatch(/自动同步暂停|Merge then upload/i);
        expect(welcomeCloudUserNote("generic", true, "timeout", "push")).toContain("上传失败");
    });

    it("requires fromStorage push after a successful conflict pull", () => {
        expect(shouldPushWelcomeFromStorageAfterPull(true)).toBe(true);
        expect(shouldPushWelcomeFromStorageAfterPull(false)).toBe(false);
    });

    it("labels conflict resolve phases", () => {
        expect(welcomeCloudConflictPhaseLabel("idle", true)).toMatch(/冲突/);
        expect(welcomeCloudConflictPhaseLabel("pulling", true)).toMatch(/拉取/);
        expect(welcomeCloudConflictPhaseLabel("pushing", false)).toMatch(/Uploading/i);
        expect(welcomeCloudConflictResolveButtonLabel("idle", false)).toMatch(/Merge then upload/i);
        expect(welcomeCloudConflictResolveButtonLabel("pulling", true)).toBe("处理中…");
    });

    it("fingerprints local welcome state for auto-sync de-dupe", () => {
        const a = welcomeCloudLocalFingerprint({
            templates: [{ id: "1", title: "T", body: "b" }],
            userRole: "dev",
            recent: [{ tabId: "dev", textEn: "x", usedAt: 1 }],
        });
        const b = welcomeCloudLocalFingerprint({
            templates: [{ id: "1", title: "T", body: "b" }],
            userRole: "dev",
            recent: [{ tabId: "dev", textEn: "x", usedAt: 1 }],
        });
        const c = welcomeCloudLocalFingerprint({
            templates: [{ id: "1", title: "T", body: "changed" }],
            userRole: "dev",
            recent: [{ tabId: "dev", textEn: "x", usedAt: 1 }],
        });
        expect(a).toBe(b);
        expect(a).not.toBe(c);
        const withMode = welcomeCloudLocalFingerprint({
            templates: [{ id: "1", title: "T", body: "b", agentMode: "coding_dev" }],
            userRole: "dev",
            recent: [],
        });
        const withoutMode = welcomeCloudLocalFingerprint({
            templates: [{ id: "1", title: "T", body: "b" }],
            userRole: "dev",
            recent: [],
        });
        expect(withMode).not.toBe(withoutMode);
    });
});
