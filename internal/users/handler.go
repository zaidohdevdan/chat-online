package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	ownerID, ok := httpx.GetUserID(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	query := r.URL.Query().Get("query")
	limit := 30
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	list, err := h.service.SearchContacts(r.Context(), ownerID, query, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao buscar contatos")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.Page[UserSummary]{
		Data:       list,
		NextCursor: nil,
	})
}

type addContactRequest struct {
	Username string `json:"username"`
}

func (h *Handler) AddContact(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := httpx.GetUserID(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	var req addContactRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	cleaned := strings.TrimPrefix(strings.TrimSpace(req.Username), "@")
	if cleaned == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Informe o username do contato")
		return
	}

	contact, created, err := h.service.AddContactByUsername(r.Context(), ownerID, cleaned)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Nenhum usuário encontrado com esse username")
			return
		}
		if errors.Is(err, ErrCannotAddSelf) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Você não pode adicionar seu próprio username como contato")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao vincular contato")
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, contact)
}

type createInviteRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

func (h *Handler) CreateInviteRequest(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := httpx.GetUserID(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	var req createInviteRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "JSON inválido")
		return
	}

	cleaned := strings.TrimPrefix(strings.TrimSpace(req.Username), "@")
	if cleaned == "" || len(cleaned) > 120 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Informe um username válido para convidar")
		return
	}

	created, err := h.service.CreateAccountRequest(r.Context(), ownerID, cleaned, req.DisplayName)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao enviar solicitação ao administrador")
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, created)
}
