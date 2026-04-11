import { apiGet, apiPost, setToken } from './client';

export interface AdminStatus { setup: boolean }
export interface CaptchaData { id: string; question: string }

export async function checkSetup(): Promise<AdminStatus> {
  return apiGet('/api/admin/status');
}

export async function loadCaptcha(): Promise<CaptchaData> {
  return apiGet('/api/admin/captcha');
}

export async function doSetup(username: string, password: string): Promise<void> {
  await apiPost('/api/admin/setup', { username, password });
}

export async function doLogin(username: string, password: string, captchaId: string, captchaAnswer: string): Promise<void> {
  const r = await apiPost<{ token: string }>('/api/admin/login', {
    username, password, captcha_id: captchaId, captcha_answer: captchaAnswer,
  });
  setToken(r.token);
}

export async function changePassword(username: string, oldPassword: string, newPassword: string): Promise<void> {
  await apiPost('/api/admin/password', { username, old_password: oldPassword, new_password: newPassword });
}
