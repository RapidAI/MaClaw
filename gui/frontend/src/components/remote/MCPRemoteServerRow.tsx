import type { CSSProperties } from "react";
import { colors } from "./styles";

type MCPSecretStatus = "configured" | "needs_config" | "optional";

interface MCPToolView {
    name: string;
    description: string;
    input_schema: Record<string, any>;
}

interface MCPServerCapabilityRef {
    capability_id: string;
    version_key?: string;
    source?: string;
    global_key?: string;
}

interface MCPServerView {
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
    source?: "manual" | "mdns" | "project" | "marketplace";
    managed?: boolean;
}

type Props = {
    server: MCPServerView;
    busy: boolean;
    expandedServerID: string | null;
    expandedTools: MCPToolView[];
    toolsLoading: boolean;
    healthDetailID: string | null;
    onEdit: () => void;
    onDelete: () => void;
    onToggleTools: () => void;
    onHealthCheck: () => void;
    onToggleHealthDetail: () => void;
    healthColor: (s: string) => string;
    healthBg: (s: string) => string;
    healthBorder: (s: string) => string;
    healthLabel: (s: string) => string;
    secretStatus?: MCPSecretStatus;
    translate: (key: string) => string;
};

export function MCPRemoteServerRow({
    server,
    busy,
    expandedServerID,
    expandedTools,
    toolsLoading,
    healthDetailID,
    onEdit,
    onDelete,
    onToggleTools,
    onHealthCheck,
    onToggleHealthDetail,
    healthColor,
    healthBg,
    healthBorder,
    healthLabel,
    secretStatus,
    translate,
}: Props) {
    const isExpanded = expandedServerID === server.id;
    const showHealthDetail = healthDetailID === server.id;
    const toolCount = server.tools ? server.tools.length : 0;
    const toolCountDisplay = server.health_status === "checking" ? "..." : String(toolCount);
    return (
        <>
            <tr style={{ borderTop: `1px solid ${colors.border}` }}>
                <td style={tdStyle} title={server.endpoint_url}>
                    <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                        <span style={{ cursor: "default", borderBottom: `1px dashed ${colors.textMuted}`, paddingBottom: "1px" }}>{server.name}</span>
                        {secretStatus === "needs_config" && <span style={secretNeedsConfigStyle}>{translate("mcpSecretNeedsConfig")}</span>}
                        {secretStatus === "configured" && <span style={secretConfiguredStyle}>{translate("mcpSecretConfigured")}</span>}
                    </div>
                </td>
                <td style={{ ...tdStyle, textAlign: "right" }}>
                    <span
                        style={{
                            ...statusBadgeStyle,
                            background: healthBg(server.health_status),
                            color: healthColor(server.health_status),
                            border: `1px solid ${healthBorder(server.health_status)}`,
                            cursor: "pointer",
                        }}
                        onClick={onToggleHealthDetail}
                        title={translate("mcpHealthRecord")}
                    >
                        {server.health_status === "checking" ? "..." : "?"} {healthLabel(server.health_status)}
                    </span>
                </td>
                <td style={{ ...tdStyle, textAlign: "center" }}>{toolCountDisplay}</td>
                <td style={tdStyle}>
                    <div style={{ display: "flex", gap: "4px", flexWrap: "wrap", alignItems: "center" }}>
                        <button className="btn-secondary" style={smallBtnStyle} onClick={onToggleTools} disabled={busy}>
                            {isExpanded ? translate("mcpCollapse") : translate("mcpTools")}
                        </button>
                        <button className={secretStatus === "needs_config" ? "btn-primary" : "btn-secondary"} style={smallBtnStyle} onClick={onEdit} disabled={busy}>{secretStatus === "needs_config" ? translate("mcpConfigureSecret") : translate("mcpEdit")}</button>
                        {server.managed ? (
                            <span style={managedBadgeStyle} title={translate("mcpCannotDeleteManaged")}>🔒 {translate("mcpManagedLabel")}</span>
                        ) : (
                            <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={onDelete} disabled={busy}>{translate("mcpDelete")}</button>
                        )}
                    </div>
                </td>
            </tr>
            {showHealthDetail && (
                <tr>
                    <td colSpan={4} style={{ padding: "6px 8px", background: colors.surfaceMuted, borderTop: `1px solid ${colors.border}`, textAlign: "left" }}>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                            <div style={{ fontWeight: 600, marginBottom: "4px" }}>{translate("mcpHealthRecord")}</div>
                            <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                                <span>{translate("mcpHealthStatus")}: <span style={{ color: healthColor(server.health_status), fontWeight: 600 }}>{healthLabel(server.health_status)}</span></span>
                                <span>/</span>
                                <span>{translate("mcpFailCount")}: {server.fail_count}</span>
                                <span>/</span>
                                <span>{translate("mcpLastCheck")}: {server.last_check_at ? new Date(server.last_check_at).toLocaleString() : "-"}</span>
                                <button className="btn-secondary" style={{ ...smallBtnStyle, marginLeft: "8px" }} onClick={onHealthCheck} disabled={busy}>
                                    {translate("mcpCheckNow")}
                                </button>
                            </div>
                        </div>
                    </td>
                </tr>
            )}
            {isExpanded && (
                <tr>
                    <td colSpan={4} style={{ padding: "6px 8px", background: colors.surfaceMuted, borderTop: `1px solid ${colors.border}`, textAlign: "left" }}>
                        {toolsLoading ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted, padding: "4px 0", textAlign: "left" }}>{translate("mcpLoadingTools")}</div>
                        ) : expandedTools.length > 0 ? (
                            <div style={{ display: "flex", flexDirection: "column", gap: "4px", textAlign: "left" }}>
                                <div style={{ fontSize: "0.72rem", fontWeight: 600, color: colors.textSecondary, marginBottom: "2px", textAlign: "left" }}>
                                    {translate("mcpToolList")} ({expandedTools.length})
                                </div>
                                {expandedTools.map((tool) => (
                                    <div key={tool.name} style={{ background: colors.surface, border: `1px solid ${colors.border}`, borderRadius: "4px", padding: "6px 8px", textAlign: "left" }}>
                                        <div style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text, textAlign: "left" }}>{tool.name}</div>
                                        <div style={toolDescriptionStyle}>{tool.description || translate("mcpNoDescription")}</div>
                                    </div>
                                ))}
                            </div>
                        ) : (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted, padding: "4px 0", textAlign: "left" }}>{translate("mcpNoTools")}</div>
                        )}
                    </td>
                </tr>
            )}
        </>
    );
}

const tdStyle: CSSProperties = {
    padding: "6px 8px",
    fontSize: "0.76rem",
    color: colors.text,
    verticalAlign: "top",
};
const statusBadgeStyle: CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
};
const secretNeedsConfigStyle: CSSProperties = {
    ...statusBadgeStyle,
    color: "var(--theme-warning)",
    border: "1px solid var(--theme-warning)",
    background: "var(--theme-warning-bg)",
};
const secretConfiguredStyle: CSSProperties = {
    ...statusBadgeStyle,
    color: "var(--theme-success)",
    border: "1px solid var(--theme-success)",
    background: "var(--theme-success-bg)",
};
const smallBtnStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};
const managedBadgeStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: "2px",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
    color: colors.textMuted,
    border: `1px solid ${colors.border}`,
    background: colors.surfaceMuted,
    cursor: "default",
};

const toolDescriptionStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: colors.textSecondary,
    lineHeight: 1.45,
    marginTop: "2px",
    textAlign: "left",
    whiteSpace: "normal",
    overflowWrap: "anywhere",
};
