import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { GetMoAConfig, GetMoASessionState, GetMoAStats, SaveMoAConfig, SetMoASticky } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
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

/** Normalize unknown throw values (Wails may reject with string or Error). */
export function moaErrorMessage(e: unknown): string {
    if (e instanceof Error) return e.message.trim();
    if (typeof e === "string") return e.trim();
    if (e && typeof e === "object" && "message" in e) {
        const m = (e as { message?: unknown }).message;
        if (typeof m === "string" && m.trim()) return m.trim();
    }
    const s = String(e ?? "").trim();
    return s === "[object Object]" ? "unknown error" : s;
}

type MoATranslate = (en: string, zhHans: string, zhHant?: string) => string;

/**
 * Map known backend MoA detail strings to localized, actionable UI copy.
 * Match exact backend tokens from gui/moa_session.go — avoid broad substrings.
 */
export function localizeMoAError(raw: string, t: MoATranslate): string {
    const msg = String(raw || "").trim();
    const lower = msg.toLowerCase();
    if (lower.includes("enable multi-model in llm settings")) {
        return t(
            "Turn on multi-model above and click Save first.",
            "请先勾选「开启」，并点击「保存」后再试。",
            "請先勾選「開啟」，並點擊「保存」後再試。",
        );
    }
    if (
        lower.includes("configure other models in multi-model settings") ||
        lower.includes("no usable other models")
    ) {
        return t(
            "Pick at least one usable other model, then Save.",
            "请至少选择一个可用的「其他模型」并保存。",
            "請至少選擇一個可用的「其他模型」並保存。",
        );
    }
    if (lower.includes("maclaw_moa=off") || lower.includes("kill switch")) {
        return t(
            "Multi-model is blocked by MACLAW_MOA=off.",
            "环境变量 MACLAW_MOA=off 已紧急关闭多模型会诊。",
            "環境變數 MACLAW_MOA=off 已緊急關閉多模型會診。",
        );
    }
    if (lower.includes("configure a primary llm first")) {
        return t(
            "Configure the primary LLM above first.",
            "请先在上方配置当前主模型。",
            "請先在上方配置當前主模型。",
        );
    }
    if (lower.includes("agent not ready") || lower.includes("app not ready")) {
        return t("App is still starting, try again in a moment.", "应用仍在启动，请稍后再试。");
    }
    if (lower.includes("load config failed")) {
        return t("Failed to load settings.", "加载配置失败。", "載入配置失敗。");
    }
    return msg;
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
    /** Bumps on unmount / superseding load so stale async results are ignored. */
    const loadGenRef = useRef(0);

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

    const applyStats = useCallback((st: MoAStatsSnapshot | null | undefined) => {
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
    }, []);

    const load = useCallback(
        async (opts?: { soft?: boolean; quiet?: boolean }) => {
            const soft = !!opts?.soft;
            const quiet = !!opts?.quiet;
            const gen = ++loadGenRef.current;
            if (!soft) {
                setLoading(true);
                setError(null);
            }
            try {
                const cfg = (await GetMoAConfig()) as MoAConfig;
                if (gen !== loadGenRef.current) return;
                setBaseCfg(cfg);
                setState(loadState(cfg));
                try {
                    const sess = (await GetMoASessionState()) as { sticky?: boolean };
                    if (gen !== loadGenRef.current) return;
                    setSticky(!!sess?.sticky);
                } catch {
                    // Soft/quiet: keep sticky checkbox; hard load: assume off.
                    if (gen !== loadGenRef.current) return;
                    if (!soft) setSticky(false);
                }
                try {
                    const st = (await GetMoAStats()) as MoAStatsSnapshot;
                    if (gen !== loadGenRef.current) return;
                    applyStats(st);
                } catch {
                    if (gen !== loadGenRef.current) return;
                    if (!soft) setStatsLine(null);
                }
            } catch (e: unknown) {
                if (gen !== loadGenRef.current) return;
                // After a successful Save, quiet soft-reload must not look like save failed.
                if (!quiet) setError(localizeMoAError(moaErrorMessage(e), t));
            } finally {
                if (!soft && gen === loadGenRef.current) setLoading(false);
            }
        },
        [applyStats, t],
    );

    useEffect(() => {
        void load();
        return () => {
            loadGenRef.current += 1;
        };
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

    /**
     * Sticky arms backend session state — only after config is actually saved enabled.
     * Turning sticky OFF is always allowed (even if UI enable was toggled off and not saved).
     */
    const savedEnabled = !!baseCfg?.enabled;
    const canArmSticky = savedEnabled && state.enabled;

    const toggleSticky = async (next: boolean) => {
        if (next && !canArmSticky) {
            setError(
                t(
                    "Turn on multi-model above and click Save first.",
                    "请先勾选「开启」，并点击「保存」后再试。",
                    "請先勾選「開啟」，並點擊「保存」後再試。",
                ),
            );
            return;
        }
        setStickyBusy(true);
        setError(null);
        try {
            await SetMoASticky(next);
            setSticky(next);
        } catch (e: unknown) {
            setError(localizeMoAError(moaErrorMessage(e), t));
            // Only clear local sticky when an arm attempt failed. A failed disarm
            // must keep sticky=true so the user can retry turning it off.
            if (next) setSticky(false);
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
                    return;
                }
            }
            await SaveMoAConfig(next as corelib.MoAConfig);
            // Optimistic: unlock sticky arm immediately (canArmSticky) without waiting for reload.
            setBaseCfg(next);
            // Disabling MoA should also clear session sticky arm.
            if (!next.enabled && sticky) {
                try {
                    await SetMoASticky(false);
                    setSticky(false);
                } catch {
                    // Config is saved; sticky clear is best-effort.
                }
            }
            setSavedOk(true);
            // Soft reload for server-normalized config; keep form mounted.
            // quiet: reload failure must not clobber the successful-save UX.
            await load({ soft: true, quiet: true });
        } catch (e: unknown) {
            setError(localizeMoAError(moaErrorMessage(e), t));
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

            {/* Sticky lives outside enabled-only block so it can be turned OFF after unchecking 开启. */}
            {(state.enabled || sticky) && (
                <label
                    style={{
                        display: "flex",
                        alignItems: "flex-start",
                        gap: 8,
                        marginTop: 12,
                        fontSize: "0.75rem",
                        cursor: stickyBusy || saving ? "wait" : "pointer",
                        lineHeight: 1.4,
                    }}
                >
                    <input
                        type="checkbox"
                        data-testid="moa-sticky-toggle"
                        checked={sticky}
                        // Always allow turning OFF; only arming requires saved+enabled config.
                        // Block during save so arm doesn't race with config write.
                        disabled={stickyBusy || saving || (!sticky && !canArmSticky)}
                        onChange={(e) => void toggleSticky(e.target.checked)}
                        style={{ marginTop: 2 }}
                        title={
                            stickyBusy || saving
                                ? t("Please wait…", "请稍候…")
                                : sticky || canArmSticky
                                  ? undefined
                                  : t(
                                        "Save multi-model settings first",
                                        "请先保存多模型会诊设置",
                                        "請先保存多模型會診設置",
                                    )
                        }
                    />
                    <span>
                        <strong>{t("Keep multi-model on for this session", "本会话保持多模型会诊")}</strong>
                        <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: 2 }}>
                            {!state.enabled && sticky
                                ? t(
                                      "Session multi-model is still on. Turn this off, or re-enable and Save multi-model settings.",
                                      "本会话多模型仍在生效。可关闭此项，或重新开启并保存多模型设置。",
                                      "本會話多模型仍在生效。可關閉此項，或重新開啟並保存多模型設置。",
                                  )
                                : state.enabled && !savedEnabled
                                  ? t(
                                        "Click Save below first, then you can keep multi-model on for this session.",
                                        "请先点击下方「保存」，保存成功后才能开启本会话保持。",
                                        "請先點擊下方「保存」，保存成功後才能開啟本會話保持。",
                                    )
                                  : t(
                                        "Every message uses multi-model until you turn this off or switch the main provider.",
                                        "开启后每条消息都先问其他模型；关闭此项或切换主服务商后恢复普通模式。",
                                    )}
                        </div>
                    </span>
                </label>
            )}

            <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 12 }}>
                <button
                    type="button"
                    data-testid="moa-config-save"
                    disabled={saving}
                    onClick={() => void handleSave()}
                    style={{
                        fontSize: "0.75rem",
                        fontWeight: 600,
                        border: "1px solid var(--theme-primary-strong, #183b63)",
                        borderRadius: 6,
                        /* primary-strong + on-primary: dark mode primary alone is too light for white text */
                        background: "var(--theme-primary-strong, #183b63)",
                        color: "var(--theme-on-primary, #ffffff)",
                        padding: "6px 14px",
                        cursor: saving ? "wait" : "pointer",
                        opacity: saving ? 0.7 : 1,
                    }}
                >
                    {saving ? t("Saving…", "保存中…") : t("Save", "保存")}
                </button>
                {savedOk && (
                    <span
                        style={{ fontSize: "0.72rem", color: "var(--theme-success, #15803d)", fontWeight: 600 }}
                        data-testid="moa-config-saved"
                    >
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
