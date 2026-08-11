import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetMaclawLLMProfilePanelState, SaveMaclawLLMProfiles, TestMaclawLLMProfile } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { inputStyle, labelStyle } from "./LLMConfigPanelShared";

type Profile = { provider_id?: string; model?: string; inherit_assistant?: boolean };
type Provider = { id: string; name: string; model?: string; models?: string[] };
type Summary = { provider_id?: string; provider_name?: string; model?: string; inherit_assistant?: boolean; health?: string };
type ProbeResult = { profile: "assistant" | "coding"; health: string; reason_code?: string };
type PanelState = {
    providers: Provider[];
    profiles: { version: number; assistant: Profile; coding: Profile };
    assistant: Summary;
    coding: Summary;
    revision: string;
};

type Props = {
    lang?: string;
    onSaved?: () => void;
};

const cloneProfiles = (profiles: PanelState["profiles"]): PanelState["profiles"] => ({
    version: profiles.version,
    assistant: { ...profiles.assistant },
    coding: { ...profiles.coding },
});

export function LLMProfileAssignments({ lang, onSaved }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en, [lang]);
    const [state, setState] = useState<PanelState | null>(null);
    const [draft, setDraft] = useState<PanelState["profiles"] | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState("");
    const [testingProfile, setTestingProfile] = useState<"assistant" | "coding" | null>(null);
    const [probeResults, setProbeResults] = useState<Partial<Record<"assistant" | "coding", ProbeResult>>>({});
    const dirtyRef = useRef(false);
    const loadGenerationRef = useRef(0);
    // A probe is asynchronous while its draft remains editable. Bump the
    // generation whenever a selection changes so a late response cannot label
    // a newer provider/model as connected (or unavailable).
    const probeGenerationRef = useRef<Record<"assistant" | "coding", number>>({ assistant: 0, coding: 0 });
    const invalidateProbeResults = (...profiles: Array<"assistant" | "coding">) => {
        for (const profile of profiles) probeGenerationRef.current[profile] += 1;
        setProbeResults(prev => {
            const next = { ...prev };
            for (const profile of profiles) delete next[profile];
            return next;
        });
    };

    const load = useCallback(async () => {
        const generation = ++loadGenerationRef.current;
        setLoading(true);
        setError("");
        try {
            const next = await GetMaclawLLMProfilePanelState() as unknown as PanelState;
            if (generation !== loadGenerationRef.current) return;
            setState(next);
            setDraft(cloneProfiles(next.profiles));
            invalidateProbeResults("assistant", "coding");
        } catch (err) {
            if (generation !== loadGenerationRef.current) return;
            setError(String(err));
        } finally {
            if (generation === loadGenerationRef.current) setLoading(false);
        }
    }, []);

    useEffect(() => { void load(); }, [load]);

    const providers = state?.providers || [];
    const providerByID = useMemo(() => new Map(providers.map(p => [p.id, p])), [providers]);
    const assistant = draft?.assistant;
    const coding = draft?.coding;
    const codingFollows = coding?.inherit_assistant === true;
    // While editing, "follow assistant" must reflect the unsaved assistant
    // draft, not the last persisted panel snapshot. Otherwise changing the
    // assistant provider/model makes coding appear to keep the old choice
    // until after Save + reload, which contradicts the follow relationship.
    const followingAssistantProvider = providerByID.get(String(assistant?.provider_id || ""));
    const followingAssistantProviderName = followingAssistantProvider?.name || t("No provider", "未配置服务商");
    const followingAssistantModel = assistant?.model || t("No model", "未配置模型");
    const followingPreviewPending = codingFollows && !!state && (
        state.profiles.coding.inherit_assistant !== true ||
        state.profiles.assistant.provider_id !== assistant?.provider_id ||
        state.profiles.assistant.model !== assistant?.model
    );
    const dirty = !!state && !!draft && JSON.stringify(state.profiles) !== JSON.stringify(draft);
    dirtyRef.current = dirty;

    useEffect(() => {
        const onProfilesChanged = () => {
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
    }, [load, t]);

    useEffect(() => () => {
        loadGenerationRef.current += 1;
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

    const providerModels = (providerID?: string) => {
        const provider = providerByID.get(String(providerID || ""));
        if (!provider) return [];
        const options = [provider.model, ...(provider.models || [])].map(value => String(value || "").trim()).filter(Boolean);
        return Array.from(new Set(options));
    };
    const setProvider = (profile: "assistant" | "coding", providerID: string) => {
        const models = providerModels(providerID);
        setDraft(prev => prev ? ({
            ...prev,
            [profile]: { ...prev[profile], provider_id: providerID, model: models[0] || "" },
        }) : prev);
        invalidateProbeResults(profile, ...(profile === "assistant" && codingFollows ? ["coding" as const] : []));
    };
    const setModel = (profile: "assistant" | "coding", model: string) => {
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
        if (result.health === "configured") return t("Connected", "已连接");
        if (result.health === "unavailable") return t("Unavailable", "不可用");
        if (result.health === "invalid") return t("Invalid configuration", "配置无效");
        return t("Unverified — try again", "未验证，请重试");
    };
    const testProfile = async (profile: "assistant" | "coding") => {
        if (!draft || testingProfile) return;
        const value = profile === "assistant" ? draft.assistant : draft.coding;
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
                setProbeResults(prev => ({ ...prev, [profile]: { ...result, profile } }));
            }
        } catch {
            if (probeGenerationRef.current[profile] === generation) {
                setProbeResults(prev => ({ ...prev, [profile]: { profile, health: "unverified", reason_code: "probe_retryable" } }));
            }
        } finally {
            setTestingProfile(null);
        }
    };

    const renderSelectors = (profile: "assistant" | "coding", value: Profile) => (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 180px), 1fr))", gap: 10, alignItems: "end" }}>
            <div>
                <label style={labelStyle}>{t("Provider", "服务商")}</label>
                <select aria-label={profile === "assistant" ? t("Assistant provider", "普通 AI 助手服务商") : t("Coding provider", "编程 Agent 服务商")}
                    value={value.provider_id || ""} onChange={e => setProvider(profile, e.target.value)} style={{ ...inputStyle, cursor: "pointer" }}>
                    <option value="">{t("Select provider", "选择服务商")}</option>
                    {providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name}</option>)}
                </select>
            </div>
            <div>
                <label style={labelStyle}>{t("Model", "模型")}</label>
                <input list={`${profile}-profile-models`} value={value.model || ""} onChange={e => setModel(profile, e.target.value)}
                    aria-label={profile === "assistant" ? t("Assistant model", "普通 AI 助手模型") : t("Coding model", "编程 Agent 模型")}
                    placeholder={t("Select or enter model ID", "选择或输入模型 ID")} style={inputStyle} />
                <datalist id={`${profile}-profile-models`}>{providerModels(value.provider_id).map(model => <option key={model} value={model} />)}</datalist>
            </div>
        </div>
    );
    const renderProbeAction = (profile: "assistant" | "coding") => (
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 8 }}>
            <button type="button" onClick={() => void testProfile(profile)} disabled={testingProfile !== null}
                style={{ fontSize: "0.7rem", padding: "4px 8px", cursor: testingProfile ? "wait" : "pointer", background: colors.surface, color: colors.primaryDark, border: `1px solid ${colors.border}`, borderRadius: 4 }}>
                {testingProfile === profile ? t("Checking…", "正在检查…") : t("Test connection", "测试连接")}
            </button>
            {probeResults[profile] && <span role="status" style={{ fontSize: "0.7rem", color: probeResults[profile]?.health === "configured" ? colors.success : probeResults[profile]?.health === "unavailable" || probeResults[profile]?.health === "invalid" ? colors.danger : colors.textMuted }}>{probeLabel(probeResults[profile])}</span>}
        </div>
    );

    if (loading) return <div style={{ color: colors.textMuted, fontSize: "0.78rem", padding: "8px 0" }}>{t("Loading model assignments…", "正在加载模型分配…")}</div>;
    if (!state || !draft) return <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem" }}>{error || t("Could not load model assignments.", "无法加载模型分配。")}</div>;

    return <section aria-labelledby="llm-profile-assignments-title" style={{ marginBottom: 16, padding: "14px 16px", border: `1px solid ${colors.border}`, background: colors.surface, borderRadius: 6, minWidth: 0 }}>
        <div style={{ marginBottom: 14 }}>
            <div style={{ minWidth: 0, flex: "1 1 280px" }}>
                <h3 id="llm-profile-assignments-title" style={{ fontSize: "0.86rem", color: colors.text, margin: 0 }}>{t("Model assignments", "模型分配")}</h3>
                <p style={{ color: colors.textMuted, fontSize: "0.72rem", lineHeight: 1.5, margin: "4px 0 0" }}>{t("Choose models for each work context. Connections and credentials are managed separately.", "为不同工作场景选择模型；连接与凭据在服务商管理中维护。")}</p>
            </div>
        </div>

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
        </div>
        {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.72rem", marginTop: 10, display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}><span>{error}</span>{dirty && <button type="button" onClick={refreshDraft} style={{ fontSize: "0.7rem", padding: "3px 7px", cursor: "pointer", background: colors.surface, color: colors.danger, border: `1px solid ${colors.danger}`, borderRadius: 4 }}>{t("Refresh draft", "刷新草稿")}</button>}</div>}
        <div style={{ display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 8, marginTop: 14, flexWrap: "wrap" }}>
            {dirty && <span style={{ marginRight: "auto", fontSize: "0.7rem", color: colors.primaryDark }}>{t("Unsaved changes", "有未保存更改")}</span>}
            {dirty && <button type="button" onClick={refreshDraft} disabled={saving} style={{ fontSize: "0.74rem", padding: "6px 10px", cursor: saving ? "default" : "pointer", background: colors.surface, color: colors.textSecondary, border: `1px solid ${colors.border}`, borderRadius: 4 }}>{t("Discard", "放弃更改")}</button>}
            <button type="button" disabled={!dirty || saving} onClick={() => void save()} style={{ fontSize: "0.74rem", padding: "6px 12px", cursor: !dirty || saving ? "default" : "pointer", opacity: !dirty || saving ? 0.6 : 1, background: colors.primaryLight, color: colors.primaryDark, border: `1px solid ${colors.primary}`, borderRadius: 4 }}>
                {saving ? t("Saving…", "正在保存…") : t("Save changes", "保存更改")}
            </button>
        </div>
    </section>;
}
