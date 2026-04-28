import { useEffect, useState } from 'react';
import { quickTasks as defaultQuickTasks } from '../mock/tasks';
import type { CenterGoalPush, HistoryTaskItem } from '../types';

type Props = {
  draft: string;
  selectedTask: string;
  selectedColleagueName: string;
  recentTasks: HistoryTaskItem[];
  goalPushes: CenterGoalPush[];
  goalPushLoading: boolean;
  goalPushError: string;
  goalPushAckingId: string;
  onDraftChange: (value: string) => void;
  onPickTask: (task: string, colleagueName?: string) => void;
  onOpenNewTask: () => void;
  onOpenRecentTask: (task: HistoryTaskItem) => void;
  onRefreshGoalPushes: () => void | Promise<void>;
  onAckGoalPush: (eventId: string, status: 'resumed' | 'blocked') => void | Promise<void>;
};

type WorkMode = 'voice' | 'text';

const skillChips = [
  'Talk to my iWorker',
  'Summarize what changed',
  'Ask the organization',
  'Prepare handoff evidence',
  'Draft a customer reply',
  'Check policy memory',
];

const statusCards = [
  {
    label: 'Body node',
    value: 'Local container',
    detail: 'This computer runs the visible body and tool access for the digital worker.',
  },
  {
    label: 'Memory owner',
    value: 'Center synced',
    detail: 'Durable memory belongs to iWorkerCenter; local cache only accelerates access.',
  },
  {
    label: 'Human role',
    value: 'Callable skill',
    detail: 'People remain part of the organization, but execution continuity sits in the AI system.',
  },
];

export function HomePage({ draft, selectedTask, selectedColleagueName, recentTasks, goalPushes, goalPushLoading, goalPushError, goalPushAckingId, onDraftChange, onPickTask, onOpenNewTask, onOpenRecentTask, onRefreshGoalPushes, onAckGoalPush }: Props) {
  const [workMode, setWorkMode] = useState<WorkMode>('voice');
  const [quickTasks, setQuickTasks] = useState<string[]>(defaultQuickTasks);

  useEffect(() => {
    const welcomeLoader = (window as Window & {
      go?: {
        main?: {
          App?: {
            GetWelcomeData?: () => Promise<{ quick_tasks?: string[] }>;
          };
        };
      };
    }).go?.main?.App?.GetWelcomeData;

    if (!welcomeLoader) {
      return;
    }

    welcomeLoader()
      .then((data: { quick_tasks?: string[] }) => {
        if (data?.quick_tasks && data.quick_tasks.length > 0) {
          setQuickTasks(data.quick_tasks);
        }
      })
      .catch(() => {});
  }, []);

  const handleSubmit = () => {
    if (draft.trim() || selectedTask) {
      onOpenNewTask();
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      handleSubmit();
    }
  };

  const taskChips = workMode === 'voice' ? skillChips : quickTasks;

  return (
    <div className="iw-home-shell">
      <section className="iw-command-surface">
        <div className="iw-command-head">
          <div>
            <span className="iw-kicker">iWorker body</span>
            <h2>Speak to the digital employee, not the computer.</h2>
            <p>
              This desktop is the local body and tool container. Memory, policy, and reusable ability are synced back to iWorkerCenter so the company keeps running even when a body or person changes.
            </p>
          </div>
          <div className="iw-body-badge">
            <strong>{selectedColleagueName || 'Auto matched iWorker'}</strong>
            <span>{selectedTask || 'Ready for IM or voice instruction'}</span>
          </div>
        </div>

        <div className="iw-mode-switch" role="tablist" aria-label="Interaction mode">
          <button type="button" className={workMode === 'voice' ? 'is-active' : ''} onClick={() => setWorkMode('voice')}>Voice / IM</button>
          <button type="button" className={workMode === 'text' ? 'is-active' : ''} onClick={() => setWorkMode('text')}>Structured task</button>
        </div>

        <div className="iw-chip-row">
          {taskChips.map((task) => (
            <button key={task} type="button" onClick={() => onPickTask(task)}>{task}</button>
          ))}
        </div>

        <div className="iw-composer-card">
          <textarea
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Say what you need. Example: ask Operations iWorker to summarize today's delivery exception and prepare evidence for the center."
            rows={4}
          />
          <div className="iw-composer-footer">
            <div>
              <strong>{workMode === 'voice' ? 'Conversation first' : 'Task first'}</strong>
              <span>{workMode === 'voice' ? 'The iWorker can clarify intent before creating work.' : 'The iWorker will enter the structured task editor.'}</span>
            </div>
            <button type="button" onClick={handleSubmit}>Open task workspace</button>
          </div>
        </div>
      </section>

      <aside className="iw-body-panel">
        <div className="iw-panel-section">
          <h3>Operating role</h3>
          <div className="iw-status-list">
            {statusCards.map((item) => (
              <div key={item.label} className="iw-status-card">
                <span>{item.label}</span>
                <strong>{item.value}</strong>
                <p>{item.detail}</p>
              </div>
            ))}
          </div>
        </div>


        <div className="iw-panel-section">
          <div className="iw-section-title-row">
            <h3>GoalWatch pushes</h3>
            <button type="button" onClick={() => { void onRefreshGoalPushes(); }} disabled={goalPushLoading}>{goalPushLoading ? 'Syncing' : 'Refresh'}</button>
          </div>
          {goalPushError ? <p className="iw-panel-error">{goalPushError}</p> : null}
          <div className="iw-goal-list">
            {goalPushes.length === 0 ? (
              <div className="iw-goal-empty">No stalled task push. The watcher is quiet.</div>
            ) : goalPushes.slice(0, 4).map((push) => (
              <article key={push.eventId || push.taskId} className="iw-goal-card">
                <span>{push.reason || 'goal_push'} ? {Math.max(1, Math.round(push.ageSeconds / 60))}m</span>
                <strong>{push.title || push.taskId}</strong>
                <p>{push.status} ? {push.toRoleCode || push.toColleagueId || 'assigned iWorker'}</p>
                {push.eventId ? (
                  <div className="iw-goal-actions">
                    <button type="button" disabled={goalPushAckingId === push.eventId} onClick={() => { void onAckGoalPush(push.eventId || '', 'resumed'); }}>Resumed</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId} onClick={() => { void onAckGoalPush(push.eventId || '', 'blocked'); }}>Blocked</button>
                  </div>
                ) : null}
              </article>
            ))}
          </div>
        </div>

        <div className="iw-panel-section">
          <h3>Recent work</h3>
          <div className="iw-recent-list">
            {recentTasks.slice(0, 4).map((task) => (
              <button key={task.id} type="button" onClick={() => onOpenRecentTask(task)}>
                <strong>{task.title}</strong>
                <span>{task.owner} ? {task.status}</span>
              </button>
            ))}
          </div>
        </div>
      </aside>
    </div>
  );
}
