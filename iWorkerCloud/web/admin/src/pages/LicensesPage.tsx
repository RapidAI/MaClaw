import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getCenterManagement, type Center } from '../api/centers';
import { listLicenses, issueLicense, revokeLicense, type License } from '../api/licenses';

const moduleOptions = ['compute', 'skill_market', 'upgrade', 'support', 'all'];
type Notice = { tone: 'ok' | 'danger' | 'info'; text: string };
type LicenseDurationMode = 'month' | 'quarter' | 'year' | 'multi_year' | 'permanent';
const MS_PER_DAY = 24 * 60 * 60 * 1000;

function formatDate(value?: string) {
  if (!value || value.startsWith('0001-')) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

const durationDays = (mode: LicenseDurationMode, customYears: string) => {
  if (mode === 'permanent') return 0;
  if (mode === 'month') return 30;
  if (mode === 'quarter') return 90;
  if (mode === 'year') return 365;
  const years = Math.max(2, parseInt(customYears, 10) || 2);
  return years * 365;
};

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
  const [durationMode, setDurationMode] = useState<LicenseDurationMode>('month');
  const [customYears, setCustomYears] = useState('2');
  const [modules, setModules] = useState<string[]>(['compute']);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [issuing, setIssuing] = useState(false);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);

  const centerName = useMemo(() => Object.fromEntries(centers.map(center => [center.id, center.company_name || center.id])), [centers]);
  const moduleLabel = (module: string) => t(`licenses.moduleLabels.${module}`, { defaultValue: module });
  const computedDays = useMemo(() => durationDays(durationMode, customYears), [durationMode, customYears]);

  const licenseStats = useMemo(() => {
    const now = Date.now();
    const active = licenses.filter(license => !license.revoked_at);
    const licensedCenterIds = new Set(active.map(license => license.center_id));
    const expiringSoon = active.filter(license => {
      if (license.is_long_term || !license.expires_at || license.expires_at.startsWith('0001-')) return false;
      const expiresAt = new Date(license.expires_at).getTime();
      return Number.isFinite(expiresAt) && expiresAt >= now && expiresAt - now <= 30 * MS_PER_DAY;
    }).length;
    return {
      total: licenses.length,
      active: active.length,
      revoked: licenses.length - active.length,
      expiringSoon,
      unlicensedCenters: centers.filter(center => !licensedCenterIds.has(center.id)).length,
    };
  }, [centers, licenses]);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const [licenseRows, centerRows] = await Promise.all([
        listLicenses().catch(() => []),
        getCenterManagement().then(report => (report.items || []).map(item => item.center)).catch(() => []),
      ]);
      setLicenses(licenseRows ?? []);
      setCenters(centerRows ?? []);
      if (!centerId && centerRows.length > 0) setCenterId(centerRows[0].id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    load();
  }, []);

  const toggleModule = (module: string) => {
    setModules(current => current.includes(module) ? current.filter(item => item !== module) : [...current, module]);
  };

  const handleIssue = async () => {
    const normalizedCenterId = centerId.trim();
    const normalizedDays = computedDays;
    if (!normalizedCenterId) {
      const message = t('licenses.selectCenter');
      setError(message);
      setNotice({ tone: 'danger', text: message });
      return;
    }
    setError('');
    setNotice(null);
    setIssuing(true);
    try {
      await issueLicense(normalizedCenterId, modules.length > 0 ? modules : ['compute'], normalizedDays);
      setShowForm(false);
      setNotice({ tone: 'ok', text: t('licenses.noticeIssued') });
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setNotice({ tone: 'danger', text: message });
    } finally {
      setIssuing(false);
    }
  };

  const handleRevoke = async (license: License) => {
    if (!confirm(t('licenses.confirmRevoke'))) return;
    setRevokingId(license.id);
    setError('');
    setNotice(null);
    try {
      await revokeLicense(license.id);
      setNotice({ tone: 'ok', text: t('licenses.noticeRevoked') });
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setNotice({ tone: 'danger', text: message });
    } finally {
      setRevokingId(null);
    }
  };

  return (
    <div className="license-page-stack cloud-page-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">{t('licenses.eyebrow')}</div>
          <h3>{t('nav.licenses')}</h3>
          <p>{t('licenses.positionDesc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('licenses.positionTitle')}</strong>
          <span>{t('licenses.boundaryNote')}</span>
        </div>
      </section>

      <div className="metrics cloud-metrics ops-summary-grid">
        <div className="metric"><label>{t('licenses.stats.total')}</label><strong>{licenseStats.total}</strong><span>{t('licenses.stats.totalHint')}</span></div>
        <div className="metric"><label>{t('licenses.stats.active')}</label><strong>{licenseStats.active}</strong><span>{t('licenses.stats.activeHint')}</span></div>
        <div className="metric"><label>{t('licenses.stats.expiring')}</label><strong>{licenseStats.expiringSoon}</strong><span>{t('licenses.stats.expiringHint')}</span></div>
        <div className="metric"><label>{t('licenses.stats.unlicensed')}</label><strong>{licenseStats.unlicensedCenters}</strong><span>{t('licenses.stats.unlicensedHint')}</span></div>
      </div>

      <div className="head">
        <div>
          <h3>{t('nav.licenses')}</h3>
          <div className="item-meta">{t('licenses.desc')}</div>
        </div>
        <div className="actions">
          <button className="btn-ghost" onClick={load}>{loading ? t('common.loading') : t('common.refresh')}</button>
          <button className="btn-primary" onClick={() => { setShowForm(!showForm); setNotice(null); setError(''); }}>{t('licenses.issue')}</button>
        </div>
      </div>

      {notice ? <div className={`notice ${notice.tone}`}>{notice.text}</div> : null}
      {error && !notice ? <div className="hint danger">{error}</div> : null}

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
            <div>
              <label>{t('licenses.duration')}</label>
              <select value={durationMode} onChange={event => setDurationMode(event.target.value as LicenseDurationMode)}>
                <option value="month">{t('licenses.durationOptions.month')}</option>
                <option value="quarter">{t('licenses.durationOptions.quarter')}</option>
                <option value="year">{t('licenses.durationOptions.year')}</option>
                <option value="multi_year">{t('licenses.durationOptions.multiYear')}</option>
                <option value="permanent">{t('licenses.durationOptions.permanent')}</option>
              </select>
            </div>
            {durationMode === 'multi_year' ? <div>
              <label>{t('licenses.customYears')}</label>
              <input type="number" min="2" value={customYears} onChange={event => setCustomYears(event.target.value)} />
            </div> : <div>
              <label>{t('licenses.calculatedDays')}</label>
              <input readOnly value={computedDays === 0 ? t('licenses.permanentValue') : String(computedDays)} />
            </div>}
            {durationMode === 'multi_year' ? <div className="field-span-2 license-duration-hint">{t('licenses.calculatedDays')}: {computedDays}</div> : null}
            <div className="field-span-2">
              <label>{t('licenses.modules')}</label>
              <div className="module-check-grid">
                {moduleOptions.map(module => (
                  <label key={module} className="module-check">
                    <input type="checkbox" checked={modules.includes(module)} onChange={() => toggleModule(module)} />
                    <span>{moduleLabel(module)}</span>
                  </label>
                ))}
              </div>
            </div>
          </div>
          <div className="actions">
            <button className="btn-primary" disabled={!centerId.trim() || issuing} onClick={handleIssue}>{issuing ? t('common.loading') : t('common.confirm')}</button>
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
                  {licenseModules.map(module => <span key={module}>{moduleLabel(module)}</span>)}
                  <span>{license.type}</span>
                  <span>{t('licenses.expiresAt')}: {license.is_long_term ? t('licenses.longTerm') : formatDate(license.expires_at)}</span>
                  <span>{t('licenses.issuedAt')}: {formatDate(license.created_at)}</span>
                </div>
                <div className="actions">
                  {!license.revoked_at && <button className="btn-danger" disabled={revokingId === license.id} onClick={() => handleRevoke(license)}>{revokingId === license.id ? t('common.loading') : t('licenses.revoke')}</button>}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
