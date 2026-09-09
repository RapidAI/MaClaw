import { describe, expect, it } from "vitest";
import { clearAssistantRoundProse } from "./assistantRoundProse";

describe("clearAssistantRoundProse", () => {
    it("clears the answer draft while keeping prior thinking", () => {
        const next = clearAssistantRoundProse({
            content: "I'll connect next.",
            reasoning: "Need the MySQL profile first.",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("Need the MySQL profile first.\n");
    });

    it("keeps thinking when the previous round had no answer text", () => {
        const next = clearAssistantRoundProse({
            content: "",
            reasoning: "First I listed connections.",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("First I listed connections.\n");
    });

    it("does not add a second separator when thinking already ends with a newline", () => {
        const next = clearAssistantRoundProse({
            content: "draft",
            reasoning: "Done thinking.\n",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("Done thinking.\n");
    });

    it("leaves an empty reasoning trail untouched", () => {
        const next = clearAssistantRoundProse({
            content: "draft",
            reasoning: "",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("");
    });
});
