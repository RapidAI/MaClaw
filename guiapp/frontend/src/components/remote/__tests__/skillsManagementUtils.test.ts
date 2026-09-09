import { describe, expect, it } from "vitest";
import { displayHubVersion } from "../skillsManagementUtils";

describe("displayHubVersion", () => {
    it("keeps compact version strings", () => {
        expect(displayHubVersion("1.2.3")).toBe("1.2.3");
        expect(displayHubVersion(" v2.0.0-beta+5 ")).toBe("v2.0.0-beta+5");
    });

    it("hides install refs and other long identifiers", () => {
        expect(displayHubVersion("enterprise:功能Skill!hubcenter-8bb7c35f-448e-bd70-1d28805770b0@5")).toBe("");
        expect(displayHubVersion("https://hub.example/skills/demo")).toBe("");
        expect(displayHubVersion("v123456789012345678901234567890123")).toBe("");
    });
});
