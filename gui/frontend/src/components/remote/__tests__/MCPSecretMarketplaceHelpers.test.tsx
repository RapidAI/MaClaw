import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MCPSecretConfigurationEditor } from "../MCPSecretConfigurationEditor";
import { MCPSecretRequirementsNotice } from "../MCPSecretRequirementsNotice";
import { parseRelaxedJson } from "../MCPJsonImportParser";

const labels: Record<string, string> = {
    mcpMarketplaceSecrets: "MCP secrets",
    mcpSecretConfiguredHub: "Configured in Hub",
    mcpSecretConfiguredLocal: "Configured locally",
    mcpSecretSaveHub: "Save in Hub",
    mcpSecretSaveLocal: "Save locally",
};

const t = (key: string) => labels[key] || key;

describe("MCP secret marketplace helpers", () => {
    it("shows secret requirements with storage policy", () => {
        render(
            <MCPSecretRequirementsNotice
                translate={t}
                requirements={[{ name: "api_key", label: "API key", required: true, storage_policy: "hub_or_local" }]}
            />,
        );

        expect(screen.getByText("MCP secrets")).toBeTruthy();
        expect(screen.getByText("API key *")).toBeTruthy();
        expect(screen.getByText("hub_or_local")).toBeTruthy();
    });

    it("edits Hub/local secret storage inputs", () => {
        const onChange = vi.fn();
        render(
            <MCPSecretConfigurationEditor
                translate={t}
                requirements={[{ name: "token", label: "Access token", required: true, storage_policy: "hub_or_local" }]}
                inputs={{ token: { storage: "hub", value: "" } }}
                onChange={onChange}
            />,
        );

        fireEvent.change(screen.getByRole("combobox"), { target: { value: "local" } });
        expect(onChange).toHaveBeenCalledWith({ token: { storage: "local", value: "" } });

        fireEvent.change(screen.getByPlaceholderText("Save in Hub"), { target: { value: "secret-value" } });
        expect(onChange).toHaveBeenLastCalledWith({ token: { storage: "hub", value: "secret-value" } });
    });

    it("clears configured state when switching secret storage", () => {
        const onChange = vi.fn();
        render(
            <MCPSecretConfigurationEditor
                translate={t}
                requirements={[{ name: "token", label: "Access token", required: true, storage_policy: "hub_or_local" }]}
                inputs={{ token: { storage: "hub", value: "", configured: true } }}
                onChange={onChange}
            />,
        );

        expect(screen.getByText("Configured in Hub")).toBeTruthy();
        fireEvent.change(screen.getByRole("combobox"), { target: { value: "local" } });

        expect(onChange).toHaveBeenCalledWith({ token: { storage: "local", value: "", configured: false } });
    });
    it("normalizes stale local input when requirement only allows Hub storage", () => {
        render(
            <MCPSecretConfigurationEditor
                translate={t}
                requirements={[{ name: "token", label: "Access token", required: true, storage_policy: "hub" }]}
                inputs={{ token: { storage: "local", value: "", configured: true } }}
                onChange={vi.fn()}
            />,
        );

        expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("hub");
        expect(screen.queryByText("Configured locally")).toBeNull();
        expect(screen.getByPlaceholderText("Save in Hub")).toBeTruthy();
    });

    it("normalizes stale Hub input when requirement only allows local storage", () => {
        render(
            <MCPSecretConfigurationEditor
                translate={t}
                requirements={[{ name: "token", label: "Access token", required: true, storage_policy: "local" }]}
                inputs={{ token: { storage: "hub", value: "", configured: true } }}
                onChange={vi.fn()}
            />,
        );

        expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("local");
        expect(screen.queryByText("Configured in Hub")).toBeNull();
        expect(screen.getByPlaceholderText("Save locally")).toBeTruthy();
    });
    it("parses pasted MCP JSON with relaxed punctuation", () => {
        const parsed = parseRelaxedJson("{ name: 'jira', endpoint_url: \uff02https://mcp.example.com\uff02\uff0c headers: { Authorization: 'Bearer x' } }");

        expect(parsed).toEqual({
            name: "jira",
            endpoint_url: "https://mcp.example.com",
            headers: { Authorization: "Bearer x" },
        });
    });
});