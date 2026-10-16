/**
 * Chip that opens the provider and model picker. Used in the assistant
 * composer toolbar (the gap before send) and, by default, the quick-settings bar.
 */
import { memo, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { AssistantQuickModelMenuPopover } from "./AssistantQuickModelMenuPopover";
import {
    modelIdsEqual,
    resolveQuickModelList,
    resolveQuickModelMenuSections,
} from "./assistantQuickModelMenu";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import type { SidebarLLMProviderSummary } from "../../types/appShell";
import { capabilityModelMenuLabel, capabilityMultiplierFor } from "../../utils/capabilityModelLabel";
import type { AIExecutionProfile } from "./AITabTypes";

const EMPTY_PROVIDERS: SidebarLLMProviderSummary[] = [];

export type AssistantQuickModelSwitcherProps = {
    lang: string;
    theme: Theme;
    active?: boolean;
    availableProviders?: SidebarLLMProviderSummary[];
    currentModel?: string;
    modelOptions?: string[];
    modelMultipliers?: Record<string, number>;
    modelsLoading?: boolean;
    onSwitchProvider?: (providerID: string) => void;
    onSwitchModel?: (modelId: string) => void;
    onOpenModelMenu?: () => void;
    onDismissModelMenu?: () => void;
    activeProfile?: AIExecutionProfile;
    codingInheritsAssistant?: boolean;
    providerSelectionPending?: boolean;
    profileSavePending?: boolean;
    onOpenLLMSettings?: () => void;
    chipTestId?: string;
};

export const AssistantQuickModelSwitcher = memo(function AssistantQuickModelSwitcher({
    lang,
    theme: t,
    active = true,
    availableProviders,
    currentModel,
    modelOptions,
    modelMultipliers,
    modelsLoading,
    onSwitchProvider,
    onSwitchModel,
    onOpenModelMenu,
    onDismissModelMenu,
    activeProfile = "assistant",
    codingInheritsAssistant = false,
    providerSelectionPending = false,
    profileSavePending = false,
    onOpenLLMSettings,
    chipTestId = "qs-model-chip",
}: AssistantQuickModelSwitcherProps) {
    const tr = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang],
    );
    const [menuOpen, setMenuOpen] = useState(false);
    const menuOpenRef = useRef(false);
    menuOpenRef.current = menuOpen;
    const dismissRef = useRef(onDismissModelMenu);
    dismissRef.current = onDismissModelMenu;
    const savePendingRef = useRef(profileSavePending);
    savePendingRef.current = profileSavePending;
    const modelChipRef = useRef<HTMLButtonElement | null>(null);
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
    const isReadOnlyFollowingCoding = activeProfile === "coding" && codingInheritsAssistant;
    const hasModelMenu = activeProfile !== "none" && (isReadOnlyFollowingCoding || !!(onSwitchProvider || onSwitchModel))
        && (providers.length > 0 || modelList.length > 0 || !!String(currentModel || "").trim());
    const selectedModel = String(currentModel || "").trim();
    const showCapabilityFee = currentProvider?.isHubService === true;
    const selectedModelLabel = selectedModel
        ? capabilityModelMenuLabel(selectedModel, capabilityMultiplierFor(selectedModel, modelMultipliers), showCapabilityFee)
        : "";
    const roleLabel = activeProfile === "coding"
        ? tr("Coding", "编程", "編程")
        : tr("Assistant", "助手", "助手");
    const providerName = String(currentProvider?.name || "").trim();
    const modelPart = selectedModelLabel || (providerName ? "" : tr("Model", "模型", "模型"));
    const normalModelChipLabel = isReadOnlyFollowingCoding
        ? `${tr("Coding · Follows assistant", "编程 · 跟随助手", "編程 · 跟隨助手")}${selectedModelLabel ? ` · ${selectedModelLabel}` : ""}`
        : [roleLabel, providerName, modelPart].filter(Boolean).join(" · ");
    const modelChipLabel = providerSelectionPending && !isReadOnlyFollowingCoding
        ? `${roleLabel} · ${currentProvider?.name || tr("Provider", "服务商", "服務商")} · ${tr("choose model", "选择模型", "選擇模型")}`
        : normalModelChipLabel;

    const closeModelMenu = useCallback(() => {
        setMenuOpen(false);
        if (!profileSavePending) onDismissModelMenu?.();
        modelChipRef.current?.focus();
    }, [onDismissModelMenu, profileSavePending]);

    useEffect(() => {
        if (menuOpen && (!active || !hasModelMenu)) {
            setMenuOpen(false);
            if (!profileSavePending) onDismissModelMenu?.();
        }
    }, [active, hasModelMenu, menuOpen, onDismissModelMenu, profileSavePending]);

    // Welcome, chat, and VE each mount their own chip. Leaving a view while the
    // menu is open must drop the staged provider; otherwise the next chip opens
    // already pointing at a choice the user never confirmed.
    useEffect(() => {
        return () => {
            if (menuOpenRef.current && !savePendingRef.current) dismissRef.current?.();
        };
    }, []);

    const openModelMenu = useCallback(() => {
        if (profileSavePending) return;
        if (isReadOnlyFollowingCoding) {
            onOpenLLMSettings?.();
            return;
        }
        if (menuOpen) {
            setMenuOpen(false);
            onDismissModelMenu?.();
            return;
        }
        setMenuOpen(true);
        onOpenModelMenu?.();
    }, [isReadOnlyFollowingCoding, menuOpen, onDismissModelMenu, onOpenLLMSettings, onOpenModelMenu, profileSavePending]);

    const handleSelectProvider = useCallback((name: string) => {
        if (profileSavePending) return;
        onSwitchProvider?.(name);
    }, [onSwitchProvider, profileSavePending]);

    const handleSelectModel = useCallback((modelId: string) => {
        const next = String(modelId || "").trim();
        if (!next) return;
        // A provider switch may still be saving. Forward the model anyway so
        // it replaces that default once the first write finishes, and keep the
        // menu from dismissing the in-flight choice.
        if (profileSavePending) {
            setMenuOpen(false);
            onSwitchModel?.(next);
            return;
        }
        closeModelMenu();
        if (modelIdsEqual(next, currentModel)) return;
        onSwitchModel?.(next);
    }, [closeModelMenu, currentModel, onSwitchModel, profileSavePending]);

    const modelChipStyle = useMemo((): CSSProperties => ({
        display: "inline-flex",
        alignItems: "center",
        gap: 5,
        maxWidth: 220,
        padding: "2px 8px",
        borderRadius: 999,
        fontSize: 11,
        fontWeight: 650,
        lineHeight: 1.2,
        cursor: profileSavePending ? "default" : "pointer",
        userSelect: "none",
        border: `1px solid ${t.titleBarBorder}`,
        background: t.fieldBg,
        color: t.promptColor,
        flexShrink: 1,
        minWidth: 0,
        height: 22,
    }), [profileSavePending, t.fieldBg, t.promptColor, t.titleBarBorder]);

    if (!hasModelMenu) return null;

    return (
        <div className="aqs-model-wrap" style={{ minWidth: 0, maxWidth: "100%" }}>
            <button
                type="button"
                ref={setModelChipRef}
                className="mc-composer-model-chip"
                data-testid={chipTestId}
                onClick={openModelMenu}
                disabled={profileSavePending}
                aria-expanded={isReadOnlyFollowingCoding ? undefined : menuOpen}
                aria-haspopup={isReadOnlyFollowingCoding ? undefined : "listbox"}
                style={modelChipStyle}
                title={isReadOnlyFollowingCoding
                    ? tr("View coding model settings", "查看编程模型设置", "檢視編程模型設定")
                    : tr("Switch model or provider", "切换模型或服务商", "切換模型或服務商")}
            >
                <span className="aqs-model-label">{modelChipLabel}</span>
                <svg
                    width="7"
                    height="7"
                    viewBox="0 0 8 8"
                    aria-hidden="true"
                    focusable="false"
                    style={{ transform: menuOpen ? "rotate(180deg)" : "none", transition: "transform 120ms ease", opacity: 0.75, flexShrink: 0 }}
                >
                    <path d="M1 3l3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
            </button>
            {!isReadOnlyFollowingCoding && (
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
                    modelMultipliers={modelMultipliers}
                    showCapabilityDefaults={showCapabilityFee}
                    currentModel={currentModel}
                    modelsLoading={modelsLoading}
                    onSelectProvider={handleSelectProvider}
                    onSelectModel={handleSelectModel}
                    onClose={closeModelMenu}
                />
            )}
        </div>
    );
});
