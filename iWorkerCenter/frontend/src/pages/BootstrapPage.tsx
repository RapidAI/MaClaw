import { useEffect, useMemo, useState } from 'react';
import {
  applyBootstrapPlan,
  defaultBootstrapPlan,
  draftBootstrapPlan,
  fetchBootstrapStatus,
  isBootstrapComplete,
  startBootstrapFirstWave,
  validateBootstrapPlan,
  type AppliedAsset,
  type BootstrapPlan,
  type BootstrapRun,
  type BootstrapStatus,
  type FirstWaveTask,
  type ValidationIssue,
} from '../api/bootstrap';
import { useI18n } from '../i18n';

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;
type WizardStep = 0 | 1 | 2 | 3;

type Props = {
  wizardOpen?: boolean;
  onWizardClose?: () => void;
  onBootstrapChanged?: (status: BootstrapStatus | null) => void;
};

const splitLines = (value: string) => value.split('\n').map(item => item.trim()).filter(Boolean);
const joinLines = (value: string[] | undefined) => (value || []).join('\n');

function TextAreaList({ label, value, onChange, hint, rows = 5 }: { label: string; value: string[]; onChange: (next: string[]) => void; hint?: string; rows?: number }) {
  return (
    <label className="cloud-field">
      <span>{label}</span>
      <textarea value={joinLines(value)} onChange={event => onChange(splitLines(event.target.value))} rows={rows} />
      {hint ? <small className="cloud-inline-note">{hint}</small> : null}
    </label>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}

function FirstWaveList({ tasks }: { tasks: FirstWaveTask[] }) {
  const { t } = useI18n();
  if (!tasks.length) return <p className="cloud-inline-note">{t('保存或校验启动计划后，会生成建议的首批任务。', 'Suggested first-wave tasks appear after saving or validating the bootstrap plan.')}</p>;
  return (
    <div className="item-list">
      {tasks.map(task => (
        <div className="item-row" key={task.id || task.title}>
          <strong>{task.title}</strong>
          <p>{t('负责人：', 'Owner: ')}{task.owner_iworker || '-'} / {t('触发：', 'Trigger: ')}{task.recommended_trigger || '-'} / {t('记忆范围：', 'Memory: ')}{task.memory_scope || '-'}</p>
          <p>{t('产出：', 'Output: ')}{task.expected_output || '-'} / {t('升级边界：', 'Escalation: ')}{task.escalation_threshold || '-'}</p>
          <span className={task.requires_peer_review ? 'badge warn' : 'badge info'}>{task.requires_peer_review ? t('需要同伴复核', 'Peer review required') : t('进入人工可见队列', 'Visible to human queue')}</span>
        </div>
      ))}
    </div>
  );
}

function AssetList({ assets }: { assets: AppliedAsset[] }) {
  const { t } = useI18n();
  const assetKindText = (kind: string) => ({
    role: t('角色', 'Role'),
    iworker: 'iWorker',
    memory: t('记忆', 'Memory'),
    workflow: t('流程模板', 'Workflow template'),
    workflow_instance: t('流程实例', 'Workflow run'),
    goalwatch_policy: t('自动运行策略', 'Automation policy'),
  }[kind] || kind);
  if (!assets.length) return <p className="cloud-inline-note">{t('应用启动计划后，会显示创建或复用的角色、iWorker、流程、记忆和策略。', 'Applied roles, iWorkers, workflows, memories, and policies appear after the plan is applied.')}</p>;
  return (
    <div className="item-list">
      {assets.map((asset, index) => (
        <div className="item-row" key={asset.kind + '-' + (asset.id || asset.name) + '-' + index}>
          <strong>{asset.name || asset.id || asset.kind}</strong>
          <p>{assetKindText(asset.kind)} / {asset.status || 'ready'}{asset.id ? ' / ' + asset.id : ''}</p>
        </div>
      ))}
    </div>
  );
}

function IssueList({ issues }: { issues: ValidationIssue[] }) {
  const { t } = useI18n();
  const fieldLabel = (field: string) => ({
    company_name: t('单位名称', 'Company name'),
    legal_person: t('法人姓名', 'Legal representative'),
    company_address: t('公司地址', 'Company address'),
    contact_email: t('联系邮箱', 'Contact email'),
    business_summary: t('业务摘要', 'Business summary'),
    virtual_departments: t('虚拟部门', 'Virtual departments'),
    initial_iworkers: t('首批 iWorker', 'Initial iWorkers'),
    memory_scopes: t('记忆范围', 'Memory scopes'),
    recurring_tasks: t('循环任务', 'Recurring tasks'),
    requires_executive_confirmation: t('负责人确认边界', 'Executive confirmation boundaries'),
    watcher_policy: t('自动运行策略', 'Automation policy'),
  }[field] || field);
  if (!issues.length) return null;
  return (
    <div className="bootstrap-issues">
      {issues.map((issue, index) => (
        <p key={issue.field + '-' + index} className={'cloud-message ' + (issue.level === 'error' ? 'danger' : 'warn')}>
          {fieldLabel(issue.field) + ': ' + issue.message}
        </p>
      ))}
    </div>
  );
}

function RunSummary({ run }: { run?: BootstrapRun }) {
  const { t } = useI18n();
  if (!run) return null;
  return (
    <section className="card section-card">
      <div className="section-head"><div><h3>{t('最近一次首批运行', 'Latest First-Wave Run')}</h3><p>{run.id} / {run.status} / {run.updated_at ? new Date(run.updated_at).toLocaleString() : '-'}</p></div></div>
      <FirstWaveList tasks={run.tasks || []} />
    </section>
  );
}

export function BootstrapPage({ wizardOpen = false, onWizardClose, onBootstrapChanged }: Props) {
  const { t } = useI18n();
  const [status, setStatus] = useState<BootstrapStatus | null>(null);
  const [plan, setPlan] = useState<BootstrapPlan>(defaultBootstrapPlan());
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [firstWave, setFirstWave] = useState<FirstWaveTask[]>([]);
  const [assets, setAssets] = useState<AppliedAsset[]>([]);
  const [message, setMessage] = useState<Message>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [localWizardOpen, setLocalWizardOpen] = useState(false);
  const [step, setStep] = useState<WizardStep>(0);

  const effectiveWizardOpen = wizardOpen || localWizardOpen;
  const blockingIssues = useMemo(() => issues.filter(issue => issue.level === 'error'), [issues]);
  const contactEmailValid = !plan.contact_email.trim() || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(plan.contact_email.trim());
  const companyInfoReady = Boolean(plan.company_name.trim() && plan.legal_person.trim() && plan.company_address.trim() && plan.contact_email.trim() && contactEmailValid);
  const planReady = blockingIssues.length === 0 && companyInfoReady && plan.initial_iworkers.length >= 2 && plan.virtual_departments.length >= 3;
  const bootstrapDone = isBootstrapComplete(status);

  const stepCopy: Array<{ title: string; desc: string }> = [
    { title: t('单位信息', 'Company'), desc: t('确认单位名称、法人姓名、公司地址、公司联系邮箱和上线边界。', 'Confirm company identity, legal representative, address, email, and launch boundary.') },
    { title: t('组织骨架', 'Organization'), desc: t('配置虚拟部门、首批数字同事和可复用记忆范围。', 'Configure departments, first digital colleagues, and reusable memory scopes.') },
    { title: t('运行边界', 'Operating Boundary'), desc: t('设置循环任务、人工确认边界和自动运行策略。', 'Set recurring tasks, human checkpoints, and automation policy.') },
    { title: t('应用启动', 'Apply'), desc: t('校验计划，写入本地资源，并启动首批任务。', 'Validate, write local assets, and start first-wave work.') },
  ];

  const publishStatus = (next: BootstrapStatus | null) => {
    setStatus(next);
    onBootstrapChanged?.(next);
  };

  const syncFromStatus = (next: BootstrapStatus) => {
    publishStatus(next);
    if (next.plan) setPlan(next.plan);
    setIssues(next.validation_issues || []);
    setFirstWave(next.suggested_first_wave || []);
    setAssets(next.applied_assets || []);
  };

  const loadStatus = async () => {
    setBusy('load');
    try {
      const next = await fetchBootstrapStatus();
      syncFromStatus(next);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('读取初始化状态失败。', 'Failed to load bootstrap status.') });
      publishStatus(null);
    } finally {
      setBusy(null);
    }
  };

  useEffect(() => { void loadStatus(); }, []);

  const updatePlan = (patch: Partial<BootstrapPlan>) => {
    setMessage(null);
    setPlan(current => ({ ...current, ...patch }));
  };

  const saveDraft = async () => {
    setBusy('draft');
    setMessage(null);
    try {
      const res = await draftBootstrapPlan(plan);
      setPlan(res.plan);
      setIssues(res.validation_issues || []);
      setFirstWave(res.suggested_first_wave || []);
      setMessage({ kind: 'ok', text: t('启动计划已保存，可以继续校验或应用。', 'Bootstrap plan saved. You can validate or apply it next.') });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('保存启动计划失败。', 'Failed to save bootstrap plan.') });
      return false;
    } finally {
      setBusy(null);
    }
  };

  const validatePlan = async () => {
    setBusy('validate');
    setMessage(null);
    try {
      const res = await validateBootstrapPlan(plan);
      setPlan(res.plan);
      setIssues(res.validation_issues || []);
      setMessage({ kind: res.ready_to_start ? 'ok' : 'warn', text: res.ready_to_start ? t('启动计划已通过校验。', 'Bootstrap plan passed validation.') : t('启动计划仍有阻塞项，请先修正。', 'The plan still has blockers. Fix them before applying.') });
      return res.ready_to_start;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('校验启动计划失败。', 'Failed to validate bootstrap plan.') });
      return false;
    } finally {
      setBusy(null);
    }
  };

  const applyPlan = async () => {
    setBusy('apply');
    setMessage(null);
    try {
      const res = await applyBootstrapPlan(plan);
      setPlan(res.plan);
      setIssues(res.validation_issues || []);
      setFirstWave(res.suggested_first_wave || []);
      setAssets(res.applied_assets || []);
      setMessage({ kind: 'ok', text: t('启动计划已应用：角色、首批 iWorker、流程模板、记忆和策略已写入 Center。', 'Bootstrap plan applied: roles, first iWorkers, workflow templates, memories, and policies were written to Center.') });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('应用启动计划失败。', 'Failed to apply bootstrap plan.') });
      return false;
    } finally {
      setBusy(null);
    }
  };

  const startFirstWave = async () => {
    setBusy('start');
    setMessage(null);
    try {
      const res = await startBootstrapFirstWave();
      setMessage({ kind: 'ok', text: t('首批任务已启动：', 'First-wave tasks started: ') + res.run.id });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('启动首批任务失败，请确认已经应用启动计划。', 'Failed to start first-wave tasks. Confirm the bootstrap plan has been applied.') });
      return false;
    } finally {
      setBusy(null);
    }
  };

  const closeWizard = () => {
    setLocalWizardOpen(false);
    onWizardClose?.();
  };

  const nextWizardStep = async () => {
    if (step === 0) {
      if (!companyInfoReady) {
        setMessage({ kind: 'warn', text: contactEmailValid ? t('请先填写单位名称、法人姓名、公司地址和联系邮箱。', 'Fill company name, legal representative, company address, and contact email first.') : t('请填写有效的公司联系邮箱。', 'Enter a valid company contact email.') });
        return;
      }
      const saved = await saveDraft();
      if (saved) setStep(1);
      return;
    }
    if (step === 1) {
      if (plan.initial_iworkers.length < 2) {
        setMessage({ kind: 'warn', text: t('请至少配置 2 个首批 iWorker。', 'Configure at least 2 initial iWorkers.') });
        return;
      }
      const saved = await saveDraft();
      if (saved) setStep(2);
      return;
    }
    if (step === 2) {
      const saved = await saveDraft();
      if (!saved) return;
      const ok = await validatePlan();
      if (ok) setStep(3);
    }
  };

  const renderPlanFields = (mode: 'full' | 'wizard') => (
    <div className="cloud-form-grid">
      <label className="cloud-field">
        <span>{t('单位/企业名称', 'Company name')}</span>
        <input value={plan.company_name} onChange={event => updatePlan({ company_name: event.target.value })} placeholder={t('例如：XX 科技有限公司', 'Example: Acme Technology Ltd.')} />
      </label>
      <label className="cloud-field">
        <span>{t('法人姓名', 'Legal representative')}</span>
        <input value={plan.legal_person} onChange={event => updatePlan({ legal_person: event.target.value })} placeholder={t('法人代表姓名', 'Legal representative name')} />
      </label>
      <label className="cloud-field">
        <span>{t('公司联系邮箱', 'Company contact email')}</span>
        <input type="email" value={plan.contact_email} onChange={event => updatePlan({ contact_email: event.target.value })} placeholder="admin@example.com" />
      </label>
      <label className="cloud-field">
        <span>{t('公司地址', 'Company address')}</span>
        <input value={plan.company_address} onChange={event => updatePlan({ company_address: event.target.value })} placeholder={t('注册地址或办公地址', 'Registered or office address')} />
      </label>
      <label className="cloud-field cloud-field-wide">
        <span>{t('当前优先级', 'Current priority')}</span>
        <input value={plan.priority} onChange={event => updatePlan({ priority: event.target.value })} />
      </label>
      <label className="cloud-field cloud-field-wide">
        <span>{t('业务摘要', 'Business summary')}</span>
        <textarea value={plan.business_summary} onChange={event => updatePlan({ business_summary: event.target.value })} rows={mode === 'wizard' ? 3 : 4} placeholder={t('说明主营业务、当前目标和上线边界', 'Describe main business, current goals, and launch boundaries')} />
      </label>
      {(mode === 'full' || step === 1) ? <TextAreaList label={t('虚拟部门', 'Virtual departments')} value={plan.virtual_departments} onChange={value => updatePlan({ virtual_departments: value })} hint={t('每行一个部门，建议至少 3 个。', 'One department per line, at least 3 recommended.')} /> : null}
      {(mode === 'full' || step === 1) ? <TextAreaList label={t('首批 iWorker', 'Initial iWorkers')} value={plan.initial_iworkers} onChange={value => updatePlan({ initial_iworkers: value })} hint={t('每行一个数字同事，建议至少 2 个。', 'One digital colleague per line, at least 2 recommended.')} /> : null}
      {(mode === 'full' || step === 1) ? <TextAreaList label={t('记忆范围', 'Memory scopes')} value={plan.memory_scopes} onChange={value => updatePlan({ memory_scopes: value })} hint={t('建议保留 company / department / personal。', 'Recommended: company / department / personal.')} /> : null}
      {(mode === 'full' || step === 2) ? <TextAreaList label={t('循环任务', 'Recurring tasks')} value={plan.recurring_tasks} onChange={value => updatePlan({ recurring_tasks: value })} /> : null}
      {(mode === 'full' || step === 2) ? <TextAreaList label={t('需要负责人确认的边界', 'Executive confirmation boundaries')} value={plan.requires_executive_confirmation} onChange={value => updatePlan({ requires_executive_confirmation: value })} /> : null}
    </div>
  );

  const renderPolicy = () => (
    <div className="bootstrap-policy-grid">
      <label><input type="checkbox" checked={plan.watcher_policy.enabled} onChange={event => updatePlan({ watcher_policy: { ...plan.watcher_policy, enabled: event.target.checked } })} /> {t('启用自动运行观察器', 'Enable automation watcher')}</label>
      <label><input type="checkbox" checked={plan.watcher_policy.single_flight} onChange={event => updatePlan({ watcher_policy: { ...plan.watcher_policy, single_flight: event.target.checked } })} /> {t('单任务防重入', 'Single-flight protection')}</label>
      <label><input type="checkbox" checked={plan.watcher_policy.scale_by_worker_count} onChange={event => updatePlan({ watcher_policy: { ...plan.watcher_policy, scale_by_worker_count: event.target.checked } })} /> {t('按 iWorker 数量扩展', 'Scale by iWorker count')}</label>
      <label className="cloud-field"><span>{t('单次最长运行秒数', 'Max run seconds')}</span><input type="number" min={30} value={plan.watcher_policy.max_run_seconds} onChange={event => updatePlan({ watcher_policy: { ...plan.watcher_policy, max_run_seconds: Number(event.target.value) || 120 } })} /></label>
    </div>
  );

  const wizardBody = () => {
    if (step === 0) return renderPlanFields('wizard');
    if (step === 1) return renderPlanFields('wizard');
    if (step === 2) return <>{renderPlanFields('wizard')}{renderPolicy()}</>;
    return (
      <div className="bootstrap-wizard-review">
        <div className="cloud-status-grid">
          <StatusTile label={t('当前租户', 'Tenant')} value={status?.tenant_id || plan.tenant_id || '-'} tone={status?.tenant_id ? 'ok' : 'warn'} />
          <StatusTile label={t('联系邮箱', 'Email')} value={plan.contact_email || '-'} tone={contactEmailValid && plan.contact_email ? 'ok' : 'warn'} />
          <StatusTile label={t('首批 iWorker', 'Initial iWorkers')} value={String(plan.initial_iworkers.length)} tone={plan.initial_iworkers.length >= 2 ? 'ok' : 'warn'} />
          <StatusTile label={t('阻塞项', 'Blockers')} value={String(blockingIssues.length)} tone={blockingIssues.length ? 'warn' : 'ok'} />
        </div>
        <IssueList issues={issues} />
        <div className="panel-grid bootstrap-wizard-panels">
          <section className="section-card"><div className="section-head"><div><h3>{t('建议首批任务', 'Suggested First-Wave Tasks')}</h3><p>{t('应用计划后可以启动这些任务。', 'These tasks can be started after applying the plan.')}</p></div></div><FirstWaveList tasks={firstWave} /></section>
          <section className="section-card"><div className="section-head"><div><h3>{t('已应用资源', 'Applied Assets')}</h3><p>{t('应用计划后写入 Center 本地。', 'Written locally to Center after applying the plan.')}</p></div></div><AssetList assets={assets} /></section>
        </div>
      </div>
    );
  };

  return (
    <div className="center-page-stack">
      <section className="card section-card">
        <div className="section-head">
          <div>
            <h3>{t('Bootstrap 流程', 'Bootstrap Flow')}</h3>
            <p>{t('每个租户独立完成 Center 本地初始化。Cloud 只处理注册、授权和算力协调；Cloud 离线时，本地 iWorker、流程、记忆和人工协作仍可继续工作。', 'Each tenant is bootstrapped locally in Center. Cloud only handles registration, licensing, and compute coordination; local iWorkers, workflows, memories, and human collaboration continue when Cloud is offline.')}</p>
          </div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={loadStatus} disabled={busy !== null}>{busy === 'load' ? t('刷新中...', 'Refreshing...') : t('刷新状态', 'Refresh status')}</button>
            <button className="cloud-primary" type="button" onClick={() => { setStep(0); setLocalWizardOpen(true); }}>{t('打开向导', 'Open wizard')}</button>
            <span className={bootstrapDone ? 'badge ok' : 'badge warn'}>{bootstrapDone ? t('当前租户已完成 bootstrap', 'Tenant bootstrapped') : status?.has_plan ? t('当前租户计划未完成', 'Plan not complete') : t('当前租户需要 bootstrap', 'Bootstrap needed')}</span>
          </div>
        </div>
        <div className="cloud-step-list bootstrap-step-list">
          <div className="cloud-step is-done"><span>1</span><strong>{t('创建本地租户', 'Create Local Tenant')}</strong><p>{t('管理员账号和企业基础资料保存在 Center 本地。', 'Admin account and company profile are stored locally in Center.')}</p></div>
          <div className={status?.has_plan || plan.company_name ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>2</span><strong>{t('保存启动计划', 'Save Bootstrap Plan')}</strong><p>{t('确认部门、首批 iWorker、记忆范围和人工确认边界。', 'Confirm departments, first iWorkers, memory scopes, and human checkpoints.')}</p></div>
          <div className={assets.length ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>3</span><strong>{t('应用组织骨架', 'Apply Organization')}</strong><p>{t('创建角色、数字员工、流程模板、记忆和自动运行策略。', 'Create roles, digital colleagues, workflow templates, memories, and automation policy.')}</p></div>
          <div className={status?.last_run ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>4</span><strong>{t('启动首批任务', 'Start First Wave')}</strong><p>{t('让 iWorker 在可审计边界内开始协作，并向人类员工显式提示。', 'Let iWorkers collaborate inside auditable boundaries and surface human prompts clearly.')}</p></div>
        </div>
      </section>

      <section className="card section-card">
        <div className="section-head">
          <div><h3>{t('启动计划', 'Bootstrap Plan')}</h3><p>{t('这里定义当前租户的本地业务启动方式，不会把业务数据交给 Cloud。', 'This defines how the current tenant starts locally; business data is not sent to Cloud.')}</p></div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={validatePlan} disabled={busy !== null}>{busy === 'validate' ? t('校验中...', 'Validating...') : t('校验计划', 'Validate plan')}</button>
            <button className="cloud-primary" type="button" onClick={saveDraft} disabled={busy !== null}>{busy === 'draft' ? t('保存中...', 'Saving...') : t('保存草稿', 'Save draft')}</button>
          </div>
        </div>
        {renderPlanFields('full')}
        {renderPolicy()}
        <IssueList issues={issues} />
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </section>

      <div className="panel-grid">
        <section className="section-card">
          <div className="section-head"><div><h3>{t('建议首批任务', 'Suggested First-Wave Tasks')}</h3><p>{t('这些任务会进入 Center 的流程实例。iWorker 执行时仍需要把人工干预点显式推给人类员工。', 'These tasks become Center workflow runs. iWorkers still surface human intervention points explicitly.')}</p></div><button className="cloud-primary" type="button" onClick={startFirstWave} disabled={busy !== null || !planReady}>{busy === 'start' ? t('启动中...', 'Starting...') : t('启动首批任务', 'Start first wave')}</button></div>
          <FirstWaveList tasks={firstWave} />
        </section>
        <section className="section-card">
          <div className="section-head"><div><h3>{t('已应用资源', 'Applied Assets')}</h3><p>{t('应用计划会把组织骨架写入当前租户的 Center 本地空间，不依赖 Cloud 在线。', 'Applying the plan writes the organization shape into the current tenant space in Center and does not require Cloud online.')}</p></div><button className="btn-secondary" type="button" onClick={applyPlan} disabled={busy !== null || !planReady}>{busy === 'apply' ? t('应用中...', 'Applying...') : t('应用启动计划', 'Apply bootstrap plan')}</button></div>
          <AssetList assets={assets} />
        </section>
      </div>

      <RunSummary run={status?.last_run} />

      {effectiveWizardOpen ? (
        <div className="bootstrap-wizard-overlay" role="dialog" aria-modal="true" aria-label={t('Bootstrap 向导', 'Bootstrap wizard')}>
          <section className="bootstrap-wizard-modal">
            <header className="bootstrap-wizard-head">
              <div><span className="mini">BOOTSTRAP WIZARD</span><h3>{t('单位初始化向导', 'Tenant Bootstrap Wizard')}</h3><p>{t('当前租户尚未完成正常 bootstrap。按步骤完成 Center 本地初始化；可以随时关闭，已保存草稿会保留。', 'The current tenant is not fully bootstrapped. Complete Center local initialization step by step; saved drafts remain if you close the wizard.')}</p></div>
              <button type="button" className="btn-secondary" onClick={closeWizard}>{t('稍后处理', 'Later')}</button>
            </header>
            <div className="bootstrap-wizard-steps">
              {stepCopy.map((item, index) => <button key={item.title} type="button" className={index === step ? 'is-active' : index < step ? 'is-done' : ''} onClick={() => setStep(index as WizardStep)}><span>{index + 1}</span><strong>{item.title}</strong><small>{item.desc}</small></button>)}
            </div>
            <div className="bootstrap-wizard-body">{wizardBody()}</div>
            {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
            <footer className="bootstrap-wizard-actions">
              <button type="button" className="btn-secondary" onClick={() => setStep(Math.max(0, step - 1) as WizardStep)} disabled={step === 0 || busy !== null}>{t('上一步', 'Previous')}</button>
              {step < 3 ? <button type="button" className="cloud-primary" onClick={nextWizardStep} disabled={busy !== null}>{busy ? t('处理中...', 'Working...') : t('下一步', 'Next')}</button> : null}
              {step === 3 ? <button type="button" className="btn-secondary" onClick={validatePlan} disabled={busy !== null}>{busy === 'validate' ? t('校验中...', 'Validating...') : t('重新校验', 'Validate again')}</button> : null}
              {step === 3 ? <button type="button" className="cloud-primary" onClick={applyPlan} disabled={busy !== null || !planReady}>{busy === 'apply' ? t('应用中...', 'Applying...') : t('应用计划', 'Apply plan')}</button> : null}
              {step === 3 ? <button type="button" className="cloud-primary" onClick={async () => { const ok = await startFirstWave(); if (ok) closeWizard(); }} disabled={busy !== null || !assets.length}>{busy === 'start' ? t('启动中...', 'Starting...') : t('启动首批任务并完成', 'Start first wave and finish')}</button> : null}
            </footer>
          </section>
        </div>
      ) : null}
    </div>
  );
}
