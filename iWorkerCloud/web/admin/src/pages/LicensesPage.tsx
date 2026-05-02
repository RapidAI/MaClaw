import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getCenterManagement, type Center } from '../api/centers';
import { listLicenses, issueLicense, revokeLicense, type License } from '../api/licenses';

const moduleOptions = ['compute', 'skill_market', 'upgrade', 'support', 'all'];

function formatDate(value?: string) {
  if (!value || value.startsWith('0001-')) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function parseModules(raw: string) {
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // Keep compatibility with old comma-separated values.
  }
  return raw.split(',').map(item => item.trim()).filter(Boolean);
}

export function LicensesPage() {
  const { t } = useTranslation();
  const [licenses, setLicenses] = useState<License[]>([]);
  const [centers, setCenters] = useState<Center[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [centerId, setCenterId] = useState('');
  const [days, setDays] = useState('30');
  const [modules, setModules] = useState<string[]>(['compute']);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const centerName = useMemo(() => Object.fromEntries(centers.map(center => [center.id, center.company_name || center.id])), [centers]);

  const load = () => {
    setLoading(true);
    setError('');
    Promise.all([
      listLicenses().catch(() => []),
      getCenterManagement().then(report => (report.items || []).map(item => item.center)).catch(() => []),
    ]).then(([licenseRows, centerRows]) => {
      setLicenses(licenseRows ?? []);
      setCenters(centerRows ?? []);
      if (!centerId && centerRows.length > 0) setCenterId(centerRows[0].id);
    }).catch(err => setError(err instanceof Error ? err.message : String(err))).finally(() => setLoading(false));
  };
  useEffect(load, []);

  const toggleModule = (module: string) => {
    setModules(current => current.includes(module) ? current.filter(item => item !== module) : [...current, module]);
  };

  const handleIssue = async () => {
    setError('');
    try {
      await issueLicense(centerId, modules.length > 0 ? modules : ['compute'], parseInt(days, 10) || 30);
      setShowForm(false);
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleRevoke = async (license: License) => {
    if (!confirm(t('licenses.confirmRevoke'))) return;
    await revokeLicense(license.id);
    load();
  };

  return (
    <div className="license-page-stack">
      <div className="head">
        <div>
          <h3>{t('nav.licenses')}</h3>
          <div className="item-meta">{t('licenses.desc')}</div>
        </div>
        <div className="actions">
          <button className="btn-ghost" onClick={load}>{loading ? t('common.loading') : t('common.refresh')}</button>
          <button className="btn-primary" onClick={() => setShowForm(!showForm)}>{t('licenses.issue')}</button>
        </div>
      </div>

      {error ? <div className="hint danger">{error}</div> : null}

      {showForm && (
        <div className="stage-card license-editor">
          <div className="license-editor-grid">
            <div>
              <label>{t('licenses.centerId')}</label>
              <select value={centerId} onChange={event => setCenterId(event.target.value)}>
                {centers.map(center => <option key={center.id} value={center.id}>{center.company_name || center.id} ({center.id})</option>)}
                {centers.length === 0 ? <option value="">{t('centers.empty')}</option> : null}
              </select>
            </div>
            <div><label>{t('licenses.days')}</label><input type="number" value={days} onChange={event => setDays(event.target.value)} /></div>
            <div className="field-span-2">
              <label>{t('licenses.modules')}</label>
              <div className="module-check-grid">
                {moduleOptions.map(module => (
                  <label key={module} className="module-check">
                    <input type="checkbox" checked={modules.includes(module)} onChange={() => toggleModule(module)} />
                    <span>{module}</span>
                  </label>
                ))}
              </div>
            </div>
          </div>
          <div className="actions">
            <button className="btn-primary" disabled={!centerId} onClick={handleIssue}>{t('common.confirm')}</button>
            <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
          </div>
        </div>
      )}

      {licenses.length === 0 ? (
        <div className="hint">{t('licenses.empty')}</div>
      ) : (
        <div className="list license-list">
          {licenses.map(license => {
            const licenseModules = parseModules(license.modules);
            return (
              <div key={license.id} className="item license-card">
                <div className="item-head">
                  <div>
                    <span className="item-title">{centerName[license.center_id] || license.center_id}</span>
                    <div className="item-meta">{license.id} | {license.center_id}</div>
                  </div>
                  <span className={`badge ${license.revoked_at ? 'danger' : license.is_long_term ? 'ok' : 'info'}`}>
                    {license.revoked_at ? t('licenses.revoked') : license.is_long_term ? t('licenses.longTerm') : t('licenses.valid')}
                  </span>
                </div>
                <div className="cloud-pill-list">
                  {licenseModules.map(module => <span key={module}>{module}</span>)}
                  <span>{license.type}</span>
                  <span>{t('licenses.expiresAt')}: {license.is_long_term ? t('licenses.longTerm') : formatDate(license.expires_at)}</span>
                  <span>{t('licenses.issuedAt')}: {formatDate(license.created_at)}</span>
                </div>
                <div className="actions">
                  {!license.revoked_at && <button className="btn-danger" onClick={() => handleRevoke(license)}>{t('licenses.revoke')}</button>}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
