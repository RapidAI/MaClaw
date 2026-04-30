import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
    inactive_reasons?: string[];
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
    border: `1px solid ${colors.primary}`,
    borderRadius: radius.md,
    padding: "9px 16px",
    cursor: "pointer",
    background: colors.primaryLight,
    color: colors.primaryDark,
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

const detailTableStyle: React.CSSProperties = {
    width: "100%",
    borderCollapse: "collapse",
    fontSize: "0.8rem",
};

const detailThStyle: React.CSSProperties = {
    textAlign: "left",
    padding: "7px 10px",
    color: colors.textMuted,
    fontWeight: 600,
    fontSize: "0.72rem",
    whiteSpace: "nowrap",
    borderBottom: `1px solid ${colors.border}`,
    verticalAlign: "top",
    width: "30%",
};

const detailTdStyle: React.CSSProperties = {
    padding: "7px 10px",
    color: colors.text,
    borderBottom: `1px solid ${colors.border}`,
    wordBreak: "break-word",
    lineHeight: 1.6,
};

const detailTheadThStyle: React.CSSProperties = {
    textAlign: "left",
    padding: "6px 10px",
    color: colors.textMuted,
    fontWeight: 600,
    fontSize: "0.72rem",
    borderBottom: `2px solid ${colors.border}`,
    whiteSpace: "nowrap",
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

    // Stabilize onStatusChange with a ref to break the re-render cascade.
    // Without this, every App re-render (e.g. PingMaclawLLM every 60s) creates
    // a new inline arrow function → loadStatus is recreated → useEffect fires →
    // setLoading(true) destroys the DOM → input loses focus + page flickers.
    const onStatusChangeRef = useRef(onStatusChange);
    onStatusChangeRef.current = onStatusChange;

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
            onStatusChangeRef.current?.(next);
        } catch (error) {
            setLoadError(String(error));
        } finally {
            setLoading(false);
            setRefreshing(false);
        }
    }, []); // no deps — onStatusChange is read from ref

    // Load once on mount. loadStatus is stable (no deps), so this runs exactly once.
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
            setLoadError(null);
            onStatusChangeRef.current?.(next);
            setRedeemResult({ ok: true, msg: t("Redeem successful", "兑换成功") });
        } catch (error) {
            setRedeemResult({ ok: false, msg: String(error) });
        } finally {
            setRedeeming(false);
        }
    }, [redeemCode, t]);

    if (loading) {
        return <div style={{ padding: 16, color: colors.textMuted }}>{t("Loading service status...", "正在加载服务状态...")}</div>;
    }

    return (
        <div style={panelStyle}>
            {/* ── Card 1: Redeem code input — primary action, always visible first ── */}
            <div style={cardStyle}>
                <div style={{ marginBottom: 12 }}>
                    <label style={labelStyle}>{t("Redeem Code", "兑换码")}</label>
                    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                        <input
                            value={redeemCode}
                            onChange={(e) => setRedeemCode(e.target.value)}
                            onKeyDown={(e) => { if (e.key === "Enter" && !redeeming) handleRedeem(); }}
                            placeholder={t("Enter service card code", "请输入服务卡兑换码")}
                            disabled={redeeming}
                            style={{ ...inputStyle, flex: 1 }}
                        />
                        <button type="button" onClick={handleRedeem} disabled={redeeming} style={{ ...primaryButtonStyle, whiteSpace: "nowrap" }}>
                            {redeeming ? t("Redeeming...", "兑换中...") : t("Redeem Now", "立即兑换")}
                        </button>
                    </div>
                </div>
                {redeemResult && (
                    <div style={{ padding: "8px 10px", borderRadius: radius.md, background: redeemResult.ok ? colors.successBg : colors.dangerBg, color: redeemResult.ok ? colors.success : colors.danger, border: `1px solid ${redeemResult.ok ? colors.success : colors.danger}` }}>
                        {redeemResult.msg}
                    </div>
                )}
            </div>

            {/* ── Card 2: Service status overview ── */}
            <div style={cardStyle}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, marginBottom: 12 }}>
                    <h3 style={sectionTitleStyle}>{t("Service Status", "服务状态")}</h3>
                    <button type="button" onClick={() => loadStatus(true)} disabled={refreshing} style={secondaryButtonStyle}>
                        {refreshing ? t("Refreshing...", "刷新中...") : t("Refresh", "刷新")}
                    </button>
                </div>

                {/* loadError: show inline in the status card — not above the redeem input */}
                {loadError && (
                    <div style={{ marginBottom: 12, padding: "8px 10px", borderRadius: radius.md, background: colors.warningBg, color: colors.warning, border: `1px solid ${colors.warning}`, fontSize: "0.8rem" }}>
                        {loadError}
                    </div>
                )}

                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 12 }}>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Status", "状态")}</div>
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

                {/* inactive_reasons from Hub — diagnostic info for "Not Active" state */}
                {!status?.active && status?.inactive_reasons?.length ? (
                    <div style={{ marginTop: 12, padding: "8px 10px", borderRadius: radius.md, background: colors.warningBg, color: colors.warning, border: `1px solid ${colors.warning}`, fontSize: "0.78rem", lineHeight: 1.6 }}>
                        {status.inactive_reasons.map((reason, i) => (
                            <div key={i}>• {reason}</div>
                        ))}
                    </div>
                ) : null}
            </div>

            {/* ── Card 3: Authorization details — table layout ── */}
            <div style={cardStyle}>
                <h3 style={{ ...sectionTitleStyle, marginBottom: 12 }}>{t("Current Authorization Details", "当前授权详情")}</h3>
                <table style={detailTableStyle}>
                    <tbody>
                        <tr>
                            <td style={detailThStyle}>{t("Exposed API URL", "对外 API 地址")}</td>
                            <td style={detailTdStyle}>{status?.hub_llm_base_url || "-"}</td>
                        </tr>
                        <tr>
                            <td style={detailThStyle}>{t("Available Models", "可用模型列表")}</td>
                            <td style={detailTdStyle}>{availableModels.length ? availableModels.join(", ") : "auto"}</td>
                        </tr>
                        <tr>
                            <td style={detailThStyle}>{t("Nearest Expiry", "最近到期")}</td>
                            <td style={detailTdStyle}>{formatTime(status?.nearest_expires_at, lang)}</td>
                        </tr>
                    </tbody>
                </table>

                {/* Active Grants table */}
                <div style={{ marginTop: 16 }}>
                    <div style={labelStyle}>{t("Active Grants", "生效中的授权")}</div>
                    {(status?.active_grants || []).length ? (
                        <table style={detailTableStyle}>
                            <thead>
                                <tr>
                                    <th style={detailTheadThStyle}>{t("Service Group", "服务组")}</th>
                                    <th style={detailTheadThStyle}>{t("Source", "来源")}</th>
                                    <th style={detailTheadThStyle}>{t("Expires At", "到期时间")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {(status?.active_grants || []).map((grant, index) => (
                                    <tr key={`${grant.service_group_id}-${index}`}>
                                        <td style={detailTdStyle}><span style={{ fontWeight: 600 }}>{grant.service_group_id || "-"}</span></td>
                                        <td style={detailTdStyle}>{grant.source || "-"}</td>
                                        <td style={detailTdStyle}>{formatTime(grant.expires_at, lang)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    ) : (
                        <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No active grants", "暂无生效授权")}</div>
                    )}
                </div>

                {/* Authorized Models table */}
                <div style={{ marginTop: 16 }}>
                    <div style={labelStyle}>{t("Authorized Models", "授权模型列表")}</div>
                    {(status?.authorized_models || []).length ? (
                        <table style={detailTableStyle}>
                            <thead>
                                <tr>
                                    <th style={detailTheadThStyle}>{t("Model", "模型")}</th>
                                    <th style={detailTheadThStyle}>{t("Service Groups", "服务组")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {(status?.authorized_models || []).map((model) => (
                                    <tr key={model.name}>
                                        <td style={detailTdStyle}><span style={{ fontWeight: 600 }}>{model.name}</span></td>
                                        <td style={detailTdStyle}>{(model.service_group_ids || []).join(", ") || "-"}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    ) : (
                        <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No model permissions yet", "当前还没有模型权限")}</div>
                    )}
                </div>
            </div>
        </div>
    );
}
