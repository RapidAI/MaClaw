import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AITabItem, getAITabDisplayTitle } from "../AITabItem";
import type { Theme } from "../aiAssistantPanelTheme";

const theme = {
    bg: "#fff",
    text: "#111",
    textMuted: "#777",
    btnColor: "#2563eb",
    divider: "#ddd",
} as Theme;

describe("AITabItem", () => {
    it("shows live group participant names instead of raw ids in the tab title", () => {
        const tab = {
            id: "group-live",
            type: "group" as const,
            title: "Agent A",
            veId: "ve-a",
            participants: ["ve-a", "m_b1821505498d817c", "local-maclaw"],
            participantNames: { "m_b1821505498d817c": "Contract Bot" },
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Agent A, Contract Bot, Local AI");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-group-live");
        expect(item.textContent).toContain("Agent A, Contract Bot, Local AI");
        expect(item.textContent).not.toContain("m_b1821505498d817c");
        expect(item.getAttribute("title")).toBe("Agent A, Contract Bot, Local AI");
    });

    it("uses a friendly direct VE tab title when the title looks like a profile id", () => {
        const tab = {
            id: "ve-profile-tab",
            type: "ve" as const,
            title: "profile-raw",
            veId: "profile-raw",
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Digital employee");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-ve-profile-tab");
        expect(item.textContent).toContain("Digital employee");
        expect(item.textContent).not.toContain("profile-raw");
    });

    it("uses a friendly direct VE tab title when the title is a raw id", () => {
        const tab = {
            id: "ve-raw-tab",
            type: "ve" as const,
            title: "m_b1821505498d817c",
            veId: "m_b1821505498d817c",
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Digital employee");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-ve-raw-tab");
        expect(item.textContent).toContain("Digital employee");
        expect(item.textContent).not.toContain("m_b1821505498d817c");
        expect(item.getAttribute("title")).toBe("Digital employee");
    });

    it("uses a friendly title when the primary VE title is a raw id", () => {
        const tab = {
            id: "group-raw-title",
            type: "group" as const,
            title: "m_b1821505498d817c",
            veId: "m_b1821505498d817c",
            participants: ["m_b1821505498d817c", "local-maclaw"],
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Participant 1, Local AI");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-group-raw-title");
        expect(item.textContent).toContain("Participant 1, Local AI");
        expect(item.textContent).not.toContain("m_b1821505498d817c");
    });

    it("keeps history group topic titles unchanged", () => {
        const tab = { id: "history-disc-1", type: "group" as const, title: "Case discussion", participants: ["me", "ve-a"], closable: true };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Case discussion");
    });
});
