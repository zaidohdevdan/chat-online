package middleware

import (
	"bufio"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"chat-online/internal/auth"
	"chat-online/internal/httpx"
	"chat-online/internal/metrics"

	"github.com/google/uuid"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if reqID == "" || len(reqID) > 64 {
			if id, err := uuid.NewV7(); err == nil {
				reqID = id.String()
			} else {
				reqID = uuid.NewString()
			}
		}
		w.Header().Set("X-Request-Id", reqID)
		ctx := httpx.WithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func SecurityHeaders(allowedOrigins []string, enableHSTS bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: data: blob:; connect-src 'self' https: ws: wss:; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if enableHSTS {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" && isAllowedOrigin(origin, r.Host, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-Id")
				w.Header().Set("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			switch r.Method {
			case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodHead:
			default:
				httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método HTTP não permitido")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Unwrap() http.ResponseWriter {
	return sr.ResponseWriter
}

func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := sr.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("http.Hijacker not supported")
}

func StructuredLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		metrics.Default.RecordHTTP(rec.status, duration)

		// Logs never include passwords, tokens, or message bodies.
		slog.Info("http_request",
			"requestId", httpx.GetRequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", duration.Milliseconds(),
		)
	})
}

func Authenticate(authService *auth.Service, authHandler *auth.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawCookie := authHandler.ReadSessionCookie(r)
			if rawCookie == "" {
				httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Sessão não encontrada")
				return
			}

			sess, err := authService.AuthenticateToken(r.Context(), rawCookie)
			if err != nil {
				if errors.Is(err, auth.ErrSessionExpired) {
					httpx.WriteError(w, r, http.StatusUnauthorized, "SESSION_EXPIRED", "Sessão expirada")
					return
				}
				httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Sessão inválida")
				return
			}

			ctx := auth.WithSession(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireCSRF(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPatch, http.MethodDelete:
				origin := strings.TrimSpace(r.Header.Get("Origin"))
				if origin != "" && !isAllowedOrigin(origin, r.Host, allowedOrigins) {
					httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Origem não autorizada")
					return
				}

				sess, ok := auth.SessionFromContext(r.Context())
				if !ok {
					httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
					return
				}

				headerToken := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
				if headerToken == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(sess.Session.CSRFToken)) != 1 {
					httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Token CSRF ausente ou inválido")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(origin, host string, allowedOrigins []string) bool {
	parsedOrigin, err := url.Parse(origin)
	for _, allowed := range allowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
		// Support subdomain patterns like https://*.vercel.app
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
		if strings.EqualFold(parsedOrigin.Host, host) {
			return true
		}
	}
	return false
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
}

func NewRateLimiter(requestsPerMinute int, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(requestsPerMinute) / 60.0,
		burst:   float64(burst),
	}
}

func (rl *RateLimiter) Allow(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[key]
	if !exists {
		rl.buckets[key] = &bucket{
			tokens:     rl.burst - 1,
			lastRefill: now,
		}
		return true, 0
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens -= 1
		return true, 0
	}

	retryAfter := int((1.0 - b.tokens) / rl.rate)
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractClientKey(r)
		allowed, retryAfter := rl.Allow(key)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			httpx.WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Muitas requisições. Tente novamente em instantes.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractClientKey(r *http.Request) string {
	if sess, ok := auth.SessionFromContext(r.Context()); ok {
		return "user:" + sess.User.ID
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return "ip:" + host
	}
	return "ip:" + r.RemoteAddr
}

