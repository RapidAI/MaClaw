import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';

type ConfigBundle = { id: string; version: number; content_type: string; payload?: string; status: string; note: string; created_at: string; published_at: string };
type ApplyRecord = { bundle_id: string; version: number; worker_id: string; department_id: string; status: string; message: string; applied_at: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };
type BundleForm = { content_type: string; note: string; payload: string };

const defaultPayload = () => JSON.stringify({ source: 'iworkercenter', includes: ['model-routing', 'security', 'capabilities', 'im-gateway'], local_continuity: true }, null, 2);
const defaultForm = (note = 'Center configuration delivery bundle'): BundleForm => ({ content_type: 'full', note, payload: defaultPayload() });

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  return data as T;
}

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

const workerCountByStatus = (records: ApplyRecord[], status: string) => new Set(records.filter(row => row.status === status).map(row => row.worker_id).filter(Boolean)).size;
const successWorkerCount = (records: ApplyRecord[]) => workerCountByStatus(records, 'success');

export function DeliveryPage() {
  const { t } = useI18n();
  const [bundles, setBundles] = useState<ConfigBundle[]>([]);
  const [applyRecords, setApplyRecords] = useState<ApplyRecord[]>([]);
  const [form, setForm] = useState<BundleForm>(() => defaultForm(t('Center 配置下发包', 'Center configuration delivery bundle')));
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const statusLabel = (status: string) => ({ draft: t('草稿', 'Draft'), published: t('已发布', 'Published'), failed: t('失败', 'Failed') }[status] || status || t('未知', 'Unknown'));
  const contentTypeLabel = (type: string) => ({ full: t('全量配置', 'Full config'), incremental: t('增量配置', 'Incremental config'), skills: t('Skill/MCP 能力', 'Skill/MCP capabilities'), security: t('安全策略', 'Security policy'), models: t('模型路由', 'Model routing') }[type] || type || t('配置包', 'Config bundle'));

  const loadBundles = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const data = await requestJSON<{ bundles: ConfigBundle[]; apply_records?: ApplyRecord[] }>('/admin/config-bundles');
      setBundles(data.bundles || []);
      setApplyRecords(data.apply_records || []);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载下发配置失败。', 'Failed to load delivery bundles.') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadBundles(); }, []);

  const summary = useMemo(() => {
    const published = bundles.filter(row => row.status === 'published').length;
    const draft = bundles.filter(row => row.status === 'draft').length;
    const latest = bundles.find(row => row.status === 'published');
    const latestRecords = latest ? applyRecords.filter(row => row.bundle_id === latest.id) : [];
    return { published, draft, latestVersion: latest ? 'v' + latest.version : '-', synced: successWorkerCount(latestRecords) };
  }, [bundles, applyRecords]);

  const applyRecordsByBundle = useMemo(() => {
    const grouped = new Map<string, ApplyRecord[]>();
    for (const record of applyRecords) {
      const list = grouped.get(record.bundle_id) || [];
      list.push(record);
      grouped.set(record.bundle_id, list);
    }
    return grouped;
  }, [applyRecords]);

  const createBundle = async () => {
    let payload: unknown;
    try {
      payload = form.payload.trim() ? JSON.parse(form.payload) : {};
    } catch {
      setMessage({ kind: 'warn', text: t('Payload 必须是合法 JSON。', 'Payload must be valid JSON.') });
      return;
    }
    setBusy('create');
    setMessage(null);
    try {
      const created = await requestJSON<ConfigBundle>('/admin/config-bundles', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content_type: form.content_type, note: form.note, payload }),
      });
      setForm(defaultForm(t('Center 配置下发包', 'Center configuration delivery bundle')));
      setMessage({ kind: 'ok', text: t('配置包草稿已创建：v', 'Config bundle draft created: v') + created.version });
      await loadBundles();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('创建配置包失败。', 'Failed to create config bundle.') });
    } finally {
      setBusy('');
    }
  };

  const publishBundle = async (bundle: ConfigBundle) => {
    setBusy('publish:' + bundle.id);
    setMessage(null);
    try {
      await requestJSON('/admin/config-bundles/' + bundle.id + '/publish', { method: 'POST' });
      setMessage({ kind: 'ok', text: t('已发布配置包 v', 'Published config bundle v') + bundle.version + t('。iWorker 下次同步会获取该版本。', '. iWorkers will fetch this version on the next sync.') });
      await loadBundles();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('发布配置包失败。', 'Failed to publish config bundle.') });
    } finally {
      setBusy('');
    }
  };

  return <div className="center-page-stack">
    <SectionCard title={t('下发管理', 'Delivery Management')} desc={t('Center 将企业配置、模型路由、安全策略、Skill 和 MCP 下发到 iWorker。Cloud 失联时，已发布配置仍可由 Center 和本地 iWorker 继续使用。', 'Center delivers enterprise configuration, model routing, security policy, Skill and MCP packages to iWorkers. If Cloud is unavailable, published configuration still works through Center and local iWorkers.')}>
      <div className="cloud-status-grid"><StatusTile label={t('配置包', 'Bundles')} value={String(bundles.length)} tone="ok" /><StatusTile label={t('已发布', 'Published')} value={String(summary.published)} tone="ok" /><StatusTile label={t('草稿/待发布', 'Drafts')} value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} /><StatusTile label={t('最新发布', 'Latest')} value={summary.latestVersion} /><StatusTile label={t('已同步 iWorker', 'Synced iWorkers')} value={String(summary.synced)} tone="ok" /></div>
      <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void loadBundles(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新下发状态', 'Refresh delivery status')}</button><span className="cloud-inline-note">{t('客户端接口：', 'Client APIs: ')}/client/config/version, /client/config/latest, /client/config/apply-result</span></div>
      {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
    </SectionCard>
    <SectionCard title={t('创建配置包草稿', 'Create Config Bundle Draft')} desc={t('配置包以 JSON 形式保存。建议把模型路由、安全策略、能力包/MCP 和 IM 网关变更合并成一个版本发布。', 'Bundles are saved as JSON. Combine model routing, security policy, capability/MCP, and IM gateway changes into one version when possible.')}>
      <div className="cloud-form-grid"><label className="cloud-field"><span>{t('配置类型', 'Content type')}</span><select value={form.content_type} onChange={e => setForm({ ...form, content_type: e.target.value })}><option value="full">{t('全量配置', 'Full config')}</option><option value="incremental">{t('增量配置', 'Incremental config')}</option><option value="skills">Skill/MCP</option><option value="security">{t('安全策略', 'Security')}</option><option value="models">{t('模型路由', 'Models')}</option></select></label><label className="cloud-field"><span>{t('备注', 'Note')}</span><input value={form.note} onChange={e => setForm({ ...form, note: e.target.value })} /></label><label className="cloud-field cloud-field-wide"><span>Payload JSON</span><textarea rows={10} value={form.payload} onChange={e => setForm({ ...form, payload: e.target.value })} /></label></div>
      <div className="cloud-actions"><button className="cloud-primary" type="button" onClick={createBundle} disabled={busy === 'create'}>{busy === 'create' ? t('创建中...', 'Creating...') : t('创建草稿', 'Create draft')}</button></div>
    </SectionCard>
    <SectionCard title={t('配置下发记录', 'Delivery Records')} desc={t('共 ' + bundles.length + ' 个配置包。草稿发布后才会被 iWorker 客户端获取。', 'Total ' + bundles.length + ' bundles. Drafts are visible to iWorker clients only after publishing.')}>
      <div className="data-table-wrap"><table className="data-table"><thead><tr><th>{t('版本', 'Version')}</th><th>{t('类型', 'Type')}</th><th>{t('备注', 'Note')}</th><th>{t('状态', 'Status')}</th><th>{t('创建时间', 'Created')}</th><th>{t('发布时间', 'Published')}</th><th>{t('同步状态', 'Sync')}</th><th>{t('操作', 'Actions')}</th></tr></thead><tbody>{bundles.map(bundle => { const isBusy = busy === 'publish:' + bundle.id; const records = applyRecordsByBundle.get(bundle.id) || []; const latestRecord = records[0]; const syncedWorkers = successWorkerCount(records); return <tr key={bundle.id}><td>v{bundle.version}</td><td>{contentTypeLabel(bundle.content_type)}</td><td>{bundle.note || '-'}</td><td><span className={bundle.status === 'published' ? 'badge ok' : 'badge warn'}>{statusLabel(bundle.status)}</span></td><td>{formatTime(bundle.created_at)}</td><td>{formatTime(bundle.published_at)}</td><td>{records.length ? <span className="cloud-inline-note">{syncedWorkers} {t('成功', 'success')} / {workerCountByStatus(records, 'failed')} {t('失败', 'failed')} / {workerCountByStatus(records, 'skipped')} {t('跳过', 'skipped')} / {latestRecord?.status || '-'} / {formatTime(latestRecord?.applied_at)}</span> : <span className="cloud-inline-note">{t('尚未同步', 'Not synced')}</span>}</td><td>{bundle.status === 'draft' ? <button className="btn-secondary" type="button" onClick={() => publishBundle(bundle)} disabled={isBusy}>{isBusy ? t('发布中...', 'Publishing...') : t('发布', 'Publish')}</button> : <span className="cloud-inline-note">{t('已发布', 'Published')}</span>}</td></tr>; })}{bundles.length === 0 && <tr><td colSpan={8} style={{ textAlign: 'center', color: 'var(--muted)' }}>{t('暂无配置包。请先创建草稿。', 'No bundles yet. Create a draft first.')}</td></tr>}</tbody></table></div>
    </SectionCard>
  </div>;
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
