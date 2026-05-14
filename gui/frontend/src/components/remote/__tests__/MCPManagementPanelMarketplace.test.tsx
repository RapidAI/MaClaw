import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MCPManagementPanel } from "../MCPManagementPanel";
import {
    CheckMCPServerHealth,
    GetHubCapability,
    GetHubMCPHubSecrets,
    GetHubMCPSecretBindings,
    GetHubMCPSecretRequirements,
    GetHubRecommendedCapabilities,
    GetLocalMCPServerStatuses,
    GetMCPServerTools,
    InstallHubCapability,
    ListHubCapabilities,
    ListLocalMCPServers,
    ListMCPServers,
    RegisterLocalMCPServer,
    RegisterMCPServer,
    SaveHubMCPHubSecret,
    SaveHubMCPSecretBinding,
    SetLocalMCPAutoStart,
    SyncHubManagedCapabilities,
    SyncLocalMCPServers,
    UnregisterLocalMCPServer,
    UnregisterMCPServer,
    UpdateLocalMCPServer,
    UpdateMCPServer,
} from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    CheckMCPServerHealth: vi.fn(),
    GetHubCapability: vi.fn(),
    GetHubMCPHubSecrets: vi.fn(),
    GetHubMCPSecretBindings: vi.fn(),
    GetHubMCPSecretRequirements: vi.fn(),
    GetHubRecommendedCapabilities: vi.fn(),
    GetLocalMCPServerStatuses: vi.fn(),
    GetMCPServerTools: vi.fn(),
    InstallHubCapability: vi.fn(),
    ListHubCapabilities: vi.fn(),
    ListLocalMCPServers: vi.fn(),
    ListMCPServers: vi.fn(),
    ProbeMCPServers: vi.fn(),
    RegisterLocalMCPServer: vi.fn(),
    RegisterMCPServer: vi.fn(),
    SaveHubMCPHubSecret: vi.fn(),
    SaveHubMCPSecretBinding: vi.fn(),
    SetLocalMCPAutoStart: vi.fn(),
    SyncHubManagedCapabilities: vi.fn(),
    SyncLocalMCPServers: vi.fn(),
    UnregisterLocalMCPServer: vi.fn(),
    UnregisterMCPServer: vi.fn(),
    UpdateLocalMCPServer: vi.fn(),
    UpdateMCPServer: vi.fn(),
}));

const labels: Record<string, string> = {
    cancel: "Cancel",
    mcpAdd: "Add",
    mcpAddHeader: "Add header",
    mcpAuthApiKey: "API key",
    mcpAuthBearer: "Bearer",
    mcpAuthNone: "None",
    mcpAuthType: "Auth type",
    mcpCheckNow: "Check now",
    mcpChecking: "Checking",
    mcpColActions: "Actions",
    mcpColHealth: "Health",
    mcpColName: "Name",
    mcpColTools: "Tools",
    mcpCollapse: "Collapse",
    mcpConfirmDelete: "Delete MCP",
    mcpConfirmDeleteRemote: "Delete {name}?",
    mcpCustomHeaders: "Custom headers",
    mcpDelete: "Delete",
    mcpDeleting: "Deleting",
    mcpEdit: "Edit",
    mcpEditServer: "Edit MCP server",
    mcpEndpointLabel: "Endpoint",
    mcpEndpointRequired: "Endpoint is required",
    mcpEnterApiKey: "Enter API key",
    mcpEnterBearer: "Enter bearer token",
    mcpFailCount: "Failures",
    mcpHealthRecord: "Health record",
    mcpHealthStatus: "Status",
    mcpHealthy: "Healthy",
    mcpImportJson: "Import JSON",
    mcpInstallRecommended: "Install",
    mcpLastCheck: "Last check",
    mcpLoading: "Loading",
    mcpLoadingTools: "Loading tools",
    mcpMarketplace: "Capability Marketplace",
    mcpMarketplaceDone: "Marketplace sync complete",
    mcpMarketplaceInstalled: "installed",
    mcpMarketplaceInstalledState: "Installed",
    mcpMarketplaceNeedsAttention: "Marketplace sync needs attention",
    mcpMarketplaceNeedsConfig: "need config",
    mcpMarketplaceNoResults: "No MCP capabilities found",
    mcpMarketplaceSearch: "Search",
    mcpMarketplaceSearchPlaceholder: "Search enterprise MCP marketplace",
    mcpMarketplaceSearching: "Searching...",
    mcpMarketplaceSecrets: "MCP secrets",
    mcpMarketplaceSync: "Sync required",
    mcpMarketplaceSyncing: "Syncing...",
    mcpMarketplaceUpdated: "updated",
    mcpNameLabel: "Name",
    mcpNameRequired: "Name is required",
    mcpNoCustomHeaders: "No custom headers",
    mcpNoDescription: "No description",
    mcpNoRemoteServers: "No remote MCP servers",
    mcpNoTools: "No tools",
    mcpNotChecked: "Not checked",
    mcpRegister: "Register",
    mcpRegisterServer: "Register server",
    mcpRegisterServerTitle: "Register MCP server",
    mcpRemoteImportJsonDesc: "Paste MCP JSON",
    mcpRemoteImportJsonTitle: "Import remote MCP JSON",
    mcpSave: "Save",
    mcpSecretConfigured: "Secret ready",
    mcpSecretConfiguredHub: "Configured in Hub",
    mcpSecretConfiguredLocal: "Configured locally",
    mcpSecretSaveHub: "Save in Hub",
    mcpSecretSaveLocal: "Save locally",
    mcpSecretRequired: "Required MCP secret is missing",
    mcpSecretNeedsConfig: "Needs secret",
    mcpConfigureSecret: "Configure",
    mcpServersRegistered: "registered",
    mcpSlow: "Slow",
    mcpSubmitting: "Submitting",
    mcpTabLocal: "Local",
    mcpTabRemote: "Remote",
    mcpToolList: "Tool list",
    mcpTools: "Tools",
    mcpUnavailable: "Unavailable",
};

const t = (key: string) => labels[key] || key;

const jiraServer = {
    id: "jira-server",
    name: "Jira MCP",
    endpoint_url: "https://mcp.example.com/jira",
    auth_type: "none",
    auth_secret: "",
    headers: {},
    capability: { capability_id: "jira-mcp", version_key: "1.0.0", source: "hub", global_key: "hub:jira-mcp" },
    tools: [],
    health_status: "healthy",
    fail_count: 0,
    last_check_at: "",
    created_at: "",
};

describe("MCPManagementPanel marketplace integration", () => {
    let servers: any[];

    beforeEach(() => {
        vi.clearAllMocks();
        servers = [];
        vi.mocked(ListMCPServers).mockImplementation(async () => servers);
        vi.mocked(ListLocalMCPServers).mockResolvedValue([]);
        vi.mocked(GetLocalMCPServerStatuses).mockResolvedValue([]);
        vi.mocked(GetHubRecommendedCapabilities).mockResolvedValue([]);
        vi.mocked(GetHubCapability).mockResolvedValue({});
        vi.mocked(ListHubCapabilities).mockResolvedValue([
            { id: "jira-mcp", capability_type: "mcp", display_name: "Jira MCP", source: "hub", current_version_key: "1.0.0" },
        ]);
        vi.mocked(InstallHubCapability).mockImplementation(async () => {
            servers = [jiraServer];
            return { managed_installed: 1, updated: 0, needs_user_config: ["jira-mcp"] };
        });
        vi.mocked(SyncHubManagedCapabilities).mockResolvedValue({ managed_installed: 0, updated: 0, needs_user_config: [] });
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_token", label: "API token", required: true, storage_policy: "local" },
        ]);
        vi.mocked(GetHubMCPHubSecrets).mockResolvedValue([]);
        vi.mocked(GetHubMCPSecretBindings).mockResolvedValue([]);
        vi.mocked(UpdateMCPServer).mockResolvedValue(undefined);
        vi.mocked(SaveHubMCPHubSecret).mockResolvedValue(undefined);
        vi.mocked(SaveHubMCPSecretBinding).mockResolvedValue(undefined);
        vi.mocked(RegisterMCPServer).mockResolvedValue(undefined);
        vi.mocked(UnregisterMCPServer).mockResolvedValue(undefined);
        vi.mocked(GetMCPServerTools).mockResolvedValue([]);
        vi.mocked(CheckMCPServerHealth).mockResolvedValue(undefined);
        vi.mocked(RegisterLocalMCPServer).mockResolvedValue(undefined);
        vi.mocked(UpdateLocalMCPServer).mockResolvedValue(undefined);
        vi.mocked(UnregisterLocalMCPServer).mockResolvedValue(undefined);
        vi.mocked(SyncLocalMCPServers).mockResolvedValue(undefined);
        vi.mocked(SetLocalMCPAutoStart).mockResolvedValue(undefined);
    });

    it("opens secret configuration after installing a marketplace MCP and saves a local token", async () => {
        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Install"));

        expect(await screen.findByText("MCP secrets")).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText("Save locally"), { target: { value: "jira-token" } });
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({
                id: "jira-server",
                auth_type: "bearer",
                auth_secret: "jira-token",
            }));
            expect(SaveHubMCPSecretBinding).toHaveBeenCalledWith(expect.objectContaining({
                mcp_server_id: "jira-server",
                requirement_name: "api_token",
                storage: "local",
                status: "configured",
            }));
        });
        expect(SaveHubMCPHubSecret).not.toHaveBeenCalled();
    });
    it("saves a Hub-hosted secret for marketplace MCPs", async () => {
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_key", label: "API key", required: true, storage_policy: "hub" },
        ]);

        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Install"));

        expect(await screen.findByText("MCP secrets")).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText("Save in Hub"), { target: { value: "hub-secret" } });
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({ id: "jira-server" }));
            expect(SaveHubMCPHubSecret).toHaveBeenCalledWith(expect.objectContaining({
                mcp_server_id: "jira-server",
                requirement_name: "api_key",
                secret_value: "hub-secret",
            }));
        });
        expect(SaveHubMCPSecretBinding).not.toHaveBeenCalled();
    });

    it("blocks saving when a required marketplace secret is missing", async () => {
        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Install"));
        expect(await screen.findByText("MCP secrets")).toBeTruthy();
        fireEvent.click(screen.getByText("Save"));

        expect(await screen.findByText("Required MCP secret is missing: API token")).toBeTruthy();
        expect(UpdateMCPServer).not.toHaveBeenCalled();
        expect(SaveHubMCPSecretBinding).not.toHaveBeenCalled();
        expect(SaveHubMCPHubSecret).not.toHaveBeenCalled();
    });
    it("treats the native local auth secret field as satisfying a required marketplace secret", async () => {
        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Install"));
        expect(await screen.findByText("MCP secrets")).toBeTruthy();

        fireEvent.change(screen.getAllByRole("combobox")[0], { target: { value: "bearer" } });
        fireEvent.change(screen.getByPlaceholderText("Enter bearer token"), { target: { value: "native-token" } });
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({
                id: "jira-server",
                auth_type: "bearer",
                auth_secret: "native-token",
            }));
            expect(SaveHubMCPSecretBinding).toHaveBeenCalledWith(expect.objectContaining({
                mcp_server_id: "jira-server",
                requirement_name: "api_token",
                storage: "local",
                status: "configured",
            }));
        });
    });
    it("highlights installed marketplace MCPs that still need secrets", async () => {
        servers = [jiraServer];

        render(<MCPManagementPanel translate={t} />);

        expect(await screen.findByText("Needs secret")).toBeTruthy();
        expect(screen.getByText("Configure")).toBeTruthy();
    });

    it("marks installed marketplace MCP secrets as ready when local binding is configured", async () => {
        servers = [{ ...jiraServer, auth_secret: "existing-token" }];
        vi.mocked(GetHubMCPSecretBindings).mockResolvedValue([
            { requirement_name: "api_token", storage: "local", local_secret_ref: "mcp:jira-server:api_token", status: "configured" },
        ]);

        render(<MCPManagementPanel translate={t} />);

        expect(await screen.findByText("Secret ready")).toBeTruthy();
        expect(screen.queryByText("Needs secret")).toBeNull();
    });
    it("allows saving a marketplace MCP that already has a Hub-hosted secret", async () => {
        servers = [jiraServer];
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_key", label: "API key", required: true, storage_policy: "hub" },
        ]);
        vi.mocked(GetHubMCPHubSecrets).mockResolvedValue([
            { requirement_name: "api_key", metadata: { capability_id: "jira-mcp" } },
        ]);

        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Edit"));
        expect(await screen.findByText("Configured in Hub")).toBeTruthy();
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({ id: "jira-server" }));
        });
        expect(SaveHubMCPHubSecret).not.toHaveBeenCalled();
        expect(SaveHubMCPSecretBinding).not.toHaveBeenCalled();
    });
    it("does not create a needs_config binding for untouched optional local secrets", async () => {
        servers = [jiraServer];
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "optional_token", label: "Optional token", required: false, storage_policy: "local" },
        ]);

        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Edit"));
        expect(await screen.findByText("MCP secrets")).toBeTruthy();
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({ id: "jira-server" }));
        });
        expect(SaveHubMCPSecretBinding).not.toHaveBeenCalled();
        expect(SaveHubMCPHubSecret).not.toHaveBeenCalled();
    });
    it("saves an optional local marketplace secret when the user provides one", async () => {
        servers = [jiraServer];
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_key", label: "API key", required: false, storage_policy: "local" },
        ]);

        render(<MCPManagementPanel translate={t} />);

        fireEvent.click(await screen.findByText("Edit"));
        expect(await screen.findByText("MCP secrets")).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText("Save locally"), { target: { value: "optional-key" } });
        fireEvent.click(screen.getByText("Save"));

        await waitFor(() => {
            expect(UpdateMCPServer).toHaveBeenCalledWith(expect.objectContaining({
                id: "jira-server",
                auth_type: "api_key",
                auth_secret: "optional-key",
            }));
            expect(SaveHubMCPSecretBinding).toHaveBeenCalledWith(expect.objectContaining({
                mcp_server_id: "jira-server",
                requirement_name: "api_key",
                storage: "local",
                status: "configured",
            }));
        });
    });
    it("does not treat a stale local binding as configured when policy is Hub-only", async () => {
        servers = [{ ...jiraServer, auth_secret: "old-local-token" }];
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_key", label: "API key", required: true, storage_policy: "hub" },
        ]);
        vi.mocked(GetHubMCPSecretBindings).mockResolvedValue([
            { requirement_name: "api_key", storage: "local", local_secret_ref: "mcp:jira-server:api_key", status: "configured" },
        ]);

        render(<MCPManagementPanel translate={t} />);

        expect(await screen.findByText("Needs secret")).toBeTruthy();
    });

    it("does not treat a stale Hub secret as configured when policy is local-only", async () => {
        servers = [jiraServer];
        vi.mocked(GetHubMCPSecretRequirements).mockResolvedValue([
            { name: "api_token", label: "API token", required: true, storage_policy: "local" },
        ]);
        vi.mocked(GetHubMCPHubSecrets).mockResolvedValue([
            { requirement_name: "api_token", metadata: { capability_id: "jira-mcp" } },
        ]);

        render(<MCPManagementPanel translate={t} />);

        expect(await screen.findByText("Needs secret")).toBeTruthy();
    });
});