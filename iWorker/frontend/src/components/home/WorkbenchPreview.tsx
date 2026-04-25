type Props = {
  selectedTask: string;
  draft: string;
};

const timeline = [
  { label: '需求进入', detail: '识别目标与输出', status: 'done' },
  { label: '分派同事', detail: '按任务类型推荐处理人', status: 'active' },
  { label: '整理结果', detail: '输出摘要或文档', status: 'todo' },
];

const signalCards = [
  { label: '状态', value: '待开始' },
  { label: '推荐', value: '小迪 + 阿宁' },
  { label: '输出', value: '摘要 / 文档' },
];

export function WorkbenchPreview({ selectedTask, draft }: Props) {
  const title = selectedTask || '自由任务';
  const summary = draft.trim() || '帮我把今天的异常情况整理成简洁汇报，给主管同步。';

  return (
    <div className="dw-workbench dw-workbench-compact dw-inspector-section">
      <div className="dw-inspector-rowhead">
        <div>
          <h3>当前任务</h3>
          <p>{selectedTask ? `已带入：${selectedTask}` : '未指定任务'}</p>
        </div>
      </div>
      <div className="dw-workbench-signal-grid">
        {signalCards.map((item) => (
          <div key={item.label} className="dw-workbench-signal-card">
            <label>{item.label}</label>
            <strong>{item.value}</strong>
          </div>
        ))}
      </div>
      <div className="dw-workbench-compact-grid">
        <div className="dw-workbench-summary-card">
          <label>任务快照</label>
          <strong>{title}</strong>
          <p>{summary}</p>
        </div>
        <div className="dw-workbench-summary-card">
          <label>处理进度</label>
          <div className="dw-timeline dw-timeline-compact">
            {timeline.map((item) => (
              <div key={item.label} className={`dw-timeline-item ${item.status}`}>
                <span className="dw-timeline-dot" />
                <div>
                  <strong>{item.label}</strong>
                  <p>{item.detail}</p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
