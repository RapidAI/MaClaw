import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type AuditStats = {
  total_requests: number;
  ok_count: number;
  error_count: number;
  avg_latency_ms: number;
  top_provider: string;
  top_work_type: string;
};

type UsageRow = { metric: string; value: string; detail: string; scope: string };
const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
const emptyStats: AuditStats = { total_requests: 0, ok_count: 0, error_count: 0, avg_latency_ms: 0, top_provider: '', top_work_type: '' };

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { const resp = await fetch(url); if (!resp.ok) return null; return resp.json(); } catch { return null; }
}

const rowsFromStats = (stats: AuditStats): UsageRow[] => {
  const successRate = stats.total_requests > 0 ? ((stats.ok_count / stats.total_requests) * 100).toFixed(1) + '%' : '-';
  return [
    { metric: '调用量', value: String(stats.total_requests), detail: '成功 ' + stats.ok_count, scope: '最近 24 小时' },
    { metric: '成功率', value: successRate, detail: '失败 ' + stats.error_count, scope: '最近 24 小时' },
    { metric: '平均响应耗时', value: stats.avg_latency_ms ? stats.avg_latency_ms + 'ms' : '-', detail: stats.top_provider ? '最常用提供商：' + stats.top_provider : '-', scope: '核心任务' },
  ];
};

export function UsagePage() {
  const [stats, setStats] = useState<AuditStats>(emptyStats);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState('Center API');
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await fetchJSON<AuditStats>('/admin/audit/stats?hours=24');
      if (data) { setStats({ ...emptyStats, ...data }); setSource('Center API'); return; }
      if (hasWails()) {
        const localStats = await (window as any).go.main.App.GetAuditStats(24);
        if (localStats) { setStats({ ...emptyStats, ...localStats }); setSource('本地运行时'); return; }
      }
      setStats(emptyStats);
      setSource('无数据');
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载使用情况失败');
    } finally { setLoading(false); }
  };

  useEffect(() => { void load(); }, []);
  const rows = useMemo(() => rowsFromStats(stats), [stats]);
  const successRate = stats.total_requests > 0 ? Math.round((stats.ok_count / stats.total_requests) * 100) : 0;

  return (
    <div className="center-page-stack">
      <SectionCard title="使用情况" desc="查看最近 24 小时的模型调用、错误和延迟，用来判断 Center 与 iWorker 的运行负载。">
        <div className="cloud-status-grid">
          <StatusTile label="调用量" value={String(stats.total_requests)} tone="ok" />
          <StatusTile label="成功率" value={stats.total_requests ? successRate + '%' : '-'} tone={stats.error_count ? 'warn' : 'ok'} />
          <StatusTile label="平均耗时" value={stats.avg_latency_ms ? stats.avg_latency_ms + 'ms' : '-'} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? '刷新中...' : '刷新统计'}</button><span className="cloud-inline-note">数据来源：{source}</span></div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      <SectionCard title="调用统计" desc="LLM 代理调用统计。">
        <DataTable columns={[{ key: 'metric', label: '指标' }, { key: 'value', label: '当前值' }, { key: 'detail', label: '详情' }, { key: 'scope', label: '统计范围' }]} rows={rows} />
      </SectionCard>

      {(stats.top_provider || stats.top_work_type) && (
        <SectionCard title="热点观察" desc="最近 24 小时的高频使用模式。">
          <div className="item-list">
            {stats.top_provider && <div className="item-row"><strong>最常用提供商：{stats.top_provider}</strong><p>该提供商在最近 24 小时内被调用最多。</p><span className="badge info">热点</span></div>}
            {stats.top_work_type && <div className="item-row"><strong>最常见任务类型：{stats.top_work_type}</strong><p>该任务类型在最近 24 小时内出现最多。</p><span className="badge info">热点</span></div>}
          </div>
        </SectionCard>
      )}
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
