import {
    codingAgentStatusClassName,
    codingAgentStatusDataAttrs,
    codingAgentFilePreviewText,
    codingAgentStatusLabel,
    codingAgentProgressMetaText,
    codingAgentStatusTone,
    codingAgentVariantDisplayText,
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
};

export const CodingAgentCompactStatus = ({ progress, lang, testId, variant }: CodingAgentCompactStatusProps) => {
    const normalized = normalizeCodingAgentProgress(progress);
    const tone = codingAgentStatusTone(normalized.phase);
    const agentLabel = lang.startsWith('zh') ? '\u7f16\u7a0b\u667a\u80fd\u4f53' : 'Coding Agent';
    const taskStatusLabel = lang.startsWith('zh') ? '\u4efb\u52a1\u72b6\u6001' : 'Task status';
    const statusLabel = codingAgentStatusLabel(normalized.phase, lang);
    const isSidebar = variant === 'sidebar';
    const displayText = codingAgentVariantDisplayText(normalized, lang, variant);
    const metaText = codingAgentProgressMetaText(normalized, lang);
    const filePreview = isSidebar ? codingAgentFilePreviewText(normalized, lang) : undefined;
    const filesLabel = lang.startsWith('zh') ? '\u53d8\u66f4\u6587\u4ef6' : 'Files';

    return (
        <span
            className={codingAgentStatusClassName(normalized, variant)}
            data-testid={testId}
            {...codingAgentStatusDataAttrs(normalized, variant)}
            role="status"
            aria-live="polite"
            aria-label={displayText}
            title={displayText}
            style={{
                maxWidth: isSidebar ? undefined : '360px',
                minWidth: 0,
                display: isSidebar ? 'grid' : 'flex',
                gridTemplateColumns: isSidebar ? 'auto minmax(0, 1fr)' : undefined,
                alignItems: 'center',
                gap: isSidebar ? '4px 8px' : '6px',
                padding: isSidebar ? '7px 8px' : '2px 8px',
                borderRadius: '7px',
                border: `1px solid ${tone.border}`,
                boxShadow: isSidebar ? `inset 0 0 0 1px ${tone.border}` : undefined,
                background: tone.bg,
                color: isSidebar ? 'var(--theme-text)' : tone.accent,
                fontSize: '0.72rem',
                fontWeight: isSidebar ? undefined : 650,
                lineHeight: isSidebar ? 1.35 : undefined,
                whiteSpace: isSidebar ? undefined : 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
            }}
        >
            <span style={{ color: tone.accent, fontWeight: 700, whiteSpace: 'nowrap', flexShrink: 0 }}>{agentLabel}</span>
            <span style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center', overflow: 'hidden' }}>
                {isSidebar && <span style={{ color: 'var(--theme-text-muted)', flexShrink: 0 }}>{taskStatusLabel}</span>}
                <span style={{ color: tone.accent, fontWeight: 600, flexShrink: 0 }}>{statusLabel}</span>
                {normalized.taskID && <span style={{ color: isSidebar ? 'var(--theme-text-muted)' : tone.accent, flexShrink: 0 }}>{normalized.taskID}</span>}
                {metaText && (
                    <span
                        style={{
                            color: isSidebar ? 'var(--theme-text-muted)' : tone.accent,
                            flexShrink: 0,
                            maxWidth: isSidebar ? '120px' : '140px',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }}
                    >
                        {metaText}
                    </span>
                )}
                {normalized.title && (
                    <span style={{ color: isSidebar ? undefined : 'var(--theme-text)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
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
                        gap: '6px',
                        color: 'var(--theme-text-muted)',
                        overflow: 'hidden',
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
