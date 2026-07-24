/**
 * Bottom quick-settings bar below the chat input: global system switches
 * (model/provider switch, auto routing, TTS, theme, keep-awake, verbose logs,
 * LLM cache, language). Session/window-level actions stay in the title bar.
 * Optional statusSlot rides the same row on the right (shell status / warnings).
 */
import { memo, useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { LoadConfig, PatchConfigFields } from "../../../wailsjs/go/main/App";
import { EVENT_MACLAW_CONFIG_CHANGED } from "../../constants/events";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { TTSLevelBars } from "./TTSLevelBars";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";
import type { SidebarLLMProviderSummary } from "../../types/appShell";

type Props = {
    lang: string;
    theme: Theme;
    themeMode: "light" | "dark";
    onToggleTheme: () => void;
    workflowEnabled: boolean;
    onToggleWorkflow: () => void;
    ttsEnabled: boolean;
    ttsPlaying: boolean;
    onToggleTts: () => void;
    availableProviders?: SidebarLLMProviderSummary[];
    currentModel?: string;
    modelOptions?: string[];
    modelsLoading?: boolean;
    onSwitchProvider?: (providerName: string) => void;
    onSwitchModel?: (modelId: string) => void;
    onOpenModelMenu?: () => void;
    onLanguageChange?: (lang: string) => void;
    /** Shell status cluster (inline AppStatusMessageBar); right side of this row. */
    statusSlot?: ReactNode;
};

const LANG_CYCLE: Record<string, string> = {
    "zh-Hans": "en",
    "en": "zh-Hant",
    "zh-Hant": "zh-Hans",
};

function langShortLabel(lang: string): string {
    if (lang === "en") return "EN";
    if (lang === "zh-Hant") return "繁";
    return "中";
}

export const AssistantQuickSettingsBar = memo(function AssistantQuickSettingsBar({ lang, theme: t, themeMode, onToggleTheme, workflowEnabled, onToggleWorkflow, ttsEnabled, ttsPlaying, onToggleTts, availableProviders, currentModel, modelOptions, modelsLoading, onSwitchProvider, onSwitchModel, onOpenModelMenu, onLanguageChange, statusSlot }: Props) {
    const tr = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang]
    );

    // Global config switches owned by this bar; loaded on mount and kept in
    // sync when other UI (settings panels) patches the config.
    const [workstationMode, setWorkstationMode] = useState(false);
    const [logDetailEnabled, setLogDetailEnabled] = useState(false);
    const [llmCacheEnabled, setLlmCacheEnabled] = useState(false);
    const llmCacheRef = useRef<Record<string, any>>({});

    const syncFromConfig = useCallback((cfg: any) => {
        if (!cfg || typeof cfg !== "object") return;
        setWorkstationMode(cfg.workstation_mode === true);
        setLogDetailEnabled(cfg.log_detail_enabled === true);
        const cache = cfg.llm_prompt_cache;
        if (cache && typeof cache === "object") {
            llmCacheRef.current = { ...cache };
            setLlmCacheEnabled(cache.enabled === true);
        }
    }, []);

    useEffect(() => {
        LoadConfig().then(syncFromConfig).catch(() => { /* ignore */ });
        const onConfigChanged = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (detail && typeof detail === "object") {
                syncFromConfig(detail);
            } else {
                LoadConfig().then(syncFromConfig).catch(() => { /* ignore */ });
            }
        };
        window.addEventListener(EVENT_MACLAW_CONFIG_CHANGED, onConfigChanged);
        return () => window.removeEventListener(EVENT_MACLAW_CONFIG_CHANGED, onConfigChanged);
    }, [syncFromConfig]);

    // Optimistic update + rollback on failure, mirroring AIAssistantPanel.handleToggleWorkflow.
    // Per-field sequence guard: rapid repeated clicks fire overlapping patches, and
    // out-of-order responses must not overwrite state from the latest request.
    const patchSeqRef = useRef<Record<string, number>>({});
    const nextPatchSeq = useCallback((key: string) => {
        const seq = (patchSeqRef.current[key] || 0) + 1;
        patchSeqRef.current[key] = seq;
        return seq;
    }, []);
    const isLatestPatch = useCallback((key: string, seq: number) => patchSeqRef.current[key] === seq, []);

    const toggleConfigField = useCallback((field: "workstation_mode" | "log_detail_enabled", next: boolean, setState: (v: boolean) => void) => {
        setState(next);
        const seq = nextPatchSeq(field);
        PatchConfigFields({ [field]: next } as Record<string, any>).then((saved: any) => {
            // A stale (superseded) response carries outdated config — neither apply
            // it locally nor broadcast it to other listeners.
            if (!isLatestPatch(field, seq)) return;
            setState(saved?.[field] === true);
            window.dispatchEvent(new CustomEvent(EVENT_MACLAW_CONFIG_CHANGED, { detail: saved }));
        }).catch(() => {
            LoadConfig().then((cfg: any) => {
                if (isLatestPatch(field, seq)) setState(cfg?.[field] === true);
            }).catch(() => {
                if (isLatestPatch(field, seq)) setState(!next);
            });
        });
    }, [nextPatchSeq, isLatestPatch]);

    const toggleLlmCache = useCallback((next: boolean) => {
        setLlmCacheEnabled(next);
        const nextCache = { ...llmCacheRef.current, enabled: next };
        llmCacheRef.current = nextCache;
        const seq = nextPatchSeq("llm_prompt_cache");
        PatchConfigFields({ llm_prompt_cache: nextCache } as Record<string, any>).then((saved: any) => {
            // Skip stale responses entirely (see toggleConfigField).
            if (!isLatestPatch("llm_prompt_cache", seq)) return;
            const cache = saved?.llm_prompt_cache;
            if (cache && typeof cache === "object") {
                llmCacheRef.current = { ...cache };
                setLlmCacheEnabled(cache.enabled === true);
            }
            window.dispatchEvent(new CustomEvent(EVENT_MACLAW_CONFIG_CHANGED, { detail: saved }));
        }).catch(() => {
            LoadConfig().then((cfg: any) => {
                const cache = cfg?.llm_prompt_cache;
                if (cache && typeof cache === "object") llmCacheRef.current = { ...cache };
                if (isLatestPatch("llm_prompt_cache", seq)) setLlmCacheEnabled(cache?.enabled === true);
            }).catch(() => {
                if (isLatestPatch("llm_prompt_cache", seq)) setLlmCacheEnabled(!next);
            });
        });
    }, [nextPatchSeq, isLatestPatch]);

    // Model/provider menu
    const [menuOpen, setMenuOpen] = useState(false);
    const menuWrapRef = useRef<HTMLDivElement | null>(null);
    useEffect(() => {
        if (!menuOpen) return;
        const onDown = (e: MouseEvent) => {
            if (menuWrapRef.current && !menuWrapRef.current.contains(e.target as Node)) setMenuOpen(false);
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setMenuOpen(false);
        };
        document.addEventListener("mousedown", onDown);
        document.addEventListener("keydown", onKey);
        return () => {
            document.removeEventListener("mousedown", onDown);
            document.removeEventListener("keydown", onKey);
        };
    }, [menuOpen]);

    const providers = availableProviders || [];
    // App builds the list with the current provider first.
    const currentProvider = providers[0] || null;
    const switchableProviders = providers.slice(1);
    const models = modelOptions || [];
    const hasModelMenu = !!(onSwitchProvider || onSwitchModel) && (providers.length > 0 || models.length > 0 || !!currentModel);
    const modelChipLabel = currentModel || currentProvider?.name || tr("Model", "模型", "模型");

    const openModelMenu = () => {
        // Side effect stays out of the state updater (StrictMode double-invokes updaters).
        const next = !menuOpen;
        setMenuOpen(next);
        if (next) onOpenModelMenu?.();
    };

    const chipStyle = (active: boolean): CSSProperties => ({
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        padding: "2px 8px",
        borderRadius: 999,
        fontSize: 10,
        fontWeight: 600,
        lineHeight: 1,
        cursor: "pointer",
        userSelect: "none",
        border: active ? "1px solid rgba(79, 127, 111, 0.36)" : `1px solid ${t.titleBarBorder}`,
        background: active ? "rgba(79, 127, 111, 0.12)" : t.fieldBg,
        color: active ? "#4f7f6f" : t.promptColor,
        transition: "all 150ms ease",
        flexShrink: 0,
        height: 20,
    });
    const dot = (active: boolean) => (
        <span aria-hidden="true" style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: active ? "#4f7f6f" : t.promptColor, opacity: active ? 1 : 0.4, transition: "all 150ms ease" }} />
    );

    const menuItemStyle = (active: boolean): CSSProperties => ({
        display: "flex",
        alignItems: "center",
        gap: 6,
        width: "100%",
        boxSizing: "border-box",
        padding: "5px 8px",
        border: "none",
        borderRadius: 6,
        background: active ? "rgba(79, 127, 111, 0.12)" : "transparent",
        color: active ? "#4f7f6f" : t.text,
        fontSize: 11,
        fontWeight: active ? 600 : 400,
        lineHeight: 1.3,
        cursor: "pointer",
        textAlign: "left",
    });
    const menuLabelStyle: CSSProperties = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };

    const nextLang = LANG_CYCLE[lang] || "zh-Hans";

    return (
        // Outer row stays non-scrolling so statusSlot can pin to the right while
        // chips alone scroll when the window is narrow.
        // Owns the window bottom edge under the composer. Use minHeight (not fixed
        // height) so safe-area padding extends the bar instead of squeezing chips.
        <div data-testid="assistant-quick-settings-bar" style={{ display: "flex", alignItems: "center", gap: 6, minHeight: 28, padding: "0 10px", paddingBottom: "env(safe-area-inset-bottom, 0px)", borderTop: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, overflow: "hidden", flexShrink: 0, boxSizing: "border-box", minWidth: 0 }}>
            <div data-testid="assistant-quick-settings-chips" style={{ display: "flex", alignItems: "center", gap: 6, flex: "1 1 auto", minWidth: 0, overflowX: "auto", overflowY: "visible" }}>
            {hasModelMenu && (
                <div ref={menuWrapRef} style={{ position: "relative", flexShrink: 0 }}>
                    <button type="button" data-testid="qs-model-chip" onClick={openModelMenu} aria-expanded={menuOpen} aria-haspopup="listbox" style={chipStyle(menuOpen)} title={tr("Switch model or provider", "切换模型或服务商", "切換模型或服務商")}>
                        {dot(true)}
                        <span style={{ maxWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{modelChipLabel}</span>
                        <svg width="7" height="7" viewBox="0 0 8 8" aria-hidden="true" focusable="false"><path d="M1 3l3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></svg>
                    </button>
                    {menuOpen && (
                        <div role="listbox" aria-label={tr("Select provider or model", "选择服务商或模型", "選擇服務商或模型")} style={{ position: "absolute", bottom: "100%", left: 0, marginBottom: 4, minWidth: 200, maxWidth: 280, maxHeight: 280, overflowY: "auto", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 8, boxShadow: "0 8px 24px rgba(15, 23, 42, 0.18)", zIndex: 40000, padding: 4 }}>
                            {currentProvider && (
                                <>
                                    <div aria-hidden="true" style={{ padding: "4px 8px 2px", fontSize: 9, fontWeight: 700, letterSpacing: "0.04em", textTransform: "uppercase", color: t.promptColor }}>
                                        {tr("Providers", "服务商", "服務商")}
                                    </div>
                                    <div role="option" aria-selected="true" style={{ ...menuItemStyle(true), cursor: "default" }} title={currentProvider.name}>
                                        <span aria-hidden="true" style={{ width: 12, flexShrink: 0, textAlign: "center" }}>✓</span>
                                        <span style={menuLabelStyle}>{currentProvider.name}</span>
                                    </div>
                                    {switchableProviders.map((p) => (
                                        <button key={p.name} type="button" role="option" aria-selected="false" style={menuItemStyle(false)} title={p.name} onClick={() => { setMenuOpen(false); onSwitchProvider?.(p.name); }}>
                                            <span aria-hidden="true" style={{ width: 12, flexShrink: 0 }} />
                                            <span style={menuLabelStyle}>{p.name}</span>
                                        </button>
                                    ))}
                                </>
                            )}
                            {onSwitchModel && (models.length > 0 || modelsLoading || currentModel) && (
                                <>
                                    {currentProvider && <div aria-hidden="true" style={{ height: 1, margin: "4px 6px", background: t.titleBarBorder }} />}
                                    <div aria-hidden="true" style={{ padding: "4px 8px 2px", fontSize: 9, fontWeight: 700, letterSpacing: "0.04em", textTransform: "uppercase", color: t.promptColor }}>
                                        {modelsLoading ? tr("Models (loading…)", "模型（加载中…）", "模型（載入中…）") : tr("Models", "模型", "模型")}
                                    </div>
                                    {(models.length > 0 ? models : (currentModel ? [currentModel] : [])).map((modelId) => {
                                        const active = modelId === currentModel;
                                        return (
                                            <button key={modelId} type="button" role="option" aria-selected={active} style={menuItemStyle(active)} title={modelId} onClick={() => { setMenuOpen(false); onSwitchModel(modelId); }}>
                                                <span aria-hidden="true" style={{ width: 12, flexShrink: 0, textAlign: "center" }}>{active ? "✓" : ""}</span>
                                                <span style={menuLabelStyle}>{modelId}</span>
                                            </button>
                                        );
                                    })}
                                </>
                            )}
                        </div>
                    )}
                </div>
            )}
            <button type="button" data-testid="qs-workflow-toggle" role="switch" aria-checked={!!workflowEnabled} onClick={onToggleWorkflow} style={chipStyle(!!workflowEnabled)} title={workflowEnabled ? tr("Automatic task routing ON - click to disable", "自动决策已开启，点击关闭", "自動決策已開啟，點擊關閉") : tr("Automatic task routing OFF - click to enable", "自动决策已关闭，点击开启", "自動決策已關閉，點擊開啟")} aria-label={workflowEnabled ? tr("Automatic task routing ON - click to disable", "自动决策已开启，点击关闭", "自動決策已開啟，點擊關閉") : tr("Automatic task routing OFF - click to enable", "自动决策已关闭，点击开启", "自動決策已關閉，點擊開啟")}>
                {dot(!!workflowEnabled)}
                {tr("Auto routing", "自动决策", "自動決策")}
            </button>
            <button type="button" data-testid="qs-tts-toggle" role="switch" aria-checked={!!ttsEnabled} onClick={onToggleTts} style={{ ...chipStyle(!!ttsEnabled), position: "relative" }} title={ttsEnabled ? tr("Voice readback ON - click to disable", "语音播报已开启，点击关闭", "語音播報已開啟，點擊關閉") : tr("Voice readback OFF - click to enable", "语音播报已关闭，点击开启", "語音播報已關閉，點擊開啟")} aria-label={ttsEnabled ? tr("Voice readback ON - click to disable", "语音播报已开启，点击关闭", "語音播報已開啟，點擊關閉") : tr("Voice readback OFF - click to enable", "语音播报已关闭，点击开启", "語音播報已關閉，點擊開啟")}>
                <span aria-hidden="true" style={{ display: "inline-flex", opacity: ttsPlaying ? 0 : 1, transition: "opacity 150ms" }}>
                    <TitleBarToolIcon name={ttsEnabled ? "volumeOn" : "volumeOff"} />
                </span>
                {ttsPlaying && <span aria-hidden="true" style={{ position: "absolute", left: 8 }}><TTSLevelBars accentColor={t.headingColor} /></span>}
                {tr("Voice", "语音", "語音")}
            </button>
            <button type="button" data-testid="qs-theme-toggle" onClick={onToggleTheme} style={chipStyle(themeMode === "dark")} title={themeMode === "dark" ? tr("Switch to light mode", "切换到普通模式", "切換到普通模式") : tr("Switch to dark mode", "切换到暗黑模式", "切換到暗黑模式")} aria-label={themeMode === "dark" ? tr("Switch to light mode", "切换到普通模式", "切換到普通模式") : tr("Switch to dark mode", "切换到暗黑模式", "切換到暗黑模式")}>
                <span aria-hidden="true" style={{ display: "inline-flex" }}>
                    <TitleBarToolIcon name={themeMode === "dark" ? "moon" : "sun"} />
                </span>
                {themeMode === "dark" ? tr("Dark", "暗黑", "暗黑") : tr("Light", "浅色", "淺色")}
            </button>
            <button type="button" data-testid="qs-workstation-toggle" role="switch" aria-checked={workstationMode} onClick={() => toggleConfigField("workstation_mode", !workstationMode, setWorkstationMode)} style={chipStyle(workstationMode)} title={workstationMode ? tr("Keep-awake ON - click to disable", "防睡眠已开启，点击关闭", "防睡眠已開啟，點擊關閉") : tr("Keep-awake OFF - click to enable", "防睡眠已关闭，点击开启", "防睡眠已關閉，點擊開啟")} aria-label={workstationMode ? tr("Keep-awake ON - click to disable", "防睡眠已开启，点击关闭", "防睡眠已開啟，點擊關閉") : tr("Keep-awake OFF - click to enable", "防睡眠已关闭，点击开启", "防睡眠已關閉，點擊開啟")}>
                {dot(workstationMode)}
                {tr("Keep awake", "防睡眠", "防睡眠")}
            </button>
            <button type="button" data-testid="qs-logdetail-toggle" role="switch" aria-checked={logDetailEnabled} onClick={() => toggleConfigField("log_detail_enabled", !logDetailEnabled, setLogDetailEnabled)} style={chipStyle(logDetailEnabled)} title={logDetailEnabled ? tr("Verbose logs ON - click to disable", "日志详情已开启，点击关闭", "日誌詳情已開啟，點擊關閉") : tr("Verbose logs OFF - click to enable", "日志详情已关闭，点击开启", "日誌詳情已關閉，點擊開啟")} aria-label={logDetailEnabled ? tr("Verbose logs ON - click to disable", "日志详情已开启，点击关闭", "日誌詳情已開啟，點擊關閉") : tr("Verbose logs OFF - click to enable", "日志详情已关闭，点击开启", "日誌詳情已關閉，點擊開啟")}>
                {dot(logDetailEnabled)}
                {tr("Verbose logs", "日志详情", "日誌詳情")}
            </button>
            <button type="button" data-testid="qs-llmcache-toggle" role="switch" aria-checked={llmCacheEnabled} onClick={() => toggleLlmCache(!llmCacheEnabled)} style={chipStyle(llmCacheEnabled)} title={llmCacheEnabled ? tr("LLM cache ON - click to disable", "LLM 缓存已开启，点击关闭", "LLM 快取已開啟，點擊關閉") : tr("LLM cache OFF - click to enable", "LLM 缓存已关闭，点击开启", "LLM 快取已關閉，點擊開啟")} aria-label={llmCacheEnabled ? tr("LLM cache ON - click to disable", "LLM 缓存已开启，点击关闭", "LLM 快取已開啟，點擊關閉") : tr("LLM cache OFF - click to enable", "LLM 缓存已关闭，点击开启", "LLM 快取已關閉，點擊開啟")}>
                {dot(llmCacheEnabled)}
                {tr("LLM cache", "LLM 缓存", "LLM 快取")}
            </button>
            {onLanguageChange && (
                <button type="button" data-testid="qs-lang-toggle" onClick={() => onLanguageChange(nextLang)} style={chipStyle(false)} title={tr("Switch language", "切换语言", "切換語言")} aria-label={tr("Switch language", "切换语言", "切換語言")}>
                    {langShortLabel(lang)}
                </button>
            )}
            </div>
            {statusSlot || null}
        </div>
    );
});

export default AssistantQuickSettingsBar;
