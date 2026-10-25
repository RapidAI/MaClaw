import React from "react";

/**
 * Kind of one coding-process row. `other` is a tool the verb table does not name.
 * `find` is a file lookup (glob / list), distinct from content `search`.
 * `todo` is a checklist write. `agent` is a delegated child agent.
 */
export type CodingAgentProcessKind = "think" | "search" | "find" | "read" | "run" | "edit" | "write" | "explore" | "todo" | "agent" | "other";

const svgProps = {
    width: 16,
    height: 16,
    viewBox: "0 0 16 16",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.5,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true as const,
    focusable: false as const,
};

/** 16px stroke icon for a coding-process row. Color comes from `currentColor`. */
export function CodingAgentProcessIcon({
    kind,
    className,
}: {
    kind: CodingAgentProcessKind;
    className?: string;
}): React.ReactElement {
    return (
        <svg {...svgProps} className={className} data-process-icon={kind}>
            {iconPaths(kind)}
        </svg>
    );
}

function iconPaths(kind: CodingAgentProcessKind): React.ReactNode {
    switch (kind) {
        case "think":
            return (
                <>
                    <path d="M8 1.75a3.15 3.15 0 0 0-1.7 5.8c.28.22.45.52.5.86h2.4c.05-.34.22-.64.5-.86A3.15 3.15 0 0 0 8 1.75z" />
                    <path d="M6.55 10.15h2.9M6.9 11.55h2.2M7.35 12.9h1.3" />
                </>
            );
        case "search":
            return (
                <>
                    <circle cx="7" cy="7" r="3.15" />
                    <path d="M9.4 9.4 L13.1 13.1" />
                </>
            );
        case "find":
            return (
                <>
                    <path d="M2.35 2.15h4.2l2.05 2.05V13.7H2.35z" />
                    <path d="M6.55 2.15v2.05h2.05" />
                    <circle cx="10.45" cy="11.2" r="2.15" />
                    <path d="M11.95 12.7 L14.15 14.4" />
                </>
            );
        case "read":
            return (
                <>
                    <path d="M3.75 2.25h5.1L12.25 5.7V13.75H3.75z" />
                    <path d="M8.85 2.25V5.85h3.4" />
                </>
            );
        case "run":
            return (
                <>
                    <rect x="2.35" y="3.15" width="11.3" height="9.7" rx="1.5" />
                    <path d="M4.7 6.55 L7.15 8.15 L4.7 9.75" />
                    <path d="M8.35 10.15 H11.15" />
                </>
            );
        case "edit":
            return (
                <>
                    <path d="M9.7 2.55l3.75 3.75L6.15 13.6H2.4V9.85z" />
                    <path d="M8.35 3.9l3.75 3.75" />
                </>
            );
        case "write":
            return (
                <>
                    <path d="M3.5 2.25h5.2L12.5 6.05V13.75H3.5z" />
                    <path d="M8.7 2.25V6.15h3.8" />
                    <path d="M5.7 9.4h4.6M5.7 11.35h3.1" />
                </>
            );
        case "explore":
            return (
                <>
                    <circle cx="3.8" cy="8" r="1.45" />
                    <circle cx="12.15" cy="4.15" r="1.45" />
                    <circle cx="12.15" cy="11.85" r="1.45" />
                    <path d="M5.2 7.35 L10.7 4.7M5.25 8.65 L10.7 11.3" />
                </>
            );
        case "todo":
            return (
                <>
                    <path d="M3.1 4.15h9.8" />
                    <path d="M3.1 8h9.8" />
                    <path d="M3.1 11.85h6.4" />
                </>
            );
        case "agent":
            return (
                <>
                    <circle cx="8" cy="8" r="1.55" />
                    <circle cx="3.35" cy="3.7" r="1.15" />
                    <circle cx="12.65" cy="3.7" r="1.15" />
                    <circle cx="8" cy="12.85" r="1.15" />
                    <path d="M4.35 4.45 L6.7 6.75" />
                    <path d="M11.65 4.45 L9.3 6.75" />
                    <path d="M8 9.55 V11.7" />
                </>
            );
        default:
            return (
                <>
                    <circle cx="8" cy="8" r="5.15" />
                    <circle cx="8" cy="8" r="1.15" fill="currentColor" stroke="none" />
                </>
            );
    }
}

/** Disclosure chevron. Points right; the summary rotates it when the list is open. */
export function CodingAgentProcessChevron({ className }: { className?: string }): React.ReactElement {
    return (
        <svg {...svgProps} className={className}>
            <path d="M6 3.75 L10.25 8 L6 12.25" />
        </svg>
    );
}

/** Two overlapping squares, used to copy a running command. */
export function CodingAgentProcessCopyIcon({ className }: { className?: string }): React.ReactElement {
    return (
        <svg {...svgProps} className={className}>
            <rect x="5.6" y="5.6" width="7" height="7" rx="1.1" />
            <path d="M4.6 10.2H3.9A1.1 1.1 0 0 1 2.8 9.1V3.9A1.1 1.1 0 0 1 3.9 2.8H9.1A1.1 1.1 0 0 1 10.2 3.9V4.6" />
        </svg>
    );
}
