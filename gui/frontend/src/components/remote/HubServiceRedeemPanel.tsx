import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetHubLLMServiceStatus, LoadConfig, RedeemHubLLMService } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { colors, radius } from "./styles";
import { useDialog } from "../CustomDialog";
import { buildHubCreditsURL } from "../../utils/hubCredits";

interface HubLLMAuthorizedModel {
    name: string;
    provider_ids?: string[];
    service_group_ids?: string[];
}

interface HubLLMActiveGrant {
    service_group_id: string;
    source: string;
    starts_at?: string;
    expires_at: string;
    active?: boolean;
    status?: string;
    status_reason?: string;
    credits_total?: number;
    credits_used?: number;
    credits_available?: number;
    retry_after_seconds?: number;
    retry_after_at?: string;
    credits_remaining?: number;
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
    credit_grants?: HubLLMActiveGrant[];
    inactive_reasons?: string[];
    nearest_expires_at?: string;
    effective_expires_at?: string;
    default_model?: string;
    hub_llm_base_url?: string;
    credits_total?: number;
    credits_used?: number;
    credits_remaining?: number;
    credits_available?: number;
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

const creditMetricStyle: React.CSSProperties = {
    ...mutedCardStyle,
    padding: "10px 12px",
};

const authorizedModelsSectionStyle: React.CSSProperties = {
    marginTop: 0,
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    overflow: "hidden",
    background: colors.surface,
};

const authorizedModelsTableStyle: React.CSSProperties = {
    ...detailTableStyle,
    tableLayout: "fixed",
};

const authorizedModelsHeaderStyle: React.CSSProperties = {
    ...detailTheadThStyle,
    textAlign: "left",
    padding: "8px 14px",
    background: colors.surfaceMuted,
};

const authorizedModelsCellStyle: React.CSSProperties = {
    ...detailTdStyle,
    textAlign: "left",
    padding: "10px 14px",
    verticalAlign: "middle",
};

const authorizedModelsNameStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "flex-start",
    maxWidth: "100%",
    fontWeight: 700,
    color: colors.text,
    wordBreak: "break-word",
};

const authorizedGroupListStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "flex-start",
    flexWrap: "wrap",
    gap: 6,
};

const authorizedGroupTagStyle: React.CSSProperties = {
    ...chipStyle,
    background: colors.surface,
    color: colors.text,
    borderColor: colors.border,
};

function formatCredits(value?: number): string {
    const num = Number(value || 0);
    if (!Number.isFinite(num)) return "0";
    return num.toFixed(3).replace(/\.000$/, "").replace(/(\.\d*[1-9])0+$/, "$1");
}

function creditGrants(status: HubLLMServiceStatus | null): HubLLMActiveGrant[] {
    return (status?.credit_grants?.length ? status.credit_grants : status?.active_grants) || [];
}

function creditTotals(status: HubLLMServiceStatus | null) {
    const grants = creditGrants(status);
    const grantTotal = grants.reduce((sum, grant) => sum + Number(grant.credits_total || 0), 0);
    const grantUsed = grants.reduce((sum, grant) => sum + Number(grant.credits_used || 0), 0);
    const grantRemaining = grants.reduce((sum, grant) => sum + Number(grant.credits_remaining || 0), 0);
    const total = Number(status?.credits_total ?? grantTotal);
    const used = Number(status?.credits_used ?? grantUsed);
    const remainingRaw = Number(status?.credits_remaining ?? grantRemaining);
    const available = Number(status?.credits_available || 0);
    const onlyExpiredGrants = !status?.active && grants.length > 0 && grants.every((grant) => String(grant.status || "").toLowerCase() === "expired");
    if (onlyExpiredGrants) return { total, used, remaining: Math.max(0, available) };
    const remaining = remainingRaw > 0 ? remainingRaw : available;
    return { total, used, remaining };
}

function serviceExpiry(status: HubLLMServiceStatus | null): string {
    const grants = creditGrants(status);
    const primary = status?.active
        ? grants.find((grant) => String(grant.status || "").toLowerCase() === "active" || grant.active === true)
        : grants[0];
    return status?.effective_expires_at || status?.nearest_expires_at || primary?.expires_at || grants.map((grant) => String(grant.expires_at || "")).filter(Boolean).sort()[0] || "";
}

function grantRemainingCredits(grant: HubLLMActiveGrant): number {
    const remaining = Number(grant.credits_remaining || 0);
    if (remaining > 0) return remaining;
    const available = Number(grant.credits_available || 0);
    return available > 0 ? available : remaining;
}

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
function formatRetryDuration(seconds: number, lang?: string): string {
    const safeSeconds = Math.max(0, Math.ceil(Number(seconds || 0)));
    const zh = lang === "zh-Hans" || lang === "zh-Hant";
    if (safeSeconds < 60) return zh ? `${safeSeconds} 秒` : `${safeSeconds}s`;
    const minutes = Math.ceil(safeSeconds / 60);
    if (minutes < 60) return zh ? `${minutes} 分钟` : `${minutes}m`;
    const hours = Math.ceil(minutes / 60);
    if (hours < 24) return zh ? `${hours} 小时` : `${hours}h`;
    const days = Math.ceil(hours / 24);
    return zh ? `${days} 天` : `${days}d`;
}

function grantRetrySeconds(grant: HubLLMActiveGrant): number {
    let seconds = Number(grant.retry_after_seconds || 0);
    if ((!Number.isFinite(seconds) || seconds <= 0) && grant.retry_after_at) {
        const retryAt = new Date(grant.retry_after_at).getTime();
        if (Number.isFinite(retryAt)) seconds = Math.ceil((retryAt - Date.now()) / 1000);
    }
    return Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
}

function grantStatusLabel(
    grant: HubLLMActiveGrant,
    lang: string | undefined,
    t: (en: string, zhHans: string, zhHant?: string) => string,
): string {
    const status = String(grant.status || (grant.active === false ? "queued" : "active")).toLowerCase();
    const labels: Record<string, string> = {
        active: t("Active", "生效中"),
        queued: t("Not active yet", "未生效"),
        period_limited: t("Period limit exhausted", "周期限额用尽"),
        exhausted: t("Credits exhausted", "额度已用尽"),
        expired: t("Expired", "已过期"),
    };
    let label = labels[status] || (grant.active === false ? t("Queued", "排队中") : t("Active", "生效中"));
    const retrySeconds = grantRetrySeconds(grant);
    if (retrySeconds > 0 && (status === "period_limited" || status === "queued")) {
        const suffix = status === "queued"
            ? t("starts in about", "约", "約")
            : t("recovers in about", "约", "約");
        const zh = lang === "zh-Hans" || lang === "zh-Hant";
        label += zh
            ? ` · ${suffix} ${formatRetryDuration(retrySeconds, lang)} 后${status === "queued" ? "生效" : "恢复"}`
            : ` · ${suffix} ${formatRetryDuration(retrySeconds, lang)}`;
    }
    return label;
}
function periodLimitedGrant(status: HubLLMServiceStatus | null): HubLLMActiveGrant | undefined {
    return creditGrants(status).find((grant) => String(grant.status || "").toLowerCase() === "period_limited");
}
function firstGrantWithStatus(status: HubLLMServiceStatus | null, target: string): HubLLMActiveGrant | undefined {
    return creditGrants(status).find((grant) => String(grant.status || "").toLowerCase() === target);
}

function serviceStatusSummary(
    status: HubLLMServiceStatus | null,
    lang: string | undefined,
    t: (en: string, zhHans: string, zhHant?: string) => string,
) {
    const limitedGrant = periodLimitedGrant(status);
    if (limitedGrant && !status?.active) {
        const retrySeconds = grantRetrySeconds(limitedGrant);
        const retryText = retrySeconds > 0 ? formatRetryDuration(retrySeconds, lang) : "";
        const zh = lang === "zh-Hans" || lang === "zh-Hant";
        return {
            kind: "limited" as const,
            label: t("Period limited", "周期限流"),
            detail: retryText
                ? (zh
                    ? `当前周期额度已用尽，约 ${retryText} 后恢复。若官方还有其它可用通道会自动切换；不会静默切到你的私有服务商。`
                    : `The current period quota is exhausted and recovers in about ${retryText}. Routing switches automatically only to another available official route; it will not silently switch to your private provider.`)
                : t(
                    "The current period quota is exhausted. Routing switches automatically only to another available official route; it will not silently switch to your private provider.",
                    "当前周期额度已用尽。若官方还有其它可用通道会自动切换；不会静默切到你的私有服务商。",
                    "目前週期額度已用盡。若官方還有其他可用通道會自動切換；不會靜默切到你的私有服務商。",
                ),
        };
    }
    const queuedGrant = firstGrantWithStatus(status, "queued");
    if (queuedGrant && !status?.active) {
        const retrySeconds = grantRetrySeconds(queuedGrant);
        const retryText = retrySeconds > 0 ? formatRetryDuration(retrySeconds, lang) : "";
        const zh = lang === "zh-Hans" || lang === "zh-Hant";
        return {
            kind: "queued" as const,
            label: t("Not active yet", "授权尚未生效"),
            detail: retryText
                ? (zh ? `授权约 ${retryText} 后生效。` : `Authorization starts in about ${retryText}.`)
                : t("Authorization is not active yet.", "授权尚未生效。", "授權尚未生效。"),
        };
    }
    const exhaustedGrant = firstGrantWithStatus(status, "exhausted");
    if (exhaustedGrant && !status?.active) {
        return {
            kind: "exhausted" as const,
            label: t("Credits exhausted", "额度已用尽"),
            detail: t(
                "Official credits are exhausted. You can redeem more credits or switch to another provider.",
                "官方额度已用尽。可以兑换更多额度，或切换到其它服务商。",
                "官方額度已用盡。可以兌換更多額度，或切換到其他服務商。",
            ),
        };
    }
    const expiredGrant = firstGrantWithStatus(status, "expired");
    if (expiredGrant && !status?.active) {
        return {
            kind: "expired" as const,
            label: t("Expired", "授权已过期"),
            detail: t(
                "Official authorization has expired. You can redeem a new grant or switch to another provider.",
                "官方授权已过期。可以兑换新的授权，或切换到其它服务商。",
                "官方授權已過期。可以兌換新的授權，或切換到其他服務商。",
            ),
        };
    }
    if (status?.active) {
        return { kind: "active" as const, label: t("Active", "已开通"), detail: "" };
    }
    return { kind: "inactive" as const, label: t("Not Active", "未开通"), detail: "" };
}

export function HubServiceRedeemPanel({ lang, onStatusChange }: Props) {
    const { showAlert } = useDialog();
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

    useEffect(() => {
        const timers = new Set<number>();
        const scheduleReload = (delayMs: number) => {
            const timer = window.setTimeout(() => {
                timers.delete(timer);
                void loadStatus(true);
            }, delayMs);
            timers.add(timer);
        };
        const handler = () => {
            void loadStatus(true);
            scheduleReload(2500);
        };
        EventsOn("llm-token-usage-changed", handler);
        EventsOn("hub-llm-service-changed", handler);
        return () => {
            timers.forEach((timer) => window.clearTimeout(timer));
            EventsOff("llm-token-usage-changed");
            EventsOff("hub-llm-service-changed");
        };
    }, [loadStatus]);

    const activeGroupNames = useMemo(() => {
        if (!status?.service_group_names?.length) return [] as string[];
        return status.service_group_names.filter(Boolean);
    }, [status]);

    const availableModels = useMemo(() => {
        return (status?.available_models || []).filter(Boolean);
    }, [status]);

    const totals = useMemo(() => creditTotals(status), [status]);
    const grantsForDetails = useMemo(() => creditGrants(status), [status]);
    const statusSummary = useMemo(() => serviceStatusSummary(status, lang, t), [status, lang, t]);

    const openHubCreditsPage = useCallback(async () => {
        try {
            const cfg = await LoadConfig() as { remote_hub_url?: string; remote_viewer_token?: string } | null;
            const url = buildHubCreditsURL(cfg?.remote_hub_url, cfg?.remote_viewer_token);
            if (!url) {
                await showAlert(t("Credits page is unavailable because Hub login information is missing.", "Hub 登录信息缺失，暂时无法打开 Credits 页面。"));
                return;
            }
            BrowserOpenURL(url);
        } catch (error) {
            await showAlert(String(error || t("Failed to open Credits page", "打开 Credits 页面失败")));
        }
    }, [showAlert, t]);

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
                    <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", justifyContent: "flex-end" }}>
                        <button type="button" onClick={() => void openHubCreditsPage()} style={secondaryButtonStyle}>
                            {t("View Credits", "查看Credits 或 购买兑换码")}
                        </button>
                        <button type="button" onClick={() => loadStatus(true)} disabled={refreshing} style={secondaryButtonStyle}>
                            {refreshing ? t("Refreshing...", "刷新中...") : t("Refresh", "刷新")}
                        </button>
                    </div>
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
                        <div style={{ ...chipStyle, background: statusSummary.kind === "active" ? colors.successBg : colors.warningBg, color: statusSummary.kind === "active" ? colors.success : colors.warning, borderColor: statusSummary.kind === "active" ? colors.success : colors.warning }}>
                            {statusSummary.label}
                        </div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Authorized Groups", "已授权服务组")}</div>
                        <div style={valueStyle}>{activeGroupNames.length ? activeGroupNames.join(", ") : "-"}</div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Valid Until", "有效期至")}</div>
                        <div style={valueStyle}>{formatTime(serviceExpiry(status), lang)}</div>
                    </div>
                    <div style={mutedCardStyle}>
                        <div style={labelStyle}>{t("Default Model", "默认模型")}</div>
                        <div style={valueStyle}>{status?.default_model || "auto"}</div>
                    </div>
                </div>

                {statusSummary.kind !== "active" && statusSummary.detail ? (
                    <div style={{ marginTop: 12, padding: "8px 10px", borderRadius: radius.md, background: colors.warningBg, color: colors.warning, border: `1px solid ${colors.warning}`, fontSize: "0.78rem", lineHeight: 1.6 }}>
                        {statusSummary.detail}
                    </div>
                ) : null}

                {/* inactive_reasons from Hub — diagnostic info for unavailable states */}
                {!statusSummary.detail && !status?.active && status?.inactive_reasons?.length ? (
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
                            <td style={detailThStyle}>{t("Available Models", "可用模型列表")}</td>
                            <td style={detailTdStyle}>{availableModels.length ? availableModels.join(", ") : "auto"}</td>
                        </tr>
                        <tr>
                            <td style={detailThStyle}>{t("Valid Until", "有效期至")}</td>
                            <td style={detailTdStyle}>{formatTime(serviceExpiry(status), lang)}</td>
                        </tr>
                        <tr>
                            <td style={detailThStyle}>{t("Current Grant Expiry", "当前授权到期")}</td>
                            <td style={detailTdStyle}>{formatTime(status?.nearest_expires_at || serviceExpiry(status), lang)}</td>
                        </tr>
                    </tbody>
                </table>

                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 10, marginTop: 14 }}>
                    <div style={creditMetricStyle}>
                        <div style={labelStyle}>{t("Total credits", "总 credits")}</div>
                        <div style={{ ...valueStyle, color: colors.primary, fontWeight: 700 }}>{formatCredits(totals.total)}</div>
                    </div>
                    <div style={creditMetricStyle}>
                        <div style={labelStyle}>{t("Used credits", "已用 credits")}</div>
                        <div style={{ ...valueStyle, color: colors.warning, fontWeight: 700 }}>{formatCredits(totals.used)}</div>
                    </div>
                    <div style={creditMetricStyle}>
                        <div style={labelStyle}>{t("Remaining credits", "剩余 credits")}</div>
                        <div style={{ ...valueStyle, color: colors.success, fontWeight: 700 }}>{formatCredits(totals.remaining)}</div>
                    </div>
                </div>

                {/* Grant details include active, queued, period-limited, exhausted, and expired grants. */}
                <div style={{ marginTop: 16 }}>
                    <div style={labelStyle}>{t("Grant credit details", "授权额度明细", "授權額度明細")}</div>
                    {grantsForDetails.length ? (
                        <table style={detailTableStyle}>
                            <thead>
                                <tr>
                                    <th style={detailTheadThStyle}>{t("Service Group", "服务组")}</th>
                                    <th style={detailTheadThStyle}>{t("Source", "来源")}</th>
                                    <th style={detailTheadThStyle}>{t("Starts At", "开始时间")}</th>
                                    <th style={detailTheadThStyle}>{t("Expires At", "到期时间")}</th>
                                    <th style={detailTheadThStyle}>{t("Total", "总额")}</th>
                                    <th style={detailTheadThStyle}>{t("Used", "已用")}</th>
                                    <th style={detailTheadThStyle}>{t("Remaining", "剩余")}</th>
                                    <th style={detailTheadThStyle}>{t("Status", "状态")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {grantsForDetails.map((grant, index) => (
                                    <tr key={`${grant.service_group_id}-${index}`}>
                                        <td style={detailTdStyle}><span style={{ fontWeight: 600 }}>{grant.service_group_id || "-"}</span></td>
                                        <td style={detailTdStyle}>{grant.source || "-"}</td>
                                        <td style={detailTdStyle}>{formatTime(grant.starts_at, lang)}</td>
                                        <td style={detailTdStyle}>{formatTime(grant.expires_at, lang)}</td>
                                        <td style={detailTdStyle}>{formatCredits(grant.credits_total)}</td>
                                        <td style={{ ...detailTdStyle, color: colors.warning }}>{formatCredits(grant.credits_used)}</td>
                                        <td style={{ ...detailTdStyle, color: colors.success }}>{formatCredits(grantRemainingCredits(grant))}</td>
                                        <td style={detailTdStyle}>{grantStatusLabel(grant, lang, t)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    ) : (
                        <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No grant credit details", "暂无授权额度明细", "暫無授權額度明細")}</div>
                    )}
                </div>

                {/* Authorized Models table */}
                <div style={{ marginTop: 18 }}>
                    <div style={{ ...labelStyle, marginBottom: 8 }}>{t("Authorized Models", "\u6388\u6743\u6a21\u578b\u5217\u8868")}</div>
                    {(status?.authorized_models || []).length ? (
                        <div style={authorizedModelsSectionStyle}>
                            <table style={authorizedModelsTableStyle}>
                                <colgroup>
                                    <col style={{ width: "42%" }} />
                                    <col style={{ width: "58%" }} />
                                </colgroup>
                                <thead>
                                    <tr>
                                        <th style={authorizedModelsHeaderStyle}>{t("Model", "\u6a21\u578b")}</th>
                                        <th style={authorizedModelsHeaderStyle}>{t("Service Groups", "\u670d\u52a1\u7ec4")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {(status?.authorized_models || []).map((model) => {
                                        const groups = (model.service_group_ids || []).filter(Boolean);
                                        return (
                                            <tr key={model.name}>
                                                <td style={authorizedModelsCellStyle}>
                                                    <span style={authorizedModelsNameStyle}>{model.name || "auto"}</span>
                                                </td>
                                                <td style={authorizedModelsCellStyle}>
                                                    <div style={authorizedGroupListStyle}>
                                                        {groups.length ? groups.map((group) => (
                                                            <span key={group} style={authorizedGroupTagStyle}>{group}</span>
                                                        )) : <span style={{ color: colors.textMuted }}>-</span>}
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    ) : (
                        <div style={{ ...valueStyle, color: colors.textMuted }}>{t("No model permissions yet", "\u5f53\u524d\u8fd8\u6ca1\u6709\u6a21\u578b\u6743\u9650")}</div>
                    )}
                </div>
            </div>
        </div>
    );
}

