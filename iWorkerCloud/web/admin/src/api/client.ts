let token = '';

export class ApiError extends Error {
  status: number;
  body: unknown;

  constructor(message: string, status: number, body: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

export function setToken(t: string) { token = t; }
export function getToken() { return token; }
export function clearToken() { token = ''; }

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(path, {
    method,
    headers,
    body: body != null ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const j = await res.json().catch(() => ({}));
    throw new ApiError((j as any).message || res.statusText, res.status, j);
  }
  return res.json();
}

export const apiGet = <T>(path: string) => request<T>('GET', path);
export const apiPost = <T>(path: string, body?: unknown) => request<T>('POST', path, body);
export const apiPut = <T>(path: string, body?: unknown) => request<T>('PUT', path, body);
export const apiDelete = <T>(path: string) => request<T>('DELETE', path);
