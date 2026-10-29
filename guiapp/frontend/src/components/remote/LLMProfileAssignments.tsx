import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { FetchMaclawLLMProfileModels, GetMaclawLLMProfilePanelState, SaveMaclawLLMProfiles, TestMaclawLLMProfile } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { inputStyle, labelStyle } from "./LLMConfigPanelShared";
import { SuggestCombobox } from "../ui/SuggestCombobox";
import { capabilityBandName, hubOfficialModelAliases, officialModelAlias } from "../../utils/capabilityModelLabel";
import { panelText } from "../../i18n/mnPanel";

type Profile = { provider_id?: string; model?: string; inherit_assistant?: boolean };
type RoleAssignment = { model?: string; inherit_hub?: boolean };
type RoleKey = "foreground" | "background" | "computer_use" | "grounder";
type Provider = { id: string; name: string; model?: string; models?: string[]; connection_test_passed?: boolean; is_hub_service?: boolean; supports_vision?: boolean; vision_models?: string[]; vision_tested_models?: string[] };
type Summary = { provider_id?: string; provider_name?: string; model?: string; inherit_assistant?: boolean; health?: string };
type RoleSummary = { model?: string; inherit_hub?: boolean; provider_id?: string; provider_name?: string; configured?: boolean };
type ProbeResult = {
    profile: "assistant" | "coding" | "caption";
    health?: string;
    Health?: string;
    reason_code?: string;
    provider_id?: string;
    ProviderID?: string;
    model?: string;
    Model?: string;
    supports_vision?: boolean;
    SupportsVision?: boolean;
    vision_probe_status?: string;
    VisionProbeStatus?: string;
    vision_persist_failed?: boolean;
    VisionPersistFailed?: boolean;
};
type PanelState = {
    providers: Provider[];
    profiles: {
        version: number;
        assistant: Profile;
        coding: Profile;
        caption?: Profile;
        foreground?: RoleAssignment;
        background?: RoleAssignment;
        computer_use?: RoleAssignment;
        // Optional grounding model for Computer Use: turns a target described
        // in words into a click point. Unset (inherit_hub) means not used.
        grounder?: RoleAssignment;
        horizon?: unknown;
    };
    hub_role_models?: { foreground?: string; background?: string; computer_use?: string };
    assistant: Summary;
    coding: Summary;
    caption?: Summary;
    foreground?: RoleSummary;
    background?: RoleSummary;
    computer_use?: RoleSummary;
    grounder?: RoleSummary;
    revision: string;
};

type Props = {
    lang?: string;
    onSaved?: () => void;
    // Provider management owns the connection-test workflow. This revision is
    // bumped by the parent after a successful Test & Save so this independent
    // assignment read model refreshes even if a Wails event was missed while
    // the provider dialog was open.
    providerListRevision?: number;
    // Optional control rendered immediately after the assignment help text.
    // The parent uses this for "Import other agents" so the action stays
    // available while this panel is still loading or failed to load.
    descriptionAction?: ReactNode;
};

const cloneProfiles = (profiles: PanelState["profiles"]): PanelState["profiles"] => ({
    ...profiles,
    version: profiles.version,
    assistant: { ...profiles.assistant },
    coding: { ...profiles.coding },
    caption: { ...(profiles.caption || {}) },
    foreground: { ...(profiles.foreground || {}) },
    background: { ...(profiles.background || {}) },
    computer_use: { ...(profiles.computer_use || {}) },
    grounder: { ...(profiles.grounder || {}) },
});

const roleInheritsHub = (role?: RoleAssignment) => role?.inherit_hub !== false;

export function captionModelMissingVision(provider?: Provider, model?: string): boolean {
    if (!String(model || "").trim()) return false;
    return modelVisionStatus(provider, model) !== "supported";
}

export function modelVisionStatus(provider?: Provider, model?: string): "supported" | "unsupported" | "untested" {
    if (!provider) return "untested";
    const selected = String(model || "").trim();
    if (!selected) return "untested";
    const want = selected.toLowerCase();
    const visionModels = (provider.vision_models || []).map(value => String(value || "").trim()).filter(Boolean);
    if (visionModels.some(value => value.toLowerCase() === want)) return "supported";
    if (visionModels.length === 0 && provider.supports_vision === true) {
        const fallback = String(provider.model || "").trim();
        if (fallback && fallback.toLowerCase() === want) return "supported";
    }
    if (provider.is_hub_service) return "untested";
    const tested = (provider.vision_tested_models || []).map(value => String(value || "").trim()).filter(Boolean);
    if (tested.some(value => value.toLowerCase() === want)) return "unsupported";
    if (provider.connection_test_passed && String(provider.model || "").trim().toLowerCase() === want) {
        return "unsupported";
    }
    return "untested";
}

function probeHealth(result: ProbeResult): string {
    return result.health || result.Health || "";
}

// Optional parameter, matching probeVisionPersistFailed below: callers hold a
// possibly-absent probe, and the non-null `probe || {}` fallback they used to
// need here did not satisfy ProbeResult.
function probeVisionStatus(result?: ProbeResult): string | undefined {
    return result?.vision_probe_status || result?.VisionProbeStatus;
}

function probeVisionPersistFailed(result?: ProbeResult): boolean {
    return !!(result?.vision_persist_failed || result?.VisionPersistFailed);
}

function shownVisionStatus(stored: "supported" | "unsupported" | "untested", probe?: ProbeResult): string {
    if (stored === "supported" || stored === "unsupported") return stored;
    if (probeVisionPersistFailed(probe)) return "inconclusive";
    return probeVisionStatus(probe) || stored;
}

function providerLookupKey(id?: string): string {
    return String(id || "").trim().toLowerCase();
}

function applyVisionProbeToProvider(provider: Provider, model: string, status?: string): Provider {
    if (status !== "supported" && status !== "unsupported") return provider;
    const selected = String(model || "").trim();
    if (!selected) return provider;
    const want = selected.toLowerCase();
    const keep = (values: string[] | undefined) => (values || []).map(value => String(value || "").trim()).filter(Boolean);
    const replaceFold = (values: string[], value: string) => [...values.filter(item => item.toLowerCase() !== want), value];
    const tested = replaceFold(keep(provider.vision_tested_models), selected);
    const others = keep(provider.vision_models).filter(value => value.toLowerCase() !== want);
    const supported = status === "supported";
    const fallback = String(provider.model || "").trim();
    return {
        ...provider,
        vision_tested_models: tested,
        vision_models: supported ? [...others, selected] : others,
        supports_vision: fallback.toLowerCase() === want ? supported : provider.supports_vision,
    };
}

export function LLMProfileAssignments({ lang, onSaved, providerListRevision = 0, descriptionAction }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant = zhHans) =>
        panelText(lang ?? "", en, zhHans, zhHant), [lang]);
    const [state, setState] = useState<PanelState | null>(null);
    const [draft, setDraft] = useState<PanelState["profiles"] | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState("");
    const [testingProfile, setTestingProfile] = useState<"assistant" | "coding" | "caption" | null>(null);
    const [probeResults, setProbeResults] = useState<Partial<Record<"assistant" | "coding" | "caption", ProbeResult>>>({});
    const [catalogByProvider, setCatalogByProvider] = useState<Record<string, string[]>>({});
    const catalogFetchSeqRef = useRef<Record<string, number>>({});
    const dirtyRef = useRef(false);
    const loadGenerationRef = useRef(0);
    // Eligibility refresh shares loadGenerationRef so a late snapshot cannot
    // overwrite a newer read. It must not also cancel the spinner: the panel
    // read itself emits a provider refresh, and that used to leave
    // "正在加载模型分配…" up after the data had already arrived.
    const loadingGenerationRef = useRef(0);
    const eligibilityRefreshQueuedRef = useRef(false);
    const eligibilityRefreshRunningRef = useRef(false);
    const eligibilityRefreshRerunRef = useRef(false);
    const eligibilityRefreshCancelledRef = useRef(false);
    // A probe is asynchronous while its draft remains editable. Bump the
    // generation whenever a selection changes so a late response cannot label
    // a newer provider/model as connected (or unavailable).
    const probeGenerationRef = useRef<Record<"assistant" | "coding" | "caption", number>>({ assistant: 0, coding: 0, caption: 0 });
    const invalidateProbeResults = (...profiles: Array<"assistant" | "coding" | "caption">) => {
        for (const profile of profiles) probeGenerationRef.current[profile] += 1;
        setProbeResults(prev => {
            const next = { ...prev };
            for (const profile of profiles) delete next[profile];
            return next;
        });
    };

    const load = useCallback(async () => {
        const generation = ++loadGenerationRef.current;
        const loadingGeneration = ++loadingGenerationRef.current;
        setLoading(true);
        setError("");
        try {
            const next = await GetMaclawLLMProfilePanelState() as unknown as PanelState;
            if (generation !== loadGenerationRef.current) return;
            setState(next);
            setDraft(cloneProfiles(next.profiles));
            setCatalogByProvider({});
            invalidateProbeResults("assistant", "coding", "caption");
        } catch (err) {
            if (generation !== loadGenerationRef.current) return;
            setError(String(err));
        } finally {
            if (loadingGeneration === loadingGenerationRef.current) setLoading(false);
        }
    }, []);

    useEffect(() => { void load(); }, [load]);

    const refreshEligibleProviders = useCallback(async (): Promise<void> => {
        const generation = ++loadGenerationRef.current;
        try {
            const next = await GetMaclawLLMProfilePanelState() as unknown as PanelState;
            if (generation !== loadGenerationRef.current) return;

            if (dirtyRef.current) {
                // A successful Provider management test changes eligibility,
                // not the user's assignment. Replace only the directory and
                // revision so the new provider is selectable without losing a
                // draft that is already being edited in this panel.
                setState(previous => previous ? { ...next, profiles: previous.profiles } : next);
                return;
            }
            setState(next);
            setDraft(cloneProfiles(next.profiles));
            setCatalogByProvider({});
            invalidateProbeResults("assistant", "coding", "caption");
        } catch {
            // The existing state remains usable. A later profile-change event
            // or an explicit refresh will retry the read.
        }
    }, []);

    const scheduleEligibleProviderRefresh = useCallback(() => {
        // Test & Save invokes both the direct parent callback and the backend
        // event. Coalesce same-turn signals, but remember a distinct update
        // that arrives while a read is already in flight; otherwise it could
        // be lost if the backend snapshot was taken before that later save.
        if (eligibilityRefreshRunningRef.current) {
            eligibilityRefreshRerunRef.current = true;
            return;
        }
        if (eligibilityRefreshQueuedRef.current) return;
        eligibilityRefreshQueuedRef.current = true;
        queueMicrotask(() => {
            eligibilityRefreshQueuedRef.current = false;
            if (eligibilityRefreshCancelledRef.current) {
                eligibilityRefreshCancelledRef.current = false;
                return;
            }
            eligibilityRefreshRunningRef.current = true;
            void refreshEligibleProviders().finally(() => {
                eligibilityRefreshRunningRef.current = false;
                if (eligibilityRefreshRerunRef.current) {
                    eligibilityRefreshRerunRef.current = false;
                    scheduleEligibleProviderRefresh();
                }
            });
        });
    }, [refreshEligibleProviders]);

    useEffect(() => {
        // The initial load above already covers revision zero. Subsequent
        // revisions are authoritative post-save signals from Provider
        // management, not merely optimistic UI edits.
        if (providerListRevision > 0) scheduleEligibleProviderRefresh();
    }, [providerListRevision, scheduleEligibleProviderRefresh]);

    const refreshProvidersPreservingDraft = useCallback(() => {
        scheduleEligibleProviderRefresh();
    }, [scheduleEligibleProviderRefresh]);

    const providers = state?.providers || [];
    const providerByID = useMemo(() => {
        const map = new Map<string, Provider>();
        for (const provider of providers) {
            const key = providerLookupKey(provider.id);
            if (key) map.set(key, provider);
        }
        return map;
    }, [providers]);
    const assistant = draft?.assistant;
    const coding = draft?.coding;
    const caption = draft?.caption;
    const codingFollows = coding?.inherit_assistant === true;
    // While editing, "follow assistant" must reflect the unsaved assistant
    // draft, not the last persisted panel snapshot. Otherwise changing the
    // assistant provider/model makes coding appear to keep the old choice
    // until after Save + reload, which contradicts the follow relationship.
    const followingAssistantProvider = providerByID.get(providerLookupKey(assistant?.provider_id));
    const followingAssistantProviderName = followingAssistantProvider?.name || t("No provider", "未配置服务商");
    const followingAssistantModel = (followingAssistantProvider?.is_hub_service
        ? officialModelAlias(assistant?.model || "")
        : String(assistant?.model || "").trim()) || t("No model", "未配置模型");
    const followingPreviewPending = codingFollows && !!state && (
        state.profiles.coding.inherit_assistant !== true ||
        state.profiles.assistant.provider_id !== assistant?.provider_id ||
        state.profiles.assistant.model !== assistant?.model
    );
    const dirty = !!state && !!draft && JSON.stringify(cloneProfiles(state.profiles)) !== JSON.stringify(draft);
    dirtyRef.current = dirty;

    useEffect(() => {
        const onProfilesChanged = (payload?: { changed?: string }) => {
            if (payload?.changed === "providers" || payload?.changed === "hub-provider") {
                refreshProvidersPreservingDraft();
                return;
            }
            // A real assignment change supersedes a queued provider-only
            // refresh from the same event turn. Without this cancellation the
            // delayed directory read could invalidate the newer assignment
            // read before it applies.
            eligibilityRefreshCancelledRef.current = true;
            // Do not overwrite an unsaved assignment draft from the bottom
            // picker or another settings window. Instead make the conflict
            // explicit and let the user refresh intentionally.
            if (dirtyRef.current) {
                setError(t("Model assignments changed elsewhere. Refresh before saving.", "模型分配已在其他位置更新，请刷新后再保存。"));
                return;
            }
            void load();
        };
        const cleanup = EventsOn("llm-profiles-changed", onProfilesChanged);
        return () => { if (typeof cleanup === "function") cleanup(); else EventsOff("llm-profiles-changed"); };
    }, [load, refreshProvidersPreservingDraft, t]);

    useEffect(() => () => {
        loadGenerationRef.current += 1;
        catalogFetchSeqRef.current = {};
        eligibilityRefreshQueuedRef.current = false;
        eligibilityRefreshRunningRef.current = false;
        eligibilityRefreshRerunRef.current = false;
        eligibilityRefreshCancelledRef.current = true;
    }, []);

    useEffect(() => {
        const onBeforeUnload = (event: BeforeUnloadEvent) => {
            if (!dirtyRef.current) return;
            event.preventDefault();
            event.returnValue = "";
        };
        window.addEventListener("beforeunload", onBeforeUnload);
        return () => window.removeEventListener("beforeunload", onBeforeUnload);
    }, []);

    const refreshProviderCatalog = useCallback(async (providerID: string) => {
        const id = String(providerID || "").trim();
        if (!id) return;
        const generation = (catalogFetchSeqRef.current[id] || 0) + 1;
        catalogFetchSeqRef.current[id] = generation;
        try {
            const items = await FetchMaclawLLMProfileModels(id) as Array<{ id?: string; ID?: string; name?: string; Name?: string }>;
            if (catalogFetchSeqRef.current[id] !== generation) return;
            const ids = (Array.isArray(items) ? items : [])
                .map(item => String(item?.id ?? item?.ID ?? "").trim())
                .filter(Boolean);
            if (ids.length === 0) return;
            setCatalogByProvider(prev => {
                const existing = prev[id] || [];
                return { ...prev, [id]: Array.from(new Set([...ids, ...existing])) };
            });
        } catch {
            // The persisted provider.models catalog remains the assignment list.
        }
    }, []);

    useEffect(() => {
        const ids = [
            assistant?.provider_id,
            codingFollows ? "" : coding?.provider_id,
            caption?.provider_id,
        ].map(value => String(value || "").trim()).filter(Boolean);
        for (const id of Array.from(new Set(ids))) {
            void refreshProviderCatalog(id);
        }
    }, [assistant?.provider_id, caption?.provider_id, coding?.provider_id, codingFollows, refreshProviderCatalog]);

    const providerModels = (providerID?: string) => {
        const id = String(providerID || "").trim();
        const provider = providerByID.get(providerLookupKey(id));
        if (!provider) return [];
        // MaClaw Official's model names are the four billing aliases. The live
        // /models catalog is an upstream id list and must not replace them.
        if (provider.is_hub_service) return hubOfficialModelAliases();
        const options = [
            ...(catalogByProvider[id] || []),
            provider.model,
            ...(provider.models || []),
        ].map(value => String(value || "").trim()).filter(Boolean);
        return Array.from(new Set(options));
    };
    const modelFieldValue = (providerID: string | undefined, model: string | undefined) => {
        const raw = String(model || "");
        const provider = providerByID.get(providerLookupKey(providerID));
        return provider?.is_hub_service ? officialModelAlias(raw) : raw;
    };
    const setProvider = (profile: "assistant" | "coding" | "caption", providerID: string) => {
        const models = providerModels(providerID);
        const provider = providerByID.get(providerLookupKey(providerID));
        const preferredRaw = String(provider?.model || "").trim();
        const preferred = provider?.is_hub_service
            ? (capabilityBandName(preferredRaw) || hubOfficialModelAliases()[0])
            : preferredRaw;
        const preferredMatch = preferred
            ? models.find(model => model.toLowerCase() === preferred.toLowerCase())
            : "";
        // The live catalog is prepended and starts with aliases such as
        // default-model. Keep the provider's saved model instead of that alias.
        const nextModel = providerID ? (preferredMatch || models[0] || "") : "";
        setDraft(prev => prev ? ({
            ...prev,
            [profile]: { ...prev[profile], provider_id: providerID, model: nextModel },
        }) : prev);
        invalidateProbeResults(profile, ...(profile === "assistant" && codingFollows ? ["coding" as const] : []));
    };
    const setModel = (profile: "assistant" | "coding" | "caption", model: string) => {
        setDraft(prev => prev ? ({ ...prev, [profile]: { ...prev[profile], model } }) : prev);
        invalidateProbeResults(profile, ...(profile === "assistant" && codingFollows ? ["coding" as const] : []));
    };
    const setCodingFollows = (inherit: boolean) => {
        setDraft(prev => {
            if (!prev) return prev;
            const current = { ...prev.coding, inherit_assistant: inherit };
            if (!inherit && (!current.provider_id || !current.model)) {
                current.provider_id = prev.assistant.provider_id;
                current.model = prev.assistant.model;
            }
            return { ...prev, coding: current };
        });
        invalidateProbeResults("coding");
    };
    const setRoleInherit = (role: RoleKey, inherit: boolean) => {
        setDraft(prev => {
            if (!prev) return prev;
            const current = { ...(prev[role] || {}) };
            current.inherit_hub = inherit;
            if (!inherit && !String(current.model || "").trim()) {
                const hub = state?.hub_role_models;
                const hubModel = role === "grounder" ? "" : role === "computer_use" ? hub?.computer_use : hub?.[role];
                current.model = String(hubModel || "auto");
            }
            return { ...prev, [role]: current };
        });
    };
    const setRoleModel = (role: RoleKey, model: string) => {
        setDraft(prev => prev ? { ...prev, [role]: { ...(prev[role] || {}), inherit_hub: false, model } } : prev);
    };
    const save = async () => {
        if (!state || !draft || saving) return;
        setSaving(true);
        setError("");
        try {
            await SaveMaclawLLMProfiles(draft as any, state.revision);
            await load();
            onSaved?.();
        } catch (err) {
            setError(String(err));
        } finally {
            setSaving(false);
        }
    };
    const refreshDraft = () => {
        if (saving) return;
        void load();
    };
    const probeLabel = (result?: ProbeResult) => {
        if (!result) return "";
        const health = probeHealth(result);
        if (health === "configured") return t("Connected", "已连接");
        if (health === "unavailable") return t("Unavailable", "不可用");
        if (health === "invalid") return t("Invalid configuration", "配置无效");
        return t("Unverified — try again", "未验证，请重试");
    };
    const testProfile = async (profile: "assistant" | "coding" | "caption") => {
        if (!draft || testingProfile) return;
        const value = profile === "assistant" ? draft.assistant : profile === "caption" ? (draft.caption || {}) : draft.coding;
        // Following coding is a display alias: test assistant once instead of
        // silently probing a stale independent recovery draft.
        const effectiveProfile = profile === "coding" && codingFollows ? "assistant" : profile;
        const effectiveValue = effectiveProfile === "assistant" ? draft.assistant : value;
        const generation = probeGenerationRef.current[profile];
        setTestingProfile(profile);
        setError("");
        try {
            const result = await TestMaclawLLMProfile(effectiveProfile, effectiveValue.provider_id || "", effectiveValue.model || "") as unknown as ProbeResult;
            if (probeGenerationRef.current[profile] === generation) {
                const health = probeHealth(result);
                const visionStatus = probeVisionStatus(result);
                const persistFailed = probeVisionPersistFailed(result);
                setProbeResults(prev => ({ ...prev, [profile]: { ...result, profile, health, vision_probe_status: visionStatus, vision_persist_failed: persistFailed } }));
                const providerID = String(result.provider_id || result.ProviderID || effectiveValue.provider_id || "").trim();
                const model = String(result.model || result.Model || effectiveValue.model || "").trim();
                if (persistFailed) {
                    setError(t("The image-support result could not be saved. Test it again.", "图片能力检测结果未能保存。请再测一次。"));
                } else if (health === "configured" && providerID && model) {
                    const providerKey = providerLookupKey(providerID);
                    const previousModel = String(effectiveValue.model || "").trim();
                    setState(prev => prev ? {
                        ...prev,
                        providers: prev.providers.map(provider => providerLookupKey(provider.id) === providerKey
                            ? applyVisionProbeToProvider(provider, model, visionStatus)
                            : provider),
                    } : prev);
                    if (previousModel && previousModel.toLowerCase() !== model.toLowerCase()) {
                        setDraft(prev => {
                            if (!prev) return prev;
                            const next = cloneProfiles(prev);
                            const previousKey = previousModel.toLowerCase();
                            (["assistant", "coding", "caption"] as const).forEach(key => {
                                const row = next[key];
                                if (!row) return;
                                if (providerLookupKey(row.provider_id) === providerKey &&
                                    String(row.model || "").trim().toLowerCase() === previousKey) {
                                    next[key] = { ...row, model };
                                }
                            });
                            return next;
                        });
                    }
                }
            }
        } catch {
            if (probeGenerationRef.current[profile] === generation) {
                setProbeResults(prev => ({ ...prev, [profile]: { profile, health: "unverified", reason_code: "probe_retryable" } }));
            }
        } finally {
            setTestingProfile(null);
        }
    };

    const selectorAria = (profile: "assistant" | "coding" | "caption", kind: "provider" | "model") => {
        if (profile === "assistant") return kind === "provider" ? t("Assistant provider", "普通 AI 助手服务商") : t("Assistant model", "普通 AI 助手模型");
        if (profile === "caption") return kind === "provider" ? t("Caption provider", "Caption 服务商") : t("Caption model", "Caption 模型");
        return kind === "provider" ? t("Coding provider", "编程 Agent 服务商") : t("Coding model", "编程 Agent 模型");
    };
    const renderSelectors = (profile: "assistant" | "coding" | "caption", value: Profile) => (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 180px), 1fr))", gap: 10, alignItems: "end" }}>
            <div>
                <label style={labelStyle}>{t("Provider", "服务商")}</label>
                <select aria-label={selectorAria(profile, "provider")}
                    value={value.provider_id || ""} onChange={e => setProvider(profile, e.target.value)} style={{ ...inputStyle, cursor: "pointer" }}>
                    <option value="">{profile === "caption" ? t("Not used", "不使用") : t("Select provider", "选择服务商")}</option>
                    {providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name}</option>)}
                </select>
            </div>
            <div>
                <label style={labelStyle}>{t("Model", "模型")}</label>
                <SuggestCombobox
                    listboxId={`${profile}-profile-models`}
                    ariaLabel={selectorAria(profile, "model")}
                    value={modelFieldValue(value.provider_id, value.model)}
                    options={providerModels(value.provider_id)}
                    onChange={model => setModel(profile, model)}
                    placeholder={t("Select or enter model ID", "选择或输入模型 ID")}
                    disabled={profile === "caption" && !value.provider_id}
                    openOnEnter
                    toggleLabel={t("Show model list", "显示模型列表", "顯示模型列表")}
                    emptyText={t("No matching models", "没有匹配的模型", "沒有匹配的模型")}
                    inputStyle={{ ...inputStyle, opacity: profile === "caption" && !value.provider_id ? 0.6 : 1 }}
                />
            </div>
        </div>
    );
    const visionSelection = (profile: "assistant" | "coding" | "caption") => {
        const value = profile === "assistant" ? assistant
            : profile === "caption" ? caption
            : (codingFollows ? assistant : coding);
        const provider = providerByID.get(providerLookupKey(value?.provider_id));
        return { provider, model: String(value?.model || "").trim(), status: modelVisionStatus(provider, value?.model) };
    };
    const visionStatusLabel = (status: string, hub?: boolean) => {
        if (status === "supported") return t("Vision support: enabled", "图片理解：支持");
        if (status === "unsupported") return t("Vision support: disabled", "图片理解：不支持");
        if (hub) return t("Vision support: not verified locally", "图片理解：未在本地验证");
        if (status === "inconclusive") return t("Vision support: not confirmed; please retry", "图片理解：未确认，请重试");
        return t("Vision support: not tested", "图片理解：未测试");
    };
    const renderProbeAction = (profile: "assistant" | "coding" | "caption") => {
        if (profile === "coding" && codingFollows) return null;
        const { provider, model, status } = visionSelection(profile);
        const probe = probeResults[profile];
        const health = probe ? probeHealth(probe) : "";
        const shownVision = shownVisionStatus(status, probe);
        const canProbeVision = !!provider && !!model && !provider.is_hub_service && shownVision !== "supported" && shownVision !== "unsupported";
        const captionWarningVisible = profile === "caption" && !!caption?.provider_id && captionModelMissingVision(provider, model);
        const hideDuplicateVisionLine = captionWarningVisible && shownVision !== "inconclusive";
        const testDisabled = testingProfile !== null || (profile === "caption" && !(caption?.provider_id && caption?.model));
        return (
            <div className="llm-profile-probe">
                <div className="llm-profile-probe__row">
                    <button type="button" onClick={() => void testProfile(profile)} disabled={testDisabled}
                        className="llm-profile-probe-btn" aria-busy={testingProfile === profile || undefined}>
                        {testingProfile === profile ? t("Checking…", "正在检查…") : canProbeVision
                            ? t("Test connection and image support", "测试连接与图片能力")
                            : t("Test connection", "测试连接")}
                    </button>
                    {probe && <span role="status" className={"llm-profile-probe-status" + (health === "configured" ? " is-ok" : (health === "unavailable" || health === "invalid") ? " is-bad" : "")}>{probeLabel(probe)}</span>}
                </div>
                {provider && model && !hideDuplicateVisionLine ? <span role="status" className={"llm-profile-probe-status" + (shownVision === "supported" ? " is-ok" : "")}>{visionStatusLabel(shownVision, provider.is_hub_service)}</span> : null}
            </div>
        );
    };

    const roleModelOptions = () => {
        const hub = providers.find(provider => provider.is_hub_service);
        const base = hub ? hubOfficialModelAliases() : providerModels(draft?.assistant.provider_id);
        return Array.from(new Set(["auto", ...base].map(value => String(value || "").trim()).filter(Boolean)));
    };
    const header = (
        <div className="llm-profile-assignments__head">
            <h3 id="llm-profile-assignments-title" className="llm-profile-assignments__title">{t("Model assignments", "模型分配")}</h3>
            <div className="llm-profile-assignments__lede">
                <span>{t("Providers that passed a connection test, plus the provider currently in use. Connections and credentials are managed separately.", "此处显示已通过连接测试的服务商，以及当前正在使用的服务商；连接与凭据在服务商管理中维护。")}</span>
                {descriptionAction ? <span className="llm-profile-assignments__lede-action">{descriptionAction}</span> : null}
            </div>
        </div>
    );
    const wrapSection = (body: ReactNode) => (
        <div className="llm-config-card llm-profile-assignments" role="region" aria-labelledby="llm-profile-assignments-title">
            {header}
            {body}
        </div>
    );

    if (loading) return wrapSection(<div role="status" className="llm-profile-assignments__state">{t("Loading model assignments…", "正在加载模型分配…")}</div>);
    if (!state || !draft) return wrapSection(<div role="alert" className="llm-profile-assignments__state llm-profile-assignments__state--error">{error || t("Could not load model assignments.", "无法加载模型分配。")}</div>);

    const captionVision = visionSelection("caption");
    return wrapSection(<>
        {providers.length === 0 && <div role="status" className="llm-profile-assignments__state llm-profile-assignments__state--soft">
            {t("No eligible providers yet. Test and save a provider in Provider management, or keep using the current assistant provider.", "暂无可用服务商。请先在服务商管理中检测并保存，或继续使用当前助手服务商。")}
        </div>}

        <div style={{ display: "grid", gap: 14 }}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 280px), 1fr))", gap: 14, alignItems: "center" }}>
                <div style={{ minWidth: 0 }}><strong style={{ fontSize: "0.78rem", color: colors.text }}>{t("AI assistant", "普通 AI 助手")}</strong><p style={{ margin: "3px 0 0", color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.35 }}>{t("Chat, IM, and workflows", "聊天、IM 与工作流")}</p></div>
                <div style={{ minWidth: 0 }}>{renderSelectors("assistant", assistant || {})}{renderProbeAction("assistant")}</div>
            </div>
            <div style={{ borderTop: `1px solid ${colors.border}`, paddingTop: 14, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 280px), 1fr))", gap: 14, alignItems: "start" }}>
                <div style={{ minWidth: 0 }}><strong style={{ fontSize: "0.78rem", color: colors.text }}>{t("Coding Agent", "编程 Agent")}</strong><p style={{ margin: "3px 0 0", color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.35 }}>{t("Coding workbench and coding tasks", "编程工作台与编程任务")}</p></div>
                <div style={{ minWidth: 0 }}>
                    <label style={{ display: "inline-flex", gap: 7, alignItems: "center", cursor: "pointer", fontSize: "0.75rem", color: colors.text, marginBottom: codingFollows ? 6 : 10 }}>
                        <input type="checkbox" checked={codingFollows} onChange={e => setCodingFollows(e.target.checked)} />
                        {t("Follow AI assistant", "跟随普通 AI 助手")}
                    </label>
                    {codingFollows ? <div aria-live="polite" style={{ color: colors.textSecondary, fontSize: "0.74rem" }}>{followingPreviewPending ? t("Effective after save: ", "保存后生效：") : t("Effective now: ", "当前生效：")}{followingAssistantProviderName} · {followingAssistantModel}</div> : renderSelectors("coding", coding || {})}
                    {renderProbeAction("coding")}
                </div>
            </div>
            <div style={{ borderTop: `1px solid ${colors.border}`, paddingTop: 14, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 280px), 1fr))", gap: 14, alignItems: "start" }}>
                <div style={{ minWidth: 0 }}><strong style={{ fontSize: "0.78rem", color: colors.text }}>{t("Caption model", "Caption 模型")}</strong><p style={{ margin: "3px 0 0", color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.35 }}>{t("Used only when the chat model cannot see images. Labels unlabeled Computer Use boxes after OCR and accessibility. Leave empty to skip.", "仅在聊天模型不支持视觉、又需要给未标注控件补标签时使用。OCR / 无障碍已有文字时不会调用。留空则跳过。")}</p></div>
                <div style={{ minWidth: 0 }}>
                    {renderSelectors("caption", caption || {})}
                    {caption?.provider_id && captionModelMissingVision(captionVision.provider, caption?.model) && (
                        <p style={{ margin: "6px 0 0", color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.4 }}>
                            {captionVision.provider?.is_hub_service
                                ? t("Hub models are not image-tested here. Captioning unlabeled boxes needs a vision model.", "Hub 模型不会在此检测图片能力。给未标注控件补标签需要视觉模型。")
                                : captionVision.status === "untested"
                                    ? t("This model's image support has not been tested. Captioning unlabeled boxes needs a vision model.", "该模型尚未测试图片能力。给未标注控件补标签需要视觉模型。")
                                    : t("This model was not marked vision-capable. Captioning unlabeled boxes needs a vision model.", "该模型未标记为支持视觉。给未标注控件补标签需要视觉模型。")}
                        </p>
                    )}
                    {renderProbeAction("caption")}
                </div>
            </div>
            {([
                ["foreground", t("Foreground", "前台交互", "前台互動"), t("Planning and chat", "规划与对话", "規劃與對話"), t("Foreground follows Hub", "前台跟随 Hub", "前台跟隨 Hub")],
                ["background", t("Background", "后台执行", "後台執行"), t("Worker loops", "后台任务循环", "後台任務循環"), t("Background follows Hub", "后台跟随 Hub", "後台跟隨 Hub")],
                ["computer_use", t("Computer use", "电脑操作", "電腦操作"), t("Screenshots and clicks", "截图与点击", "截圖與點擊"), t("Computer use follows Hub", "电脑操作跟随 Hub", "電腦操作跟隨 Hub")],
                ["grounder", t("Grounding model", "定位模型", "定位模型"), t("Optional. Finds the click point when Computer Use names a target in words (e.g. UI-TARS, Qwen-VL). If unset or it fails, the Computer Use model clicks by itself.", "可选。电脑操作用文字描述点击目标时，由它给出坐标（如 UI-TARS、Qwen-VL）。不设置或定位失败时，由电脑操作模型自己点击。", "可選。電腦操作用文字描述點擊目標時，由它給出座標（如 UI-TARS、Qwen-VL）。不設定或定位失敗時，由電腦操作模型自己點擊。"), t("Grounding model not used", "不使用定位模型", "不使用定位模型")],
            ] as const).map(([key, title, hint, aria]) => {
                const assignment = draft[key] || {};
                const inherits = roleInheritsHub(assignment);
                const summary = key === "foreground" ? state.foreground : key === "background" ? state.background : key === "grounder" ? state.grounder : state.computer_use;
                const isGrounder = key === "grounder";
                const hubModel = isGrounder ? undefined : state.hub_role_models?.[key];
                const shown = isGrounder ? t("Not used (single model)", "不使用（单模型）", "不使用（單模型）") : String(summary?.model || hubModel || "auto");
                const hubProvider = providers.find(provider => provider.is_hub_service);
                const summaryProviderID = String(summary?.provider_id || "").trim();
                const foregroundUsesAssistant = key === "foreground" && inherits && summaryProviderID !== "" && summaryProviderID !== String(hubProvider?.id || "").trim();
                return (
                    <div key={key} style={{ borderTop: `1px solid ${colors.border}`, paddingTop: 14, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 280px), 1fr))", gap: 14, alignItems: "start" }}>
                        <div style={{ minWidth: 0 }}><strong style={{ fontSize: "0.78rem", color: colors.text }}>{title}</strong><p style={{ margin: "3px 0 0", color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.35 }}>{hint}</p></div>
                        <div style={{ minWidth: 0 }}>
                            <label style={{ display: "inline-flex", gap: 7, alignItems: "center", cursor: "pointer", fontSize: "0.75rem", color: colors.text, marginBottom: inherits ? 6 : 10 }}>
                                <input type="checkbox" aria-label={aria} checked={inherits} onChange={e => setRoleInherit(key, e.target.checked)} />
                                {isGrounder ? t("Not used", "不使用", "不使用") : foregroundUsesAssistant ? t("Follow AI assistant", "跟随普通 AI 助手", "跟隨普通 AI 助手") : t("Follow Hub", "跟随 Hub", "跟隨 Hub")}
                            </label>
                            {inherits
                                ? <div aria-live="polite" style={{ color: colors.textSecondary, fontSize: "0.74rem" }}>{t("Effective now: ", "当前生效：", "目前生效：")}{!isGrounder && summary?.provider_name ? `${summary.provider_name} · ` : ""}{shown}</div>
                                : <SuggestCombobox
                                    listboxId={`${key}-role-models`}
                                    ariaLabel={aria}
                                    value={String(assignment.model || "")}
                                    options={roleModelOptions()}
                                    onChange={model => setRoleModel(key, model)}
                                    placeholder={t("Select or enter model ID", "选择或输入模型 ID", "選擇或輸入模型 ID")}
                                    openOnEnter
                                    toggleLabel={t("Show model list", "显示模型列表", "顯示模型列表")}
                                    emptyText={t("No matching models", "没有匹配的模型", "沒有匹配的模型")}
                                    inputStyle={inputStyle}
                                />}
                        </div>
                    </div>
                );
            })}
        </div>
        {error && <div role="alert" className="llm-profile-error"><span>{error}</span>{dirty && <button type="button" className="llm-profile-error__btn" onClick={refreshDraft}>{t("Refresh draft", "刷新草稿")}</button>}</div>}
        <div className="llm-profile-footer">
            {dirty && <span className="llm-profile-footer__unsaved">{t("Unsaved changes", "有未保存更改")}</span>}
            {dirty && <button type="button" onClick={refreshDraft} disabled={saving} className="llm-config-btn">{t("Discard", "放弃更改")}</button>}
            <button type="button" disabled={!dirty || saving} onClick={() => void save()} className="llm-config-btn llm-config-btn--primary">
                {saving ? t("Saving…", "正在保存…") : t("Save changes", "保存更改")}
            </button>
        </div>
    </>);
}
