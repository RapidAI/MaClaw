import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

type Capability = { id: string; name: string; description: string; category: string; version: string; source: string; risk_level: string; status: string; package_status?: string };
type Colleague = { id: string; name: string; role_name?: string; role_code?: string; status?: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };
const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> { const resp = await fetch(url, init); const text = await resp.text(); const data = text ? JSON.parse(text) : null; if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status); return data as T; }
async function fetchJSON<T>(url: string): Promise<T | null> { try { return await requestJSON<T>(url); } catch { return null; } }
const statusLabel = (status: string) => ({ active: 'Active', approved: 'Approved', pending_review: 'Pending review', rejected: 'Rejected', disabled: 'Disabled' }[status] || status || 'Unknown');
const riskLabel = (risk: string) => ({ low: 'Low', medium: 'Medium', high: 'High' }[risk] || risk || 'Low');
const sourceLabel = (source?: string) => { if (!source) return 'Local'; if (source.startsWith('hubcenter:') || source.startsWith('cloud:') || source.startsWith('iworkercloud:')) return 'Cloud Market'; if (source.startsWith('center:')) return 'Center Managed'; return 'Local'; };
const toneForRisk = (risk: string) => risk === 'high' ? 'warn' : 'info';
const canApprove = (cap: Capability) => cap.status === 'pending_review' || cap.status === 'active';
const canReject = (cap: Capability) => cap.status === 'pending_review';
const canInstall = (cap: Capability) => cap.status === 'active' || cap.status === 'approved';

export function PackagesPage() {
  const [caps, setCaps] = useState<Capability[]>([]);
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [selectedWorker, setSelectedWorker] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const loadCaps = async () => {
    setLoading(true); setMessage(null);
    try {
      const data = await fetchJSON<{ capabilities: Capability[] }>('/admin/capabilities');
      if (data?.capabilities) setCaps(data.capabilities); else if (hasWails()) { const list = await (window as any).go.main.App.ListCapabilities(); if (Array.isArray(list)) setCaps(list); }
      const colleaguesResp = await fetchJSON<{ colleagues?: Colleague[] }>('/admin/colleagues');
      if (Array.isArray(colleaguesResp?.colleagues)) setColleagues(colleaguesResp.colleagues); else if (hasWails()) { const list = await (window as any).go.main.App.ListColleagues(); if (Array.isArray(list)) setColleagues(list); }
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to load capability packages' }); }
    finally { setLoading(false); }
  };
  useEffect(() => { void loadCaps(); }, []);

  const summary = useMemo(() => { const pending = caps.filter(c => c.status === 'pending_review').length; const active = caps.filter(c => ['active', 'approved'].includes(c.status)).length; const highRisk = caps.filter(c => c.risk_level === 'high').length; const cloudItems = caps.filter(c => sourceLabel(c.source) === 'Cloud Market').length; const installed = caps.filter(c => c.package_status === 'installed').length; return { pending, active, highRisk, cloudItems, installed }; }, [caps]);
  const runAction = async (cap: Capability, action: 'approve' | 'reject' | 'install') => { const labels = { approve: 'approved', reject: 'rejected', install: 'installed' }; setBusy(cap.id + ':' + action); setMessage(null); try { await requestJSON('/admin/capabilities/' + cap.id + '/' + action, { method: 'POST' }); setMessage({ kind: 'ok', text: cap.name + ' ' + labels[action] + '.' }); await loadCaps(); } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Action failed' }); } finally { setBusy(''); } };
  const bindToWorker = async (cap: Capability) => { const colleagueID = selectedWorker[cap.id] || colleagues[0]?.id || ''; if (!colleagueID) { setMessage({ kind: 'warn', text: 'Create or select an iWorker first.' }); return; } setBusy(cap.id + ':bind'); setMessage(null); try { await requestJSON('/admin/capabilities/' + cap.id + '/bind', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ colleague_id: colleagueID }) }); const worker = colleagues.find(c => c.id === colleagueID); setMessage({ kind: 'ok', text: 'Bound ' + cap.name + ' to ' + (worker?.name || colleagueID) + '.' }); } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Bind failed' }); } finally { setBusy(''); } };
  const pendingCaps = caps.filter(c => c.status === 'pending_review');
  const readyCaps = caps.filter(c => c.status !== 'pending_review');

  return <div className="center-page-stack">
    <SectionCard title="Capability Packages and MCP" desc="iWorkerCenter installs, reviews, and delivers enterprise Skill/MCP packages. iWorkerCloud is only a market and authorization source.">
      <div className="cloud-status-grid cloud-status-grid-wide"><StatusTile label="Available" value={String(summary.active)} tone="ok" /><StatusTile label="Pending" value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} /><StatusTile label="Installed runtime" value={String(summary.installed)} tone="ok" /><StatusTile label="Cloud source" value={String(summary.cloudItems)} /></div>
      <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void loadCaps(); }} disabled={loading}>{loading ? 'Refreshing...' : 'Refresh packages'}</button><span className="cloud-inline-note">Deliverable iWorkers: {colleagues.length} / High risk: {summary.highRisk}</span></div>
      {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
    </SectionCard>
    {pendingCaps.length > 0 && <SectionCard title="Pending Review" desc="Packages from Cloud Market or external sources must be reviewed before installation and delivery."><CapabilityTable caps={pendingCaps} colleagues={colleagues} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} /></SectionCard>}
    <SectionCard title="Installed and Available Packages" desc={'Total ' + readyCaps.length + ' packages. Install runtime entry, then bind the package to an iWorker.'}><CapabilityTable caps={readyCaps} colleagues={colleagues} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} />{readyCaps.length === 0 && <p className="cloud-inline-note">No enabled packages yet. Import from Cloud Market or install local MCP/Skill packages in Center.</p>}</SectionCard>
  </div>;
}

type CapabilityTableProps = { caps: Capability[]; colleagues: Colleague[]; selectedWorker: Record<string, string>; setSelectedWorker: (value: Record<string, string>) => void; busy: string; onApprove: (cap: Capability) => void; onReject: (cap: Capability) => void; onInstall: (cap: Capability) => void; onBind: (cap: Capability) => void };
function CapabilityTable({ caps, colleagues, selectedWorker, setSelectedWorker, busy, onApprove, onReject, onInstall, onBind }: CapabilityTableProps) { return <div className="data-table-wrap capability-action-table"><table className="data-table"><thead><tr><th>Name</th><th>Source</th><th>Version</th><th>Risk</th><th>Status</th><th>Install</th><th>Deliver to iWorker</th><th>Actions</th></tr></thead><tbody>{caps.map(cap => { const isBusy = busy.startsWith(cap.id + ':'); return <tr key={cap.id}><td><strong>{cap.name}</strong><br /><small>{cap.description || cap.category || '-'}</small></td><td>{sourceLabel(cap.source)}</td><td>{cap.version || '-'}</td><td><span className={'badge ' + toneForRisk(cap.risk_level)}>{riskLabel(cap.risk_level)}</span></td><td>{statusLabel(cap.status)}</td><td>{cap.package_status || 'Not installed'}</td><td><div className="capability-bind-controls"><select value={selectedWorker[cap.id] || colleagues[0]?.id || ''} onChange={e => setSelectedWorker({ ...selectedWorker, [cap.id]: e.target.value })} disabled={!colleagues.length || isBusy}>{colleagues.length === 0 ? <option value="">No iWorker</option> : colleagues.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select><button className="btn-secondary" type="button" disabled={!canInstall(cap) || !colleagues.length || isBusy} onClick={() => onBind(cap)}>{busy === cap.id + ':bind' ? 'Delivering...' : 'Deliver'}</button></div></td><td><div className="capability-row-actions">{canApprove(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onApprove(cap)}>{busy === cap.id + ':approve' ? 'Approving...' : 'Approve'}</button>}{canReject(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onReject(cap)}>Reject</button>}{canInstall(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onInstall(cap)}>{busy === cap.id + ':install' ? 'Installing...' : 'Install'}</button>}</div></td></tr>; })}</tbody></table></div>; }
function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) { return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>; }
