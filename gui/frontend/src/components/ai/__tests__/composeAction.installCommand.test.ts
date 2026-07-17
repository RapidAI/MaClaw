import { describe, expect, it } from "vitest";
import { installCommandFields, isInstallCommandText, normalizeInstallCommandText } from "../composeAction";

describe("isInstallCommandText", () => {
    it("matches skill/mcp/plugin slash commands", () => {
        expect(isInstallCommandText("/skill")).toBe(true);
        expect(isInstallCommandText("/skill search pdf")).toBe(true);
        expect(isInstallCommandText("/skill install owner/repo")).toBe(true);
        expect(isInstallCommandText("/skill help")).toBe(true);
        expect(isInstallCommandText("/mcp install ida-pro-mcp@mrexodia")).toBe(true);
        expect(isInstallCommandText("/plugin marketplace add mrexodia/codex-marketplace")).toBe(true);
        expect(isInstallCommandText("/plugin marketplace")).toBe(true);
        expect(isInstallCommandText("/plugin add ida-pro-mcp@mrexodia")).toBe(true);
        expect(isInstallCommandText("/PLUGIN installed")).toBe(true);
    });

    it("matches CLI-prefixed paste", () => {
        expect(isInstallCommandText("maclaw-tui skill list")).toBe(true);
        expect(isInstallCommandText("maclaw-tui.exe plugin add x@y")).toBe(true);
        expect(isInstallCommandText("maclaw skill list")).toBe(true);
    });

    it("rejects free-form chat and unknown actions", () => {
        expect(isInstallCommandText("skill is important")).toBe(false);
        expect(isInstallCommandText("please install an mcp server")).toBe(false);
        expect(isInstallCommandText("plugin system design")).toBe(false);
        expect(isInstallCommandText("/skill foo")).toBe(false);
        expect(isInstallCommandText("/plugin marketplace destroy")).toBe(false);
        expect(isInstallCommandText("/plugin destroy all")).toBe(false);
        expect(isInstallCommandText("/btw hello")).toBe(false);
        expect(isInstallCommandText("")).toBe(false);
    });

    it("accepts fullwidth slash and BOM-prefixed paste", () => {
        expect(isInstallCommandText("／skill list")).toBe(true);
        expect(isInstallCommandText("／plugin marketplace list")).toBe(true);
        expect(isInstallCommandText("\uFEFF/mcp list")).toBe(true);
    });

    it("accepts binary prefix then slash (paste parity with Go)", () => {
        expect(isInstallCommandText("maclaw-tui /skill list")).toBe(true);
        expect(isInstallCommandText("maclaw-tui ／skill list")).toBe(true);
        expect(isInstallCommandText("maclaw-tui.exe /plugin marketplace list")).toBe(true);
    });
});

describe("normalizeInstallCommandText", () => {
    it("canonicalizes aliases, fullwidth slash, and CLI prefix", () => {
        expect(normalizeInstallCommandText("/skills list")).toBe("/skill list");
        expect(normalizeInstallCommandText("／mcp list")).toBe("/mcp list");
        expect(normalizeInstallCommandText("maclaw-tui /skill list")).toBe("/skill list");
        expect(normalizeInstallCommandText("maclaw-tui ／plugin market list")).toBe("/plugin market list");
        expect(normalizeInstallCommandText("\uFEFF/skills search pdf")).toBe("/skill search pdf");
        expect(normalizeInstallCommandText(String.raw`C:\tools\maclaw-tui.exe skill list`)).toBe("/skill list");
        expect(normalizeInstallCommandText(`"/usr/bin/maclaw-tui" skill list`)).toBe("/skill list");
        expect(normalizeInstallCommandText("skill is important")).toBeNull();
        expect(normalizeInstallCommandText("/skill foo")).toBeNull();
    });

    it("preserves quoted multi-word args", () => {
        expect(installCommandFields(`add --name "my server" --url http://x`)).toEqual([
            "add",
            "--name",
            "my server",
            "--url",
            "http://x",
        ]);
        expect(normalizeInstallCommandText(`/mcp add --name "my server" --url http://x`)).toBe(
            `/mcp add --name "my server" --url http://x`,
        );
        expect(isInstallCommandText(`/mcp add --name "my server"`)).toBe(true);
    });
});
