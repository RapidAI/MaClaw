import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';
import { fetchCloudConfig, fetchCloudLicense, fetchCloudStatus, registerCenterToCloud, saveCloudConfig, type CloudConfig, type CloudLicense, type CloudStatus, type RegisterCloudResponse } from '../api/cloud';
import { fetchRuntimeStatus, type RuntimeStatus } from '../api/runtime';

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

const draftKeyPrefix = 'iworkercenter.cloud.registrationDraft';
const emptyRegisterInfo: RegisterInfo = { company_name: '', legal_person: '', admin_phone: '', admin_email: '', address: '' };
const draftKey = (companyID?: string) => companyID ? draftKeyPrefix + '.' + companyID : draftKeyPrefix;
const hasDraftValue = (info: RegisterInfo) => Object.values(info).some(value => value.trim());

const loadDraft = (companyID?: string): RegisterInfo => {
  if (typeof window === 'undefined') return emptyRegisterInfo;
  try {
    const raw = window.localStorage.getItem(draftKey(companyID));
    return raw ? { ...emptyRegisterInfo, ...JSON.parse(raw) } : emptyRegisterInfo;
  } catch {
    return emptyRegisterInfo;
  }
};

const saveDraft = (info: RegisterInfo, companyID?: string) => {
  if (typeof window !== 'undefined') window.localStorage.setItem(draftKey(companyID), JSON.stringify(info));
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
  title: t('\u4e91\u7aef\u6ce8\u518c\u4e0e\u6388\u6743', 'Cloud Registration & Licensing'),
  desc: t('\u5c06\u672c iWorkerCenter \u6ce8\u518c\u5230 iWorkerCloud\u3002Cloud \u53ea\u8d1f\u8d23\u6ce8\u518c\u3001\u6388\u6743\u3001\u7b97\u529b\u548c\u80fd\u529b\u5e02\u573a\u534f\u8c03\uff0c\u4e0d\u53c2\u4e0e\u4f01\u4e1a\u4e1a\u52a1\u8fd0\u884c\u3002', 'Register this iWorkerCenter with iWorkerCloud. Cloud only coordinates registration, licensing, compute, and the capability marketplace; it does not participate in enterprise business workflows.'),
  status: t('\u8fde\u63a5\u72b6\u6001', 'Connection Status'),
  centerId: 'Center ID',
  machineId: t('\u673a\u5668 ID', 'Machine ID'),
  companyId: t('\u5355\u4f4d ID', 'Company ID'),
  registrationIdentity: t('\u6ce8\u518c\u53bb\u91cd\u8eab\u4efd', 'Registration Dedupe Identity'),
  registrationIdentityDesc: t('Cloud \u4f7f\u7528 machine_id + company_id \u8bc6\u522b\u540c\u4e00\u4e2a\u63a5\u5165\u5355\u4f4d\uff0c\u91cd\u590d\u63d0\u4ea4\u4f1a\u590d\u7528\u539f\u6709 Center \u8bb0\u5f55\u5e76\u4fee\u590d\u672c\u5730\u51ed\u636e\u3002', 'Cloud identifies one registration unit by machine_id + company_id. Repeated submissions reuse the existing Center record and repair local credentials.'),
  registrationIdentityPending: t('\u4fdd\u5b58 Cloud \u914d\u7f6e\u540e\u53ef\u83b7\u53d6\u673a\u5668 ID \u548c\u5355\u4f4d ID\u3002', 'Save Cloud configuration to obtain the machine ID and company ID.'),
  registrationTuple: t('\u552f\u4e00\u7ec4\u5408', 'Unique tuple'),
  controlMode: t('\u63a7\u5236\u6a21\u5f0f', 'Control Mode'),
  boundary: t('\u4e1a\u52a1\u8fb9\u754c', 'Business Boundary'),
  heartbeat: t('Cloud \u5fc3\u8df3', 'Cloud Heartbeat'),
  nonBlocking: t('\u975e\u963b\u585e', 'Non-blocking'),
  blocking: t('\u963b\u585e', 'Blocking'),
  failures: t('\u6b21\u5931\u8d25', 'failures'),
  businessImpactNone: t('\u4e1a\u52a1\u5f71\u54cd\uff1a\u65e0', 'Business impact: none'),
  localPathContinues: t('Center/iWorker \u672c\u5730\u94fe\u8def\u7ee7\u7eed\u8fd0\u884c\u3002', 'The local Center/iWorker path continues running.'),
  heartbeatWaiting: t('\u6682\u65e0\u5fc3\u8df3\u5feb\u7167', 'No heartbeat snapshot yet'),
  unknown: t('\u672a\u77e5', 'Unknown'),
  notConfigured: t('\u672a\u914d\u7f6e', 'Not Configured'),
  readyRegister: t('\u53ef\u4ee5\u6ce8\u518c', 'Ready to Register'),
  licensed: t('\u5df2\u6388\u6743', 'Licensed'),
  pendingLicense: t('\u7b49\u5f85\u6388\u6743', 'Pending License'),
  offline: t('Cloud \u79bb\u7ebf', 'Cloud Offline'),
  credentialMismatch: t('\u51ed\u636e\u5931\u6548', 'Credential Mismatch'),
  registered: t('\u5df2\u6ce8\u518c', 'Registered'),
  done: t('\u5b8c\u6210', 'Done'),
  pending: t('\u5f85\u5904\u7406', 'Pending'),
  stepConfig: t('1. \u914d\u7f6e Cloud \u5730\u5740', '1. Configure Cloud URL'),
  stepRegister: t('2. \u6ce8\u518c Center', '2. Register Center'),
  stepLicense: t('3. \u786e\u8ba4\u6388\u6743', '3. Confirm License'),
  stepLocal: t('4. \u672c\u5730\u4e1a\u52a1\u53ef\u7528', '4. Local Work Remains Available'),
  noCloudUrl: t('\u5c1a\u672a\u914d\u7f6e iWorkerCloud URL', 'iWorkerCloud URL is not configured.'),
  submitCompany: t('\u63d0\u4ea4\u4f01\u4e1a\u4fe1\u606f\u540e\u751f\u6210 Center ID \u548c\u5bc6\u94a5\u3002', 'Submit company information to generate the Center ID and secret.'),
  waitingLicense: t('\u7b49\u5f85 Cloud \u7ba1\u7406\u5458\u6388\u6743\u786e\u8ba4\u3002', 'Waiting for the Cloud administrator to approve a license.'),
  credentialMismatchDetail: t('Cloud \u8fd4\u56de\u51ed\u636e\u65e0\u6548\uff0c\u8bf4\u660e\u672c\u5730\u4fdd\u5b58\u7684 Center Secret \u4e0e Cloud \u4fa7\u8bb0\u5f55\u4e0d\u5339\u914d\u3002\u8bf7\u91cd\u65b0\u6ce8\u518c\u5230 Cloud\uff0cCloud \u4f1a\u6309 machine_id + company_id \u590d\u7528\u539f\u6709\u6ce8\u518c\u5355\u4f4d\u5e76\u4fee\u590d\u672c\u5730\u51ed\u636e\u3002', 'Cloud rejected the stored credentials. The local Center Secret no longer matches the Cloud record. Register with Cloud again; Cloud will reuse the existing registration by machine_id + company_id and repair the local credential.'),
  approvedDetail: t('\u5df2\u6279\u51c6\u5e76\u53d6\u5f97\u6709\u6548\u6388\u6743\uff0cCenter \u53ef\u4ee5\u4f7f\u7528\u5df2\u6388\u6743\u7684 Cloud \u5e73\u53f0\u80fd\u529b\u3002', 'Approved with an active license. Center can use the licensed Cloud platform capabilities.'),
  computeTitle: t('\u7b97\u529b\u5206\u53d1', 'Compute Distribution'),
  cachedSnapshot: t('\u672c\u5730\u7f13\u5b58\u5feb\u7167', 'Local cached snapshot'),
  cachedLicense: t('\u4f7f\u7528\u4e0a\u6b21\u6210\u529f\u6388\u6743\u5feb\u7167', 'Using last successful license snapshot'),
  cachedCompute: t('\u4f7f\u7528\u4e0a\u6b21\u6210\u529f\u7b97\u529b\u5feb\u7167', 'Using last successful compute snapshot'),
  cacheError: t('\u672c\u5730\u7f13\u5b58\u5f02\u5e38', 'Local cache error'),
  computeAllowed: t('\u5df2\u5141\u8bb8', 'Allowed'),
  computeBlocked: t('\u672a\u5141\u8bb8', 'Blocked'),
  providerCount: t('Provider \u6570', 'Providers'),
  forceSync: t('\u5f3a\u5236\u540c\u6b65', 'Force Sync'),
  pendingLicenseDetail: t('\u5df2\u6ce8\u518c\uff0c\u7b49\u5f85 iWorkerCloud \u7ba1\u7406\u5458\u5728 Cloud \u7ba1\u7406\u53f0\u786e\u8ba4\u5e76\u53d1\u653e\u6388\u6743\u3002Cloud \u8fd4\u56de no active license \u8868\u793a\u5c1a\u672a\u751f\u6210\u6709\u6548\u6388\u6743\u8bb0\u5f55\uff0c\u4e0d\u5f71\u54cd Center \u4e0e iWorker \u7684\u672c\u5730\u4e1a\u52a1\u8fd0\u884c\u3002', 'Registered and waiting for an iWorkerCloud administrator to approve and issue a license. A Cloud response of no active license means no active license record exists yet; local Center and iWorker work is not blocked.'),
  offlineDetail: t('\u5df2\u6ce8\u518c\uff0c\u4f46\u5f53\u524d\u65e0\u6cd5\u8fde\u63a5 iWorkerCloud\u3002Center \u4e0e iWorker \u4f1a\u7ee7\u7eed\u6309\u672c\u5730\u7b56\u7565\u8fd0\u884c\uff1b\u82e5\u5df2\u6210\u529f\u83b7\u53d6\u8fc7\u6388\u6743\u548c\u7b97\u529b\uff0c\u754c\u9762\u4f1a\u663e\u793a\u672c\u5730\u7f13\u5b58\u5feb\u7167\u3002Cloud \u6062\u590d\u540e\u518d\u540c\u6b65\u6388\u6743\u3001\u7b97\u529b\u548c\u80fd\u529b\u5e02\u573a\u72b6\u6001\u3002', 'Registered, but iWorkerCloud is currently unreachable. Center and iWorker continue by local policy; when a successful license or compute sync exists, this page shows the local cached snapshot. Licensing, compute, and marketplace state sync after Cloud recovers.'),
  localDetail: t('Cloud \u6545\u969c\u4e0d\u4f1a\u963b\u65ad Center \u5230 iWorker \u7684\u672c\u5730\u4efb\u52a1\u3001\u8bb0\u5fc6\u548c\u5df2\u4e0b\u53d1\u80fd\u529b\u3002', 'Cloud failures do not block local tasks, memory, or delivered capabilities between Center and iWorker.'),
  nextActionTitle: t('\u5efa\u8bae\u4e0b\u4e00\u6b65', 'Recommended Next Step'),
  nextSaveConfig: t('\u5148\u4fdd\u5b58 Cloud \u8fde\u63a5\u914d\u7f6e', 'Save the Cloud connection first'),
  nextSaveConfigDetail: t('\u586b\u5199 iWorkerCloud \u5730\u5740\u540e\u4fdd\u5b58\u3002\u672a\u4fdd\u5b58\u524d\uff0cCenter \u4e0d\u4f1a\u5c1d\u8bd5\u6ce8\u518c\u6216\u62c9\u53d6\u6388\u6743\u3002', 'Enter and save the iWorkerCloud URL before Center attempts registration or license sync.'),
  nextRegister: t('\u63d0\u4ea4 Center \u6ce8\u518c', 'Submit Center registration'),
  nextRegisterDetail: t('\u5355\u4f4d\u540d\u79f0\u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u5df2\u5c31\u7eea\u3002\u6ce8\u518c\u540e Cloud \u4f1a\u8fd4\u56de Center ID \u548c\u5bc6\u94a5\uff0c\u5e76\u7b49\u5f85 Cloud \u7ba1\u7406\u5458\u6388\u6743\u3002', 'Company name and admin email are ready. Registration returns a Center ID and secret, then waits for Cloud administrator licensing.'),
  nextWaitLicense: t('\u7b49\u5f85 Cloud \u6388\u6743\uff0c\u5e76\u5b9a\u671f\u5237\u65b0', 'Wait for Cloud authorization and refresh'),
  nextWaitLicenseDetail: t('\u672c\u5730 Center/iWorker \u53ef\u7ee7\u7eed\u5de5\u4f5c\u3002Cloud \u7ba1\u7406\u5458\u6279\u51c6\u540e\uff0c\u70b9\u51fb\u5237\u65b0\u6388\u6743\u5373\u53ef\u67e5\u770b\u6a21\u5757\u548c\u7b97\u529b\u6743\u76ca\u3002', 'Local Center/iWorker work can continue. After the Cloud administrator approves, refresh license to view module and compute entitlements.'),
  nextRepairCredential: t('\u91cd\u65b0\u6ce8\u518c\u4ee5\u4fee\u590d\u51ed\u636e', 'Register again to repair credentials'),
  nextRepairCredentialDetail: t('Cloud \u5df2\u62d2\u7edd\u672c\u5730\u5bc6\u94a5\u3002\u4f7f\u7528\u5f53\u524d machine_id + company_id \u91cd\u65b0\u6ce8\u518c\uff0cCloud \u4f1a\u590d\u7528\u539f\u6ce8\u518c\u5355\u4f4d\u5e76\u4e0b\u53d1\u65b0\u5bc6\u94a5\u3002', 'Cloud rejected the stored secret. Register again with the same machine_id + company_id; Cloud will reuse the existing unit and issue a repaired secret.'),
  nextCloudOffline: t('Cloud \u79bb\u7ebf\uff0c\u5148\u4fdd\u6301\u672c\u5730\u8fd0\u884c', 'Cloud is offline; keep local work running'),
  nextCloudOfflineDetail: t('Cloud \u5931\u8054\u53ea\u5f71\u54cd\u63a7\u5236\u9762\u540c\u6b65\u3002\u5148\u786e\u8ba4\u672c\u5730 iWorker \u3001MCP/Skill \u548c\u4efb\u52a1\u63a8\u9001\u6b63\u5e38\uff0cCloud \u6062\u590d\u540e\u518d\u5237\u65b0\u6388\u6743\u3002', 'Cloud loss only affects control-plane sync. Keep local iWorker, MCP/Skill, and pushed tasks running; refresh licensing after Cloud recovers.'),
  nextReady: t('\u5df2\u5c31\u7eea\uff0c\u68c0\u67e5\u7b97\u529b\u548c\u80fd\u529b\u4e0b\u53d1', 'Ready; review compute and capability rollout'),
  nextReadyDetail: t('\u6388\u6743\u5df2\u751f\u6548\u3002\u53ef\u7ee7\u7eed\u68c0\u67e5 Cloud \u7b97\u529b\u3001MCP/Skill \u4e0b\u53d1\u548c iWorker \u5ba2\u6237\u7aef\u5b89\u88c5\u72b6\u6001\u3002', 'License is active. Continue reviewing Cloud compute, MCP/Skill delivery, and iWorker client installation status.'),
  nextBlocked: t('\u8865\u9f50\u5fc5\u586b\u4fe1\u606f', 'Complete required information'),
  nextBlockedDetail: t('\u8bf7\u8865\u9f50 iWorkerCloud URL\u3001\u5355\u4f4d\u540d\u79f0\u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u3002', 'Fill in the iWorkerCloud URL, company name, and admin email.'),
  continuityTitle: t('\u79bb\u7ebf\u8fde\u7eed\u6027', 'Offline Continuity'),
  continuityDesc: t('iWorkerCloud \u5931\u8054\u65f6\uff0cCenter \u4ecd\u6309\u672c\u5730\u7b56\u7565\u7ee7\u7eed\u63a8\u9001\u4efb\u52a1\u3001\u63d0\u4f9b\u8bb0\u5fc6\u3001\u7ba1\u7406 MCP/Skill \u548c\u652f\u6301\u4eba\u673a\u534f\u4f5c\u3002Cloud \u6062\u590d\u540e\u518d\u540c\u6b65\u6388\u6743\u3001\u7b97\u529b\u548c\u5e02\u573a\u72b6\u6001\u3002', 'When iWorkerCloud is unavailable, Center continues pushing tasks, serving memory, managing MCP/Skill, and supporting human collaboration by local policy. Licensing, compute, and marketplace state sync after Cloud recovers.'),
  isolationTitle: t('\u4e1a\u52a1\u9694\u79bb', 'Business Isolation'),
  isolationDesc: t('Cloud \u4e0d\u8bfb\u53d6\u79df\u6237\u3001\u5458\u5de5\u3001\u6d41\u7a0b\u3001\u4f1a\u8bdd\u548c\u5ba2\u6237\u4e1a\u52a1\u6570\u636e\u3002\u8fd9\u4e9b\u4ecd\u5c5e\u4e8e iWorkerCenter \u672c\u5730\u7ba1\u7406\u8fb9\u754c\u3002', 'Cloud does not read tenant, employee, workflow, conversation, or customer business data. Those stay inside the local iWorkerCenter boundary.'),
  configTitle: t('Cloud \u8fde\u63a5\u914d\u7f6e', 'Cloud Connection'),
  registerTitle: t('Center \u6ce8\u518c\u4fe1\u606f', 'Center Registration Information'),
  draftHint: t('\u6ce8\u518c\u8868\u5355\u4f1a\u81ea\u52a8\u4fdd\u5b58\u5728\u672c\u673a\uff0c\u4fdd\u5b58 Cloud \u914d\u7f6e\u540e\u4e0d\u4f1a\u4e22\u5931\u8f93\u5165\u5185\u5bb9\u3002', 'The registration form is auto-saved locally, so saving Cloud configuration will not clear your inputs.'),
  save: t('\u4fdd\u5b58\u914d\u7f6e', 'Save Configuration'),
  register: t('\u6ce8\u518c\u5230 Cloud', 'Register to Cloud'),
  refreshLicense: t('\u5237\u65b0\u6388\u6743', 'Refresh License'),
  busy: t('\u5904\u7406\u4e2d...', 'Working...'),
  needCloudUrl: t('\u8bf7\u5148\u586b\u5199 iWorkerCloud URL\u3002', 'Enter the iWorkerCloud URL first.'),
  needRegister: t('\u8bf7\u5148\u586b\u5199 iWorkerCloud URL\u3001\u516c\u53f8\u540d\u79f0\u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u3002', 'Enter the iWorkerCloud URL, company name, and admin email first.'),
  configSaved: t('Cloud \u914d\u7f6e\u5df2\u4fdd\u5b58\uff0c\u53ef\u4ee5\u7ee7\u7eed\u6ce8\u518c\u672c Center\u3002', 'Cloud configuration saved. You can continue registering this Center.'),
  registeredPrefix: t('\u5df2\u5411 iWorkerCloud \u6ce8\u518c\uff1a', 'Registered with iWorkerCloud: '),
  registeredSuffix: t('\u3002Center \u5df2\u7acb\u5373\u53d1\u9001\u5fc3\u8df3\uff0c\u7b49\u5f85 Cloud \u7ba1\u7406\u5458\u6388\u6743\u786e\u8ba4\u3002', '. Center has sent a heartbeat and is waiting for Cloud administrator license approval.'),
  reusedPrefix: t('Cloud \u5df2\u590d\u7528\u539f\u6709\u6ce8\u518c\u5355\u4f4d\uff1a', 'Cloud reused the existing registration unit: '),
  reusedSuffix: t('\u3002\u672c\u5730\u51ed\u636e\u5df2\u4fee\u590d\uff0cCenter \u5df2\u53d1\u9001\u5fc3\u8df3\u5e76\u7ee7\u7eed\u7b49\u5f85\u6388\u6743\u72b6\u6001\u3002', '. Local credentials were repaired, Center sent a heartbeat, and authorization status remains visible.'),
  registeredNoHeartbeatSuffix: t('\u3002\u6ce8\u518c\u51ed\u636e\u5df2\u4fdd\u5b58\uff0c\u4f46\u9996\u6b21 Cloud \u5fc3\u8df3\u5931\u8d25\uff1a', '. Registration credentials were saved, but the first Cloud heartbeat failed: '),
  reusedNoHeartbeatSuffix: t('\u3002\u672c\u5730\u51ed\u636e\u5df2\u4fee\u590d\uff0c\u4f46\u9996\u6b21 Cloud \u5fc3\u8df3\u5931\u8d25\uff1a', '. Local credentials were repaired, but the first Cloud heartbeat failed: '),
  license: t('\u6388\u6743', 'License'),
  cloudStatus: t('Cloud \u72b6\u6001', 'Cloud Status'),
  baseLicense: t('\u57fa\u7840\u6388\u6743', 'Base License'),
  longTerm: t('\u957f\u671f\u6709\u6548', 'Long Term'),
  noExpiry: t('\u672a\u8bbe\u7f6e\u5230\u671f\u65e5', 'No Expiry Date'),
  localBusiness: 'local_center_business',
  loadFailed: t('\u52a0\u8f7d\u5931\u8d25', 'Load failed'),
  saveFailed: t('\u4fdd\u5b58\u5931\u8d25', 'Save failed'),
  registrationFailed: t('\u6ce8\u518c\u5931\u8d25', 'Registration failed'),
  licenseRefreshFailed: t('\u6388\u6743\u5237\u65b0\u5931\u8d25', 'License refresh failed'),
  cloudUrl: t('iWorkerCloud \u5730\u5740', 'iWorkerCloud URL'),
  centerUrl: t('Center \u516c\u5f00\u5730\u5740', 'Center Public URL'),
  registrationName: t('\u6ce8\u518c\u540d\u79f0', 'Registration Name'),
  registrationEmail: t('\u6ce8\u518c\u90ae\u7bb1', 'Registration Email'),
  cloudManaged: t('Cloud \u7ba1\u7406', 'Cloud Managed'),
  hybrid: t('\u6df7\u5408\u6a21\u5f0f', 'Hybrid'),
  selfManaged: t('\u672c\u5730\u81ea\u7ba1', 'Self Managed'),
  companyName: t('\u5355\u4f4d\u540d\u79f0', 'Company Name'),
  legalPerson: t('\u6cd5\u4eba\u59d3\u540d', 'Legal Person'),
  contactPhone: t('\u8054\u7cfb\u7535\u8bdd', 'Contact Phone'),
  adminEmail: t('\u7ba1\u7406\u5458\u90ae\u7bb1', 'Admin Email'),
  companyAddress: t('\u5355\u4f4d\u5730\u5740', 'Company Address'),
  companyPlaceholder: t('\u793a\u4f8b\u79d1\u6280\u6709\u9650\u516c\u53f8', 'Acme Inc'),
  legalPlaceholder: t('\u5f20\u4e09', 'Jane Doe'),
  addressPlaceholder: t('\u5355\u4f4d\u6ce8\u518c\u5730\u5740', 'Company registered address'),
  registrationChecklist: t('\u6ce8\u518c\u524d\u68c0\u67e5', 'Pre-registration checklist'),
  requiredReady: t('\u5df2\u5c31\u7eea', 'Ready'),
  requiredMissing: t('\u5f85\u586b\u5199', 'Missing'),
  optionalReady: t('\u5df2\u586b\u5199', 'Provided'),
  optionalMissing: t('\u5efa\u8bae\u8865\u5145', 'Recommended'),
  modules: {
    compute: t('\u7b97\u529b', 'Compute'),
    capability_market: t('\u80fd\u529b\u5e02\u573a', 'Capability Market'),
    upgrade: t('\u5347\u7ea7', 'Upgrade'),
    support: t('\u652f\u6301', 'Support'),
    all: t('\u5168\u90e8\u6a21\u5757', 'All Modules'),
  },
  modes: {
    cloud_managed: t('Cloud \u7ba1\u7406', 'Cloud Managed'),
    hybrid: t('\u6df7\u5408\u6a21\u5f0f', 'Hybrid'),
    self_managed: t('\u672c\u5730\u81ea\u7ba1', 'Self Managed'),
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

const recommendedAction = (status: CloudStatus | null, canSaveConfig: boolean, canRegister: boolean, labels: Labels) => {
  if (!canSaveConfig) return { tone: 'danger' as const, title: labels.nextSaveConfig, detail: labels.nextSaveConfigDetail, action: 'save' as const };
  if (!status?.registered) {
    if (!canRegister) return { tone: 'danger' as const, title: labels.nextBlocked, detail: labels.nextBlockedDetail, action: 'none' as const };
    return { tone: 'warn' as const, title: labels.nextRegister, detail: labels.nextRegisterDetail, action: 'register' as const };
  }
  if (status.status === 'credential_mismatch') {
    if (!canRegister) return { tone: 'danger' as const, title: labels.nextBlocked, detail: labels.nextBlockedDetail, action: 'none' as const };
    return { tone: 'danger' as const, title: labels.nextRepairCredential, detail: labels.nextRepairCredentialDetail, action: 'register' as const };
  }
  if (status.status === 'offline') return { tone: 'warn' as const, title: labels.nextCloudOffline, detail: labels.nextCloudOfflineDetail, action: 'refresh' as const };
  if (status.status === 'licensed') return { tone: 'ok' as const, title: labels.nextReady, detail: labels.nextReadyDetail, action: 'refresh' as const };
  return { tone: 'warn' as const, title: labels.nextWaitLicense, detail: labels.nextWaitLicenseDetail, action: 'refresh' as const };
};

const registrationNoticeText = (resp: RegisterCloudResponse, labels: Labels) => {
  const prefix = resp.reused ? labels.reusedPrefix : labels.registeredPrefix;
  if (resp.heartbeat_sent) {
    return prefix + resp.center_id + (resp.reused ? labels.reusedSuffix : labels.registeredSuffix);
  }
  const suffix = resp.reused ? labels.reusedNoHeartbeatSuffix : labels.registeredNoHeartbeatSuffix;
  return prefix + resp.center_id + suffix + (resp.heartbeat_error || labels.heartbeatWaiting);
};

export function CloudRegistrationPage() {
  const { t } = useI18n();
  const text = useMemo(() => createLabels(t), [t]);
  const [config, setConfig] = useState<CloudConfig>(defaultConfig);
  const [status, setStatus] = useState<CloudStatus | null>(null);
  const [runtimeStatus, setRuntimeStatus] = useState<RuntimeStatus | null>(null);
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
  const registrationReadiness = [
    { label: text.cloudUrl, ready: Boolean(trimmedConfig.base_url), required: true },
    { label: text.companyName, ready: Boolean(trimmedRegisterInfo.company_name), required: true },
    { label: text.adminEmail, ready: Boolean(trimmedRegisterInfo.admin_email), required: true },
    { label: text.legalPerson, ready: Boolean(trimmedRegisterInfo.legal_person), required: false },
    { label: text.companyAddress, ready: Boolean(trimmedRegisterInfo.address), required: false },
  ];

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

  const actionPlan = useMemo(() => recommendedAction(status, canSaveConfig, canRegister, text), [status, canSaveConfig, canRegister, text]);
  const registrationTuple = status?.machine_id && status?.company_id
    ? `${status.machine_id} / ${status.company_id}`
    : '-';

  const steps = useMemo(() => ([
    { label: text.stepConfig, done: Boolean(status?.configured || config.base_url), detail: config.base_url || text.noCloudUrl },
    { label: text.stepRegister, done: Boolean(status?.registered), detail: status?.center_id || text.submitCompany },
    { label: text.stepLicense, done: status?.status === 'licensed', detail: status?.status === 'licensed' ? licenseSummary(status.license, text) : text.waitingLicense },
    { label: text.stepLocal, done: Boolean(status?.non_blocking ?? true), detail: text.localDetail },
  ]), [config.base_url, status, text]);

  const load = async () => {
    try {
      const [cfg, st, runtime] = await Promise.all([fetchCloudConfig(), fetchCloudStatus().catch(() => null), fetchRuntimeStatus().catch(() => null)]);
      setConfig({ ...defaultConfig, ...cfg });
      setRuntimeStatus(runtime);
      setRegisterInfo(prev => {
        const scopedDraft = st?.company_id ? loadDraft(st.company_id) : emptyRegisterInfo;
        const globalDraft = loadDraft();
        const base = st?.company_id
          ? hasDraftValue(scopedDraft)
            ? scopedDraft
            : hasDraftValue(prev)
              ? prev
              : hasDraftValue(globalDraft)
                ? globalDraft
                : emptyRegisterInfo
          : hasDraftValue(prev)
            ? prev
            : globalDraft;
        const next = { ...base, company_name: base.company_name || cfg.registration_name || '', admin_email: base.admin_email || cfg.registration_email || '' };
        saveDraft(next, st?.company_id);
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

  const heartbeat = runtimeStatus?.cloud_heartbeat;
  const heartbeatValue = heartbeat?.status || text.heartbeatWaiting;
  const heartbeatDetail = heartbeat
    ? `${heartbeat.non_blocking ? text.nonBlocking : text.blocking}${heartbeat.consecutive_failures ? ' / ' + heartbeat.consecutive_failures + ' ' + text.failures : ''}`
    : text.heartbeatWaiting;
  const heartbeatImpact = heartbeat?.business_impact === 'none_local_center_and_iworker_continue'
    ? text.businessImpactNone + '. ' + text.localPathContinues
    : text.localDetail;

  const updateRegisterInfo = (patch: Partial<RegisterInfo>) => setRegisterInfo(prev => {
    setNotice(null);
    const next = { ...prev, ...patch };
    saveDraft(next, status?.company_id);
    saveDraft(next);
    return next;
  });

  const save = async () => {
    setBusy(true);
    setNotice(null);
    try {
      saveDraft(trimmedRegisterInfo, status?.company_id);
      saveDraft(trimmedRegisterInfo);
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
      setNotice({ tone: resp.heartbeat_sent ? 'ok' : 'warn', text: registrationNoticeText(resp, text) });
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
          <StatusTile label={text.machineId} value={status?.machine_id || '-'} />
          <StatusTile label={text.companyId} value={status?.company_id || '-'} />
          <StatusTile label={text.controlMode} value={text.modes[config.cloud_control_mode as keyof Labels['modes']] || config.cloud_control_mode || '-'} />
          <StatusTile label={text.heartbeat} value={heartbeatValue} tone={heartbeat?.status === 'online' ? 'ok' : 'warn'} />
          <StatusTile label={text.boundary} value={status?.business_scope || text.localBusiness} tone="ok" />
        </div>

        <div className="cloud-registration-identity">
          <div>
            <span>{text.registrationIdentity}</span>
            <strong>{registrationTuple}</strong>
            <p>{status?.machine_id && status?.company_id ? text.registrationIdentityDesc : text.registrationIdentityPending}</p>
          </div>
          <div className="cloud-registration-identity-grid">
            <StatusTile label={text.machineId} value={status?.machine_id || '-'} />
            <StatusTile label={text.companyId} value={status?.company_id || '-'} />
            <StatusTile label={text.registrationTuple} value={registrationTuple} tone={status?.machine_id && status?.company_id ? 'ok' : 'warn'} />
          </div>
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

        <div className={'cloud-next-action ' + actionPlan.tone}>
          <div>
            <span>{text.nextActionTitle}</span>
            <strong>{actionPlan.title}</strong>
            <p>{actionPlan.detail}</p>
          </div>
          <div className="cloud-next-action-controls">
            <button className={actionPlan.action === 'save' ? 'cloud-primary' : 'ghost'} type="button" onClick={save} disabled={busy || !canSaveConfig}>{busy ? text.busy : text.save}</button>
            <button className={actionPlan.action === 'register' ? 'cloud-primary' : 'ghost'} type="button" onClick={register} disabled={busy || !canRegister}>{busy ? text.busy : text.register}</button>
            <button className={actionPlan.action === 'refresh' ? 'cloud-primary' : 'ghost'} type="button" onClick={refreshLicense} disabled={busy || !status?.registered}>{busy ? text.busy : text.refreshLicense}</button>
          </div>
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
            <p>{status.license ? status.license.id : text.waitingLicense}{status.license_cached ? ' / ' + text.cachedLicense : ''}</p>
          </article>
          <article className={status.compute?.compute_permission ? 'ok' : 'warn'}>
            <span>{text.computeTitle}</span>
            <strong>{status.compute?.compute_permission ? text.computeAllowed : text.computeBlocked}</strong>
            <p>{text.providerCount}: {status.compute?.provider_count ?? 0}{status.compute?.force_sync ? ' / ' + text.forceSync : ''}{status.compute_cached ? ' / ' + text.cachedCompute : ''}{status.compute?.error ? ' / ' + status.compute.error : ''}</p>
          </article>
        </div> : null}

        {status?.registered && (status.license_cached || status.compute_cached) ? (
          <div className="cloud-cached-snapshot">
            <strong>{text.cachedSnapshot}</strong>
            <p>{[status.license_cached ? text.cachedLicense : '', status.compute_cached ? text.cachedCompute : ''].filter(Boolean).join(' / ')}</p>
          </div>
        ) : null}

        <div className="cloud-continuity-grid">
          <article className="cloud-continuity-card ok"><strong>{text.continuityTitle}</strong><p>{text.continuityDesc}</p></article>
          <article className={heartbeat?.status === 'online' ? 'cloud-continuity-card ok' : 'cloud-continuity-card warn'}><strong>{text.heartbeat}</strong><p>{heartbeatImpact}</p><span>{heartbeatDetail}</span></article>
          <article className="cloud-continuity-card"><strong>{text.isolationTitle}</strong><p>{text.isolationDesc}</p></article>
        </div>

        <div className="cloud-form-section-title"><strong>{text.configTitle}</strong><span>{text.draftHint}</span></div>
        <div className="cloud-form-grid">
          <Field label={text.cloudUrl} value={config.base_url} placeholder="http://127.0.0.1:9366" onChange={v => update({ base_url: v })} />
          <Field label={text.centerUrl} value={config.center_base_url} placeholder="http://127.0.0.1:9377" onChange={v => update({ center_base_url: v })} />
          <Field label={text.registrationName} value={config.registration_name} placeholder="HQ iWorkerCenter" onChange={v => {
            update({ registration_name: v });
            if (!registerInfo.company_name.trim()) updateRegisterInfo({ company_name: v });
          }} />
          <Field label={text.registrationEmail} value={config.registration_email} placeholder="admin@example.com" onChange={v => {
            update({ registration_email: v });
            if (!registerInfo.admin_email.trim()) updateRegisterInfo({ admin_email: v });
          }} />
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

        <div className="cloud-registration-checklist" aria-label={text.registrationChecklist}>
          <strong>{text.registrationChecklist}</strong>
          <div>
            {registrationReadiness.map(item => (
              <span key={item.label} className={item.ready ? 'ok' : item.required ? 'danger' : 'warn'}>
                {item.label}: {item.ready ? (item.required ? text.requiredReady : text.optionalReady) : (item.required ? text.requiredMissing : text.optionalMissing)}
              </span>
            ))}
          </div>
        </div>

        {notice ? <p className={'cloud-message ' + notice.tone}>{notice.text}</p> : null}
        {licenseText ? <p className="cloud-message ok">{text.license}: {licenseText}</p> : null}
        {status?.registered && status.status === 'pending' ? <p className="cloud-message warn">{text.pendingLicenseDetail}</p> : null}
        {status?.registered && status.status === 'credential_mismatch' ? <p className="cloud-message warn">{text.credentialMismatchDetail}</p> : null}
        {status?.registered && status.status === 'offline' ? <p className="cloud-message warn">{text.offlineDetail}</p> : null}
        {status?.cache_error ? <p className="cloud-message warn">{text.cacheError}: {status.cache_error}</p> : null}
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
