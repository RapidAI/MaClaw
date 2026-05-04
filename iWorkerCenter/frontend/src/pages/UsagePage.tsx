import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type AuditStats = { total_requests: number; ok_count: number; error_count: number; avg_latency_ms: number; top_provider: string; top_work_type: string };
type UsageRow = { metric: string; value: string; detail: string; scope: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';
const emptyStats: AuditStats = { total_requests: 0, ok_count: 0, error_count: 0, avg_latency_ms: 0, top_provider: '', top_work_type: '' };

async function fetchJSON<T>(url: string): Promise<T | null> { try { const resp = await fetch(url); if (!resp.ok) return null; return resp.json(); } catch { return null; } }

export function UsagePage() {
  const { t } = useI18n();
  const [stats, setStats] = useState<AuditStats>(emptyStats);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState('Center API');
  const [error, setError] = useState('');

  const rowsFromStats = (value: AuditStats): UsageRow[] => {
    const successRate = value.total_requests > 0 ? ((value.ok_count / value.total_requests) * 100).toFixed(1) + '%' : '-';
    return [
      { metric: t('调用量', 'Calls'), value: String(value.total_requests), detail: t('成功 ', 'Succeeded ') + value.ok_count, scope: t('最近 24 小时', 'Last 24 hours') },
      { metric: t('成功率', 'Success rate'), value: successRate, detail: t('失败 ', 'Failed ') + value.error_count, scope: t('最近 24 小时', 'Last 24 hours') },
      { metric: t('平均响应耗时', 'Average latency'), value: value.avg_latency_ms ? value.avg_latency_ms + 'ms' : '-', detail: value.top_provider ? t('最常用提供商：', 'Top provider: ') + value.top_provider : '-', scope: t('核心任务', 'Core work') },
    ];
  };

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await fetchJSON<AuditStats>('/admin/audit/stats?hours=24');
      if (data) { setStats({ ...emptyStats, ...data }); setSource('Center API'); return; }
      if (hasWails()) {
        const localStats = await (window as any).go.main.App.GetAuditStats(24);
        if (localStats) { setStats({ ...emptyStats, ...localStats }); setSource(t('本地运行时', 'Local runtime')); return; }
      }
      setStats(emptyStats);
      setSource(t('无数据', 'No data'));
    } catch (err) { setError(err instanceof Error ? err.message : t('加载使用情况失败。', 'Failed to load usage.')); }
    finally { setLoading(false); }
  };

  useEffect(() => { void load(); }, []);
  const rows = useMemo(() => rowsFromStats(stats), [stats, t]);
  const successRate = stats.total_requests > 0 ? Math.round((stats.ok_count / stats.total_requests) * 100) : 0;

  return (
    <div className="center-page-stack">
      <SectionCard title={t('使用情况', 'Usage')} desc={t('查看最近 24 小时的模型调用、错误和延迟，用来判断 Center 与 iWorker 的运行负载。', 'Review model calls, errors, and latency from the last 24 hours to understand Center and iWorker workload.')}>
        <div className="cloud-status-grid">
          <StatusTile label={t('调用量', 'Calls')} value={String(stats.total_requests)} tone="ok" />
          <StatusTile label={t('成功率', 'Success rate')} value={stats.total_requests ? successRate + '%' : '-'} tone={stats.error_count ? 'warn' : 'ok'} />
          <StatusTile label={t('平均耗时', 'Average latency')} value={stats.avg_latency_ms ? stats.avg_latency_ms + 'ms' : '-'} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新统计', 'Refresh statistics')}</button><span className="cloud-inline-note">{t('数据来源：', 'Data source: ')}{source}</span></div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      <SectionCard title={t('调用统计', 'Call Statistics')} desc={t('LLM 代理调用统计。', 'LLM proxy call statistics.')}>
        <DataTable columns={[{ key: 'metric', label: t('指标', 'Metric') }, { key: 'value', label: t('当前值', 'Value') }, { key: 'detail', label: t('详情', 'Detail') }, { key: 'scope', label: t('统计范围', 'Scope') }]} rows={rows} />
      </SectionCard>

      {(stats.top_provider || stats.top_work_type) && (
        <SectionCard title={t('热点观察', 'Hotspots')} desc={t('最近 24 小时的高频使用模式。', 'High-frequency usage patterns from the last 24 hours.')}>
          <div className="item-list">
            {stats.top_provider && <div className="item-row"><strong>{t('最常用提供商：', 'Top provider: ')}{stats.top_provider}</strong><p>{t('该提供商在最近 24 小时内被调用最多。', 'This provider was used most often in the last 24 hours.')}</p><span className="badge info">{t('热点', 'Hotspot')}</span></div>}
            {stats.top_work_type && <div className="item-row"><strong>{t('最常见任务类型：', 'Top work type: ')}{stats.top_work_type}</strong><p>{t('该任务类型在最近 24 小时内出现最多。', 'This task type appeared most often in the last 24 hours.')}</p><span className="badge info">{t('热点', 'Hotspot')}</span></div>}
          </div>
        </SectionCard>
      )}
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) { return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>; }
