import { useEffect, useMemo, useState } from 'react';
import {
  applyBootstrapPlan,
  defaultBootstrapPlan,
  draftBootstrapPlan,
  fetchBootstrapStatus,
  startBootstrapFirstWave,
  validateBootstrapPlan,
  type AppliedAsset,
  type BootstrapPlan,
  type BootstrapStatus,
  type FirstWaveTask,
  type ValidationIssue,
} from '../api/bootstrap';

const splitLines = (value: string) => value.split('\n').map(item => item.trim()).filter(Boolean);
const joinLines = (value: string[] | undefined) => (value || []).join('\n');

const issueText = (issue: ValidationIssue) => {
  const fields: Record<string, string> = {
    company_name: '企业名称',
    virtual_departments: '虚拟部门',
    initial_iworkers: '首批 iWorker',
    memory_scopes: '记忆范围',
    watcher_policy: '自动运行策略',
  };
  return (fields[issue.field] || issue.field) + ': ' + issue.message;
};

const assetKindText = (kind: string) => ({
  role: '角色',
  iworker: 'iWorker',
  memory: '记忆',
  workflow: '流程模板',
  workflow_instance: '流程实例',
  goalwatch_policy: '自动运行策略',
}[kind] || kind);

function TextAreaList({ label, value, onChange, hint }: { label: string; value: string[]; onChange: (next: string[]) => void; hint?: string }) {
  return (
    <label className="cloud-field">
      <span>{label}</span>
      <textarea value={joinLines(value)} onChange={e => onChange(splitLines(e.target.value))} rows={5} />
      {hint && <small className="cloud-inline-note">{hint}</small>}
    </label>
  );
}

function FirstWaveList({ tasks }: { tasks: FirstWaveTask[] }) {
  if (!tasks.length) return <p className="cloud-inline-note">保存或校验启动计划后，会生成建议的首批任务。</p>;
  return (
    <div className="item-list">
      {tasks.map(task => (
        <div className="item-row" key={task.id}>
          <strong>{task.title}</strong>
          <p>负责人：{task.owner_iworker} · 触发：{task.recommended_trigger} · 记忆范围：{task.memory_scope}</p>
          <p>产出：{task.expected_output}；升级边界：{task.escalation_threshold}</p>
          <span className={task.requires_peer_review ? 'badge warn' : 'badge info'}>{task.requires_peer_review ? '需要同伴复核' : '可直接进入人工可见队列'}</span>
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
          <strong>{asset.name || asset.id}</strong>
          <p>{assetKindText(asset.kind)} · {asset.status || 'ready'}{asset.id ? ' · ' + asset.id : ''}</p>
        </div>
      ))}
    </div>
  );
}

export function BootstrapPage() {
  const [status, setStatus] = useState<BootstrapStatus | null>(null);
  const [plan, setPlan] = useState<BootstrapPlan>(defaultBootstrapPlan());
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [firstWave, setFirstWave] = useState<FirstWaveTask[]>([]);
  const [assets, setAssets] = useState<AppliedAsset[]>([]);
  const [message, setMessage] = useState<{ kind: 'ok' | 'warn' | 'danger'; text: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const blockingIssues = useMemo(() => issues.filter(issue => issue.level === 'error'), [issues]);
  const ready = blockingIssues.length === 0 && Boolean(plan.company_name.trim()) && plan.initial_iworkers.length >= 2;

  const loadStatus = async () => {
    setBusy('load');
    try {
      const next = await fetchBootstrapStatus();
      setStatus(next);
      if (next.plan) setPlan(next.plan);
      setIssues(next.validation_issues || []);
      setFirstWave(next.suggested_first_wave || []);
      setAssets(next.applied_assets || []);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '读取初始化状态失败' });
    } finally {
      setBusy(null);
    }
  };

  useEffect(() => { void loadStatus(); }, []);

  const updatePlan = (patch: Partial<BootstrapPlan>) => setPlan(current => ({ ...current, ...patch }));

  const saveDraft = async () => {
    setBusy('draft');
    setMessage(null);
    try {
      const res = await draftBootstrapPlan(plan);
      setPlan(res.plan);
      setIssues(res.validation_issues || []);
      setFirstWave(res.suggested_first_wave || []);
      setMessage({ kind: 'ok', text: '启动计划已保存，后续可以继续校验或应用。' });
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '保存启动计划失败' });
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
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '校验启动计划失败' });
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
      setMessage({ kind: 'ok', text: '已应用启动计划：组织角色、首批 iWorker、流程模板、记忆和自动运行策略已写入 Center。' });
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '应用启动计划失败' });
    } finally {
      setBusy(null);
    }
  };

  const startFirstWave = async () => {
    setBusy('start');
    setMessage(null);
    try {
      const res = await startBootstrapFirstWave();
      setStatus(current => current ? { ...current, last_run: res.run } : current);
      setMessage({ kind: 'ok', text: '首批任务已启动：' + res.run.id });
      void loadStatus();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '启动首批任务失败，请确认已经应用启动计划。' });
      setBusy(null);
    }
  };

  return (
    <div className="center-page-stack">
      <section className="card section-card">
        <div className="section-head">
          <div>
            <h3>Bootstrap 流程</h3>
            <p>新单位先在 Center 本地完成组织初始化，再按需注册到 iWorkerCloud。Cloud 离线时，本地 iWorker、流程、记忆和人工协作仍可继续工作。</p>
          </div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={loadStatus} disabled={busy !== null}>{busy === 'load' ? '刷新中...' : '刷新状态'}</button>
            <span className={status?.ready_to_start || ready ? 'badge ok' : 'badge warn'}>{status?.has_plan ? '已有启动计划' : '待创建计划'}</span>
          </div>
        </div>
        <div className="cloud-step-list bootstrap-step-list">
          <div className="cloud-step is-done"><span>1</span><strong>创建本地租户</strong><p>管理员账号和企业基础资料保存在 Center 本地。</p></div>
          <div className={plan.company_name ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>2</span><strong>保存启动计划</strong><p>确认部门、首批 iWorker、记忆范围和人工确认边界。</p></div>
          <div className={assets.length ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>3</span><strong>应用组织骨架</strong><p>创建角色、数字员工、流程模板、记忆和自动运行策略。</p></div>
          <div className={status?.last_run ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>4</span><strong>启动首批任务</strong><p>让 iWorker 在可审计边界内开始协作，并向人类员工显式提示。</p></div>
        </div>
      </section>

      <section className="card section-card">
        <div className="section-head">
          <div>
            <h3>启动计划</h3>
            <p>这里定义的是企业本地业务启动方式，不会把业务数据交给 Cloud。</p>
          </div>
          <div className="cloud-actions">
            <button className="btn-secondary" type="button" onClick={validatePlan} disabled={busy !== null}>{busy === 'validate' ? '校验中...' : '校验计划'}</button>
            <button className="cloud-primary" type="button" onClick={saveDraft} disabled={busy !== null}>{busy === 'draft' ? '保存中...' : '保存草稿'}</button>
          </div>
        </div>
        <div className="cloud-form-grid">
          <label className="cloud-field">
            <span>单位/企业名称</span>
            <input value={plan.company_name} onChange={e => updatePlan({ company_name: e.target.value })} placeholder="例如：XX 科技有限公司" />
          </label>
          <label className="cloud-field">
            <span>当前优先级</span>
            <input value={plan.priority} onChange={e => updatePlan({ priority: e.target.value })} />
          </label>
          <label className="cloud-field cloud-field-wide">
            <span>业务摘要</span>
            <textarea value={plan.business_summary} onChange={e => updatePlan({ business_summary: e.target.value })} rows={4} placeholder="说明这个单位的主营业务、当前目标和上线边界" />
          </label>
          <TextAreaList label="虚拟部门" value={plan.virtual_departments} onChange={value => updatePlan({ virtual_departments: value })} hint="每行一个部门，至少 3 个。" />
          <TextAreaList label="首批 iWorker" value={plan.initial_iworkers} onChange={value => updatePlan({ initial_iworkers: value })} hint="每行一个数字同事，至少 2 个。" />
          <TextAreaList label="记忆范围" value={plan.memory_scopes} onChange={value => updatePlan({ memory_scopes: value })} hint="建议保留 company / department / personal。" />
          <TextAreaList label="循环任务" value={plan.recurring_tasks} onChange={value => updatePlan({ recurring_tasks: value })} />
          <TextAreaList label="需要负责人确认的边界" value={plan.requires_executive_confirmation} onChange={value => updatePlan({ requires_executive_confirmation: value })} />
        </div>
        <div className="bootstrap-policy-grid">
          <label><input type="checkbox" checked={plan.watcher_policy.enabled} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, enabled: e.target.checked } })} /> 启用自动运行观察器</label>
          <label><input type="checkbox" checked={plan.watcher_policy.single_flight} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, single_flight: e.target.checked } })} /> 单任务防重入</label>
          <label><input type="checkbox" checked={plan.watcher_policy.scale_by_worker_count} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, scale_by_worker_count: e.target.checked } })} /> 按 iWorker 数量扩展</label>
          <label className="cloud-field"><span>单次最长运行秒数</span><input type="number" min={30} value={plan.watcher_policy.max_run_seconds} onChange={e => updatePlan({ watcher_policy: { ...plan.watcher_policy, max_run_seconds: Number(e.target.value) || 120 } })} /></label>
        </div>
        {issues.length > 0 && (
          <div className="bootstrap-issues">
            {issues.map((issue, index) => <p key={issue.field + '-' + index} className={'cloud-message ' + (issue.level === 'error' ? 'danger' : 'warn')}>{issueText(issue)}</p>)}
          </div>
        )}
        {message && <p className={'cloud-message ' + message.kind}>{message.text}</p>}
      </section>

      <div className="panel-grid">
        <section className="section-card">
          <div className="section-head">
            <div><h3>建议首批任务</h3><p>这些任务会进入 Center 的流程实例，iWorker 执行时仍需把人工干预点显式推给人类员工。</p></div>
            <button className="cloud-primary" type="button" onClick={startFirstWave} disabled={busy !== null || !ready}>{busy === 'start' ? '启动中...' : '启动首批任务'}</button>
          </div>
          <FirstWaveList tasks={firstWave} />
        </section>
        <section className="section-card">
          <div className="section-head">
            <div><h3>已应用资产</h3><p>应用计划会把组织骨架写入 Center 本地，不依赖 Cloud 在线。</p></div>
            <button className="btn-secondary" type="button" onClick={applyPlan} disabled={busy !== null || !ready}>{busy === 'apply' ? '应用中...' : '应用启动计划'}</button>
          </div>
          <AssetList assets={assets} />
        </section>
      </div>

      {status?.last_run && (
        <section className="card section-card">
          <div className="section-head"><div><h3>最近一次首批运行</h3><p>{status.last_run.id} · {status.last_run.status} · {new Date(status.last_run.updated_at).toLocaleString()}</p></div></div>
          <FirstWaveList tasks={status.last_run.tasks || []} />
        </section>
      )}
    </div>
  );
}
