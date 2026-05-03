import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import {
  fetchCloudConfig,
  fetchCloudLicense,
  fetchCloudStatus,
  registerCenterToCloud,
  saveCloudConfig,
  type CloudConfig,
  type CloudLicense,
  type CloudStatus,
} from '../api/cloud';

const defaultConfig: CloudConfig = {
  base_url: '',
  center_base_url: '',
  registration_name: '',
  registration_email: '',
  cloud_control_mode: 'cloud_managed',
};


type RegisterInfo = { company_name: string; legal_person: string; admin_phone: string; admin_email: string; address: string };

const registrationDraftKey = 'iworkercenter.cloud.registrationDraft';

const defaultRegisterInfo: RegisterInfo = { company_name: '', legal_person: '', admin_phone: '', admin_email: '', address: '' };

const loadRegistrationDraft = (): RegisterInfo => {
  if (typeof window === 'undefined') return defaultRegisterInfo;
  try {
    const raw = window.localStorage.getItem(registrationDraftKey);
    return raw ? { ...defaultRegisterInfo, ...JSON.parse(raw) } : defaultRegisterInfo;
  } catch {
    return defaultRegisterInfo;
  }
};

const saveRegistrationDraft = (info: RegisterInfo) => {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(registrationDraftKey, JSON.stringify(info));
};

const modeLabels: Record<string, string> = {
  cloud_managed: 'Cloud managed',
  hybrid: 'Hybrid',
  self_managed: 'Self managed',
};

const statusTone = (status: CloudStatus | null): 'ok' | 'warn' => {
  if (!status?.configured) return 'warn';
  if (status.status === 'offline' || status.status === 'pending') return 'warn';
  return 'ok';
};

const parseModules = (modules?: string) => {
  if (!modules) return [] as string[];
  try {
    const parsed = JSON.parse(modules);
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // Fall back to comma-separated module lists from older Cloud versions.
  }
  return modules.split(',').map(item => item.trim()).filter(Boolean);
};

const licenseSummary = (license?: CloudLicense) => {
  if (!license) return '等待授权确认';
  const modules = parseModules(license.modules);
  const scope = modules.length ? modules.join(', ') : '基础授权';
  const expiry = license.is_long_term ? '长期有效' : (license.expires_at || '未设置到期日');
  return (license.type || 'license') + ' / ' + scope + ' / ' + expiry;
};

export function CloudRegistrationPage() {
  const [config, setConfig] = useState<CloudConfig>(defaultConfig);
  const [status, setStatus] = useState<CloudStatus | null>(null);
  const [licenseText, setLicenseText] = useState('');
  const [message, setMessage] = useState('');
  const [registerInfo, setRegisterInfo] = useState<RegisterInfo>(() => loadRegistrationDraft());
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

  const registrationSteps = useMemo(() => ([
    { label: '1. 配置 Cloud 地址', done: Boolean(status?.configured || config.base_url), detail: config.base_url || '尚未配置 iWorkerCloud URL' },
    { label: '2. 注册 Center', done: Boolean(status?.registered), detail: status?.center_id || '提交企业信息后生成 Center ID 和密钥' },
    { label: '3. 确认授权', done: status?.status === 'licensed', detail: status?.status === 'licensed' ? licenseSummary(status.license) : '等待 Cloud 管理员确认模块和有效期' },
    { label: '4. 本地业务可用', done: Boolean(status?.non_blocking ?? true), detail: 'Cloud 故障不会阻断 Center 与 iWorker 的本地任务、记忆和已发布能力。' },
  ]), [config.base_url, status]);

  const load = async () => {
    setError('');
    try {
      const [cfg, st] = await Promise.all([fetchCloudConfig(), fetchCloudStatus().catch(() => null)]);
      setConfig({ ...defaultConfig, ...cfg });
      setRegisterInfo(prev => {
        const next = {
          ...prev,
          company_name: prev.company_name || cfg.registration_name || '',
          admin_email: prev.admin_email || cfg.registration_email || '',
        };
        saveRegistrationDraft(next);
        return next;
      });
      if (st) setStatus(st);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Load failed');
    }
  };

  useEffect(() => { void load(); }, []);

  const update = (patch: Partial<CloudConfig>) => setConfig(prev => ({ ...prev, ...patch }));
  const updateRegisterInfo = (patch: Partial<RegisterInfo>) => setRegisterInfo(prev => {
    const next = { ...prev, ...patch };
    saveRegistrationDraft(next);
    return next;
  });

  const save = async () => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const saved = await saveCloudConfig(config);
      setConfig(saved);
      setMessage('Cloud 配置已保存，可以继续注册本 Center。');
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
      setMessage('已向 iWorkerCloud 注册：' + resp.center_id + '。Center 已立即发送心跳，等待 Cloud 管理员授权确认。');
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
      setLicenseText(licenseSummary(lic));
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
      <SectionCard title="Cloud 注册与授权" desc="将本 iWorkerCenter 注册到 iWorkerCloud。Cloud 只负责注册、授权、算力和能力市场协调，不承载企业业务流程。">
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label="连接状态" value={statusLabel} tone={statusTone(status)} />
          <StatusTile label="Center ID" value={status?.center_id || '-'} />
          <StatusTile label="控制模式" value={modeLabels[config.cloud_control_mode] || config.cloud_control_mode || '-'} />
          <StatusTile label="业务隔离" value={status?.business_scope || 'local_center_business'} tone="ok" />
        </div>

        <div className="cloud-step-list">
          {registrationSteps.map(step => (
            <div key={step.label} className={'cloud-step ' + (step.done ? 'is-done' : 'is-pending')}>
              <span>{step.done ? '完成' : '待处理'}</span>
              <strong>{step.label}</strong>
              <p>{step.detail}</p>
            </div>
          ))}
        </div>

        <p className="cloud-message ok">本地连续性：即使 iWorkerCloud 离线，Center 到 iWorker 的任务下发、记忆读取、已发布 MCP/Skill 和人工协作仍按本地状态继续运行。</p>

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