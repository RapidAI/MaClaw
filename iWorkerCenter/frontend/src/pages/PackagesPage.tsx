import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';

type Capability = { id: string; name: string; description: string; category: string; version: string; source: string; risk_level: string; status: string; package_status?: string };
type Colleague = { id: string; name: string; role_name?: string; role_code?: string; status?: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> { const resp = await fetch(url, init); const text = await resp.text(); const data = text ? JSON.parse(text) : null; if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status); return data as T; }
async function fetchJSON<T>(url: string): Promise<T | null> { try { return await requestJSON<T>(url); } catch { return null; } }

const toneForRisk = (risk: string) => risk === 'high' ? 'warn' : 'info';
const canApprove = (cap: Capability) => cap.status === 'pending_review' || cap.status === 'active';
const canReject = (cap: Capability) => cap.status === 'pending_review';
const canInstall = (cap: Capability) => cap.status === 'active' || cap.status === 'approved';

export function PackagesPage() {
  const { t } = useI18n();
  const [caps, setCaps] = useState<Capability[]>([]);
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [selectedWorker, setSelectedWorker] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const sourceLabel = (source?: string) => { if (!source) return t('本地', 'Local'); if (source.startsWith('hubcenter:') || source.startsWith('cloud:') || source.startsWith('iworkercloud:')) return t('Cloud 市场', 'Cloud Market'); if (source.startsWith('center:')) return t('Center 管理', 'Center Managed'); return t('本地', 'Local'); };
  const statusLabel = (status: string) => ({ active: t('启用', 'Active'), approved: t('已批准', 'Approved'), pending_review: t('待审核', 'Pending review'), rejected: t('已拒绝', 'Rejected'), disabled: t('禁用', 'Disabled') }[status] || status || t('未知', 'Unknown'));
  const riskLabel = (risk: string) => ({ low: t('低', 'Low'), medium: t('中', 'Medium'), high: t('高', 'High') }[risk] || risk || t('低', 'Low'));

  const loadCaps = async () => {
    setLoading(true); setMessage(null);
    try {
      const data = await fetchJSON<{ capabilities: Capability[] }>('/admin/capabilities');
      if (data?.capabilities) setCaps(data.capabilities); else if (hasWails()) { const list = await (window as any).go.main.App.ListCapabilities(); if (Array.isArray(list)) setCaps(list); }
      const colleaguesResp = await fetchJSON<{ colleagues?: Colleague[] }>('/admin/colleagues');
      if (Array.isArray(colleaguesResp?.colleagues)) setColleagues(colleaguesResp.colleagues); else if (hasWails()) { const list = await (window as any).go.main.App.ListColleagues(); if (Array.isArray(list)) setColleagues(list); }
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载能力包失败。', 'Failed to load capability packages.') }); }
    finally { setLoading(false); }
  };

  useEffect(() => { void loadCaps(); }, []);

  const summary = useMemo(() => {
    const pending = caps.filter(c => c.status === 'pending_review').length;
    const active = caps.filter(c => ['active', 'approved'].includes(c.status)).length;
    const highRisk = caps.filter(c => c.risk_level === 'high').length;
    const cloudItems = caps.filter(c => sourceLabel(c.source) === t('Cloud 市场', 'Cloud Market')).length;
    const installed = caps.filter(c => c.package_status === 'installed').length;
    return { pending, active, highRisk, cloudItems, installed };
  }, [caps, t]);

  const runAction = async (cap: Capability, action: 'approve' | 'reject' | 'install') => {
    const labels = { approve: t('已批准', 'approved'), reject: t('已拒绝', 'rejected'), install: t('已安装', 'installed') };
    setBusy(cap.id + ':' + action); setMessage(null);
    try { await requestJSON('/admin/capabilities/' + cap.id + '/' + action, { method: 'POST' }); setMessage({ kind: 'ok', text: cap.name + ' ' + labels[action] + '.' }); await loadCaps(); }
    catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('操作失败。', 'Action failed.') }); }
    finally { setBusy(''); }
  };

  const bindToWorker = async (cap: Capability) => {
    const colleagueID = selectedWorker[cap.id] || colleagues[0]?.id || '';
    if (!colleagueID) { setMessage({ kind: 'warn', text: t('请先创建或选择一个 iWorker。', 'Create or select an iWorker first.') }); return; }
    setBusy(cap.id + ':bind'); setMessage(null);
    try {
      await requestJSON('/admin/capabilities/' + cap.id + '/bind', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ colleague_id: colleagueID }) });
      const worker = colleagues.find(c => c.id === colleagueID);
      setMessage({ kind: 'ok', text: t('已下发 ', 'Delivered ') + cap.name + t(' 给 ', ' to ') + (worker?.name || colleagueID) + '.' });
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('下发失败。', 'Delivery failed.') }); }
    finally { setBusy(''); }
  };

  const pendingCaps = caps.filter(c => c.status === 'pending_review');
  const readyCaps = caps.filter(c => c.status !== 'pending_review');

  return <div className="center-page-stack">
    <SectionCard title={t('能力包与 MCP', 'Capability Packages and MCP')} desc={t('iWorkerCenter 负责安装、审核并向企业 iWorker 下发 Skill/MCP。iWorkerCloud 只作为市场和授权来源，不参与企业业务执行。', 'iWorkerCenter installs, reviews, and delivers enterprise Skill/MCP packages. iWorkerCloud is only a market and authorization source and does not participate in enterprise work execution.')}>
      <div className="cloud-status-grid cloud-status-grid-wide"><StatusTile label={t('可用', 'Available')} value={String(summary.active)} tone="ok" /><StatusTile label={t('待审核', 'Pending')} value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} /><StatusTile label={t('已安装运行时', 'Installed runtime')} value={String(summary.installed)} tone="ok" /><StatusTile label={t('Cloud 来源', 'Cloud source')} value={String(summary.cloudItems)} /></div>
      <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void loadCaps(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新能力包', 'Refresh packages')}</button><span className="cloud-inline-note">{t('可下发 iWorker：', 'Deliverable iWorkers: ')}{colleagues.length} / {t('高风险：', 'High risk: ')}{summary.highRisk}</span></div>
      {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
    </SectionCard>
    {pendingCaps.length > 0 && <SectionCard title={t('待审核', 'Pending Review')} desc={t('来自 Cloud 市场或外部来源的能力包，需要先审核再安装和下发。', 'Packages from Cloud Market or external sources must be reviewed before installation and delivery.')}><CapabilityTable caps={pendingCaps} colleagues={colleagues} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} labels={{ statusLabel, riskLabel, sourceLabel }} /></SectionCard>}
    <SectionCard title={t('已安装与可用能力包', 'Installed and Available Packages')} desc={t('共 ' + readyCaps.length + ' 个能力包。先安装运行时入口，再绑定到对应 iWorker。', 'Total ' + readyCaps.length + ' packages. Install runtime entry first, then bind the package to an iWorker.')}><CapabilityTable caps={readyCaps} colleagues={colleagues} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} labels={{ statusLabel, riskLabel, sourceLabel }} />{readyCaps.length === 0 && <p className="cloud-inline-note">{t('还没有启用的能力包。可从 Cloud 市场导入，或在 Center 本地安装 MCP/Skill。', 'No enabled packages yet. Import from Cloud Market or install local MCP/Skill packages in Center.')}</p>}</SectionCard>
  </div>;
}

type CapabilityTableProps = { caps: Capability[]; colleagues: Colleague[]; selectedWorker: Record<string, string>; setSelectedWorker: (value: Record<string, string>) => void; busy: string; onApprove: (cap: Capability) => void; onReject: (cap: Capability) => void; onInstall: (cap: Capability) => void; onBind: (cap: Capability) => void; labels: { statusLabel: (v: string) => string; riskLabel: (v: string) => string; sourceLabel: (v?: string) => string } };
function CapabilityTable({ caps, colleagues, selectedWorker, setSelectedWorker, busy, onApprove, onReject, onInstall, onBind, labels }: CapabilityTableProps) {
  const { t } = useI18n();
  return <div className="data-table-wrap capability-action-table"><table className="data-table"><thead><tr><th>{t('名称', 'Name')}</th><th>{t('来源', 'Source')}</th><th>{t('版本', 'Version')}</th><th>{t('风险', 'Risk')}</th><th>{t('状态', 'Status')}</th><th>{t('安装', 'Install')}</th><th>{t('下发到 iWorker', 'Deliver to iWorker')}</th><th>{t('操作', 'Actions')}</th></tr></thead><tbody>{caps.map(cap => { const isBusy = busy.startsWith(cap.id + ':'); return <tr key={cap.id}><td><strong>{cap.name}</strong><br /><small>{cap.description || cap.category || '-'}</small></td><td>{labels.sourceLabel(cap.source)}</td><td>{cap.version || '-'}</td><td><span className={'badge ' + toneForRisk(cap.risk_level)}>{labels.riskLabel(cap.risk_level)}</span></td><td>{labels.statusLabel(cap.status)}</td><td>{cap.package_status || t('未安装', 'Not installed')}</td><td><div className="capability-bind-controls"><select value={selectedWorker[cap.id] || colleagues[0]?.id || ''} onChange={e => setSelectedWorker({ ...selectedWorker, [cap.id]: e.target.value })} disabled={!colleagues.length || isBusy}>{colleagues.length === 0 ? <option value="">{t('无 iWorker', 'No iWorker')}</option> : colleagues.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select><button className="btn-secondary" type="button" disabled={!canInstall(cap) || !colleagues.length || isBusy} onClick={() => onBind(cap)}>{busy === cap.id + ':bind' ? t('下发中...', 'Delivering...') : t('下发', 'Deliver')}</button></div></td><td><div className="capability-row-actions">{canApprove(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onApprove(cap)}>{busy === cap.id + ':approve' ? t('批准中...', 'Approving...') : t('批准', 'Approve')}</button>}{canReject(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onReject(cap)}>{t('拒绝', 'Reject')}</button>}{canInstall(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onInstall(cap)}>{busy === cap.id + ':install' ? t('安装中...', 'Installing...') : t('安装', 'Install')}</button>}</div></td></tr>; })}</tbody></table></div>;
}
function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) { return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>; }
