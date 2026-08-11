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

function openProviderDropdown() {
    const buttons = screen.getAllByRole("button");
    const chevron = buttons.find((b) => b.getAttribute("aria-haspopup") === "listbox");
    expect(chevron).toBeTruthy();
    fireEvent.click(chevron!);
    return chevron!;
}

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

        openProviderDropdown();
        const toggle = screen.getByTestId("sidebar-moa-sticky-toggle");
        fireEvent.click(toggle);
        expect(onToggle).toHaveBeenCalledWith(false);
    });
});

describe("SidebarSystemStatus provider dropdown UX", () => {
    it("marks the current provider with a checkmark, not the literal OK", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { id: "openai", name: "OpenAI", url: "https://x", isHubService: false },
                    { id: "anthropic", name: "Anthropic", url: "https://y", isHubService: false },
                ]}
            />,
        );

        openProviderDropdown();

        const listbox = screen.getByRole("listbox");
        expect(listbox).toBeTruthy();
        expect(listbox.textContent).not.toMatch(/\bOK\b/);
        expect(listbox.textContent).toContain("\u2713");

        const selected = screen.getByRole("option", { selected: true });
        expect(selected.textContent).toContain("\u2713");
        expect(selected.textContent).toContain("OpenAI");
    });

    it("positions the fixed menu with left/bottom (or top), never right-aligned off-screen", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { id: "openai", name: "OpenAI", url: "https://x", isHubService: false },
                    { id: "anthropic", name: "Anthropic", url: "https://y", isHubService: false },
                ]}
            />,
        );

        openProviderDropdown();
        const listbox = screen.getByRole("listbox") as HTMLElement;
        expect(listbox.style.position).toBe("fixed");
        expect(listbox.style.left).toMatch(/px$/);
        // Right-alignment was the clip bug; we must not pin via `right`.
        expect(listbox.style.right).toBe("");
        // Must not remain in the pre-measure hidden state after layout.
        expect(listbox.style.visibility).not.toBe("hidden");
        const opensAbove = listbox.style.bottom !== "" && listbox.style.bottom !== "auto";
        const opensBelow = listbox.style.top !== "" && listbox.style.top !== "auto";
        expect(opensAbove || opensBelow).toBe(true);
        // Only one vertical edge should be active.
        expect(opensAbove && opensBelow).toBe(false);
        expect(listbox.style.maxHeight).toMatch(/px$/);
    });

    it("closes on Escape and restores focus to the chevron", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { id: "openai", name: "OpenAI", url: "https://x", isHubService: false },
                    { id: "anthropic", name: "Anthropic", url: "https://y", isHubService: false },
                ]}
            />,
        );
        const chevron = openProviderDropdown();
        expect(screen.getByRole("listbox")).toBeTruthy();
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByRole("listbox")).toBeNull();
        expect(document.activeElement).toBe(chevron);
    });

    it("does not render a leading separator when the menu only has settings", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                // Only the current provider → no switchable rows; settings still available.
                availableProviders={[{ name: "OpenAI", url: "https://x", isHubService: false }]}
            />,
        );
        openProviderDropdown();
        const listbox = screen.getByRole("listbox");
        expect(listbox.querySelector(".sidebar-system-status__provider-dropdown-sep")).toBeNull();
        expect(screen.getByText(/大模型设置/)).toBeTruthy();
    });

    it("stages a provider without closing the dropdown so a model can be chosen next", () => {
        const onSwitchProvider = vi.fn();
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { id: "openai", name: "OpenAI", url: "https://x", isHubService: false },
                    { id: "anthropic", name: "Anthropic", url: "https://y", isHubService: false },
                ]}
                onSwitchProvider={onSwitchProvider}
            />,
        );
        openProviderDropdown();
        fireEvent.click(screen.getByRole("option", { name: "Anthropic" }));
        expect(onSwitchProvider).toHaveBeenCalledWith("anthropic");
        expect(screen.getByRole("listbox")).toBeTruthy();
    });

    it("discards a staged provider when the dropdown is dismissed", () => {
        const onDismissModelMenu = vi.fn();
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[{ name: "OpenAI", url: "https://x", isHubService: false }]}
                currentModel="m1"
                modelOptions={["m1"]}
                onSwitchModel={vi.fn()}
                providerSelectionPending
                onDismissModelMenu={onDismissModelMenu}
            />,
        );

        openProviderDropdown();
        fireEvent.keyDown(document, { key: "Escape" });

        expect(onDismissModelMenu).toHaveBeenCalledTimes(1);
    });

    it("discards a staged provider if the sidebar unmounts while its dropdown is open", () => {
        const onDismissModelMenu = vi.fn();
        const { unmount } = render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[{ name: "OpenAI", url: "https://x", isHubService: false }]}
                currentModel="m1"
                modelOptions={["m1"]}
                onSwitchModel={vi.fn()}
                providerSelectionPending
                onDismissModelMenu={onDismissModelMenu}
            />,
        );

        openProviderDropdown();
        unmount();

        expect(onDismissModelMenu).toHaveBeenCalledTimes(1);
    });

    it("disables the quick picker while an atomic profile save is pending", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                currentModel="m1"
                modelOptions={["m1"]}
                onSwitchModel={vi.fn()}
                profileSavePending
            />,
        );

        const chevron = screen.getAllByRole("button").find((b) => b.getAttribute("aria-haspopup") === "listbox") as HTMLButtonElement;
        expect(chevron.disabled).toBe(true);
        fireEvent.click(chevron);
        expect(screen.queryByRole("listbox")).toBeNull();
    });

    it("lists models under the current provider and switches on click", () => {
        const onSwitchModel = vi.fn();
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { name: "OpenAI", url: "https://x", isHubService: false },
                    { name: "Anthropic", url: "https://y", isHubService: false },
                ]}
                currentModel="gpt-4o"
                modelOptions={["gpt-4o", "gpt-4.1", "o3"]}
                onSwitchModel={onSwitchModel}
            />,
        );
        openProviderDropdown();
        expect(screen.getByText("模型")).toBeTruthy();
        expect(screen.getByRole("option", { name: /gpt-4o/ }).textContent).toContain("\u2713");
        fireEvent.click(screen.getByRole("option", { name: "gpt-4.1" }));
        expect(onSwitchModel).toHaveBeenCalledWith("gpt-4.1");
        expect(screen.queryByRole("listbox")).toBeNull();
    });

    it("shows the configured model when options only contain the settings fallback", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[{ name: "OpenAI", url: "https://x", isHubService: false }]}
                currentModel="settings-only-model"
                modelOptions={["settings-only-model"]}
                onSwitchModel={vi.fn()}
            />,
        );
        openProviderDropdown();
        expect(screen.getByRole("option", { name: "settings-only-model" })).toBeTruthy();
    });

    it("calls onOpenModelMenu when the dropdown opens", () => {
        const onOpenModelMenu = vi.fn();
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { name: "OpenAI", url: "https://x", isHubService: false },
                    { name: "Anthropic", url: "https://y", isHubService: false },
                ]}
                onOpenModelMenu={onOpenModelMenu}
                onSwitchModel={vi.fn()}
                currentModel="m1"
                modelOptions={["m1"]}
            />,
        );
        openProviderDropdown();
        expect(onOpenModelMenu).toHaveBeenCalled();
    });

    it("omits MoA section and leading separators when all presets are disabled", () => {
        render(
            <SidebarSystemStatus
                {...baseProps}
                availableProviders={[
                    { name: "OpenAI", url: "https://x", isHubService: false },
                    { name: "Anthropic", url: "https://y", isHubService: false },
                ]}
                moaSticky={{
                    available: true,
                    active: false,
                    presets: [
                        { id: "a", display_name: "A", enabled: false, ref_count: 1 },
                        { id: "b", display_name: "B", enabled: false, ref_count: 1 },
                    ],
                }}
                onToggleMoASticky={vi.fn()}
            />,
        );
        openProviderDropdown();
        const listbox = screen.getByRole("listbox");
        expect(screen.queryByText(/会诊/)).toBeNull();
        // Provider rows + settings only → one separator (before settings), not an empty MoA block.
        expect(listbox.querySelectorAll(".sidebar-system-status__provider-dropdown-sep").length).toBe(1);
    });
});
