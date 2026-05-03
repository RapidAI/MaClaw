import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Memory = { id: string; title?: string; content?: string; level?: string; scope?: string; tags?: string[]; version?: number; status?: string; created_at?: string; updated_at?: string };
type MemoryRow = { topic: string; scope: string; level: string; tags: string; updated: string; status: string };
type MemoryForm = { title: string; content: string; level: string; scope: string; tags: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
const emptyForm = (): MemoryForm => ({ title: '', content: '', level: 'enterprise', scope: 'all', tags: '' });

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  return data as T;
}
async function fetchJSON<T>(url: string): Promise<T | null> { try { return await requestJSON<T>(url); } catch { return null; } }
const splitTags = (value: string) => value
  .replaceAll(String.fromCharCode(13), '')
  .replaceAll(String.fromCharCode(10), ',')
  .split(',')
  .map(tag => tag.trim())
  .filter(Boolean);
const levelLabel = (level?: string) => ({ enterprise: 'Enterprise', company: 'Company', department: 'Department', role: 'Role', team: 'Team', personal: 'Personal' }[level || ''] || level || 'General');
const statusLabel = (status?: string) => ({ active: 'Active', draft: 'Draft', merged: 'Merged', expired: 'Expired', disabled: 'Disabled' }[status || ''] || status || 'Active');
const toRows = (memories: Memory[]): MemoryRow[] => memories.map(memory => ({ topic: memory.title || (memory.content || memory.id).slice(0, 40), scope: memory.scope || 'all', level: levelLabel(memory.level || memory.scope), tags: (memory.tags || []).join(', ') || '-', updated: memory.updated_at || memory.created_at || '-', status: statusLabel(memory.status) }));

export function KnowledgePage() {
  const [memories, setMemories] = useState<Memory[]>([]);
  const [form, setForm] = useState<MemoryForm>(emptyForm());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [source, setSource] = useState('Center API');
  const [message, setMessage] = useState<Message | null>(null);

  const loadMemories = async () => {
    setLoading(true); setMessage(null);
    try {
      const data = await fetchJSON<{ memories?: Memory[] }>('/admin/memories');
      if (Array.isArray(data?.memories)) { setMemories(data.memories); setSource('Center API'); return; }
      if (hasWails()) { const mems = await (window as any).go.main.App.ListMemories(); if (Array.isArray(mems)) { setMemories(mems); setSource('Local runtime'); return; } }
      setSource('No data');
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to load memories' }); }
    finally { setLoading(false); }
  };
  useEffect(() => { void loadMemories(); }, []);

  const createMemory = async () => {
    if (!form.title.trim()) { setMessage({ kind: 'warn', text: 'Please enter a memory title.' }); return; }
    setBusy(true); setMessage(null);
    try {
      await requestJSON<Memory>('/admin/memories', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title: form.title, content: form.content, level: form.level, scope: form.scope, tags: splitTags(form.tags) }) });
      setForm(emptyForm()); setMessage({ kind: 'ok', text: 'Memory created. iWorker can read it by scope.' }); await loadMemories();
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to create memory' }); }
    finally { setBusy(false); }
  };

  const rows = useMemo(() => toRows(memories), [memories]);
  const summary = useMemo(() => {
    const active = memories.filter(memory => (memory.status || 'active') === 'active').length;
    const draft = memories.filter(memory => memory.status === 'draft').length;
    const autoExtracted = memories.filter(memory => (memory.tags || []).some(tag => ['auto', 'auto_extract'].includes(tag))).length;
    const levelCounts = memories.reduce<Record<string, number>>((acc, memory) => { if ((memory.status || 'active') !== 'active') return acc; const key = levelLabel(memory.level || memory.scope); acc[key] = (acc[key] || 0) + 1; return acc; }, {});
    return { active, draft, autoExtracted, levelCounts };
  }, [memories]);

  return <div className="center-page-stack">
    <SectionCard title="Knowledge Memory" desc="Store reusable company, department, role, and personal knowledge in local Center. Cloud does not read enterprise business content.">
      <div className="cloud-status-grid"><StatusTile label="Total" value={String(memories.length)} tone="ok" /><StatusTile label="Active" value={String(summary.active)} tone="ok" /><StatusTile label="Draft" value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} /></div>
      <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void loadMemories(); }} disabled={loading}>{loading ? 'Refreshing...' : 'Refresh memories'}</button><span className="cloud-inline-note">Source: {source} / Auto extracted {summary.autoExtracted}</span></div>
      {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
    </SectionCard>
    <SectionCard title="Add Memory" desc="Add rules, operating knowledge, or team agreements that iWorkers should reuse.">
      <div className="cloud-form-grid"><label className="cloud-field"><span>Title</span><input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} /></label><label className="cloud-field"><span>Level</span><select value={form.level} onChange={e => setForm({ ...form, level: e.target.value })}><option value="enterprise">Enterprise</option><option value="department">Department</option><option value="role">Role</option><option value="team">Team</option><option value="personal">Personal</option></select></label><label className="cloud-field"><span>Scope</span><input value={form.scope} onChange={e => setForm({ ...form, scope: e.target.value })} placeholder="all / office / data" /></label><label className="cloud-field"><span>Tags</span><input value={form.tags} onChange={e => setForm({ ...form, tags: e.target.value })} placeholder="comma or newline separated" /></label><label className="cloud-field cloud-field-wide"><span>Content</span><textarea rows={6} value={form.content} onChange={e => setForm({ ...form, content: e.target.value })} /></label></div>
      <div className="cloud-actions"><button className="cloud-primary" type="button" onClick={createMemory} disabled={busy}>{busy ? 'Creating...' : 'Create memory'}</button></div>
    </SectionCard>
    <SectionCard title="Memory List" desc={'Total ' + rows.length + ' memories.'}><DataTable columns={[{ key: 'topic', label: 'Topic' }, { key: 'scope', label: 'Scope' }, { key: 'level', label: 'Level' }, { key: 'tags', label: 'Tags' }, { key: 'updated', label: 'Updated' }, { key: 'status', label: 'Status' }]} rows={rows} />{rows.length === 0 && <p className="cloud-inline-note">No memories yet.</p>}</SectionCard>
    <SectionCard title="Memory Distribution" desc="Active memory count by level."><div className="item-list">{Object.entries(summary.levelCounts).map(([level, count]) => <div key={level} className="item-row"><strong>{level}</strong><p>{count} memories</p><span className="badge info">{level}</span></div>)}{Object.keys(summary.levelCounts).length === 0 && <p className="cloud-inline-note">No active memories.</p>}</div></SectionCard>
  </div>;
}
function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) { return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>; }
