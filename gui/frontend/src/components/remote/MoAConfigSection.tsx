import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import { GetMoAConfig, GetMoASessionState, GetMoAStats, SaveMoAConfig, SetMoASticky } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { inputStyle, labelStyle } from "./LLMConfigPanelShared";
import type { LLMProvider } from "./LLMConfigPanelShared";

type MoAStatsSnapshot = {
    fanouts?: number;
    ref_ok?: number;
    ref_fail?: number;
    last_preset?: string;
    last_ms?: number;
    last_ref_ok?: number;
    last_ref_fail?: number;
};

export interface MoAModelRef {
    provider?: string;
    task_route?: string;
    model?: string;
    use_primary?: boolean;
    use_aux?: boolean;
}

export interface MoAPresetConfig {
    enabled: boolean;
    display_name?: string;
    reference_models?: MoAModelRef[];
    aggregator: MoAModelRef;
    reference_max_tokens?: number;
    max_tokens?: number;
}

export interface MoAConfig {
    enabled: boolean;
    default_preset?: string;
    allow_auto?: boolean;
    max_references?: number;
    reference_timeout_sec?: number;
    fanout_max_iterations?: number;
    only_before_first_tool?: boolean;
    presets?: Record<string, MoAPresetConfig>;
}

const DEFAULT_PRESET_ID = "review";
const DEFAULT_DISPLAY = { zh: "方案评审", en: "Design review" };

type SimpleState = {
    enabled: boolean;
    /** Preset id loaded into the simple form (same id written on save). */
    editPresetId: string;
    /** Up to 3 provider names for advisors (empty = unused). */
    advisors: string[];
    referenceMaxTokens: number;
    fanoutMax: number;
    onlyBeforeFirstTool: boolean;
    allowAuto: boolean;
    displayName: string;
};

/** Which preset the simple UI edits — prefer default_preset when it exists. */
function resolveEditPresetId(cfg: MoAConfig): string {
    const want = (cfg.default_preset || DEFAULT_PRESET_ID).trim() || DEFAULT_PRESET_ID;
    if (cfg.presets?.[want]) return want;
    if (cfg.presets?.[DEFAULT_PRESET_ID]) return DEFAULT_PRESET_ID;
    const keys = Object.keys(cfg.presets || {});
    if (keys.length > 0) return keys.sort()[0];
    return DEFAULT_PRESET_ID;
}

function defaultState(): SimpleState {
    return {
        enabled: false,
        editPresetId: DEFAULT_PRESET_ID,
        advisors: ["", ""],
        referenceMaxTokens: 600,
        fanoutMax: 1,
        onlyBeforeFirstTool: true,
        allowAuto: false,
        displayName: "",
    };
}

function loadState(cfg: MoAConfig): SimpleState {
    const editPresetId = resolveEditPresetId(cfg);
    const preset = cfg.presets?.[editPresetId];
    const refs = preset?.reference_models || [];
    const advisors = refs
        .filter((r) => r.provider && !r.use_aux && !r.task_route)
        .map((r) => r.provider || "");
    // Pad to at least 2 slots for simple UI
    while (advisors.length < 2) advisors.push("");
    // Cap simple slots at 3
    const simpleAdvisors = advisors.slice(0, 3);
    return {
        enabled: !!cfg.enabled,
        editPresetId,
        advisors: simpleAdvisors.length > 0 ? simpleAdvisors : ["", ""],
        referenceMaxTokens: preset?.reference_max_tokens && preset.reference_max_tokens > 0 ? preset.reference_max_tokens : 600,
        fanoutMax: cfg.fanout_max_iterations && cfg.fanout_max_iterations > 0 ? cfg.fanout_max_iterations : 1,
        onlyBeforeFirstTool: cfg.only_before_first_tool !== false,
        allowAuto: !!cfg.allow_auto,
        displayName: preset?.display_name || "",
    };
}

/**
 * Build save payload from the simple UI.
 * Preserves other presets / advanced fields from `existing` so a simple save
 * cannot wipe multi-preset configs used by sticky pickers.
 * Writes back to the same preset id that was loaded (editPresetId).
 */
function buildConfig(state: SimpleState, isZh: boolean, existing?: MoAConfig | null): MoAConfig {
    const reference_models: MoAModelRef[] = state.advisors
        .map((p) => p.trim())
        .filter(Boolean)
        .map((provider) => ({ provider }));
    const display =
        state.displayName.trim() ||
        (isZh ? DEFAULT_DISPLAY.zh : DEFAULT_DISPLAY.en);
    const editId = (state.editPresetId || DEFAULT_PRESET_ID).trim() || DEFAULT_PRESET_ID;
    const prev = existing?.presets?.[editId];
    const presets: Record<string, MoAPresetConfig> = { ...(existing?.presets || {}) };
    presets[editId] = {
        enabled: true,
        display_name: display,
        // Simple UI always aggregates with primary; keep optional max_tokens.
        aggregator: { use_primary: true },
        reference_models,
        reference_max_tokens: state.referenceMaxTokens > 0 ? state.referenceMaxTokens : undefined,
        max_tokens: prev?.max_tokens,
    };
    const defaultPreset =
        (existing?.default_preset || "").trim() ||
        editId;
    return {
        enabled: state.enabled,
        default_preset: defaultPreset,
        allow_auto: state.allowAuto || undefined,
        max_references: existing?.max_references,
        reference_timeout_sec: existing?.reference_timeout_sec,
        fanout_max_iterations: state.fanoutMax > 0 ? state.fanoutMax : 1,
        only_before_first_tool: state.onlyBeforeFirstTool,
        presets,
    };
}

interface Props {
    lang?: string;
    providers: LLMProvider[];
}

export function MoAConfigSection({ lang, providers }: Props) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant: string = zhHans) =>
            lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en,
        [lang],
    );
    const isZh = !lang?.startsWith("en");

    const providerNames = useMemo(
        () =>
            providers
                .map((p) => (p.name || "").trim())
                .filter((n) => n && n !== "None" && !n.startsWith("__")),
        [providers],
    );

    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [savedOk, setSavedOk] = useState(false);
    const [showAdvanced, setShowAdvanced] = useState(false);
    const [state, setState] = useState<SimpleState>(defaultState);
    /** Full last-loaded config so saves can merge and preserve extra presets. */
    const [baseCfg, setBaseCfg] = useState<MoAConfig | null>(null);
    const [sticky, setSticky] = useState(false);
    const [stickyBusy, setStickyBusy] = useState(false);
    const [statsLine, setStatsLine] = useState<string | null>(null);

    const patch = (partial: Partial<SimpleState>) => {
        setState((s) => ({ ...s, ...partial }));
        setSavedOk(false);
    };

    const setAdvisor = (index: number, value: string) => {
        setState((s) => {
            const advisors = s.advisors.slice();
            advisors[index] = value;
            return { ...s, advisors };
        });
        setSavedOk(false);
    };

    const load = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const cfg = (await GetMoAConfig()) as MoAConfig;
            setBaseCfg(cfg);
            setState(loadState(cfg));
            try {
                const sess = (await GetMoASessionState()) as { sticky?: boolean };
                setSticky(!!sess?.sticky);
            } catch {
                setSticky(false);
            }
            try {
                const st = (await GetMoAStats()) as MoAStatsSnapshot;
                if (st && (st.fanouts || 0) > 0) {
                    const last =
                        st.last_preset || st.last_ms
                            ? ` · last=${st.last_preset || "?"} ${st.last_ms || 0}ms ${st.last_ref_ok || 0}ok/${st.last_ref_fail || 0}fail`
                            : "";
                    setStatsLine(
                        `fanouts=${st.fanouts} ref_ok=${st.ref_ok || 0} ref_fail=${st.ref_fail || 0}${last}`,
                    );
                } else {
                    setStatsLine(null);
                }
            } catch {
                setStatsLine(null);
            }
        } catch (e: unknown) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void load();
    }, [load]);

    useEffect(() => {
        EventsOn("moa-session-changed", () => {
            void GetMoASessionState()
                .then((sess: { sticky?: boolean }) => setSticky(!!sess?.sticky))
                .catch(() => {});
        });
        return () => {
            EventsOff("moa-session-changed");
        };
    }, []);

    const toggleSticky = async (next: boolean) => {
        setStickyBusy(true);
        setError(null);
        try {
            await SetMoASticky(next);
            setSticky(next);
        } catch (e: unknown) {
            setError(e instanceof Error ? e.message : String(e));
            setSticky(false);
        } finally {
            setStickyBusy(false);
        }
    };

    const handleSave = async () => {
        setSaving(true);
        setError(null);
        setSavedOk(false);
        try {
            const next = buildConfig(state, isZh, baseCfg);
            if (next.enabled) {
                const editId = state.editPresetId || DEFAULT_PRESET_ID;
                const refs = next.presets?.[editId]?.reference_models || [];
                if (refs.length === 0) {
                    setError(t("Pick at least one other model.", "请至少选择一个「其他模型」。"));
                    setSaving(false);
                    return;
                }
            }
            await SaveMoAConfig(next);
            setSavedOk(true);
            await load();
        } catch (e: unknown) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setSaving(false);
        }
    };

    const card: CSSProperties = {
        marginBottom: 16,
        padding: "12px 16px",
        borderRadius: 6,
        border: `1px solid ${colors.border}`,
        background: colors.surface,
    };

    if (loading) {
        return (
            <div className="llm-config-card" style={card}>
                <span style={{ fontSize: "0.8rem", color: colors.textMuted }}>
                    {t("Loading MoA…", "加载多模型会诊配置…")}
                </span>
            </div>
        );
    }

    return (
        <div className="llm-config-card moa-config-section" style={card} data-testid="moa-config-section">
            <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 12, marginBottom: 8 }}>
                <div style={{ minWidth: 0 }}>
                    <label style={{ ...labelStyle, marginBottom: 2, display: "block", color: "var(--theme-text-primary, inherit)" }}>
                        {t("Ask multiple models first", "先问多个模型再回答")}
                    </label>
                    <div style={{ fontSize: "0.68rem", color: colors.textMuted, lineHeight: 1.45 }}>
                        {t(
                            "For hard questions, other models give opinions first; your current main model writes the final answer. Chat: + → multi-model. Kill switch: MACLAW_MOA=off.",
                            "遇到难题时，可先让其他模型各说一遍看法，再由你现在用的主模型综合并写最终答案。聊天点 + →「多模型会诊」。紧急关闭：环境变量 MACLAW_MOA=off。",
                        )}
                    </div>
                </div>
                <label
                    style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 6,
                        fontSize: "0.78rem",
                        whiteSpace: "nowrap",
                        cursor: "pointer",
                        flexShrink: 0,
                    }}
                >
                    <input
                        type="checkbox"
                        checked={state.enabled}
                        data-testid="moa-config-enabled"
                        onChange={(e) => patch({ enabled: e.target.checked })}
                    />
                    {t("On", "开启")}
                </label>
            </div>

            {state.enabled && (
                <div style={{ marginTop: 10 }}>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: 8, lineHeight: 1.4 }}>
                        {t(
                            "Final answer always uses your current main model (set above).",
                            "最终答案始终用你在上方配置的「当前主模型」。",
                        )}
                    </div>

                    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                        {state.advisors.map((adv, idx) => (
                            <label
                                key={idx}
                                style={{ display: "flex", alignItems: "center", gap: 8, fontSize: "0.75rem", flexWrap: "wrap" }}
                            >
                                <span style={{ minWidth: 72, color: colors.textSecondary || colors.textMuted }}>
                                    {t(`Other model ${idx + 1}`, `其他模型 ${idx + 1}`)}
                                </span>
                                <select
                                    value={adv}
                                    data-testid={`moa-advisor-${idx}`}
                                    onChange={(e) => setAdvisor(idx, e.target.value)}
                                    style={{ ...inputStyle, width: "auto", minWidth: 160, flex: 1 }}
                                >
                                    <option value="">{t("Not used", "不选")}</option>
                                    {providerNames.map((n) => (
                                        <option key={n} value={n}>
                                            {n}
                                        </option>
                                    ))}
                                </select>
                                {idx > 0 && (
                                    <button
                                        type="button"
                                        aria-label={t("Remove", "移除")}
                                        onClick={() => {
                                            setState((s) => ({
                                                ...s,
                                                advisors: s.advisors.filter((_, i) => i !== idx),
                                            }));
                                            setSavedOk(false);
                                        }}
                                        style={{
                                            border: "none",
                                            background: "transparent",
                                            color: colors.textMuted,
                                            cursor: "pointer",
                                            fontSize: "0.85rem",
                                            padding: "0 4px",
                                        }}
                                    >
                                        ×
                                    </button>
                                )}
                            </label>
                        ))}
                    </div>

                    {state.advisors.length < 3 && (
                        <button
                            type="button"
                            data-testid="moa-add-advisor"
                            onClick={() => {
                                setState((s) => ({ ...s, advisors: [...s.advisors, ""] }));
                                setSavedOk(false);
                            }}
                            style={{
                                marginTop: 8,
                                fontSize: "0.72rem",
                                border: `1px solid ${colors.border}`,
                                borderRadius: 4,
                                background: "transparent",
                                padding: "4px 10px",
                                cursor: "pointer",
                                color: colors.textSecondary || colors.text,
                            }}
                        >
                            {t("+ Another model", "+ 再加一个模型")}
                        </button>
                    )}

                    {providerNames.length === 0 && (
                        <div style={{ marginTop: 8, fontSize: "0.68rem", color: colors.danger || "#b91c1c" }}>
                            {t(
                                "Add an LLM provider in the section above first.",
                                "请先在本页上方添加至少一个大模型服务商。",
                            )}
                        </div>
                    )}

                    <label
                        style={{
                            display: "flex",
                            alignItems: "flex-start",
                            gap: 8,
                            marginTop: 12,
                            fontSize: "0.75rem",
                            cursor: stickyBusy ? "wait" : "pointer",
                            lineHeight: 1.4,
                        }}
                    >
                        <input
                            type="checkbox"
                            data-testid="moa-sticky-toggle"
                            checked={sticky}
                            disabled={stickyBusy || !state.enabled}
                            onChange={(e) => void toggleSticky(e.target.checked)}
                            style={{ marginTop: 2 }}
                        />
                        <span>
                            <strong>{t("Keep multi-model on for this session", "本会话保持多模型会诊")}</strong>
                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: 2 }}>
                                {t(
                                    "Every message uses multi-model until you turn this off or switch the main provider.",
                                    "开启后每条消息都先问其他模型；关闭此项或切换主服务商后恢复普通模式。",
                                )}
                            </div>
                        </span>
                    </label>

                    <button
                        type="button"
                        data-testid="moa-advanced-toggle"
                        onClick={() => setShowAdvanced((v) => !v)}
                        style={{
                            display: "block",
                            marginTop: 12,
                            border: "none",
                            background: "transparent",
                            padding: 0,
                            fontSize: "0.72rem",
                            color: colors.textMuted,
                            cursor: "pointer",
                        }}
                    >
                        {showAdvanced
                            ? t("▾ More options", "▾ 更多选项（一般不用改）")
                            : t("▸ More options", "▸ 更多选项（一般不用改）")}
                    </button>

                    {showAdvanced && (
                        <div
                            data-testid="moa-advanced"
                            style={{
                                marginTop: 10,
                                padding: "10px 12px",
                                borderRadius: 6,
                                border: `1px dashed ${colors.border}`,
                                display: "flex",
                                flexDirection: "column",
                                gap: 10,
                                fontSize: "0.75rem",
                            }}
                        >
                            <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                                {t("Name shown in UI", "界面显示名称")}
                                <input
                                    value={state.displayName}
                                    placeholder={isZh ? DEFAULT_DISPLAY.zh : DEFAULT_DISPLAY.en}
                                    onChange={(e) => patch({ displayName: e.target.value })}
                                    style={inputStyle}
                                />
                            </label>
                            <label style={{ display: "flex", alignItems: "center", gap: 8 }}>
                                {t("Max length per other model", "每个其他模型的回答长度上限")}
                                <input
                                    type="number"
                                    min={0}
                                    max={4000}
                                    value={state.referenceMaxTokens}
                                    onChange={(e) =>
                                        patch({
                                            referenceMaxTokens: Math.max(0, Math.min(4000, Number(e.target.value) || 0)),
                                        })
                                    }
                                    style={{ ...inputStyle, width: 80, textAlign: "center" }}
                                />
                            </label>
                            <label style={{ display: "flex", alignItems: "center", gap: 8 }}>
                                {t("How many times to ask them", "最多问几轮其他模型")}
                                <input
                                    type="number"
                                    min={1}
                                    max={5}
                                    value={state.fanoutMax}
                                    onChange={(e) =>
                                        patch({
                                            fanoutMax: Math.max(1, Math.min(5, Number(e.target.value) || 1)),
                                        })
                                    }
                                    style={{ ...inputStyle, width: 56, textAlign: "center" }}
                                />
                            </label>
                            <label style={{ display: "flex", alignItems: "center", gap: 6, cursor: "pointer" }}>
                                <input
                                    type="checkbox"
                                    checked={state.onlyBeforeFirstTool}
                                    onChange={(e) => patch({ onlyBeforeFirstTool: e.target.checked })}
                                />
                                {t("Only ask other models before tools run (saves cost)", "动手改文件/跑命令前再问一遍即可（更省）")}
                            </label>
                            <label style={{ display: "flex", alignItems: "center", gap: 6, cursor: "pointer" }}>
                                <input
                                    type="checkbox"
                                    checked={state.allowAuto}
                                    onChange={(e) => patch({ allowAuto: e.target.checked })}
                                />
                                {t("Auto use on hard tasks (experimental)", "难题时自动使用（实验功能）")}
                            </label>
                        </div>
                    )}
                </div>
            )}

            <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 12 }}>
                <button
                    type="button"
                    data-testid="moa-config-save"
                    disabled={saving}
                    onClick={() => void handleSave()}
                    style={{
                        fontSize: "0.75rem",
                        border: "none",
                        borderRadius: 6,
                        background: "var(--theme-primary, #2f5f98)",
                        color: "#fff",
                        padding: "6px 14px",
                        cursor: saving ? "wait" : "pointer",
                        opacity: saving ? 0.7 : 1,
                    }}
                >
                    {saving ? t("Saving…", "保存中…") : t("Save", "保存")}
                </button>
                {savedOk && (
                    <span style={{ fontSize: "0.72rem", color: "#15803d" }} data-testid="moa-config-saved">
                        {t("Saved", "已保存")}
                    </span>
                )}
            </div>

            {statsLine && (
                <div
                    data-testid="moa-stats-line"
                    style={{ marginTop: 10, fontSize: "0.68rem", color: colors.textMuted, lineHeight: 1.4, fontFamily: "ui-monospace, monospace" }}
                    title={t("Today’s multi-model runtime counters", "今日多模型会诊运行计数")}
                >
                    {t("Usage today: ", "今日使用：")}
                    {statsLine}
                </div>
            )}

            {error && (
                <div
                    data-testid="moa-config-error"
                    style={{ marginTop: 10, fontSize: "0.72rem", color: colors.danger || "#b91c1c", lineHeight: 1.45 }}
                >
                    {error}
                </div>
            )}
        </div>
    );
}
