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

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;
type WizardStep = 0 | 1 | 2 | 3;

type Props = {
  wizardOpen?: boolean;
  onWizardClose?: () => void;
  onBootstrapChanged?: (status: BootstrapStatus | null) => void;
};

const splitLines = (value: string) => value.split('\n').map(item => item.trim()).filter(Boolean);
const joinLines = (value: string[] | undefined) => (value || []).join('\n');


const fieldLabels: Record<string, string> = {
  company_name: '单位名称',
  legal_person: '法人姓名',
  company_address: '公司地址',
  contact_email: '联系邮箱',
  business_summary: '业务摘要',
  virtual_departments: '虚拟部门',
  initial_iworkers: '首批 iWorker',
  memory_scopes: '记忆范围',
  recurring_tasks: '循环任务',
  requires_executive_confirmation: '负责人确认边界',
  watcher_policy: '自动运行策略',
};

const assetKindText = (kind: string) => ({
  role: '角色',
  iworker: 'iWorker',
  memory: '记忆',
  workflow: '流程模板',
  workflow_instance: '流程实例',
  goalwatch_policy: '自动运行策略',
}[kind] || kind);

const stepCopy: Array<{ title: string; desc: string }> = [
  { title: '单位信息', desc: '确认单位名称、业务目标和本地上线边界。' },
  { title: '组织骨架', desc: '配置虚拟部门、首批数字同事和可复用记忆范围。' },
  { title: '运行边界', desc: '设置循环任务、人工确认边界和自动守护策略。' },
  { title: '应用启动', desc: '校验计划，写入 Center 本地资产，并启动首批任务。' },
];

function TextAreaList({ label, value, onChange, hint, rows = 5 }: { label: string; value: string[]; onChange: (next: string[]) => void; hint?: string; rows?: number }) {
  return (
    <label className="cloud-field">
      <span>{label}</span>
      <textarea value={joinLines(value)} onChange={e => onChange(splitLines(e.target.value))} rows={rows} />
      {hint ? <small className="cloud-inline-note">{hint}</small> : null}
    </label>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}

function FirstWaveList({ tasks }: { tasks: FirstWaveTask[] }) {
  if (!tasks.length) return <p className="cloud-inline-note">保存或校验启动计划后，会生成建议的首批任务。</p>;
  return (
    <div className="item-list">
      {tasks.map(task => (
        <div className="item-row" key={task.id || task.title}>
          <strong>{task.title}</strong>
          <p>负责人：{task.owner_iworker || '-'} / 触发：{task.recommended_trigger || '-'} / 记忆范围：{task.memory_scope || '-'}</p>
          <p>产出：{task.expected_output || '-'} / 升级边界：{task.escalation_threshold || '-'}</p>
          <span className={task.requires_peer_review ? 'badge warn' : 'badge info'}>{task.requires_peer_review ? '需要同伴复核' : '进入人工可见队列'}</span>
        </div>
      ))}
    </div>
  );
}

function AssetList({ assets }: { assets: AppliedAsset[] }) {
  if (!assets.length) return <p className="cloud-inline-note">应用启动计划后会展示创建或复用的角色、iWorker、流程、记忆和策略。</p>;
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
  if (!issues.length) return null;
  return (
    <div className="bootstrap-issues">
      {issues.map((issue, index) => (
        <p key={issue.field + '-' + index} className={'cloud-message ' + (issue.level === 'error' ? 'danger' : 'warn')}>
          {(fieldLabels[issue.field] || issue.field) + ': ' + issue.message}
        </p>
      ))}
    </div>
  );
}

function RunSummary({ run }: { run?: BootstrapRun }) {
  if (!run) return null;
  return (
    <section className="card section-card">
      <div className="section-head"><div><h3>最近一次首批运行</h3><p>{run.id} / {run.status} / {run.updated_at ? new Date(run.updated_at).toLocaleString() : '-'}</p></div></div>
      <FirstWaveList tasks={run.tasks || []} />
    </section>
  );
}

export function BootstrapPage({ wizardOpen = false, onWizardClose, onBootstrapChanged }: Props) {
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
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '读取初始化状态失败' });
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
      setMessage({ kind: 'ok', text: '启动计划已保存，可以继续校验或应用。' });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '保存启动计划失败' });
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
      setMessage({ kind: res.ready_to_start ? 'ok' : 'warn', text: res.ready_to_start ? '启动计划已通过校验。' : '启动计划还有阻塞项，请先修正。' });
      return res.ready_to_start;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '校验启动计划失败' });
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
      setMessage({ kind: 'ok', text: '已应用启动计划：角色、首批 iWorker、流程模板、记忆和策略已写入 Center。' });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '应用启动计划失败' });
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
      setMessage({ kind: 'ok', text: '首批任务已启动：' + res.run.id });
      await loadStatus();
      return true;
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '启动首批任务失败，请确认已经应用启动计划。' });
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
        setMessage({ kind: 'warn', text: contactEmailValid ? '请先填写单位名称、法人姓名、公司地址和联系邮箱。' : '请填写有效的公司联系邮箱。' });
        return;
      }
      await saveDraft();
      setStep(1);
      return;
    }
    if (step === 1) {
      if (plan.initial_iworkers.length < 2) {
        setMessage({ kind: 'warn', text: '请至少配置 2 个首批 iWorker。' });
        return;
      }
      await saveDraft();
      setStep(2);
      return;
    }
    if (step === 2) {
      await saveDraft();
      const ok = await validatePlan();
      if (ok) setStep(3);
    }
  };

  const renderPlanFields = (mode: 'full' | 'wizard') => (
    <div className="cloud-form-grid">
      <label className="cloud-field">
        <span>单位/企业名称</span>
        <input value={plan.company_name} onChange={e => updatePlan({ company_name: e.target.value })} placeholder="例如：XX 科技有限公司" />
      </label>
      <label className="cloud-field">
        <span>法人姓名</span>
        <input value={plan.legal_person} onChange={e => updatePlan({ legal_person: e.target.value })} placeholder="法人代表姓名" />
      </label>
      <label className="cloud-field">
        <span>公司联系邮箱</span>
        <input type="email" value={plan.contact_email} onChange={e => updatePlan({ contact_email: e.target.value })} placeholder="admin@example.com" />
      </label>
      <label className="cloud-field">
        <span>公司地址</span>
        <input value={plan.company_address} onChange={e => updatePlan({ company_address: e.target.value })} placeholder="注册地址或办公地址" />
      </label>
      <label className="cloud-field cloud-field-wide">
        <span>当前优先级</span>
        <input value={plan.priority} onChange={e => updatePlan({ priority: e.target.value })} />
      </label>
      <label className="cloud-field cloud-field-wide">
        <span>业务摘要</span>
        <textarea value={plan.business_summary} onChange={e => updatePlan({ business_summary: e.target.value })} rows={mode === 'wizard' ? 3 : 4} placeholder="说明主营业务、当前目标和上线边界" />
      </label>
      {(mode === 'full' || step === 1) ? <TextAreaList label="虚拟部门" value={plan.virtual_departments} onChange={value => updatePlan({ virtual_departments: value })} hint="每行一个部门，建议至少 3 个。" /> : null}
      {(mode === 'full' || step === 1) ? <TextAreaList label="首批 iWorker" value={plan.initial_iworkers} onChange={value => updatePlan({ initial_iworkers: value })} hint="每行一个数字同事，建议至少 2 个。" /> : null}
      {(mode === 'full' || step === 1) ? <TextAreaList label="记忆范围" value={plan.memory_scopes} onChange={value => updatePlan({ memory_scopes: value })} hint="建议保留 company / department / personal。" /> : null}
      {(mode === 'full' || step === 2) ? <TextAreaList label="循环任务" value={plan.recurring_tasks} onChange={value => updatePlan({ recurring_tasks: value })} /> : null}
      {(mode === 'full' || step === 2) ? <TextAreaList label="需要负责人确认的边界" value={plan.requires_executive_confirmation} onChange={value => updatePlan({ requires_executive_confirmation: value })} /> : null}
    </div>
  );

  const renderPolicy = () => (
    <div className="bootstrap-policy-grid">
      <label><input type="checkbox" checked={plan.watcher_policy.enabled} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, enabled: e.target.checked } })} /> 启用自动运行观察器</label>
      <label><input type="checkbox" checked={plan.watcher_policy.single_flight} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, single_flight: e.target.checked } })} /> 单任务防重入</label>
      <label><input type="checkbox" checked={plan.watcher_policy.scale_by_worker_count} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, scale_by_worker_count: e.target.checked } })} /> 按 iWorker 数量扩展</label>
      <label className="cloud-field"><span>单次最长运行秒数</span><input type="number" min={30} value={plan.watcher_policy.max_run_seconds} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, max_run_seconds: Number(e.target.value) || 120 } })} /></label>
    </div>
  );

  const wizardBody = () => {
    if (step === 0) return <>{renderPlanFields('wizard')}</>;
    if (step === 1) return <>{renderPlanFields('wizard')}</>;
    if (step === 2) return <>{renderPlanFields('wizard')}{renderPolicy()}</>;
    return (
      <div className="bootstrap-wizard-review">
        <div className="cloud-status-grid">
          <StatusTile label="当前租户" value={status?.tenant_id || plan.tenant_id || '-'} tone={status?.tenant_id ? 'ok' : 'warn'} />
          <StatusTile label="联系邮箱" value={plan.contact_email || '-'} tone={contactEmailValid && plan.contact_email ? 'ok' : 'warn'} />
          <StatusTile label="首批 iWorker" value={String(plan.initial_iworkers.length)} tone={plan.initial_iworkers.length >= 2 ? 'ok' : 'warn'} />
          <StatusTile label="阻塞项" value={String(blockingIssues.length)} tone={blockingIssues.length ? 'warn' : 'ok'} />
        </div>
        <IssueList issues={issues} />
        <div className="panel-grid bootstrap-wizard-panels">
          <section className="section-card"><div className="section-head"><div><h3>建议首批任务</h3><p>应用计划后可以启动这些任务。</p></div></div><FirstWaveList tasks={firstWave} /></section>
          <section className="section-card"><div className="section-head"><div><h3>已应用资产</h3><p>应用计划后写入 Center 本地。</p></div></div><AssetList assets={assets} /></section>
        </div>
      </div>
    );
  };

  return (
    <div className="center-page-stack">
      <section className="card section-card">
        <div className="section-head">
          <div>
            <h3>Bootstrap 流程</h3>
            <p>每个租户独立完成 Center 本地初始化。Cloud 只处理注册、授权和算力协调；Cloud 离线时，本地 iWorker、流程、记忆和人工协作仍可继续工作。</p>
          </div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={loadStatus} disabled={busy !== null}>{busy === 'load' ? '刷新中...' : '刷新状态'}</button>
            <button className="cloud-primary" type="button" onClick={() => { setStep(0); setLocalWizardOpen(true); }}>打开向导</button>
            <span className={bootstrapDone ? 'badge ok' : 'badge warn'}>{bootstrapDone ? '当前租户已完成 bootstrap' : status?.has_plan ? '当前租户计划未完成' : '当前租户需要 bootstrap'}</span>
          </div>
        </div>
        <div className="cloud-step-list bootstrap-step-list">
          <div className="cloud-step is-done"><span>1</span><strong>创建本地租户</strong><p>管理员账号和企业基础资料保存在 Center 本地。</p></div>
          <div className={status?.has_plan || plan.company_name ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>2</span><strong>保存启动计划</strong><p>确认部门、首批 iWorker、记忆范围和人工确认边界。</p></div>
          <div className={assets.length ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>3</span><strong>应用组织骨架</strong><p>创建角色、数字员工、流程模板、记忆和自动运行策略。</p></div>
          <div className={status?.last_run ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>4</span><strong>启动首批任务</strong><p>让 iWorker 在可审计边界内开始协作，并向人类员工显式提示。</p></div>
        </div>
      </section>

      <section className="card section-card">
        <div className="section-head">
          <div><h3>启动计划</h3><p>这里定义当前租户的本地业务启动方式，不会把业务数据交给 Cloud。</p></div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={validatePlan} disabled={busy !== null}>{busy === 'validate' ? '校验中...' : '校验计划'}</button>
            <button className="cloud-primary" type="button" onClick={saveDraft} disabled={busy !== null}>{busy === 'draft' ? '保存中...' : '保存草稿'}</button>
          </div>
        </div>
        {renderPlanFields('full')}
        {renderPolicy()}
        <IssueList issues={issues} />
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </section>

      <div className="panel-grid">
        <section className="section-card">
          <div className="section-head"><div><h3>建议首批任务</h3><p>这些任务会进入 Center 的流程实例，iWorker 执行时仍需把人工干预点显式推给人类员工。</p></div><button className="cloud-primary" type="button" onClick={startFirstWave} disabled={busy !== null || !planReady}>{busy === 'start' ? '启动中...' : '启动首批任务'}</button></div>
          <FirstWaveList tasks={firstWave} />
        </section>
        <section className="section-card">
          <div className="section-head"><div><h3>已应用资产</h3><p>应用计划会把组织骨架写入当前租户的 Center 本地空间，不依赖 Cloud 在线。</p></div><button className="btn-secondary" type="button" onClick={applyPlan} disabled={busy !== null || !planReady}>{busy === 'apply' ? '应用中...' : '应用启动计划'}</button></div>
          <AssetList assets={assets} />
        </section>
      </div>

      <RunSummary run={status?.last_run} />

      {effectiveWizardOpen ? (
        <div className="bootstrap-wizard-overlay" role="dialog" aria-modal="true" aria-label="Bootstrap 向导">
          <section className="bootstrap-wizard-modal">
            <header className="bootstrap-wizard-head">
              <div><span className="mini">BOOTSTRAP WIZARD</span><h3>单位初始化向导</h3><p>当前租户尚未完成正常 bootstrap。按步骤完成 Center 本地初始化；可以随时关闭，已保存草稿会保留。</p></div>
              <button type="button" className="btn-secondary" onClick={closeWizard}>稍后处理</button>
            </header>
            <div className="bootstrap-wizard-steps">
              {stepCopy.map((item, index) => <button key={item.title} type="button" className={index === step ? 'is-active' : index < step ? 'is-done' : ''} onClick={() => setStep(index as WizardStep)}><span>{index + 1}</span><strong>{item.title}</strong><small>{item.desc}</small></button>)}
            </div>
            <div className="bootstrap-wizard-body">{wizardBody()}</div>
            {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
            <footer className="bootstrap-wizard-actions">
              <button type="button" className="btn-secondary" onClick={() => setStep(Math.max(0, step - 1) as WizardStep)} disabled={step === 0 || busy !== null}>上一步</button>
              {step < 3 ? <button type="button" className="cloud-primary" onClick={nextWizardStep} disabled={busy !== null}>{busy ? '处理中...' : '下一步'}</button> : null}
              {step === 3 ? <button type="button" className="btn-secondary" onClick={validatePlan} disabled={busy !== null}>{busy === 'validate' ? '校验中...' : '重新校验'}</button> : null}
              {step === 3 ? <button type="button" className="cloud-primary" onClick={applyPlan} disabled={busy !== null || !planReady}>{busy === 'apply' ? '应用中...' : '应用计划'}</button> : null}
              {step === 3 ? <button type="button" className="cloud-primary" onClick={async () => { const ok = await startFirstWave(); if (ok) closeWizard(); }} disabled={busy !== null || !assets.length}>{busy === 'start' ? '启动中...' : '启动首批任务并完成'}</button> : null}
            </footer>
          </section>
        </div>
      ) : null}
    </div>
  );
}
