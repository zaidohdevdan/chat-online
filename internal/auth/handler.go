package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"chat-online/internal/httpx"
	"chat-online/internal/users"
)

type ctxKey string

const sessionContextKey ctxKey = "auth_session"

func WithSession(ctx context.Context, sess *AuthenticatedSession) context.Context {
	if sess != nil {
		ctx = httpx.WithUserID(ctx, sess.User.ID)
	}
	return context.WithValue(ctx, sessionContextKey, sess)
}

func SessionFromContext(ctx context.Context) (*AuthenticatedSession, bool) {
	sess, ok := ctx.Value(sessionContextKey).(*AuthenticatedSession)
	return sess, ok && sess != nil
}

type Handler struct {
	service       *Service
	secureCookies bool
	sameSiteMode  http.SameSite
}

func NewHandler(service *Service, secureCookies bool, sameSite ...string) *Handler {
	mode := http.SameSiteLaxMode
	if len(sameSite) > 0 {
		switch sameSite[0] {
		case "none":
			mode = http.SameSiteNoneMode
		case "strict":
			mode = http.SameSiteStrictMode
		}
	}
	return &Handler{
		service:       service,
		secureCookies: secureCookies,
		sameSiteMode:  mode,
	}
}

func (h *Handler) CookieName() string {
	if h.secureCookies {
		return "__Host-session_id"
	}
	return "session_id"
}

func (h *Handler) ReadSessionCookie(r *http.Request) string {
	if c, err := r.Cookie("__Host-session_id"); err == nil && c.Value != "" {
		return c.Value
	}
	if c, err := r.Cookie("session_id"); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sessionResponse struct {
	User      users.User `json:"user"`
	CSRFToken string     `json:"csrfToken"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	authSess, err := h.service.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Credenciais inválidas")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao autenticar")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.CookieName(),
		Value:    authSess.RawCookie,
		Path:     "/",
		Expires:  authSess.Session.ExpiresAt,
		MaxAge:   int(time.Until(authSess.Session.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: h.sameSiteMode,
	})

	httpx.WriteJSON(w, http.StatusOK, sessionResponse{
		User:      authSess.User,
		CSRFToken: authSess.Session.CSRFToken,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	rawCookie := h.ReadSessionCookie(r)
	var userID string
	if sess, ok := SessionFromContext(r.Context()); ok {
		userID = sess.User.ID
	}

	_ = h.service.Logout(r.Context(), rawCookie, userID)

	http.SetCookie(w, &http.Cookie{
		Name:     h.CookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: h.sameSiteMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sessionResponse{
		User:      sess.User,
		CSRFToken: sess.Session.CSRFToken,
	})
}

