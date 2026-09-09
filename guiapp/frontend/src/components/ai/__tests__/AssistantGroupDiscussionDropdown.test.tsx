import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantGroupDiscussionDropdown } from "../AssistantGroupDiscussionDropdown";
import { lightTheme } from "../aiAssistantPanelTheme";

describe("AssistantGroupDiscussionDropdown", () => {
    const baseProps = {
        bindGroupDiscussionPress: (handler: () => void) => ({ onClick: handler }),
        copiedHandoff: false,
        copySafeHandoff: vi.fn(),
        groupActiveTalks: 0,
        groupDiscussion: { onAcceptInvite: vi.fn(), onRejectInvite: vi.fn() },
        groupDiscussionBusy: "",
        groupDiscussionEnabled: true,
        groupDiscussionScopeText: "Project",
        groupDiscussionStatus: null,
        groupPendingInvites: [],
        groupReadyTalks: 0,
        groupStaleTalks: 0,
        groupWaitingTalks: 0,
        lang: "en",
        primaryTraceFocus: "",
        runGroupDiscussionAction: vi.fn(),
        safeHandoff: "",
        setGroupDiscussionOpen: vi.fn(),
        theme: lightTheme,
        themeMode: "light" as const,
    };

    it("uses readable invite fallbacks instead of raw ids", () => {
        render(
            <AssistantGroupDiscussionDropdown
                {...baseProps}
                groupPendingInvites={[{
                    id: "invite-1",
                    invite_id: "invite-1",
                    consultation_id: "disc-raw-123",
                    from_id: "m_b1821505498d817c",
                }]}
            />
        );

        expect(screen.getByTestId("group-discussion-invite-title").textContent).toBe("Discussion invite");
        expect(screen.getByTestId("group-discussion-invite-sender").textContent).toBe("Inviter");
        expect(document.body.textContent).not.toContain("disc-raw-123");
        expect(document.body.textContent).not.toContain("m_b1821505498d817c");
    });

    it("uses invite sender fallback when from_name looks like a raw id", () => {
        render(
            <AssistantGroupDiscussionDropdown
                {...baseProps}
                groupPendingInvites={[{
                    id: "invite-raw-name",
                    invite_id: "invite-raw-name",
                    topic: "Review",
                    from_id: "profile-1",
                    from_name: "m_b1821505498d817c",
                }]}
            />
        );

        expect(screen.getByTestId("group-discussion-invite-sender").textContent).toBe("Inviter");
        expect(document.body.textContent).not.toContain("m_b1821505498d817c");
    });

});
