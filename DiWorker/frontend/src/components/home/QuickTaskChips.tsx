type Props = {
  tasks: string[];
  onPick?: (task: string) => void;
};

export function QuickTaskChips({ tasks, onPick }: Props) {
  return (
    <section className="card dw-quick-entry-card dw-quick-entry-card-compact dw-embedded-list-card">
      <div className="dw-pane-head">
        <strong>快速开始</strong>
        <span>常用任务</span>
      </div>
      <div className="dw-quick-entry-grid dw-quick-entry-grid-compact">
        {tasks.map((task) => (
          <button key={task} type="button" className="dw-quick-entry-button dw-embedded-list-item" onClick={() => onPick?.(task)}>
            <strong>{task}</strong>
            <span>带入</span>
          </button>
        ))}
      </div>
    </section>
  );
}
