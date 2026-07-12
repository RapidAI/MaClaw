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
    it("renders a digital employee avatar when the tab has one", () => {
        const tab = {
            id: "ve-avatar-tab",
            type: "ve" as const,
            title: "Avatar Agent",
            veId: "ve-avatar",
            avatarDataURL: "data:image/jpeg;base64,/9j/",
            closable: true,
        };

        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const avatar = screen.getByTestId("ai-tab-avatar-ve-avatar-tab") as HTMLImageElement;
        expect(avatar.getAttribute("src")).toBe("data:image/jpeg;base64,/9j/");
        expect(screen.getByTestId("ai-tab-ve-avatar-tab").textContent).toContain("Avatar Agent");
    });

    it("keeps VE online status visible when an avatar replaces the default icon", () => {
        const tab = {
            id: "ve-avatar-offline-tab",
            type: "ve" as const,
            title: "Avatar Agent",
            veId: "ve-avatar",
            onlineStatus: "offline" as const,
            avatarDataURL: "data:image/jpeg;base64,/9j/",
            closable: true,
        };

        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        expect(screen.getByTestId("ai-tab-avatar-ve-avatar-offline-tab")).toBeTruthy();
        expect(screen.getByTestId("ai-tab-status-ve-avatar-offline-tab").style.background).toBe("rgb(107, 114, 128)");
    });

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

        expect(getAITabDisplayTitle(tab, "en")).toBe("Agent A, Contract Bot, Local");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-group-live");
        expect(item.textContent).toContain("Agent A, Contract Bot, Local");
        expect(item.textContent).not.toContain("m_b1821505498d817c");
        expect(item.getAttribute("title")).toBe("Agent A, Contract Bot, Local");
    });

    it("uses participant names and primary title across ve aliases", () => {
        const tab = {
            id: "group-alias",
            type: "group" as const,
            title: "Agent A",
            veId: "ve-machine-a",
            participants: ["machine-a", "machine-b"],
            participantNames: { "ve-machine-b": "Contract Bot" },
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Agent A, Contract Bot");
    });

    it("uses the custom group title without treating it as the primary VE name", () => {
        const tab = {
            id: "group-renamed",
            type: "group" as const,
            title: "Agent A",
            groupTitle: "Review room",
            veId: "ve-a",
            participants: ["ve-a", "local-maclaw"],
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Review room");
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

        expect(getAITabDisplayTitle(tab, "en")).toBe("Participant 1, Local");
        render(<AITabItem tab={tab} active={true} theme={theme} onActivate={vi.fn()} onClose={vi.fn()} lang="en" />);

        const item = screen.getByTestId("ai-tab-group-raw-title");
        expect(item.textContent).toContain("Participant 1, Local");
        expect(item.textContent).not.toContain("m_b1821505498d817c");
    });

    it("localizes fallback group participant names", () => {
        const tab = {
            id: "group-raw-title",
            type: "group" as const,
            title: "m_b1821505498d817c",
            veId: "m_b1821505498d817c",
            participants: ["m_b1821505498d817c", "local-maclaw"],
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "zh-CN")).toBe("参与者 1, 本机");
        expect(getAITabDisplayTitle(tab, "zh-Hant")).toBe("參與者 1, 本機");
    });

    it("normalizes Local participant ids in group titles", () => {
        const tab = {
            id: "group-local-normalized",
            type: "group" as const,
            title: "Agent A",
            veId: "ve-a",
            participants: ["ve-a", " LOCAL-MACLAW "],
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Agent A, Local");
        expect(getAITabDisplayTitle(tab, "zh-CN")).toBe("Agent A, \u672c\u673a");
    });


    it("localizes canonical Local participant ids", () => {
        const tab = {
            id: "group-local-canonical",
            type: "group" as const,
            title: "Agent A",
            veId: "ve-a",
            participants: ["ve-a", "machine-local"],
            participantNames: { "machine-local": "Local" },
            localParticipantIds: ["machine-local"],
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Agent A, Local");
        expect(getAITabDisplayTitle(tab, "zh-CN")).toBe("Agent A, 本机");
    });

    it("localizes direct VE fallback titles", () => {
        const tab = {
            id: "ve-raw-tab-zh",
            type: "ve" as const,
            title: "m_b1821505498d817c",
            veId: "m_b1821505498d817c",
            closable: true,
        };

        expect(getAITabDisplayTitle(tab, "zh-CN")).toBe("数字员工");
        expect(getAITabDisplayTitle(tab, "zh-Hant")).toBe("數字員工");
    });
    it("keeps history group topic titles unchanged", () => {
        const tab = { id: "history-disc-1", type: "group" as const, title: "Case discussion", participants: ["me", "ve-a"], closable: true };

        expect(getAITabDisplayTitle(tab, "en")).toBe("Case discussion");
    });
});
