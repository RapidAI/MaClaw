import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchCloudConfig, fetchCloudLicense, fetchCloudStatus, registerCenterToCloud, saveCloudConfig, type CloudConfig, type CloudLicense, type CloudStatus } from '../api/cloud';

const defaultConfig: CloudConfig = {
  base_url: '',
  center_base_url: '',
  registration_name: '',
  registration_email: '',
  cloud_control_mode: 'cloud_managed',
};

type RegisterInfo = { company_name: string; legal_person: string; admin_phone: string; admin_email: string; address: string };
type Notice = { tone: 'ok' | 'warn' | 'danger'; text: string };

const draftKey = 'iworkercenter.cloud.registrationDraft';
const emptyRegisterInfo: RegisterInfo = { company_name: '', legal_person: '', admin_phone: '', admin_email: '', address: '' };

const text = {
  title: '\u0043\u006c\u006f\u0075\u0064 \u6ce8\u518c\u4e0e\u6388\u6743',
  desc: '\u5c06\u672c iWorkerCenter \u6ce8\u518c\u5230 iWorkerCloud\u3002Cloud \u53ea\u8d1f\u8d23\u6ce8\u518c\u3001\u6388\u6743\u3001\u7b97\u529b\u548c\u80fd\u529b\u5e02\u573a\u534f\u8c03\uff0c\u4e0d\u53c2\u4e0e\u4f01\u4e1a\u4e1a\u52a1\u8fd0\u884c\u3002',
  status: '\u8fde\u63a5\u72b6\u6001',
  centerId: 'Center ID',
  controlMode: '\u63a7\u5236\u6a21\u5f0f',
  boundary: '\u4e1a\u52a1\u8fb9\u754c',
  unknown: '\u672a\u77e5',
  notConfigured: '\u672a\u914d\u7f6e',
  readyRegister: '\u53ef\u4ee5\u6ce8\u518c',
  licensed: '\u5df2\u6388\u6743',
  pendingLicense: '\u7b49\u5f85\u6388\u6743',
  offline: 'Cloud \u79bb\u7ebf',
  registered: '\u5df2\u6ce8\u518c',
  done: '\u5b8c\u6210',
  pending: '\u5f85\u5904\u7406',
  stepConfig: '1. \u914d\u7f6e Cloud \u5730\u5740',
  stepRegister: '2. \u6ce8\u518c Center',
  stepLicense: '3. \u786e\u8ba4\u6388\u6743',
  stepLocal: '4. \u672c\u5730\u4e1a\u52a1\u53ef\u7528',
  noCloudUrl: '\u5c1a\u672a\u914d\u7f6e iWorkerCloud URL',
  submitCompany: '\u63d0\u4ea4\u4f01\u4e1a\u4fe1\u606f\u540e\u751f\u6210 Center ID \u548c\u5bc6\u94a5',
  waitingLicense: '\u7b49\u5f85 Cloud \u7ba1\u7406\u5458\u6388\u6743\u786e\u8ba4',
  localDetail: 'Cloud \u6545\u969c\u4e0d\u4f1a\u963b\u65ad Center \u5230 iWorker \u7684\u672c\u5730\u4efb\u52a1\u3001\u8bb0\u5fc6\u548c\u5df2\u4e0b\u53d1\u80fd\u529b\u3002',
  continuityTitle: '\u79bb\u7ebf\u8fde\u7eed\u6027',
  continuityDesc: 'iWorkerCloud \u5931\u8054\u65f6\uff0cCenter \u4ecd\u6309\u672c\u5730\u7b56\u7565\u7ee7\u7eed\u63a8\u9001\u4efb\u52a1\u3001\u63d0\u4f9b\u8bb0\u5fc6\u3001\u7ba1\u7406 MCP/Skill \u548c\u652f\u6301\u4eba\u673a\u534f\u4f5c\u3002Cloud \u6062\u590d\u540e\u518d\u540c\u6b65\u6388\u6743\u3001\u7b97\u529b\u548c\u5e02\u573a\u72b6\u6001\u3002',
  isolationTitle: '\u4e1a\u52a1\u9694\u79bb',
  isolationDesc: 'Cloud \u4e0d\u8bfb\u53d6\u79df\u6237\u3001\u5458\u5de5\u3001\u6d41\u7a0b\u3001\u4f1a\u8bdd\u548c\u5ba2\u6237\u4e1a\u52a1\u6570\u636e\u3002\u8fd9\u4e9b\u4ecd\u5c5e\u4e8e iWorkerCenter \u672c\u5730\u7ba1\u7406\u8fb9\u754c\u3002',
  configTitle: 'Cloud \u8fde\u63a5\u914d\u7f6e',
  registerTitle: 'Center \u6ce8\u518c\u4fe1\u606f',
  draftHint: '\u6ce8\u518c\u8868\u5355\u4f1a\u81ea\u52a8\u4fdd\u5b58\u5728\u672c\u673a\uff0c\u4fdd\u5b58 Cloud \u914d\u7f6e\u540e\u4e0d\u4f1a\u4e22\u5931\u8f93\u5165\u5185\u5bb9\u3002',
  save: '\u4fdd\u5b58\u914d\u7f6e',
  register: '\u6ce8\u518c\u5230 Cloud',
  refreshLicense: '\u5237\u65b0\u6388\u6743',
  busy: '\u5904\u7406\u4e2d...',
  needCloudUrl: '\u8bf7\u5148\u586b\u5199 iWorkerCloud URL\u3002',
  needRegister: '\u8bf7\u5148\u586b\u5199 iWorkerCloud URL\u3001\u516c\u53f8\u540d\u79f0\u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u3002',
  configSaved: 'Cloud \u914d\u7f6e\u5df2\u4fdd\u5b58\uff0c\u53ef\u4ee5\u7ee7\u7eed\u6ce8\u518c\u672c Center\u3002',
  registeredPrefix: '\u5df2\u5411 iWorkerCloud \u6ce8\u518c\uff1a',
  registeredSuffix: '\u3002Center \u5df2\u7acb\u5373\u53d1\u9001\u5fc3\u8df3\uff0c\u7b49\u5f85 Cloud \u7ba1\u7406\u5458\u6388\u6743\u786e\u8ba4\u3002',
  license: '\u6388\u6743',
  cloudStatus: 'Cloud \u72b6\u6001',
  baseLicense: '\u57fa\u7840\u6388\u6743',
  longTerm: '\u957f\u671f\u6709\u6548',
  noExpiry: '\u672a\u8bbe\u7f6e\u5230\u671f\u65e5',
  localBusiness: 'local_center_business',
};

const modeLabels: Record<string, string> = { cloud_managed: 'Cloud managed', hybrid: 'Hybrid', self_managed: 'Self managed' };
const moduleLabels: Record<string, string> = { compute: '\u7b97\u529b', skill_market: '\u6280\u80fd\u5e02\u573a', upgrade: '\u5347\u7ea7', support: '\u652f\u6301', all: '\u5168\u90e8\u6a21\u5757' };

const loadDraft = (): RegisterInfo => {
  if (typeof window === 'undefined') return emptyRegisterInfo;
  try {
    const raw = window.localStorage.getItem(draftKey);
    return raw ? { ...emptyRegisterInfo, ...JSON.parse(raw) } : emptyRegisterInfo;
  } catch {
    return emptyRegisterInfo;
  }
};

const saveDraft = (info: RegisterInfo) => {
  if (typeof window !== 'undefined') window.localStorage.setItem(draftKey, JSON.stringify(info));
};

const parseModules = (modules?: string) => {
  if (!modules) return [] as string[];
  try {
    const parsed = JSON.parse(modules);
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // Compatibility with old comma-separated module values.
  }
  return modules.split(',').map(item => item.trim()).filter(Boolean);
};

const licenseSummary = (license?: CloudLicense) => {
  if (!license) return text.waitingLicense;
  const modules = parseModules(license.modules).map(module => moduleLabels[module] || module);
  const scope = modules.length ? modules.join(', ') : text.baseLicense;
  const expiry = license.is_long_term ? text.longTerm : (license.expires_at || text.noExpiry);
  return (license.type || 'license') + ' / ' + scope + ' / ' + expiry;
};

const tileTone = (status: CloudStatus | null): 'ok' | 'warn' => {
  if (!status?.configured) return 'warn';
  if (status.status === 'offline' || status.status === 'pending') return 'warn';
  return 'ok';
};

export function CloudRegistrationPage() {
  const [config, setConfig] = useState<CloudConfig>(defaultConfig);
  const [status, setStatus] = useState<CloudStatus | null>(null);
  const [licenseText, setLicenseText] = useState('');
  const [notice, setNotice] = useState<Notice | null>(null);
  const [registerInfo, setRegisterInfo] = useState<RegisterInfo>(() => loadDraft());
  const [busy, setBusy] = useState(false);

  const trimmedConfig = useMemo(() => ({
    base_url: config.base_url.trim(),
    center_base_url: config.center_base_url.trim(),
    registration_name: config.registration_name.trim(),
    registration_email: config.registration_email.trim(),
    cloud_control_mode: config.cloud_control_mode.trim() || 'cloud_managed',
  }), [config]);

  const trimmedRegisterInfo = useMemo(() => ({
    company_name: registerInfo.company_name.trim(),
    legal_person: registerInfo.legal_person.trim(),
    admin_phone: registerInfo.admin_phone.trim(),
    admin_email: registerInfo.admin_email.trim(),
    address: registerInfo.address.trim(),
  }), [registerInfo]);

  const canSaveConfig = Boolean(trimmedConfig.base_url);
  const canRegister = canSaveConfig && Boolean(trimmedRegisterInfo.company_name && trimmedRegisterInfo.admin_email);

  const statusLabel = useMemo(() => {
    if (!status) return text.unknown;
    if (!status.configured) return text.notConfigured;
    if (!status.registered) return text.readyRegister;
    if (status.status === 'licensed') return text.licensed;
    if (status.status === 'pending') return text.pendingLicense;
    if (status.status === 'offline') return text.offline;
    return status.status || text.registered;
  }, [status]);

  const steps = useMemo(() => ([
    { label: text.stepConfig, done: Boolean(status?.configured || config.base_url), detail: config.base_url || text.noCloudUrl },
    { label: text.stepRegister, done: Boolean(status?.registered), detail: status?.center_id || text.submitCompany },
    { label: text.stepLicense, done: status?.status === 'licensed', detail: status?.status === 'licensed' ? licenseSummary(status.license) : text.waitingLicense },
    { label: text.stepLocal, done: Boolean(status?.non_blocking ?? true), detail: text.localDetail },
  ]), [config.base_url, status]);

  const load = async () => {
    try {
      const [cfg, st] = await Promise.all([fetchCloudConfig(), fetchCloudStatus().catch(() => null)]);
      setConfig({ ...defaultConfig, ...cfg });
      setRegisterInfo(prev => {
        const next = { ...prev, company_name: prev.company_name || cfg.registration_name || '', admin_email: prev.admin_email || cfg.registration_email || '' };
        saveDraft(next);
        return next;
      });
      if (st) setStatus(st);
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : 'Load failed' });
    }
  };

  useEffect(() => { void load(); }, []);

  const update = (patch: Partial<CloudConfig>) => {
    setNotice(null);
    setConfig(prev => ({ ...prev, ...patch }));
  };

  const updateRegisterInfo = (patch: Partial<RegisterInfo>) => setRegisterInfo(prev => {
    setNotice(null);
    const next = { ...prev, ...patch };
    saveDraft(next);
    return next;
  });

  const save = async () => {
    setBusy(true);
    setNotice(null);
    try {
      if (!canSaveConfig) {
        setNotice({ tone: 'danger', text: text.needCloudUrl });
        return;
      }
      const saved = await saveCloudConfig(trimmedConfig);
      setConfig(saved);
      setNotice({ tone: 'ok', text: text.configSaved });
      await load();
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : 'Save failed' });
    } finally {
      setBusy(false);
    }
  };

  const register = async () => {
    setBusy(true);
    setNotice(null);
    try {
      if (!canRegister) {
        setNotice({ tone: 'danger', text: text.needRegister });
        return;
      }
      const resp = await registerCenterToCloud(trimmedRegisterInfo);
      setNotice({ tone: 'ok', text: text.registeredPrefix + resp.center_id + text.registeredSuffix });
      await load();
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : 'Registration failed' });
    } finally {
      setBusy(false);
    }
  };

  const refreshLicense = async () => {
    setBusy(true);
    setNotice(null);
    setLicenseText('');
    try {
      const lic = await fetchCloudLicense();
      setLicenseText(licenseSummary(lic));
      await load();
    } catch (err) {
      setNotice({ tone: 'warn', text: err instanceof Error ? err.message : 'License refresh failed' });
      await load();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title={text.title} desc={text.desc}>
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label={text.status} value={statusLabel} tone={tileTone(status)} />
          <StatusTile label={text.centerId} value={status?.center_id || '-'} />
          <StatusTile label={text.controlMode} value={modeLabels[config.cloud_control_mode] || config.cloud_control_mode || '-'} />
          <StatusTile label={text.boundary} value={status?.business_scope || text.localBusiness} tone="ok" />
        </div>

        <div className="cloud-step-list">
          {steps.map(step => (
            <div key={step.label} className={'cloud-step ' + (step.done ? 'is-done' : 'is-pending')}>
              <span>{step.done ? text.done : text.pending}</span>
              <strong>{step.label}</strong>
              <p>{step.detail}</p>
            </div>
          ))}
        </div>

        <div className="cloud-continuity-grid">
          <article className="cloud-continuity-card ok"><strong>{text.continuityTitle}</strong><p>{text.continuityDesc}</p></article>
          <article className="cloud-continuity-card"><strong>{text.isolationTitle}</strong><p>{text.isolationDesc}</p></article>
        </div>

        <div className="cloud-form-section-title"><strong>{text.configTitle}</strong><span>{text.draftHint}</span></div>
        <div className="cloud-form-grid">
          <Field label="iWorkerCloud URL" value={config.base_url} placeholder="http://127.0.0.1:9366" onChange={v => update({ base_url: v })} />
          <Field label="Center public URL" value={config.center_base_url} placeholder="http://127.0.0.1:9377" onChange={v => update({ center_base_url: v })} />
          <Field label="Registration name" value={config.registration_name} placeholder="HQ iWorkerCenter" onChange={v => update({ registration_name: v })} />
          <Field label="Registration email" value={config.registration_email} placeholder="admin@example.com" onChange={v => update({ registration_email: v })} />
          <label className="cloud-field"><span>Control mode</span><select value={config.cloud_control_mode} onChange={e => update({ cloud_control_mode: e.target.value })}><option value="cloud_managed">Cloud managed</option><option value="hybrid">Hybrid</option><option value="self_managed">Self managed</option></select></label>
        </div>

        <div className="cloud-form-section-title"><strong>{text.registerTitle}</strong></div>
        <div className="cloud-form-grid cloud-registration-grid">
          <Field label="Company name" value={registerInfo.company_name} placeholder="Acme Inc" onChange={v => updateRegisterInfo({ company_name: v })} />
          <Field label="Legal person" value={registerInfo.legal_person} placeholder="Jane Doe" onChange={v => updateRegisterInfo({ legal_person: v })} />
          <Field label="Contact phone" value={registerInfo.admin_phone} placeholder="+86 13800000000" onChange={v => updateRegisterInfo({ admin_phone: v })} />
          <Field label="Admin email" value={registerInfo.admin_email} placeholder="admin@example.com" onChange={v => updateRegisterInfo({ admin_email: v })} />
          <Field label="Company address" value={registerInfo.address} placeholder="Company registered address" onChange={v => updateRegisterInfo({ address: v })} />
        </div>

        <div className="cloud-actions">
          <button className="cloud-primary" type="button" onClick={save} disabled={busy || !canSaveConfig}>{busy ? text.busy : text.save}</button>
          <button className="ghost" type="button" onClick={register} disabled={busy || !canRegister}>{busy ? text.busy : text.register}</button>
          <button className="ghost" type="button" onClick={refreshLicense} disabled={busy || !status?.registered}>{busy ? text.busy : text.refreshLicense}</button>
        </div>
        {notice ? <p className={'cloud-message ' + notice.tone}>{notice.text}</p> : null}
        {licenseText ? <p className="cloud-message ok">{text.license}: {licenseText}</p> : null}
        {status?.license_error ? <p className="cloud-message warn">{text.cloudStatus}: {status.license_error}</p> : null}
      </SectionCard>
    </div>
  );
}

function Field({ label, value, placeholder, onChange }: { label: string; value: string; placeholder?: string; onChange: (value: string) => void }) {
  return <label className="cloud-field"><span>{label}</span><input value={value || ''} placeholder={placeholder} onChange={e => onChange(e.target.value)} /></label>;
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
