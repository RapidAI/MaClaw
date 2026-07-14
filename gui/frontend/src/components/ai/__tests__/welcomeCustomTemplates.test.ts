import { beforeEach, describe, expect, it } from "vitest";
import {
    buildWelcomeTemplatesExport,
    customTemplateToWelcomePrompt,
    deleteWelcomeCustomTemplate,
    filterWelcomeRecentForQuickAccess,
    importWelcomeCustomTemplates,
    isWelcomeTemplateBodySaved,
    loadWelcomeCloudRevision,
    loadWelcomeCustomTemplates,
    loadWelcomeRecentEntries,
    loadWelcomeUserRole,
    moveWelcomeCustomTemplate,
    parseWelcomeTemplatesImport,
    previewWelcomeTemplatesImport,
    renameWelcomeCustomTemplate,
    saveWelcomeCloudRevision,
    saveWelcomeCustomTemplate,
    shouldOfferWelcomeTemplateSave,
    stringifyWelcomeTemplatesExport,
    touchWelcomeCustomTemplate,
    welcomeTemplatesExportFilename,
    WELCOME_CLOUD_REVISION_KEY,
    WELCOME_CUSTOM_TEMPLATES_KEY,
    WELCOME_CUSTOM_TEMPLATES_MAX,
    WELCOME_TEMPLATES_EXPORT_KIND,
    type WelcomeCustomTemplate,
} from "../welcomeTaskMemory";
import { resolveWelcomeQuickHints, WELCOME_QUICK_HINTS } from "../welcomeQuickHints";
import { SCENARIO_TABS } from "../welcomeScenarioTasks";

describe("welcome custom templates", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("saves, dedups by body, and loads newest first", () => {
        expect(saveWelcomeCustomTemplate({ title: "A", body: "hello world task" }).saved).toBeTruthy();
        saveWelcomeCustomTemplate({ title: "B", body: "other body text here" });
        // Same body → replace/refresh as newest
        const third = saveWelcomeCustomTemplate({ title: "A2", body: "hello world task" });

        const list = loadWelcomeCustomTemplates();
        expect(list).toHaveLength(2);
        expect(list[0].title).toBe("A2");
        expect(list[0].body).toBe("hello world task");
        expect(third.saved?.title).toBe("A2");
        expect(localStorage.getItem(WELCOME_CUSTOM_TEMPLATES_KEY)).toBeTruthy();
    });

    it("caps stored templates", () => {
        for (let i = 0; i < WELCOME_CUSTOM_TEMPLATES_MAX + 3; i++) {
            saveWelcomeCustomTemplate({ title: `T${i}`, body: `body-${i}-xxxxxxxx` });
        }
        expect(loadWelcomeCustomTemplates().length).toBe(WELCOME_CUSTOM_TEMPLATES_MAX);
    });

    it("touches and deletes templates", () => {
        const { saved: first } = saveWelcomeCustomTemplate({ title: "Keep", body: "keep body content" });
        expect(first).toBeTruthy();
        saveWelcomeCustomTemplate({ title: "Drop", body: "drop body content" });
        const afterTouch = touchWelcomeCustomTemplate(first!.id);
        expect(afterTouch[0].id).toBe(first!.id);
        const afterDelete = deleteWelcomeCustomTemplate(first!.id);
        expect(afterDelete.find((t) => t.id === first!.id)).toBeUndefined();
    });

    it("converts to WelcomePrompt shape", () => {
        const { saved: tpl } = saveWelcomeCustomTemplate({
            title: "本地功能",
            body: "实现登录页\n需求：[描述]",
            agentMode: "coding_dev",
        });
        expect(tpl).toBeTruthy();
        const prompt = customTemplateToWelcomePrompt(tpl!);
        expect(prompt.text).toBe("本地功能");
        expect(prompt.template).toContain("实现登录页");
        expect(prompt.agentMode).toBe("coding_dev");
        expect(prompt.icon).toBe("spark");
    });

    it("rejects empty title or body", () => {
        const empty = saveWelcomeCustomTemplate({ title: "", body: "x" });
        expect(empty.saved).toBeNull();
        expect(empty.templates).toEqual([]);
    });

    it("renames a template and ignores empty titles", () => {
        const { saved } = saveWelcomeCustomTemplate({ title: "Old", body: "rename body content here" });
        expect(saved).toBeTruthy();
        const renamed = renameWelcomeCustomTemplate(saved!.id, "  New Title  ");
        expect(renamed.find((t) => t.id === saved!.id)?.title).toBe("New Title");
        const ignored = renameWelcomeCustomTemplate(saved!.id, "   ");
        expect(ignored.find((t) => t.id === saved!.id)?.title).toBe("New Title");
    });

    it("moves templates left/right and no-ops at edges", () => {
        const a = saveWelcomeCustomTemplate({ title: "A", body: "body-a-xxxxxxxx" }).saved!;
        const b = saveWelcomeCustomTemplate({ title: "B", body: "body-b-xxxxxxxx" }).saved!;
        // B is newest (front), then A
        expect(loadWelcomeCustomTemplates().map((t) => t.id)).toEqual([b.id, a.id]);

        let list = moveWelcomeCustomTemplate(b.id, "down");
        expect(list.map((t) => t.id)).toEqual([a.id, b.id]);
        list = moveWelcomeCustomTemplate(b.id, "down");
        // already last — no-op
        expect(list.map((t) => t.id)).toEqual([a.id, b.id]);
        list = moveWelcomeCustomTemplate(b.id, "up");
        expect(list.map((t) => t.id)).toEqual([b.id, a.id]);
        list = moveWelcomeCustomTemplate(b.id, "up");
        expect(list.map((t) => t.id)).toEqual([b.id, a.id]);
    });

    it("exports and re-imports templates with merge dedupe", () => {
        saveWelcomeCustomTemplate({ title: "Keep", body: "shared-body-content-xxx" });
        saveWelcomeCustomTemplate({ title: "OnlyLocal", body: "only-local-body-yyyy" });
        const exported = stringifyWelcomeTemplatesExport();
        expect(exported).toContain(WELCOME_TEMPLATES_EXPORT_KIND);
        expect(buildWelcomeTemplatesExport().templates.length).toBe(2);

        // Fresh machine with one overlapping body
        localStorage.clear();
        saveWelcomeCustomTemplate({ title: "KeepOld", body: "shared-body-content-xxx" });
        const result = importWelcomeCustomTemplates(exported, "merge");
        expect(result.error).toBeUndefined();
        expect(result.added).toBe(1); // OnlyLocal
        expect(result.skipped).toBeGreaterThanOrEqual(1); // shared body
        const titles = loadWelcomeCustomTemplates().map((t) => t.title);
        expect(titles).toContain("OnlyLocal");
        expect(titles).toContain("KeepOld");
    });

    it("replace import overwrites local list", () => {
        saveWelcomeCustomTemplate({ title: "Local", body: "local-only-zzzzzzzz" });
        const payload = JSON.stringify({
            version: 1,
            kind: WELCOME_TEMPLATES_EXPORT_KIND,
            exportedAt: new Date().toISOString(),
            templates: [{ title: "Imported", body: "imported-body-wwwwww" }],
        });
        const result = importWelcomeCustomTemplates(payload, { mode: "replace" });
        expect(result.added).toBe(1);
        expect(result.restoredExtras).toBe(false);
        expect(loadWelcomeCustomTemplates().map((t) => t.title)).toEqual(["Imported"]);
    });

    it("full backup restores role and recent on import", () => {
        localStorage.clear();
        saveWelcomeCustomTemplate({ title: "T1", body: "full-backup-body-aaaaaa" });
        const payload = buildWelcomeTemplatesExport(undefined, {
            includeExtras: true,
            userRole: "dev",
            recent: [{ key: "dev::Implement a feature", tabId: "dev", textEn: "Implement a feature", usedAt: 99 }],
            lastScenarioTab: "ops",
        });
        expect(payload.userRole).toBe("dev");
        expect(payload.recent?.[0].tabId).toBe("dev");
        expect(payload.lastScenarioTab).toBe("ops");

        localStorage.clear();
        const result = importWelcomeCustomTemplates(JSON.stringify(payload), {
            mode: "merge",
            restoreExtras: true,
        });
        expect(result.restoredExtras).toBe(true);
        expect(loadWelcomeUserRole()).toBe("dev");
        expect(loadWelcomeRecentEntries()[0]?.textEn).toBe("Implement a feature");
        expect(localStorage.getItem("maclaw:welcome-scenario-tab")).toBe("ops");
    });

    it("parses bare arrays and rejects invalid JSON", () => {
        const bare = parseWelcomeTemplatesImport(
            JSON.stringify([{ title: "T", body: "bare-array-body-qqqq" }]),
        );
        expect(bare.ok).toBe(true);
        if (bare.ok) expect(bare.items[0].title).toBe("T");
        expect(parseWelcomeTemplatesImport("{not json").ok).toBe(false);
        expect(welcomeTemplatesExportFilename(new Date("2026-07-14T00:00:00Z"))).toBe(
            "maclaw-welcome-backup-20260714.json",
        );
    });

    it("previews merge add/skip without writing storage", () => {
        saveWelcomeCustomTemplate({ title: "Local", body: "preview-dup-body-zzzz" });
        const raw = JSON.stringify({
            version: 2,
            kind: WELCOME_TEMPLATES_EXPORT_KIND,
            templates: [
                { title: "Dup", body: "preview-dup-body-zzzz" },
                { title: "New", body: "preview-new-body-yyyy" },
            ],
            userRole: "ops",
        });
        const before = loadWelcomeCustomTemplates().length;
        const previewed = previewWelcomeTemplatesImport(raw, "merge");
        expect(previewed.ok).toBe(true);
        if (!previewed.ok) return;
        expect(previewed.preview.toAdd.map((x) => x.title)).toEqual(["New"]);
        expect(previewed.preview.toSkip).toHaveLength(1);
        expect(previewed.preview.toSkip[0].reason).toBe("duplicate");
        expect(previewed.preview.hasExtras).toBe(true);
        expect(previewed.preview.extras.userRole).toBe("ops");
        // Dry-run: no storage change
        expect(loadWelcomeCustomTemplates().length).toBe(before);
    });

    it("remembers cloud revision for optimistic concurrency", () => {
        expect(loadWelcomeCloudRevision()).toBe("");
        saveWelcomeCloudRevision("abc123");
        expect(loadWelcomeCloudRevision()).toBe("abc123");
        expect(localStorage.getItem(WELCOME_CLOUD_REVISION_KEY)).toBe("abc123");
        saveWelcomeCloudRevision("");
        expect(loadWelcomeCloudRevision()).toBe("");
    });
});

describe("welcome quick hints", () => {
    it("resolves all curated hints to live scenario cards", () => {
        const resolved = resolveWelcomeQuickHints();
        expect(resolved.length).toBe(WELCOME_QUICK_HINTS.length);
        for (const h of resolved) {
            expect(h.prompt.textEn).toBe(h.textEn);
            expect(h.key).toContain(h.tabId);
        }
    });
});

describe("shouldOfferWelcomeTemplateSave", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("offers save for long enough titled chat prompts", () => {
        expect(shouldOfferWelcomeTemplateSave({
            title: "竞品分析",
            body: "帮我做一份可汇报的竞品分析\n行业：SaaS\n请输出结论。",
        })).toBe(true);
    });

    it("skips coding agent modes, short bodies, and already-saved bodies", () => {
        expect(shouldOfferWelcomeTemplateSave({
            title: "实现功能",
            body: "按需求实现功能\n需求：登录",
            agentMode: "coding_dev",
        })).toBe(false);
        expect(shouldOfferWelcomeTemplateSave({
            title: "短",
            body: "太短了",
        })).toBe(false);

        const body = "帮我写一份项目周报\n项目：Alpha\n本周进展：完成上线。";
        saveWelcomeCustomTemplate({ title: "周报", body });
        expect(isWelcomeTemplateBodySaved(body)).toBe(true);
        expect(shouldOfferWelcomeTemplateSave({ title: "周报", body })).toBe(false);
    });
});

describe("filterWelcomeRecentForQuickAccess", () => {
    it("drops recent cards that match a custom template sourceKey and caps length", () => {
        const tab = SCENARIO_TABS[0];
        const p0 = tab.prompts[0];
        const p1 = tab.prompts[1];
        const recent = [
            { tabId: tab.id, prompt: p0, key: `${tab.id}::${p0.textEn}` },
            { tabId: tab.id, prompt: p1, key: `${tab.id}::${p1.textEn}` },
            ...tab.prompts.slice(2, 6).map((p) => ({
                tabId: tab.id,
                prompt: p,
                key: `${tab.id}::${p.textEn}`,
            })),
        ];
        const custom: WelcomeCustomTemplate[] = [{
            id: "c1",
            title: p0.text,
            body: p0.template || p0.text,
            sourceKey: `${tab.id}::${p0.textEn}`,
            createdAt: 1,
            usedAt: 1,
        }];
        const filtered = filterWelcomeRecentForQuickAccess(recent, custom, 4);
        expect(filtered.every((r) => r.key !== `${tab.id}::${p0.textEn}`)).toBe(true);
        expect(filtered.length).toBeLessThanOrEqual(4);
        expect(filtered[0].key).toBe(`${tab.id}::${p1.textEn}`);
    });
});
