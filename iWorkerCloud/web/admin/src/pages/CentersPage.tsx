import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listCenters, confirmTrial, confirmManual, disableCenter, enableCenter, deleteCenter, type Center } from '../api/centers';

export function CentersPage() {
  const { t } = useTranslation();
  const [centers, setCenters] = useState<Center[]>([]);
  const load = () => { listCenters().then(d => setCenters(d ?? [])).catch(() => {}); };
  useEffect(load, []);

  const handleConfirmManual = async (id: string) => {
    const days = prompt('Days (0=long-term):', '30');
    if (days === null) return;
    await confirmManual(id, ['compute'], parseInt(days) || 30);
    load();
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete?')) return;
    await deleteCenter(id);
    load();
  };

  if (centers.length === 0) return <div className="hint">{t('centers.empty')}</div>;

  return (
    <div className="list">
      {centers.map(c => (
        <div key={c.id} className="item">
          <div className="item-head">
            <span className="item-title">{c.company_name}</span>
            <span className={`badge ${c.status === 'active' ? 'ok' : c.status === 'pending' ? 'warn' : 'danger'}`}>
              {t(`centers.${c.status}`)}
            </span>
          </div>
          <div className="item-meta">
            ID: {c.id} | {c.admin_email}
            {c.created_at && <> | {new Date(c.created_at).toLocaleString()}</>}
          </div>
          <div className="actions">
            {c.status === 'pending' && <>
              <button className="btn-primary" onClick={() => { confirmTrial(c.id).then(load); }}>{t('centers.confirmTrial')}</button>
              <button className="btn-secondary" onClick={() => handleConfirmManual(c.id)}>{t('centers.confirmManual')}</button>
            </>}
            {c.status === 'active' && <button className="btn-danger" onClick={() => { disableCenter(c.id).then(load); }}>{t('centers.disable')}</button>}
            {c.status === 'disabled' && <button className="btn-secondary" onClick={() => { enableCenter(c.id).then(load); }}>{t('centers.enable')}</button>}
            <button className="btn-danger" onClick={() => handleDelete(c.id)}>{t('centers.delete')}</button>
          </div>
        </div>
      ))}
    </div>
  );
}
