import type { CSSProperties } from "react";
import type { Theme } from "../aiAssistantPanelTheme";

/** 弹层头部（搜索框容器）。 */
export function popoverHeadStyle(): CSSProperties {
    return { padding: "12px 14px 8px", flexShrink: 0 };
}

/** 弹层滚动列表容器。 */
export function popoverListStyle(): CSSProperties {
    return { flex: 1, overflowY: "auto", padding: "4px 8px 10px", minHeight: 0 };
}

export interface PopoverItemVisual {
    item: CSSProperties;
    icon: CSSProperties;
    meta: CSSProperties;
    desc: CSSProperties;
    badge: CSSProperties;
}

/**
 * 单选列表项。selected = 当前值（主色高亮）；disabled = 锁定置灰（互斥）。
 * 颜色全部走 Theme token。
 */
export function popoverItemStyle(t: Theme, options: { selected?: boolean; disabled?: boolean; accent?: string } = {}): PopoverItemVisual {
    const accent = options.accent || t.btnColor;
    const selected = !!options.selected;
    const disabled = !!options.disabled;
    return {
        item: {
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "9px 10px",
            borderRadius: 10,
            cursor: disabled ? "not-allowed" : "pointer",
            fontSize: 13.5,
            color: selected ? accent : disabled ? t.textMuted : t.text,
            fontWeight: selected ? 600 : 400,
            opacity: disabled ? 0.45 : 1,
            background: selected ? `color-mix(in srgb, ${accent} 9%, ${t.bg})` : "transparent",
            fontFamily: "system-ui, -apple-system, sans-serif",
            userSelect: "none",
        },
        icon: {
            width: 28,
            height: 28,
            borderRadius: 8,
            background: `color-mix(in srgb, ${accent} 7%, ${t.fieldBg})`,
            color: accent,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flex: "0 0 28px",
        },
        meta: { flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
        desc: {
            fontSize: 12,
            fontWeight: 400,
            color: selected ? accent : t.textMuted,
            marginTop: 1,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
        },
        badge: {
            fontSize: 11,
            color: t.textMuted,
            background: t.fieldBg,
            border: `1px solid ${t.fieldBorder}`,
            borderRadius: 6,
            padding: "2px 7px",
            whiteSpace: "nowrap",
            flexShrink: 0,
        },
    };
}

export function popoverSepStyle(t: Theme): CSSProperties {
    return { height: 1, background: t.divider || t.fieldBorder, margin: "4px 6px" };
}
