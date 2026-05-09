import { codingAgentCommandStatusLabel, codingAgentCommandStatusTone, codingAgentDiffCheckStatusLabel, codingAgentDiffCheckStatusTone, codingAgentExplorationStatusLabel, codingAgentExplorationStatusTone, codingAgentFileActivityStatusLabel, codingAgentFileActivityStatusTone, codingAgentFilePreviewText, codingAgentGuardrailStatusLabel, codingAgentGuardrailStatusTone, codingAgentQualityStatusLabel, codingAgentQualityStatusTone, codingAgentToolOutcomeLabel, codingAgentToolOutcomeTone, codingAgentToolTraceText, codingAgentTurnSnapshotText, codingAgentVerificationStatusLabel, codingAgentVerificationStatusTone, formatCodingAgentDuration, normalizeCodingAgentCommandStatus, normalizeCodingAgentDiffCheckStatus, normalizeCodingAgentExplorationStatus, normalizeCodingAgentFileActivityStatus, normalizeCodingAgentGuardrailStatus, normalizeCodingAgentProgress, normalizeCodingAgentQualityStatus, normalizeCodingAgentToolOutcome, normalizeCodingAgentVerificationStatus, type CodingAgentProgress, type CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { CodingAgentCompactStatus } from './CodingAgentCompactStatus';

type CodingAgentSidebarStatusProps = {
    progress: CodingAgentProgress;
    snapshot?: CodingAgentTurnSnapshot | null;
    lang: string;
};

const formatOptionalCount = (count?: number): string => (count !== undefined ? String(count) : '');
const formatOptionalListCount = <T,>(items?: T[]): string => (items !== undefined ? String(items.length) : '');
const formatCountBadge = (count?: number): string | undefined => (count !== undefined ? `(${count})` : undefined);

export const CodingAgentSidebarStatus = ({ progress, snapshot, lang }: CodingAgentSidebarStatusProps) => {
    const normalized = normalizeCodingAgentProgress(progress);
    const headerProgress = { ...normalized, files: undefined };
    const toolLabel = lang.startsWith('zh') ? '\u5de5\u5177' : 'Tool';
    const traceLabel = lang.startsWith('zh') ? '\u8f68\u8ff9' : 'Trace';
    const outcomeLabel = lang.startsWith('zh') ? '\u7ed3\u679c' : 'Result';
    const durationLabel = lang.startsWith('zh') ? '\u8017\u65f6' : 'Duration';
    const guardLabel = lang.startsWith('zh') ? '\u8fb9\u754c' : 'Guard';
    const commandLabel = lang.startsWith('zh') ? '\u547d\u4ee4' : 'Commands';
    const fileActivityLabel = lang.startsWith('zh') ? '\u6587\u4ef6\u52a8\u4f5c' : 'Activity';
    const qualityLabel = lang.startsWith('zh') ? '\u8d28\u91cf' : 'Quality';
    const exploreLabel = lang.startsWith('zh') ? '\u63a2\u7d22' : 'Explore';
    const verifyLabel = lang.startsWith('zh') ? '\u9a8c\u8bc1' : 'Verify';
    const diffCheckLabel = lang.startsWith('zh') ? 'Diff \u81ea\u68c0' : 'Diff check';
    const diffLabel = lang.startsWith('zh') ? 'Diff' : 'Diff';
    const filesLabel = lang.startsWith('zh') ? '\u53d8\u66f4\u6587\u4ef6' : 'Files';
    const files = snapshot?.files !== undefined ? snapshot.files : normalized.files;
    const filePreview = files?.length ? codingAgentFilePreviewText({ ...normalized, files }, lang) : undefined;
    const diffSummary = snapshot?.diffSummary;
    const cardText = snapshot ? codingAgentTurnSnapshotText(snapshot, lang) : undefined;
    const normalizedOutcome = normalizeCodingAgentToolOutcome(snapshot?.toolOutcome);
    const outcomeTone = codingAgentToolOutcomeTone(snapshot?.toolOutcome);
    const outcomeText = snapshot?.toolOutcome ? codingAgentToolOutcomeLabel(snapshot.toolOutcome, lang) : '';
    const durationText = formatCodingAgentDuration(snapshot?.toolDurationMs);
    const traceText = snapshot ? codingAgentToolTraceText(snapshot, lang) : undefined;
    const guardrailState = normalizeCodingAgentGuardrailStatus(snapshot?.guardrailStatus);
    const guardrailText = snapshot?.guardrailStatus ? codingAgentGuardrailStatusLabel(snapshot.guardrailStatus, lang) : '';
    const guardrailTone = codingAgentGuardrailStatusTone(snapshot?.guardrailStatus);
    const commandState = normalizeCodingAgentCommandStatus(snapshot?.commandStatus);
    const commandText = snapshot?.commandStatus ? codingAgentCommandStatusLabel(snapshot.commandStatus, lang) : '';
    const commandTone = codingAgentCommandStatusTone(snapshot?.commandStatus);
    const fileActivityState = normalizeCodingAgentFileActivityStatus(snapshot?.fileActivityStatus);
    const fileActivityText = snapshot?.fileActivityStatus ? codingAgentFileActivityStatusLabel(snapshot.fileActivityStatus, lang) : '';
    const fileActivityTone = codingAgentFileActivityStatusTone(snapshot?.fileActivityStatus);
    const qualityState = normalizeCodingAgentQualityStatus(snapshot?.qualityStatus);
    const qualityText = snapshot?.qualityStatus ? codingAgentQualityStatusLabel(snapshot.qualityStatus, lang) : '';
    const qualityTone = codingAgentQualityStatusTone(snapshot?.qualityStatus);
    const explorationState = normalizeCodingAgentExplorationStatus(snapshot?.explorationStatus);
    const explorationText = snapshot?.explorationStatus ? codingAgentExplorationStatusLabel(snapshot.explorationStatus, lang) : '';
    const explorationTone = codingAgentExplorationStatusTone(snapshot?.explorationStatus);
    const verificationState = normalizeCodingAgentVerificationStatus(snapshot?.verificationStatus);
    const verificationText = snapshot?.verificationStatus ? codingAgentVerificationStatusLabel(snapshot.verificationStatus, lang) : '';
    const verificationTone = codingAgentVerificationStatusTone(snapshot?.verificationStatus);
    const diffCheckState = normalizeCodingAgentDiffCheckStatus(snapshot?.diffCheckStatus);
    const diffCheckText = snapshot?.diffCheckStatus ? codingAgentDiffCheckStatusLabel(snapshot.diffCheckStatus, lang) : '';
    const diffCheckTone = codingAgentDiffCheckStatusTone(snapshot?.diffCheckStatus);
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
            data-file-count={formatOptionalListCount(files)}
            style={{
                display: 'grid',
                gap: '5px',
                minWidth: 0,
            }}
        >
            <CodingAgentCompactStatus progress={headerProgress} lang={lang} testId="sidebar-coding-agent-status" variant="sidebar" />
            {(snapshot?.tool || traceText || snapshot?.guardrailStatus || snapshot?.commandStatus || snapshot?.fileActivityStatus || snapshot?.qualityStatus || snapshot?.explorationStatus || snapshot?.verificationStatus || snapshot?.diffCheckStatus || diffSummary || filePreview) && (
                <div
                    style={{
                        display: 'grid',
                        gap: '3px',
                        padding: '0 8px 2px 11px',
                        fontSize: '0.68rem',
                        lineHeight: 1.35,
                        color: 'var(--theme-text-muted)',
                    }}
                >
                    {filePreview && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{filesLabel}</span>
                            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--theme-text)' }}>{filePreview}</span>
                        </div>
                    )}
                    {traceText && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{traceLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-tool-trace"
                                aria-label={traceText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: 'var(--theme-text)',
                                    display: 'inline-flex',
                                    alignItems: 'center',
                                    gap: '4px',
                                }}
                            >
                                {(snapshot?.tools || []).map((tool, index) => {
                                    const traceTone = codingAgentToolOutcomeTone(tool.outcome);
                                    const traceOutcome = normalizeCodingAgentToolOutcome(tool.outcome);
                                    const traceDuration = formatCodingAgentDuration(tool.durationMs);
                                    const traceLabelText = [tool.name, tool.outcome ? codingAgentToolOutcomeLabel(tool.outcome, lang) : undefined, traceDuration, tool.summary ? `(${tool.summary})` : undefined].filter(Boolean).join(' ');
                                    return (
                                        <span key={`${tool.name}-${index}`} style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', minWidth: 0 }}>
                                            {index > 0 && <span aria-hidden="true" style={{ color: 'var(--theme-text-muted)' }}> -&gt; </span>}
                                            <span
                                                data-tool-trace-name={tool.name}
                                                data-tool-trace-outcome={tool.outcome || ''}
                                                data-tool-trace-outcome-state={traceOutcome}
                                                data-tool-trace-summary={tool.summary || ''}
                                                title={traceLabelText}
                                                style={{
                                                    minWidth: 0,
                                                    maxWidth: '92px',
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                    color: tool.outcome ? traceTone.accent : 'var(--theme-text)',
                                                    border: `1px solid ${tool.outcome ? traceTone.border : 'rgba(100, 116, 139, 0.18)'}`,
                                                    background: tool.outcome ? traceTone.bg : 'rgba(100, 116, 139, 0.06)',
                                                    borderRadius: '6px',
                                                    padding: '1px 5px',
                                                    fontWeight: tool.outcome ? 650 : 500,
                                                }}
                                            >
                                                {traceLabelText}
                                            </span>
                                        </span>
                                    );
                                })}
                            </span>
                        </div>
                    )}
                    {snapshot?.tool && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{toolLabel}</span>
                            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--theme-text)' }}>{snapshot.tool}</span>
                        </div>
                    )}
                    {snapshot?.toolOutcome && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{outcomeLabel}</span>
                            <span
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: outcomeTone.accent,
                                    border: `1px solid ${outcomeTone.border}`,
                                    background: outcomeTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {outcomeText}
                            </span>
                        </div>
                    )}
                    {durationText && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{durationLabel}</span>
                            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--theme-text)' }}>{durationText}</span>
                        </div>
                    )}
                    {snapshot?.guardrailStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{guardLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-guardrail"
                                data-guardrail-summary={snapshot.guardrailSummary || ''}
                                title={snapshot.guardrailSummary || guardrailText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: guardrailTone.accent,
                                    border: `1px solid ${guardrailTone.border}`,
                                    background: guardrailTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[guardrailText, formatCountBadge(snapshot.guardrailCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.commandStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{commandLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-commands"
                                data-command-summary={snapshot.commandSummary || ''}
                                title={snapshot.commandSummary || commandText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: commandTone.accent,
                                    border: `1px solid ${commandTone.border}`,
                                    background: commandTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[commandText, formatCountBadge(snapshot.commandCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.fileActivityStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{fileActivityLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-file-activity"
                                data-file-activity-summary={snapshot.fileActivitySummary || ''}
                                data-file-activity-detail={snapshot.fileActivityDetail || ''}
                                title={snapshot.fileActivitySummary || snapshot.fileActivityDetail || fileActivityText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: fileActivityTone.accent,
                                    border: `1px solid ${fileActivityTone.border}`,
                                    background: fileActivityTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[fileActivityText, snapshot.fileActivityDetail ? `(${snapshot.fileActivityDetail})` : formatCountBadge(snapshot.fileActivityCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.qualityStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{qualityLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-quality"
                                data-quality-summary={snapshot.qualitySummary || ''}
                                title={snapshot.qualitySummary || qualityText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: qualityTone.accent,
                                    border: `1px solid ${qualityTone.border}`,
                                    background: qualityTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[qualityText, formatCountBadge(snapshot.qualityCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.explorationStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{exploreLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-exploration"
                                data-exploration-summary={snapshot.explorationSummary || ''}
                                title={snapshot.explorationSummary || explorationText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: explorationTone.accent,
                                    border: `1px solid ${explorationTone.border}`,
                                    background: explorationTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[explorationText, formatCountBadge(snapshot.explorationCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.verificationStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{verifyLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-verification"
                                data-verification-summary={snapshot.verificationSummary || ''}
                                title={snapshot.verificationSummary || verificationText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: verificationTone.accent,
                                    border: `1px solid ${verificationTone.border}`,
                                    background: verificationTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {[verificationText, formatCountBadge(snapshot.verificationCount)].filter(Boolean).join(' ')}
                            </span>
                        </div>
                    )}
                    {snapshot?.diffCheckStatus && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{diffCheckLabel}</span>
                            <span
                                data-testid="sidebar-coding-agent-diff-check"
                                data-diff-check-summary={snapshot.diffCheckSummary || ''}
                                title={snapshot.diffCheckSummary || diffCheckText}
                                style={{
                                    minWidth: 0,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    color: diffCheckTone.accent,
                                    border: `1px solid ${diffCheckTone.border}`,
                                    background: diffCheckTone.bg,
                                    borderRadius: '6px',
                                    padding: '1px 5px',
                                    fontWeight: 650,
                                }}
                            >
                                {diffCheckText}
                            </span>
                        </div>
                    )}
                    {diffSummary && (
                        <div style={{ minWidth: 0, display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <span style={{ flexShrink: 0 }}>{diffLabel}</span>
                            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--theme-text)' }}>{diffSummary}</span>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
};
