import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import {
  getComputeStatus, listComputeProviders, testComputeProvider,
  syncFromCloud, switchComputeSource,
  type ComputeProvider, type ComputeStatus,
} from '../api/compute';

export function ComputePowerPage() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<ComputeStatus | null>(null);
  const [providers, setProviders] = useState<ComputeProvider[]>([]);
  const [loading, setLoading] = useState(true);

  const load = () => {
    setLoading(true);
    Promise.all([
      getComputeStatus().catch(() => null),
      listComputeProviders().catch(() => []),
    ]).then(([s, p]) => {
      setStatus(s);
      setProviders(p);
    }).finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleSync = async () => {
    await syncFromCloud().catch(() => {});
    load();
  };

  const handleSwitchSource = async (source: 'cloud' | 'local') => {
    await switchComputeSource(source).catch(() => {});
    load();
  };

  const handleTest = async (id: string) => {
    const result = await testComputeProvider(id).catch(() => ({ ok: false, latency_ms: 0, error: 'failed' }));
    alert(result.ok ? `OK (${result.latency_ms}ms)` : `Failed: ${result.error}`);
  };

  const isCloud = status?.compute_source === 'cloud';
  const canLocal = status?.compute_permission === true;

  const rows = providers.map(p => ({
    name: p.name,
    protocol: p.protocol,
    compute_type: p.compute_type || '-',
    user_agent: p.user_agent || '-',
    model: p.model,
    enabled: p.enabled ? '✓' : '✗',
    source: isCloud ? (t('compute.fromCloud')) : (t('compute.local')),
  }));

  return (
    <div className="center-page-stack">
      {/* Status bar */}
      <SectionCard title={t('compute.title')} desc={loading ? t('common.loading') : undefined}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 16 }}>
          <span className={`badge ${isCloud ? 'info' : 'ok'}`}>
            {isCloud ? t('compute.modeCloud') : t('compute.modeLocal')}
          </span>
          {status?.last_sync_at && (
            <span style={{ fontSize: 13, color: '#5f7692' }}>
              {t('compute.lastSync')}: {new Date(status.last_sync_at).toLocaleString()}
            </span>
          )}
          {isCloud && (
            <button className="btn-ghost" onClick={handleSync} style={{ height: 32, fontSize: 13 }}>
              {t('compute.syncNow')}
            </button>
          )}
          {canLocal && isCloud && (
            <button className="btn-ghost" onClick={() => handleSwitchSource('local')} style={{ height: 32, fontSize: 13 }}>
              {t('compute.switchLocal')}
            </button>
          )}
          {!isCloud && (
            <button className="btn-ghost" onClick={() => handleSwitchSource('cloud')} style={{ height: 32, fontSize: 13 }}>
              {t('compute.switchCloud')}
            </button>
          )}
          {!canLocal && !isCloud && (
            <span style={{ fontSize: 13, color: '#b98219' }}>{t('compute.noPermission')}</span>
          )}
        </div>

        <DataTable
          columns={[
            { key: 'name', label: t('compute.providerName') },
            { key: 'protocol', label: t('compute.protocol') },
            { key: 'compute_type', label: t('compute.computeType') },
            { key: 'model', label: t('compute.model') },
            { key: 'user_agent', label: 'User-Agent' },
            { key: 'enabled', label: t('compute.enabled') },
            { key: 'source', label: t('compute.source') },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
