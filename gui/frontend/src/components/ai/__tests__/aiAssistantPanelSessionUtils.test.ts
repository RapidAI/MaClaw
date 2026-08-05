import { describe, expect, it } from "vitest";
import { buildProjectTabRecentMessages, chatHistoriesEquivalent, isACPAssistantSessionKey, messageBelongsToSession, normalizeAssistantSessionKey, normalizeProjectSessionPath, projectPathFromSessionKey, projectSessionKey } from "../aiAssistantPanelSessionUtils";
import type { ChatMessage } from "../useAIAssistant";

describe("aiAssistantPanelSessionUtils", () => {
    it("persists an async guide-injection marker and its session owner", () => {
        const plain: ChatMessage = {
            id: "same-message",
            role: "user",
            content: "redirect the running task",
            sessionKey: "desktop-user:D:/tasks/demo",
            timestamp: 1,
        };
        const injected: ChatMessage = { ...plain, kind: "guideInjection" };

        expect(chatHistoriesEquivalent([plain], [injected])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, sessionKey: "desktop-user:D:/tasks/other" }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, reasoning: "a later streamed detail" }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, actions: [{ label: "Apply", command: "apply", style: "primary" }] }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [plain])).toBe(true);
    });

    it("does not replay an injected guide as a later project-tab user turn", () => {
        expect(buildProjectTabRecentMessages([
            { id: "guide", role: "user", kind: "guideInjection", content: "redirect this live task", timestamp: 1 },
            { id: "assistant", role: "assistant", content: "continuing with the revised scope", timestamp: 2 },
        ])).toEqual([
            { role: "assistant", content: "continuing with the revised scope" },
        ]);
    });
    it("normalizes project session paths for stable task identity", () => {
        expect(normalizeProjectSessionPath(" d:\\workprj\\task\\. ")).toBe("D:/workprj/task");
        expect(projectSessionKey("d:/workprj/task/.")).toBe("desktop-user:D:/workprj/task");
        expect(projectPathFromSessionKey("desktop-user:d:\\workprj\\task\\")).toBe("D:/workprj/task");
    });

    it("matches legacy slash variants to the normalized session", () => {
        const message = { id: "m1", role: "assistant", content: "ok", sessionKey: "desktop-user:d:\\workprj\\task\\" } as ChatMessage;
        expect(messageBelongsToSession(message, "desktop-user:D:/workprj/task")).toBe(true);
    });

    it("keeps ACP owners opaque and outside path-based routing", () => {
        const owner = "desktop-user:acp:acp_gui_session_42";
        expect(isACPAssistantSessionKey(owner)).toBe(true);
        expect(normalizeAssistantSessionKey(owner)).toBe(owner);
        expect(projectPathFromSessionKey(owner)).toBe("");
    });
});
