import { useEffect, useMemo, useState } from 'react';
import { applyBootstrapPlan, draftBootstrapPlan, fetchBootstrapStatus, startFirstWave, type AppliedAsset, type BootstrapRun, type FirstWaveTask, type ValidationIssue } from '../api/bootstrap';

type BootstrapPhaseId = 'profile' | 'organization' | 'workers' | 'memory' | 'goals' | 'watcher';

type BootstrapPhase = {
  id: BootstrapPhaseId;
  title: string;
  owner: string;
  description: string;
  outputs: string[];
};

const phases: BootstrapPhase[] = [
  { id: 'profile', title: 'Company profile intake', owner: 'Admin + executive sponsor', description: 'Collect business model, products, customers, systems, compliance boundaries, and operating priorities.', outputs: ['Company context', 'Data boundary', 'Decision thresholds'] },
  { id: 'organization', title: 'Virtual organization map', owner: 'iWorkerCenter', description: 'Create virtual departments and business domains. These are execution structures, not human middle management.', outputs: ['Virtual departments', 'Role codes', 'Escalation rules'] },
  { id: 'workers', title: 'Initial iWorker crew', owner: 'iWorkerCenter', description: 'Generate the first digital employees from domain templates and bind them to Center memory and skills.', outputs: ['Office iWorker', 'Ops iWorker', 'Data iWorker', 'Quality iWorker'] },
  { id: 'memory', title: 'Memory seeding', owner: 'Bootstrap tools', description: 'Import policies, SOPs, historical decisions, customer rules, and reusable experience into company and department memory.', outputs: ['Company memory', 'Department memory', 'Source evidence'] },
  { id: 'goals', title: 'Goal and task discovery', owner: 'Goal planner', description: 'Turn business priorities into a target tree, recurring work, exception monitors, and first-wave execution tasks.', outputs: ['Goal tree', 'Recurring tasks', 'First task batch'] },
  { id: 'watcher', title: 'Autonomous run loop', owner: 'Goal watcher cluster', description: 'Start scheduled checks and push stalled or new work to the right iWorker while keeping single-flight protection.', outputs: ['Watcher policy', 'Push queue', 'Executive escalation lane'] },
];

const defaultDepartments = ['Sales', 'Operations', 'Customer Success', 'Finance', 'Quality', 'Office', 'Data'];
const defaultWorkers = ['Office iWorker', 'Ops iWorker', 'Data iWorker', 'Quality iWorker'];
const defaultRecurringTasks = ['Daily operating brief', 'Customer exception scan', 'Weekly decision summary', 'Policy memory review'];

export function EnterpriseBootstrapPage() {
  const [companyName, setCompanyName] = useState('');
  const [businessSummary, setBusinessSummary] = useState('');
  const [priority, setPriority] = useState('Stabilize daily operations and customer delivery.');
  const [completed, setCompleted] = useState<Record<BootstrapPhaseId, boolean>>({ profile: false, organization: false, workers: false, memory: false, goals: false, watcher: false });
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [firstWave, setFirstWave] = useState<FirstWaveTask[]>([]);
  const [lastRun, setLastRun] = useState<BootstrapRun | null>(null);
  const [appliedAssets, setAppliedAssets] = useState<AppliedAsset[]>([]);
  const [apiMessage, setApiMessage] = useState('');
  const [apiError, setApiError] = useState('');
  const [apiBusy, setApiBusy] = useState(false);

  const completedCount = useMemo(() => phases.filter((phase) => completed[phase.id]).length, [completed]);
  const requiredPhasesConfirmed = completed.profile && completed.organization && completed.workers && completed.goals;
  const readyToStart = requiredPhasesConfirmed && !issues.some((issue) => issue.level === 'error');

  const bootstrapSpec = useMemo(() => ({
    company_name: companyName || 'New customer company',
    business_summary: businessSummary || 'To be collected during executive intake.',
    priority,
    virtual_departments: defaultDepartments,
    initial_iworkers: defaultWorkers,
    recurring_tasks: defaultRecurringTasks,
    memory_scopes: ['company', 'department', 'personal'],
    requires_executive_confirmation: ['business priorities', 'risk boundaries', 'external communication rules'],
    watcher_policy: { enabled: true, single_flight: true, max_run_seconds: 120, scale_by_worker_count: true },
  }), [businessSummary, companyName, priority]);

  useEffect(() => {
    let cancelled = false;
    fetchBootstrapStatus()
      .then((status) => {
        if (cancelled) return;
        if (status.plan) {
          setCompanyName(status.plan.company_name || '');
          setBusinessSummary(status.plan.business_summary || '');
          setPriority(status.plan.priority || '');
        }
        setIssues(status.validation_issues || []);
        setFirstWave(status.suggested_first_wave || []);
        setLastRun(status.last_run || null);
        setAppliedAssets(status.applied_assets || []);
        if (status.has_plan) {
          setApiMessage(status.ready_to_start ? 'Existing bootstrap plan is ready.' : 'Existing bootstrap plan needs review.');
        }
      })
      .catch((error: Error) => setApiError(error.message));
    return () => { cancelled = true; };
  }, []);

  const togglePhase = (phaseId: BootstrapPhaseId) => setCompleted((current) => ({ ...current, [phaseId]: !current[phaseId] }));

  const saveDraft = async () => {
    setApiBusy(true);
    setApiError('');
    try {
      const result = await draftBootstrapPlan(bootstrapSpec);
      setIssues(result.validation_issues || []);
      setFirstWave(result.suggested_first_wave || []);
      setApiMessage('Bootstrap draft saved to iWorkerCenter.');
    } catch (error) {
      setApiError(error instanceof Error ? error.message : 'Save draft failed');
    } finally {
      setApiBusy(false);
    }
  };

  const applyPlan = async () => {
    setApiBusy(true);
    setApiError('');
    try {
      const result = await applyBootstrapPlan(bootstrapSpec);
      setIssues(result.validation_issues || []);
      setFirstWave(result.suggested_first_wave || []);
      setAppliedAssets(result.applied_assets || []);
      setApiMessage('Bootstrap plan applied. Roles and iWorkers are provisioned; first task wave can now be started.');
    } catch (error) {
      setApiError(error instanceof Error ? error.message : 'Apply plan failed');
    } finally {
      setApiBusy(false);
    }
  };

  const launchFirstWave = async () => {
    setApiBusy(true);
    setApiError('');
    try {
      const result = await startFirstWave();
      setLastRun(result.run);
      setFirstWave(result.run.tasks || []);
      setAppliedAssets(result.run.applied_assets || []);
      setApiMessage(`First task wave started: ${result.run.id}`);
    } catch (error) {
      setApiError(error instanceof Error ? error.message : 'Start first wave failed');
    } finally {
      setApiBusy(false);
    }
  };

  const firstWaveTasks = lastRun?.tasks || firstWave;

  return (
    <div className="bootstrap-page">
      <section className="bootstrap-hero card">
        <div>
          <span className="eyebrow">Enterprise Bootstrap</span>
          <h2>Start a newly purchased iWorker organization</h2>
          <p>This is the missing launch console: it turns a blank iWorkerCenter into a runnable AI-native organization with virtual departments, first iWorkers, seeded memory, goals, recurring tasks, and watcher-driven execution.</p>
        </div>
        <div className="bootstrap-readiness">
          <strong>{completedCount}/{phases.length}</strong>
          <span>bootstrap phases confirmed</span>
          <button type="button" className={readyToStart ? 'primary' : 'secondary'} disabled={!readyToStart || apiBusy} onClick={launchFirstWave}>{readyToStart ? 'Start first task wave' : 'Confirm required phases'}</button>
        </div>
      </section>

      <section className="bootstrap-grid">
        <div className="card bootstrap-card">
          <h3>1. Intake interface</h3>
          <label>Company name<input value={companyName} onChange={(event) => setCompanyName(event.target.value)} placeholder="Acme Manufacturing" /></label>
          <label>Business summary<textarea value={businessSummary} onChange={(event) => setBusinessSummary(event.target.value)} placeholder="What does this company sell, deliver, support, and measure?" rows={4} /></label>
          <label>First operating priority<textarea value={priority} onChange={(event) => setPriority(event.target.value)} rows={3} /></label>
        </div>

        <div className="card bootstrap-card">
          <h3>Generated bootstrap spec</h3>
          <pre>{JSON.stringify(bootstrapSpec, null, 2)}</pre>
          <div className="bootstrap-api-actions">
            <button type="button" className="secondary" disabled={apiBusy} onClick={saveDraft}>Save draft</button>
            <button type="button" className="primary" disabled={apiBusy} onClick={applyPlan}>Apply plan</button>
          </div>
          {apiMessage ? <p className="bootstrap-api-message">{apiMessage}</p> : null}
          {apiError ? <p className="bootstrap-api-error">{apiError}</p> : null}
          {issues.length > 0 ? <div className="bootstrap-issue-list">{issues.map((issue) => <span key={`${issue.field}-${issue.message}`} className={issue.level === 'error' ? 'is-error' : ''}>{issue.field}: {issue.message}</span>)}</div> : null}
        </div>
      </section>

      <section className="bootstrap-phases">
        {phases.map((phase, index) => (
          <article key={phase.id} className={completed[phase.id] ? 'card bootstrap-phase is-complete' : 'card bootstrap-phase'}>
            <div className="bootstrap-phase-index">{index + 1}</div>
            <div>
              <span>{phase.owner}</span>
              <h3>{phase.title}</h3>
              <p>{phase.description}</p>
              <div className="bootstrap-output-list">{phase.outputs.map((output) => <small key={output}>{output}</small>)}</div>
            </div>
            <button type="button" onClick={() => togglePhase(phase.id)}>{completed[phase.id] ? 'Confirmed' : 'Confirm'}</button>
          </article>
        ))}
      </section>

      {appliedAssets.length > 0 ? (
        <section className="card bootstrap-assets">
          <h3>Provisioned organization assets</h3>
          <div>
            {appliedAssets.map((asset) => (
              <span key={`${asset.kind}-${asset.id || asset.name}`}>{asset.kind}: {asset.name} · {asset.status}</span>
            ))}
          </div>
        </section>
      ) : null}
      <section className="card bootstrap-first-wave">
        <div>
          <h3>First-wave task preview</h3>
          <p>These tasks are generated by iWorkerCenter before the organization enters autonomous operation.</p>
        </div>
        <div className="bootstrap-first-wave-grid">
          {firstWaveTasks.map((task) => (
            <article key={task.id}>
              <span>{task.owner_iworker} / {task.memory_scope}</span>
              <strong>{task.title}</strong>
              <p>{task.expected_output} · {task.recommended_trigger}</p>
              <small>{task.escalation_threshold}</small>
            </article>
          ))}
        </div>
      </section>

      <section className="card bootstrap-interfaces">
        <h3>Interfaces and tools this implies</h3>
        <div>
          <article><strong>Admin UI</strong><p>Wizard for company profile, virtual organization, iWorker crew, memory import, goal tree, recurring tasks, and watcher policy.</p></article>
          <article><strong>Backend APIs</strong><p>Draft bootstrap plan, validate plan, apply plan, generate first task wave, and query bootstrap run status.</p></article>
          <article><strong>Import tools</strong><p>Document/SOP/customer-rule importers that classify durable memory into company, department, and personal scopes.</p></article>
          <article><strong>Goal tools</strong><p>Goal discovery, recurring task templates, exception monitors, and GoalWatcher cluster launch controls.</p></article>
        </div>
      </section>
    </div>
  );
}
