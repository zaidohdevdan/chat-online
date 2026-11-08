export interface ApiErrorDetail {
  field?: string;
  code: string;
  message: string;
}

export interface ApiErrorPayload {
  error: {
    code: string;
    message: string;
    requestId?: string;
    details?: ApiErrorDetail[];
  };
}

export class ApiError extends Error {
  public readonly status: number;
  public readonly code: string;
  public readonly requestId?: string;
  public readonly details?: ApiErrorDetail[];
  public readonly retryAfterSeconds?: number;

  constructor(params: {
    status: number;
    code: string;
    message: string;
    requestId?: string;
    details?: ApiErrorDetail[];
    retryAfterSeconds?: number;
  }) {
    super(params.message);
    this.name = 'ApiError';
    this.status = params.status;
    this.code = params.code;
    this.requestId = params.requestId;
    this.details = params.details;
    this.retryAfterSeconds = params.retryAfterSeconds;
  }
}

export type Page<T> = {
  data: T[];
  nextCursor: string | null;
};

const rawApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
export const API_BASE_URL = rawApiBaseUrl
  ? rawApiBaseUrl.replace(/\/+$/, '')
  : '/api/v1';

// CSRF token is kept strictly in memory (never in localStorage or sessionStorage).
let inMemoryCsrfToken: string | null = null;

type SessionExpiredListener = () => void;
const sessionExpiredListeners = new Set<SessionExpiredListener>();

export function setCsrfToken(token: string | null): void {
  inMemoryCsrfToken = token;
}

export function getCsrfToken(): string | null {
  return inMemoryCsrfToken;
}

export function onSessionExpired(listener: SessionExpiredListener): () => void {
  sessionExpiredListeners.add(listener);
  return () => {
    sessionExpiredListeners.delete(listener);
  };
}

function notifySessionExpired(): void {
  inMemoryCsrfToken = null;
  for (const listener of sessionExpiredListeners) {
    listener();
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  formData?: FormData;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET';
  const headers: Record<string, string> = {
    Accept: 'application/json',
    ...options.headers,
  };

  if (!options.formData && options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  if (MUTATING_METHODS.has(method) && inMemoryCsrfToken) {
    headers['X-CSRF-Token'] = inMemoryCsrfToken;
  }

  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  const response = await fetch(`${API_BASE_URL}${normalizedPath}`, {
    method,
    headers,
    credentials: 'include',
    body: options.formData
      ? options.formData
      : options.body !== undefined
        ? JSON.stringify(options.body)
        : undefined,
    signal: options.signal,
  });

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get('content-type') ?? '';
  const isJson = contentType.includes('application/json');
  const payload = isJson ? ((await response.json()) as unknown) : null;

  if (!response.ok) {
    const errorObj =
      payload && typeof payload === 'object' && 'error' in payload
        ? (payload as ApiErrorPayload).error
        : undefined;

    const code = errorObj?.code ?? `HTTP_${response.status}`;
    const message = errorObj?.message ?? 'Não foi possível concluir a requisição.';
    const retryAfterHeader = response.headers.get('Retry-After');
    const retryAfterSeconds = retryAfterHeader ? Number.parseInt(retryAfterHeader, 10) : undefined;

    if (response.status === 401 && code === 'SESSION_EXPIRED') {
      notifySessionExpired();
    }

    throw new ApiError({
      status: response.status,
      code,
      message,
      requestId: errorObj?.requestId,
      details: errorObj?.details,
      retryAfterSeconds: Number.isFinite(retryAfterSeconds) ? retryAfterSeconds : undefined,
    });
  }

  return payload as T;
}

