/**
 * Fixed-position model/provider picker popover for AssistantQuickSettingsBar.
 * Portaled to document.body so parent overflow cannot clip it.
 */
import {
    memo,
    useCallback,
    useEffect,
    useId,
    useLayoutEffect,
    useMemo,
    useRef,
    useState,
    type CSSProperties,
    type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import {
    computeProviderDropdownPos,
    providerDropdownPosEqual,
    type ProviderDropdownPos,
} from "../layout/sidebarProviderDropdownPos";
import { modelIdsEqual } from "./assistantQuickModelMenu";
import type { Theme } from "./aiAssistantPanelTheme";
import type { SidebarLLMProviderSummary } from "../../types/appShell";

const MENU_MIN_WIDTH = 200;
const MENU_MAX_WIDTH = 300;

type MenuAction =
    | { kind: "provider"; id: string }
    | { kind: "model"; id: string };

export type AssistantQuickModelMenuPopoverProps = {
    open: boolean;
    anchorEl: HTMLElement | null;
    theme: Theme;
    listLabel: string;
    providersLabel: string;
    modelsLabel: string;
    loadingModelsLabel: string;
    emptyModelsLabel: string;
    loadingModelsHint: string;
    currentProvider: SidebarLLMProviderSummary | null;
    switchableProviders: SidebarLLMProviderSummary[];
    showProviders: boolean;
    showModels: boolean;
    modelList: string[];
    currentModel?: string;
    modelsLoading?: boolean;
    /** Stable provider id (legacy callers may supply a display name as fallback). */
    onSelectProvider: (providerID: string) => void;
    onSelectModel: (modelId: string) => void;
    onClose: () => void;
};

const labelStyleBase: CSSProperties = {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

const checkColStyle: CSSProperties = {
    width: 12,
    flexShrink: 0,
    textAlign: "center",
    opacity: 0.7,
};

function buildActions(
    showProviders: boolean,
    switchableProviders: SidebarLLMProviderSummary[],
    showModels: boolean,
    modelList: string[],
): MenuAction[] {
    const next: MenuAction[] = [];
    if (showProviders) {
        for (const p of switchableProviders) next.push({ kind: "provider", id: String(p.id || p.name).trim() });
    }
    if (showModels) {
        for (const m of modelList) next.push({ kind: "model", id: m });
    }
    return next;
}

export const AssistantQuickModelMenuPopover = memo(function AssistantQuickModelMenuPopover({
    open,
    anchorEl,
    theme: t,
    listLabel,
    providersLabel,
    modelsLabel,
    loadingModelsLabel,
    emptyModelsLabel,
    loadingModelsHint,
    currentProvider,
    switchableProviders,
    showProviders,
    showModels,
    modelList,
    currentModel,
    modelsLoading,
    onSelectProvider,
    onSelectModel,
    onClose,
}: AssistantQuickModelMenuPopoverProps) {
    const listRef = useRef<HTMLDivElement | null>(null);
    const [pos, setPos] = useState<ProviderDropdownPos | null>(null);
    const [activeIndex, setActiveIndex] = useState(0);
    // Keep a ref in sync so Enter/Space never activate a stale index after Arrow keys.
    const activeIndexRef = useRef(0);
    const listId = useId();
    const repositionRafRef = useRef<number | null>(null);
    const wasOpenRef = useRef(false);
    const skipScrollRef = useRef(true);

    const actions = useMemo(
        () => buildActions(showProviders, switchableProviders, showModels, modelList),
        [showProviders, switchableProviders, showModels, modelList],
    );
    const actionsRef = useRef(actions);
    actionsRef.current = actions;

    const setActive = useCallback((index: number) => {
        const len = actionsRef.current.length;
        const next = len === 0 ? 0 : Math.max(0, Math.min(index, len - 1));
        activeIndexRef.current = next;
        setActiveIndex(next);
    }, []);

    const updatePos = useCallback(() => {
        if (!anchorEl) return;
        const measured = listRef.current?.offsetWidth;
        const next = computeProviderDropdownPos(anchorEl.getBoundingClientRect(), {
            viewportWidth: window.innerWidth,
            viewportHeight: window.innerHeight,
            menuWidth: measured && measured > 0 ? measured : MENU_MAX_WIDTH,
        });
        setPos((prev) => (providerDropdownPosEqual(prev, next) ? prev : next));
    }, [anchorEl]);

    useEffect(() => {
        if (!open) {
            wasOpenRef.current = false;
            setPos(null);
            activeIndexRef.current = 0;
            setActiveIndex(0);
            skipScrollRef.current = true;
            return;
        }
        if (!wasOpenRef.current) {
            wasOpenRef.current = true;
            skipScrollRef.current = true;
            const selectedModelIdx = actions.findIndex(
                (a) => a.kind === "model" && modelIdsEqual(a.id, currentModel),
            );
            setActive(selectedModelIdx >= 0 ? selectedModelIdx : 0);
            return;
        }
        // Catalog refresh while open: clamp, don't yank focus back to current model.
        setActive(activeIndexRef.current);
    }, [open, actions, currentModel, setActive]);

    useLayoutEffect(() => {
        if (!open) return;
        updatePos();
        const raf = requestAnimationFrame(() => updatePos());
        return () => cancelAnimationFrame(raf);
    }, [open, updatePos, actions.length, modelsLoading, showProviders, showModels, currentModel]);

    useEffect(() => {
        if (!open) return;
        const schedule = () => {
            if (repositionRafRef.current != null) return;
            repositionRafRef.current = requestAnimationFrame(() => {
                repositionRafRef.current = null;
                updatePos();
            });
        };
        window.addEventListener("resize", schedule);
        window.addEventListener("scroll", schedule, true);
        return () => {
            window.removeEventListener("resize", schedule);
            window.removeEventListener("scroll", schedule, true);
            if (repositionRafRef.current != null) {
                cancelAnimationFrame(repositionRafRef.current);
                repositionRafRef.current = null;
            }
        };
    }, [open, updatePos]);

    // Outside click + Escape owned here (parent only toggles the chip).
    useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent) => {
            const target = e.target as Node;
            if (anchorEl?.contains(target)) return;
            if (listRef.current?.contains(target)) return;
            onClose();
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            e.preventDefault();
            e.stopPropagation();
            onClose();
        };
        document.addEventListener("mousedown", onDown);
        document.addEventListener("keydown", onKey);
        return () => {
            document.removeEventListener("mousedown", onDown);
            document.removeEventListener("keydown", onKey);
        };
    }, [open, anchorEl, onClose]);

    // Focus listbox once open so arrow keys work without an extra Tab.
    useEffect(() => {
        if (!open) return;
        const id = requestAnimationFrame(() => listRef.current?.focus());
        return () => cancelAnimationFrame(id);
    }, [open]);

    const activate = useCallback((index: number) => {
        const action = actionsRef.current[index];
        if (!action) return;
        if (action.kind === "provider") onSelectProvider(action.id);
        else onSelectModel(action.id);
    }, [onSelectProvider, onSelectModel]);

    const onKeyDown = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
        const list = actionsRef.current;
        if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onClose();
            return;
        }
        if (list.length === 0) return;

        if (e.key === "ArrowDown") {
            e.preventDefault();
            skipScrollRef.current = false;
            setActive((activeIndexRef.current + 1) % list.length);
            return;
        }
        if (e.key === "ArrowUp") {
            e.preventDefault();
            skipScrollRef.current = false;
            setActive((activeIndexRef.current - 1 + list.length) % list.length);
            return;
        }
        if (e.key === "Home") {
            e.preventDefault();
            skipScrollRef.current = false;
            setActive(0);
            return;
        }
        if (e.key === "End") {
            e.preventDefault();
            skipScrollRef.current = false;
            setActive(list.length - 1);
            return;
        }
        if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            activate(activeIndexRef.current);
        }
    }, [activate, onClose, setActive]);

    // Scroll only after user moves highlight (not on initial open — avoids page jump).
    useEffect(() => {
        if (!open || skipScrollRef.current) return;
        const el = listRef.current?.querySelector<HTMLElement>(`[data-qs-option-index="${activeIndex}"]`);
        el?.scrollIntoView({ block: "nearest" });
    }, [activeIndex, open]);

    const surfaceStyle = useMemo((): CSSProperties => ({
        position: "fixed",
        minWidth: MENU_MIN_WIDTH,
        maxWidth: MENU_MAX_WIDTH,
        overflowY: "auto",
        background: t.titleBarBg,
        border: `1px solid ${t.titleBarBorder}`,
        borderRadius: 8,
        boxShadow: "0 8px 24px rgba(15, 23, 42, 0.18)",
        zIndex: 50000,
        padding: 4,
        boxSizing: "border-box",
        outline: "none",
    }), [t.titleBarBg, t.titleBarBorder]);

    const sectionLabelStyle = useMemo((): CSSProperties => ({
        padding: "4px 8px 2px",
        fontSize: 9,
        fontWeight: 700,
        letterSpacing: "0.04em",
        textTransform: "uppercase",
        color: t.promptColor,
    }), [t.promptColor]);

    const separatorStyle = useMemo((): CSSProperties => ({
        height: 1,
        margin: "4px 6px",
        background: t.titleBarBorder,
    }), [t.titleBarBorder]);

    const itemStyle = useCallback((opts: { active: boolean; focused: boolean; interactive: boolean }): CSSProperties => ({
        display: "flex",
        alignItems: "center",
        gap: 6,
        width: "100%",
        boxSizing: "border-box",
        padding: "6px 8px",
        border: "none",
        borderRadius: 6,
        background: opts.focused
            ? "rgba(127, 127, 127, 0.18)"
            : opts.active
                ? "rgba(127, 127, 127, 0.12)"
                : "transparent",
        color: t.text,
        fontSize: 12,
        fontWeight: opts.active ? 600 : 400,
        lineHeight: 1.3,
        cursor: opts.interactive ? "pointer" : "default",
        textAlign: "left",
    }), [t.text]);

    if (!open || typeof document === "undefined" || !document.body) return null;

    // Prefer measured free space; never invent a tall box that overflows the viewport strip.
    const maxHeight = pos == null ? 280 : Math.max(40, pos.maxHeight);

    return createPortal(
        <div
            ref={listRef}
            id={listId}
            role="listbox"
            tabIndex={-1}
            data-testid="qs-model-menu"
            aria-label={listLabel}
            aria-activedescendant={actions[activeIndex] ? `${listId}-opt-${activeIndex}` : undefined}
            onKeyDown={onKeyDown}
            style={{
                ...surfaceStyle,
                left: pos?.left ?? 0,
                top: pos?.top == null ? "auto" : pos.top,
                bottom: pos?.bottom == null ? "auto" : pos.bottom,
                maxHeight,
                visibility: pos ? "visible" : "hidden",
                pointerEvents: pos ? "auto" : "none",
            }}
        >
            {showProviders && currentProvider && (
                <>
                    <div aria-hidden="true" style={sectionLabelStyle}>{providersLabel}</div>
                    {/* Current provider is context only — not a selectable option. */}
                    <div
                        data-testid="qs-model-menu-current-provider"
                        style={itemStyle({ active: true, focused: false, interactive: false })}
                        title={currentProvider.name}
                    >
                        <span aria-hidden="true" style={checkColStyle}>✓</span>
                        <span style={labelStyleBase}>{currentProvider.name}</span>
                    </div>
                    {switchableProviders.map((p, i) => {
                        const index = i; // providers come first in actions
                        const focused = index === activeIndex;
                        return (
                            <button
                                key={String(p.id || p.name)}
                                id={`${listId}-opt-${index}`}
                                type="button"
                                role="option"
                                aria-selected={false}
                                data-qs-option-index={index}
                                style={itemStyle({ active: false, focused, interactive: true })}
                                title={p.name}
                                onMouseEnter={() => {
                                    skipScrollRef.current = true;
                                    setActive(index);
                                }}
                                onClick={() => onSelectProvider(String(p.id || p.name).trim())}
                            >
                                <span aria-hidden="true" style={checkColStyle} />
                                <span style={labelStyleBase}>{p.name}</span>
                            </button>
                        );
                    })}
                </>
            )}
            {showModels && (
                <>
                    {showProviders && <div aria-hidden="true" style={separatorStyle} />}
                    <div aria-hidden="true" style={sectionLabelStyle}>
                        {modelsLoading ? loadingModelsLabel : modelsLabel}
                    </div>
                    {modelList.map((modelId, modelIdx) => {
                        const index = (showProviders ? switchableProviders.length : 0) + modelIdx;
                        const active = modelIdsEqual(modelId, currentModel);
                        const focused = index === activeIndex;
                        return (
                            <button
                                key={modelId}
                                id={`${listId}-opt-${index}`}
                                type="button"
                                role="option"
                                aria-selected={active}
                                data-qs-option-index={index}
                                style={itemStyle({ active, focused, interactive: true })}
                                title={modelId}
                                onMouseEnter={() => {
                                    skipScrollRef.current = true;
                                    setActive(index);
                                }}
                                onClick={() => onSelectModel(modelId)}
                            >
                                <span aria-hidden="true" style={checkColStyle}>{active ? "✓" : ""}</span>
                                <span style={labelStyleBase}>{modelId}</span>
                            </button>
                        );
                    })}
                    {modelsLoading && modelList.length === 0 && (
                        <div style={itemStyle({ active: false, focused: false, interactive: false })}>
                            <span style={{ ...labelStyleBase, opacity: 0.65 }}>{loadingModelsHint}</span>
                        </div>
                    )}
                    {!modelsLoading && modelList.length === 0 && (
                        <div style={itemStyle({ active: false, focused: false, interactive: false })}>
                            <span style={{ ...labelStyleBase, opacity: 0.65 }}>{emptyModelsLabel}</span>
                        </div>
                    )}
                </>
            )}
        </div>,
        document.body,
    );
});

export default AssistantQuickModelMenuPopover;
