import { describe, expect, it } from "vitest";
import {
    applyComposeActionToText,
    btwQueryFromText,
    getComposeActionIcon,
    getComposeActionLabel,
    getComposeActionPlaceholder,
    isBtwCommandText,
    LOOP_COMMAND_TEMPLATE,
    PLUS_MENU_ACTION_ITEMS,
    PLUS_MENU_COMPOSE_TEMPLATE_ITEMS,
    PLUS_MENU_FIRE_ITEMS,
    PLUS_MENU_ITEMS,
} from "../composeAction";

describe("applyComposeActionToText", () => {
    it("returns empty for blank input", () => {
        expect(applyComposeActionToText("", "goal")).toBe("");
        expect(applyComposeActionToText("   ", "btw")).toBe("");
        expect(applyComposeActionToText("hello", null)).toBe("hello");
    });

    it("prefixes free-form text in goal, btw, moa, and Computer Use modes", () => {
        expect(applyComposeActionToText("实现用户登录", "goal")).toBe("/goal 实现用户登录");
        expect(applyComposeActionToText("  ship the API  ", "goal")).toBe("/goal ship the API");
        expect(applyComposeActionToText("这个项目用什么框架", "btw")).toBe("/btw 这个项目用什么框架");
        expect(applyComposeActionToText("React 19 changes", "btw")).toBe("/btw React 19 changes");
        expect(applyComposeActionToText("评审迁移方案", "moa")).toBe("/moa 评审迁移方案");
        expect(applyComposeActionToText("  design a migration plan  ", "moa")).toBe("/moa design a migration plan");
        expect(applyComposeActionToText("打开记事本并写简历", "computer")).toBe("@computer 打开记事本并写简历");
    });

    it("does not double-prefix existing commands", () => {
        expect(applyComposeActionToText("/goal", "goal")).toBe("/goal");
        expect(applyComposeActionToText("/goal status", "goal")).toBe("/goal status");
        expect(applyComposeActionToText("/btw", "btw")).toBe("/btw");
        expect(applyComposeActionToText("  /btw latest Go  ", "btw")).toBe("/btw latest Go");
        expect(applyComposeActionToText("/moa", "moa")).toBe("/moa");
        expect(applyComposeActionToText("/moa already", "moa")).toBe("/moa already");
        expect(applyComposeActionToText("/MOA already", "moa")).toBe("/MOA already");
        expect(applyComposeActionToText("@computer already active", "computer")).toBe("@computer already active");
        expect(applyComposeActionToText("@COMPUTER\nopen the report", "computer")).toBe("@COMPUTER\nopen the report");
        expect(applyComposeActionToText("@computer\topen the report", "computer")).toBe("@computer\topen the report");
        expect(applyComposeActionToText("@computerized report", "computer")).toBe("@computerized report");
        expect(applyComposeActionToText("@computerx", "computer")).toBe("@computerx");
        expect(applyComposeActionToText("/goalkeeper review", "goal")).toBe("/goalkeeper review");
        expect(applyComposeActionToText("@browser open the report", "computer")).toBe("@browser open the report");
        expect(applyComposeActionToText("@浏览器 打开报告", "computer")).toBe("@浏览器 打开报告");
        expect(applyComposeActionToText("@teammate review this", "goal")).toBe("/goal @teammate review this");
        expect(applyComposeActionToText("@teammate review this", "btw")).toBe("/btw @teammate review this");
    });

    it("does not wrap install slash commands under compose modes", () => {
        expect(applyComposeActionToText("/skill list", "goal")).toBe("/skill list");
        expect(applyComposeActionToText("/mcp install x@y", "btw")).toBe("/mcp install x@y");
        expect(applyComposeActionToText("/plugin add ida-pro-mcp@mrexodia", "moa")).toBe(
            "/plugin add ida-pro-mcp@mrexodia",
        );
        // CLI prefix / fullwidth / aliases are canonicalized to /cmd …
        expect(applyComposeActionToText("maclaw-tui skill list", "goal")).toBe("/skill list");
        expect(applyComposeActionToText("／skill list", "goal")).toBe("/skill list");
        expect(applyComposeActionToText("\uFEFF/skill list", "btw")).toBe("/skill list");
        expect(applyComposeActionToText("/skills list", "goal")).toBe("/skill list");
        // Other utility slash commands also stay intact (not install → not rewritten).
        expect(applyComposeActionToText("/help", "goal")).toBe("/help");
        expect(applyComposeActionToText("/memory", "btw")).toBe("/memory");
        expect(applyComposeActionToText("／help", "goal")).toBe("／help");
    });
});

describe("isBtwCommandText / btwQueryFromText", () => {
    it("detects and strips /btw (case-insensitive)", () => {
        expect(isBtwCommandText("/btw")).toBe(true);
        expect(isBtwCommandText("/BTW hello")).toBe(true);
        expect(isBtwCommandText("  /btw hello  ")).toBe(true);
        expect(isBtwCommandText("/goal x")).toBe(false);
        expect(btwQueryFromText("/btw hello world")).toBe("hello world");
        expect(btwQueryFromText("/BTW")).toBe("");
        expect(btwQueryFromText("  /btw   spaced  ")).toBe("spaced");
    });

    it("does not double-prefix when prefix casing differs", () => {
        expect(applyComposeActionToText("/BTW already", "btw")).toBe("/BTW already");
        expect(applyComposeActionToText("/Goal already", "goal")).toBe("/Goal already");
    });
});

describe("PLUS_MENU_ITEMS", () => {
    it("assigns a distinct meaning-matched icon to every command", () => {
        const icons = PLUS_MENU_ITEMS.map((item) => item.icon);
        expect(new Set(icons).size).toBe(PLUS_MENU_ITEMS.length);
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "newConversation")?.icon).toBe("messagePlus");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "goal")?.icon).toBe("target");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "btw")?.icon).toBe("search");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "moa")?.icon).toBe("layers");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "computer")?.icon).toBe("monitor");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "loop")?.icon).toBe("repeat");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "memory")?.icon).toBe("brain");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "compress")?.icon).toBe("compress");
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "sessions")).toBeUndefined();
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "help")?.icon).toBe("helpCircle");
    });

    it("pre-splits action / compose-template / fire groups without dropping items", () => {
        expect(
            PLUS_MENU_ACTION_ITEMS.length + PLUS_MENU_COMPOSE_TEMPLATE_ITEMS.length + PLUS_MENU_FIRE_ITEMS.length,
        ).toBe(PLUS_MENU_ITEMS.length);
        expect(PLUS_MENU_ACTION_ITEMS.every((i) => i.kind === "action")).toBe(true);
        expect(PLUS_MENU_FIRE_ITEMS.every((i) => i.kind === "fire")).toBe(true);
        expect(PLUS_MENU_COMPOSE_TEMPLATE_ITEMS.every((i) => i.kind !== "fire" && i.kind !== "action")).toBe(true);
    });

    it("provides a loop template for insertion", () => {
        expect(LOOP_COMMAND_TEMPLATE.startsWith("/loop ")).toBe(true);
        expect(PLUS_MENU_ITEMS.find((i) => i.id === "loop")?.template).toBe(LOOP_COMMAND_TEMPLATE);
    });
});

describe("compose action labels and placeholders", () => {
    it("derives labels/icons from the menu registry", () => {
        expect(getComposeActionLabel("goal", true)).toBe("目标");
        expect(getComposeActionLabel("btw", true)).toBe("旁路查询");
        expect(getComposeActionLabel("moa", true)).toBe("多模型会诊");
        expect(getComposeActionLabel("moa", false)).toBe("MoA council");
        expect(getComposeActionLabel("computer", true)).toBe("桌面操控");
        expect(getComposeActionLabel("computer", false)).toBe("Computer Use");
        expect(getComposeActionIcon("goal")).toBe("target");
        expect(getComposeActionIcon("btw")).toBe("search");
        expect(getComposeActionIcon("moa")).toBe("layers");
        expect(getComposeActionIcon("computer")).toBe("monitor");
        expect(getComposeActionPlaceholder("goal", true)).toContain("目标");
        expect(getComposeActionPlaceholder("btw", true)).toContain("旁路");
        expect(getComposeActionPlaceholder("moa", true)).toContain("多模型会诊");
        expect(getComposeActionPlaceholder("computer", true)).toContain("桌面");
        expect(getComposeActionPlaceholder(null, true)).toBeNull();
    });
});
