import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Capability = {
  id: string;
  name: string;
  description: string;
  category: string;
  version: string;
  source: string;
  risk_level: string;
  status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

const statusLabel = (status: string) => {
  switch (status) {
    case 'active': return '已启用';
    case 'pending_review': return '待审核';
    case 'rejected': return '已拒绝';
    case 'disabled': return '已停用';
    default: return status || '未知';
  }
};

const riskLabel = (risk: string) => {
  switch (risk) {
    case 'low': return '低';
    case 'medium': return '中';
    case 'high': return '高';
    default: return risk || '低';
  }
};

const sourceLabel = (source?: string) => {
  if (!source) return '本地';
  if (source.startsWith('hubcenter:') || source.startsWith('cloud:')) return 'Cloud 市场';
  if (source.startsWith('center:')) return 'Center 管理';
  return '本地';
};

export function PackagesPage() {
  const [caps, setCaps] = useState<Capability[]>([]);
  const [pendingCaps, setPendingCaps] = useState<Capability[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const loadCaps = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await fetchJSON<{ capabilities: Capability[] }>('/admin/capabilities');
      if (data?.capabilities) {
        setCaps(data.capabilities.filter(c => c.status !== 'pending_review'));
        setPendingCaps(data.capabilities.filter(c => c.status === 'pending_review'));
        return;
      }
      if (!hasWails()) return;
      const list = await (window as any).go.main.App.ListCapabilities();
      if (Array.isArray(list)) {
        setCaps(list.filter((c: Capability) => c.status !== 'pending_review'));
        setPendingCaps(list.filter((c: Capability) => c.status === 'pending_review'));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载能力包失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadCaps(); }, []);

  const summary = useMemo(() => {
    const highRisk = [...caps, ...pendingCaps].filter(c => c.risk_level === 'high').length;
    const cloudItems = [...caps, ...pendingCaps].filter(c => sourceLabel(c.source) === 'Cloud 市场').length;
    return { active: caps.length, pending: pendingCaps.length, cloudItems, highRisk };
  }, [caps, pendingCaps]);

  const activeRows = caps.map(c => ({
    name: c.name,
    category: c.category || '通用',
    version: c.version || '-',
    source: sourceLabel(c.source),
    risk: riskLabel(c.risk_level),
    status: statusLabel(c.status),
  }));

  const pendingRows = pendingCaps.map(c => ({
    name: c.name,
    category: c.category || '通用',
    version: c.version || '-',
    source: sourceLabel(c.source),
    risk: riskLabel(c.risk_level),
    status: statusLabel(c.status),
  }));

  return (
    <div className="center-page-stack">
      <SectionCard title="能力包与 MCP" desc="iWorkerCenter 负责企业内技能/MCP 的安装、审核和下发；iWorkerCloud 只作为能力市场和授权来源，不参与企业业务执行。">
        <div className="cloud-status-grid">
          <StatusTile label="已启用" value={String(summary.active)} tone="ok" />
          <StatusTile label="待审核" value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} />
          <StatusTile label="Cloud 来源" value={String(summary.cloudItems)} />
          <StatusTile label="高风险" value={String(summary.highRisk)} tone={summary.highRisk ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void loadCaps(); }} disabled={loading}>{loading ? '刷新中' : '刷新能力包'}</button>
        </div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      {pendingCaps.length > 0 && (
        <SectionCard title="待审核能力包" desc={String(pendingCaps.length) + ' 个从 Cloud 市场或外部来源导入的能力包等待 Center 管理员审核。'}>
          <DataTable
            columns={[
              { key: 'name', label: '名称' },
              { key: 'category', label: '分类' },
              { key: 'version', label: '版本' },
              { key: 'source', label: '来源' },
              { key: 'risk', label: '风险等级' },
              { key: 'status', label: '状态' },
            ]}
            rows={pendingRows}
          />
        </SectionCard>
      )}

      <SectionCard title="已安装能力包" desc={'共 ' + caps.length + ' 个能力包。启用后可通过下发管理同步给对应 iWorker。'}>
        <DataTable
          columns={[
            { key: 'name', label: '名称' },
            { key: 'category', label: '分类' },
            { key: 'version', label: '版本' },
            { key: 'source', label: '来源' },
            { key: 'risk', label: '风险等级' },
            { key: 'status', label: '状态' },
          ]}
          rows={activeRows}
        />
        {caps.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无已启用能力包。可以先从 Cloud 市场导入，或在 Center 本地安装企业 MCP/Skill。</p>}
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
