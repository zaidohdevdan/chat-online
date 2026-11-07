import type { UserSummary } from '../../auth/types';

export type Upload = {
  id: string;
  fileName: string;
  mimeType: string;
  size: number;
  createdAt: string;
};

export type ServerMessageStatus = 'sent' | 'delivered' | 'read';

export type Message = {
  id: string;
  conversationId: string;
  sender: UserSummary;
  body: string | null;
  attachments: Upload[];
  replyToMessageId?: string;
  createdAt: string;
  editedAt?: string;
  deletedAt?: string;
  status?: ServerMessageStatus;
};

export type ClientMessageStatus = 'sending' | 'failed' | 'sent' | 'delivered' | 'read';

export type ClientMessage = Omit<Message, 'status'> & {
  status?: ClientMessageStatus;
  clientMessageId?: string;
};

export type SendMessageRequest = {
  body: string;
  clientMessageId: string;
  replyToMessageId: string | null;
  attachmentIds: string[];
};

export type EditMessageRequest = {
  body: string;
};

export type PendingAttachment = {
  localId: string;
  file: File;
  state: 'uploading' | 'uploaded' | 'failed';
  upload?: Upload;
  errorMessage?: string;
};

