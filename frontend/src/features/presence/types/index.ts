export type PresenceStatus = 'online' | 'offline';

export type UserPresence = {
  userId: string;
  status: PresenceStatus;
  lastSeenAt?: string;
};

export type TypingUserEntry = {
  userId: string;
  expiresAt: number;
};

