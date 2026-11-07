package messages

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"chat-online/internal/auth"
	"chat-online/internal/conversations"
	"chat-online/internal/httpx"
	"chat-online/internal/users"

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

	convID := chi.URLParam(r, "id")
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

	list, nextCursor, err := h.service.List(r.Context(), convID, sess.User.ID, cursor, limit)
	if err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Conversa não encontrada")
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Parâmetros de consulta inválidos")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.Page[Message]{
		Data:       list,
		NextCursor: nextCursor,
	})
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	convID := chi.URLParam(r, "id")
	var input SendInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	if err := dec.Decode(&input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	sender := users.UserSummary{
		ID:          sess.User.ID,
		DisplayName: sess.User.DisplayName,
		AvatarURL:   sess.User.AvatarURL,
	}

	msg, created, err := h.service.Send(r.Context(), convID, sender, input)
	if err != nil {
		switch {
		case errors.Is(err, conversations.ErrConversationNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Conversa não encontrada")
		case errors.Is(err, ErrIdempotencyConflict):
			httpx.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "clientMessageId já utilizado com conteúdo diferente")
		case errors.Is(err, ErrBodyTooLong):
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Requisição inválida", httpx.ErrorDetail{
				Field:   "body",
				Code:    "TOO_LONG",
				Message: "Máximo de 4000 caracteres",
			})
		case errors.Is(err, ErrTooManyAttachments), errors.Is(err, ErrEmptyMessage), errors.Is(err, ErrMissingClientID), errors.Is(err, ErrInvalidAttachment):
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Dados da mensagem inválidos")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao enviar mensagem")
		}
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, msg)
}

type editRequest struct {
	Body string `json:"body"`
}

func (h *Handler) Edit(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	messageID := chi.URLParam(r, "id")
	var req editRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	msg, err := h.service.Edit(r.Context(), messageID, sess.User.ID, req.Body)
	if err != nil {
		switch {
		case errors.Is(err, ErrMessageNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Mensagem não encontrada")
		case errors.Is(err, ErrForbiddenAuthor):
			httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Apenas o autor pode editar a mensagem")
		case errors.Is(err, ErrBodyTooLong), errors.Is(err, ErrEmptyMessage):
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Corpo da mensagem inválido")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao editar mensagem")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, msg)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	messageID := chi.URLParam(r, "id")
	err := h.service.Delete(r.Context(), messageID, sess.User.ID)
	if err != nil {
		switch {
		case errors.Is(err, ErrMessageNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Mensagem não encontrada")
		case errors.Is(err, ErrForbiddenAuthor):
			httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Apenas o autor pode apagar a mensagem")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao apagar mensagem")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

