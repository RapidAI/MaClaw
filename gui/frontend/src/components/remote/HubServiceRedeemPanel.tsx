import { useCallback, useEffect, useMemo, useState } from "react";
import { GetHubLLMServiceStatus, RedeemHubLLMService } from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";

interface HubLLMAuthorizedModel {
    name: string;
    provider_ids?: string[];
    service_group_ids?: string[];
}

interface HubLLMActiveGrant {
    service_group_id: string;
    source: string;
    expires_at: string;
}

interface HubLLMServiceStatus {
    active: boolean;
    skip_llm_config: boolean;
    auth_mode?: string;
    service_group_ids?: string[];
    service_group_names?: string[];
    available_models?: string[];
    authorized_models?: HubLLMAuthorizedModel[];
    active_grants?: HubLLMActiveGrant[];
    nearest_expires_at?: string;
    default_model?: string;
    hub_llm_base_url?: string;
}

interface Props {
    lang?: string;
    onStatusChange?: (status: HubLLMServiceStatus) => void;
}

const panelStyle: React.CSSProperties = {
    display: "grid",
    gap: 12,
};

const cardStyle: React.CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    background: colors.surface,
    padding: "14px 16px",
};

const mutedCardStyle: React.CSSProperties = {
    ...cardStyle,
    background: colors.surfaceMuted,
};

const sectionTitleStyle: React.CSSProperties = {
    fontSize: "0.9rem",
    fontWeight: 700,
    color: colors.text,
    margin: 0,
};

const labelStyle: React.CSSProperties = {
    fontSize: "0.72rem",
    color: colors.textMuted,
    marginBottom: 6,
    display: "block",
    fontWeight: 600,
    letterSpacing: "0.02em",
};

const valueStyle: React.CSSProperties = {
    fontSize: "0.8rem",
    color: colors.text,
    lineHeight: 1.6,
    wordBreak: "break-word",
};

const inputStyle: React.CSSProperties = {
    width: "100%",
    padding: "10px 12px",
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.surface,
    color: colors.text,
    boxSizing: "border-box",
    fontSize: "0.84rem",
};

const primaryButtonStyle: React.CSSProperties = {
    border: "none",
    borderRadius: radius.md,
    padding: "9px 16px",
    cursor: "pointer",
    background: colors.primary,
    color: colors.onPrimary,
    fontWeight: 600,
    fontSize: "0.8rem",
};

const secondaryButtonStyle: React.CSSProperties = {
    ...primaryButtonStyle,
    background: colors.surfaceMuted,
    color: colors.text,
    border: `1px solid ${colors.border}`,
};

const chipStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    padding: "3px 10px",
    borderRadius: radius.pill,
    fontSize: "0.72rem",
    fontWeight: 600,
    border: `1px solid ${colors.border}`,
    background: colors.surfaceMuted,
    color: colors.textSecondary,
};

function formatTime(value?: string, lang?: string): string {
    if (!value) return "-";
    const dt = new Date(value);
    if (Number.isNaN(dt.getTime())) return value;
    const locale = lang === "zh-Hant" ? "zh-Hant" : lang === "zh-Hans" ? "zh-Hans" : "en-US";
    return new Intl.DateTimeFormat(locale, {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
    }).format(dt);
}

export function HubServiceRedeemPanel({ lang, onStatusChange }: Props) {
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [status, setStatus] = useState<HubLLMServiceStatus | null>(null);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [redeemCode, setRedeemCode] = useState("");
    const [redeeming, setRedeeming] = useState(false);
    const [redeemResult, setRedeemResult] = useState<{ ok: boolean; msg: string } | null>(null);

    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) => (
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
    ), [lang]);

    const loadStatus = useCallback(async (silent?: boolean) => {
        if (silent) setRefreshing(true);
        else setLoading(true);
        setLoadError(null);
        try {
            const next = await GetHubLLMServiceStatus() as HubLLMServiceStatus;
            setStatus(next);
            onStatusChange?.(next);
        } catch (error) {
            setLoadError(String(error));
        } finally {
            setLoading(false);
            setRefreshing(false);
        }
    }, [onStatusChange]);

    useEffect(() => {
        loadStatus();
    }, [loadStatus]);

    const activeGroupNames = useMemo(() => {
        if (!status?.service_group_names?.length) return [] as string[];
        return status.service_group_names.filter(Boolean);
    }, [status]);

    const availableModels = useMemo(() => {
        return (status?.available_models || []).filter(Boolean);
    }, [status]);

    const handleRedeem = useCallback(async () => {
        const code = redeemCode.trim();
        if (!code) {
            setRedeemResult({ ok: false, msg: t("Please enter a redeem code", "请输入兑换码") });
            return;
        }
        setRedeeming(true);
        setRedeemResult(null);
        try {
            const next = await RedeemHubLLMService(code) as HubLLMServiceStatus;
            setStatus(next);
            setRedeemCode("");
            onStatusChange?.(next);
            setRedeemResult({ ok: true, msg: t("Redeem successful", "兑换成功") });
        } catch (error) {
            setRedeemResult({ ok: false, msg: String(error) });
        } finally {
            setRedeeming(false);
        }
    }, [redeemCode, onStatusChange, t]);

    if (loading) {
        return <div style={{ padding: 16, color: colors.textMuted }}>{t("Loading service status...", "正在加载服务状态...")}</div>;
    }

    return (
        <div style={panelStyle}>
            <div style={cardStyle}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, marginBottom: 12 }}>
                    <div>
                        <h3 style={sectionTitleStyle}>{t("Service Exchange", "服务兑换")}</h3>
                        <div style={{ ...valueStyle, color: colors.textSecondary, marginTop: 4 }}>
                            {t(
                                "Redeem a service card to activate MaClaw model service permissions on this account.",
                                "兑换服务卡后，可为当前账号开通 MaClaw 模型服务权限。"
                            )}
                        </div>
                    </div>
                    <button type="button" onClick={() => loadStatus(true)} disabled={refreshing} style={secondaryButtonStyle}>
                        {refreshing ? t("Refreshing...", "刷新中...") : t("Refresh", "刷新")}
                    </button>
                </div>

                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 12 }}>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Service Status", "服务状态")}</div>
                        <div style={{ ...chipStyle, background: status?.active ? colors.successBg : colors.warningBg, color: status?.active ? colors.success : colors.warning, borderColor: status?.active ? colors.success : colors.warning }}>
                            {status?.active ? t("Active", "已开通") : t("Not Active", "未开通")}
                        </div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Authorized Groups", "已授权服务组")}</div>
                        <div style={valueStyle}>{activeGroupNames.length ? activeGroupNames.join(", ") : "-"}</div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Nearest Expiry", "最近到期时间")}</div>
                        <div style={valueStyle}>{formatTime(status?.nearest_expires_at, lang)}</div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Default Model", "默认模型")}</div>
                        <div style={valueStyle}>{status?.default_model || "auto"}</div>
                    </div>
                </div>

                {loadError && (
                    <div style={{ marginTop: 12, padding: "8px 10px", borderRadius: radius.md, background: colors.dangerBg, color: colors.danger, border: `1px solid ${colors.danger}` }}>
                        {loadError}
                    </div>
                )}
            </div>

            <div style={cardStyle}>
                <div style={{ marginBottom: 12 }}>
                    <label style={labelStyle}>{t("Redeem Code", "兑换码")}</label>
                    <input
                        value={redeemCode}
                        onChange={(e) => setRedeemCode(e.target.value)}
                        placeholder={t("Enter service card code", "请输入服务卡兑换码")}
                        style={inputStyle}
                    />
                </div>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
                    <div style={{ ...valueStyle, color: colors.textSecondary, flex: 1, minWidth: 220 }}>
                        {t(
                            "After successful redeem, Hub-managed MaClaw model service will be enabled automatically if your account becomes authorized.",
                            "兑换成功后，如果当前账号获得授权，Hub 托管的 MaClaw 模型服务会自动启用。"
                        )}
                    </div>
                    <button type="button" onClick={handleRedeem} disabled={redeeming} style={primaryButtonStyle}>
                        {redeeming ? t("Redeeming...", "兑换中...") : t("Redeem Now", "立即兑换")}
                    </button>
                </div>
                {redeemResult && (
                    <div style={{ marginTop: 12, padding: "8px 10px", borderRadius: radius.md, background: redeemResult.ok ? colors.successBg : colors.dangerBg, color: redeemResult.ok ? colors.success : colors.danger, border: `1px solid ${redeemResult.ok ? colors.success : colors.danger}` }}>
                        {redeemResult.msg}
                    </div>
                )}
            </div>

            <div style={cardStyle}>
                <h3 style={{ ...sectionTitleStyle, marginBottom: 12 }}>{t("Current Authorization Details", "当前授权详情")}</h3>
                <div style={{ display: "grid", gap: 12 }}>
                    <div>
                        <div style={labelStyle}>{t("Exposed API URL", "对外 API 地址")}</div>
                        <div style={valueStyle}>{status?.hub_llm_base_url || "-"}</div>
                    </div>
                    <div>
                        <div style={labelStyle}>{t("Available Models", "可用模型列表")}</div>
                        <div style={valueStyle}>{availableModels.length ? availableModels.join(", ") : "auto"}</div>
                    </div>
                    <div>
                        <div style={labelStyle}>{t("Active Grants", "生效中的授权")}</div>
                        <div style={{ display: "grid", gap: 8 }}>
                            {(status?.active_grants || []).length ? (status?.active_grants || []).map((grant, index) => (
                                <div key={`${grant.service_group_id}-${index}`} style={mutedCardStyle}>
                                    <div style={{ ...valueStyle, fontWeight: 600 }}>{grant.service_group_id || "-"}</div>
                                    <div style={{ ...valueStyle, color: colors.textSecondary }}>
                                        {t("Source", "来源")}: {grant.source || "-"}
                                    </div>
                                    <div style={{ ...valueStyle, color: colors.textSecondary }}>
                                        {t("Expires At", "到期时间")}: {formatTime(grant.expires_at, lang)}
                                    </div>
                                </div>
                            )) : (
                                <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No active grants", "暂无生效授权")}</div>
                            )}
                        </div>
                    </div>
                    <div>
                        <div style={labelStyle}>{t("Authorized Models", "授权模型列表")}</div>
                        <div style={{ display: "grid", gap: 8 }}>
                            {(status?.authorized_models || []).length ? (status?.authorized_models || []).map((model) => (
                                <div key={model.name} style={mutedCardStyle}>
                                    <div style={{ ...valueStyle, fontWeight: 600 }}>{model.name}</div>
                                    <div style={{ ...valueStyle, color: colors.textSecondary }}>
                                        {t("Service Groups", "服务组")}: {(model.service_group_ids || []).join(", ") || "-"}
                                    </div>
                                </div>
                            )) : (
                                <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No model permissions yet", "当前还没有模型权限")}</div>
                            )}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}

