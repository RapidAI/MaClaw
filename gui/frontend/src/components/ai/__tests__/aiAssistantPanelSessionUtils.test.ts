import { describe, expect, it } from "vitest";
import { messageBelongsToSession, normalizeProjectSessionPath, projectPathFromSessionKey, projectSessionKey } from "../aiAssistantPanelSessionUtils";
import type { ChatMessage } from "../useAIAssistant";

describe("aiAssistantPanelSessionUtils", () => {
    it("normalizes project session paths for stable task identity", () => {
        expect(normalizeProjectSessionPath(" d:\\workprj\\task\\. ")).toBe("D:/workprj/task");
        expect(projectSessionKey("d:/workprj/task/.")).toBe("desktop-user:D:/workprj/task");
        expect(projectPathFromSessionKey("desktop-user:d:\\workprj\\task\\")).toBe("D:/workprj/task");
    });

    it("matches legacy slash variants to the normalized session", () => {
        const message = { id: "m1", role: "assistant", content: "ok", sessionKey: "desktop-user:d:\\workprj\\task\\" } as ChatMessage;
        expect(messageBelongsToSession(message, "desktop-user:D:/workprj/task")).toBe(true);
    });
});
