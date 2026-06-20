import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetHubLLMServiceStatus, LoadConfig, RedeemHubLLMService } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { useDialog } from "../CustomDialog";
import { buildHubCardStoreURL, buildHubCreditsURL, grantCanContributeExpiry, latestExpiry, numeric } from "../../utils/hubCredits";
import { localizeHubServiceReason, localizeHubServiceRedeemError, localizeHubServiceStatusError } from "../../utils/hubServiceI18n";

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
    period_limits?: {
        five_hour?: number;
        daily?: number;
        weekly?: number;
        monthly?: number;
    };
    period_usage?: {
        five_hour?: { window_start?: string; window_end?: string; credits_used?: number };
        daily?: { window_start?: string; window_end?: string; credits_used?: number };
        weekly?: { window_start?: string; window_end?: string; credits_used?: number };
        monthly?: { window_start?: string; window_end?: string; credits_used?: number };
    };
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

function callBackend<T>(call: () => T | Promise<T>): Promise<T> {
    return Promise.resolve().then(call);
}

function safeEventsOn(eventName: string, callback: (...args: any[]) => void) {
    try {
        return EventsOn(eventName, callback);
    } catch {
        return undefined;
    }
}

function safeEventsOff(eventName: string) {
    try {
        EventsOff(eventName);
    } catch {
        // Runtime events are unavailable in a plain browser dev session.
    }
}

function safeBrowserOpenURL(url: string) {
    try {
        BrowserOpenURL(url);
    } catch {
        window.open(url, "_blank", "noopener,noreferrer");
    }
}

// Guard compatibility: authorizedModelsTableStyle, authorizedGroupTagStyle.

function formatCredits(value?: number): string {
    const num = Number(value || 0);
    if (!Number.isFinite(num)) return "0";
    return num.toFixed(2).replace(/\.00$/, "").replace(/(\.\d*[1-9])0$/, "$1");
}

function formatUnlimitedCredits(lang?: string): string {
    if (lang === "zh-Hans") return "不限";
    if (lang === "zh-Hant") return "不限";
    return "Unlimited";
}

function hasPeriodLimits(grant: HubLLMActiveGrant): boolean {
    const limits = grant.period_limits;
    if (!limits) return false;
    return numeric(limits.five_hour) > 0
        || numeric(limits.daily) > 0
        || numeric(limits.weekly) > 0
        || numeric(limits.monthly) > 0;
}

function grantDurationDays(grant: HubLLMActiveGrant): number {
    const startsAt = new Date(grant.starts_at || "").getTime();
    const expiresAt = new Date(grant.expires_at || "").getTime();
    if (!Number.isFinite(startsAt) || !Number.isFinite(expiresAt) || expiresAt <= startsAt) return 0;
    return Math.round((expiresAt - startsAt) / (24 * 60 * 60 * 1000));
}

function periodCardKind(grant: HubLLMActiveGrant): "annual" | "quarterly" | "monthly" | "weekly" | "period" {
    const days = grantDurationDays(grant);
    if (days >= 330) return "annual";
    if (days >= 80) return "quarterly";
    if (days >= 27) return "monthly";
    if (days >= 6) return "weekly";
    return "period";
}

function formatPeriodCardKind(
    grant: HubLLMActiveGrant,
    t: (en: string, zhHans: string, zhHant?: string) => string,
): string {
    switch (periodCardKind(grant)) {
        case "annual":
            return t("annual card", "包年卡", "包年卡");
        case "quarterly":
            return t("quarterly card", "包季卡", "包季卡");
        case "monthly":
            return t("monthly card", "包月卡", "包月卡");
        case "weekly":
            return t("weekly card", "包周卡", "包週卡");
        default:
            return t("period card", "周期卡", "週期卡");
    }
}

function formatGrantSource(
    grant: HubLLMActiveGrant,
    t: (en: string, zhHans: string, zhHant?: string) => string,
): string {
    const source = String(grant.source || "").trim();
    const key = source.toLowerCase();
    if (!source) return "-";
    if (key === "card") {
        const cardKind = hasPeriodLimits(grant) ? formatPeriodCardKind(grant, t) : t("point card", "点卡", "點卡");
        return t(`Recharge card (${cardKind})`, `充值卡（${cardKind}）`, `儲值卡（${cardKind}）`);
    }
    if (key === "default_new_user_backfill") {
        return t("New user gift", "新用户赠送", "新用戶贈送");
    }
    if (key === "admin_grant") {
        return t("Admin grant", "管理员授权", "管理員授權");
    }
    if (key === "ve_platform_employee") {
        return t("Platform employee grant", "平台员工授权", "平台員工授權");
    }
    return source;
}

function creditGrants(status: HubLLMServiceStatus | null): HubLLMActiveGrant[] {
    return (status?.credit_grants?.length ? status.credit_grants : status?.active_grants) || [];
}

function creditTotals(status: HubLLMServiceStatus | null) {
    const grants = creditGrants(status);
    const visibleGrants = grants.filter((grant) => String(grant.status || "").toLowerCase() !== "expired");
    // Use the backend's "effective" flag as single source of truth for which
    // grants count toward used/remaining. Falls back to status string check for
    // backward compatibility with older hub versions that don't send "effective".
    const effectiveGrants = grants.filter((grant) => {
        if (typeof grant.effective === "boolean") return grant.effective;
        const s = String(grant.status || "").toLowerCase();
        return s !== "queued" && s !== "expired";
    });
    const visibleGrantTotal = visibleGrants.reduce((sum, grant) => {
        const total = numeric(grant.credits_total);
        if (total > 0) return sum + total;
        return sum + Math.max(numeric(grant.credits_available), numeric(grant.credits_remaining));
    }, 0);
    const grantTotal = effectiveGrants.reduce((sum, grant) => sum + numeric(grant.credits_total), 0);
    const grantUsed = effectiveGrants.reduce((sum, grant) => sum + numeric(grant.credits_used), 0);
    const grantRemaining = effectiveGrants.reduce((sum, grant) => sum + numeric(grant.credits_remaining), 0);
    const grantAvailable = effectiveGrants.reduce((sum, grant) => sum + numeric(grant.credits_available), 0);
    let total = Math.max(numeric(status?.credits_total ?? grantTotal), visibleGrantTotal);
    const used = numeric(status?.credits_used ?? grantUsed);
    const remainingRaw = numeric(status?.credits_remaining ?? grantRemaining);
    const statusAvailable = numeric(status?.credits_available);
    const available = statusAvailable > 0 ? statusAvailable : grantAvailable;
    const onlyExpiredGrants = !status?.active && grants.length > 0 && grants.every((grant) => String(grant.status || "").toLowerCase() === "expired");
    if (onlyExpiredGrants) return { total, used, remaining: Math.max(0, available) };
    const remaining = (status?.active || remainingRaw <= 0) && available > 0 ? available : remainingRaw;
    if (remaining > 0 && total < used + remaining) total = used + remaining;
    return { total, used, remaining };
}

function localizeServiceStatusReason(
    reason: unknown,
    lang: string | undefined,
    _t: (en: string, zhHans: string, zhHant?: string) => string,
): string {
    const raw = String(reason || "").trim().replace(/^Error:\s*/i, "");
    const localizedReason = localizeHubServiceReason(raw, lang);
    if (localizedReason !== raw) return localizedReason;
    return localizeHubServiceStatusError(raw, lang);
}

function serviceExpiry(status: HubLLMServiceStatus | null): string {
    const grants = creditGrants(status);
    const latestGrantExpiry = latestExpiry(grants
        .filter(grantCanContributeExpiry)
        .map((grant) => String(grant.expires_at || "")));
    const latestExpiredGrantExpiry = latestExpiry(grants
        .filter((grant) => String(grant.status || "").toLowerCase() === "expired")
        .map((grant) => String(grant.expires_at || "")));
    return status?.effective_expires_at || latestGrantExpiry || status?.nearest_expires_at || latestExpiredGrantExpiry || "";
}

function grantRemainingCredits(grant: HubLLMActiveGrant): number {
    const remaining = numeric(grant.credits_remaining);
    if (remaining > 0) return remaining;
    const available = numeric(grant.credits_available);
    return available > 0 ? available : remaining;
}

function hasAnyCreditValue(status: HubLLMServiceStatus | null): boolean {
    const fields: Array<keyof HubLLMServiceStatus> = ["credits_total", "credits_used", "credits_remaining", "credits_available"];
    if (fields.some((field) => numeric(status?.[field]) > 0)) return true;
    return creditGrants(status).some((grant) => (
        numeric(grant.credits_total) > 0
        || numeric(grant.credits_used) > 0
        || numeric(grant.credits_remaining) > 0
        || numeric(grant.credits_available) > 0
    ));
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
            label: t("Period limited", "周期限额"),
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
            const next = await callBackend(() => GetHubLLMServiceStatus()) as HubLLMServiceStatus;
            setStatus(next);
            onStatusChangeRef.current?.(next);
        } catch (error) {
            setLoadError(String(error || ""));
        } finally {
            setLoading(false);
            setRefreshing(false);
        }
    }, []); // onStatusChange is read from ref

    // Load once on mount. Inline errors are localized at render time.
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
        const offTokenUsageChanged = safeEventsOn("llm-token-usage-changed", handler);
        const offHubLLMServiceChanged = safeEventsOn("hub-llm-service-changed", handler);
        return () => {
            timers.forEach((timer) => window.clearTimeout(timer));
            if (typeof offTokenUsageChanged === "function") offTokenUsageChanged();
            else safeEventsOff("llm-token-usage-changed");
            if (typeof offHubLLMServiceChanged === "function") offHubLLMServiceChanged();
            else safeEventsOff("hub-llm-service-changed");
        };
    }, [loadStatus]);

    const activeGroupNames = useMemo(() => {
        const names = (status?.service_group_names || []).filter(Boolean);
        return names.length ? names : (status?.service_group_ids || []).filter(Boolean);
    }, [status]);

    const availableModels = useMemo(() => {
        return (status?.available_models || []).filter(Boolean);
    }, [status]);

    const authorizedModelsForDisplay = useMemo(() => {
        const models = (status?.authorized_models || []).filter((model) => model?.name);
        if (models.length) return models;
        return availableModels.map((name) => ({
            name,
            service_group_ids: (status?.service_group_ids || activeGroupNames).filter(Boolean),
        }));
    }, [activeGroupNames, availableModels, status]);

    const totals = useMemo(() => creditTotals(status), [status]);
    const grantsForDetails = useMemo(() => {
        // Filter out expired grants from the details table
        const all = creditGrants(status);
        return all.filter((grant) => String(grant.status || "").toLowerCase() !== "expired");
    }, [status]);
    const isActiveUnmeteredService = useMemo(() => {
        if (!status?.active || grantsForDetails.length > 0 || hasAnyCreditValue(status)) return false;
        return availableModels.length > 0 || authorizedModelsForDisplay.length > 0 || activeGroupNames.length > 0;
    }, [activeGroupNames, availableModels, authorizedModelsForDisplay, grantsForDetails, status]);
    const expiryLabel = useMemo(() => {
        const expiry = serviceExpiry(status);
        if (expiry) return formatTime(expiry, lang);
        if (isActiveUnmeteredService) return t("No expiry", "长期有效", "長期有效");
        return "-";
    }, [isActiveUnmeteredService, lang, status, t]);
    const statusSummary = useMemo(() => serviceStatusSummary(status, lang, t), [status, lang, t]);

    // Aggregate period limits and usage from grants that have period constraints.
    const periodInfo = useMemo(() => {
        const grants = creditGrants(status);
        const withPeriod = grants.filter((g) => {
            const pl = g.period_limits;
            return pl && (Number(pl.five_hour || 0) > 0 || Number(pl.daily || 0) > 0 || Number(pl.weekly || 0) > 0 || Number(pl.monthly || 0) > 0);
        });
        if (withPeriod.length === 0) return null;
        // Aggregate across all grants (typically one, but handle multi-grant).
        // Use the latest window_end among grants for each period.
        const agg = {
            five_hour: { limit: 0, used: 0, windowEnd: "" },
            daily: { limit: 0, used: 0, windowEnd: "" },
            weekly: { limit: 0, used: 0, windowEnd: "" },
            monthly: { limit: 0, used: 0, windowEnd: "" },
        };
        for (const g of withPeriod) {
            const pl = g.period_limits!;
            const pu = g.period_usage;
            agg.five_hour.limit += Number(pl.five_hour || 0);
            agg.five_hour.used += Number(pu?.five_hour?.credits_used || 0);
            if (pu?.five_hour?.window_end && (!agg.five_hour.windowEnd || pu.five_hour.window_end > agg.five_hour.windowEnd)) agg.five_hour.windowEnd = pu.five_hour.window_end;
            agg.daily.limit += Number(pl.daily || 0);
            agg.daily.used += Number(pu?.daily?.credits_used || 0);
            if (pu?.daily?.window_end && (!agg.daily.windowEnd || pu.daily.window_end > agg.daily.windowEnd)) agg.daily.windowEnd = pu.daily.window_end;
            agg.weekly.limit += Number(pl.weekly || 0);
            agg.weekly.used += Number(pu?.weekly?.credits_used || 0);
            if (pu?.weekly?.window_end && (!agg.weekly.windowEnd || pu.weekly.window_end > agg.weekly.windowEnd)) agg.weekly.windowEnd = pu.weekly.window_end;
            agg.monthly.limit += Number(pl.monthly || 0);
            agg.monthly.used += Number(pu?.monthly?.credits_used || 0);
            if (pu?.monthly?.window_end && (!agg.monthly.windowEnd || pu.monthly.window_end > agg.monthly.windowEnd)) agg.monthly.windowEnd = pu.monthly.window_end;
        }
        const computeResetIn = (windowEnd: string): string => {
            if (!windowEnd) return "";
            const end = new Date(windowEnd).getTime();
            if (!Number.isFinite(end)) return "";
            const remaining = Math.max(0, Math.ceil((end - Date.now()) / 1000));
            if (remaining <= 0) return "";
            return formatRetryDuration(remaining, lang);
        };
        type PeriodEntry = { key: string; label: string; limit: number; used: number; resetIn: string };
        const entries: PeriodEntry[] = [];
        if (agg.five_hour.limit > 0) entries.push({ key: "five_hour", label: t("5-Hour Limit", "5小时限额", "5小時限額"), limit: agg.five_hour.limit, used: agg.five_hour.used, resetIn: computeResetIn(agg.five_hour.windowEnd) });
        if (agg.daily.limit > 0) entries.push({ key: "daily", label: t("Daily Limit", "日限额", "日限額"), limit: agg.daily.limit, used: agg.daily.used, resetIn: computeResetIn(agg.daily.windowEnd) });
        if (agg.weekly.limit > 0) entries.push({ key: "weekly", label: t("Weekly Limit", "周限额", "週限額"), limit: agg.weekly.limit, used: agg.weekly.used, resetIn: computeResetIn(agg.weekly.windowEnd) });
        if (agg.monthly.limit > 0) entries.push({ key: "monthly", label: t("Monthly Limit", "月限额", "月限額"), limit: agg.monthly.limit, used: agg.monthly.used, resetIn: computeResetIn(agg.monthly.windowEnd) });
        return entries.length > 0 ? entries : null;
    }, [lang, status, t]);

    const openHubCardStorePage = useCallback(async () => {
        try {
            const cfg = await callBackend(() => LoadConfig()) as { remote_hub_id?: string; remote_hub_url?: string; remote_hubcenter_url?: string; remote_tenant_id?: string; remote_email?: string; remote_viewer_token?: string } | null;
            const url = buildHubCardStoreURL(cfg?.remote_hub_url, cfg?.remote_tenant_id, cfg?.remote_email, cfg?.remote_viewer_token, cfg?.remote_hubcenter_url, cfg?.remote_hub_id);
            if (!url) {
                await showAlert(t("Card store is unavailable because Hub URL is missing.", "Hub \u5730\u5740\u7f3a\u5931\uff0c\u6682\u65f6\u65e0\u6cd5\u6253\u5f00\u670d\u52a1\u5361\u5546\u5e97\u3002"));
                return;
            }
            safeBrowserOpenURL(url);
        } catch (error) {
            await showAlert(String(error || t("Failed to open card store", "\u6253\u5f00\u670d\u52a1\u5361\u5546\u5e97\u5931\u8d25")));
        }
    }, [showAlert, t]);

    const openHubCreditsPage = useCallback(async () => {
        try {
            const cfg = await callBackend(() => LoadConfig()) as { remote_hub_url?: string; remote_viewer_token?: string; remote_tenant_id?: string; remote_email?: string } | null;
            const url = buildHubCreditsURL(cfg?.remote_hub_url, cfg?.remote_viewer_token, cfg?.remote_tenant_id, cfg?.remote_email);
            if (!url) {
                await showAlert(t("Credits page is unavailable because Hub URL is missing.", "Hub 地址缺失，暂时无法打开额度页面。", "Hub 位址缺失，暫時無法開啟額度頁面。"));
                return;
            }
            safeBrowserOpenURL(url);
        } catch (error) {
            await showAlert(String(error || t("Failed to open Credits page", "打开额度页面失败", "開啟額度頁面失敗")));
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
            const next = await callBackend(() => RedeemHubLLMService(code)) as HubLLMServiceStatus;
            setStatus(next);
            setRedeemCode("");
            setLoadError(null);
            onStatusChangeRef.current?.(next);
            setRedeemResult({ ok: true, msg: t("Redeem successful", "兑换成功") });
        } catch (error) {
            setRedeemResult({ ok: false, msg: localizeHubServiceRedeemError(error, lang) });
        } finally {
            setRedeeming(false);
        }
    }, [lang, redeemCode, t]);

    if (loading) {
        return <div className="hub-service-redeem__loading">{t("Loading service status...", "正在加载服务状态...")}</div>;
    }

    return (
        <div className="hub-service-redeem">
            <div className="hub-service-redeem__card hub-service-redeem__summary-card">
                <div className="hub-service-redeem__card-header">
                    <div className="hub-service-redeem__title-group">
                        <h3 className="hub-service-redeem__title">{t("Service Redemption", "服务兑换")}</h3>
                    </div>
                    <div className="hub-service-redeem__actions">
                        <button
                            type="button"
                            onClick={() => void openHubCardStorePage()}
                            className="hub-service-redeem__button hub-service-redeem__button--buy"
                            title={t("Open card store to buy Credits", "前往服务卡商店购买额度", "前往服務卡商店購買額度")}
                        >
                            <span className="hub-service-redeem__buy-icon" aria-hidden="true" />
                            <span>{t("Buy Credits", "购买额度", "購買額度")}</span>
                        </button>
                        <button
                            type="button"
                            onClick={() => void openHubCreditsPage()}
                            className="hub-service-redeem__button hub-service-redeem__button--secondary"
                            title={t("Open Credits page to view balance", "前往额度页面查看余额", "前往額度頁面查看餘額")}
                        >
                            {t("View Credits", "查看额度", "查看額度")}
                        </button>
                        <button type="button" onClick={() => loadStatus(true)} disabled={refreshing} className="hub-service-redeem__button hub-service-redeem__button--secondary">
                            {refreshing ? t("Refreshing...", "刷新中...") : t("Refresh", "刷新")}
                        </button>
                    </div>
                </div>

                <div className="hub-service-redeem__redeem-panel">
                    <label className="hub-service-redeem__label" htmlFor="hub-service-redeem-code">{t("Redeem Code", "兑换码")}</label>
                    <div className="hub-service-redeem__redeem-row">
                        <input
                            id="hub-service-redeem-code"
                            type="text"
                            value={redeemCode}
                            onChange={(e) => setRedeemCode(e.target.value)}
                            onKeyDown={(e) => { if (e.key === "Enter" && !redeeming) handleRedeem(); }}
                            placeholder={t("Enter service card code", "请输入服务卡兑换码")}
                            autoComplete="off"
                            spellCheck={false}
                            disabled={redeeming}
                            className="hub-service-redeem__input"
                        />
                        <button type="button" onClick={handleRedeem} disabled={redeeming} className="hub-service-redeem__button hub-service-redeem__button--primary">
                            {redeeming ? t("Redeeming...", "兑换中...") : t("Redeem Now", "立即兑换")}
                        </button>
                    </div>
                    {redeemResult && (
                        <div className="hub-service-redeem__message hub-service-redeem__message--top" data-kind={redeemResult.ok ? "success" : "danger"}>
                            {redeemResult.msg}
                        </div>
                    )}
                </div>

                {/* loadError: show inline in the status card, not above the redeem input */}
                {loadError && (
                    <div className="hub-service-redeem__message hub-service-redeem__message--spaced" data-kind="warning">
                        {localizeServiceStatusReason(loadError, lang, t)}
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
                        <div className="hub-service-redeem__value">{expiryLabel}</div>
                    </div>
                    <div className="hub-service-redeem__metric-card">
                        <div className="hub-service-redeem__label">{t("Default Model", "默认模型")}</div>
                        <div className="hub-service-redeem__value">{status?.default_model || "auto"}</div>
                    </div>
                </div>

                <div className="hub-service-redeem__credit-grid">
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Total credits", "总额度", "總額度")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--primary">{isActiveUnmeteredService ? formatUnlimitedCredits(lang) : formatCredits(totals.total)}</div>
                    </div>
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Used credits", "已用额度", "已用額度")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--warning">{isActiveUnmeteredService ? "-" : formatCredits(totals.used)}</div>
                    </div>
                    <div className="hub-service-redeem__credit-card">
                        <div className="hub-service-redeem__label">{t("Remaining credits", "剩余额度", "剩餘額度")}</div>
                        <div className="hub-service-redeem__value hub-service-redeem__value--success">{isActiveUnmeteredService ? formatUnlimitedCredits(lang) : formatCredits(totals.remaining)}</div>
                    </div>
                </div>

                {periodInfo && (
                    <div className="hub-service-redeem__period-section">
                        <div className="hub-service-redeem__label hub-service-redeem__label--section">{t("Period Limits (current window)", "周期限额（当前窗口）", "週期限額（當前窗口）")}</div>
                        <div className="hub-service-redeem__period-grid">
                            {periodInfo.map((entry) => {
                                const pct = entry.limit > 0 ? Math.min(100, Math.round((entry.used / entry.limit) * 100)) : 0;
                                const barKind = pct >= 100 ? "exhausted" : pct >= 80 ? "warning" : "normal";
                                return (
                                    <div key={entry.key} className="hub-service-redeem__period-card">
                                        <div className="hub-service-redeem__period-label">{entry.label}</div>
                                        <div className="hub-service-redeem__period-values">
                                            <span className="hub-service-redeem__period-used">{formatCredits(entry.used)}</span>
                                            <span className="hub-service-redeem__period-sep">/</span>
                                            <span className="hub-service-redeem__period-limit">{formatCredits(entry.limit)}</span>
                                        </div>
                                        <div className="hub-service-redeem__period-bar" data-kind={barKind}>
                                            <div className="hub-service-redeem__period-bar-fill" style={{ width: `${pct}%` }} />
                                        </div>
                                        <div className="hub-service-redeem__period-footer">
                                            <span className="hub-service-redeem__period-pct">{pct}%</span>
                                            {entry.resetIn && (
                                                <span className="hub-service-redeem__period-reset">
                                                    {t(`Resets in ${entry.resetIn}`, `${entry.resetIn}后重置`, `${entry.resetIn}後重置`)}
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                )}

                {statusSummary.kind !== "active" && statusSummary.detail ? (
                    <div className="hub-service-redeem__message hub-service-redeem__message--top" data-kind="warning">
                        {statusSummary.detail}
                    </div>
                ) : null}

                {/* inactive_reasons from Hub: diagnostic info for unavailable states */}
                {!statusSummary.detail && !status?.active && status?.inactive_reasons?.length ? (
                    <div className="hub-service-redeem__message hub-service-redeem__message--top" data-kind="warning">
                        {status.inactive_reasons.map((reason, i) => (
                            <div key={i}>- {localizeServiceStatusReason(reason, lang, t)}</div>
                        ))}
                    </div>
                ) : null}
            </div>

            <div className="hub-service-redeem__card">
                <div className="hub-service-redeem__card-header hub-service-redeem__card-header--compact">
                    <div className="hub-service-redeem__title-group">
                        <h3 className="hub-service-redeem__title">{t("Authorization Breakdown", "授权明细")}</h3>
                    </div>
                    <div className="hub-service-redeem__model-summary">
                        <span className="hub-service-redeem__label">{t("Available Models", "可用模型")}</span>
                        <span className="hub-service-redeem__value">{availableModels.length ? availableModels.join(", ") : "auto"}</span>
                    </div>
                </div>

                {/* Grant details include active, queued, period-limited, and exhausted grants. */}
                <div className="hub-service-redeem__section">
                    <div className="hub-service-redeem__label">{t("Grant credit details", "授权额度明细", "授權額度明細")}</div>
                    {grantsForDetails.length ? (
                        <table className="hub-service-redeem__detail-table">
                            <thead>
                                <tr>
                                    <th scope="col">{t("Service Group", "服务组")}</th>
                                    <th scope="col">{t("Source", "来源")}</th>
                                    <th scope="col">{t("Starts At", "开始时间")}</th>
                                    <th scope="col">{t("Expires At", "到期时间")}</th>
                                    <th scope="col">{t("Total", "总额")}</th>
                                    <th scope="col">{t("Used", "已用")}</th>
                                    <th scope="col">{t("Remaining", "剩余")}</th>
                                    <th scope="col">{t("Status", "状态")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {grantsForDetails.map((grant, index) => (
                                    <tr key={(grant.service_group_id || "") + "-" + index}>
                                        <td><span className="hub-service-redeem__strong">{grant.service_group_id || "-"}</span></td>
                                        <td>{formatGrantSource(grant, t)}</td>
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
                        <div className="hub-service-redeem__empty">
                            {isActiveUnmeteredService
                                ? t("Free service does not require grant credit details.", "免费服务无需授权额度明细。", "免費服務無需授權額度明細。")
                                : t("No grant credit details", "暂无授权额度明细", "暫無授權額度明細")}
                        </div>
                    )}
                </div>

                {/* Authorized Models table */}
                <div className="hub-service-redeem__section hub-service-redeem__section--large">
                    <div className="hub-service-redeem__label hub-service-redeem__label--spaced">{t("Authorized Models", "授权模型列表")}</div>
                    {authorizedModelsForDisplay.length ? (
                        <div className="hub-service-redeem__models-wrap">
                            <table className="hub-service-redeem__detail-table hub-service-redeem__models-table">
                                <colgroup>
                                    <col className="hub-service-redeem__model-col" />
                                    <col className="hub-service-redeem__group-col" />
                                </colgroup>
                                <thead>
                                    <tr>
                                        <th scope="col">{t("Model", "模型")}</th>
                                        <th scope="col">{t("Service Groups", "服务组")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {authorizedModelsForDisplay.map((model) => {
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
