import type { DashboardData, CenterStatus, CenterSettings } from '../types';

// These endpoints are not yet implemented in the backend.
// Return empty/default data to avoid 404 errors.

export function fetchDashboard(): Promise<DashboardData> {
  return Promise.resolve({ alerts: [], recent: [], metrics: [] } as DashboardData);
}

export function fetchCenterStatus(): Promise<CenterStatus> {
  return Promise.resolve({ status: 'unknown' } as CenterStatus);
}

export function fetchCenterSettings(): Promise<CenterSettings> {
  return Promise.resolve({} as CenterSettings);
}
