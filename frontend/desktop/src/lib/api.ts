let connection: { address: string; token: string } | undefined;
export function configureAPI(address: string, token: string) { connection = { address: address.replace(/\/$/, ''), token }; }
export class ApiError extends Error {
  constructor(public status: number, public code = 'request_failed') { super(code); this.name = 'ApiError'; }
}
export function authenticatedFetch(path: string, init: RequestInit = {}) {
  if (!connection) throw new ApiError(503, 'disconnected');
  const headers = new Headers(init.headers);
  headers.set('Authorization', `Bearer ${connection.token}`);
  headers.set('X-Memplua-Client', 'desktop');
  if (init.body) headers.set('Content-Type', 'application/json');
  return fetch(`${connection.address}${path}`, { ...init, headers, credentials: 'omit', cache: 'no-store' });
}
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await authenticatedFetch(path, init);
  if (!response.ok) {
    // Legacy endpoints may include developer diagnostics. Never render them.
    const body = await response.json().catch(() => ({})) as { code?: string; error?: string };
    throw new ApiError(response.status, body.code || 'request_failed');
  }
  return (response.status === 204 ? undefined : await response.json()) as T;
}
