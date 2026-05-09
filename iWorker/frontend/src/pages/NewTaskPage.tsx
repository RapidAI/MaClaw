import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import type { CenterTaskContext, TaskAttachment } from '../types';

type SubmitTaskResult = {
  task_type: string;
  task_title?: string;
  colleague_name: string;
  expected_output: string;
  model: string;
  content: string;
};

type Props = {
  selectedTask: string;
  selectedColleagueName: string;
  draft: string;
  expectedOutput: string;
  attachments: TaskAttachment[];
  centerTaskContext: CenterTaskContext | null;
  submitting: boolean;
  submitError: string;
  submitResult: SubmitTaskResult | null;
  onDraftChange: (value: string) => void;
  onExpectedOutputChange: (value: string) => void;
  onApplySuggestion: (suggestion: string) => void;
  onAddAttachment: (files: FileList | null) => void;
  onRemoveAttachment: (attachmentId: string) => void;
  onClearTask: () => void;
  onSubmit: () => void;
  onOpenColleagues: () => void;
};

const suggestions = ['Daily brief', 'Exception note', 'Meeting summary'];
const suggestionDrafts: Record<string, string> = {
  'Daily brief': "Prepare a concise daily work brief. Include completed work, current risks, blockers, and tomorrow's plan.",
  'Exception note': 'Explain the exception, business impact, likely cause, current handling progress, and recommended next step.',
  'Meeting summary': 'Summarize the meeting decisions, owners, due dates, unresolved questions, and follow-up actions.',
};
const handoffSteps = ['understand', 'assign', 'return'] as const;

export function NewTaskPage({
  selectedTask,
  selectedColleagueName,
  draft,
  expectedOutput,
  attachments,
  centerTaskContext,
  submitting,
  submitError,
  submitResult,
  onDraftChange,
  onExpectedOutputChange,
  onApplySuggestion,
  onAddAttachment,
  onRemoveAttachment,
  onClearTask,
  onSubmit,
  onOpenColleagues,
}: Props) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const isInterventionTask = Boolean(centerTaskContext) || draft.includes('Human intervention needed:') || draft.includes('需要人工介入：') || draft.includes('This push is cached.') || draft.includes('此推送来自缓存。');
  const isCachedInterventionTask = Boolean(centerTaskContext?.cached) || draft.includes('This push is cached.') || draft.includes('此推送来自缓存。');
  const interventionTitle = centerTaskContext?.title || selectedTask || t('newTask.humanTask', 'Human intervention task');
  const interventionSource = centerTaskContext
    ? centerTaskContext.cached
      ? t('newTask.cachedPush', 'Cached Center push')
      : centerTaskContext.kind === 'collaboration'
        ? t('newTask.centerHandoff', 'Center handoff')
        : t('newTask.livePush', 'Live Center push')
    : isCachedInterventionTask ? t('newTask.cachedPush', 'Cached Center push') : t('newTask.livePush', 'Live Center push');
  const interventionDetail = centerTaskContext?.detail || (isCachedInterventionTask
    ? t('newTask.cachedPushDetail', 'This is a cached Center push. Reconnect iWorkerCenter before Resume, Block, or Run actions so the decision is applied to the live task.')
    : t('newTask.livePushDetail', 'Capture the human decision here, then return to the workbench to Resume or Block the live Center push.'));
  const outputLabel = t(`newTask.output.${expectedOutput}`, expectedOutput);

  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header">
          <div>
            <span className="eyebrow">{t('newTask.eyebrow', 'Task space')}</span>
            <h2>{t('newTask.title', 'New task')}</h2>
          </div>
          <small>{t('newTask.subtitle', 'Collect the request, evidence, output format, and coworker handoff in one focused workspace.')}</small>
        </div>
        {isInterventionTask ? (
          <section className={`dw-center-push-context ${isCachedInterventionTask ? 'is-cached' : ''}`} aria-label={t('newTask.centerPushAria', 'Center push intervention task')}>
            <div>
              <span>{interventionSource}</span>
              <strong>{interventionTitle}</strong>
              <p>{interventionDetail}</p>
              {centerTaskContext?.workflowStepInstanceId ? <small>{t('newTask.workflowStep', 'Workflow step')}: {centerTaskContext.workflowStepInstanceId}</small> : null}
            </div>
            <span className="dw-center-push-context-badge">{isCachedInterventionTask ? t('newTask.displayOnly', 'Display only') : t('newTask.decisionNeeded', 'Decision needed')}</span>
          </section>
        ) : null}
        <div className="dw-task-layout">
          <div className="dw-task-main dw-editor-main">
            <div className="dw-editor-toolbar card-subtle">
              <div className="dw-form-grid">
                <label>
                  {t('newTask.taskType', 'Task type')}
                  <input value={selectedTask || t('newTask.freeInput', 'Free input')} readOnly />
                </label>
                <label>
                  {t('newTask.expectedOutput', 'Expected output')}
                  <select value={expectedOutput} onChange={(event) => onExpectedOutputChange(event.target.value)}>
                    <option value="summary">{t('newTask.output.summary', 'Summary / brief')}</option>
                    <option value="document">{t('newTask.output.document', 'Formal document')}</option>
                    <option value="table">{t('newTask.output.table', 'Structured table')}</option>
                  </select>
                </label>
              </div>
            </div>
            <section className="card-subtle dw-editor-section">
              <div className="section-head with-gap">
                <div>
                  <span className="eyebrow">{t('newTask.contentEyebrow', 'Task content')}</span>
                  <h3>{t('newTask.editRequest', 'Edit request')}</h3>
                </div>
                <small>{draft.trim() ? t('newTask.readyHint', 'Request content is ready. Add evidence or clarify the human decision if needed.') : t('newTask.emptyHint', 'Start with the goal, then add evidence and output requirements.')}</small>
              </div>
              <label>
                {t('newTask.request', 'Request')}
                <textarea value={draft} onChange={(event) => onDraftChange(event.target.value)} rows={8} placeholder={t('newTask.requestPlaceholder', 'Describe the work you want this digital coworker to complete.')} />
              </label>
              <div className="dw-composer-toolbar">
                {suggestions.map((item) => (
                  <button key={item} type="button" className="chip-button" onClick={() => onApplySuggestion(suggestionDrafts[item])}>{t(`newTask.suggestions.${item}`, item)}</button>
                ))}
              </div>
            </section>
            <section className="card-subtle dw-editor-section">
              <div className="section-head with-gap">
                <div>
                  <span className="eyebrow">{t('newTask.actionsEyebrow', 'Task actions')}</span>
                  <h3>{t('newTask.handoffControls', 'Handoff controls')}</h3>
                </div>
                <small>{t('newTask.actionsHint', 'Add evidence, choose a partner, clear the draft, or start work.')}</small>
              </div>
              <div className="dw-composer-actions">
                <input ref={fileInputRef} type="file" multiple className="dw-hidden-file-input" onChange={(event) => { onAddAttachment(event.target.files); event.target.value = ''; }} />
                <button type="button" className="light-button" onClick={() => fileInputRef.current?.click()}>{t('newTask.uploadEvidence', 'Upload evidence')}</button>
                <button type="button" className="light-button" onClick={onOpenColleagues}>{t('newTask.choosePartner', 'Choose partner')}</button>
                <button type="button" className="secondary" onClick={onClearTask}>{t('newTask.clearTask', 'Clear task')}</button>
                <button type="button" className="primary" onClick={onSubmit} disabled={submitting || !draft.trim()}>{submitting ? t('newTask.working', 'Working...') : t('newTask.startWork', 'Start work')}</button>
              </div>
            </section>
            {attachments.length > 0 ? (
              <section className="dw-attachment-list card-subtle dw-editor-section">
                <div className="dw-attachment-summary">
                  <label>{t('newTask.attachedEvidence', 'Attached evidence')}</label>
                  <strong>{t('newTask.itemCount', { count: attachments.length, defaultValue: '{{count}} item' })}</strong>
                </div>
                <div className="dw-attachment-items">
                  {attachments.map((attachment) => (
                    <div key={attachment.id} className="dw-attachment-item">
                      <div>
                        <strong>{attachment.name}</strong>
                        <span className="dw-attachment-meta">{attachment.type} / {attachment.sizeLabel}</span>
                        <p>{attachment.summary}</p>
                      </div>
                      <button type="button" className="secondary" onClick={() => onRemoveAttachment(attachment.id)}>{t('newTask.remove', 'Remove')}</button>
                    </div>
                  ))}
                </div>
              </section>
            ) : null}
            {submitError ? <div className="dw-feedback-card dw-feedback-error">{submitError}</div> : null}
            {submitResult ? (
              <div className="dw-feedback-card dw-feedback-success">
                <div className="dw-feedback-meta">
                  <span className="dw-status-pill">{t('newTask.resultTask', 'Task')}: {submitResult.task_title || submitResult.task_type}</span>
                  <span className="dw-status-pill">{t('newTask.resultCoworker', 'Coworker')}: {submitResult.colleague_name}</span>
                  <span className="dw-status-pill">{t('newTask.resultModel', 'Model')}: {submitResult.model}</span>
                </div>
                <strong>{t('newTask.result', 'Result')}</strong>
                <pre>{submitResult.content}</pre>
              </div>
            ) : null}
          </div>
          <aside className="dw-task-side">
            <div className="card-subtle dw-side-panel-block">
              <label>{t('newTask.selectedPartner', 'Selected partner')}</label>
              <strong>{selectedColleagueName ? selectedColleagueName : t('newTask.autoMatch', 'Auto-match from task context')}</strong>
              <p>{t('newTask.partnerHint', 'Selections from the workbench or colleague view stay attached to this task.')}</p>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>{t('newTask.outputContract', 'Output contract')}</label>
              <strong>{outputLabel}</strong>
              <p>{t('newTask.outputHint', 'The digital coworker should return a result that is ready for review, handoff, or direct use.')}</p>
            </div>
            <div className="card-subtle dw-handoff-list dw-side-panel-block">
              <label>{t('newTask.handoffFlow', 'Handoff flow')}</label>
              {handoffSteps.map((item) => (
                <div key={item} className="dw-handoff-item">
                  <strong>{t(`newTask.handoff.${item}.title`)}</strong>
                  <p>{t(`newTask.handoff.${item}.detail`)}</p>
                </div>
              ))}
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
