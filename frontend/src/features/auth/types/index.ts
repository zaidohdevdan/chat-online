export type UserSummary = {
  id: string;
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

