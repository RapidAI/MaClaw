import { apiGet, apiPut } from './client';

export interface AdminProfile {
  username: string;
  email: string;
}

export function fetchProfile(): Promise<AdminProfile> {
  return apiGet<AdminProfile>('/admin/profile');
}

export function updateProfile(email: string): Promise<{ status: string }> {
  return apiPut<{ status: string }>('/admin/profile', { email });
}

export function updatePassword(oldPassword: string, newPassword: string): Promise<{ status: string }> {
  return apiPut<{ status: string }>('/admin/password', { old_password: oldPassword, new_password: newPassword });
}
