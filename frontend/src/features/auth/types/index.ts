export type UserSummary = {
  id: string;
  username?: string;
  displayName: string;
  avatarUrl?: string;
  publicKey?: string;
};

export type User = UserSummary & {
  isAdmin?: boolean;
  createdAt: string;
};

export type AccountRequestItem = {
  id: string;
  requesterId: string;
  requesterUsername: string;
  requesterDisplayName: string;
  requestedUsername: string;
  requestedDisplayName?: string;
  status: 'pending' | 'approved' | string;
  createdAt: string;
};

export type AuthSessionResponse = {
  user: User;
  csrfToken: string;
};

export type LoginRequest = {
  username: string;
  password: string;
};

