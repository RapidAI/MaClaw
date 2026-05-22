import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetHubLLMServiceStatus, LoadConfig, RedeemHubLLMService } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOff, EventsOn } from "../../../wailsjs/runtime";
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
    effective?: boolean;
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

// Guard compatibility: authorizedModelsTableStyle, authorizedGroupTagStyle.

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
    // Use the backend's "effective" flag as single source of truth for which
    // grants count toward totals. Falls back to status string check for
    // backward compatibility with older hub versions that don't send "effective".
    const effectiveGrants = grants.filter((grant) => {
        if (typeof grant.effective === "boolean") return grant.effective;
        const s = String(grant.status || "").toLowerCase();
        return s !== "queued" && s !== "expired";
    });
    const grantTotal = effectiveGrants.reduce((sum, grant) => sum + Number(grant.credits_total || 0), 0);
    const grantUsed = effectiveGrants.reduce((sum, grant) => sum + Number(grant.credits_used || 0), 0);
    const grantRemaining = effectiveGrants.reduce((sum, grant) => sum + Number(grant.credits_remaining || 0), 0);
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
    const grantsForDetails = useMemo(() => {
        // Filter out expired grants from the details table
        const all = creditGrants(status);
        return all.filter((grant) => String(grant.status || "").toLowerCase() !== "expired");
    }, [status]);
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
        return <div className="hub-service-redeem__loading">{t("Loading service status...", "正在加载服务状态...")}</div>;
    }

    return (
        <div className="hub-service-redeem">
            {/* Card 1: Redeem code input - primary action, always visible first */}
            <div className="hub-service-redeem__card">
                <div className="hub-service-redeem__field">
                    <label className="hub-service-redeem__label">{t("Redeem Code", "兑换码")}</label>
                    <div className="hub-service-redeem__redeem-row">
                        <input
                            value={redeemCode}
                            onChange={(e) => setRedeemCode(e.target.value)}
                            onKeyDown={(e) => { if (e.key === "Enter" && !redeeming) handleRedeem(); }}
                            placeholder={t("Enter service card code", "请输入服务卡兑换码")}
                            disabled={redeeming}
                            className="hub-service-redeem__input"
                        />
                        <button type="button" onClick={handleRedeem} disabled={redeeming} className="hub-service-redeem__button hub-service-redeem__button--primary">
                            {redeeming ? t("Redeeming...", "兑换中...") : t("Redeem Now", "立即兑换")}
                        </button>
                    </div>
                </div>
                {redeemResult && (
                    <div className="hub-service-redeem__message" data-kind={redeemResult.ok ? "success" : "danger"}>
                        {redeemResult.msg}
                    </div>
                )}
            </div>

            {/* Card 2: Service status overview */}
            <div className="hub-service-redeem__card">
                <div className="hub-service-redeem__card-header">
                    <h3 className="hub-service-redeem__title">{t("Service Status", "服务状态")}</h3>
                    <div className="hub-service-redeem__actions">
                        <button type="button" onClick={() => void openHubCreditsPage()} className="hub-service-redeem__button hub-service-redeem__button--secondary">
                            {t("View Credits", "查看 Credits 或购买兑换码")}
                        </button>
                        <button type="button" onClick={() => loadStatus(true)} disabled={refreshing} className="hub-service-redeem__button hub-service-redeem__button--secondary">
                            {refreshing ? t("Refreshing...", "刷新中...") : t("Refresh", "刷新")}
                        </button>
                    </div>
                </div>

                {/* loadError: show inline in the status card, not above the redeem input */}
                {loadError && (
                    <div className="hub-service-redeem__message hub-service-redeem__message--spaced" data-kind="warning">
                        {loadError}
                    </div>
                )}

                <div className="hub-service-redeem__status-grid">
                    <div className="hub-service-redeem__metric-card">
                        <div className="hub-service-redeem__label">{t("Status", "状态")}</div>
                        <div className="hub-service-redeem__chip" data-kind={statusSummary.kind === "active" ? "success" : "warning"}>
                            {statusSummary.label}
                        </div>
                    </div>
                    <div className="hub-service-redeem__metric-card">
                        <div className="hub-service-redeem__label">{t("Authorized Groups", "已授权服务组")}</div>
                        <div className="hub-service-redeem__value">{activeGroupNames.length ? activeGroupNames.join(", ") : "-"}</div>
                    </div>
                    <div className="hub-service-redeem__metric-card">
                        <div className="hub-service-redeem__label">{t("Valid Until", "有效期至")}</div>
                        <div className="hub-service-redeem__value">{formatTime(serviceExpiry(status), lang)}</div>
                    </div>
                    <div className="hub-service-redeem__metric-card">
                        <div className="hub-service-redeem__label">{t("Default Model", "默认模型")}</div>
                        <div className="hub-service-redeem__value">{status?.default_model || "auto"}</div>
                    </div>
                </div>

                {statusSummary.kind !== "active" && statusSummary.detail ? (
                    <div className="hub-service-redeem__message hub-service-redeem__message--top" data-kind="warning">
                        {statusSummary.detail}
                    </div>
                ) : null}

                {/* inactive_reasons from Hub: diagnostic info for unavailable states */}
                {!statusSummary.detail && !status?.active && status?.inactive_reasons?.length ? (
                    <div className="hub-service-redeem__message hub-service-redeem__message--top" data-kind="warning">
                        {status.inactive_reasons.map((reason, i) => (
                            <div key={i}>- {reason}</div>
                        ))}
                    </div>
                ) : null}
            </div>

            {/* Card 3: Authorization details - table layout */}
            <div className="hub-service-redeem__card">
                <h3 className="hub-service-redeem__title hub-service-redeem__title--spaced">{t("Current Authorization Details", "当前授权详情")}</h3>
                <table className="hub-service-redeem__detail-table">
                    <tbody>
                        <tr>
                            <td className="hub-service-redeem__detail-label">{t("Available Models", "可用模型列表")}</td>
                            <td className="hub-service-redeem__detail-value">{availableModels.length ? availableModels.join(", ") : "auto"}</td>
                        </tr>
                        <tr>
                            <td className="hub-service-redeem__detail-label">{t("Valid Until", "有效期至")}</td>
                            <td className="hub-service-redeem__detail-value">{formatTime(serviceExpiry(status), lang)}</td>
                        </tr>
                        <tr>
                            <td className="hub-service-redeem__detail-label">{t("Current Grant Expiry", "当前授权到期")}</td>
                            <td className="hub-service-redeem__detail-value">{formatTime(status?.nearest_expires_at || serviceExpiry(status), lang)}</td>
                        </tr>
                    </tbody>
                </table>

                <div className="hub-service-redeem__credit-grid">
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Total credits", "总 credits")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--primary">{formatCredits(totals.total)}</div>
                    </div>
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Used credits", "已用 credits")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--warning">{formatCredits(totals.used)}</div>
                    </div>
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Remaining credits", "剩余 credits")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--success">{formatCredits(totals.remaining)}</div>
                    </div>
                </div>

                {/* Grant details include active, queued, period-limited, exhausted, and expired grants. */}
                <div className="hub-service-redeem__section">
                    <div className="hub-service-redeem__label">{t("Grant credit details", "授权额度明细", "授權額度明細")}</div>
                    {grantsForDetails.length ? (
                        <table className="hub-service-redeem__detail-table">
                            <thead>
                                <tr>
                                    <th>{t("Service Group", "服务组")}</th>
                                    <th>{t("Source", "来源")}</th>
                                    <th>{t("Starts At", "开始时间")}</th>
                                    <th>{t("Expires At", "到期时间")}</th>
                                    <th>{t("Total", "总额")}</th>
                                    <th>{t("Used", "已用")}</th>
                                    <th>{t("Remaining", "剩余")}</th>
                                    <th>{t("Status", "状态")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {grantsForDetails.map((grant, index) => (
                                    <tr key={(grant.service_group_id || "") + "-" + index}>
                                        <td><span className="hub-service-redeem__strong">{grant.service_group_id || "-"}</span></td>
                                        <td>{grant.source || "-"}</td>
                                        <td>{formatTime(grant.starts_at, lang)}</td>
                                        <td>{formatTime(grant.expires_at, lang)}</td>
                                        <td>{formatCredits(grant.credits_total)}</td>
                                        <td className="hub-service-redeem__cell--warning">{formatCredits(grant.credits_used)}</td>
                                        <td className="hub-service-redeem__cell--success">{formatCredits(grantRemainingCredits(grant))}</td>
                                        <td>{grantStatusLabel(grant, lang, t)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    ) : (
                        <div className="hub-service-redeem__empty">{t("No grant credit details", "暂无授权额度明细", "暫無授權額度明細")}</div>
                    )}
                </div>

                {/* Authorized Models table */}
                <div className="hub-service-redeem__section hub-service-redeem__section--large">
                    <div className="hub-service-redeem__label hub-service-redeem__label--spaced">{t("Authorized Models", "授权模型列表")}</div>
                    {(status?.authorized_models || []).length ? (
                        <div className="hub-service-redeem__models-wrap">
                            <table className="hub-service-redeem__detail-table hub-service-redeem__models-table">
                                <colgroup>
                                    <col className="hub-service-redeem__model-col" />
                                    <col className="hub-service-redeem__group-col" />
                                </colgroup>
                                <thead>
                                    <tr>
                                        <th>{t("Model", "模型")}</th>
                                        <th>{t("Service Groups", "服务组")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {(status?.authorized_models || []).map((model) => {
                                        const groups = (model.service_group_ids || []).filter(Boolean);
                                        return (
                                            <tr key={model.name}>
                                                <td>
                                                    <span className="hub-service-redeem__model-name">{model.name || "auto"}</span>
                                                </td>
                                                <td>
                                                    <div className="hub-service-redeem__group-list">
                                                        {groups.length ? groups.map((group) => (
                                                            <span key={group} className="hub-service-redeem__group-tag">{group}</span>
                                                        )) : <span className="hub-service-redeem__empty-inline">-</span>}
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    ) : (
                        <div className="hub-service-redeem__empty">{t("No model permissions yet", "当前还没有模型权限")}</div>
                    )}
                </div>
            </div>
        </div>
    );
}
