import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  apiRequest,
  getCsrfToken,
  onSessionExpired,
  setCsrfToken,
} from '../http';

describe('http client & CSRF protection', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock);
    setCsrfToken(null);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchMock.mockReset();
  });

  it('envia X-CSRF-Token em requisições POST, PATCH e DELETE, mas não em GET', async () => {
    setCsrfToken('csrf-secret-123');

    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => ({ ok: true }),
    });

    await apiRequest('/conversations', { method: 'GET' });
    const getCallHeaders = fetchMock.mock.calls[0]?.[1]?.headers as Record<
      string,
      string
    >;
    expect(getCallHeaders['X-CSRF-Token']).toBeUndefined();

    await apiRequest('/conversations', {
      method: 'POST',
      body: { type: 'direct', memberIds: ['u2'] },
    });
    const postCallHeaders = fetchMock.mock.calls[1]?.[1]?.headers as Record<
      string,
      string
    >;
    expect(postCallHeaders['X-CSRF-Token']).toBe('csrf-secret-123');
  });

  it('dispara listener de sessão expirada e limpa CSRF em 401 SESSION_EXPIRED', async () => {
    setCsrfToken('csrf-active');
    const expiredSpy = vi.fn();
    const unsubscribe = onSessionExpired(expiredSpy);

    fetchMock.mockResolvedValue({
      ok: false,
      status: 401,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: async () => ({
        error: {
          code: 'SESSION_EXPIRED',
          message: 'Sessão expirada',
          requestId: 'req-401',
        },
      }),
    });

    await expect(apiRequest('/me')).rejects.toBeInstanceOf(ApiError);
    expect(expiredSpy).toHaveBeenCalledTimes(1);
    expect(getCsrfToken()).toBeNull();

    unsubscribe();
  });
});

