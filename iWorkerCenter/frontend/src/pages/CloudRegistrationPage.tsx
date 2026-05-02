import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import {
  fetchCloudConfig,
  fetchCloudLicense,
  fetchCloudStatus,
  registerCenterToCloud,
  saveCloudConfig,
  type CloudConfig,
  type CloudStatus,
} from '../api/cloud';

const defaultConfig: CloudConfig = {
  base_url: '',
  center_base_url: '',
  registration_name: '',
  registration_email: '',
  cloud_control_mode: 'cloud_managed',
};

const modeLabels: Record<string, string> = {
  cloud_managed: 'Cloud managed',
  hybrid: 'Hybrid',
  self_managed: 'Self managed',
};

export function CloudRegistrationPage() {
  const [config, setConfig] = useState<CloudConfig>(defaultConfig);
  const [status, setStatus] = useState<CloudStatus | null>(null);
  const [licenseText, setLicenseText] = useState('');
  const [message, setMessage] = useState('');
  const [registerInfo, setRegisterInfo] = useState({ company_name: '', legal_person: '', admin_phone: '', admin_email: '', address: '' });
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const statusLabel = useMemo(() => {
    if (!status) return 'Unknown';
    if (!status.configured) return 'Not configured';
    if (!status.registered) return 'Ready to register';
    if (status.status === 'licensed') return 'Licensed';
    if (status.status === 'pending') return 'Pending license';
    if (status.status === 'offline') return 'Cloud offline';
    return status.status || 'Registered';
  }, [status]);

  const load = async () => {
    setError('');
    try {
      const [cfg, st] = await Promise.all([fetchCloudConfig(), fetchCloudStatus().catch(() => null)]);
      setConfig({ ...defaultConfig, ...cfg });
      if (st) setStatus(st);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Load failed');
    }
  };

  useEffect(() => { void load(); }, []);

  const update = (patch: Partial<CloudConfig>) => setConfig(prev => ({ ...prev, ...patch }));
  const updateRegisterInfo = (patch: Partial<typeof registerInfo>) => setRegisterInfo(prev => ({ ...prev, ...patch }));

  const save = async () => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const saved = await saveCloudConfig(config);
      setConfig(saved);
      setMessage('Config saved. You can register this Center now.');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setBusy(false);
    }
  };

  const register = async () => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const resp = await registerCenterToCloud(registerInfo);
      setMessage(`Registered with Cloud: ${resp.center_id}. Heartbeat was sent immediately.`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Registration failed');
    } finally {
      setBusy(false);
    }
  };

  const refreshLicense = async () => {
    setBusy(true);
    setError('');
    setLicenseText('');
    try {
      const lic = await fetchCloudLicense();
      setLicenseText(`${lic.type || 'license'} / ${lic.is_long_term ? 'long term' : lic.expires_at}`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'License refresh failed');
      await load();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="Cloud registration" desc="Connect this iWorkerCenter service to iWorkerCloud without restarting the Center.">
        <div className="cloud-status-grid">
          <StatusTile label="Connection" value={statusLabel} tone={status?.configured ? 'ok' : 'warn'} />
          <StatusTile label="Center ID" value={status?.center_id || '-'} />
          <StatusTile label="Control mode" value={modeLabels[config.cloud_control_mode] || config.cloud_control_mode || '-'} />
        </div>

        <div className="cloud-form-grid">
          <Field label="iWorkerCloud URL" value={config.base_url} placeholder="http://127.0.0.1:9366" onChange={v => update({ base_url: v })} />
          <Field label="Center public URL" value={config.center_base_url} placeholder="http://127.0.0.1:9377" onChange={v => update({ center_base_url: v })} />
          <Field label="Registration name" value={config.registration_name} placeholder="HQ iWorkerCenter" onChange={v => update({ registration_name: v })} />
          <Field label="Registration email" value={config.registration_email} placeholder="admin@example.com" onChange={v => update({ registration_email: v })} />
          <label className="cloud-field">
            <span>Control mode</span>
            <select value={config.cloud_control_mode} onChange={e => update({ cloud_control_mode: e.target.value })}>
              <option value="cloud_managed">Cloud managed</option>
              <option value="hybrid">Hybrid</option>
              <option value="self_managed">Self managed</option>
            </select>
          </label>
        </div>

        <div className="cloud-form-grid cloud-registration-grid">
          <Field label="Company name" value={registerInfo.company_name} placeholder="Acme Inc" onChange={v => updateRegisterInfo({ company_name: v })} />
          <Field label="Legal person" value={registerInfo.legal_person} placeholder="Jane Doe" onChange={v => updateRegisterInfo({ legal_person: v })} />
          <Field label="Contact phone" value={registerInfo.admin_phone} placeholder="+86 13800000000" onChange={v => updateRegisterInfo({ admin_phone: v })} />
          <Field label="Admin email" value={registerInfo.admin_email} placeholder="admin@example.com" onChange={v => updateRegisterInfo({ admin_email: v })} />
          <Field label="Company address" value={registerInfo.address} placeholder="Company registered address" onChange={v => updateRegisterInfo({ address: v })} />
        </div>

        <div className="cloud-actions">
          <button className="cloud-primary" type="button" onClick={save} disabled={busy}>Save config</button>
          <button className="ghost" type="button" onClick={register} disabled={busy || !config.base_url || !registerInfo.company_name || !registerInfo.admin_email}>Register</button>
          <button className="ghost" type="button" onClick={refreshLicense} disabled={busy || !status?.registered}>Refresh license</button>
        </div>
        {message ? <p className="cloud-message ok">{message}</p> : null}
        {licenseText ? <p className="cloud-message ok">License: {licenseText}</p> : null}
        {error ? <p className="cloud-message danger">{error}</p> : null}
        {status?.license_error ? <p className="cloud-message warn">Cloud status: {status.license_error}</p> : null}
      </SectionCard>
    </div>
  );
}

function Field({ label, value, placeholder, onChange }: { label: string; value: string; placeholder?: string; onChange: (value: string) => void }) {
  return (
    <label className="cloud-field">
      <span>{label}</span>
      <input value={value || ''} placeholder={placeholder} onChange={e => onChange(e.target.value)} />
    </label>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={`cloud-status-tile ${tone || ''}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}