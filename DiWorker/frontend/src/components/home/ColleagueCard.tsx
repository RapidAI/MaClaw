import type { Colleague } from '../../types';

type Props = {
  colleague: Colleague;
  compact?: boolean;
  isSelected?: boolean;
  onSelect?: (id: string) => void;
};

export function ColleagueCard({ colleague, compact = false, isSelected = false, onSelect }: Props) {
  if (compact) {
    return (
      <article className={`dw-colleague-card dw-colleague-card-compact${isSelected ? ' is-selected' : ''}`}>
        <div className="dw-colleague-compact-main">
          <div className="dw-avatar">{colleague.name.slice(0, 1)}</div>
          <div className="dw-colleague-head">
            <h3>{colleague.name}</h3>
            <span>{colleague.role}</span>
          </div>
        </div>
        <p>{colleague.description}</p>
        <div className="dw-chip-row">
          {colleague.strengths.slice(0, 3).map((item) => (
            <span key={item} className="dw-chip">{item}</span>
          ))}
        </div>
        <div className="dw-colleague-compact-footer">
          <span>{colleague.tasks[0]}</span>
          <button type="button" className="secondary" onClick={() => onSelect?.(colleague.id)}>
            找 TA 帮忙
          </button>
        </div>
      </article>
    );
  }

  return (
    <article className="dw-colleague-card card">
      <div className="dw-avatar">{colleague.name.slice(0, 1)}</div>
      <div className="dw-colleague-head">
        <h3>{colleague.name}</h3>
        <span>{colleague.role}</span>
      </div>
      <p>{colleague.description}</p>
      <div>
        <label>更擅长什么</label>
        <div className="dw-chip-row">
          {colleague.strengths.map((item) => (
            <span key={item} className="dw-chip">{item}</span>
          ))}
        </div>
      </div>
      <div>
        <label>会做的事</label>
        <ul>
          {colleague.tasks.map((task) => (
            <li key={task}>{task}</li>
          ))}
        </ul>
      </div>
      <button type="button" className="primary" onClick={() => onSelect?.(colleague.id)}>
        找 TA 帮忙
      </button>
    </article>
  );
}
