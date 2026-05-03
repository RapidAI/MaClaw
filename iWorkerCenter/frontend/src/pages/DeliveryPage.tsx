import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

type ConfigBundle = {
  id: string;
  version: number;
  content_type: string;
  payload?: string;
  status: string;
  note: string;
  created_at: string;
  published_at: string;
};

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

type BundleForm = {
  content_type: string;
  note: string;
  payload: string;
};

const defaultForm = (): BundleForm => ({
  content_type: 'full',
  note: 'Center 配置下发包',
  payload: JSON.stringify({
    source: 'iworkercenter',
    includes: ['model-routing', 'security', 'capabilities', 'im-gateway'],
    local_continuity: true,
  }, null, 2),
});

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || '请求失败: ' + resp.status);
  return data as T;
}

const statusLabel = (status: string) => {
  switch (status) {
    case 'draft': return '草稿';
    case 'published': return '已发布';
    case 'failed': return '失败';
    default: return status || '未知';
  }
};

const contentTypeLabel = (type: string) => {
  switch (type) {
    case 'full': return '全量配置';
    case 'incremental': return '增量配置';
    case 'skills': return 'Skill/MCP 能力';
    case 'security': return '安全策略';
    case 'models': return '模型路由';
    default: return type || '配置包';
  }
};

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

export function DeliveryPage() {
  const [bundles, setBundles] = useState<ConfigBundle[]>([]);
  const [form, setForm] = useState<BundleForm>(defaultForm());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const loadBundles = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const data = await requestJSON<{ bundles: ConfigBundle[] }>('/admin/config-bundles');
      setBundles(data.bundles || []);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '加载下发配置失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadBundles(); }, []);

  const summary = useMemo(() => {
    const published = bundles.filter(row => row.status === 'published').length;
    const draft = bundles.filter(row => row.status === 'draft').length;
    const latest = bundles.find(row => row.status === 'published');
    return { published, draft, latestVersion: latest ? 'v' + latest.version : '-' };
  }, [bundles]);

  const createBundle = async () => {
    let payload: unknown;
    try {
      payload = form.payload.trim() ? JSON.parse(form.payload) : {};
    } catch {
      setMessage({ kind: 'warn', text: 'Payload 必须是合法 JSON。' });
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
      setForm(defaultForm());
      setMessage({ kind: 'ok', text: '配置包草稿已创建：v' + created.version });
      await loadBundles();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '创建配置包失败' });
    } finally {
      setBusy('');
    }
  };

  const publishBundle = async (bundle: ConfigBundle) => {
    setBusy('publish:' + bundle.id);
    setMessage(null);
    try {
      await requestJSON('/admin/config-bundles/' + bundle.id + '/publish', { method: 'POST' });
      setMessage({ kind: 'ok', text: '已发布配置包 v' + bundle.version + '。iWorker 下次同步会获取该版本。' });
      await loadBundles();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '发布配置包失败' });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="下发管理" desc="Center 将企业配置、模型路由、安全策略、Skill 和 MCP 下发到 iWorker。Cloud 失联时，已发布配置仍可由 Center 和本地 iWorker 继续使用。">
        <div className="cloud-status-grid">
          <StatusTile label="配置包" value={String(bundles.length)} tone="ok" />
          <StatusTile label="已发布" value={String(summary.published)} tone="ok" />
          <StatusTile label="草稿/待发布" value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} />
          <StatusTile label="最新发布" value={summary.latestVersion} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void loadBundles(); }} disabled={loading}>{loading ? '刷新中...' : '刷新下发状态'}</button>
          <span className="cloud-inline-note">客户端接口：/client/config/version 和 /client/config/latest</span>
        </div>
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard title="创建配置包草稿" desc="配置包以 JSON 形式保存。建议把模型路由、安全策略、能力包/MCP 和 IM 网关变更合并成一个版本发布。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>配置类型</span><select value={form.content_type} onChange={e => setForm({ ...form, content_type: e.target.value })}><option value="full">全量配置</option><option value="incremental">增量配置</option><option value="skills">Skill/MCP 能力</option><option value="security">安全策略</option><option value="models">模型路由</option></select></label>
          <label className="cloud-field"><span>备注</span><input value={form.note} onChange={e => setForm({ ...form, note: e.target.value })} /></label>
          <label className="cloud-field cloud-field-wide"><span>Payload JSON</span><textarea rows={10} value={form.payload} onChange={e => setForm({ ...form, payload: e.target.value })} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" onClick={createBundle} disabled={busy === 'create'}>{busy === 'create' ? '创建中...' : '创建草稿'}</button></div>
      </SectionCard>

      <SectionCard title="配置下发记录" desc={'共 ' + bundles.length + ' 个配置包。草稿发布后才会被 iWorker 客户端获取。'}>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr><th>版本</th><th>类型</th><th>备注</th><th>状态</th><th>创建时间</th><th>发布时间</th><th>操作</th></tr></thead>
            <tbody>
              {bundles.map(bundle => {
                const isBusy = busy === 'publish:' + bundle.id;
                return (
                  <tr key={bundle.id}>
                    <td>v{bundle.version}</td>
                    <td>{contentTypeLabel(bundle.content_type)}</td>
                    <td>{bundle.note || '-'}</td>
                    <td><span className={bundle.status === 'published' ? 'badge ok' : 'badge warn'}>{statusLabel(bundle.status)}</span></td>
                    <td>{formatTime(bundle.created_at)}</td>
                    <td>{formatTime(bundle.published_at)}</td>
                    <td>{bundle.status === 'draft' ? <button className="btn-secondary" type="button" onClick={() => publishBundle(bundle)} disabled={isBusy}>{isBusy ? '发布中...' : '发布'}</button> : <span className="cloud-inline-note">已发布</span>}</td>
                  </tr>
                );
              })}
              {bundles.length === 0 && <tr><td colSpan={7} style={{ textAlign: 'center', color: 'var(--muted)' }}>暂无配置包。请先创建草稿。</td></tr>}
            </tbody>
          </table>
        </div>
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
