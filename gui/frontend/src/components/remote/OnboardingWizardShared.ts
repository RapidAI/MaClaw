import type { CSSProperties } from "react";
import { colors } from "./styles";
import { localizeByLang } from "../../utils/hubServiceI18n";

export interface HubLLMServiceStatus {
    active?: boolean;
    skip_llm_config?: boolean;
    active_grants?: HubLLMActiveGrant[];
    credit_grants?: HubLLMActiveGrant[];
    inactive_reasons?: string[];
}

export interface HubLLMActiveGrant {
    id?: string;
    service_group_id?: string;
    source?: string;
    card_id?: string;
    card_order_id?: string;
    starts_at?: string;
    expires_at?: string;
    permanent?: boolean;
    rolling_five_hour?: boolean;
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
    period_limits?: {
        five_hour?: number;
        daily?: number;
        weekly?: number;
        monthly?: number;
    };
    period_usage?: {
        five_hour?: { window_start?: string; window_end?: string; credits_used?: number; rolling?: boolean };
        daily?: { window_start?: string; window_end?: string; credits_used?: number };
        weekly?: { window_start?: string; window_end?: string; credits_used?: number };
        monthly?: { window_start?: string; window_end?: string; credits_used?: number };
    };
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
    vision_models?: string[];
    supports_vision?: boolean;
    wire_api?: string;
    /** Set after this provider's current configuration passes a connection test. */
    connection_test_passed?: boolean;
}

export type Props = {
    lang: string;
    hubUrl: string;
    referralHandoff?: string;
    email: string;
    brandId?: string;
    brandDisplayName?: string;
    onClose: () => void;
    onLLMConfigured: () => void;
    onRegistered: () => void | Promise<void>;
    onMigrationCompleted?: () => void | Promise<void>;
    onOnboardingCompleted?: () => void | Promise<void>;
    onSaveField: (patch: Record<string, any>) => void | Promise<unknown>;
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

export const localizeText = localizeByLang;

/* ── Shared button styles for the onboarding wizard ──
   These centralize the visual hierarchy so the same look is reused across
   every step without changing any behavior:
     • primary  – the dominant call-to-action (solid accent, white text)
     • success  – the primary CTA's completed state (green, non-interactive)
     • disabled – the primary CTA's blocked state (muted, non-interactive)
     • ghost    – neutral / dismissive actions (transparent + border)
   Disabled/done variations are applied inline at each call site so the
   existing conditional logic stays untouched.
   The shadow uses a neutral translucent tint (not a fixed accent hue) so it
   stays correct when a brand overrides --theme-primary. */

export const wizardPrimaryButtonStyle: CSSProperties = {
    width: "100%",
    padding: "10px 0",
    fontSize: "0.82rem",
    fontWeight: 600,
    background: colors.primary,
    color: colors.onPrimary,
    border: `1px solid ${colors.primary}`,
    borderRadius: 8,
    cursor: "pointer",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    boxShadow: "0 1px 3px rgba(15,23,42,0.16)",
    transition: "background 0.15s, box-shadow 0.15s",
};

export const wizardSuccessButtonStyle: CSSProperties = {
    ...wizardPrimaryButtonStyle,
    background: colors.successBg,
    color: colors.success,
    border: `1px solid ${colors.success}`,
    boxShadow: "none",
    cursor: "default",
};

export const wizardDisabledButtonStyle: CSSProperties = {
    ...wizardPrimaryButtonStyle,
    background: colors.surfaceMuted,
    color: colors.textMuted,
    border: `1px solid ${colors.border}`,
    boxShadow: "none",
    cursor: "default",
};

export const wizardGhostButtonStyle: CSSProperties = {
    padding: "8px 18px",
    fontSize: "0.8rem",
    fontWeight: 500,
    background: "transparent",
    color: colors.textSecondary,
    border: `1px solid ${colors.border}`,
    borderRadius: 8,
    cursor: "pointer",
    transition: "background 0.15s, border-color 0.15s",
};

/* Status banner container (success / warning / error) used for inline results. */
export const wizardStatusBannerStyle: CSSProperties = {
    marginTop: 10,
    padding: "8px 12px",
    borderRadius: 8,
    fontSize: "0.74rem",
    lineHeight: 1.55,
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
};

/* Tone palette for inline result banners. Centralizes the success/error/warning
   tints that were previously duplicated at every call site. These are
   intentionally fixed semantic tints (not theme-overridable), matching the
   convention already used across this component. */
export type WizardBannerTone = "success" | "error" | "warning";

const wizardBannerPalette: Record<WizardBannerTone, { bg: string; border: string; color: string }> = {
    success: { bg: "var(--theme-success-bg)", border: "color-mix(in srgb, var(--theme-success) 34%, transparent)", color: colors.success },
    error: { bg: "var(--theme-danger-bg)", border: "color-mix(in srgb, var(--theme-danger) 34%, transparent)", color: colors.danger },
    warning: { bg: "var(--theme-info-bg)", border: "color-mix(in srgb, var(--theme-primary) 28%, transparent)", color: colors.primaryDark },
};

export const wizardBannerStyle = (tone: WizardBannerTone): CSSProperties => {
    const palette = wizardBannerPalette[tone];
    return {
        ...wizardStatusBannerStyle,
        background: palette.bg,
        border: `1px solid ${palette.border}`,
        color: palette.color,
    };
};

/* Full-width, compact ghost button used for inline secondary actions
   (SSO open-in-browser / retry, OAuth cancel). Callers override `color`. */
export const wizardGhostButtonBlockStyle: CSSProperties = {
    ...wizardGhostButtonStyle,
    width: "100%",
    padding: "8px 0",
    fontSize: "0.76rem",
    marginTop: 6,
};

/* Selectable "soft" chip used for non-exclusive option toggles (model picks,
   protocol / user-agent segmented toggles). Active = soft accent fill; idle =
   plain surface. The provider selector intentionally uses a stronger solid-fill
   treatment and is not built from this helper.
   `size` controls only font/padding so the two existing footprints stay intact:
     • "sm"  – dense chips (model lists)
     • "md"  – segmented toggles (protocol / user-agent) */
export const wizardSelectableChipStyle = (active: boolean, size: "sm" | "md" = "md"): CSSProperties => ({
    fontSize: size === "sm" ? "0.7rem" : "0.76rem",
    padding: size === "sm" ? "4px 10px" : "5px 16px",
    cursor: "pointer",
    background: active ? colors.primaryLight : colors.surface,
    color: active ? colors.primaryDark : colors.text,
    border: `1px solid ${active ? colors.primary : colors.border}`,
    borderRadius: 4,
    transition: "all 0.15s",
});

/* Base layout for a full-width selectable "option card" (radio / checkbox rows
   with a title + description). Captures only the shared layout; callers supply
   `border` and `background` because their selection semantics differ (the run-mode
   radios highlight the border on selection, the free-trial checkbox does not). */
export const wizardOptionCardStyle: CSSProperties = {
    display: "flex",
    alignItems: "flex-start",
    gap: 8,
    padding: "8px 10px",
    borderRadius: 8,
    cursor: "pointer",
    fontSize: "0.76rem",
    color: colors.text,
};
