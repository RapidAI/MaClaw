import type { CSSProperties } from "react";
import { colors } from "./styles";
import type { HubMCPSecretRequirement } from "./MCPSecretRequirementsNotice";

export type MCPSecretInputState = { storage: "hub" | "local"; value: string; configured?: boolean };

type Props = {
    requirements: HubMCPSecretRequirement[];
    inputs: Record<string, MCPSecretInputState>;
    onChange: (next: Record<string, MCPSecretInputState>) => void;
    translate: (key: string) => string;
};

export function MCPSecretConfigurationEditor({ requirements, inputs, onChange, translate }: Props) {
    if (requirements.length === 0) return null;
    const updateInput = (name: string, input: MCPSecretInputState) => {
        onChange({ ...inputs, [name]: input });
    };
    return (
        <div style={containerStyle}>
            {requirements.map((req) => {
                const policy = req.storage_policy || "hub_or_local";
                const rawInput = inputs[req.name] || { storage: "hub" as const, value: "" };
                const normalizedStorage = policy === "local" ? "local" : policy === "hub" ? "hub" : rawInput.storage;
                const input = normalizedStorage === rawInput.storage ? rawInput : { ...rawInput, storage: normalizedStorage, configured: false };
                const canUseHub = policy !== "local";
                const canUseLocal = policy !== "hub";
                return (
                    <div key={req.name} style={rowStyle}>
                        <div style={{ flex: "1 1 140px", minWidth: 0, textAlign: "left" }}>
                            <div style={{ fontSize: "0.72rem", fontWeight: 600, color: colors.text }}>{req.label || req.name}{req.required ? " *" : ""}</div>
                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, lineHeight: 1.4, textAlign: "left" }}>{input.configured && !input.value ? translate(input.storage === "local" ? "mcpSecretConfiguredLocal" : "mcpSecretConfiguredHub") : policy}</div>
                        </div>
                        <select
                            className="form-input"
                            style={{ width: "92px", fontSize: "0.7rem", flexShrink: 0 }}
                            value={input.storage}
                            onChange={(e) => {
                                const storage = e.target.value as "hub" | "local";
                                updateInput(req.name, { ...input, storage, configured: input.configured && storage !== input.storage ? false : input.configured });
                            }}
                        >
                            {canUseHub && <option value="hub">Hub</option>}
                            {canUseLocal && <option value="local">Local</option>}
                        </select>
                        <input
                            className="form-input"
                            type="password"
                            style={{ flex: "1 1 160px", minWidth: 0, fontSize: "0.72rem" }}
                            value={input.value}
                            onChange={(e) => updateInput(req.name, { ...input, value: e.target.value })}
                            placeholder={input.storage === "hub" ? translate("mcpSecretSaveHub") : translate("mcpSecretSaveLocal")}
                            spellCheck={false}
                        />
                    </div>
                );
            })}
        </div>
    );
}

const containerStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    padding: "8px",
    background: colors.surface,
    display: "flex",
    flexDirection: "column",
    gap: "6px",
    textAlign: "left",
};

const rowStyle: CSSProperties = {
    display: "flex",
    alignItems: "flex-start",
    flexWrap: "wrap",
    gap: "6px",
    textAlign: "left",
};
