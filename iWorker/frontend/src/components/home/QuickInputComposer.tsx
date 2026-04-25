type Props = {
  value: string;
  selectedTask: string;
  selectedColleagueName: string;
  onChange: (value: string) => void;
  onPickTask: (task: string) => void;
  onOpenNewTask: () => void;
};

const quickModes = [
  { label: '汇报', task: '异常说明' },
  { label: '纪要', task: '会议纪要' },
  { label: '表格', task: '整理表格' },
  { label: '通知', task: '写通知' },
];

const quickCategories = [
  { title: '写汇报', task: '异常说明' },
  { title: '做纪要', task: '会议纪要' },
  { title: '整理表格', task: '整理表格' },
];

export function QuickInputComposer({ value, selectedTask, selectedColleagueName, onChange, onPickTask, onOpenNewTask }: Props) {
  const activeModeTask = quickModes.find((item) => item.task === selectedTask)?.task || '';

  return (
    <section className="card dw-composer dw-home-composer dw-home-composer-panel">
      <div className="dw-home-composer-strip">
        <div className="dw-home-mode-bar">
          {quickModes.map((item) => (
            <button
              key={item.label}
              type="button"
              className={`dw-home-mode-tab${activeModeTask === item.task ? ' is-active' : ''}`}
              onClick={() => onPickTask(item.task)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <span className="dw-toolbar-meta">{selectedColleagueName ? `已选：${selectedColleagueName}` : '自动匹配'}</span>
      </div>
      <div className="dw-home-composer-body">
        <div className="dw-home-composer-editor">
          <div className="dw-home-composer-prompt dw-home-composer-prompt-compact">
            <strong>{selectedTask || '直接输入任务'}</strong>
            <span>写清目标、范围和想要的结果。</span>
          </div>
          <textarea
            value={value}
            onChange={(event) => onChange(event.target.value)}
            placeholder="例如：整理今天的生产异常，并生成一份汇报摘要"
            rows={6}
          />
        </div>
        <aside className="dw-home-composer-sidebar">
          <div className="dw-home-sidebar-inline">
            <label>快捷带入</label>
            <div className="dw-home-composer-categories">
              {quickCategories.map((item) => (
                <button key={item.title} type="button" className="dw-home-category-button" onClick={() => onPickTask(item.task)}>
                  <strong>{item.title}</strong>
                </button>
              ))}
            </div>
          </div>
          <div className="dw-home-sidebar-inline dw-home-sidebar-inline-status">
            <label>当前</label>
            <strong>{selectedTask || '未指定'}</strong>
            <p>{selectedColleagueName ? `已带入 ${selectedColleagueName}` : '未指定同事'}</p>
          </div>
        </aside>
      </div>
      <div className="dw-composer-actions dw-home-composer-actions">
        <button type="button" className="secondary" onClick={onOpenNewTask}>打开</button>
        <button type="button" className="primary" onClick={onOpenNewTask}>开始</button>
      </div>
    </section>
  );
}
