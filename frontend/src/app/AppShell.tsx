import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ConnectionStatus } from '../components/feedback/ConnectionStatus';
import { EmptyState } from '../components/feedback/EmptyState';
import { useToast } from '../components/feedback/Toast';
import { Button } from '../components/ui/Button';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { useAuth } from '../features/auth/hooks/useAuth';
import { ChatHeader } from '../features/conversations/components/ChatHeader';
import { ConversationDetails } from '../features/conversations/components/ConversationDetails';
import { ConversationSidebar } from '../features/conversations/components/ConversationSidebar';
import { NewConversationModal } from '../features/conversations/components/NewConversationModal';
import {
  useConversationDetail,
  useConversations,
  useCreateConversation,
  useMarkConversationRead,
} from '../features/conversations/hooks/useConversations';
import type { CreateConversationRequest } from '../features/conversations/types';
import { MessageComposer } from '../features/messages/components/MessageComposer';
import { MessageTimeline } from '../features/messages/components/MessageTimeline';
import { useMessages } from '../features/messages/hooks/useMessages';
import type { ClientMessage } from '../features/messages/types';
import { emitTypingStart, emitTypingStop } from '../features/presence/api';
import { TypingIndicator } from '../features/presence/components/TypingIndicator';
import { usePresenceAndTyping } from '../features/presence/hooks/usePresence';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { useTheme } from '../hooks/useTheme';
import { ApiError } from '../lib/http';
import { ptBR } from '../lib/i18n';
import type { ConnectionState } from '../lib/realtime';
import styles from './AppShell.module.css';

export function AppShell() {
  const { conversationId } = useParams<{ conversationId?: string }>();
  const navigate = useNavigate();
  const { user, logout, isLoggingOut, realtimeClient } = useAuth();
  const { pushToast } = useToast();
  const { theme, cycleTheme } = useTheme();
  const isMobile = useMediaQuery('(max-width: 768px)');

  const [connectionState, setConnectionState] = useState<ConnectionState>(() =>
    realtimeClient.getConnectionState(),
  );
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [newConvOpen, setNewConvOpen] = useState(false);
  const [replyToMessage, setReplyToMessage] = useState<ClientMessage | null>(null);
  const [editingMessage, setEditingMessage] = useState<ClientMessage | null>(null);
  const [messageToDelete, setMessageToDelete] = useState<ClientMessage | null>(null);

  const lastMarkedReadRef = useRef<Record<string, string>>({});

  useEffect(() => {
    return realtimeClient.subscribeConnectionState(setConnectionState);
  }, [realtimeClient]);

  useEffect(() => {
    setReplyToMessage(null);
    setEditingMessage(null);
  }, [conversationId]);

  const {
    conversations,
    isLoading: isLoadingConversations,
  } = useConversations(realtimeClient, user?.id);

  const activeConvQuery = useConversationDetail(conversationId, conversations);
  const activeConversation = activeConvQuery.data;

  const createConvMutation = useCreateConversation();
  const markReadMutation = useMarkConversationRead();

  const { getPresence, getTypingUserIds } = usePresenceAndTyping(
    realtimeClient,
    user?.id,
  );

  const {
    messages,
    isLoading: isLoadingMessages,
    hasNextPage: hasOlderMessages,
    isFetchingNextPage: isFetchingOlderMessages,
    fetchNextPage: fetchOlderMessages,
    sendNewMessage,
    retryFailedMessage,
    editMessage,
    deleteMessage,
    isDeleting,
  } = useMessages({
    conversationId,
    currentUser: user,
    realtimeClient,
  });

  const handleSelectConversation = useCallback(
    (id: string) => {
      navigate(`/conversations/${encodeURIComponent(id)}`);
    },
    [navigate],
  );

  const handleBackToList = useCallback(() => {
    navigate('/');
  }, [navigate]);

  const handleCreateConversation = async (input: CreateConversationRequest) => {
    try {
      const created = await createConvMutation.mutateAsync(input);
      setNewConvOpen(false);
      navigate(`/conversations/${encodeURIComponent(created.id)}`);
    } catch {
      pushToast(ptBR.common.errorGeneric, 'error');
    }
  };

  const handleVisibleLatestMessage = useCallback(
    (latestMessageId: string) => {
      if (!conversationId) return;
      if (lastMarkedReadRef.current[conversationId] === latestMessageId) return;
      lastMarkedReadRef.current[conversationId] = latestMessageId;
      markReadMutation.mutate({
        conversationId,
        lastReadMessageId: latestMessageId,
      });
    },
    [conversationId, markReadMutation],
  );

  const handleConfirmDelete = async () => {
    if (!messageToDelete) return;
    try {
      await deleteMessage(messageToDelete.id);
      setMessageToDelete(null);
    } catch {
      pushToast(ptBR.common.errorGeneric, 'error');
    }
  };

  if (!user) return null;

  const showSidebar = !isMobile || !conversationId;
  const showMainPane = !isMobile || Boolean(conversationId);

  const isConversationNotFound =
    Boolean(conversationId) &&
    activeConvQuery.isError &&
    activeConvQuery.error instanceof ApiError &&
    activeConvQuery.error.status === 404;

  const typingUserIds = conversationId ? getTypingUserIds(conversationId) : [];

  return (
    <div className={styles.shell}>
      <a href="#chat-main-pane" className={styles.skipLink}>
        {ptBR.app.skipToMain}
      </a>

      <ConnectionStatus state={connectionState} />

      <div className={styles.workspace}>
        {showSidebar && (
          <ConversationSidebar
            currentUser={user}
            conversations={conversations}
            selectedConversationId={conversationId}
            isLoading={isLoadingConversations}
            isMobile={isMobile}
            theme={theme}
            connectionState={connectionState}
            onCycleTheme={cycleTheme}
            onSelectConversation={handleSelectConversation}
            onOpenNewConversation={() => setNewConvOpen(true)}
            onLogout={() => {
              void logout();
            }}
            isLoggingOut={isLoggingOut}
            getPresence={getPresence}
            getTypingUserIds={getTypingUserIds}
          />
        )}

        {showMainPane && (
          <main id="chat-main-pane" className={styles.mainPane}>
            {!conversationId ? (
              <EmptyState
                title={ptBR.chat.selectConversationTitle}
                description={ptBR.chat.selectConversationBody}
                action={
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => setNewConvOpen(true)}
                  >
                    {ptBR.sidebar.newConversation}
                  </Button>
                }
              />
            ) : isConversationNotFound ? (
              <EmptyState
                title={ptBR.chat.notFoundTitle}
                description={ptBR.chat.notFoundBody}
                action={
                  <Button variant="secondary" size="sm" onClick={handleBackToList}>
                    {ptBR.chat.backToList}
                  </Button>
                }
              />
            ) : !activeConversation ? (
              <EmptyState title={ptBR.chat.loadingMessages} />
            ) : (
              <>
                <ChatHeader
                  conversation={activeConversation}
                  currentUserId={user.id}
                  isMobile={isMobile}
                  detailsOpen={detailsOpen}
                  onBackToList={handleBackToList}
                  onToggleDetails={() => setDetailsOpen((prev) => !prev)}
                  getPresence={getPresence}
                />

                <MessageTimeline
                  conversationId={activeConversation.id}
                  currentUserId={user.id}
                  isGroup={activeConversation.type === 'group'}
                  messages={messages}
                  isLoading={isLoadingMessages}
                  hasNextPage={Boolean(hasOlderMessages)}
                  isFetchingNextPage={isFetchingOlderMessages}
                  onFetchNextPage={fetchOlderMessages}
                  onReply={(msg) => {
                    setEditingMessage(null);
                    setReplyToMessage(msg);
                  }}
                  onEdit={(msg) => {
                    setReplyToMessage(null);
                    setEditingMessage(msg);
                  }}
                  onRequestDelete={(msg) => setMessageToDelete(msg)}
                  onRetry={(clientMsgId) => {
                    void retryFailedMessage(clientMsgId);
                  }}
                  onVisibleLatestMessage={handleVisibleLatestMessage}
                />

                <div className={styles.typingRegion}>
                  <TypingIndicator
                    typingUserIds={typingUserIds}
                    members={activeConversation.members}
                  />
                </div>

                <MessageComposer
                  conversationId={activeConversation.id}
                  isMobile={isMobile}
                  replyToMessage={replyToMessage}
                  editingMessage={editingMessage}
                  onCancelReply={() => setReplyToMessage(null)}
                  onCancelEdit={() => setEditingMessage(null)}
                  onSend={(input) => sendNewMessage(input)}
                  onSaveEdit={(messageId, body) => editMessage({ messageId, body })}
                  onTypingStart={() =>
                    emitTypingStart(realtimeClient, activeConversation.id)
                  }
                  onTypingStop={() =>
                    emitTypingStop(realtimeClient, activeConversation.id)
                  }
                />
              </>
            )}
          </main>
        )}

        {activeConversation && detailsOpen && (
          <ConversationDetails
            conversation={activeConversation}
            currentUserId={user.id}
            messages={messages}
            isMobile={isMobile}
            onClose={() => setDetailsOpen(false)}
            getPresence={getPresence}
          />
        )}
      </div>

      <NewConversationModal
        open={newConvOpen}
        currentUserId={user.id}
        isCreating={createConvMutation.isPending}
        onClose={() => setNewConvOpen(false)}
        onCreate={handleCreateConversation}
      />

      <ConfirmDialog
        open={Boolean(messageToDelete)}
        title={ptBR.message.deleteConfirmTitle}
        description={ptBR.message.deleteConfirmBody}
        confirmLabel={ptBR.message.deleteConfirmButton}
        danger
        isLoading={isDeleting}
        onCancel={() => setMessageToDelete(null)}
        onConfirm={() => {
          void handleConfirmDelete();
        }}
      />
    </div>
  );
}

