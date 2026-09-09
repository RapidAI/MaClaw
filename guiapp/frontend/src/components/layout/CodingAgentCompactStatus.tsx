import {
    codingAgentBrandLabel,
    codingAgentStatusClassName,
    codingAgentStatusDataAttrs,
    codingAgentFilePreviewText,
    codingAgentProgressStatusText,
    codingAgentProgressMetaText,
    codingAgentUiIsDark,
    codingAgentVariantDisplayText,
    resolveCodingAgentStatusTone,
    normalizeCodingAgentProgress,
    type CodingAgentProgress,
    type CodingAgentStatusVariant,
} from '../ai/CodingAgentProgressStatus';

type CodingAgentCompactStatusVariant = Extract<CodingAgentStatusVariant, 'sidebar' | 'status-bar'>;

type CodingAgentCompactStatusProps = {
    progress: CodingAgentProgress;
    lang: string;
    testId: string;
    variant: CodingAgentCompactStatusVariant;
    /** When omitted, reads `#App[data-ai-theme]` so dark chrome stays legible. */
    isDark?: boolean;
};

/**
 * Compact coding-agent status chip for sidebar / status bar.
 * One dense professional line — short brand, phase, task, optional meta/title.
 */
export const CodingAgentCompactStatus = ({ progress, lang, testId, variant, isDark }: CodingAgentCompactStatusProps) => {
    const normalized = normalizeCodingAgentProgress(progress);
    const tone = resolveCodingAgentStatusTone(normalized, codingAgentUiIsDark(isDark));
    const agentLabel = codingAgentBrandLabel(lang);
    const statusLabel = codingAgentProgressStatusText(normalized, lang);
    const isSidebar = variant === 'sidebar';
    const displayText = codingAgentVariantDisplayText(normalized, lang, variant);
    const metaText = codingAgentProgressMetaText(normalized, lang);
    // File preview only on sidebar when no turn-snapshot card will show it below.
    // (Sidebar card body owns files when a snapshot is present.)
    const filePreview = isSidebar ? codingAgentFilePreviewText(normalized, lang) : undefined;
    const filesLabel = lang.startsWith('zh') ? '\u6587\u4ef6' : 'Files';

    return (
        <span
            className={codingAgentStatusClassName(normalized, variant)}
            data-testid={testId}
            data-tone-accent={tone.accent}
            {...codingAgentStatusDataAttrs(normalized, variant)}
            role="status"
            aria-live="polite"
            aria-label={displayText}
            title={displayText}
            style={{
                maxWidth: isSidebar ? undefined : '320px',
                minWidth: 0,
                display: isSidebar ? 'grid' : 'flex',
                gridTemplateColumns: isSidebar ? 'auto minmax(0, 1fr)' : undefined,
                alignItems: 'center',
                gap: isSidebar ? '2px 6px' : '5px',
                padding: isSidebar ? '5px 7px' : '1px 7px',
                borderRadius: '6px',
                border: `1px solid ${tone.border}`,
                background: tone.bg,
                color: isSidebar ? 'var(--theme-text)' : tone.accent,
                fontSize: isSidebar ? '0.7rem' : '0.68rem',
                fontWeight: isSidebar ? undefined : 650,
                lineHeight: isSidebar ? 1.3 : 1.25,
                whiteSpace: isSidebar ? undefined : 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Cascadia Mono", monospace',
            }}
        >
            <span
                style={{
                    color: tone.accent,
                    fontWeight: 700,
                    whiteSpace: 'nowrap',
                    flexShrink: 0,
                    letterSpacing: '0.02em',
                    textTransform: lang.startsWith('zh') ? undefined : 'uppercase',
                    fontSize: isSidebar ? '0.62rem' : '0.64rem',
                }}
            >
                {agentLabel}
            </span>
            <span style={{ minWidth: 0, display: 'flex', gap: '5px', alignItems: 'center', overflow: 'hidden' }}>
                <span style={{ color: tone.accent, fontWeight: 600, flexShrink: 0 }}>{statusLabel}</span>
                {normalized.taskID && (
                    <span style={{ color: isSidebar ? 'var(--theme-text-muted)' : tone.accent, flexShrink: 0, opacity: 0.92 }}>
                        {normalized.taskID}
                    </span>
                )}
                {metaText && (
                    <span
                        style={{
                            color: isSidebar ? 'var(--theme-text-muted)' : tone.accent,
                            flexShrink: 0,
                            maxWidth: isSidebar ? '100px' : '120px',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            opacity: 0.9,
                        }}
                    >
                        {metaText}
                    </span>
                )}
                {normalized.title && (
                    <span
                        style={{
                            color: isSidebar ? undefined : 'var(--theme-text)',
                            minWidth: 0,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            fontWeight: 500,
                        }}
                    >
                        {normalized.title}
                    </span>
                )}
            </span>
            {filePreview && (
                <span
                    style={{
                        gridColumn: '1 / -1',
                        minWidth: 0,
                        display: 'flex',
                        alignItems: 'center',
                        gap: '5px',
                        color: 'var(--theme-text-muted)',
                        overflow: 'hidden',
                        fontSize: '0.64rem',
                    }}
                >
                    <span style={{ flexShrink: 0 }}>{filesLabel}</span>
                    <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--theme-text)' }}>
                        {filePreview}
                    </span>
                </span>
            )}
        </span>
    );
};
