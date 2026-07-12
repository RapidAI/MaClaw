/**
 * Professional workbench SVG icons.
 * Prefer these over emoji decorations to keep the UI product-like, not chatbot-like.
 */
import type { CSSProperties, ReactNode } from "react";

type IconProps = {
    size?: number;
    color?: string;
    style?: CSSProperties;
    className?: string;
    title?: string;
};

function baseSvg({ size = 16, color = "currentColor", style, className, title, children }: IconProps & { children: ReactNode }) {
    return (
        <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke={color}
            strokeWidth={1.65}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden={title ? undefined : true}
            role={title ? "img" : undefined}
            focusable="false"
            className={className}
            style={{ display: "block", flexShrink: 0, ...style }}
        >
            {title ? <title>{title}</title> : null}
            {children}
        </svg>
    );
}

export function IconFolder(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H9l2 2h7.5A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z" />
                <path d="M3 10h18" />
            </>
        ),
    });
}

export function IconFolderOpen(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M3 8.5A2.5 2.5 0 0 1 5.5 6H9l1.8 1.8H18.5A2.5 2.5 0 0 1 21 10.3V11" />
                <path d="M3 10.5 5.2 17.2A2 2 0 0 0 7.1 18.5h10.2a2 2 0 0 0 1.9-1.4L21 11H5.2" />
            </>
        ),
    });
}

export function IconBell(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M18 9a6 6 0 1 0-12 0c0 7-3 8.5-3 8.5h18S18 16 18 9" />
                <path d="M10.2 20a2.2 2.2 0 0 0 3.6 0" />
                <path d="M12 3.2V4.5" />
            </>
        ),
    });
}

export function IconRocket(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M5.5 15.5c1.8.4 3.4 1 4.7 2.1 1.2 1 2.1 2.4 2.7 4.1" />
                <path d="M8.2 16.8 5 19l2.2-3.2" />
                <path d="M14.2 4.5c2.6 1.1 4.7 3.2 5.8 5.8-1.5 3.5-4.4 6.5-8 8.2-1.6-2.5-3.3-4.3-5.7-5.9 1.7-3.5 4.5-6.3 7.9-8.1Z" />
                <circle cx="14.5" cy="9.5" r="1.4" />
                <path d="M9.2 14.8 7 17" />
            </>
        ),
    });
}

export function IconBranch(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="6.5" cy="5.5" r="2.2" />
                <circle cx="6.5" cy="18.5" r="2.2" />
                <circle cx="17.5" cy="12" r="2.2" />
                <path d="M6.5 7.7v8.6" />
                <path d="M6.5 12h6.2a2.8 2.8 0 0 0 2.8-2.8V9.3" />
            </>
        ),
    });
}

export function IconAlert(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="m12 3.8 9 16.2H3l9-16.2Z" />
                <path d="M12 10v4.2" />
                <circle cx="12" cy="17.2" r="0.9" fill="currentColor" stroke="none" />
            </>
        ),
    });
}

export function IconCheck(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="12" cy="12" r="9" />
                <path d="m8.2 12.2 2.4 2.4 5.2-5.4" />
            </>
        ),
    });
}

export function IconCross(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="12" cy="12" r="9" />
                <path d="m9 9 6 6" />
                <path d="m15 9-6 6" />
            </>
        ),
    });
}

export function IconHourglass(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M7 3h10" />
                <path d="M7 21h10" />
                <path d="M8 3c0 4.5 2.2 5.8 4 7.5 1.8-1.7 4-3 4-7.5" />
                <path d="M8 21c0-4.5 2.2-5.8 4-7.5 1.8 1.7 4 3 4 7.5" />
                <path d="M10 12h4" />
            </>
        ),
    });
}

export function IconPaperclip(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <path d="m21.2 11.1-9.1 9.1a5.7 5.7 0 0 1-8-8l9.9-9.9a3.8 3.8 0 0 1 5.4 5.4l-9.9 9.9a1.9 1.9 0 1 1-2.7-2.7l8.5-8.5" />
        ),
    });
}

export function IconDocument(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M7 3.5h7l4 4V20a1.5 1.5 0 0 1-1.5 1.5h-9.5A1.5 1.5 0 0 1 5.5 20V5A1.5 1.5 0 0 1 7 3.5Z" />
                <path d="M14 3.5V8h4.5" />
                <path d="M9 12h6" />
                <path d="M9 15.5h6" />
                <path d="M9 19h3.5" />
            </>
        ),
    });
}

export function IconEdit(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M4 20h4l11-11-4-4L4 16v4Z" />
                <path d="m13.5 6.5 4 4" />
                <path d="M14 20h6" />
            </>
        ),
    });
}

export function IconSearch(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="11" cy="11" r="6.5" />
                <path d="m16 16 4.2 4.2" />
                <path d="M8.5 11h5" />
                <path d="M11 8.5v5" />
            </>
        ),
    });
}

export function IconRecord(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="12" cy="12" r="9" />
                <circle cx="12" cy="12" r="4.2" fill="currentColor" stroke="none" />
            </>
        ),
    });
}

export function IconInfo(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="12" cy="12" r="9" />
                <path d="M12 10.5V17" />
                <circle cx="12" cy="7.5" r="0.9" fill="currentColor" stroke="none" />
            </>
        ),
    });
}

export function IconMessage(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M5 5h14a2 2 0 0 1 2 2v8.2a2 2 0 0 1-2 2H9.5L5 20.5V7a2 2 0 0 1 2-2Z" />
                <path d="M8.5 10h7" />
                <path d="M8.5 13.2h4.5" />
            </>
        ),
    });
}

export function IconLock(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="5" y="11" width="14" height="10" rx="1.5" />
                <path d="M8 11V8a4 4 0 0 1 8 0v3" />
                <path d="M12 15v2" />
            </>
        ),
    });
}

export function IconStar(props: IconProps & { filled?: boolean }) {
    const { filled = true, ...rest } = props;
    return baseSvg({
        ...rest,
        children: (
            <path
                d="m12 3.4 2.3 4.7 5.2.8-3.8 3.6.9 5.2L12 15.6 7.4 17.7l.9-5.2L4.5 8.9l5.2-.8L12 3.4Z"
                fill={filled ? "currentColor" : "none"}
            />
        ),
    });
}

export function IconUsers(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="9" cy="8" r="2.5" />
                <path d="M3.5 19c.7-3 2.7-4.5 5.5-4.5S14 16 14.5 19" />
                <circle cx="17" cy="9" r="2.1" />
                <path d="M15.5 14.5c1.8.3 3.2 1.4 3.8 3.5" />
            </>
        ),
    });
}

export function IconBuilding(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M4 20h16" />
                <path d="M6 20V6.5A1.5 1.5 0 0 1 7.5 5h9A1.5 1.5 0 0 1 18 6.5V20" />
                <path d="M9 9h1.5M13.5 9H15M9 12.5h1.5M13.5 12.5H15M9 16h1.5M13.5 16H15" />
                <path d="M10.5 20v-3h3v3" />
            </>
        ),
    });
}

export function IconClipboard(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="6" y="5" width="12" height="16" rx="1.5" />
                <path d="M9 5V4h6v1" />
                <path d="M9 10h6M9 13.5h6M9 17h4" />
            </>
        ),
    });
}

export function IconWave(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="8" cy="10" r="2.2" />
                <circle cx="16" cy="10" r="2.2" />
                <path d="M5.5 14.5c1.2 2 2.8 3 6.5 3s5.3-1 6.5-3" />
            </>
        ),
    });
}

export function IconMail(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="3.5" y="5.5" width="17" height="13" rx="1.5" />
                <path d="m3.5 7 8.5 6.5L20.5 7" />
            </>
        ),
    });
}

export function IconSkip(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="m7 7 6 5-6 5V7Z" />
                <path d="M17 7v10" />
            </>
        ),
    });
}

export function IconKeyboard(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="3" y="7" width="18" height="11" rx="1.5" />
                <path d="M7 10.5h.01M10.5 10.5h.01M14 10.5h.01M17.5 10.5h.01" />
                <path d="M7 14h10" />
            </>
        ),
    });
}

export function IconMonitor(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="3.5" y="4" width="17" height="12" rx="1.5" />
                <path d="M8 20h8" />
                <path d="M12 16v4" />
            </>
        ),
    });
}

export function IconLightbulb(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M9 18h6" />
                <path d="M10 21h4" />
                <path d="M8.5 15.2A5.5 5.5 0 1 1 15.5 15.2c-.8 1-1.5 1.8-1.5 3.3h-4c0-1.5-.7-2.3-1.5-3.3Z" />
            </>
        ),
    });
}

export function IconPresentation(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <rect x="3.5" y="4" width="17" height="11" rx="1.2" />
                <path d="M12 15v5" />
                <path d="M8 20h8" />
                <path d="M7 8h4v4H7z" />
                <path d="M13 9h4M13 12h3" />
            </>
        ),
    });
}

export function IconPen(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M5 19h4L19 9l-4-4L5 15v4Z" />
                <path d="m13.5 6.5 4 4" />
            </>
        ),
    });
}

export function IconRefresh(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M20 6v5h-5" />
                <path d="M4 18v-5h5" />
                <path d="M18 10a6 6 0 0 0-10-3L4 11" />
                <path d="M6 14a6 6 0 0 0 10 3l4-4" />
            </>
        ),
    });
}

/** Ranking trophy (rank 1) or medal (2/3/other). Color encodes place. */
export function IconRankBadge({ rank = 0, size = 18, style, className, title }: IconProps & { rank?: number }) {
    const color =
        rank === 1 ? "#eab308" :
        rank === 2 ? "#94a3b8" :
        rank === 3 ? "#d97706" :
        "#64748b";
    if (rank === 1) {
        return baseSvg({
            size,
            color,
            style,
            className,
            title: title ?? "Rank 1",
            children: (
                <>
                    <path d="M8 4h8v4a4 4 0 0 1-8 0V4Z" />
                    <path d="M8 6H5.5A2.5 2.5 0 0 0 8 9.8" />
                    <path d="M16 6h2.5A2.5 2.5 0 0 1 16 9.8" />
                    <path d="M10 14h4v3h-4z" />
                    <path d="M8 20h8" />
                </>
            ),
        });
    }
    return baseSvg({
        size,
        color,
        style,
        className,
        title: title ?? (rank > 0 ? `Rank ${rank}` : "Rank"),
        children: (
            <>
                <circle cx="12" cy="9" r="5" />
                <path d="m9 13.5-1.5 7.5L12 18l4.5 3L15 13.5" />
            </>
        ),
    });
}

export function IconChevronRight(props: IconProps) {
    return baseSvg({
        ...props,
        children: <path d="m9 6 6 6-6 6" />,
    });
}

export function IconArrowUpRight(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M7 17 17 7" />
                <path d="M9 7h8v8" />
            </>
        ),
    });
}

export function IconArrowDownLeft(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <path d="M17 7 7 17" />
                <path d="M15 17H7v-8" />
            </>
        ),
    });
}

export function IconCircleDot(props: IconProps) {
    return baseSvg({
        ...props,
        children: (
            <>
                <circle cx="12" cy="12" r="8" />
                <circle cx="12" cy="12" r="2.5" fill="currentColor" stroke="none" />
            </>
        ),
    });
}

/** Hollow ring — idle / offline (distinct from error cross). */
export function IconCircle(props: IconProps) {
    return baseSvg({
        ...props,
        children: <circle cx="12" cy="12" r="8" />,
    });
}

export function IconBolt(props: IconProps) {
    return baseSvg({
        ...props,
        children: <path d="M13 2 4 14h7l-1 8 9-12h-7l1-8Z" />,
    });
}

export type StatusGlyphKind = "ok" | "error" | "pending" | "warn" | "tool" | "info" | "offline";

/** Inline status mark used next to short labels. Prefer this over emoji or text badges. */
export function StatusGlyph({
    kind,
    size = 14,
    color,
}: {
    kind: StatusGlyphKind;
    size?: number;
    /** Optional color override (e.g. dark console themes). */
    color?: string;
}) {
    switch (kind) {
        case "ok":
            return <IconCheck size={size} color={color ?? "var(--theme-success, #16a34a)"} />;
        case "error":
            return <IconCross size={size} color={color ?? "var(--theme-danger, #dc2626)"} />;
        case "warn":
            return <IconAlert size={size} color={color ?? "var(--theme-warning, #d97706)"} />;
        case "tool":
            return <IconRecord size={size} color={color ?? "var(--theme-primary, #2f5f98)"} />;
        case "info":
            return <IconInfo size={size} color={color ?? "var(--theme-primary, #2f5f98)"} />;
        case "offline":
            // Distinct from error (cross): muted hollow ring for disconnected/idle.
            return <IconCircle size={size} color={color ?? "var(--theme-text-muted, #64748b)"} />;
        case "pending":
        default:
            return <IconHourglass size={size} color={color ?? "currentColor"} style={color ? undefined : { opacity: 0.75 }} />;
    }
}
