import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listLicenses, issueLicense, revokeLicense, type License } from '../api/licenses';

export function LicensesPage() {
  const { t } = useTranslation();
  const [licenses, setLicenses] = useState<License[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [centerId, setCenterId] = useState('');
  const [days, setDays] = useState('30');
  const [modules, setModules] = useState('compute');

  const load = () => { listLicenses().then(d => setLicenses(d ?? [])).catch(() => {}); };
  useEffect(load, []);

  const handleIssue = async () => {
    await issueLicense(centerId, modules.split(',').map(s => s.trim()), parseInt(days) || 30);
    setShowForm(false);
    load();
  };

  return (
    <div>
      <div className="head">
        <h3>{t('nav.licenses')}</h3>
        <div className="actions">
          <button className="btn-ghost" onClick={load}>{t('common.refresh')}</button>
          <button className="btn-primary" onClick={() => setShowForm(!showForm)}>{t('licenses.issue')}</button>
        </div>
      </div>

      {showForm && (
        <div className="stage-card" style={{ marginBottom: 18 }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div><label>{t('licenses.centerId')}</label><input value={centerId} onChange={e => setCenterId(e.target.value)} /></div>
            <div><label>{t('licenses.days')}</label><input value={days} onChange={e => setDays(e.target.value)} /></div>
          </div>
          <div><label>{t('licenses.modules')}</label><input value={modules} onChange={e => setModules(e.target.value)} /></div>
          <div className="actions">
            <button className="btn-primary" onClick={handleIssue}>{t('common.confirm')}</button>
            <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
          </div>
        </div>
      )}

      {licenses.length === 0 ? (
        <div className="hint">{t('licenses.empty')}</div>
      ) : (
        <div className="list">
          {licenses.map(l => (
            <div key={l.id} className="item">
              <div className="item-head">
                <span className="item-title">{l.id}</span>
                <span className={`badge ${l.revoked_at ? 'danger' : l.is_long_term ? 'ok' : 'info'}`}>
                  {l.revoked_at ? t('licenses.revoked') : l.is_long_term ? t('licenses.longTerm') : t('licenses.valid')}
                </span>
              </div>
              <div className="item-meta">
                Center: {l.center_id} | {l.modules} | {l.type}
              </div>
              <div className="actions">
                {!l.revoked_at && <button className="btn-danger" onClick={() => { revokeLicense(l.id).then(load); }}>{t('licenses.revoke')}</button>}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
