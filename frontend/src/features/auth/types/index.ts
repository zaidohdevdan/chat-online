export type UserSummary = {
  id: string;
  username?: string;
  displayName: string;
  avatarUrl?: string;
};

export type User = UserSummary & {
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

