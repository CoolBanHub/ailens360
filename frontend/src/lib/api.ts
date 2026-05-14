import { clearAuth, getAuth } from './auth';

// Backend wraps every response as { code, message, data }.
// We unwrap data on success and throw a typed Error on failure.

export class ApiError extends Error {
  status: number;
  code?: number;
  constructor(status: number, message: string, code?: number) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const BASE = '/api';

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const { token } = getAuth();
  if (token) headers['Authorization'] = 'Bearer ' + token;

  const r = await fetch(BASE + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  // 401 anywhere → log the user out so route guards can push them to /login.
  if (r.status === 401) {
    clearAuth();
    throw new ApiError(401, 'unauthenticated');
  }
  let json: any = null;
  try {
    json = await r.json();
  } catch {
    /* ignore */
  }
  if (!r.ok) {
    throw new ApiError(r.status, json?.message || r.statusText, json?.code);
  }
  return json?.data as T;
}

export const api = {
  get:  <T>(p: string) => req<T>('GET', p),
  post: <T>(p: string, body?: unknown) => req<T>('POST', p, body),
  put:  <T>(p: string, body?: unknown) => req<T>('PUT', p, body),
  del:  <T>(p: string) => req<T>('DELETE', p),
};
