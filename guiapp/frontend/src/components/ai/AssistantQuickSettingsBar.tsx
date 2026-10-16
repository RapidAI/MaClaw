/**
 * Bottom quick-settings bar below the chat input: global system switches
 * (model/provider switch, TTS, theme, keep-awake, verbose logs,
 * LLM cache, language). Session/window-level actions stay in the title bar.
 * Optional statusSlot rides the same row on the right (shell status / warnings).
 */
import { memo, useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { LoadConfig, PatchConfigFields } from "../../../wailsjs/go/main/App";
import { EVENT_MACLAW_CONFIG_CHANGED } from "../../constants/events";
import { AssistantQuickModelSwitcher } from "./AssistantQuickModelSwitcher";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { TTSLevelBars } from "./TTSLevelBars";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";
import type { SidebarLLMProviderSummary } from "../../types/appShell";
import type { AIExecutionProfile } from "./AITabTypes";

type Props = {
    lang: string;
    theme: Theme;
    themeMode: "light" | "dark";
    /** Whether the retained assistant panel is visible in the app shell. */
    active?: boolean;
    onToggleTheme: () => void;
    ttsEnabled: boolean;
    ttsPlaying: boolean;
    onToggleTts: () => void;
    availableProviders?: SidebarLLMProviderSummary[];
    currentModel?: string;
    modelOptions?: string[];
    modelMultipliers?: Record<string, number>;
    modelsLoading?: boolean;
    /** Stable provider id (legacy callers may supply a display name as fallback). */
    onSwitchProvider?: (providerID: string) => void;
    onSwitchModel?: (modelId: string) => void;
    onOpenModelMenu?: () => void;
    /** Discards an uncommitted provider choice when this picker is dismissed. */
    onDismissModelMenu?: () => void;
    activeProfile?: AIExecutionProfile;
    codingInheritsAssistant?: boolean;
    /** A provider is staged until the user chooses its model. */
    providerSelectionPending?: boolean;
    /** An atomic profile update is in flight; keep the picker non-interactive. */
    profileSavePending?: boolean;
    onOpenLLMSettings?: () => void;
    onLanguageChange?: (lang: string) => void;
    /** Shell status cluster (inline AppStatusMessageBar); right side of this row. */
    statusSlot?: ReactNode;
    /** Hide the bar without unmounting (search overlay owns the assistant pane). */
    hidden?: boolean;
    /** The composer toolbar owns the switcher, so this bar should not repeat it. */
    hideModelChip?: boolean;
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

export const AssistantQuickSettingsBar = memo(function AssistantQuickSettingsBar({ lang, theme: t, themeMode, active = true, onToggleTheme, ttsEnabled, ttsPlaying, onToggleTts, availableProviders, currentModel, modelOptions, modelMultipliers, modelsLoading, onSwitchProvider, onSwitchModel, onOpenModelMenu, onDismissModelMenu, activeProfile = "assistant", codingInheritsAssistant = false, providerSelectionPending = false, profileSavePending = false, onOpenLLMSettings, onLanguageChange, statusSlot, hidden = false, hideModelChip = false }: Props) {
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
        border: active ? "1px solid color-mix(in srgb, var(--theme-success, #4f7f6f) 36%, transparent)" : `1px solid ${t.titleBarBorder}`,
        background: active ? "color-mix(in srgb, var(--theme-success, #4f7f6f) 12%, transparent)" : t.fieldBg,
        color: active ? "var(--theme-success, #4f7f6f)" : t.promptColor,
        transition: "all 150ms ease",
        flexShrink: 0,
        height: 20,
    }), [t.titleBarBorder, t.fieldBg, t.promptColor]);

    const dot = (active: boolean) => (
        <span aria-hidden="true" style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: active ? "var(--theme-success, #4f7f6f)" : t.promptColor, opacity: active ? 1 : 0.4, transition: "all 150ms ease" }} />
    );

    const nextLang = LANG_CYCLE[lang] || "zh-Hans";

    return (
        // Outer row stays non-scrolling so statusSlot can pin to the right while
        // chips alone scroll when the window is narrow.
        // Owns the window bottom edge under the composer. Use minHeight (not fixed
        // height) so safe-area padding extends the bar instead of squeezing chips.
        <div data-testid="assistant-quick-settings-bar" hidden={hidden} aria-hidden={hidden || undefined} style={{ display: hidden ? "none" : "flex", alignItems: "center", gap: 6, minHeight: 28, padding: "0 10px", paddingBottom: "env(safe-area-inset-bottom, 0px)", borderTop: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, overflow: "hidden", flexShrink: 0, boxSizing: "border-box", minWidth: 0 }}>
            <div data-testid="assistant-quick-settings-chips" className="aqs-chips">
            {!hideModelChip && (
                <AssistantQuickModelSwitcher
                    lang={lang}
                    theme={t}
                    active={active}
                    availableProviders={availableProviders}
                    currentModel={currentModel}
                    modelOptions={modelOptions}
                    modelMultipliers={modelMultipliers}
                    modelsLoading={modelsLoading}
                    onSwitchProvider={onSwitchProvider}
                    onSwitchModel={onSwitchModel}
                    onOpenModelMenu={onOpenModelMenu}
                    onDismissModelMenu={onDismissModelMenu}
                    activeProfile={activeProfile}
                    codingInheritsAssistant={codingInheritsAssistant}
                    providerSelectionPending={providerSelectionPending}
                    profileSavePending={profileSavePending}
                    onOpenLLMSettings={onOpenLLMSettings}
                />
            )}
            <button type="button" data-testid="qs-tts-toggle" role="switch" aria-checked={!!ttsEnabled} onClick={onToggleTts} style={{ ...chipStyle(!!ttsEnabled), position: "relative" }} title={ttsEnabled ? tr("Voice readback ON - click to disable", "语音播报已开启，点击关闭", "語音播報已開啟，點擊關閉") : tr("Voice readback OFF - click to enable", "语音播报已关闭，点击开启", "語音播報已關閉，點擊開啟")} aria-label={ttsEnabled ? tr("Voice readback ON - click to disable", "语音播报已开启，点击关闭", "語音播報已開啟，點擊關閉") : tr("Voice readback OFF - click to enable", "语音播报已关闭，点击开启", "語音播報已關閉，點擊開啟")}>
                <span aria-hidden="true" style={{ display: "inline-flex", opacity: ttsPlaying ? 0 : 1, transition: "opacity 150ms" }}>
                    <TitleBarToolIcon name={ttsEnabled ? "volumeOn" : "volumeOff"} />
                </span>
                {ttsPlaying && <span aria-hidden="true" className="aqs-tts-bars"><TTSLevelBars accentColor={t.headingColor} /></span>}
                {tr("Voice", "语音", "語音")}
            </button>
            <button type="button" data-testid="qs-theme-toggle" onClick={onToggleTheme} style={chipStyle(themeMode === "dark")} title={themeMode === "dark" ? tr("Switch to light mode", "切换到普通模式", "切換到普通模式") : tr("Switch to dark mode", "切换到暗黑模式", "切換到暗黑模式")} aria-label={themeMode === "dark" ? tr("Switch to light mode", "切换到普通模式", "切換到普通模式") : tr("Switch to dark mode", "切换到暗黑模式", "切換到暗黑模式")}>
                <span aria-hidden="true" className="aqs-icon">
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
