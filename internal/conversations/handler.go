package conversations

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"chat-online/internal/auth"
	"chat-online/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	var cursor *string
	if rawCursor := r.URL.Query().Get("cursor"); rawCursor != "" {
		cursor = &rawCursor
	}
	limit := 30
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	list, nextCursor, err := h.service.ListForUser(r.Context(), sess.User.ID, cursor, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Parâmetros de paginação inválidos")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.Page[Conversation]{
		Data:       list,
		NextCursor: nextCursor,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	var input CreateInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	if err := dec.Decode(&input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	conv, created, err := h.service.Create(r.Context(), sess.User.ID, input)
	if err != nil {
		if errors.Is(err, ErrInvalidConversationInput) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Dados da conversa inválidos")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao criar conversa")
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, conv)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	convID := chi.URLParam(r, "id")
	conv, err := h.service.GetByIDForUser(r.Context(), convID, sess.User.ID)
	if err != nil {
		if errors.Is(err, ErrConversationNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Conversa não encontrada")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao buscar conversa")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, conv)
}

type markReadRequest struct {
	LastReadMessageID string `json:"lastReadMessageId"`
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	convID := chi.URLParam(r, "id")
	var req markReadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil || req.LastReadMessageID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "lastReadMessageId é obrigatório")
		return
	}

	err := h.service.MarkRead(r.Context(), convID, sess.User.ID, req.LastReadMessageID)
	if err != nil {
		if errors.Is(err, ErrConversationNotFound) || errors.Is(err, ErrMessageNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Recurso não encontrado")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao registrar leitura")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

