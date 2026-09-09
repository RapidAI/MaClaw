import {
    adaptCodingAgentStatusTone,
    codingAgentCommandProgressLabel,
    codingAgentCommandProgressTone,
    codingAgentDiffCheckStatusLabel,
    codingAgentDiffCheckStatusTone,
    codingAgentExplorationStatusLabel,
    codingAgentExplorationStatusTone,
    codingAgentFileActivityStatusLabel,
    codingAgentFileActivityStatusTone,
    codingAgentFilePreviewText,
    codingAgentGuardrailStatusLabel,
    codingAgentGuardrailStatusTone,
    codingAgentQualityStatusLabel,
    codingAgentQualityStatusTone,
    codingAgentToolProgressLabel,
    codingAgentToolProgressTone,
    codingAgentToolTraceText,
    codingAgentTurnSnapshotText,
    codingAgentUiIsDark,
    codingAgentVariantDisplayText,
    codingAgentVerificationStatusLabel,
    codingAgentVerificationStatusTone,
    formatCodingAgentDuration,
    normalizeCodingAgentCommandStatus,
    normalizeCodingAgentDiffCheckStatus,
    normalizeCodingAgentExplorationStatus,
    normalizeCodingAgentFileActivityStatus,
    normalizeCodingAgentGuardrailStatus,
    normalizeCodingAgentProgress,
    normalizeCodingAgentQualityStatus,
    normalizeCodingAgentToolOutcome,
    normalizeCodingAgentVerificationStatus,
    type CodingAgentProgress,
    type CodingAgentStatusTone,
    type CodingAgentTurnSnapshot,
} from '../ai/CodingAgentProgressStatus';
import { CodingAgentCompactStatus } from './CodingAgentCompactStatus';

type CodingAgentSidebarStatusProps = {
    progress: CodingAgentProgress;
    snapshot?: CodingAgentTurnSnapshot | null;
    lang: string;
    isDark?: boolean;
};

const formatOptionalCount = (count?: number): string => (count !== undefined ? String(count) : '');
const formatOptionalListCount = <T,>(items?: T[]): string => (items !== undefined ? String(items.length) : '');
const formatCountBadge = (count?: number): string | undefined => (count !== undefined ? `(${count})` : undefined);

const monoFont = 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Cascadia Mono", monospace';

type MetricChip = {
    key: string;
    testId?: string;
    label: string;
    value: string;
    title?: string;
    tone: CodingAgentStatusTone;
    dataAttrs?: Record<string, string>;
    /** Higher = more important; failures sort first in the chip row. */
    priority: number;
    /** Insertion index — stable secondary sort so equal-priority chips keep order. */
    order: number;
};

const MetricChipView = ({ chip, isDark }: { chip: MetricChip; isDark?: boolean }) => {
    // Parent resolves dark once; do not re-read document here.
    const tone = adaptCodingAgentStatusTone(chip.tone, isDark);
    return (
        <span
            data-testid={chip.testId}
            title={chip.title || `${chip.label} ${chip.value}`}
            {...(chip.dataAttrs || {})}
            style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '3px',
                maxWidth: '100%',
                minWidth: 0,
                padding: '1px 5px',
                borderRadius: '4px',
                border: `1px solid ${tone.border}`,
                background: tone.bg,
                color: tone.accent,
                fontWeight: 600,
                fontSize: '0.62rem',
                lineHeight: 1.3,
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
            }}
        >
            <span style={{ opacity: 0.78, fontWeight: 500 }}>{chip.label}</span>
            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>{chip.value}</span>
        </span>
    );
};

/** Attention-first ordering so critical chips appear before routine green ones. */
function metricPriority(status: string | undefined): number {
    const s = (status || '').trim().toLowerCase();
    if (s === 'failed' || s === 'blocked' || s === 'missing') return 3;
    if (s === 'warning' || s === 'skipped') return 2;
    if (s === 'passed' || s === 'checked' || s === 'explored' || s === 'changed' || s === 'success') return 1;
    return 0;
}

function pushChip(
    chips: MetricChip[],
    chip: Omit<MetricChip, 'priority' | 'order'> & { status?: string },
): void {
    chips.push({
        ...chip,
        priority: metricPriority(chip.status),
        order: chips.length,
    });
}

/**
 * Sidebar coding-agent monitor: dense header + tool trail + metric chips.
 * Prefer horizontal chips over stacked labeled rows to save vertical space.
 */
export const CodingAgentSidebarStatus = ({ progress, snapshot, lang, isDark }: CodingAgentSidebarStatusProps) => {
    const dark = codingAgentUiIsDark(isDark);
    const normalized = normalizeCodingAgentProgress(progress);
    // Without a turn snapshot, the compact header owns the file preview.
    // With a snapshot, body owns files so the header stays a single dense line.
    const headerProgress = { ...normalized, files: snapshot ? undefined : normalized.files };
    const filesLabel = lang.startsWith('zh') ? '\u6587\u4ef6' : 'Files';
    const diffLabel = 'Diff';
    const allFiles = snapshot?.files !== undefined ? snapshot.files : normalized.files;
    const bodyFiles = snapshot ? allFiles : undefined;
    const filePreview = bodyFiles?.length
        ? codingAgentFilePreviewText({ ...normalized, files: bodyFiles }, lang)
        : undefined;
    const diffSummary = snapshot?.diffSummary;
    // Always give the group an accessible name (snapshot summary or compact header line).
    const cardText = snapshot
        ? codingAgentTurnSnapshotText(snapshot, lang)
        : codingAgentVariantDisplayText(normalized, lang, "sidebar");
    const normalizedOutcome = normalizeCodingAgentToolOutcome(snapshot?.toolOutcome);
    const durationText = formatCodingAgentDuration(snapshot?.toolDurationMs);
    const traceText = snapshot ? codingAgentToolTraceText(snapshot, lang) : undefined;

    const guardrailState = normalizeCodingAgentGuardrailStatus(snapshot?.guardrailStatus);
    const guardrailText = snapshot?.guardrailStatus ? codingAgentGuardrailStatusLabel(snapshot.guardrailStatus, lang) : '';
    const guardrailTone = codingAgentGuardrailStatusTone(snapshot?.guardrailStatus);
    const commandState = normalizeCodingAgentCommandStatus(snapshot?.commandStatus);
    const commandText = snapshot?.commandStatus
        ? codingAgentCommandProgressLabel(snapshot.commandStatus, lang, snapshot.commandSummary)
        : '';
    const commandTone = codingAgentCommandProgressTone(snapshot?.commandStatus, snapshot?.commandSummary);
    const fileActivityState = normalizeCodingAgentFileActivityStatus(snapshot?.fileActivityStatus);
    const fileActivityText = snapshot?.fileActivityStatus
        ? codingAgentFileActivityStatusLabel(snapshot.fileActivityStatus, lang)
        : '';
    const fileActivityTone = codingAgentFileActivityStatusTone(snapshot?.fileActivityStatus);
    const qualityState = normalizeCodingAgentQualityStatus(snapshot?.qualityStatus);
    const qualityText = snapshot?.qualityStatus ? codingAgentQualityStatusLabel(snapshot.qualityStatus, lang) : '';
    const qualityTone = codingAgentQualityStatusTone(snapshot?.qualityStatus);
    const explorationState = normalizeCodingAgentExplorationStatus(snapshot?.explorationStatus);
    const explorationText = snapshot?.explorationStatus
        ? codingAgentExplorationStatusLabel(snapshot.explorationStatus, lang)
        : '';
    const explorationTone = codingAgentExplorationStatusTone(snapshot?.explorationStatus);
    const verificationState = normalizeCodingAgentVerificationStatus(snapshot?.verificationStatus);
    const verificationText = snapshot?.verificationStatus
        ? codingAgentVerificationStatusLabel(snapshot.verificationStatus, lang)
        : '';
    const verificationTone = codingAgentVerificationStatusTone(snapshot?.verificationStatus);
    const diffCheckState = normalizeCodingAgentDiffCheckStatus(snapshot?.diffCheckStatus);
    const diffCheckText = snapshot?.diffCheckStatus
        ? codingAgentDiffCheckStatusLabel(snapshot.diffCheckStatus, lang)
        : '';
    const diffCheckTone = codingAgentDiffCheckStatusTone(snapshot?.diffCheckStatus);

    const chips: MetricChip[] = [];
    if (snapshot?.guardrailStatus) {
        pushChip(chips, {
            key: 'guard',
            testId: 'sidebar-coding-agent-guardrail',
            label: lang.startsWith('zh') ? '\u8fb9\u754c' : 'Guard',
            value: [guardrailText, formatCountBadge(snapshot.guardrailCount)].filter(Boolean).join(' '),
            title: snapshot.guardrailSummary || guardrailText,
            tone: guardrailTone,
            dataAttrs: { 'data-guardrail-summary': snapshot.guardrailSummary || '' },
            status: snapshot.guardrailStatus,
        });
    }
    if (snapshot?.commandStatus) {
        pushChip(chips, {
            key: 'commands',
            testId: 'sidebar-coding-agent-commands',
            label: lang.startsWith('zh') ? '\u547d\u4ee4' : 'Cmds',
            value: [commandText, formatCountBadge(snapshot.commandCount)].filter(Boolean).join(' '),
            title: snapshot.commandSummary || commandText,
            tone: commandTone,
            dataAttrs: { 'data-command-summary': snapshot.commandSummary || '' },
            status: snapshot.commandStatus,
        });
    }
    if (snapshot?.fileActivityStatus) {
        pushChip(chips, {
            key: 'activity',
            testId: 'sidebar-coding-agent-file-activity',
            label: lang.startsWith('zh') ? '\u52a8\u4f5c' : 'Activity',
            value: [
                fileActivityText,
                snapshot.fileActivityDetail
                    ? `(${snapshot.fileActivityDetail})`
                    : formatCountBadge(snapshot.fileActivityCount),
            ]
                .filter(Boolean)
                .join(' '),
            title: snapshot.fileActivitySummary || snapshot.fileActivityDetail || fileActivityText,
            tone: fileActivityTone,
            dataAttrs: {
                'data-file-activity-summary': snapshot.fileActivitySummary || '',
                'data-file-activity-detail': snapshot.fileActivityDetail || '',
            },
            status: snapshot.fileActivityStatus,
        });
    }
    if (snapshot?.qualityStatus) {
        pushChip(chips, {
            key: 'quality',
            testId: 'sidebar-coding-agent-quality',
            label: lang.startsWith('zh') ? '\u8d28\u91cf' : 'Quality',
            value: [qualityText, formatCountBadge(snapshot.qualityCount)].filter(Boolean).join(' '),
            title: snapshot.qualitySummary || qualityText,
            tone: qualityTone,
            dataAttrs: { 'data-quality-summary': snapshot.qualitySummary || '' },
            status: snapshot.qualityStatus,
        });
    }
    if (snapshot?.explorationStatus) {
        pushChip(chips, {
            key: 'explore',
            testId: 'sidebar-coding-agent-exploration',
            label: lang.startsWith('zh') ? '\u63a2\u7d22' : 'Explore',
            value: [explorationText, formatCountBadge(snapshot.explorationCount)].filter(Boolean).join(' '),
            title: snapshot.explorationSummary || explorationText,
            tone: explorationTone,
            dataAttrs: { 'data-exploration-summary': snapshot.explorationSummary || '' },
            status: snapshot.explorationStatus,
        });
    }
    if (snapshot?.verificationStatus) {
        pushChip(chips, {
            key: 'verify',
            testId: 'sidebar-coding-agent-verification',
            label: lang.startsWith('zh') ? '\u9a8c\u8bc1' : 'Verify',
            value: [verificationText, formatCountBadge(snapshot.verificationCount)].filter(Boolean).join(' '),
            title: snapshot.verificationSummary || verificationText,
            tone: verificationTone,
            dataAttrs: { 'data-verification-summary': snapshot.verificationSummary || '' },
            status: snapshot.verificationStatus,
        });
    }
    if (snapshot?.diffCheckStatus) {
        pushChip(chips, {
            key: 'diff-check',
            testId: 'sidebar-coding-agent-diff-check',
            label: lang.startsWith('zh') ? 'Diff' : 'Diff check',
            value: diffCheckText,
            title: snapshot.diffCheckSummary || diffCheckText,
            tone: diffCheckTone,
            dataAttrs: { 'data-diff-check-summary': snapshot.diffCheckSummary || '' },
            status: snapshot.diffCheckStatus,
        });
    }
    // Attention first; equal priority keeps insertion order (not key alpha).
    chips.sort((a, b) => b.priority - a.priority || a.order - b.order);

    const tools = snapshot?.tools || [];
    const hasToolTrail = tools.length > 0 || !!snapshot?.tool;
    // Body is snapshot-only detail chrome; bare progress uses header alone.
    const hasBody =
        !!snapshot && (
            hasToolTrail ||
            chips.length > 0 ||
            !!diffSummary ||
            !!filePreview
        );

    return (
        <div
            data-testid="sidebar-coding-agent-card"
            role="group"
            aria-label={cardText}
            title={cardText}
            className={`coding-agent-turn-card coding-agent-turn-card--${normalizedOutcome}`}
            data-turn-id={snapshot?.turnID || normalized.turnID || ''}
            data-tool={snapshot?.tool || ''}
            data-tool-outcome={snapshot?.toolOutcome || ''}
            data-tool-outcome-state={normalizedOutcome}
            data-tool-duration-ms={formatOptionalCount(snapshot?.toolDurationMs)}
            data-tool-count={formatOptionalListCount(snapshot?.tools)}
            data-guardrail-status={snapshot?.guardrailStatus || ''}
            data-guardrail-state={snapshot?.guardrailStatus ? guardrailState : ''}
            data-guardrail-count={formatOptionalCount(snapshot?.guardrailCount)}
            data-command-status={snapshot?.commandStatus || ''}
            data-command-state={snapshot?.commandStatus ? commandState : ''}
            data-command-count={formatOptionalCount(snapshot?.commandCount)}
            data-file-activity-status={snapshot?.fileActivityStatus || ''}
            data-file-activity-state={snapshot?.fileActivityStatus ? fileActivityState : ''}
            data-file-activity-count={formatOptionalCount(snapshot?.fileActivityCount)}
            data-quality-status={snapshot?.qualityStatus || ''}
            data-quality-state={snapshot?.qualityStatus ? qualityState : ''}
            data-quality-count={formatOptionalCount(snapshot?.qualityCount)}
            data-exploration-status={snapshot?.explorationStatus || ''}
            data-exploration-state={snapshot?.explorationStatus ? explorationState : ''}
            data-exploration-count={formatOptionalCount(snapshot?.explorationCount)}
            data-verification-status={snapshot?.verificationStatus || ''}
            data-verification-state={snapshot?.verificationStatus ? verificationState : ''}
            data-verification-count={formatOptionalCount(snapshot?.verificationCount)}
            data-diff-check-status={snapshot?.diffCheckStatus || ''}
            data-diff-check-state={snapshot?.diffCheckStatus ? diffCheckState : ''}
            data-change-count={formatOptionalCount(snapshot?.changeCount ?? normalized.count)}
            data-file-count={formatOptionalListCount(allFiles)}
            style={{
                display: 'grid',
                gap: '4px',
                minWidth: 0,
            }}
        >
            <CodingAgentCompactStatus
                progress={headerProgress}
                lang={lang}
                testId="sidebar-coding-agent-status"
                variant="sidebar"
                isDark={dark}
            />
            {hasBody && (
                <div
                    style={{
                        display: 'grid',
                        gap: '3px',
                        padding: '0 6px 1px 8px',
                        fontSize: '0.64rem',
                        lineHeight: 1.3,
                        color: 'var(--theme-text-muted)',
                        fontFamily: monoFont,
                    }}
                >
                    {hasToolTrail && (
                        <div
                            data-testid="sidebar-coding-agent-tool-trace"
                            aria-label={traceText || [snapshot?.tool, durationText].filter(Boolean).join(' ')}
                            style={{
                                minWidth: 0,
                                display: 'flex',
                                flexWrap: 'wrap',
                                alignItems: 'center',
                                gap: '3px',
                                color: 'var(--theme-text)',
                            }}
                        >
                            {tools.length > 0
                                ? tools.map((tool, index) => {
                                    const traceTone = adaptCodingAgentStatusTone(
                                        codingAgentToolProgressTone(tool.outcome, tool.summary, tool.name),
                                        dark,
                                    );
                                    const traceOutcome = normalizeCodingAgentToolOutcome(tool.outcome);
                                    const traceDuration = formatCodingAgentDuration(tool.durationMs);
                                    const outcomeText = tool.outcome
                                        ? codingAgentToolProgressLabel(tool.outcome, lang, tool.summary, tool.name)
                                        : undefined;
                                    const traceLabelText = [tool.name, outcomeText, traceDuration, tool.summary ? `(${tool.summary})` : undefined]
                                        .filter(Boolean)
                                        .join(' ');
                                    return (
                                        <span
                                            key={`${tool.name}-${index}`}
                                            style={{ display: 'inline-flex', alignItems: 'center', gap: '3px', minWidth: 0 }}
                                        >
                                            {index > 0 && (
                                                <span aria-hidden="true" style={{ color: 'var(--theme-text-muted)', opacity: 0.7 }}>
                                                    {'\u2192'}
                                                </span>
                                            )}
                                            <span
                                                data-tool-trace-name={tool.name}
                                                data-tool-trace-outcome={tool.outcome || ''}
                                                data-tool-trace-outcome-state={traceOutcome}
                                                data-tool-trace-summary={tool.summary || ''}
                                                title={traceLabelText}
                                                style={{
                                                    minWidth: 0,
                                                    maxWidth: '110px',
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                    color: tool.outcome ? traceTone.accent : 'var(--theme-text)',
                                                    border: `1px solid ${tool.outcome ? traceTone.border : 'rgba(100, 116, 139, 0.18)'}`,
                                                    background: tool.outcome ? traceTone.bg : 'rgba(100, 116, 139, 0.06)',
                                                    borderRadius: '4px',
                                                    padding: '0 4px',
                                                    fontWeight: tool.outcome ? 650 : 500,
                                                    fontSize: '0.62rem',
                                                }}
                                            >
                                                {[tool.name, outcomeText, traceDuration].filter(Boolean).join(' ')}
                                            </span>
                                        </span>
                                    );
                                })
                                : snapshot?.tool && (
                                    <span
                                        data-tool-trace-name={snapshot.tool}
                                        data-tool-trace-outcome={snapshot.toolOutcome || ''}
                                        data-tool-trace-outcome-state={normalizedOutcome}
                                        title={[snapshot.tool, durationText].filter(Boolean).join(' ')}
                                        style={{
                                            minWidth: 0,
                                            maxWidth: '140px',
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap',
                                            color: 'var(--theme-text)',
                                            border: '1px solid rgba(100, 116, 139, 0.18)',
                                            background: 'rgba(100, 116, 139, 0.06)',
                                            borderRadius: '4px',
                                            padding: '0 4px',
                                            fontWeight: 500,
                                            fontSize: '0.62rem',
                                        }}
                                    >
                                        {[snapshot.tool, durationText].filter(Boolean).join(' ')}
                                    </span>
                                )}
                        </div>
                    )}
                    {chips.length > 0 && (
                        <div
                            style={{
                                display: 'flex',
                                flexWrap: 'wrap',
                                gap: '3px',
                                alignItems: 'center',
                                minWidth: 0,
                            }}
                        >
                            {chips.map((chip) => (
                                <MetricChipView key={chip.key} chip={chip} isDark={dark} />
                            ))}
                        </div>
                    )}
                    {(filePreview || diffSummary) && (
                        <div
                            style={{
                                minWidth: 0,
                                display: 'flex',
                                gap: '6px',
                                alignItems: 'center',
                                overflow: 'hidden',
                            }}
                        >
                            {filePreview && (
                                <>
                                    <span style={{ flexShrink: 0 }}>{filesLabel}</span>
                                    <span
                                        style={{
                                            minWidth: 0,
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap',
                                            color: 'var(--theme-text)',
                                        }}
                                    >
                                        {filePreview}
                                    </span>
                                </>
                            )}
                            {diffSummary && (
                                <>
                                    {filePreview && <span style={{ opacity: 0.45 }}>{'\u00b7'}</span>}
                                    <span style={{ flexShrink: 0 }}>{diffLabel}</span>
                                    <span
                                        style={{
                                            minWidth: 0,
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap',
                                            color: 'var(--theme-text)',
                                        }}
                                    >
                                        {diffSummary}
                                    </span>
                                </>
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
};
