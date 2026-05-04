import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';
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
type Labels = ReturnType<typeof createLabels>;

const draftKey = 'iworkercenter.cloud.registrationDraft';
const emptyRegisterInfo: RegisterInfo = { company_name: '', legal_person: '', admin_phone: '', admin_email: '', address: '' };

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

const createLabels = (t: (zh: string, en: string) => string) => ({
  title: t('Cloud 注册与授权', 'Cloud Registration & Licensing'),
  desc: t('将本 iWorkerCenter 注册到 iWorkerCloud。Cloud 只负责注册、授权、算力和能力市场协调，不参与企业业务运行。', 'Register this iWorkerCenter with iWorkerCloud. Cloud only coordinates registration, licensing, compute, and the capability marketplace; it does not participate in enterprise business workflows.'),
  status: t('连接状态', 'Connection Status'),
  centerId: 'Center ID',
  controlMode: t('控制模式', 'Control Mode'),
  boundary: t('业务边界', 'Business Boundary'),
  unknown: t('未知', 'Unknown'),
  notConfigured: t('未配置', 'Not Configured'),
  readyRegister: t('可以注册', 'Ready to Register'),
  licensed: t('已授权', 'Licensed'),
  pendingLicense: t('等待授权', 'Pending License'),
  offline: t('Cloud 离线', 'Cloud Offline'),
  credentialMismatch: t('凭据失效', 'Credential Mismatch'),
  registered: t('已注册', 'Registered'),
  done: t('完成', 'Done'),
  pending: t('待处理', 'Pending'),
  stepConfig: t('1. 配置 Cloud 地址', '1. Configure Cloud URL'),
  stepRegister: t('2. 注册 Center', '2. Register Center'),
  stepLicense: t('3. 确认授权', '3. Confirm License'),
  stepLocal: t('4. 本地业务可用', '4. Local Work Remains Available'),
  noCloudUrl: t('尚未配置 iWorkerCloud URL', 'iWorkerCloud URL is not configured.'),
  submitCompany: t('提交企业信息后生成 Center ID 和密钥', 'Submit company information to generate the Center ID and secret.'),
  waitingLicense: t('等待 Cloud 管理员授权确认', 'Waiting for the Cloud administrator to approve a license.'),
  credentialMismatchDetail: t('Cloud 返回凭据无效，说明本地保存的 Center Secret 与 Cloud 侧记录不匹配。请重新注册到 Cloud，Cloud 会按 machine_id + company_id 复用原有注册单位并修复本地凭据。', 'Cloud rejected the stored credentials. The local Center Secret no longer matches the Cloud record. Register with Cloud again; Cloud will reuse the existing registration by machine_id + company_id and repair the local credential.'),
  approvedDetail: t('已批准并取得有效授权，Center 可以使用已授权的 Cloud 平台能力。', 'Approved with an active license. Center can use the licensed Cloud platform capabilities.'),
  computeTitle: t('算力分发', 'Compute Distribution'),
  computeAllowed: t('已允许', 'Allowed'),
  computeBlocked: t('未允许', 'Blocked'),
  providerCount: t('Provider 数', 'Providers'),
  forceSync: t('强制同步', 'Force Sync'),
  pendingLicenseDetail: t('已注册，等待 iWorkerCloud 管理员在 Cloud 管理台确认并发放授权。Cloud 返回 no active license 表示尚未生成有效授权记录，不影响 Center 与 iWorker 的本地业务运行。', 'Registered and waiting for an iWorkerCloud administrator to approve and issue a license. A Cloud response of no active license means no active license record exists yet; local Center and iWorker work is not blocked.'),
  offlineDetail: t('已注册，但当前无法连接 iWorkerCloud。Center 与 iWorker 会继续按本地策略运行；Cloud 恢复后再同步授权、算力和能力市场状态。', 'Registered, but iWorkerCloud is currently unreachable. Center and iWorker continue by local policy; licensing, compute, and marketplace state sync after Cloud recovers.'),
  localDetail: t('Cloud 故障不会阻断 Center 到 iWorker 的本地任务、记忆和已下发能力。', 'Cloud failures do not block local tasks, memory, or delivered capabilities between Center and iWorker.'),
  continuityTitle: t('离线连续性', 'Offline Continuity'),
  continuityDesc: t('iWorkerCloud 失联时，Center 仍按本地策略继续推送任务、提供记忆、管理 MCP/Skill 和支持人机协作。Cloud 恢复后再同步授权、算力和市场状态。', 'When iWorkerCloud is unavailable, Center continues pushing tasks, serving memory, managing MCP/Skill, and supporting human collaboration by local policy. Licensing, compute, and marketplace state sync after Cloud recovers.'),
  isolationTitle: t('业务隔离', 'Business Isolation'),
  isolationDesc: t('Cloud 不读取租户、员工、流程、会话和客户业务数据。这些仍属于 iWorkerCenter 本地管理边界。', 'Cloud does not read tenant, employee, workflow, conversation, or customer business data. Those stay inside the local iWorkerCenter boundary.'),
  configTitle: t('Cloud 连接配置', 'Cloud Connection'),
  registerTitle: t('Center 注册信息', 'Center Registration Information'),
  draftHint: t('注册表单会自动保存在本机，保存 Cloud 配置后不会丢失输入内容。', 'The registration form is auto-saved locally, so saving Cloud configuration will not clear your inputs.'),
  save: t('保存配置', 'Save Configuration'),
  register: t('注册到 Cloud', 'Register to Cloud'),
  refreshLicense: t('刷新授权', 'Refresh License'),
  busy: t('处理中...', 'Working...'),
  needCloudUrl: t('请先填写 iWorkerCloud URL。', 'Enter the iWorkerCloud URL first.'),
  needRegister: t('请先填写 iWorkerCloud URL、公司名称和管理员邮箱。', 'Enter the iWorkerCloud URL, company name, and admin email first.'),
  configSaved: t('Cloud 配置已保存，可以继续注册本 Center。', 'Cloud configuration saved. You can continue registering this Center.'),
  registeredPrefix: t('已向 iWorkerCloud 注册：', 'Registered with iWorkerCloud: '),
  registeredSuffix: t('。Center 已立即发送心跳，等待 Cloud 管理员授权确认。', '. Center has sent a heartbeat and is waiting for Cloud administrator license approval.'),
  license: t('授权', 'License'),
  cloudStatus: t('Cloud 状态', 'Cloud Status'),
  baseLicense: t('基础授权', 'Base License'),
  longTerm: t('长期有效', 'Long Term'),
  noExpiry: t('未设置到期日', 'No Expiry Date'),
  localBusiness: 'local_center_business',
  loadFailed: t('加载失败', 'Load failed'),
  saveFailed: t('保存失败', 'Save failed'),
  registrationFailed: t('注册失败', 'Registration failed'),
  licenseRefreshFailed: t('授权刷新失败', 'License refresh failed'),
  cloudUrl: t('iWorkerCloud 地址', 'iWorkerCloud URL'),
  centerUrl: t('Center 公开地址', 'Center Public URL'),
  registrationName: t('注册名称', 'Registration Name'),
  registrationEmail: t('注册邮箱', 'Registration Email'),
  cloudManaged: t('Cloud 管理', 'Cloud Managed'),
  hybrid: t('混合模式', 'Hybrid'),
  selfManaged: t('本地自管', 'Self Managed'),
  companyName: t('单位名称', 'Company Name'),
  legalPerson: t('法人姓名', 'Legal Person'),
  contactPhone: t('联系电话', 'Contact Phone'),
  adminEmail: t('管理员邮箱', 'Admin Email'),
  companyAddress: t('单位地址', 'Company Address'),
  companyPlaceholder: t('示例科技有限公司', 'Acme Inc'),
  legalPlaceholder: t('张三', 'Jane Doe'),
  addressPlaceholder: t('单位注册地址', 'Company registered address'),
  modules: {
    compute: t('算力', 'Compute'),
    skill_market: t('技能市场', 'Skill Market'),
    upgrade: t('升级', 'Upgrade'),
    support: t('支持', 'Support'),
    all: t('全部模块', 'All Modules'),
  },
  modes: {
    cloud_managed: t('Cloud 管理', 'Cloud Managed'),
    hybrid: t('混合模式', 'Hybrid'),
    self_managed: t('本地自管', 'Self Managed'),
  },
});

const licenseSummary = (license: CloudLicense | undefined, labels: Labels) => {
  if (!license) return labels.waitingLicense;
  const modules = parseModules(license.modules).map(module => labels.modules[module as keyof Labels['modules']] || module);
  const scope = modules.length ? modules.join(', ') : labels.baseLicense;
  const expiry = license.is_long_term ? labels.longTerm : (license.expires_at || labels.noExpiry);
  return (license.type || 'license') + ' / ' + scope + ' / ' + expiry;
};

const isCredentialMismatchError = (message?: string) => {
  const normalized = (message || '').toLowerCase();
  return normalized.includes('auth_failed') || normalized.includes('invalid center credentials') || normalized.includes('status 401') || normalized.includes('status 403');
};

const isPendingLicenseError = (message?: string) => {
  const normalized = (message || '').toLowerCase();
  return normalized.includes('not_found') || normalized.includes('no active license') || normalized.includes('status 404') || normalized.includes('not found');
};

const tileTone = (status: CloudStatus | null): 'ok' | 'warn' => {
  if (!status?.configured) return 'warn';
  if (status.status === 'offline' || status.status === 'pending') return 'warn';
  return 'ok';
};

const registrationStatusDetail = (status: CloudStatus | null, labels: Labels) => {
  if (!status?.registered) return labels.submitCompany;
  if (status.status === 'licensed') return labels.approvedDetail;
  if (status.status === 'credential_mismatch') return labels.credentialMismatchDetail;
  if (status.status === 'offline') return labels.offlineDetail;
  return labels.pendingLicenseDetail;
};

export function CloudRegistrationPage() {
  const { t } = useI18n();
  const text = useMemo(() => createLabels(t), [t]);
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
  const hasRegisterInfo = Boolean(trimmedRegisterInfo.company_name && trimmedRegisterInfo.admin_email);
  const canRegister = canSaveConfig && hasRegisterInfo;

  const statusLabel = useMemo(() => {
    if (!status) return text.unknown;
    if (!status.configured) return text.notConfigured;
    if (!status.registered) return text.readyRegister;
    if (status.status === 'licensed') return text.licensed;
    if (status.status === 'pending') return text.pendingLicense;
    if (status.status === 'offline') return text.offline;
    if (status.status === 'credential_mismatch') return text.credentialMismatch;
    return status.status || text.registered;
  }, [status, text]);

  const steps = useMemo(() => ([
    { label: text.stepConfig, done: Boolean(status?.configured || config.base_url), detail: config.base_url || text.noCloudUrl },
    { label: text.stepRegister, done: Boolean(status?.registered), detail: status?.center_id || text.submitCompany },
    { label: text.stepLicense, done: status?.status === 'licensed', detail: status?.status === 'licensed' ? licenseSummary(status.license, text) : text.waitingLicense },
    { label: text.stepLocal, done: Boolean(status?.non_blocking ?? true), detail: text.localDetail },
  ]), [config.base_url, status, text]);

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
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : text.loadFailed });
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
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : text.saveFailed });
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
      await saveCloudConfig(trimmedConfig);
      const resp = await registerCenterToCloud(trimmedRegisterInfo);
      setNotice({ tone: 'ok', text: text.registeredPrefix + resp.center_id + text.registeredSuffix });
      await load();
    } catch (err) {
      setNotice({ tone: 'danger', text: err instanceof Error ? err.message : text.registrationFailed });
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
      setLicenseText(licenseSummary(lic, text));
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : text.licenseRefreshFailed;
      setNotice({ tone: 'warn', text: isCredentialMismatchError(message) ? text.credentialMismatchDetail : isPendingLicenseError(message) ? text.pendingLicenseDetail : message });
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
          <StatusTile label={text.controlMode} value={text.modes[config.cloud_control_mode as keyof Labels['modes']] || config.cloud_control_mode || '-'} />
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

        {status?.registered ? <div className="cloud-registration-insight">
          <article className={status.status === 'licensed' ? 'ok' : status.status === 'credential_mismatch' ? 'danger' : 'warn'}>
            <span>{text.stepLicense}</span>
            <strong>{statusLabel}</strong>
            <p>{registrationStatusDetail(status, text)}</p>
          </article>
          <article className={status.status === 'licensed' ? 'ok' : 'warn'}>
            <span>{text.license}</span>
            <strong>{licenseSummary(status.license, text)}</strong>
            <p>{status.license ? status.license.id : text.waitingLicense}</p>
          </article>
          <article className={status.compute?.compute_permission ? 'ok' : 'warn'}>
            <span>{text.computeTitle}</span>
            <strong>{status.compute?.compute_permission ? text.computeAllowed : text.computeBlocked}</strong>
            <p>{text.providerCount}: {status.compute?.provider_count ?? 0}{status.compute?.force_sync ? ' / ' + text.forceSync : ''}{status.compute?.error ? ' / ' + status.compute.error : ''}</p>
          </article>
        </div> : null}

        <div className="cloud-continuity-grid">
          <article className="cloud-continuity-card ok"><strong>{text.continuityTitle}</strong><p>{text.continuityDesc}</p></article>
          <article className="cloud-continuity-card"><strong>{text.isolationTitle}</strong><p>{text.isolationDesc}</p></article>
        </div>

        <div className="cloud-form-section-title"><strong>{text.configTitle}</strong><span>{text.draftHint}</span></div>
        <div className="cloud-form-grid">
          <Field label={text.cloudUrl} value={config.base_url} placeholder="http://127.0.0.1:9366" onChange={v => update({ base_url: v })} />
          <Field label={text.centerUrl} value={config.center_base_url} placeholder="http://127.0.0.1:9377" onChange={v => update({ center_base_url: v })} />
          <Field label={text.registrationName} value={config.registration_name} placeholder="HQ iWorkerCenter" onChange={v => update({ registration_name: v })} />
          <Field label={text.registrationEmail} value={config.registration_email} placeholder="admin@example.com" onChange={v => update({ registration_email: v })} />
          <label className="cloud-field"><span>{text.controlMode}</span><select value={config.cloud_control_mode} onChange={e => update({ cloud_control_mode: e.target.value })}><option value="cloud_managed">{text.cloudManaged}</option><option value="hybrid">{text.hybrid}</option><option value="self_managed">{text.selfManaged}</option></select></label>
        </div>

        <div className="cloud-form-section-title"><strong>{text.registerTitle}</strong></div>
        <div className="cloud-form-grid cloud-registration-grid">
          <Field label={text.companyName} value={registerInfo.company_name} placeholder={text.companyPlaceholder} onChange={v => updateRegisterInfo({ company_name: v })} />
          <Field label={text.legalPerson} value={registerInfo.legal_person} placeholder={text.legalPlaceholder} onChange={v => updateRegisterInfo({ legal_person: v })} />
          <Field label={text.contactPhone} value={registerInfo.admin_phone} placeholder="+86 13800000000" onChange={v => updateRegisterInfo({ admin_phone: v })} />
          <Field label={text.adminEmail} value={registerInfo.admin_email} placeholder="admin@example.com" onChange={v => updateRegisterInfo({ admin_email: v })} />
          <Field label={text.companyAddress} value={registerInfo.address} placeholder={text.addressPlaceholder} onChange={v => updateRegisterInfo({ address: v })} />
        </div>

        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={save} disabled={busy || !canSaveConfig}>{busy ? text.busy : text.save}</button>
          <button className={canRegister ? 'cloud-primary' : 'ghost'} type="button" onClick={register} disabled={busy || !canRegister}>{busy ? text.busy : text.register}</button>
          <button className="ghost" type="button" onClick={refreshLicense} disabled={busy || !status?.registered}>{busy ? text.busy : text.refreshLicense}</button>
        </div>
        {notice ? <p className={'cloud-message ' + notice.tone}>{notice.text}</p> : null}
        {licenseText ? <p className="cloud-message ok">{text.license}: {licenseText}</p> : null}
        {status?.registered && status.status === 'pending' ? <p className="cloud-message warn">{text.pendingLicenseDetail}</p> : null}
        {status?.registered && status.status === 'credential_mismatch' ? <p className="cloud-message warn">{text.credentialMismatchDetail}</p> : null}
        {status?.registered && status.status === 'offline' ? <p className="cloud-message warn">{text.offlineDetail}</p> : null}
        {status?.license_error && status.status !== 'pending' && status.status !== 'credential_mismatch' ? <p className="cloud-message warn">{text.cloudStatus}: {status.license_error}</p> : null}
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
