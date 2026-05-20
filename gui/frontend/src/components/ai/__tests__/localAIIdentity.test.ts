import { describe, expect, it } from "vitest";
import { isLocalAIName, isLocalParticipant, localAINameForLang, participantNameForId } from "../localAIIdentity";

describe("localAIIdentity", () => {
    it("uses valid localized local AI labels", () => {
        expect(localAINameForLang("en")).toBe("Local AI");
        expect(localAINameForLang("zh-CN")).toBe("\u672c\u673aAI");
        expect(localAINameForLang("zh-Hant")).toBe("\u672c\u6a5fAI");
    });

    it("recognizes localized local AI labels with optional spaces", () => {
        expect(isLocalAIName("Local AI")).toBe(true);
        expect(isLocalAIName("local-ai")).toBe(true);
        expect(isLocalAIName("\u672c\u673a AI")).toBe(true);
        expect(isLocalAIName("\u672c\u6a5f AI")).toBe(true);
        expect(isLocalAIName("\u672c\u5730 AI")).toBe(true);
        expect(isLocalAIName("\u672c\u5730")).toBe(true);
        expect(isLocalAIName("\u672c\u673aAI2")).toBe(false);
    });
    it("resolves participant names by normalized id", () => {
        const names = { "machine-local": "Local AI", "VE-A": "Agent A" };
        expect(participantNameForId(names, " MACHINE-LOCAL ")).toBe("Local AI");
        expect(participantNameForId(names, "ve-a")).toBe("Agent A");
        expect(isLocalParticipant({ participantNames: names, localParticipantIds: [] }, " MACHINE-LOCAL ")).toBe(true);
    });
});