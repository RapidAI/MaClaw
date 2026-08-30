import { afterEach, describe, expect, it } from "vitest";
import {
    assistantLightSchemes,
    getAssistantLightScheme,
    readStoredAssistantLightSchemeId,
    ASSISTANT_LIGHT_SCHEME_STORAGE_KEY,
} from "../assistantLightSchemes";

describe("assistant light schemes", () => {
    afterEach(() => {
        window.localStorage.removeItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY);
    });

    it("puts GitHub first and uses it as the fallback scheme", () => {
        expect(assistantLightSchemes[0]?.id).toBe("github");
        expect(getAssistantLightScheme("unknown").id).toBe("github");
    });

    it("defaults to GitHub when no preference is stored", () => {
        expect(readStoredAssistantLightSchemeId()).toBe("github");
    });

    it("preserves valid stored preferences", () => {
        window.localStorage.setItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY, "notion");
        expect(readStoredAssistantLightSchemeId()).toBe("notion");
    });
});
