import type { Theme } from "./aiAssistantPanelTheme";

interface AssistantPureCodingEmptyStateProps {
    /** True for the remote-coding workbench: switches the testid and shows the host line. */
    remote: boolean;
    /** Remote SSH host rendered under the title; ignored when empty. */
    remoteHost?: string;
    title: string;
    description: string;
    theme: Theme;
}

/**
 * Empty state shown in pure coding / remote maintenance tabs before the user
 * sends the first task. Extracted from AIAssistantPanel so that panel keeps
 * room under the 6800-line main UI guard.
 */
export function AssistantPureCodingEmptyState({ remote, remoteHost, title, description, theme: t }: AssistantPureCodingEmptyStateProps) {
    return (
        <div
            data-testid={remote ? "remote-coding-workbench-empty" : "coding-workbench-empty"}
            style={{
                minHeight: "100%",
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
                gap: 7,
                padding: "28px 20px",
                boxSizing: "border-box",
                textAlign: "center",
                color: t.textMuted,
            }}
        >
            <div style={{ color: t.text, fontSize: 14, fontWeight: 600 }}>
                {title}
            </div>
            {remote && remoteHost ? (
                <div style={{ color: t.headingColor, fontSize: 12 }}>{remoteHost}</div>
            ) : null}
            <div style={{ maxWidth: 520, color: t.emptyHint, fontSize: 12, lineHeight: 1.55 }}>
                {description}
            </div>
        </div>
    );
}
