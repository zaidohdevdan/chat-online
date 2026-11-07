package users

import (
	"errors"
	"net/http"
	"strconv"

	"chat-online/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Usuário não encontrado")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao buscar usuário")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	limit := 30
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	list, err := h.service.Search(r.Context(), query, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao buscar usuários")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.Page[UserSummary]{
		Data:       list,
		NextCursor: nil,
	})
}

