type Props = {
  greeting: string;
  hint: string;
  selectedTask?: string;
  selectedColleagueName?: string;
};

export function WelcomeHero({ greeting, hint, selectedTask, selectedColleagueName }: Props) {
  return (
    <section className="dw-hero card dw-hero-native">
      <div className="dw-hero-main dw-hero-main-compact">
        <div className="dw-hero-copy dw-hero-copy-compact">
          <div>
            <h2>{greeting}</h2>
            <p>{selectedTask ? `当前任务：${selectedTask}` : hint}</p>
          </div>
        </div>
        <div className="dw-hero-status-row dw-hero-status-row-native">
          <span className="dw-toolbar-meta">{selectedColleagueName ? `已选：${selectedColleagueName}` : '自动匹配'}</span>
        </div>
      </div>
    </section>
  );
}
