import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type InputHTMLAttributes, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { filterSuggestOptions, normalizeSuggestOption, shouldIgnoreSuggestInput, type SuggestOption } from "./suggestOptions";

type Props = {
    value: string;
    options: ReadonlyArray<string | SuggestOption>;
    onChange: (value: string) => void;
    open?: boolean;
    onOpenChange?: (open: boolean) => void;
    disabled?: boolean;
    placeholder?: string;
    ariaLabel?: string;
    listboxId?: string;
    toggleLabel: string;
    emptyText?: string;
    openOnEnter?: boolean;
    shellClassName?: string;
    shellStyle?: CSSProperties;
    inputClassName?: string;
    toggleClassName?: string;
    listClassName?: string;
    optionClassName?: string;
    optionValueClassName?: string;
    optionDescriptionClassName?: string;
    inputStyle?: CSSProperties;
    inputProps?: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "type" | "role"> & {
        "data-field"?: string;
    };
    onKeyDown?: (event: KeyboardEvent<HTMLInputElement>) => void;
};

type ListBox = {
    left: number;
    width: number;
    maxHeight: number;
    top: number | "auto";
    bottom: number | "auto";
};

const fieldBorder = "1px solid var(--theme-border)";

function measureListBox(anchor: HTMLElement): ListBox {
    const rect = anchor.getBoundingClientRect();
    const gap = 4;
    const margin = 8;
    const width = Math.max(0, Math.min(rect.width, window.innerWidth - margin * 2));
    const left = Math.max(margin, Math.min(rect.left, window.innerWidth - width - margin));
    const spaceBelow = window.innerHeight - rect.bottom - gap;
    const spaceAbove = rect.top - gap;
    const openUp = spaceBelow < 160 && spaceAbove > spaceBelow;
    const available = Math.max(0, openUp ? spaceAbove : spaceBelow);
    return {
        left,
        width,
        maxHeight: Math.max(80, Math.min(220, available || 80)),
        top: openUp ? "auto" : rect.bottom + gap,
        bottom: openUp ? window.innerHeight - rect.top + gap : "auto",
    };
}

function sameListBox(a: ListBox | null, b: ListBox): boolean {
    return !!a && a.left === b.left && a.width === b.width && a.maxHeight === b.maxHeight && a.top === b.top && a.bottom === b.bottom;
}

export function SuggestCombobox({
    value,
    options,
    onChange,
    open: openProp,
    onOpenChange,
    disabled = false,
    placeholder,
    ariaLabel,
    listboxId: listboxIdProp,
    toggleLabel,
    emptyText,
    openOnEnter = false,
    shellClassName,
    shellStyle,
    inputClassName,
    toggleClassName,
    listClassName,
    optionClassName,
    optionValueClassName,
    optionDescriptionClassName,
    inputStyle,
    inputProps,
    onKeyDown,
}: Props) {
    const generatedId = useId();
    const listboxId = listboxIdProp || `suggest-combobox-${generatedId}`;
    const shellRef = useRef<HTMLDivElement>(null);
    const inputRef = useRef<HTMLInputElement>(null);
    const [uncontrolledOpen, setUncontrolledOpen] = useState(false);
    const [typedQuery, setTypedQuery] = useState<string | null>(null);
    const [activeIndex, setActiveIndex] = useState(0);
    const [listBox, setListBox] = useState<ListBox | null>(null);
    const isControlled = openProp !== undefined;
    const open = isControlled ? !!openProp : uncontrolledOpen;
    const normalized = options.map(normalizeSuggestOption);
    const hasOptions = normalized.length > 0;
    const visible = filterSuggestOptions(normalized, typedQuery);
    const optionKey = visible.map((option) => option.value).join("\0");
    const highlightKey = `${open ? 1 : 0}|${typedQuery ?? `\0${value}`}|${optionKey}`;
    const [highlightKeySeen, setHighlightKeySeen] = useState(highlightKey);
    let safeIndex = activeIndex;
    if (highlightKeySeen !== highlightKey) {
        const current = typedQuery == null ? visible.findIndex((option) => option.value === value) : 0;
        safeIndex = current >= 0 ? current : 0;
        setHighlightKeySeen(highlightKey);
        setActiveIndex(safeIndex);
    }
    if (visible.length === 0) safeIndex = 0;
    else if (safeIndex >= visible.length) safeIndex = visible.length - 1;
    else if (safeIndex < 0) safeIndex = 0;

    const setOpen = (next: boolean) => {
        if (!isControlled) setUncontrolledOpen(next);
        onOpenChange?.(next);
    };
    const showAll = () => setTypedQuery(null);
    const choose = (next: string) => {
        onChange(next);
        setOpen(false);
    };

    useEffect(() => {
        if (open) return;
        setTypedQuery(null);
    }, [open]);

    useLayoutEffect(() => {
        if (!open || !hasOptions) return;
        const place = () => {
            const anchor = shellRef.current;
            if (!anchor) return;
            const next = measureListBox(anchor);
            setListBox((prev) => sameListBox(prev, next) ? prev : next);
        };
        place();
        let frame = 0;
        const onMove = (event: Event) => {
            const list = document.getElementById(listboxId);
            if (list && event.target instanceof Node && (event.target === list || list.contains(event.target))) return;
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(place);
        };
        window.addEventListener("resize", onMove);
        window.addEventListener("scroll", onMove, true);
        return () => {
            cancelAnimationFrame(frame);
            window.removeEventListener("resize", onMove);
            window.removeEventListener("scroll", onMove, true);
        };
    }, [open, hasOptions, visible.length, listboxId]);

    const scrolledOption = useRef("");
    useEffect(() => {
        if (!open || !listBox) {
            scrolledOption.current = "";
            return;
        }
        const token = `${listboxId}:${safeIndex}`;
        if (scrolledOption.current === token) return;
        scrolledOption.current = token;
        const option = document.getElementById(`${listboxId}-opt-${safeIndex}`);
        const list = document.getElementById(listboxId);
        if (!option || !list) return;
        const top = option.offsetTop;
        const bottom = top + option.offsetHeight;
        if (top < list.scrollTop) list.scrollTop = top;
        else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight;
    }, [open, listBox, listboxId, safeIndex]);

    const revealAll = () => {
        showAll();
        setOpen(true);
    };

    return (
        <div
            ref={shellRef}
            className={shellClassName}
            style={{
                ...(shellClassName ? { position: "relative" } : {
                    position: "relative",
                    display: "flex",
                    width: "100%",
                    minWidth: 0,
                    alignItems: "stretch",
                }),
                ...shellStyle,
            }}
            onBlur={(event) => {
                const nextFocus = event.relatedTarget as Node | null;
                if (!nextFocus || !event.currentTarget.contains(nextFocus)) setOpen(false);
            }}
        >
            <input
                {...inputProps}
                ref={inputRef}
                type="text"
                className={inputClassName}
                role="combobox"
                aria-autocomplete="list"
                aria-haspopup="listbox"
                aria-expanded={open && hasOptions}
                aria-controls={listboxId}
                aria-activedescendant={open && visible[safeIndex] ? `${listboxId}-opt-${safeIndex}` : undefined}
                aria-label={ariaLabel}
                value={value}
                disabled={disabled}
                placeholder={placeholder}
                autoCapitalize="off"
                autoCorrect="off"
                spellCheck={false}
                autoComplete="off"
                style={inputClassName ? { flex: 1, minWidth: 0, width: "auto" } : {
                    ...inputStyle,
                    flex: 1,
                    width: "auto",
                    minWidth: 0,
                    borderTopRightRadius: 0,
                    borderBottomRightRadius: 0,
                }}
                onChange={(event) => {
                    const next = event.target.value;
                    const inputType = (event.nativeEvent as InputEvent | undefined)?.inputType;
                    if (shouldIgnoreSuggestInput(next, value, inputType)) {
                        if (event.currentTarget.value !== value) event.currentTarget.value = value;
                        return;
                    }
                    setTypedQuery(next);
                    onChange(next);
                    if (hasOptions) setOpen(true);
                }}
                onFocus={() => {
                    if (disabled || !hasOptions || open) return;
                    revealAll();
                }}
                onKeyDown={(event) => {
                    if (event.key === "Escape") {
                        setOpen(false);
                        onKeyDown?.(event);
                        return;
                    }
                    if ((event.key === "ArrowDown" || event.key === "ArrowUp") && hasOptions && !disabled) {
                        event.preventDefault();
                        if (!open) {
                            revealAll();
                            return;
                        }
                        if (visible.length === 0) return;
                        const delta = event.key === "ArrowDown" ? 1 : -1;
                        const count = visible.length;
                        setActiveIndex((current) => {
                            const base = current >= 0 && current < count ? current : safeIndex;
                            return (base + delta + count) % count;
                        });
                        return;
                    }
                    if (event.key === "Enter" && open && openOnEnter && visible[safeIndex]) {
                        event.preventDefault();
                        choose(visible[safeIndex].value);
                        return;
                    }
                    if (event.key === "Enter" && openOnEnter && hasOptions && !disabled && !open) {
                        event.preventDefault();
                        revealAll();
                        return;
                    }
                    onKeyDown?.(event);
                }}
            />
            <button
                type="button"
                className={toggleClassName}
                aria-label={toggleLabel}
                aria-haspopup="listbox"
                aria-expanded={open && hasOptions}
                aria-controls={listboxId}
                disabled={disabled || !hasOptions}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => {
                    if (disabled || !hasOptions) return;
                    // A second click closes the list only after every option is already visible.
                    // While a typed filter is active, the same button clears it and shows the full list.
                    if (open && typedQuery == null) {
                        setOpen(false);
                        return;
                    }
                    const selectText = !open;
                    revealAll();
                    const input = inputRef.current;
                    input?.focus();
                    if (selectText) input?.select();
                }}
                style={toggleClassName ? undefined : {
                    width: 32,
                    flex: "0 0 32px",
                    cursor: disabled || !hasOptions ? "not-allowed" : "pointer",
                    background: "var(--theme-surface)",
                    color: "var(--theme-text-primary)",
                    border: fieldBorder,
                    borderLeft: 0,
                    borderRadius: "0 4px 4px 0",
                    opacity: disabled || !hasOptions ? 0.5 : 1,
                }}
            >
                ▾
            </button>
            {open && hasOptions && listBox && createPortal(
                <div
                    id={listboxId}
                    role="listbox"
                    className={listClassName}
                    onMouseDown={(event) => {
                        if (event.target === event.currentTarget) return;
                        event.preventDefault();
                    }}
                    style={{
                        position: "fixed",
                        left: listBox.left,
                        width: listBox.width,
                        right: "auto",
                        top: listBox.top,
                        bottom: listBox.bottom,
                        maxHeight: listBox.maxHeight,
                        zIndex: 10000,
                        overflow: "auto",
                        ...(listClassName ? {} : {
                            padding: 4,
                            border: fieldBorder,
                            borderRadius: 4,
                            background: "var(--theme-surface)",
                            boxShadow: "0 8px 20px rgba(15, 23, 42, 0.14)",
                        }),
                    }}
                >
                    {visible.length === 0 ? (
                        <div role="status" style={{ padding: "8px 10px", color: "var(--theme-text-muted)", fontSize: "0.75rem" }}>
                            {emptyText}
                        </div>
                    ) : visible.map((option, index) => {
                        const active = index === safeIndex;
                        return (
                            <button
                                key={`${option.value}-${index}`}
                                id={`${listboxId}-opt-${index}`}
                                type="button"
                                role="option"
                                tabIndex={-1}
                                data-value={option.value}
                                className={optionClassName}
                                aria-selected={active}
                                onMouseEnter={() => setActiveIndex(index)}
                                onClick={() => choose(option.value)}
                                style={optionClassName ? undefined : {
                                    width: "100%",
                                    display: "flex",
                                    flexDirection: "column",
                                    alignItems: "flex-start",
                                    gap: 2,
                                    padding: "8px 10px",
                                    border: 0,
                                    borderRadius: 4,
                                    background: active ? "var(--theme-primary-soft)" : "transparent",
                                    color: "var(--theme-text-primary)",
                                    cursor: "pointer",
                                    textAlign: "left",
                                }}
                            >
                                <span className={optionValueClassName} style={optionValueClassName ? undefined : { fontSize: "0.82rem", fontWeight: 700 }}>
                                    {option.label || option.value}
                                </span>
                                {option.description && option.description !== option.value && (
                                    <span className={optionDescriptionClassName} style={optionDescriptionClassName ? undefined : { color: "var(--theme-text-muted)", fontSize: "0.72rem" }}>
                                        {option.description}
                                    </span>
                                )}
                            </button>
                        );
                    })}
                </div>,
                document.body,
            )}
        </div>
    );
}
