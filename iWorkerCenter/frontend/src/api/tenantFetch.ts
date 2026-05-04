const marker = '__iworkercenterTenantFetchInstalled';

type WindowWithTenantFetch = Window & {
  [marker]?: boolean;
};

function currentTenantID() {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem('iworkercenter.tenant_id') || '';
}

function isAdminRequest(input: RequestInfo | URL) {
  if (typeof window === 'undefined') return false;
  const raw = typeof input === 'string'
    ? input
    : input instanceof URL
      ? input.toString()
      : input.url;
  try {
    return new URL(raw, window.location.href).pathname.startsWith('/admin/');
  } catch {
    return false;
  }
}

export function rememberTenantID(tenantID?: string) {
  if (!tenantID || typeof window === 'undefined') return;
  window.localStorage.setItem('iworkercenter.tenant_id', tenantID);
}

export function installTenantFetchInterceptor() {
  if (typeof window === 'undefined') return;
  const target = window as WindowWithTenantFetch;
  if (target[marker]) return;
  target[marker] = true;
  const originalFetch = window.fetch.bind(window);

  window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const tenantID = currentTenantID();
    if (!tenantID || !isAdminRequest(input)) {
      return originalFetch(input, init);
    }

    const baseHeaders = input instanceof Request ? input.headers : undefined;
    const headers = new Headers(init?.headers || baseHeaders);
    if (!headers.has('X-Tenant-ID')) headers.set('X-Tenant-ID', tenantID);

    if (input instanceof Request) {
      return originalFetch(new Request(input, { ...init, headers }));
    }
    return originalFetch(input, { ...init, headers });
  };
}
