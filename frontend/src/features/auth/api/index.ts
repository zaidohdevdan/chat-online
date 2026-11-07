import { apiRequest, setCsrfToken, type Page } from '../../../lib/http';
import type { AuthSessionResponse, LoginRequest, User, UserSummary } from '../types';

function normalizeSessionPayload(raw: AuthSessionResponse | (User & { csrfToken: string })): User {
  if ('csrfToken' in raw && typeof raw.csrfToken === 'string') {
    setCsrfToken(raw.csrfToken);
  }
  if ('user' in raw && raw.user) {
    return raw.user;
  }
  const rawUser = raw as User & { csrfToken: string };
  return {
    id: rawUser.id,
    displayName: rawUser.displayName,
    avatarUrl: rawUser.avatarUrl,
    createdAt: rawUser.createdAt,
  };
}

export async function fetchCurrentUser(signal?: AbortSignal): Promise<User> {
  const response = await apiRequest<AuthSessionResponse | (User & { csrfToken: string })>('/me', {
    method: 'GET',
    signal,
  });
  return normalizeSessionPayload(response);
}

export async function loginWithCredentials(input: LoginRequest): Promise<User> {
  const response = await apiRequest<AuthSessionResponse | (User & { csrfToken: string })>(
    '/auth/login',
    {
      method: 'POST',
      body: input,
    },
  );
  return normalizeSessionPayload(response);
}

export async function logoutSession(): Promise<void> {
  try {
    await apiRequest<void>('/auth/logout', {
      method: 'POST',
    });
  } finally {
    setCsrfToken(null);
  }
}

export async function fetchUserProfile(userId: string, signal?: AbortSignal): Promise<User> {
  return apiRequest<User>(`/users/${encodeURIComponent(userId)}`, {
    method: 'GET',
    signal,
  });
}

export async function searchUsers(
  query: string,
  limit = 20,
  signal?: AbortSignal,
): Promise<UserSummary[]> {
  const params = new URLSearchParams({
    query: query.trim(),
    limit: String(limit),
  });
  const res = await apiRequest<Page<UserSummary> | UserSummary[]>(
    `/users?${params.toString()}`,
    {
      method: 'GET',
      signal,
    },
  );
  if (Array.isArray(res)) {
    return res;
  }
  return res.data ?? [];
}
