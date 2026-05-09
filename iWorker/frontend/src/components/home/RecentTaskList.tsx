import { useTranslation } from 'react-i18next';
import type { HistoryTaskItem } from '../../types';

type Props = {
  tasks: HistoryTaskItem[];
  onOpenTask: (task: HistoryTaskItem) => void;
};

export function RecentTaskList({ tasks, onOpenTask }: Props) {
  const { t } = useTranslation();

  return (
    <section className="card dw-recent-task-card dw-recent-task-card-compact dw-embedded-list-card">
      <div className="dw-pane-head">
        <strong>{t('recentTaskList.title', 'Continue work')}</strong>
        <span>{t('recentTaskList.subtitle', 'Recent records')}</span>
      </div>
      <div className="dw-recent-task-stack dw-recent-task-stack-compact">
        {tasks.map((task) => (
          <button key={task.id} type="button" className="dw-recent-task-item dw-recent-task-item-compact dw-embedded-list-item" onClick={() => onOpenTask(task)}>
            <div className="dw-recent-task-main">
              <div className="dw-recent-task-head">
                <strong>{task.title}</strong>
                <span className="dw-recent-task-status">{task.status}</span>
              </div>
              <p>{task.description}</p>
            </div>
            <div className="dw-recent-task-meta">
              <span>{task.owner}</span>
              <span>{task.updatedAt}</span>
            </div>
          </button>
        ))}
      </div>
    </section>
  );
}
