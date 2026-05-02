import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchCloudConfig, fetchCloudStatus, registerCenterToCloud, updateCloudConfig, type CloudConfig, type CloudLicense, type CloudRegistrationStatus, type CloudRegisterResponse } from '../api/cloud';

type CloudViewState = 'loading' | 'licensed' | 'unregistered' | 'not_configured' | 'pending' | 'offline' | 'error';

function parseModules(raw?: string): string[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // Some older Cloud deployments may return a plain comma-separated list.
  }
  return raw.split(',').map(item => item.trim()).filter(Boolean);
}

function formatDate(value?: string, fallback = '-') {
  if (!value) return fallback;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function normalizeState(status?: string): CloudViewState {
  switch (status) {
    case 'licensed':
    case 'unregistered':
    case 'not_configured':
    case 'pending':
    case 'offline':
    case 'loading':
    case 'error':
      return status;
    default:
      return 'error';
  }
}

function classifyError(message: string): CloudViewState {
  const normalized = message.toLowerCase();
  if (normalized.includes('not configured')) return 'not_configured';
  if (normalized.includes('credentials missing')) return 'unregistered';
  if (normalized.includes('no active license') || normalized.includes('not found')) return 'pending';
  if (normalized.includes('failed to fetch') || normalized.includes('network') || normalized.includes('timeout') || normalized.includes('connection')) return 'offline';
  return 'error';
}

export function CloudRegistrationPage() {
  const { t } = useTranslation();
  const [config, setConfig] = useState<CloudConfig>({ base_url: '', center_base_url: '', registration_name: '', registration_email: '', cloud_control_mode: 'cloud_managed' });
  const [license, setLicense] = useState<CloudLicense | null>(null);
  const [cloudStatus, setCloudStatus] = useState<CloudRegistrationStatus | null>(null);
  const [registerResult, setRegisterResult] = useState<CloudRegisterResponse | null>(null);
  const [state, setState] = useState<CloudViewState>('loading');
  const [message, setMessage] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [registering, setRegistering] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);

  const modules = useMemo(() => parseModules(license?.modules), [license]);

  const load = async () => {
    setLoading(true);
    setMessage('');
    try {
      const [nextConfig, next] = await Promise.all([
        fetchCloudConfig().catch(() => null),
        fetchCloudStatus(),
      ]);
      if (nextConfig) {
        setConfig({
          base_url: nextConfig.base_url || '',
          center_base_url: nextConfig.center_base_url || '',
          registration_name: nextConfig.registration_name || '',
          registration_email: nextConfig.registration_email || '',
          cloud_control_mode: nextConfig.cloud_control_mode || 'cloud_managed',
        });
      }
      setCloudStatus(next);
      setLicense(next.license || null);
      setState(normalizeState(next.status));
      setMessage(next.license_error || '');
    } catch (err: any) {
      const nextMessage = err?.message || t('cloud.errors.unknown');
      setCloudStatus(null);
      setLicense(null);
      setMessage(nextMessage);
      setState(classifyError(nextMessage));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const handleConfigChange = (key: keyof CloudConfig, value: string) => {
    setConfig(current => ({ ...current, [key]: value }));
  };

  const handleSaveConfig = async () => {
    setSavingConfig(true);
    setMessage('');
    try {
      const next = await updateCloudConfig(config);
      setConfig({
        base_url: next.base_url || '',
        center_base_url: next.center_base_url || '',
        registration_name: next.registration_name || '',
        registration_email: next.registration_email || '',
        cloud_control_mode: next.cloud_control_mode || 'cloud_managed',
      });
      await load();
    } catch (err: any) {
      setMessage(err?.message || t('cloud.errors.saveConfigFailed'));
      setState('error');
    } finally {
      setSavingConfig(false);
    }
  };

  const handleRegister = async () => {
    setRegistering(true);
    setMessage('');
    try {
      const result = await registerCenterToCloud();
      setRegisterResult(result);
      setState(result.status === 'active' ? 'licensed' : 'pending');
      setMessage(result.message || '');
      await load();
    } catch (err: any) {
      const nextMessage = err?.message || t('cloud.errors.registerFailed');
      setMessage(nextMessage);
      setState(classifyError(nextMessage));
    } finally {
      setRegistering(false);
    }
  };

  const tone = state === 'licensed' ? 'ok' : state === 'not_configured' || state === 'offline' ? 'warn' : 'info';
  const centerId = license?.center_id || cloudStatus?.center_id || registerResult?.center_id || '-';

  return (
    <div className="center-page-stack cloud-registration-page">
      <SectionCard title={t('cloud.title')} desc={loading ? t('common.loading') : t('cloud.description')}>
        <div className="cloud-status-layout">
          <div className="cloud-status-panel">
            <span className={`badge ${tone}`}>{t(`cloud.states.${state}`)}</span>
            <h3>{t('cloud.statusTitle')}</h3>
            <p>{message || t(`cloud.stateHelp.${state}`)}</p>
            <div className="cloud-actions">
              <button className="btn-primary" type="button" onClick={handleRegister} disabled={registering || state === 'not_configured'}>
                {registering ? t('cloud.registering') : t('cloud.registerAction')}
              </button>
              <button className="btn-ghost" type="button" onClick={load} disabled={loading || registering}>
                {t('cloud.refresh')}
              </button>
            </div>
          </div>

          <div className="cloud-detail-grid">
            <div className="cloud-detail-item"><span>{t('cloud.fields.centerId')}</span><strong>{centerId}</strong></div>
            <div className="cloud-detail-item"><span>{t('cloud.fields.licenseType')}</span><strong>{license?.type || '-'}</strong></div>
            <div className="cloud-detail-item"><span>{t('cloud.fields.expiresAt')}</span><strong>{license?.is_long_term ? t('cloud.longTerm') : formatDate(license?.expires_at)}</strong></div>
            <div className="cloud-detail-item"><span>{t('cloud.fields.createdAt')}</span><strong>{formatDate(license?.created_at)}</strong></div>
          </div>
        </div>
      </SectionCard>

      <SectionCard title={t('cloud.configTitle')} desc={t('cloud.configDesc')}>
        <div className="cloud-config-form">
          <label>
            <span>{t('cloud.config.baseUrl')}</span>
            <input value={config.base_url || ''} onChange={event => handleConfigChange('base_url', event.target.value)} placeholder="https://cloud.example.com" />
          </label>
          <label>
            <span>{t('cloud.config.centerBaseUrl')}</span>
            <input value={config.center_base_url || ''} onChange={event => handleConfigChange('center_base_url', event.target.value)} placeholder="https://center.example.com" />
          </label>
          <label>
            <span>{t('cloud.config.registrationName')}</span>
            <input value={config.registration_name || ''} onChange={event => handleConfigChange('registration_name', event.target.value)} placeholder="HQ iWorkerCenter" />
          </label>
          <label>
            <span>{t('cloud.config.registrationEmail')}</span>
            <input value={config.registration_email || ''} onChange={event => handleConfigChange('registration_email', event.target.value)} placeholder="admin@example.com" />
          </label>
          <label>
            <span>{t('cloud.config.controlMode')}</span>
            <select value={config.cloud_control_mode || 'cloud_managed'} onChange={event => handleConfigChange('cloud_control_mode', event.target.value)}>
              <option value="cloud_managed">{t('cloud.config.cloudManaged')}</option>
              <option value="self_managed">{t('cloud.config.selfManaged')}</option>
            </select>
          </label>
          <button className="btn-primary" type="button" onClick={handleSaveConfig} disabled={savingConfig || registering}>
            {savingConfig ? t('cloud.config.saving') : t('cloud.config.save')}
          </button>
        </div>
      </SectionCard>

      <div className="panel-grid cloud-info-grid">
        <SectionCard title={t('cloud.modulesTitle')} desc={t('cloud.modulesDesc')}>
          {modules.length > 0 ? (
            <div className="cloud-module-list">
              {modules.map(module => <span key={module} className="badge info">{module}</span>)}
            </div>
          ) : <div className="hint">{t('cloud.noModules')}</div>}
        </SectionCard>

        <SectionCard title={t('cloud.boundaryTitle')} desc={t('cloud.boundaryDesc')}>
          <div className="item-list">
            <div className="item-row"><strong>{t('cloud.boundary.controlTitle')}</strong><p>{t('cloud.boundary.controlDesc')}</p></div>
            <div className="item-row"><strong>{t('cloud.boundary.businessTitle')}</strong><p>{t('cloud.boundary.businessDesc')}</p></div>
            <div className="item-row"><strong>{t('cloud.boundary.offlineTitle')}</strong><p>{t('cloud.boundary.offlineDesc')}</p></div>
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
