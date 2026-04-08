type Props = {
  selectedTask: string;
  selectedColleagueName?: string;
  onOpenNewTask: () => void;
};

const queue = ['周报汇总', '异常说明', '会议纪要'];

export function FocusPanel({ selectedTask, selectedColleagueName, onOpenNewTask }: Props) {
  return (
    <div className="dw-focus-panel dw-inspector-section">
      <div className="dw-inspector-rowhead">
        <div>
          <h3>{selectedTask || '准备开始新的任务'}</h3>
          <p>{selectedColleagueName ? `优先交给 ${selectedColleagueName}` : '先输入任务，再决定是否进入完整编辑'}</p>
        </div>
      </div>
      <div className="dw-focus-highlight dw-focus-highlight-compact">
        <label>协作模式</label>
        <strong>{selectedColleagueName || '自动匹配数字化同事'}</strong>
      </div>
      <div className="dw-focus-actions">
        <button type="button" className="primary" onClick={onOpenNewTask}>进入新建任务</button>
      </div>
      <div className="dw-mini-queue">
        {queue.map((item) => (
          <div key={item} className="dw-mini-queue-item dw-inspector-list-item">
            <strong>{item}</strong>
            <span>入口</span>
          </div>
        ))}
      </div>
    </div>
  );
}
