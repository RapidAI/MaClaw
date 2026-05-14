import type { CSSProperties } from "react";
import { colors } from "./styles";

export interface HubMCPSecretRequirement {
    name: string;
    label?: string;
    scope?: string;
    storage_policy?: string;
    required?: boolean;
    help_url?: string;
}

type Props = {
    requirements: HubMCPSecretRequirement[];
    translate: (key: string) => string;
};

export function MCPSecretRequirementsNotice({ requirements, translate }: Props) {
    if (requirements.length === 0) return null;
    return (
        <div style={containerStyle}>
            <div style={{ fontSize: "0.72rem", fontWeight: 600, color: colors.text }}>{translate("mcpMarketplaceSecrets")}</div>
            {requirements.map((req) => (
                <div key={req.name} style={rowStyle}>
                    <span>{req.label || req.name}{req.required ? " *" : ""}</span>
                    <span>{req.storage_policy || "hub_or_local"}</span>
                </div>
            ))}
        </div>
    );
}

const containerStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    padding: "6px 8px",
    background: colors.surfaceMuted,
    display: "flex",
    flexDirection: "column",
    gap: "4px",
};

const rowStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: colors.textSecondary,
    display: "flex",
    justifyContent: "space-between",
    gap: "8px",
};