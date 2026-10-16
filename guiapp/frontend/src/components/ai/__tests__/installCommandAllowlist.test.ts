import { describe, expect, it } from "vitest";
import {
    INSTALL_BIN_PREFIX,
    isInstallActionAllowed,
    isInstallCLIBinaryPrefix,
    isInstallNestedParent,
    isKnownInstallCommand,
    normalizeInstallCommand,
} from "../installCommandAllowlist";
import { isInstallCommandText } from "../composeAction";

describe("shared installCommandAllowlist", () => {
    it("normalizes aliases", () => {
        expect(normalizeInstallCommand("skills")).toBe("skill");
        expect(normalizeInstallCommand("plugins")).toBe("plugin");
        expect(normalizeInstallCommand("mcp")).toBe("mcp");
        expect(isKnownInstallCommand("skills")).toBe(true);
        expect(isKnownInstallCommand("nope")).toBe(false);
    });

    it("mirrors Go allowlist action rules", () => {
        expect(isInstallActionAllowed("skill", ["search"])).toBe(true);
        expect(isInstallActionAllowed("skills", ["install"])).toBe(true);
        expect(isInstallActionAllowed("skill", ["help"])).toBe(true);
        expect(isInstallActionAllowed("mcp", ["add"])).toBe(true);
        expect(isInstallActionAllowed("plugin", ["marketplace", "add"])).toBe(true);
        expect(isInstallActionAllowed("plugin", ["market", "list"])).toBe(true);
        expect(isInstallActionAllowed("plugin", ["marketplace"])).toBe(true);
        expect(isInstallActionAllowed("plugin", ["marketplace", "help"])).toBe(true);
        expect(isInstallActionAllowed("plugin", ["marketplace", "destroy"])).toBe(false);
        expect(isInstallActionAllowed("skill", ["foo"])).toBe(false);
        expect(isInstallActionAllowed("plugin", ["enable"])).toBe(false);
    });

    it("matches CLI binary prefixes from JSON", () => {
        expect(isInstallCLIBinaryPrefix("maclaw-tui")).toBe(true);
        expect(isInstallCLIBinaryPrefix("maclaw-tui.exe")).toBe(true);
        expect(isInstallCLIBinaryPrefix(String.raw`C:\bin\maclaw-tui.exe`)).toBe(true);
        expect(isInstallCLIBinaryPrefix("/usr/local/bin/maclaw-tui")).toBe(true);
        expect(isInstallCLIBinaryPrefix(String.raw`"C:\Program Files\maclaw.exe"`)).toBe(true);
        expect(isInstallCLIBinaryPrefix("npm")).toBe(false);
        expect(isInstallCLIBinaryPrefix("random-tui")).toBe(false);
        expect(INSTALL_BIN_PREFIX.test("maclaw-tui skill list")).toBe(true);
    });

    it("keeps frontend/backend parity for chat recognition", () => {
        const rows: Array<{ text: string; want: boolean }> = [
            { text: "/skill list", want: true },
            { text: "/skills search pdf", want: true },
            { text: "/mcp install x@y", want: true },
            { text: "/plugin marketplace add a/b", want: true },
            { text: "/plugin market list", want: true },
            { text: "/plugin marketplace destroy", want: false },
            { text: "skill is important", want: false },
            { text: "maclaw-tui skill list", want: true },
            { text: "maclaw-tui /skill list", want: true },
            { text: "maclaw-tui ／skill list", want: true },
            { text: String.raw`C:\bin\maclaw-tui.exe skill list`, want: true },
            { text: `/mcp add --name "my server"`, want: true },
            { text: "/skill foo", want: false },
            { text: "/skill install help", want: true }, // target id "help", not meta
            { text: "/plugin marketplace help", want: true },
        ];
        for (const { text, want } of rows) {
            expect(isInstallCommandText(text), text).toBe(want);
        }
    });

    it("detects nested parents", () => {
        expect(isInstallNestedParent("plugin", "marketplace")).toBe(true);
        expect(isInstallNestedParent("plugin", "market")).toBe(true);
        expect(isInstallNestedParent("skill", "install")).toBe(false);
    });
});
