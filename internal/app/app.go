package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chat-online/internal/auth"
	"chat-online/internal/config"
	"chat-online/internal/conversations"
	"chat-online/internal/httpx"
	"chat-online/internal/messages"
	"chat-online/internal/metrics"
	"chat-online/internal/middleware"
	"chat-online/internal/realtime"
	"chat-online/internal/storage"
	"chat-online/internal/uploads"
	"chat-online/internal/users"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Application struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Router       http.Handler
	RealtimeHub  *realtime.Hub
	UsersService *users.Service
	AuthService  *auth.Service
	ConvService  *conversations.Service
	MsgService   *messages.Service
}

func New(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) (*Application, error) {
	localStore, err := storage.NewLocalStorage(cfg.UploadStorageDir)
	if err != nil {
		return nil, err
	}

	usersRepo := users.NewRepository(pool)
	usersService := users.NewService(usersRepo)
	usersHandler := users.NewHandler(usersService)

	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(authRepo, usersService, cfg.SessionTTL, cfg.SeedDevUsers)
	authHandler := auth.NewHandler(authService, cfg.SecureCookies, cfg.CookieSameSite)

	hub := realtime.NewHub(pool, cfg.AllowedOrigins)

	convRepo := conversations.NewRepository(pool)
	convService := conversations.NewService(convRepo, usersService, hub)
	convHandler := conversations.NewHandler(convService)

	msgRepo := messages.NewRepository(pool)
	msgService := messages.NewService(msgRepo, convService, hub)
	msgHandler := messages.NewHandler(msgService)

	hub.SetDeliveryAckHandler(msgService)
	hub.StartOutboxWorker(ctx)
	authService.StartSessionCleanupWorker(ctx)

	uploadsRepo := uploads.NewRepository(pool)
	uploadsService := uploads.NewService(uploadsRepo, localStore, convService)
	uploadsHandler := uploads.NewHandler(uploadsService)

	app := &Application{
		Config:       cfg,
		Pool:         pool,
		RealtimeHub:  hub,
		UsersService: usersService,
		AuthService:  authService,
		ConvService:  convService,
		MsgService:   msgService,
	}

	if cfg.SeedDevUsers {
		app.seedDevelopmentData(ctx)
		authService.SetOnDevUserCreated(func(bgCtx context.Context, newUser users.User) {
			app.seedStarterConversationsForUser(bgCtx, newUser)
		})
	}

	app.Router = app.buildRouter(
		authService,
		authHandler,
		usersHandler,
		convHandler,
		msgHandler,
		uploadsHandler,
		hub,
	)

	return app, nil
}

func (a *Application) buildRouter(
	authService *auth.Service,
	authHandler *auth.Handler,
	usersHandler *users.Handler,
	convHandler *conversations.Handler,
	msgHandler *messages.Handler,
	uploadsHandler *uploads.Handler,
	hub *realtime.Hub,
) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.SecurityHeaders(a.Config.AllowedOrigins, a.Config.SecureCookies))
	r.Use(middleware.StructuredLogger)

	// Health & Readiness endpoints outside /api/v1 per docs/architecture.md section 8
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		pingCtx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := a.Pool.Ping(pingCtx); err != nil {
			httpx.WriteError(w, req, http.StatusServiceUnavailable, "UNAVAILABLE", "Banco de dados indisponível")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	r.Get("/metrics", metrics.Default.ServeHTTP)

	loginLimiter := middleware.NewRateLimiter(20, 10)
	apiLimiter := middleware.NewRateLimiter(300, 80)

	r.Route("/api/v1", func(api chi.Router) {
		api.With(loginLimiter.Middleware).Post("/auth/login", authHandler.Login)

		api.Group(func(authed chi.Router) {
			authed.Use(middleware.Authenticate(authService, authHandler))
			authed.Use(apiLimiter.Middleware)

			// WebSocket handshake uses cookie session + Origin validation
			authed.Get("/realtime", hub.ServeWS)

			// All REST endpoints (GET and CSRF-protected POST/PATCH/DELETE)
			authed.Group(func(csrfProtected chi.Router) {
				csrfProtected.Use(middleware.RequireCSRF(a.Config.AllowedOrigins))

				csrfProtected.Get("/me", authHandler.GetMe)
				csrfProtected.Post("/auth/logout", authHandler.Logout)

				csrfProtected.Get("/users", usersHandler.SearchUsers)
				csrfProtected.Post("/users/contacts", usersHandler.AddContact)
				csrfProtected.Get("/users/{id}", usersHandler.GetUser)

				csrfProtected.Get("/conversations", convHandler.List)
				csrfProtected.Post("/conversations", convHandler.Create)
				csrfProtected.Get("/conversations/{id}", convHandler.GetByID)
				csrfProtected.Post("/conversations/{id}/read", convHandler.MarkRead)

				csrfProtected.Get("/conversations/{id}/messages", msgHandler.List)
				csrfProtected.Post("/conversations/{id}/messages", msgHandler.Send)
				csrfProtected.Patch("/messages/{id}", msgHandler.Edit)
				csrfProtected.Delete("/messages/{id}", msgHandler.Delete)

				csrfProtected.Post("/uploads", uploadsHandler.Upload)
				csrfProtected.Get("/uploads/{id}", uploadsHandler.Download)
			})
		})
	})

	if a.Config.StaticFilesDir != "" {
		a.mountSPAStaticHandler(r, a.Config.StaticFilesDir)
	}

	return r
}

func (a *Application) mountSPAStaticHandler(r *chi.Mux, staticDir string) {
	absRoot, err := filepath.Abs(staticDir)
	if err != nil {
		return
	}
	indexPath := filepath.Join(absRoot, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return
	}

	fileServer := http.FileServer(http.Dir(absRoot))

	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			httpx.WriteError(w, req, http.StatusNotFound, "NOT_FOUND", "Endpoint não encontrado")
			return
		}

		cleanRel := filepath.Clean(strings.TrimPrefix(req.URL.Path, "/"))
		candidate := filepath.Join(absRoot, cleanRel)
		absCandidate, err := filepath.Abs(candidate)
		if err == nil && (absCandidate == absRoot || strings.HasPrefix(absCandidate, absRoot+string(filepath.Separator))) {
			if info, statErr := os.Stat(absCandidate); statErr == nil && !info.IsDir() {
				if strings.HasPrefix(req.URL.Path, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-store")
				}
				fileServer.ServeHTTP(w, req)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, req, indexPath)
	})
}

func (a *Application) seedDevelopmentData(ctx context.Context) {
	defaultHash, err := auth.HashPassword("12345678")
	if err != nil {
		return
	}

	ana, err := a.UsersService.CreateUser(ctx, "ana.silva", "Ana Silva", defaultHash, nil)
	if err != nil {
		slog.Warn("failed to seed ana.silva", "err", err)
		return
	}
	carlos, err := a.UsersService.CreateUser(ctx, "carlos.mendes", "Carlos Mendes", defaultHash, nil)
	if err != nil {
		return
	}
	marina, err := a.UsersService.CreateUser(ctx, "marina.costa", "Marina Costa", defaultHash, nil)
	if err != nil {
		return
	}

	a.seedStarterConversationsForUser(ctx, ana.User)
	_ = carlos
	_ = marina
}

func (a *Application) seedStarterConversationsForUser(ctx context.Context, targetUser users.User) {
	carlos, err := a.UsersService.GetByUsername(ctx, "carlos.mendes")
	if err != nil || carlos.ID == targetUser.ID {
		return
	}
	marina, err := a.UsersService.GetByUsername(ctx, "marina.costa")
	if err != nil || marina.ID == targetUser.ID {
		return
	}

	directConv, created, err := a.ConvService.Create(ctx, carlos.ID, conversations.CreateInput{
		Type:      "direct",
		MemberIDs: []string{targetUser.ID},
	})
	if err == nil && created && directConv != nil {
		clientID, _ := uuid.NewV7()
		_, _, _ = a.MsgService.Send(ctx, directConv.ID, users.UserSummary{
			ID:          carlos.ID,
			DisplayName: carlos.DisplayName,
		}, messages.SendInput{
			Body:            "Olá! Bem-vindo ao Chat Online. Como estão os testes por aí?",
			ClientMessageID: clientID.String(),
		})
	}

	existingConvs, _, err := a.ConvService.ListForUser(ctx, targetUser.ID, nil, 20)
	hasGroup := false
	if err == nil {
		for _, c := range existingConvs {
			if c.Type == "group" {
				hasGroup = true
				break
			}
		}
	}

	if !hasGroup {
		groupTitle := "Equipe de Produto"
		groupConv, groupCreated, gErr := a.ConvService.Create(ctx, marina.ID, conversations.CreateInput{
			Type:      "group",
			Title:     &groupTitle,
			MemberIDs: []string{targetUser.ID, carlos.ID},
		})
		if gErr == nil && groupCreated && groupConv != nil {
			clientID, _ := uuid.NewV7()
			_, _, _ = a.MsgService.Send(ctx, groupConv.ID, users.UserSummary{
				ID:          marina.ID,
				DisplayName: marina.DisplayName,
			}, messages.SendInput{
				Body:            "Pessoal, o ambiente do Chat Online já está integrado com REST e WebSocket!",
				ClientMessageID: clientID.String(),
			})
		}
	}
}

