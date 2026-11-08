package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"chat-online/internal/auth"
	"chat-online/internal/metrics"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	heartbeatIntervalMs = 25000
	pongTimeout         = 60 * time.Second
	maxClientFrameBytes = 4096
	clientSendBuffer    = 64
	maxSyncReplayEvents = 300
	typingThrottle      = 2 * time.Second
)

type DeliveryAckHandler interface {
	RecordDeliveryAck(ctx context.Context, userID, messageID string) (*Envelope, error)
}

type clientConn struct {
	userID     string
	ws         *websocket.Conn
	sendCh     chan []byte
	closeOnce  sync.Once
	lastTyping map[string]time.Time
	typingMu   sync.Mutex
}

func (c *clientConn) enqueue(frame []byte) bool {
	select {
	case c.sendCh <- frame:
		return true
	default:
		// Slow consumer: close connection to force client sync.resume per docs/events.md section 6.
		c.close(websocket.StatusPolicyViolation, "slow consumer buffer full")
		return false
	}
}

func (c *clientConn) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		close(c.sendCh)
		_ = c.ws.Close(code, reason)
	})
}

type Hub struct {
	pool           *pgxpool.Pool
	allowedOrigins []string
	ackHandler     DeliveryAckHandler

	mu            sync.RWMutex
	userClients   map[string]map[*clientConn]struct{}
	localEventIDs map[string]time.Time
	workerCancel  context.CancelFunc
	workerWg      sync.WaitGroup
}

func NewHub(pool *pgxpool.Pool, allowedOrigins []string) *Hub {
	return &Hub{
		pool:           pool,
		allowedOrigins: allowedOrigins,
		userClients:    make(map[string]map[*clientConn]struct{}),
		localEventIDs:  make(map[string]time.Time),
	}
}

func (h *Hub) markLocallyPublished(eventID string) {
	if eventID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.localEventIDs) > 1000 {
		for k, ts := range h.localEventIDs {
			if now.Sub(ts) > 2*time.Minute {
				delete(h.localEventIDs, k)
			}
		}
	}
	h.localEventIDs[eventID] = now
}

func (h *Hub) wasLocallyPublished(eventID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.localEventIDs[eventID]
	return ok
}

func (h *Hub) SetDeliveryAckHandler(handler DeliveryAckHandler) {
	h.ackHandler = handler
}

func (h *Hub) IsUserOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.userClients[userID]) > 0
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	cancel := h.workerCancel
	var all []*clientConn
	for _, conns := range h.userClients {
		for c := range conns {
			all = append(all, c)
		}
	}
	h.userClients = make(map[string]map[*clientConn]struct{})
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	h.workerWg.Wait()

	for _, c := range all {
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
}

func (h *Hub) PublishConversationEvent(ctx context.Context, env Envelope) {
	if env.ConversationID == nil {
		return
	}
	h.markLocallyPublished(env.ID)
	memberIDs, err := h.getConversationMemberIDs(ctx, *env.ConversationID)
	if err != nil {
		slog.Error("failed to query conversation members for realtime event", "err", err)
		return
	}
	h.sendToUsers(memberIDs, env)
}

func (h *Hub) PublishToUsers(userIDs []string, env Envelope) {
	h.markLocallyPublished(env.ID)
	h.sendToUsers(userIDs, env)
}

func (h *Hub) sendToUsers(userIDs []string, env Envelope) {
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}

	h.mu.RLock()
	var targets []*clientConn
	for _, uid := range userIDs {
		for c := range h.userClients[uid] {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.enqueue(raw)
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !h.isOriginAllowed(r) {
		http.Error(w, "Forbidden Origin", http.StatusForbidden)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin already validated against h.allowedOrigins above
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(maxClientFrameBytes)

	client := &clientConn{
		userID:     sess.User.ID,
		ws:         ws,
		sendCh:     make(chan []byte, clientSendBuffer),
		lastTyping: make(map[string]time.Time),
	}

	becameOnline := h.registerClient(client)
	metrics.Default.IncWS()
	defer func() {
		metrics.Default.DecWS()
		becameOffline := h.unregisterClient(client)
		if becameOffline {
			h.broadcastPresence(context.Background(), sess.User.ID, "offline")
		}
	}()

	// Send connection.ready immediately after handshake
	readyEnv, err := NewEphemeralEnvelope(nil, "connection.ready", map[string]any{
		"heartbeatIntervalMs": heartbeatIntervalMs,
		"maxClientFrameBytes": maxClientFrameBytes,
	})
	if err == nil {
		if b, mErr := json.Marshal(readyEnv); mErr == nil {
			client.enqueue(b)
		}
	}

	if becameOnline {
		h.broadcastPresence(r.Context(), sess.User.ID, "online")
	}
	h.sendInitialPeerPresence(r.Context(), client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go h.writePump(ctx, client)
	h.readPump(ctx, client)
}

func (h *Hub) isOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	for _, allowed := range h.allowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
		if err == nil && strings.Contains(allowed, "*.") {
			if parsedAllowed, aErr := url.Parse(strings.Replace(allowed, "*.", "", 1)); aErr == nil {
				suffix := "." + strings.ToLower(parsedAllowed.Host)
				if strings.EqualFold(parsedOrigin.Scheme, parsedAllowed.Scheme) &&
					strings.HasSuffix(strings.ToLower(parsedOrigin.Host), suffix) {
					return true
				}
			}
		}
	}
	if err == nil {
		if strings.EqualFold(parsedOrigin.Host, r.Host) {
			return true
		}
	}
	return false
}

func (h *Hub) registerClient(c *clientConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.userClients[c.userID]
	wasOffline := len(conns) == 0
	if conns == nil {
		conns = make(map[*clientConn]struct{})
		h.userClients[c.userID] = conns
	}
	conns[c] = struct{}{}
	return wasOffline
}

func (h *Hub) unregisterClient(c *clientConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.userClients[c.userID]
	delete(conns, c)
	if len(conns) == 0 {
		delete(h.userClients, c.userID)
		return true
	}
	return false
}

func (h *Hub) writePump(ctx context.Context, c *clientConn) {
	ticker := time.NewTicker(time.Duration(heartbeatIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.close(websocket.StatusInternalError, "write error")
				return
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := c.ws.Ping(pingCtx)
			cancel()
			if err != nil {
				c.close(websocket.StatusPolicyViolation, "ping timeout")
				return
			}
		}
	}
}

type clientFrame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (h *Hub) readPump(ctx context.Context, c *clientConn) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		var frame clientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			h.sendErrorFrame(c, "BAD_FRAME", "Formato de evento inválido")
			continue
		}
		h.handleClientEvent(ctx, c, frame)
	}
}

func (h *Hub) handleClientEvent(ctx context.Context, c *clientConn, frame clientFrame) {
	switch frame.Type {
	case "typing.start":
		var payload struct {
			ConversationID string `json:"conversationId"`
		}
		if err := json.Unmarshal(frame.Data, &payload); err != nil || payload.ConversationID == "" {
			return
		}
		if !h.userIsMember(ctx, payload.ConversationID, c.userID) {
			return
		}
		c.typingMu.Lock()
		last := c.lastTyping[payload.ConversationID]
		now := time.Now()
		if now.Sub(last) < typingThrottle {
			c.typingMu.Unlock()
			return
		}
		c.lastTyping[payload.ConversationID] = now
		c.typingMu.Unlock()

		convID := payload.ConversationID
		env, err := NewEphemeralEnvelope(&convID, "typing.started", map[string]string{
			"userId": c.userID,
		})
		if err == nil {
			h.PublishConversationEvent(ctx, *env)
		}

	case "typing.stop":
		var payload struct {
			ConversationID string `json:"conversationId"`
		}
		if err := json.Unmarshal(frame.Data, &payload); err != nil || payload.ConversationID == "" {
			return
		}
		if !h.userIsMember(ctx, payload.ConversationID, c.userID) {
			return
		}
		convID := payload.ConversationID
		env, err := NewEphemeralEnvelope(&convID, "typing.stopped", map[string]string{
			"userId": c.userID,
		})
		if err == nil {
			h.PublishConversationEvent(ctx, *env)
		}

	case "message.ack":
		var payload struct {
			MessageID string `json:"messageId"`
		}
		if err := json.Unmarshal(frame.Data, &payload); err != nil || payload.MessageID == "" {
			return
		}
		if h.ackHandler != nil {
			env, err := h.ackHandler.RecordDeliveryAck(ctx, c.userID, payload.MessageID)
			if err == nil && env != nil {
				h.PublishConversationEvent(ctx, *env)
			}
		}

	case "sync.resume":
		var payload struct {
			Conversations []struct {
				ConversationID string `json:"conversationId"`
				AfterSequence  int64  `json:"afterSequence"`
			} `json:"conversations"`
		}
		if err := json.Unmarshal(frame.Data, &payload); err != nil {
			return
		}
		for _, item := range payload.Conversations {
			if item.ConversationID == "" {
				continue
			}
			if !h.userIsMember(ctx, item.ConversationID, c.userID) {
				continue
			}
			h.replayConversationEvents(ctx, c, item.ConversationID, item.AfterSequence)
		}
	}
}

func (h *Hub) replayConversationEvents(ctx context.Context, c *clientConn, conversationID string, afterSequence int64) {
	var lastSequence int64
	err := h.pool.QueryRow(ctx, `
		SELECT last_sequence FROM conversations WHERE id = $1
	`, conversationID).Scan(&lastSequence)
	if err != nil {
		return
	}

	if lastSequence-afterSequence > maxSyncReplayEvents {
		h.sendSyncReset(c, conversationID)
		return
	}

	rows, err := h.pool.Query(ctx, `
		SELECT id::text, type, occurred_at, sequence, payload
		FROM events
		WHERE conversation_id = $1 AND sequence > $2
		ORDER BY sequence ASC
		LIMIT $3
	`, conversationID, afterSequence, maxSyncReplayEvents+1)
	if err != nil {
		h.sendSyncReset(c, conversationID)
		return
	}
	defer rows.Close()

	var events []Envelope
	for rows.Next() {
		var env Envelope
		var seq int64
		convCopy := conversationID
		env.ConversationID = &convCopy
		if err := rows.Scan(&env.ID, &env.Type, &env.OccurredAt, &seq, &env.Data); err != nil {
			h.sendSyncReset(c, conversationID)
			return
		}
		env.Sequence = &seq
		events = append(events, env)
	}

	// If there is a gap because old events expired past retention, issue sync.reset
	if afterSequence > 0 && lastSequence > afterSequence && len(events) > 0 && *events[0].Sequence > afterSequence+1 {
		h.sendSyncReset(c, conversationID)
		return
	}

	for _, env := range events {
		if raw, mErr := json.Marshal(env); mErr == nil {
			c.enqueue(raw)
		}
	}

	convCopy := conversationID
	completedEnv, err := NewEphemeralEnvelope(&convCopy, "sync.completed", map[string]any{
		"conversationId": conversationID,
		"lastSequence":   lastSequence,
	})
	if err == nil {
		if raw, mErr := json.Marshal(completedEnv); mErr == nil {
			c.enqueue(raw)
		}
	}
}

func (h *Hub) sendSyncReset(c *clientConn, conversationID string) {
	convCopy := conversationID
	resetEnv, err := NewEphemeralEnvelope(&convCopy, "sync.reset", map[string]string{
		"conversationId": conversationID,
	})
	if err == nil {
		if raw, mErr := json.Marshal(resetEnv); mErr == nil {
			c.enqueue(raw)
		}
	}
}

func (h *Hub) sendErrorFrame(c *clientConn, code, message string) {
	env, err := NewEphemeralEnvelope(nil, "error", map[string]string{
		"code":    code,
		"message": message,
	})
	if err == nil {
		if raw, mErr := json.Marshal(env); mErr == nil {
			c.enqueue(raw)
		}
	}
}

func (h *Hub) broadcastPresence(ctx context.Context, userID, status string) {
	peerIDs, err := h.getSharedConversationPeers(ctx, userID)
	if err != nil || len(peerIDs) == 0 {
		return
	}

	data := map[string]any{
		"userId": userID,
		"status": status,
	}
	if status == "offline" {
		data["lastSeenAt"] = time.Now().UTC().Format(time.RFC3339)
	}

	env, err := NewEphemeralEnvelope(nil, "presence.updated", data)
	if err != nil {
		return
	}
	h.sendToUsers(peerIDs, *env)
}

func (h *Hub) sendInitialPeerPresence(ctx context.Context, c *clientConn) {
	peerIDs, err := h.getSharedConversationPeers(ctx, c.userID)
	if err != nil {
		return
	}
	for _, peerID := range peerIDs {
		if h.IsUserOnline(peerID) {
			env, err := NewEphemeralEnvelope(nil, "presence.updated", map[string]any{
				"userId": peerID,
				"status": "online",
			})
			if err == nil {
				if raw, mErr := json.Marshal(env); mErr == nil {
					c.enqueue(raw)
				}
			}
		}
	}
}

func (h *Hub) getConversationMemberIDs(ctx context.Context, conversationID string) ([]string, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT user_id::text
		FROM conversation_members
		WHERE conversation_id = $1
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *Hub) getSharedConversationPeers(ctx context.Context, userID string) ([]string, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT DISTINCT cm2.user_id::text
		FROM conversation_members cm1
		JOIN conversation_members cm2 ON cm2.conversation_id = cm1.conversation_id
		WHERE cm1.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *Hub) userIsMember(ctx context.Context, conversationID, userID string) bool {
	var exists bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM conversation_members
			WHERE conversation_id = $1 AND user_id = $2
		)
	`, conversationID, userID).Scan(&exists)
	return err == nil && exists
}

