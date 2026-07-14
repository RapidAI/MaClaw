// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { SidebarSystemStatus } from "../SidebarSystemStatus";

const baseProps = {
    lang: "zh-Hans",
    maclawLLMOnline: true,
    remoteActivationStatus: {},
    qqBotStatus: "off",
    telegramStatus: "off",
    weixinStatus: "off",
    lansengerStatus: "off",
    sidebarCurrentProviderTokenUsage: {
        provider: "OpenAI",
        isHubService: false,
        input: 0,
        output: 0,
        total: 0,
        cachedInput: 0,
        cacheWrite: 0,
        requests: 0,
        cachedRequests: 0,
        localCacheRequests: 0,
        localCacheHits: 0,
    },
    sidebarHubCredits: null,
    formatSidebarTokens: (n: number) => String(n),
    formatSidebarHubExpiry: () => "",
    formatSidebarHubTotalCredits: () => "",
    formatSidebarHubUsedCredits: () => "",
    formatSidebarCredit: (n: number) => String(n),
    unlimitedHubCreditText: "",
    noHubAuthorizationText: "",
    showHubCreditAction: false,
    openHubCreditsPage: vi.fn(),
    openLLMSettingsPage: vi.fn(),
    availableProviders: [{ name: "OpenAI", url: "https://x", isHubService: false }],
} as any;

describe("SidebarSystemStatus MoA sticky", () => {
    it("shows council badge when sticky active and toggles via dropdown", () => {
        const onToggle = vi.fn();
        render(
            <SidebarSystemStatus
                {...baseProps}
                moaSticky={{ available: true, active: true, label: "方案评审" }}
                onToggleMoASticky={onToggle}
            />,
        );
        expect(screen.getByText(/会诊/)).toBeTruthy();

        // Open chevron dropdown
        const buttons = screen.getAllByRole("button");
        const chevron = buttons.find((b) => b.getAttribute("aria-haspopup") === "listbox");
        expect(chevron).toBeTruthy();
        fireEvent.click(chevron!);
        const toggle = screen.getByTestId("sidebar-moa-sticky-toggle");
        fireEvent.click(toggle);
        expect(onToggle).toHaveBeenCalledWith(false);
    });
});
