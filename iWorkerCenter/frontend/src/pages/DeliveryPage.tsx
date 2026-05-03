import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type ConfigBundle = {
  id: string;
  version: number;
  content_type: string;
  status: string;
  note: string;
  created_at: string;
  published_at: string;
};

type DeliveryRow = {
  version: string;
  content: string;
  note: string;
  status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

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
    default: return type || '配置包';
  }
};

const defaultRows: DeliveryRow[] = [
  { version: 'v1', content: '全量配置', note: '初始配置包', status: '已发布' },
];

export function DeliveryPage() {
  const [rows, setRows] = useState<DeliveryRow[]>(defaultRows);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const loadBundles = async () => {
    if (!hasWails()) return;
    setLoading(true);
    setError('');
    try {
      const bundles = await (window as any).go.main.App.ListConfigBundles();
      if (Array.isArray(bundles) && bundles.length > 0) {
        setRows(bundles.map((bundle: ConfigBundle) => ({
          version: 'v' + bundle.version,
          content: contentTypeLabel(bundle.content_type),
          note: bundle.note || '-',
          status: statusLabel(bundle.status),
        })));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载下发配置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadBundles(); }, []);

  const publishedCount = useMemo(() => rows.filter(row => row.status === '已发布').length, [rows]);
  const draftCount = rows.length - publishedCount;

  return (
    <div className="center-page-stack">
      <SectionCard title="下发管理" desc="Center 将企业配置、模型路由、Skill 和 MCP 下发到 iWorker。Cloud 失联时，已发布配置仍可由 Center 和本地 iWorker 继续使用。">
        <div className="cloud-status-grid">
          <StatusTile label="配置包" value={String(rows.length)} tone="ok" />
          <StatusTile label="已发布" value={String(publishedCount)} tone="ok" />
          <StatusTile label="草稿/待发布" value={String(draftCount)} tone={draftCount ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void loadBundles(); }} disabled={loading}>{loading ? '刷新中' : '刷新下发状态'}</button>
        </div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      <SectionCard title="配置下发记录" desc={'共 ' + rows.length + ' 个配置包。'}>
        <DataTable
          columns={[
            { key: 'version', label: '版本' },
            { key: 'content', label: '类型' },
            { key: 'note', label: '备注' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={'cloud-status-tile ' + (tone || '')}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
