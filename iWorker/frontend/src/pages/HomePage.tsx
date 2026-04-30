import { useEffect, useMemo, useState } from 'react';
import { quickTasks as defaultQuickTasks } from '../mock/tasks';
import type { CenterAgentInstance, CenterGoalPush, GoalWatchAutoHandleStatus, HistoryTaskItem } from '../types';

type Props = {
  draft: string;
  selectedTask: string;
  selectedColleagueName: string;
  recentTasks: HistoryTaskItem[];
  agentInstances: CenterAgentInstance[];
  agentInstancesLoading: boolean;
  agentInstancesError: string;
  goalPushes: CenterGoalPush[];
  goalPushLoading: boolean;
  goalPushError: string;
  goalPushAckingId: string;
  goalWatchAutoStatus: GoalWatchAutoHandleStatus | null;
  onDraftChange: (value: string) => void;
  onPickTask: (task: string, colleagueName?: string) => void;
  onOpenNewTask: () => void;
  onOpenRecentTask: (task: HistoryTaskItem) => void;
  onRefreshAgentInstances: () => void | Promise<void>;
  onRefreshGoalPushes: () => void | Promise<void>;
  onAutoHandleGoalPush: (eventId: string) => void | Promise<void>;
  onAckGoalPush: (eventId: string, status: 'resumed' | 'blocked') => void | Promise<void>;
};

type WorkMode = 'chat' | 'task' | 'research';

const modeCopy: Record<WorkMode, { label: string; title: string; detail: string; placeholder: string }> = {
  chat: {
    label: 'Talk',
    title: 'Conversation first',
    detail: 'Use voice or IM style instructions. The iWorker can clarify intent before opening a structured task.',
    placeholder: 'Talk to your iWorker. Example: help me check today\'s delivery exceptions, ask Quality iWorker if needed, and prepare a short decision brief.',
  },
  task: {
    label: 'Task',
    title: 'Structured execution',
    detail: 'Convert the request into a task workspace with output type, colleague routing, attachments, and audit trail.',
    placeholder: 'Describe the task, expected output, context, deadline, and which digital worker should own it.',
  },
  research: {
    label: 'Deep work',
    title: 'Evidence and synthesis',
    detail: 'Ask iWorker to gather facts, consult skills, discuss with peer agents, and produce reusable memory.',
    placeholder: 'Ask for analysis, options, risk points, evidence, and a recommendation that can be saved as organization memory.',
  },
};

const skillChips = [
  'Voice handoff',
  'Operating brief',
  'Ask peer iWorkers',
  'Customer reply',
  'Policy memory check',
  'Meeting decisions',
  'Data cleanup',
  'Evidence pack',
];

const capabilityCards = [
  {
    label: 'Body',
    value: 'Local container',
    detail: 'This computer provides screen, files, browser, and tool access.',
  },
  {
    label: 'Memory',
    value: 'Center owned',
    detail: 'Company, department, and personal memory persist in iWorkerCenter.',
  },
  {
    label: 'People',
    value: 'Callable skill',
    detail: 'Human staff remain available as skills without becoming the control center.',
  },
];

const quickActions = [
  { title: 'Start from IM', detail: 'Turn a voice/chat instruction into work.' },
  { title: 'Continue a task', detail: 'Resume recent evidence and result.' },
  { title: 'Capture memory', detail: 'Save reusable experience to Center.' },
];

const formatRuntimeName = (instance: CenterAgentInstance) => {
  const role = instance.role || 'worker';
  return role.replace(/[-_]/g, ' ');
};

export function HomePage({ draft, selectedTask, selectedColleagueName, recentTasks, agentInstances, agentInstancesLoading, agentInstancesError, goalPushes, goalPushLoading, goalPushError, goalPushAckingId, goalWatchAutoStatus, onDraftChange, onPickTask, onOpenNewTask, onOpenRecentTask, onRefreshAgentInstances, onRefreshGoalPushes, onAutoHandleGoalPush, onAckGoalPush }: Props) {
  const [workMode, setWorkMode] = useState<WorkMode>('chat');
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
      .catch(() => undefined);
  }, []);

  const currentMode = modeCopy[workMode];
  const visibleChips = useMemo(() => {
    if (workMode === 'task') {
      return quickTasks;
    }
    if (workMode === 'research') {
      return ['Market scan', 'Root cause analysis', 'Decision options', 'Skill search', 'Risk review', 'Reusable playbook'];
    }
    return skillChips;
  }, [quickTasks, workMode]);

  const onlineAgents = agentInstances.filter((item) => item.effectiveStatus === 'online').length;
  const pendingPushes = goalPushes.filter((item) => item.status !== 'acked' && item.status !== 'resumed').length;

  const handleSubmit = () => {
    if (draft.trim() || selectedTask) {
      onOpenNewTask();
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div className="iw-workbuddy-shell">
      <section className="iw-workbuddy-stage" aria-label="iWorker workspace">
        <header className="iw-stage-topline">
          <div>
            <span className="iw-kicker">AI native employee</span>
            <h2>What should your iWorker handle next?</h2>
            <p>
              Work with the digital employee through voice, IM, or structured tasks. The local desktop is only the body and cache; durable capability is accumulated in iWorkerCenter.
            </p>
          </div>
          <div className="iw-stage-snapshot" aria-label="runtime snapshot">
            <span>{onlineAgents}/{Math.max(agentInstances.length, 1)} bodies online</span>
            <strong>{pendingPushes} watcher pushes</strong>
          </div>
        </header>

        <div className="iw-agent-hero-card">
          <div className="iw-agent-orb" aria-hidden="true">
            <span className="iw-agent-face">i</span>
            <span className="iw-orb-ring iw-orb-ring-one" />
            <span className="iw-orb-ring iw-orb-ring-two" />
          </div>
          <div className="iw-agent-activity-card">
            <span>Active partner</span>
            <strong>{selectedColleagueName || 'Auto-routing iWorker'}</strong>
            <p>{selectedTask || 'Listening for instruction, then routing to the right skill, peer worker, or human skill.'}</p>
          </div>
        </div>

        <div className="iw-mode-pill" role="tablist" aria-label="Work mode">
          {(Object.keys(modeCopy) as WorkMode[]).map((mode) => (
            <button key={mode} type="button" role="tab" aria-selected={workMode === mode} className={workMode === mode ? 'is-active' : ''} onClick={() => setWorkMode(mode)}>
              {modeCopy[mode].label}
            </button>
          ))}
        </div>

        <div className="iw-suggestion-strip" aria-label="Quick task suggestions">
          {visibleChips.map((task) => (
            <button key={task} type="button" onClick={() => onPickTask(task)}>{task}</button>
          ))}
        </div>

        <div className="iw-bottom-composer">
          <textarea
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={currentMode.placeholder}
            rows={3}
          />
          <div className="iw-composer-toolbar">
            <div className="iw-tool-group" aria-label="Composer tools">
              <button type="button" title="Mention a worker or human skill">@</button>
              <button type="button" title="Attach local evidence">Attach</button>
              <button type="button" title="Select skill">Skill</button>
              <button type="button" title="Use Center memory">Memory</button>
            </div>
            <div className="iw-mode-summary">
              <strong>{currentMode.title}</strong>
              <span>{currentMode.detail}</span>
            </div>
            <button type="button" className="iw-send-button" onClick={handleSubmit} aria-label="Open task workspace">Send</button>
          </div>
        </div>
      </section>

      <aside className="iw-workbuddy-inspector" aria-label="iWorker operating status">
        <section className="iw-inspector-card iw-inspector-card-dark">
          <div className="iw-inspector-title-row">
            <span>Center runtime</span>
            <button type="button" onClick={() => { void onRefreshAgentInstances(); }} disabled={agentInstancesLoading}>{agentInstancesLoading ? 'Pinging' : 'Heartbeat'}</button>
          </div>
          <strong>{onlineAgents} online agent instance{onlineAgents === 1 ? '' : 's'}</strong>
          <p>Multiple agent instances can share Center memory while this desktop stays a replaceable body.</p>
          {agentInstancesError ? <p className="iw-panel-error">{agentInstancesError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <h3>Practical operating model</h3>
          <div className="iw-capability-grid">
            {capabilityCards.map((item) => (
              <article key={item.label}>
                <span>{item.label}</span>
                <strong>{item.value}</strong>
                <p>{item.detail}</p>
              </article>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>Quick starts</h3>
            <span className="iw-soft-pill">usable now</span>
          </div>
          <div className="iw-quick-action-list">
            {quickActions.map((item) => (
              <button key={item.title} type="button" onClick={() => onPickTask(item.title)}>
                <strong>{item.title}</strong>
                <span>{item.detail}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>Goal watcher</h3>
            <span className={goalWatchAutoStatus?.running ? 'iw-watch-pill is-running' : 'iw-watch-pill'}>{goalWatchAutoStatus?.running ? 'Running' : 'Single-flight'}</span>
          </div>
          <div className="iw-watch-grid">
            <div><strong>{goalWatchAutoStatus?.runCount || 0}</strong><span>runs</span></div>
            <div><strong>{goalWatchAutoStatus?.skipCount || 0}</strong><span>skips</span></div>
            <div><strong>{goalWatchAutoStatus?.timeoutCancelCount || 0}</strong><span>timeouts</span></div>
            <div><strong>{goalWatchAutoStatus?.lastHandledCount || 0}</strong><span>handled</span></div>
          </div>
          <p className="iw-watch-note">Interval {goalWatchAutoStatus?.intervalSeconds || 30}s, max run {goalWatchAutoStatus?.maxDurationSeconds || 120}s.</p>
          {goalWatchAutoStatus?.lastError ? <p className="iw-panel-error">{goalWatchAutoStatus.lastError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>Push queue</h3>
            <button type="button" onClick={() => { void onRefreshGoalPushes(); }} disabled={goalPushLoading}>{goalPushLoading ? 'Syncing' : 'Refresh'}</button>
          </div>
          {goalPushError ? <p className="iw-panel-error">{goalPushError}</p> : null}
          <div className="iw-goal-list">
            {goalPushes.length === 0 ? (
              <div className="iw-goal-empty">No stalled task push. The watcher is quiet.</div>
            ) : goalPushes.slice(0, 3).map((push) => (
              <article key={push.eventId || push.taskId} className="iw-goal-card">
                <span>{push.reason || 'goal_push'} · {Math.max(1, Math.round(push.ageSeconds / 60))}m</span>
                {push.recommendedAction ? <span className="iw-goal-action-pill">{push.recommendedAction}</span> : null}
                <strong>{push.title || push.taskId}</strong>
                <p>{push.status} · {push.toRoleCode || push.toColleagueId || 'assigned iWorker'}</p>
                {push.eventId ? (
                  <div className="iw-goal-actions">
                    <button type="button" disabled={goalPushAckingId === push.eventId} onClick={() => { void onAutoHandleGoalPush(push.eventId || ''); }}>Auto</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId} onClick={() => { void onAckGoalPush(push.eventId || '', 'resumed'); }}>Resumed</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId} onClick={() => { void onAckGoalPush(push.eventId || '', 'blocked'); }}>Blocked</button>
                  </div>
                ) : null}
              </article>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <h3>Recent work</h3>
          <div className="iw-recent-list">
            {recentTasks.slice(0, 4).map((task) => (
              <button key={task.id} type="button" onClick={() => onOpenRecentTask(task)}>
                <strong>{task.title}</strong>
                <span>{task.owner} · {task.status}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <h3>Agent instances</h3>
          <div className="iw-agent-list">
            {agentInstances.length === 0 ? (
              <div className="iw-goal-empty">Center has not seen this iWorker body yet.</div>
            ) : agentInstances.map((instance) => (
              <article key={instance.instanceId} className="iw-agent-card">
                <div>
                  <strong>{formatRuntimeName(instance)}</strong>
                  <span className={instance.effectiveStatus === 'online' ? 'is-online' : instance.effectiveStatus === 'offline' ? 'is-offline' : ''}>{instance.effectiveStatus || instance.status}</span>
                </div>
                <p>{instance.memoryAuthority || 'iWorkerCenter'} · {instance.localCacheMode || 'cache_only'} · {instance.heartbeatAgeSeconds || 0}s ago</p>
                <small>{instance.hostId || 'local body'} · {instance.capabilities.slice(0, 3).join(', ') || 'base tools'}</small>
              </article>
            ))}
          </div>
        </section>
      </aside>
    </div>
  );
}
