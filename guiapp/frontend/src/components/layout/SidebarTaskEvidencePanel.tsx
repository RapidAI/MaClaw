import { OpenFileOrShowInFolder } from '../../../wailsjs/go/main/App';
import { useState } from 'react';
import { localizeText } from '../../i18n';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';
import { normalizeWorkflowStatus, WorkflowStatus } from '../ai/workflowStatus';

const textForLang = localizeText;

function workflowEvidenceState(workflow: NonNullable<ProjectSceneDetail['active_workflow']>, lang: string) {
    const status = normalizeWorkflowStatus(workflow.status);
    const rawStatus = String(workflow.status || '').trim().toLowerCase();
    // A terminal snapshot can retain an earlier pending-review flag. Its final
    // outcome must remain authoritative so users are never invited to continue
    // an already completed or cancelled workflow.
    if (status === WorkflowStatus.Cancelled) {
        return {
            label: textForLang(lang, 'Workflow cancelled', '流程已取消', '流程已取消'),
            color: 'var(--theme-text-secondary)',
            canContinue: false,
        };
    }
    if (status === WorkflowStatus.Completed) {
        return {
            label: textForLang(lang, 'Workflow completed', '流程已完成', '流程已完成'),
            color: 'var(--theme-success)',
            canContinue: false,
        };
    }
    if (workflow.pending_review) {
        return {
            label: textForLang(lang, 'Workflow review needed', '流程待审核', '流程待審核'),
            color: 'var(--theme-warning)',
            canContinue: true,
        };
    }
    if (/(fail|error|blocked)/.test(rawStatus)) {
        return {
            label: textForLang(lang, 'Workflow needs attention', '流程需要处理', '流程需要處理'),
            color: 'var(--theme-danger, #b91c1c)',
            // These snapshots are not terminal. Keep recovery available while
            // making the condition clear before the user resumes it.
            canContinue: true,
        };
    }
    return {
        label: textForLang(lang, 'Original workflow unfinished', '原流程未完成', '原流程未完成'),
        color: 'var(--theme-primary)',
        canContinue: true,
    };
}

export function SidebarTaskEvidencePanel({ detail, loading, lang, onContinueWorkflow, error, onRetry }: {
    detail: ProjectSceneDetail | null;
    loading: boolean;
    lang: string;
    onContinueWorkflow?: (projectPath: string) => Promise<void> | void;
    error?: string;
    onRetry?: () => void;
}) {
    const [continuingWorkflow, setContinuingWorkflow] = useState(false);
    const artifacts = detail?.recent_artifacts || [];
    const activeWorkflow = detail?.active_workflow;
    const workflowProjectPath = activeWorkflow?.project_path || detail?.project_path || '';
    const workflowState = activeWorkflow ? workflowEvidenceState(activeWorkflow, lang) : null;
    return <div style={{ margin: '3px 0 5px 22px', padding: '6px 7px', border: '1px solid var(--theme-border)', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-surface) 88%, var(--theme-text-primary) 4%)', minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: artifacts.length > 0 || loading ? '4px' : 0 }}>
            <span style={{ fontSize: '0.64rem', fontWeight: 700, color: 'var(--theme-text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {loading ? textForLang(lang, 'Loading evidence...', '正在加载证据...', '正在載入證據...') : textForLang(lang, 'Recent artifact sources', '最近产物来源', '最近產物來源')}
            </span>
            {detail?.entry_count !== undefined && <span style={{ fontSize: '0.6rem', color: 'var(--theme-text-muted)', opacity: 0.75, flexShrink: 0 }}>{detail.entry_count}</span>}
        </div>
        {!loading && activeWorkflow && <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '5px', minWidth: 0 }}>
            <span data-testid="task-evidence-workflow-state" title={`${activeWorkflow.type || 'workflow'} ${activeWorkflow.phase || ''}`.trim()} style={{ flex: 1, minWidth: 0, fontSize: '0.62rem', color: workflowState?.color, fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{workflowState?.label}</span>
            {workflowState?.canContinue && onContinueWorkflow && workflowProjectPath && <button type="button" disabled={continuingWorkflow} onClick={event => {
                event.stopPropagation();
                if (continuingWorkflow) return;
                setContinuingWorkflow(true);
                void Promise.resolve(onContinueWorkflow(workflowProjectPath))
                    .catch(error => { console.error('[SidebarTaskEvidencePanel] continue workflow failed:', error); })
                    .finally(() => { setContinuingWorkflow(false); });
            }} style={{ border: '1px solid color-mix(in srgb, var(--theme-primary) 42%, transparent)', background: 'color-mix(in srgb, var(--theme-primary) 8%, transparent)', color: 'var(--theme-primary)', borderRadius: '999px', cursor: continuingWorkflow ? 'default' : 'pointer', padding: '2px 7px', fontSize: '0.6rem', fontWeight: 700, flexShrink: 0, opacity: continuingWorkflow ? 0.6 : 1 }}>{continuingWorkflow ? textForLang(lang, 'Opening workflow...', '正在打开流程...', '正在開啟流程...') : textForLang(lang, 'Continue workflow', '\u7ee7\u7eed\u539f\u6d41\u7a0b', '\u7e7c\u7e8c\u539f\u6d41\u7a0b')}</button>}
        </div>}
        {!loading && !error && artifacts.length === 0 && <div style={{ fontSize: '0.64rem', color: 'var(--theme-text-muted)', opacity: 0.75 }}>{textForLang(lang, 'No source-backed artifacts yet', '暂无可回查产物', '暫無可回查產物')}</div>}
        {!loading && error && <div role="status" style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.64rem', color: 'var(--theme-text-secondary)', lineHeight: 1.35 }}>
            <span style={{ minWidth: 0, flex: 1 }}>{error}</span>
            {onRetry && <button type="button" onClick={event => { event.stopPropagation(); onRetry(); }} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', padding: '1px 0', fontSize: '0.62rem', fontWeight: 700, flexShrink: 0 }}>{textForLang(lang, 'Retry', '重试', '重試')}</button>}
        </div>}
        {artifacts.slice(0, 3).map((artifact, index) => {
            const label = artifact.title || artifact.preview || artifact.source_url || textForLang(lang, 'Artifact', '产物', '產物');
            const source = artifact.source_url ? artifact.source_url + (artifact.source_hint ? '; ' + artifact.source_hint : '') : '';
            return <div key={artifact.source_url || label + index} style={{ display: 'flex', alignItems: 'center', gap: '5px', minWidth: 0, marginTop: index === 0 ? 0 : '3px' }}>
                <span title={source || label} style={{ flex: 1, minWidth: 0, fontSize: '0.64rem', color: 'var(--theme-text-primary)', opacity: 0.82, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
                {artifact.source_url && <button type="button" aria-label={textForLang(lang, 'Open artifact source', '打开产物来源', '打開產物來源')} title={source} onClick={event => { event.stopPropagation(); void OpenFileOrShowInFolder(artifact.source_url || ''); }} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', width: '18px', height: '18px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="externalLink" size={12} /></button>}
            </div>;
        })}
    </div>;
}
