import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MCPMarketplacePanel } from "../MCPMarketplacePanel";
import {
    GetHubCapability,
    GetHubRecommendedCapabilities,
    InstallHubCapability,
    ListHubCapabilities,
    SyncHubManagedCapabilities,
    RequestHubCapabilityInstallIntent,
    LoadConfig,
} from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetHubCapability: vi.fn(),
    GetHubRecommendedCapabilities: vi.fn(),
    InstallHubCapability: vi.fn(),
    ListHubCapabilities: vi.fn(),
    SyncHubManagedCapabilities: vi.fn(),
    RequestHubCapabilityInstallIntent: vi.fn(),
    LoadConfig: vi.fn(),
}));

const labels: Record<string, string> = {
    mcpMarketplace: "Capability Marketplace",
    mcpMarketplaceSync: "Sync required",
    mcpMarketplaceSyncing: "Syncing...",
    mcpRecommended: "Recommended",
    mcpInstallRecommended: "Install",
    mcpMarketplaceDone: "Marketplace sync complete",
    mcpMarketplaceInstalled: "installed",
    mcpMarketplaceUpdated: "updated",
    mcpMarketplaceNeedsConfig: "need config",
    mcpMarketplaceNeedsAttention: "Marketplace sync needs attention",
    mcpMarketplaceSearch: "Search",
    mcpMarketplaceSearching: "Searching...",
    mcpMarketplaceSearchPlaceholder: "Search enterprise MCP marketplace",
    mcpMarketplaceNoResults: "No MCP capabilities found",
    mcpMarketplaceInstalledState: "Installed",
    mcpMarketplacePurchaseRequested: "Purchase request submitted",
    mcpMarketplaceImportRequested: "Import request submitted",
    mcpMarketplaceRequestSubmitted: "Request submitted",
    mcpMarketplaceReadyToInstall: "Ready to install",
    mcpMarketplaceSearchHubOnly: "Hub-only search",
    mcpMarketplaceSearchMerged: "Hub + HubCenter search",
    mcpMarketplaceInstallHubOnly: "Hub-only install",
    mcpMarketplaceInstallExternalAllowed: "External install allowed",
};

const t = (key: string) => labels[key] || key;

describe("MCPMarketplacePanel", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(GetHubRecommendedCapabilities).mockResolvedValue([]);
        vi.mocked(GetHubCapability).mockResolvedValue({} as any);
        vi.mocked(ListHubCapabilities).mockResolvedValue([]);
        vi.mocked(SyncHubManagedCapabilities).mockResolvedValue({ managed_checked: 0, managed_installed: 0, updated: 0, inventory_reported: 0, recommended_count: 0, needs_user_config: [] });
        vi.mocked(InstallHubCapability).mockResolvedValue({ managed_checked: 0, managed_installed: 1, updated: 0, inventory_reported: 0, recommended_count: 0, needs_user_config: ["jira-mcp"] });
        vi.mocked(RequestHubCapabilityInstallIntent).mockResolvedValue({ action: "create_purchase_request", request_id: "req_1" } as any);
        vi.mocked(LoadConfig).mockResolvedValue({ capability_market_policy: { enterprise_only_install: true, enterprise_only_search: false } } as any);
    });


    it("shows effective enterprise marketplace policy badges", async () => {
        render(<MCPMarketplacePanel translate={t} onChanged={vi.fn()} />);

        expect(await screen.findByText("Hub + HubCenter search")).toBeTruthy();
        expect(await screen.findByText("Hub-only install")).toBeTruthy();
    });

    it("shows hub-only search when enterprise search is restricted", async () => {
        vi.mocked(LoadConfig).mockResolvedValue({ capability_market_policy: { enterprise_only_install: false, enterprise_only_search: true } } as any);

        render(<MCPMarketplacePanel translate={t} onChanged={vi.fn()} />);

        expect(await screen.findByText("Hub-only search")).toBeTruthy();
        expect(await screen.findByText("External install allowed")).toBeTruthy();
    });

    it("searches enterprise Hub MCP capabilities", async () => {
        vi.mocked(ListHubCapabilities).mockResolvedValueOnce([]).mockResolvedValueOnce([
            { id: "jira-mcp", capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hub", status: "", global_key: "", current_version_key: "1.0.0" },
        ]);

        render(<MCPMarketplacePanel translate={t} onChanged={vi.fn()} />);

        await waitFor(() => expect(screen.getByText("Search")).toBeTruthy());
        fireEvent.change(await screen.findByPlaceholderText("Search enterprise MCP marketplace"), { target: { value: "jira" } });
        fireEvent.click(screen.getByText("Search"));

        expect(await screen.findByText("Jira MCP")).toBeTruthy();
        expect(ListHubCapabilities).toHaveBeenLastCalledWith("mcp", "jira");
    });

    it("passes install status back so parent can open secret configuration", async () => {
        const onChanged = vi.fn();
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hub", status: "", global_key: "" },
        ]);

        render(<MCPMarketplacePanel translate={t} onChanged={onChanged} />);

        fireEvent.click(await screen.findByText("Install"));

        await waitFor(() => {
            expect(InstallHubCapability).toHaveBeenCalledWith("jira-mcp");
            expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({ needs_user_config: ["jira-mcp"] }));
        });
    });

    it("installs imported free external MCP after Hub creates enterprise capability", async () => {
        const onChanged = vi.fn();
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", external: true, capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hubcenter", status: "", global_key: "", metadata_json: JSON.stringify({ pricing: { mode: "free" } }) },
        ]);
        vi.mocked(RequestHubCapabilityInstallIntent).mockResolvedValue({ action: "create_import_request", request_id: "req_free", capability: { id: "ent-jira-mcp" } } as any);
        vi.mocked(InstallHubCapability).mockResolvedValue({ managed_checked: 0, managed_installed: 1, updated: 0, inventory_reported: 0, recommended_count: 0, needs_user_config: [] });

        render(<MCPMarketplacePanel translate={t} onChanged={onChanged} />);

        fireEvent.click(await screen.findByText("Install"));

        await waitFor(() => {
            expect(RequestHubCapabilityInstallIntent).toHaveBeenCalledWith(expect.objectContaining({ capability_id: "jira-mcp", pricing: "free" }));
            expect(InstallHubCapability).toHaveBeenCalledWith("ent-jira-mcp");
            expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({ managed_installed: 1 }));
        });
    });

    it("installs direct free HubCenter MCP when Hub returns imported capability", async () => {
        const onChanged = vi.fn();
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", external: true, capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hubcenter", status: "", global_key: "", metadata_json: JSON.stringify({ pricing: { mode: "free" } }) },
        ]);
        vi.mocked(RequestHubCapabilityInstallIntent).mockResolvedValue({ action: "install_external_direct", capability: { id: "ent-jira-mcp" } } as any);
        vi.mocked(InstallHubCapability).mockResolvedValue({ managed_checked: 0, managed_installed: 1, updated: 0, inventory_reported: 0, recommended_count: 0, needs_user_config: ["ent-jira-mcp"] });

        render(<MCPMarketplacePanel translate={t} onChanged={onChanged} />);

        fireEvent.click(await screen.findByText("Install"));

        await waitFor(() => {
            expect(RequestHubCapabilityInstallIntent).toHaveBeenCalledWith(expect.objectContaining({ capability_id: "jira-mcp", pricing: "free" }));
            expect(InstallHubCapability).toHaveBeenCalledWith("ent-jira-mcp");
            expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({ needs_user_config: ["ent-jira-mcp"] }));
        });
    });

    it("does not claim direct external install succeeded without an imported Hub capability", async () => {
        const onChanged = vi.fn();
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", external: true, capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hubcenter", status: "", global_key: "", metadata_json: JSON.stringify({ pricing: { mode: "free" } }) },
        ]);
        vi.mocked(RequestHubCapabilityInstallIntent).mockResolvedValue({ action: "install_external_direct", reason: "missing_import" } as any);

        render(<MCPMarketplacePanel translate={t} onChanged={onChanged} />);

        fireEvent.click(await screen.findByText("Install"));

        await waitFor(() => {
            expect(InstallHubCapability).not.toHaveBeenCalled();
            expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({ action: "install_external_direct" }));
        });
        expect(await screen.findByText("Marketplace sync needs attention: missing_import")).toBeTruthy();
    });
    it("submits Hub purchase intent for external paid HubCenter MCP results", async () => {
        const onChanged = vi.fn();
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", external: true, capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hubcenter", status: "", global_key: "", current_version_key: "1.2.0", metadata_json: JSON.stringify({ pricing: { mode: "paid", credits: 10 }, license: { seats: 5 } }) },
        ]);

        render(<MCPMarketplacePanel translate={t} onChanged={onChanged} />);

        fireEvent.click(await screen.findByText("Install"));

        await waitFor(() => {
            expect(RequestHubCapabilityInstallIntent).toHaveBeenCalledWith(expect.objectContaining({ capability_id: "jira-mcp", source: "hubcenter", pricing: "paid" }));
            expect(InstallHubCapability).not.toHaveBeenCalled();
            expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({ request_id: "req_1" }));
        });
        expect(await screen.findByText("Purchase request submitted: req_1")).toBeTruthy();
    });
    it("marks installed MCP capabilities without offering install again", async () => {
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "hub-cap-1", capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hub", status: "", global_key: "enterprise_hub:mcp:acme:jira-mcp" },
        ]);

        render(<MCPMarketplacePanel translate={t} onChanged={vi.fn()} installedCapabilities={["enterprise_hub:mcp:acme:jira-mcp"]} />);

        expect(await screen.findByText("Installed")).toBeTruthy();
        expect(screen.queryByText("Install")).toBeNull();
    });

    it("marks installed recommended MCPs by capability id", async () => {
        vi.mocked(GetHubRecommendedCapabilities).mockResolvedValue([
            { id: "rec-1", capability_ref: "hub-cap-1" },
        ] as any);
        vi.mocked(GetHubCapability).mockResolvedValue({ id: "hub-cap-1", capability_type: "mcp", capability_id: "jira-mcp", display_name: "Jira MCP", source: "hub" } as any);

        render(<MCPMarketplacePanel translate={t} onChanged={vi.fn()} installedCapabilities={["jira-mcp"]} />);

        expect(await screen.findByText("Installed")).toBeTruthy();
        expect(screen.queryByText("Install")).toBeNull();
    });
});

