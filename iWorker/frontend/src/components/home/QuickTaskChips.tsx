import { useTranslation } from 'react-i18next';

type Props = {
  tasks: string[];
  onPick?: (task: string) => void;
};

export function QuickTaskChips({ tasks, onPick }: Props) {
  const { t } = useTranslation();

  return (
    <section className="card dw-quick-entry-card dw-quick-entry-card-compact dw-embedded-list-card">
      <div className="dw-pane-head">
        <strong>{t('quickTaskChips.title', 'Quick start')}</strong>
        <span>{t('quickTaskChips.subtitle', 'Common tasks')}</span>
      </div>
      <div className="dw-quick-entry-grid dw-quick-entry-grid-compact">
        {tasks.map((task) => (
          <button key={task} type="button" className="dw-quick-entry-button dw-embedded-list-item" onClick={() => onPick?.(task)}>
            <strong>{task}</strong>
            <span>{t('quickTaskChips.apply', 'Apply')}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
