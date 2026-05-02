// Base HTTP client for all API calls.
// Replaces Wails window.go.main.App.XXX() calls with standard fetch.

const BASE = '';
const TENANT_STORAGE_KEY = 'iwc_tenant_id';

export function setCurrentTenantID(tenantID: string) {
  const value = tenantID.trim();
  if (value) {
    localStorage.setItem(TENANT_STORAGE_KEY, value);
  } else {
    localStorage.removeItem(TENANT_STORAGE_KEY);
  }
}

export function getCurrentTenantID(): string {
  return localStorage.getItem(TENANT_STORAGE_KEY) || '';
}

function withTenantHeaders(headers?: HeadersInit): HeadersInit {
  const next = new Headers(headers);
  const tenantID = getCurrentTenantID();
  if (tenantID) {
    next.set('X-Tenant-ID', tenantID);
  }
  return next;
}

async function readError(res: Response): Promise<Error> {
  const text = await res.text();
  if (!text) return new Error(res.statusText);
  try {
    const body = JSON.parse(text);
    const message = body?.error?.message || body?.message || body?.error || text;
    return new Error(typeof message === 'string' ? message : text);
  } catch {
    return new Error(text);
  }
}

export async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: 'same-origin',
    headers: withTenantHeaders(),
  });
  if (!res.ok) {
    throw await readError(res);
  }
  return res.json();
}

export async function apiPost<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: withTenantHeaders({ 'Content-Type': 'application/json' }),
    body: body != null ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw await readError(res);
  }
  return res.json();
}

export async function apiPut<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: withTenantHeaders({ 'Content-Type': 'application/json' }),
    body: body != null ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw await readError(res);
  }
  return res.json();
}

export async function apiDelete<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: withTenantHeaders(),
  });
  if (!res.ok) {
    throw await readError(res);
  }
  return res.json();
}

export async function apiPostText<T>(path: string, body: string, contentType = 'text/plain'): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: withTenantHeaders({ 'Content-Type': contentType }),
    body,
  });
  if (!res.ok) {
    throw await readError(res);
  }
  return res.json();
}