import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type Memory = { id: string; title?: string; content?: string; level?: string; scope?: string; tags?: string[]; version?: number; status?: string; created_at?: string; updated_at?: string };
type MemoryRow = { topic: string; scope: string; level: string; tags: string; updated: string; status: string };
type MemoryForm = { title: string; content: string; level: string; scope: string; tags: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
const emptyForm = (): MemoryForm => ({ title: '', content: '', level: 'enterprise', scope: 'all', tags: '' });

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> { const resp = await fetch(url, init); const text = await resp.text(); const data = text ? JSON.parse(text) : null; if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status); return data as T; }
async function fetchJSON<T>(url: string): Promise<T | null> { try { return await requestJSON<T>(url); } catch { return null; } }
const splitTags = (value: string) => value.replaceAll(String.fromCharCode(13), '').replaceAll(String.fromCharCode(10), ',').split(',').map(tag => tag.trim()).filter(Boolean);

export function KnowledgePage() {
  const { t } = useI18n();
  const [memories, setMemories] = useState<Memory[]>([]);
  const [form, setForm] = useState<MemoryForm>(emptyForm());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [source, setSource] = useState('Center API');
  const [message, setMessage] = useState<Message | null>(null);

  const levelLabel = (level?: string) => ({ enterprise: t('企业', 'Enterprise'), company: t('公司', 'Company'), department: t('部门', 'Department'), role: t('角色', 'Role'), team: t('团队', 'Team'), personal: t('个人', 'Personal') }[level || ''] || level || t('通用', 'General'));
  const statusLabel = (status?: string) => ({ active: t('启用', 'Active'), draft: t('草稿', 'Draft'), merged: t('已合并', 'Merged'), expired: t('已过期', 'Expired'), disabled: t('禁用', 'Disabled') }[status || ''] || status || t('启用', 'Active'));

  const loadMemories = async () => {
    setLoading(true); setMessage(null);
    try {
      const data = await fetchJSON<{ memories?: Memory[] }>('/admin/memories');
      if (Array.isArray(data?.memories)) { setMemories(data.memories); setSource('Center API'); return; }
      if (hasWails()) { const mems = await (window as any).go.main.App.ListMemories(); if (Array.isArray(mems)) { setMemories(mems); setSource(t('本地运行时', 'Local runtime')); return; } }
      setSource(t('无数据', 'No data'));
    } catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载记忆失败。', 'Failed to load memories.') }); }
    finally { setLoading(false); }
  };
  useEffect(() => { void loadMemories(); }, []);

  const createMemory = async () => {
    if (!form.title.trim()) { setMessage({ kind: 'warn', text: t('请输入记忆标题。', 'Please enter a memory title.') }); return; }
    setBusy(true); setMessage(null);
    try { await requestJSON<Memory>('/admin/memories', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title: form.title, content: form.content, level: form.level, scope: form.scope, tags: splitTags(form.tags) }) }); setForm(emptyForm()); setMessage({ kind: 'ok', text: t('记忆已创建，iWorker 可按范围读取。', 'Memory created. iWorkers can read it by scope.') }); await loadMemories(); }
    catch (err) { setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('创建记忆失败。', 'Failed to create memory.') }); }
    finally { setBusy(false); }
  };

  const rows = useMemo<MemoryRow[]>(() => memories.map(memory => ({ topic: memory.title || (memory.content || memory.id).slice(0, 40), scope: memory.scope || 'all', level: levelLabel(memory.level || memory.scope), tags: (memory.tags || []).join(', ') || '-', updated: memory.updated_at || memory.created_at || '-', status: statusLabel(memory.status) })), [memories, t]);
  const summary = useMemo(() => { const active = memories.filter(memory => (memory.status || 'active') === 'active').length; const draft = memories.filter(memory => memory.status === 'draft').length; const autoExtracted = memories.filter(memory => (memory.tags || []).some(tag => ['auto', 'auto_extract'].includes(tag))).length; const levelCounts = memories.reduce<Record<string, number>>((acc, memory) => { if ((memory.status || 'active') !== 'active') return acc; const key = levelLabel(memory.level || memory.scope); acc[key] = (acc[key] || 0) + 1; return acc; }, {}); return { active, draft, autoExtracted, levelCounts }; }, [memories, t]);

  return <div className="center-page-stack">
    <SectionCard title={t('知识记忆', 'Knowledge Memory')} desc={t('在本地 Center 存储可复用的公司、部门、角色和个人知识。Cloud 不读取企业业务内容。', 'Store reusable company, department, role, and personal knowledge in local Center. Cloud does not read enterprise business content.')}>
      <div className="cloud-status-grid"><StatusTile label={t('总数', 'Total')} value={String(memories.length)} tone="ok" /><StatusTile label={t('启用', 'Active')} value={String(summary.active)} tone="ok" /><StatusTile label={t('草稿', 'Draft')} value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} /></div>
      <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void loadMemories(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新记忆', 'Refresh memories')}</button><span className="cloud-inline-note">{t('来源：', 'Source: ')}{source} / {t('自动沉淀 ', 'Auto extracted ')}{summary.autoExtracted}</span></div>
      {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
    </SectionCard>
    <SectionCard title={t('添加记忆', 'Add Memory')} desc={t('添加 iWorker 应复用的规则、操作知识或团队约定。', 'Add rules, operating knowledge, or team agreements that iWorkers should reuse.')}>
      <div className="cloud-form-grid"><label className="cloud-field"><span>{t('标题', 'Title')}</span><input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} /></label><label className="cloud-field"><span>{t('层级', 'Level')}</span><select value={form.level} onChange={e => setForm({ ...form, level: e.target.value })}><option value="enterprise">{t('企业', 'Enterprise')}</option><option value="department">{t('部门', 'Department')}</option><option value="role">{t('角色', 'Role')}</option><option value="team">{t('团队', 'Team')}</option><option value="personal">{t('个人', 'Personal')}</option></select></label><label className="cloud-field"><span>{t('范围', 'Scope')}</span><input value={form.scope} onChange={e => setForm({ ...form, scope: e.target.value })} placeholder="all / office / data" /></label><label className="cloud-field"><span>{t('标签', 'Tags')}</span><input value={form.tags} onChange={e => setForm({ ...form, tags: e.target.value })} placeholder={t('逗号或换行分隔', 'comma or newline separated')} /></label><label className="cloud-field cloud-field-wide"><span>{t('内容', 'Content')}</span><textarea rows={6} value={form.content} onChange={e => setForm({ ...form, content: e.target.value })} /></label></div>
      <div className="cloud-actions"><button className="cloud-primary" type="button" onClick={createMemory} disabled={busy}>{busy ? t('创建中...', 'Creating...') : t('创建记忆', 'Create memory')}</button></div>
    </SectionCard>
    <SectionCard title={t('记忆列表', 'Memory List')} desc={t('共 ' + rows.length + ' 条记忆。', 'Total ' + rows.length + ' memories.')}><DataTable columns={[{ key: 'topic', label: t('主题', 'Topic') }, { key: 'scope', label: t('范围', 'Scope') }, { key: 'level', label: t('层级', 'Level') }, { key: 'tags', label: t('标签', 'Tags') }, { key: 'updated', label: t('更新时间', 'Updated') }, { key: 'status', label: t('状态', 'Status') }]} rows={rows} />{rows.length === 0 && <p className="cloud-inline-note">{t('暂无记忆。', 'No memories yet.')}</p>}</SectionCard>
    <SectionCard title={t('记忆分布', 'Memory Distribution')} desc={t('按层级统计启用记忆数量。', 'Active memory count by level.')}><div className="item-list">{Object.entries(summary.levelCounts).map(([level, count]) => <div key={level} className="item-row"><strong>{level}</strong><p>{t(count + ' 条记忆', count + ' memories')}</p><span className="badge info">{level}</span></div>)}{Object.keys(summary.levelCounts).length === 0 && <p className="cloud-inline-note">{t('暂无启用记忆。', 'No active memories.')}</p>}</div></SectionCard>
  </div>;
}
function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) { return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>; }
