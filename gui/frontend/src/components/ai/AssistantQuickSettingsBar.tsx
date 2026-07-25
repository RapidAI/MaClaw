/**
 * Bottom quick-settings bar below the chat input: global system switches
 * (model/provider switch, auto routing, TTS, theme, keep-awake, verbose logs,
 * LLM cache, language). Session/window-level actions stay in the title bar.
 * Optional statusSlot rides the same row on the right (shell status / warnings).
 */
import { memo, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { LoadConfig, PatchConfigFields } from "../../../wailsjs/go/main/App";
import { EVENT_MACLAW_CONFIG_CHANGED } from "../../constants/events";
import { AssistantQuickModelMenuPopover } from "./AssistantQuickModelMenuPopover";
import {
    modelIdsEqual,
    resolveQuickModelList,
    resolveQuickModelMenuSections,
} from "./assistantQuickModelMenu";
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

const EMPTY_PROVIDERS: SidebarLLMProviderSummary[] = [];

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

    // Model/provider menu — neutral picker chip; popover is portaled (no green "ON" look).
    const [menuOpen, setMenuOpen] = useState(false);
    const modelChipRef = useRef<HTMLButtonElement | null>(null);
    // State mirror of the chip element so the popover re-measures when the anchor mounts.
    const [modelChipEl, setModelChipEl] = useState<HTMLButtonElement | null>(null);
    const setModelChipRef = useCallback((el: HTMLButtonElement | null) => {
        modelChipRef.current = el;
        setModelChipEl(el);
    }, []);

    const providers = availableProviders ?? EMPTY_PROVIDERS;
    const modelList = useMemo(() => resolveQuickModelList(modelOptions, currentModel), [modelOptions, currentModel]);
    const { currentProvider, switchableProviders, showProviders, showModels } = useMemo(
        () => resolveQuickModelMenuSections({
            providers,
            modelList,
            currentModel,
            modelsLoading,
            hasSwitchModel: !!onSwitchModel,
        }),
        [providers, modelList, currentModel, modelsLoading, onSwitchModel],
    );
    const hasModelMenu = !!(onSwitchProvider || onSwitchModel)
        && (providers.length > 0 || modelList.length > 0 || !!String(currentModel || "").trim());
    const modelChipLabel = String(currentModel || "").trim() || currentProvider?.name || tr("Model", "模型", "模型");

    const closeModelMenu = useCallback(() => {
        setMenuOpen(false);
        // Return focus to the chip after dismiss (Escape / outside click / selection).
        modelChipRef.current?.focus();
    }, []);

    // Drop a stale open state when the picker itself disappears (e.g. LLM went offline).
    useEffect(() => {
        if (!hasModelMenu && menuOpen) setMenuOpen(false);
    }, [hasModelMenu, menuOpen]);

    // Outside-click / Escape / listbox focus live in AssistantQuickModelMenuPopover.

    const openModelMenu = useCallback(() => {
        // Side effect stays out of the state updater (StrictMode double-invokes updaters).
        if (menuOpen) {
            setMenuOpen(false);
            return;
        }
        setMenuOpen(true);
        // Refresh catalog; parent falls back to configured model if fetch fails.
        onOpenModelMenu?.();
    }, [menuOpen, onOpenModelMenu]);

    const handleSelectProvider = useCallback((name: string) => {
        closeModelMenu();
        onSwitchProvider?.(name);
    }, [closeModelMenu, onSwitchProvider]);

    const handleSelectModel = useCallback((modelId: string) => {
        const next = String(modelId || "").trim();
        closeModelMenu();
        if (!next || modelIdsEqual(next, currentModel)) return;
        onSwitchModel?.(next);
    }, [closeModelMenu, currentModel, onSwitchModel]);

    const chipStyle = useCallback((active: boolean): CSSProperties => ({
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
    }), [t.titleBarBorder, t.fieldBg, t.promptColor]);

    // Model chip is a picker, not a boolean switch — never use the green "ON" chip style.
    const modelChipStyle = useMemo((): CSSProperties => ({
        ...chipStyle(false),
        gap: 5,
    }), [chipStyle]);

    const dot = (active: boolean) => (
        <span aria-hidden="true" style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: active ? "#4f7f6f" : t.promptColor, opacity: active ? 1 : 0.4, transition: "all 150ms ease" }} />
    );

    const nextLang = LANG_CYCLE[lang] || "zh-Hans";

    return (
        // Outer row stays non-scrolling so statusSlot can pin to the right while
        // chips alone scroll when the window is narrow.
        // Owns the window bottom edge under the composer. Use minHeight (not fixed
        // height) so safe-area padding extends the bar instead of squeezing chips.
        <div data-testid="assistant-quick-settings-bar" style={{ display: "flex", alignItems: "center", gap: 6, minHeight: 28, padding: "0 10px", paddingBottom: "env(safe-area-inset-bottom, 0px)", borderTop: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, overflow: "hidden", flexShrink: 0, boxSizing: "border-box", minWidth: 0 }}>
            <div data-testid="assistant-quick-settings-chips" style={{ display: "flex", alignItems: "center", gap: 6, flex: "1 1 auto", minWidth: 0, overflowX: "auto", overflowY: "visible" }}>
            {hasModelMenu && (
                <div style={{ position: "relative", flexShrink: 0 }}>
                    <button
                        type="button"
                        ref={setModelChipRef}
                        data-testid="qs-model-chip"
                        onClick={openModelMenu}
                        aria-expanded={menuOpen}
                        aria-haspopup="listbox"
                        style={modelChipStyle}
                        // Accessible name comes from visible model label (do not override with a generic aria-label).
                        title={tr("Switch model or provider", "切换模型或服务商", "切換模型或服務商")}
                    >
                        <span style={{ maxWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{modelChipLabel}</span>
                        <svg
                            width="7"
                            height="7"
                            viewBox="0 0 8 8"
                            aria-hidden="true"
                            focusable="false"
                            style={{ transform: menuOpen ? "rotate(180deg)" : "none", transition: "transform 120ms ease", opacity: 0.75 }}
                        >
                            <path d="M1 3l3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </button>
                    <AssistantQuickModelMenuPopover
                        open={menuOpen}
                        anchorEl={modelChipEl}
                        theme={t}
                        listLabel={tr("Select provider or model", "选择服务商或模型", "選擇服務商或模型")}
                        providersLabel={tr("Providers", "服务商", "服務商")}
                        modelsLabel={tr("Models", "模型", "模型")}
                        loadingModelsLabel={tr("Models (loading…)", "模型（加载中…）", "模型（載入中…）")}
                        emptyModelsLabel={tr("No models listed", "暂无模型列表", "暫無模型列表")}
                        loadingModelsHint={tr("Loading models…", "正在加载模型…", "正在載入模型…")}
                        currentProvider={currentProvider}
                        switchableProviders={switchableProviders}
                        showProviders={showProviders}
                        showModels={showModels}
                        modelList={modelList}
                        currentModel={currentModel}
                        modelsLoading={modelsLoading}
                        onSelectProvider={handleSelectProvider}
                        onSelectModel={handleSelectModel}
                        onClose={closeModelMenu}
                    />
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
