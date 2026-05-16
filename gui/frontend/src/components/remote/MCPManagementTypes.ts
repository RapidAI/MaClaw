import type { CSSProperties } from "react";
import { colors } from "./styles";

export interface MCPToolView {
    name: string;
    description: string;
    input_schema: Record<string, any>;
}

export interface MCPServerCapabilityRef {
    capability_id: string;
    version_key?: string;
    source?: string;
    global_key?: string;
}

export interface MCPServerView {
    id: string;
    name: string;
    endpoint_url: string;
    auth_type: "none" | "api_key" | "bearer";
    auth_secret: string;
    headers?: Record<string, string>;
    capability?: MCPServerCapabilityRef;
    tools: MCPToolView[];
    health_status: "healthy" | "slow" | "unavailable" | "unknown" | "checking";
    fail_count: number;
    last_check_at: string;
    created_at: string;
}

export interface LocalMCPServer {
    id: string;
    name: string;
    command: string;
    args: string[];
    env: Record<string, string>;
    disabled: boolean;
    auto_start?: boolean;
    created_at: string;
}

export type MCPManagementPanelProps = {
    translate: (key: string) => string;
};

export type MCPTab = "local" | "market" | "remote";
export type MCPSecretStatus = "configured" | "needs_config" | "optional";

export const emptyServer: MCPServerView = {
    id: "",
    name: "",
    endpoint_url: "",
    auth_type: "none",
    auth_secret: "",
    tools: [],
    health_status: "healthy",
    fail_count: 0,
    last_check_at: "",
    created_at: "",
};

export const emptyLocalServer: LocalMCPServer = {
    id: "",
    name: "",
    command: "npx",
    args: [],
    env: {},
    disabled: false,
    auto_start: false,
    created_at: "",
};

export const tabStyle: CSSProperties = {
    flex: 1,
    padding: "6px 0",
    fontSize: "0.78rem",
    fontWeight: 600,
    cursor: "pointer",
    textAlign: "center",
    color: colors.textMuted,
    background: "none",
    border: "none",
    borderBottom: "2px solid transparent",
    borderRadius: 0,
    transition: "color 0.15s, border-color 0.15s",
};

export const tabActiveStyle: CSSProperties = {
    ...tabStyle,
    color: "var(--theme-primary)",
    borderBottom: "2px solid var(--theme-primary)",
};