import { API_BASE_URL, apiRequest, type Page } from '../../../lib/http';
import type {
  EditMessageRequest,
  Message,
  SendMessageRequest,
  Upload,
} from '../types';

export const MAX_MESSAGE_BODY_LENGTH = 4000;
export const MAX_ATTACHMENTS_PER_MESSAGE = 10;
export const MAX_UPLOAD_BYTES = 25 * 1024 * 1024; // 25 MB

export async function listMessages(params: {
  conversationId: string;
  cursor?: string | null;
  limit?: number;
  signal?: AbortSignal;
}): Promise<Page<Message>> {
  const search = new URLSearchParams();
  if (params.cursor) {
    search.set('cursor', params.cursor);
  }
  search.set('limit', String(params.limit ?? 30));

  return apiRequest<Page<Message>>(
    `/conversations/${encodeURIComponent(params.conversationId)}/messages?${search.toString()}`,
    {
      method: 'GET',
      signal: params.signal,
    },
  );
}

export async function sendMessage(
  conversationId: string,
  input: SendMessageRequest,
): Promise<Message> {
  return apiRequest<Message>(
    `/conversations/${encodeURIComponent(conversationId)}/messages`,
    {
      method: 'POST',
      body: input,
    },
  );
}

export async function editMessage(
  messageId: string,
  input: EditMessageRequest,
): Promise<Message> {
  return apiRequest<Message>(`/messages/${encodeURIComponent(messageId)}`, {
    method: 'PATCH',
    body: input,
  });
}

export async function deleteMessage(
  messageId: string,
  options?: { scope?: 'me' | 'all'; conversationId?: string },
): Promise<void> {
  const search = new URLSearchParams();
  if (options?.scope) {
    search.set('scope', options.scope);
  }
  if (options?.conversationId) {
    search.set('conversationId', options.conversationId);
  }
  const qs = search.toString();
  const suffix = qs ? `?${qs}` : '';
  await apiRequest<void>(`/messages/${encodeURIComponent(messageId)}${suffix}`, {
    method: 'DELETE',
  });
}

export async function uploadAttachment(file: File, signal?: AbortSignal): Promise<Upload> {
  const formData = new FormData();
  formData.append('file', file);

  return apiRequest<Upload>('/uploads', {
    method: 'POST',
    formData,
    signal,
  });
}

export function getUploadDownloadUrl(uploadId: string): string {
  return `${API_BASE_URL}/uploads/${encodeURIComponent(uploadId)}`;
}

