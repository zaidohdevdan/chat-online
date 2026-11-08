package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"chat-online/internal/app"
	"chat-online/internal/auth"
	"chat-online/internal/config"
	"chat-online/internal/conversations"
	"chat-online/internal/database"
	"chat-online/internal/messages"
	"chat-online/internal/realtime"
	"chat-online/internal/uploads"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
)

func setupTestServer(t *testing.T) (*httptest.Server, *app.Application) {
	t.Helper()

	ctx := context.Background()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		adminURL := "postgres://chat_app:chat_dev_password@127.0.0.1:5432/chat_online?sslmode=disable"
		if adminConn, err := pgx.Connect(ctx, adminURL); err == nil {
			var exists bool
			_ = adminConn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'chat_online_test')`).Scan(&exists)
			if !exists {
				_, _ = adminConn.Exec(ctx, `CREATE DATABASE chat_online_test`)
			}
			_ = adminConn.Close(ctx)
		}
		dbURL = "postgres://chat_app:chat_dev_password@127.0.0.1:5432/chat_online_test?sslmode=disable"
	}

	pool, err := database.ConnectAndMigrate(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect and migrate database: %v", err)
	}

	// Clean test tables before running test suite
	_, err = pool.Exec(ctx, `TRUNCATE events, attachments, message_deliveries, message_hidden_for, messages, conversation_members, conversations, account_requests, user_contacts, sessions, users CASCADE`)
	if err != nil {
		pool.Close()
		t.Fatalf("truncate tables: %v", err)
	}

	tmpUploads := t.TempDir()
	cfg := &config.Config{
		Env:              "test",
		HTTPAddr:         "127.0.0.1:0",
		DatabaseURL:      dbURL,
		AllowedOrigins:   []string{"http://127.0.0.1:5173"},
		UploadStorageDir: tmpUploads,
		SessionSecret:    "test-secret-0123456789abcdef0123456789abcdef",
		SessionTTL:       2 * time.Hour,
		SecureCookies:    false,
		SeedDevUsers:     false,
	}

	appCtx, cancelApp := context.WithCancel(ctx)
	application, err := app.New(appCtx, cfg, pool)
	if err != nil {
		cancelApp()
		pool.Close()
		t.Fatalf("create app: %v", err)
	}

	srv := httptest.NewUnstartedServer(application.Router)
	srv.Start()

	t.Cleanup(func() {
		cancelApp()
		application.RealtimeHub.Shutdown()
		srv.Close()
		pool.Close()
	})

	return srv, application
}

type testClient struct {
	t         *testing.T
	baseURL   string
	http      *http.Client
	csrfToken string
	userID    string
}

func newTestClient(t *testing.T, baseURL string) *testClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	return &testClient{
		t:       t,
		baseURL: baseURL,
		http:    &http.Client{Jar: jar},
	}
}

func (c *testClient) doJSON(method, path string, body any, includeCSRF bool) (*http.Response, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if includeCSRF && c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("do request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

func (c *testClient) login(username, password string) (*http.Response, []byte) {
	resp, raw := c.doJSON(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, false)

	if resp.StatusCode == http.StatusOK {
		var parsed struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			CSRFToken string `json:"csrfToken"`
		}
		if err := json.Unmarshal(raw, &parsed); err == nil {
			c.csrfToken = parsed.CSRFToken
			c.userID = parsed.User.ID
		}
	}
	return resp, raw
}

func TestBackendIntegration_AuthConversationsMessagesUploadsAndRealtime(t *testing.T) {
	srv, application := setupTestServer(t)
	ctx := context.Background()

	hash, err := auth.HashPassword("SenhaSegura123!")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	uAlice, err := application.UsersService.CreateUser(ctx, "alice", "Alice", hash, nil)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	uBob, err := application.UsersService.CreateUser(ctx, "bob", "Bob", hash, nil)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	uEve, err := application.UsersService.CreateUser(ctx, "eve", "Eve", hash, nil)
	if err != nil {
		t.Fatalf("create eve: %v", err)
	}

	// 1. Health & Readiness
	aliceClient := newTestClient(t, srv.URL)
	resp, _ := aliceClient.doJSON(http.MethodGet, "/healthz", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /healthz, got %d", resp.StatusCode)
	}
	resp, _ = aliceClient.doJSON(http.MethodGet, "/readyz", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /readyz, got %d", resp.StatusCode)
	}

	// 2. Invalid credentials return 401 INVALID_CREDENTIALS
	resp, raw := aliceClient.login("alice", "senha-incorreta")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password, got %d: %s", resp.StatusCode, string(raw))
	}

	// 3. Valid login sets cookie and returns csrfToken
	resp, raw = aliceClient.login("alice", "SenhaSegura123!")
	if resp.StatusCode != http.StatusOK || aliceClient.csrfToken == "" {
		t.Fatalf("expected 200 and csrfToken on login, got %d: %s", resp.StatusCode, string(raw))
	}

	bobClient := newTestClient(t, srv.URL)
	resp, _ = bobClient.login("bob", "SenhaSegura123!")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob login failed: %d", resp.StatusCode)
	}

	eveClient := newTestClient(t, srv.URL)
	resp, _ = eveClient.login("eve", "SenhaSegura123!")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("eve login failed: %d", resp.StatusCode)
	}

	// 4. Missing CSRF token on POST is rejected with 403 FORBIDDEN
	resp, _ = aliceClient.doJSON(http.MethodPost, "/api/v1/conversations", map[string]any{
		"type":      "direct",
		"memberIds": []string{uBob.ID},
	}, false)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when CSRF header is missing, got %d", resp.StatusCode)
	}

	// 4.5 Private contacts isolation: Alice starts with 0 contacts, links Bob by @username, and never sees Eve
	resp, raw = aliceClient.doJSON(http.MethodGet, "/api/v1/users?query=", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET /api/v1/users, got %d", resp.StatusCode)
	}
	var initialContacts struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(raw, &initialContacts)
	if len(initialContacts.Data) != 0 {
		t.Fatalf("expected 0 initial contacts for Alice, got %d", len(initialContacts.Data))
	}

	resp, raw = aliceClient.doJSON(http.MethodPost, "/api/v1/users/contacts", map[string]string{
		"username": "@bob",
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 linking Bob by username, got %d: %s", resp.StatusCode, string(raw))
	}

	// Non-existent user returns 404 on /users/contacts, and Alice can request an account invite via /users/invite-requests
	resp, _ = aliceClient.doJSON(http.MethodPost, "/api/v1/users/contacts", map[string]string{
		"username": "novo.amigo",
	}, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when linking non-existent user, got %d", resp.StatusCode)
	}

	resp, raw = aliceClient.doJSON(http.MethodPost, "/api/v1/users/invite-requests", map[string]string{
		"username":    "@novo.amigo",
		"displayName": "Novo Amigo",
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on invite request, got %d: %s", resp.StatusCode, string(raw))
	}


	resp, raw = aliceClient.doJSON(http.MethodGet, "/api/v1/users?query=", nil, false)
	_ = json.Unmarshal(raw, &initialContacts)
	if len(initialContacts.Data) != 1 {
		t.Fatalf("expected 1 contact (Bob) for Alice after linking, got %d", len(initialContacts.Data))
	}

	// 4.6 Admin endpoints & Password Change flow:
	// Non-admin Alice is forbidden from admin endpoints
	resp, _ = aliceClient.doJSON(http.MethodGet, "/api/v1/admin/account-requests", nil, false)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin on /admin/account-requests, got %d", resp.StatusCode)
	}

	// Provision Admin user (daniel.almeida) and approve @novo.amigo via POST /api/v1/admin/users
	_, err = application.UsersService.CreateUser(ctx, "daniel.almeida", "Daniel de Almeida", hash, nil)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	adminClient := newTestClient(t, srv.URL)
	resp, _ = adminClient.login("daniel.almeida", "SenhaSegura123!")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login failed: %d", resp.StatusCode)
	}

	resp, raw = adminClient.doJSON(http.MethodPost, "/api/v1/admin/users", map[string]string{
		"username":    "novo.amigo",
		"displayName": "Novo Amigo",
		"password":    "mudar1234",
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on admin user creation, got %d: %s", resp.StatusCode, string(raw))
	}

	// Now Alice automatically has 2 contacts (Bob and Novo Amigo)
	resp, raw = aliceClient.doJSON(http.MethodGet, "/api/v1/users?query=", nil, false)
	_ = json.Unmarshal(raw, &initialContacts)
	if len(initialContacts.Data) != 2 {
		t.Fatalf("expected 2 contacts for Alice after admin approved @novo.amigo, got %d", len(initialContacts.Data))
	}

	// Novo Amigo logs in with default password ("mudar1234") and changes their password via POST /api/v1/me/password
	novoClient := newTestClient(t, srv.URL)
	resp, _ = novoClient.login("novo.amigo", "mudar1234")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("novo.amigo login with default password failed: %d", resp.StatusCode)
	}
	resp, raw = novoClient.doJSON(http.MethodPost, "/api/v1/me/password", map[string]string{
		"currentPassword": "mudar1234",
		"newPassword":     "MinhaNovaSenhaForte99!",
	}, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on password change, got %d: %s", resp.StatusCode, string(raw))
	}


	// 5. Direct conversation creation (201) and uniqueness on repeat (200)
	resp, raw = aliceClient.doJSON(http.MethodPost, "/api/v1/conversations", map[string]any{
		"type":      "direct",
		"memberIds": []string{uBob.ID},
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating direct conversation, got %d: %s", resp.StatusCode, string(raw))
	}
	var createdConv conversations.Conversation
	if err := json.Unmarshal(raw, &createdConv); err != nil {
		t.Fatalf("unmarshal conversation: %v", err)
	}

	resp, raw = bobClient.doJSON(http.MethodPost, "/api/v1/conversations", map[string]any{
		"type":      "direct",
		"memberIds": []string{uAlice.ID},
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on duplicate direct conversation, got %d: %s", resp.StatusCode, string(raw))
	}
	var existingConv conversations.Conversation
	_ = json.Unmarshal(raw, &existingConv)
	if existingConv.ID != createdConv.ID {
		t.Fatalf("expected same conversation ID %s, got %s", createdConv.ID, existingConv.ID)
	}

	// 6. Non-member Eve receives 404 (never 403) when accessing Alice-Bob conversation
	resp, _ = eveClient.doJSON(http.MethodGet, "/api/v1/conversations/"+createdConv.ID, nil, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member Eve, got %d", resp.StatusCode)
	}

	// 7. Connect Bob via WebSocket to verify realtime events and sync.resume
	wsURL := "ws" + srv.URL[len("http"):] + "/api/v1/realtime"
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	wsConn, _, err := websocket.Dial(wsCtx, wsURL, &websocket.DialOptions{
		HTTPClient: bobClient.http,
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() {
		_ = wsConn.Close(websocket.StatusNormalClosure, "")
	}()

	// First frame must be connection.ready
	_, frameBytes, err := wsConn.Read(wsCtx)
	if err != nil {
		t.Fatalf("read connection.ready: %v", err)
	}
	var readyEnv realtime.Envelope
	_ = json.Unmarshal(frameBytes, &readyEnv)
	if readyEnv.Type != "connection.ready" {
		t.Fatalf("expected connection.ready, got %s", readyEnv.Type)
	}

	// 8. Message Sending & Idempotency (201 -> 200 same content -> 409 different content)
	sendPath := "/api/v1/conversations/" + createdConv.ID + "/messages"
	resp, raw = aliceClient.doJSON(http.MethodPost, sendPath, map[string]any{
		"body":             "Olá Bob!",
		"clientMessageId":  "client-msg-001",
		"replyToMessageId": nil,
		"attachmentIds":    []string{},
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on first message send, got %d: %s", resp.StatusCode, string(raw))
	}
	var sentMsg messages.Message
	_ = json.Unmarshal(raw, &sentMsg)

	// Re-submission with identical content returns 200 OK and same canonical message ID
	resp, raw = aliceClient.doJSON(http.MethodPost, sendPath, map[string]any{
		"body":             "Olá Bob!",
		"clientMessageId":  "client-msg-001",
		"replyToMessageId": nil,
		"attachmentIds":    []string{},
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on idempotent retry, got %d: %s", resp.StatusCode, string(raw))
	}
	var retryMsg messages.Message
	_ = json.Unmarshal(raw, &retryMsg)
	if retryMsg.ID != sentMsg.ID {
		t.Fatalf("expected idempotent retry to return message ID %s, got %s", sentMsg.ID, retryMsg.ID)
	}

	// Re-submission with same clientMessageId and different body returns 409 IDEMPOTENCY_CONFLICT
	resp, _ = aliceClient.doJSON(http.MethodPost, sendPath, map[string]any{
		"body":             "Conteúdo alterado!",
		"clientMessageId":  "client-msg-001",
		"replyToMessageId": nil,
		"attachmentIds":    []string{},
	}, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on idempotency conflict, got %d", resp.StatusCode)
	}

	// Read realtime events on Bob's WebSocket until we see message.created
	var receivedCreated bool
	for range 5 {
		_, b, rErr := wsConn.Read(wsCtx)
		if rErr != nil {
			t.Fatalf("read ws frame: %v", rErr)
		}
		var env realtime.Envelope
		_ = json.Unmarshal(b, &env)
		if env.Type == "message.created" {
			receivedCreated = true
			break
		}
	}
	if !receivedCreated {
		t.Fatalf("expected Bob to receive message.created over WebSocket")
	}

	// 9. Test sync.resume over WebSocket
	resumePayload, _ := json.Marshal(map[string]any{
		"type": "sync.resume",
		"data": map[string]any{
			"conversations": []map[string]any{
				{"conversationId": createdConv.ID, "afterSequence": 0},
			},
		},
	})
	if err := wsConn.Write(wsCtx, websocket.MessageText, resumePayload); err != nil {
		t.Fatalf("write sync.resume: %v", err)
	}

	var gotSyncCompleted bool
	for range 6 {
		_, b, rErr := wsConn.Read(wsCtx)
		if rErr != nil {
			t.Fatalf("read sync frame: %v", rErr)
		}
		var env realtime.Envelope
		_ = json.Unmarshal(b, &env)
		if env.Type == "sync.completed" {
			gotSyncCompleted = true
			break
		}
	}
	if !gotSyncCompleted {
		t.Fatalf("expected sync.completed after sync.resume")
	}

	// 10. Upload PNG file & Download with authorization
	pngBytes := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}

	var multiBuf bytes.Buffer
	mw := multipart.NewWriter(&multiBuf)
	part, err := mw.CreateFormFile("file", "diagrama.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write(pngBytes)
	_ = mw.Close()

	upReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/uploads", &multiBuf)
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upReq.Header.Set("X-CSRF-Token", aliceClient.csrfToken)
	upResp, err := aliceClient.http.Do(upReq)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	upRaw, _ := io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	if upResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on upload, got %d: %s", upResp.StatusCode, string(upRaw))
	}
	var uploaded uploads.Upload
	_ = json.Unmarshal(upRaw, &uploaded)

	// Eve cannot download Alice's unattached or private upload (404)
	resp, _ = eveClient.doJSON(http.MethodGet, "/api/v1/uploads/"+uploaded.ID, nil, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when Eve tries to download Alice's upload, got %d", resp.StatusCode)
	}

	// 11. Message edit & delete authorization (Bob cannot edit Alice's message -> 403; Alice can edit and delete)
	resp, _ = bobClient.doJSON(http.MethodPatch, "/api/v1/messages/"+sentMsg.ID, map[string]string{
		"body": "Tentativa de edição por Bob",
	}, true)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when non-author Bob edits Alice's message, got %d", resp.StatusCode)
	}

	resp, raw = aliceClient.doJSON(http.MethodPatch, "/api/v1/messages/"+sentMsg.ID, map[string]string{
		"body": "Olá Bob! (editada)",
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when author Alice edits message, got %d: %s", resp.StatusCode, string(raw))
	}
	var editedMsg messages.Message
	_ = json.Unmarshal(raw, &editedMsg)
	if editedMsg.EditedAt == nil || editedMsg.Body == nil || *editedMsg.Body != "Olá Bob! (editada)" {
		t.Fatalf("expected editedAt and updated body on edited message")
	}

	// 12. Mark conversation as read (POST /conversations/{id}/read)
	resp, _ = bobClient.doJSON(http.MethodPost, "/api/v1/conversations/"+createdConv.ID+"/read", map[string]string{
		"lastReadMessageId": sentMsg.ID,
	}, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 marking conversation read, got %d", resp.StatusCode)
	}

	// 13. Delete message (DELETE /messages/{id}?scope=me hides for Bob, then DELETE /messages/{id}?scope=all hard-deletes for all)
	resp, _ = bobClient.doJSON(http.MethodDelete, "/api/v1/messages/"+sentMsg.ID+"?scope=me", nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 hiding message for Bob (scope=me), got %d", resp.StatusCode)
	}

	resp, _ = aliceClient.doJSON(http.MethodDelete, "/api/v1/messages/"+sentMsg.ID+"?scope=all", nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 hard-deleting message (scope=all), got %d", resp.StatusCode)
	}

	// 13.5 E2EE Key publishing (PUT /me/keys), Signal Ephemeral Queue Purge on Ack, and Conversation Deletion (Direct vs Group)
	resp, raw = aliceClient.doJSON(http.MethodPut, "/api/v1/me/keys", map[string]string{
		"publicKey": `{"kty":"EC","crv":"P-256","x":"testX","y":"testY"}`,
	}, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 publishing E2EE public key, got %d: %s", resp.StatusCode, string(raw))
	}

	// Send an E2EE message and verify Signal Ephemeral Queue purges it once Bob acknowledges delivery
	resp, raw = aliceClient.doJSON(http.MethodPost, sendPath, map[string]any{
		"body":             "E2EE:v1:encrypted-packet",
		"clientMessageId":  "client-msg-e2ee-001",
		"replyToMessageId": nil,
		"attachmentIds":    []string{},
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 sending E2EE message, got %d: %s", resp.StatusCode, string(raw))
	}
	var e2eeMsg messages.Message
	_ = json.Unmarshal(raw, &e2eeMsg)

	// Send message.ack over Bob's WebSocket so the Signal Ephemeral Queue purges the message
	ackPayload, _ := json.Marshal(map[string]any{
		"type": "message.ack",
		"data": map[string]any{
			"conversationId": createdConv.ID,
			"messageId":      e2eeMsg.ID,
		},
	})
	if err := wsConn.Write(wsCtx, websocket.MessageText, ackPayload); err != nil {
		t.Fatalf("write message.ack: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// After Bob acknowledges delivery, the Signal queue must have purged the message from PostgreSQL
	resp, raw = aliceClient.doJSON(http.MethodGet, "/api/v1/conversations/"+createdConv.ID+"/messages", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing messages after Signal purge, got %d", resp.StatusCode)
	}
	var msgPage struct {
		Data []messages.Message `json:"data"`
	}
	_ = json.Unmarshal(raw, &msgPage)
	for _, m := range msgPage.Data {
		if m.ID == e2eeMsg.ID {
			t.Fatalf("expected message %s to be purged from DB after delivery ack", e2eeMsg.ID)
		}
	}

	// Direct conversation deletion hides it only for Alice while Bob still sees it
	resp, _ = aliceClient.doJSON(http.MethodDelete, "/api/v1/conversations/"+createdConv.ID, nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting direct conversation for Alice, got %d", resp.StatusCode)
	}
	resp, raw = bobClient.doJSON(http.MethodGet, "/api/v1/conversations", nil, false)
	var bobConvs struct {
		Data []conversations.Conversation `json:"data"`
	}
	_ = json.Unmarshal(raw, &bobConvs)
	if len(bobConvs.Data) != 1 {
		t.Fatalf("expected Bob to still see the direct conversation after Alice deleted it on her side, got %d", len(bobConvs.Data))
	}

	// Group conversation deletion removes all participants and hard-deletes the group
	resp, raw = aliceClient.doJSON(http.MethodPost, "/api/v1/conversations", map[string]any{
		"type":      "group",
		"title":     "Grupo E2EE",
		"memberIds": []string{uBob.ID},
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating group conversation, got %d: %s", resp.StatusCode, string(raw))
	}
	var groupConv conversations.Conversation
	_ = json.Unmarshal(raw, &groupConv)

	resp, _ = aliceClient.doJSON(http.MethodDelete, "/api/v1/conversations/"+groupConv.ID, nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 hard-deleting group conversation, got %d", resp.StatusCode)
	}
	resp, _ = bobClient.doJSON(http.MethodGet, "/api/v1/conversations/"+groupConv.ID, nil, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for Bob after group was hard-deleted, got %d", resp.StatusCode)
	}

	// 14. Metrics endpoint (/metrics)
	resp, _ = aliceClient.doJSON(http.MethodGet, "/metrics", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /metrics, got %d", resp.StatusCode)
	}

	// 15. LGPD Export & Account Deletion (invalidates active sessions immediately)
	exported, err := application.UsersService.ExportUserData(ctx, uEve.ID)
	if err != nil || exported.User.ID != uEve.ID {
		t.Fatalf("expected LGPD export for Eve, got err=%v", err)
	}

	_, err = application.UsersService.AnonymizeAndDeleteUser(ctx, uEve.ID)
	if err != nil {
		t.Fatalf("expected LGPD delete for Eve to succeed, got %v", err)
	}

	// Eve's session must now be invalidated (401 UNAUTHORIZED)
	resp, _ = eveClient.doJSON(http.MethodGet, "/api/v1/me", nil, false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for deleted user Eve's session, got %d", resp.StatusCode)
	}
}

