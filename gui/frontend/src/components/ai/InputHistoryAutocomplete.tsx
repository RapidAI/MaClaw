import { memo, useEffect, useRef, type CSSProperties, type ReactNode } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

interface InputHistoryAutocompleteProps {
    open: boolean;
    matches: string[];
    selectedIndex: number;
    /** Current draft — used to bold the completion suffix. */
    prefix?: string;
    /** Stable DOM id shared with the textarea aria-controls / activedescendant. */
    listboxId: string;
    onSelectIndex: (index: number) => void;
    onAccept: (index: number) => void | boolean;
    theme: Theme;
    lang: string;
}

function renderMatchLabel(item: string, prefix: string): ReactNode {
    if (!prefix || !item.startsWith(prefix) || prefix.length >= item.length) {
        return item;
    }
    return (
        <>
            <span style={{ opacity: 0.55 }}>{prefix}</span>
            <span style={{ fontWeight: 600 }}>{item.slice(prefix.length)}</span>
        </>
    );
}

/** Scroll active option into view inside the list only (avoid scrolling the chat panel). */
function scrollOptionIntoList(list: HTMLElement, option: HTMLElement) {
    const listRect = list.getBoundingClientRect();
    const optionRect = option.getBoundingClientRect();
    if (optionRect.top < listRect.top) {
        list.scrollTop -= listRect.top - optionRect.top;
    } else if (optionRect.bottom > listRect.bottom) {
        list.scrollTop += optionRect.bottom - listRect.bottom;
    }
}

/** Popup list of history drafts that share a prefix with the current input. */
export const InputHistoryAutocomplete = memo(function InputHistoryAutocomplete({
    open,
    matches,
    selectedIndex,
    prefix = "",
    listboxId,
    onSelectIndex,
    onAccept,
    theme: t,
    lang,
}: InputHistoryAutocompleteProps) {
    const listRef = useRef<HTMLDivElement>(null);
    const activeRef = useRef<HTMLButtonElement | null>(null);

    useEffect(() => {
        if (!open) return;
        const list = listRef.current;
        const option = activeRef.current;
        if (!list || !option) return;
        scrollOptionIntoList(list, option);
    }, [open, selectedIndex, matches.length]);

    if (!open || matches.length === 0) return null;

    const isZh = !lang?.startsWith("en");
    const dark = t.bg.startsWith("#0") || t.bg.startsWith("#1") || t.bg.startsWith("#2");
    const safeIndex = Math.max(0, Math.min(selectedIndex, matches.length - 1));
    const activeBg = dark ? "rgba(148, 163, 184, 0.16)" : "rgba(47, 95, 152, 0.10)";

    const baseItemStyle: CSSProperties = {
        width: "100%",
        textAlign: "left",
        border: "none",
        borderRadius: 7,
        padding: "8px 10px",
        cursor: "pointer",
        fontSize: 13,
        lineHeight: 1.35,
        color: t.text,
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
        // Cap multi-line history so one item cannot dominate the popup (~3 lines).
        maxHeight: "4.05em",
        overflow: "hidden",
    };

    return (
        <div
            ref={listRef}
            role="listbox"
            id={listboxId}
            aria-label={isZh ? "历史输入补全" : "History autocomplete"}
            data-testid="ai-input-history-autocomplete"
            style={{
                position: "absolute",
                left: 0,
                right: 0,
                bottom: "100%",
                marginBottom: 4,
                maxHeight: 220,
                overflowY: "auto",
                borderRadius: 10,
                border: `1px solid ${t.inputBarBorder || t.fieldBorder}`,
                background: t.inputBarBg || t.bg,
                boxShadow: dark
                    ? "0 12px 32px rgba(0, 0, 0, 0.55)"
                    : "0 12px 28px rgba(15, 23, 42, 0.16)",
                zIndex: 50,
                padding: 4,
            }}
        >
            {matches.map((item, index) => {
                const active = index === safeIndex;
                const optionId = `${listboxId}-option-${index}`;
                return (
                    <button
                        key={`${index}-${item.slice(0, 48)}`}
                        ref={active ? activeRef : undefined}
                        type="button"
                        role="option"
                        id={optionId}
                        aria-selected={active}
                        title={item}
                        data-testid={`ai-input-history-item-${index}`}
                        data-active={active ? "true" : "false"}
                        onMouseEnter={() => onSelectIndex(index)}
                        onMouseDown={(e) => {
                            // Prevent textarea blur before click completes.
                            e.preventDefault();
                        }}
                        onClick={() => onAccept(index)}
                        style={{
                            ...baseItemStyle,
                            background: active ? activeBg : "transparent",
                        }}
                    >
                        {renderMatchLabel(item, prefix)}
                    </button>
                );
            })}
            <div
                aria-hidden="true"
                style={{
                    padding: "4px 10px 2px",
                    fontSize: 11,
                    color: t.textMuted,
                    userSelect: "none",
                }}
            >
                {isZh ? "↑↓ 选择 · Enter / Tab 补全 · Esc 关闭" : "↑↓ select · Enter / Tab complete · Esc dismiss"}
            </div>
        </div>
    );
});
