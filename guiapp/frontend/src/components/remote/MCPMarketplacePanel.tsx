import { useCallback, useEffect, useMemo, useState } from "react";
import type { CSSProperties } from "react";
import {
    SyncHubManagedCapabilities,
    GetHubRecommendedCapabilities,
    GetHubCapability,
    ListHubCapabilities,
    InstallHubCapability,
    RequestHubCapabilityInstallIntent,
    LoadConfig,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface HubCapabilityRecommendation {
    id: string;
    capability_ref: string;
    capability_version_key?: string;
    recommendation_reason?: string;
}

interface HubCapabilitySummary {
    external?: boolean;
    id: string;
    capability_type?: string;
    capability_id?: string;
    display_name?: string;
    description?: string;
    source?: string;
    status?: string;
    global_key?: string;
    current_version_key?: string;
    metadata_json?: string;
}

type RecommendationView = HubCapabilityRecommendation & {
    capability?: HubCapabilitySummary;
};

type CapabilityMarketPolicyView = {
    enterprise_only_search?: boolean;
    enterprise_only_install?: boolean;
    view_mode?: string;
};

type Props = {
    translate: (key: string) => string;
    onChanged: (status?: any) => Promise<void> | void;
    installedCapabilities?: string[];
};

export function MCPMarketplacePanel({ translate, onChanged, installedCapabilities = [] }: Props) {
    const [recommendations, setRecommendations] = useState<RecommendationView[]>([]);
    const [catalog, setCatalog] = useState<HubCapabilitySummary[]>([]);
    const [query, setQuery] = useState("");
    const [busy, setBusy] = useState(false);
    const [searching, setSearching] = useState(false);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const [policy, setPolicy] = useState<CapabilityMarketPolicyView | null>(null);
    const installedSet = useMemo(() => new Set(installedCapabilities.filter(Boolean)), [installedCapabilities]);

    const loadPolicy = useCallback(async () => {
        try {
            const cfg = await LoadConfig();
            const raw = cfg as any;
            const next = raw?.capability_market_policy || raw?.CapabilityMarketPolicy || null;
            setPolicy(next && typeof next === "object" ? next : null);
        } catch {
            setPolicy(null);
        }
    }, []);

    const loadRecommendations = useCallback(async () => {
        try {
            const items = await GetHubRecommendedCapabilities();
            if (!Array.isArray(items) || items.length === 0) {
                setRecommendations([]);
                return;
            }
            const enriched: RecommendationView[] = await Promise.all(items.map(async (item: HubCapabilityRecommendation): Promise<RecommendationView> => {
                try {
                    const capability = await GetHubCapability(item.capability_ref);
                    return { ...item, capability };
                } catch {
                    return item;
                }
            }));
            setRecommendations(enriched.filter((item) => (item.capability?.capability_type || "mcp") === "mcp"));
        } catch {
            setRecommendations([]);
        }
    }, []);

    const searchCatalog = useCallback(async (keyword: string) => {
        setSearching(true);
        setError("");
        try {
            const items = await ListHubCapabilities("mcp", keyword);
            setCatalog(Array.isArray(items) ? items : []);
        } catch (err) {
            setCatalog([]);
            setError(String(err));
        } finally {
            setSearching(false);
        }
    }, []);

    useEffect(() => {
        loadPolicy();
        loadRecommendations();
        searchCatalog("");
    }, [loadPolicy, loadRecommendations, searchCatalog]);

    const syncRequired = async () => {
        setBusy(true);
        setError("");
        setMessage("");
        try {
            const status = await SyncHubManagedCapabilities();
            setMessage(syncStatusText(status, translate));
            await onChanged(status);
            await loadPolicy();
            await loadRecommendations();
            await searchCatalog(query);
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const installCapability = async (capabilityRef: string, capability?: HubCapabilitySummary) => {
        setBusy(true);
        setError("");
        setMessage("");
        try {
            if (capability?.external) {
                const intent = await RequestHubCapabilityInstallIntent({
                    capability_id: capability.capability_id || capability.id || capabilityRef,
                    capability_type: capability.capability_type || "mcp",
                    version: capability.current_version_key || "",
                    source: capability.source || "hubcenter",
                    pricing: capabilityPricing(capability),
                    price: capabilityPrice(capability),
                    license: capabilityLicense(capability),
                    user_reason: "maclaw_mcp_marketplace_install",
                });
                if (intent?.capability?.id) {
                    const status = await InstallHubCapability(intent.capability.id);
                    setMessage(syncStatusText(status, translate));
                    await onChanged(status);
                } else {
                    setMessage(installIntentText(intent, translate));
                    await onChanged(intent);
                }
            } else {
                const status = await InstallHubCapability(capabilityRef);
                setMessage(syncStatusText(status, translate));
                await onChanged(status);
            }
            await loadPolicy();
            await loadRecommendations();
            await searchCatalog(query);
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const visibleCatalog = catalog.slice(0, 8);

    return (
        <div style={panelStyle}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "8px" }}>
                <span style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text }}>{translate("mcpMarketplace")}</span>
                <button className="btn-secondary" style={smallBtnStyle} onClick={syncRequired} disabled={busy || searching}>
                    {busy ? translate("mcpMarketplaceSyncing") : translate("mcpMarketplaceSync")}
                </button>
            </div>
            <div style={policyRowStyle}>
                <span style={policyBadgeStyle}>{translate(effectiveEnterpriseOnlySearch(policy) ? "mcpMarketplaceSearchHubOnly" : "mcpMarketplaceSearchMerged")}</span>
                <span style={policyBadgeStyle}>{translate(effectiveEnterpriseOnlyInstall(policy) ? "mcpMarketplaceInstallHubOnly" : "mcpMarketplaceInstallExternalAllowed")}</span>
            </div>
            <div style={searchRowStyle}>
                <input
                    className="form-input"
                    style={{ height: "30px", fontSize: "0.72rem", flex: 1 }}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter") searchCatalog(query); }}
                    placeholder={translate("mcpMarketplaceSearchPlaceholder")}
                    spellCheck={false}
                />
                <button className="btn-secondary" style={smallBtnStyle} onClick={() => searchCatalog(query)} disabled={busy || searching}>
                    {searching ? translate("mcpMarketplaceSearching") : translate("mcpMarketplaceSearch")}
                </button>
            </div>
            {error && <div style={{ fontSize: "0.72rem", color: colors.danger }}>{error}</div>}
            {message && <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>{message}</div>}
            {recommendations.length > 0 && (
                <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                    {recommendations.slice(0, 3).map((item) => {
                        const ref = item.capability_ref;
                        const installed = isCapabilityInstalled(installedSet, item.capability, ref);
                        return (
                            <CapabilityRow
                                key={item.id}
                                title={`${translate("mcpRecommended")}: ${capabilityTitle(item.capability, ref)}`}
                                meta={`${capabilityMeta(item.capability, ref)}${item.recommendation_reason ? ` - ${item.recommendation_reason}` : ""}`}
                                installed={installed}
                                busy={busy}
                                translate={translate}
                                onInstall={() => installCapability(ref, item.capability)}
                            />
                        );
                    })}
                </div>
            )}
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                {visibleCatalog.map((item) => {
                    const installed = isCapabilityInstalled(installedSet, item, item.id);
                    return (
                        <CapabilityRow
                            key={item.id}
                            title={capabilityTitle(item, item.id)}
                            meta={capabilityMeta(item, item.id)}
                            installed={installed}
                            busy={busy}
                            translate={translate}
                            onInstall={() => installCapability(item.id, item)}
                        />
                    );
                })}
                {!searching && visibleCatalog.length === 0 && (
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{translate("mcpMarketplaceNoResults")}</div>
                )}
            </div>
        </div>
    );
}

function CapabilityRow({ title, meta, installed, busy, translate, onInstall }: {
    title: string;
    meta: string;
    installed: boolean;
    busy: boolean;
    translate: (key: string) => string;
    onInstall: () => void;
}) {
    return (
        <div style={recommendationStyle}>
            <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: "2px", textAlign: "left", flex: "1 1 auto" }}>
                <span style={capabilityTitleTextStyle}>{title}</span>
                <span style={capabilityMetaTextStyle}>{meta}</span>
            </div>
            {installed ? (
                <span style={installedBadgeStyle}>{translate("mcpMarketplaceInstalledState")}</span>
            ) : (
                <button className="btn-primary" style={{ ...smallBtnStyle, flexShrink: 0 }} onClick={onInstall} disabled={busy}>
                    {translate("mcpInstallRecommended")}
                </button>
            )}
        </div>
    );
}

function isCapabilityInstalled(installedSet: Set<string>, item: HubCapabilitySummary | undefined, fallback = ""): boolean {
    const keys = [
        fallback,
        item?.id,
        item?.capability_id,
        item?.global_key,
    ];
    return keys.some((key) => !!key && installedSet.has(key));
}
function effectiveEnterpriseOnlySearch(policy: CapabilityMarketPolicyView | null): boolean {
    return policy?.enterprise_only_search === true;
}

function effectiveEnterpriseOnlyInstall(policy: CapabilityMarketPolicyView | null): boolean {
    return policy?.enterprise_only_install === true;
}

function capabilityTitle(item: HubCapabilitySummary | undefined, fallback: string): string {
    return item?.display_name || item?.capability_id || fallback;
}

function capabilityMeta(item: HubCapabilitySummary | undefined, fallback: string): string {
    const type = item?.capability_type || "mcp";
    const source = item?.source || "hub";
    const version = item?.current_version_key || "";
    const pricing = capabilityPricing(item);
    const desc = item?.description || fallback;
    return [type, source, pricing, version, desc].filter(Boolean).join(" / ");
}

function capabilityMetadata(item: HubCapabilitySummary | undefined): Record<string, any> {
    if (!item?.metadata_json) return {};
    try {
        const parsed = JSON.parse(item.metadata_json);
        return parsed && typeof parsed === "object" ? parsed : {};
    } catch {
        return {};
    }
}

function capabilityPricing(item: HubCapabilitySummary | undefined): string {
    const meta = capabilityMetadata(item);
    const pricing = meta.pricing;
    if (typeof pricing === "string") return pricing;
    if (pricing && typeof pricing === "object" && typeof pricing.mode === "string") return pricing.mode;
    if (typeof meta.pricing_type === "string") return meta.pricing_type;
    return "free";
}

function capabilityPrice(item: HubCapabilitySummary | undefined): Record<string, any> | undefined {
    const pricing = capabilityMetadata(item).pricing;
    return pricing && typeof pricing === "object" ? pricing : undefined;
}

function capabilityLicense(item: HubCapabilitySummary | undefined): Record<string, any> | undefined {
    const license = capabilityMetadata(item).license;
    return license && typeof license === "object" ? license : undefined;
}

function installIntentText(intent: any, translate: (key: string) => string): string {
    if (!intent || typeof intent !== "object") return translate("mcpMarketplaceRequestSubmitted");
    if (intent.action === "install_external_direct") {
        if (intent.capability?.id) return translate("mcpMarketplaceReadyToInstall");
        return `${translate("mcpMarketplaceNeedsAttention")}: ${intent.reason || "missing_capability"}`;
    }
    if (intent.action === "create_purchase_request") return `${translate("mcpMarketplacePurchaseRequested")}${intent.request_id ? `: ${intent.request_id}` : ""}`;
    if (intent.action === "create_import_request") return `${translate("mcpMarketplaceImportRequested")}${intent.request_id ? `: ${intent.request_id}` : ""}`;
    if (intent.action === "blocked") return `${translate("mcpMarketplaceNeedsAttention")}: ${intent.reason || "blocked"}`;
    return `${translate("mcpMarketplaceRequestSubmitted")}${intent.request_id ? `: ${intent.request_id}` : ""}`;
}
function syncStatusText(status: any, translate: (key: string) => string): string {
    if (!status || typeof status !== "object") {
        return translate("mcpMarketplaceDone");
    }
    const installed = Number(status.managed_installed || 0);
    const updated = Number(status.updated || 0);
    const needsConfig = Array.isArray(status.needs_user_config) ? status.needs_user_config.length : 0;
    const errors = Array.isArray(status.errors) ? status.errors.length : 0;
    if (errors > 0) {
        return `${translate("mcpMarketplaceNeedsAttention")}: ${status.errors.join("; ")}`;
    }
    return `${translate("mcpMarketplaceDone")}: ${translate("mcpMarketplaceInstalled")} ${installed}, ${translate("mcpMarketplaceUpdated")} ${updated}, ${translate("mcpMarketplaceNeedsConfig")} ${needsConfig}`;
}

const panelStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    padding: "8px",
    background: colors.surfaceMuted,
    display: "flex",
    flexDirection: "column",
    gap: "6px",
    textAlign: "left",
};

const policyRowStyle: CSSProperties = {
    display: "flex",
    alignItems: "flex-start",
    flexWrap: "wrap",
    gap: "6px",
};

const policyBadgeStyle: CSSProperties = {
    color: colors.textSecondary,
    border: `1px solid ${colors.border}`,
    borderRadius: "999px",
    padding: "2px 8px",
    fontSize: "0.68rem",
};

const searchRowStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "6px",
};

const recommendationStyle: CSSProperties = {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "flex-start",
    gap: "8px",
    fontSize: "0.72rem",
    color: colors.textSecondary,
    textAlign: "left",
};

const capabilityMetaTextStyle: CSSProperties = {
    textAlign: "left",
    lineHeight: 1.45,
    display: "-webkit-box",
    WebkitLineClamp: 2,
    WebkitBoxOrient: "vertical",
    overflow: "hidden",
    overflowWrap: "anywhere",
};

const capabilityTitleTextStyle: CSSProperties = {
    color: colors.text,
    textAlign: "left",
    lineHeight: 1.35,
    display: "-webkit-box",
    WebkitLineClamp: 2,
    WebkitBoxOrient: "vertical",
    overflow: "hidden",
    overflowWrap: "anywhere",
};

const installedBadgeStyle: CSSProperties = {
    color: colors.textSecondary,
    border: `1px solid ${colors.border}`,
    borderRadius: "999px",
    padding: "2px 8px",
    fontSize: "0.68rem",
    flexShrink: 0,
    whiteSpace: "nowrap",
};

const smallBtnStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};
