import { apiGet } from './client';
import type { DashboardData, CenterStatus, CenterSettings } from '../types';

export function fetchDashboard(): Promise<DashboardData> {
  return apiGet('/api/dashboard');
}

export function fetchCenterStatus(): Promise<CenterStatus> {
  return apiGet('/api/center/status');
}

export function fetchCenterSettings(): Promise<CenterSettings> {
  return apiGet('/api/center/settings');
}
