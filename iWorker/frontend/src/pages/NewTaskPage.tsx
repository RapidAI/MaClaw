import { useRef } from 'react';
import type { TaskAttachment } from '../types';

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

const suggestions = ['日报整理', '异常说明', '会议纪要'];
const suggestionDrafts: Record<string, string> = {
  日报整理: '请根据今天的工作进展整理一份日报，突出已完成事项、当前风险和明日计划。',
  异常说明: '请说明本次异常的现象、影响范围、初步原因、当前处理进度和后续建议。',
  会议纪要: '请整理本次会议纪要，包含核心结论、责任人、时间节点和待办事项。',
};
const handoffSteps = [
  { title: '理解任务', detail: '先识别目标、材料和预期输出' },
  { title: '分派同事', detail: '按任务类型分配给合适的数字化同事' },
  { title: '生成结果', detail: '输出摘要、文档或结构化表格' },
];

export function NewTaskPage({
  selectedTask,
  selectedColleagueName,
  draft,
  expectedOutput,
  attachments,
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
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header">
          <div>
            <span className="eyebrow">新建任务</span>
            <h2>新建任务</h2>
          </div>
          <small>像编辑器一样补全任务内容、材料和输出格式，再交给数字化同事处理。</small>
        </div>
        <div className="dw-task-layout">
          <div className="dw-task-main dw-editor-main">
            <div className="dw-editor-toolbar card-subtle">
              <div className="dw-form-grid">
                <label>
                  任务类型
                  <input value={selectedTask || '自由输入'} readOnly />
                </label>
                <label>
                  预期输出
                  <select value={expectedOutput} onChange={(event) => onExpectedOutputChange(event.target.value)}>
                    <option value="summary">摘要 / 汇报</option>
                    <option value="document">正式文档</option>
                    <option value="table">结构化表格</option>
                  </select>
                </label>
              </div>
            </div>
            <section className="card-subtle dw-editor-section">
              <div className="section-head with-gap">
                <div>
                  <span className="eyebrow">任务内容</span>
                  <h3>编辑需求描述</h3>
                </div>
                <small>{draft.trim() ? '已输入任务内容，可继续补充细节。' : '先输入任务目标，再补充材料和输出要求。'}</small>
              </div>
              <label>
                需求描述
                <textarea value={draft} onChange={(event) => onDraftChange(event.target.value)} rows={8} placeholder="请告诉我你想完成什么工作" />
              </label>
              <div className="dw-composer-toolbar">
                {suggestions.map((item) => (
                  <button key={item} type="button" className="chip-button" onClick={() => onApplySuggestion(suggestionDrafts[item])}>{item}</button>
                ))}
              </div>
            </section>
            <section className="card-subtle dw-editor-section">
              <div className="section-head with-gap">
                <div>
                  <span className="eyebrow">任务操作</span>
                  <h3>处理入口</h3>
                </div>
                <small>支持补充材料、切换协作同事或直接提交处理。</small>
              </div>
              <div className="dw-composer-actions">
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  className="dw-hidden-file-input"
                  onChange={(event) => {
                    onAddAttachment(event.target.files);
                    event.target.value = '';
                  }}
                />
                <button type="button" className="light-button" onClick={() => fileInputRef.current?.click()}>上传材料</button>
                <button type="button" className="light-button" onClick={onOpenColleagues}>选择同事</button>
                <button type="button" className="secondary" onClick={onClearTask}>清空当前任务</button>
                <button type="button" className="primary" onClick={onSubmit} disabled={submitting || !draft.trim()}>{submitting ? '处理中...' : '开始处理'}</button>
              </div>
            </section>
            {attachments.length > 0 ? (
              <section className="dw-attachment-list card-subtle dw-editor-section">
                <div className="dw-attachment-summary">
                  <label>已添加材料</label>
                  <strong>共 {attachments.length} 份材料</strong>
                </div>
                <div className="dw-attachment-items">
                  {attachments.map((attachment) => (
                    <div key={attachment.id} className="dw-attachment-item">
                      <div>
                        <strong>{attachment.name}</strong>
                        <span className="dw-attachment-meta">{attachment.type} · {attachment.sizeLabel}</span>
                        <p>{attachment.summary}</p>
                      </div>
                      <button type="button" className="secondary" onClick={() => onRemoveAttachment(attachment.id)}>移除</button>
                    </div>
                  ))}
                </div>
              </section>
            ) : null}
            {submitError ? <div className="dw-feedback-card dw-feedback-error">{submitError}</div> : null}
            {submitResult ? (
              <div className="dw-feedback-card dw-feedback-success">
                <div className="dw-feedback-meta">
                  <span className="dw-status-pill">任务：{submitResult.task_title || submitResult.task_type}</span>
                  <span className="dw-status-pill">同事：{submitResult.colleague_name}</span>
                  <span className="dw-status-pill">模型：{submitResult.model}</span>
                </div>
                <strong>处理结果</strong>
                <pre>{submitResult.content}</pre>
              </div>
            ) : null}
          </div>
          <aside className="dw-task-side">
            <div className="card-subtle dw-side-panel-block">
              <label>当前已选同事</label>
              <strong>{selectedColleagueName ? `已选同事：${selectedColleagueName}` : '暂未指定，按任务自动匹配'}</strong>
              <p>如果你已经从首页或找同事页选了人，会在这里继续带入。</p>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>推荐接手</label>
              <strong>{selectedTask ? '按当前任务自动匹配同事' : '先输入任务后自动推荐'}</strong>
              <p>根据任务类型、材料和输出格式，给出合适同事组合。</p>
            </div>
            <div className="card-subtle dw-handoff-list dw-side-panel-block">
              <label>处理接力</label>
              {handoffSteps.map((item) => (
                <div key={item.title} className="dw-handoff-item">
                  <strong>{item.title}</strong>
                  <p>{item.detail}</p>
                </div>
              ))}
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
