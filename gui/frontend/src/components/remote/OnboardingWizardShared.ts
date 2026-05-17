import type { CSSProperties } from "react";

export interface HubLLMServiceStatus {
    active?: boolean;
    skip_llm_config?: boolean;
    active_grants?: HubLLMActiveGrant[];
    credit_grants?: HubLLMActiveGrant[];
    inactive_reasons?: string[];
}

export interface HubLLMActiveGrant {
    service_group_id?: string;
    source?: string;
    starts_at?: string;
    expires_at?: string;
    active?: boolean;
    effective?: boolean;
    status?: string;
    status_reason?: string;
    credits_total?: number;
    credits_used?: number;
    credits_remaining?: number;
    credits_available?: number;
    retry_after_seconds?: number;
    retry_after_at?: string;
}

export interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string;
    context_length?: number;
    is_custom?: boolean;
    auth_type?: string;
    agent_type?: string;
    supports_vision?: boolean;
    wire_api?: string;
}

export type Props = {
    lang: string;
    hubUrl: string;
    email: string;
    brandId?: string;
    brandDisplayName?: string;
    onClose: () => void;
    onLLMConfigured: () => void;
    onRegistered: () => void;
    onSaveField: (patch: Record<string, any>) => void;
};

export const inputStyle: CSSProperties = {
    width: "100%",
    padding: "7px 10px",
    fontSize: "0.8rem",
    border: `1px solid var(--theme-border)`,
    borderRadius: 4,
    background: "var(--theme-surface)",
    color: "var(--theme-text-primary)",
    boxSizing: "border-box",
};

export const readonlyInputStyle: CSSProperties = {
    ...inputStyle,
    background: "var(--theme-surface-muted)",
    color: "var(--theme-text-muted)",
    cursor: "default",
};

export const labelStyle: CSSProperties = {
    fontSize: "0.76rem",
    color: "var(--theme-text-muted)",
    marginBottom: 4,
    display: "block",
};

export const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);
