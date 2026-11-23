import React from "react";
import { localizeText } from "../../i18n";

export function formatContextWindowTokens(value: number): string {
    const n = Math.max(0, Number.isFinite(value) ? value : 0);
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`;
    if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k`;
    return String(Math.round(n));
}

export function ContextWindowMeter({
    lang,
    usedTokens,
    maxTokens,
    mutedColor,
}: {
    lang: string;
    usedTokens: number;
    maxTokens: number;
    mutedColor: string;
}): React.ReactElement | null {
    if (!(maxTokens > 0)) return null;
    const used = Math.max(0, usedTokens || 0);
    const pct = Math.max(0, Math.round((used / maxTokens) * 100));
    const label = localizeText(lang, "Context", "上下文", "上下文");
    const title = localizeText(
        lang,
        "Latest model request input / usable context window (output space reserved)",
        "最近一次模型请求输入 / 可用上下文窗口（已预留输出空间）",
        "最近一次模型請求輸入 / 可用上下文視窗（已預留輸出空間）",
    );
    return (
        <div
            data-testid="context-window-meter"
            role="status"
            aria-label={`${label} ${used} / ${maxTokens}`}
            title={title}
            style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "flex-end",
                gap: 7,
                margin: "0 16px 5px",
                color: mutedColor,
                fontSize: 11,
                lineHeight: 1.4,
                fontVariantNumeric: "tabular-nums",
                whiteSpace: "nowrap",
            }}
        >
            <span>{label}</span>
            <span data-testid="context-window-meter-value">
                {formatContextWindowTokens(used)} / {formatContextWindowTokens(maxTokens)} · {pct}%
            </span>
            <span
                aria-hidden="true"
                style={{
                    width: 52,
                    height: 3,
                    overflow: "hidden",
                    borderRadius: 999,
                    background: "color-mix(in srgb, currentColor 16%, transparent)",
                }}
            >
                <span
                    data-testid="context-window-meter-fill"
                    style={{
                        display: "block",
                        width: `${Math.min(100, pct)}%`,
                        height: "100%",
                        borderRadius: "inherit",
                        background: pct >= 85 ? "var(--theme-warning, #b7791f)" : "currentColor",
                    }}
                />
            </span>
        </div>
    );
}
