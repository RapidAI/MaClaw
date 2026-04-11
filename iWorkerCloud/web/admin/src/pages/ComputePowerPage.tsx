import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listProviders, listCenterPermissions, toggleCenterPermission, testProvider, type LLMProvider, type CenterPermission } from '../api/compute';

export function ComputePowerPage() {
  const { t } = useTranslation();
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [permissions, setPermissions] = useState<CenterPermission[]>([]);

  const load = () => {
    listProviders().then(setProviders).catch(() => {});
    listCenterPermissions().then(setPermissions).catch(() => {});
  };
  useEffect(load, []);

  const handleTest = async (id: string) => {
    const r = await testProvider(id).catch(() => ({ ok: false, latency_ms: 0, error: 'failed' }));
    alert(r.ok ? `OK (${r.latency_ms}ms)` : `Failed: ${r.error}`);
  };

  const handleToggle = async (centerId: string, current: boolean) => {
    await toggleCenterPermission(centerId, !current).catch(() => {});
    load();
  };

  return (
    <div>
      {/* Provider management */}
      <div className="head">
        <h3>{t('compute.providers')}</h3>
        <button className="btn-ghost" onClick={load}>{t('common.refresh')}</button>
      </div>

      {providers.length === 0 ? (
        <div className="hint">{t('common.noData')}</div>
      ) : (
        <div className="list" style={{ marginBottom: 24 }}>
          {providers.map(p => (
            <div key={p.id} className="item">
              <div className="item-head">
                <span className="item-title">{p.name}</span>
                <span className={`badge ${p.enabled ? 'ok' : 'danger'}`}>
                  {p.enabled ? t('compute.enabled') : t('compute.disabled')}
                </span>
              </div>
              <div className="item-meta">
                {p.protocol} | {p.compute_type} | {p.model} | UA: {p.user_agent || '-'}
                {p.input_price_per_mtoken != null && <> | ¥{p.input_price_per_mtoken}/{p.output_price_per_mtoken} per MToken</>}
              </div>
              <div className="actions">
                <button className="btn-ghost" onClick={() => handleTest(p.id)}>{t('compute.test')}</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Center permissions */}
      <div className="head">
        <h3>{t('compute.centerPermissions')}</h3>
      </div>

      {permissions.length === 0 ? (
        <div className="hint">{t('common.noData')}</div>
      ) : (
        <div className="list">
          {permissions.map(cp => (
            <div key={cp.center_id} className="item">
              <div className="item-head">
                <span className="item-title">{cp.company_name}</span>
                <span className={`badge ${cp.compute_permission ? 'ok' : 'info'}`}>
                  {cp.compute_permission ? t('compute.selfManaged') : t('compute.cloudManaged')}
                </span>
              </div>
              <div className="actions">
                <button
                  className={cp.compute_permission ? 'btn-danger' : 'btn-primary'}
                  onClick={() => handleToggle(cp.center_id, cp.compute_permission)}
                >
                  {cp.compute_permission ? t('compute.revokePermission') : t('compute.grantPermission')}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
