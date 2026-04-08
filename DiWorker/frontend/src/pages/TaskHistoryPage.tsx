import type { HistoryTaskItem } from '../types';

type Props = {
  tasks: HistoryTaskItem[];
  onResumeTask: (task: HistoryTaskItem) => void;
  onViewResult: (task: HistoryTaskItem) => void;
  onCloneTask: (task: HistoryTaskItem) => void;
  onDeleteTask: (task: HistoryTaskItem) => void;
  viewedTask: HistoryTaskItem | null;
};

export function TaskHistoryPage({ tasks, onResumeTask, onViewResult, onCloneTask, onDeleteTask, viewedTask }: Props) {
  const summaries = [
    { label: '处理中', value: tasks.filter((task) => task.status === '处理中').length.toString() },
    { label: '已完成', value: tasks.filter((task) => task.status === '已完成').length.toString() },
    { label: '待确认', value: tasks.filter((task) => task.status === '待确认').length.toString() },
  ];

  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header">
          <div>
            <span className="eyebrow">历史任务</span>
            <h2>历史任务</h2>
          </div>
          <small>像记录列表一样查看最近结果、继续处理或重新发起任务。</small>
        </div>
        <div className="dw-task-layout">
          <div className="dw-task-main">
            <div className="dw-history-table dw-history-table-compact">
              {tasks.map((task) => (
                <div key={task.id} className="dw-history-row dw-history-row-compact">
                  <div>
                    <strong>{task.title}</strong>
                    <p>{task.description}</p>
                  </div>
                  <div className="dw-history-meta">
                    <span>处理同事：{task.owner}</span>
                    <span>状态：{task.status}</span>
                    <span>更新时间：{task.updatedAt}</span>
                  </div>
                  <div className="dw-history-actions">
                    <button type="button" className="secondary" onClick={() => onResumeTask(task)}>继续处理</button>
                    <button type="button" className="secondary" onClick={() => onViewResult(task)}>查看结果</button>
                    <button type="button" className="secondary" onClick={() => onCloneTask(task)}>复制为新任务</button>
                    <button type="button" className="secondary" onClick={() => onDeleteTask(task)}>删除任务</button>
                  </div>
                </div>
              ))}
            </div>
            {viewedTask?.result ? (
              <div className="dw-feedback-card dw-feedback-success">
                <div className="dw-feedback-meta">
                  <span className="dw-status-pill">任务：{viewedTask.title}</span>
                  <span className="dw-status-pill">同事：{viewedTask.owner}</span>
                  {viewedTask.model ? <span className="dw-status-pill">模型：{viewedTask.model}</span> : null}
                  <span className="dw-status-pill">输出：{viewedTask.expectedOutput || 'summary'}</span>
                </div>
                <strong>历史结果</strong>
                <pre>{viewedTask.result}</pre>
              </div>
            ) : null}
          </div>
          <aside className="dw-task-side">
            <div className="card-subtle dw-side-panel-block">
              <label>当前状态</label>
              <strong>{viewedTask ? `正在查看：${viewedTask.title}` : '历史任务续办区'}</strong>
              <p>{viewedTask ? '当前展示的是历史结果，可直接对照查看，不会进入编辑态。' : '这里会汇总最近任务状态，并作为继续处理入口。'}</p>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>任务状态</label>
              <div className="dw-summary-grid">
                {summaries.map((item) => (
                  <div key={item.label}>
                    <strong>{item.value}</strong>
                    <span>{item.label}</span>
                  </div>
                ))}
              </div>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>续办建议</label>
              <p>优先打开“处理中”和“待确认”的任务，它们最适合作为今天的继续处理入口。</p>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
