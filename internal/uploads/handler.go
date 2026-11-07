package uploads

import (
	"errors"
	"fmt"
	"io"
	"net/http"

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

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSizeBytes+1024*1024)
	if err := r.ParseMultipartForm(10 * 1024 * 1024); err != nil {
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Arquivo excede o limite de 25 MB")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Campo 'file' obrigatório")
		return
	}
	defer func() {
		_ = file.Close()
	}()

	uploaded, err := h.service.UploadFile(r.Context(), sess.User.ID, header.Filename, file)
	if err != nil {
		switch {
		case errors.Is(err, ErrFileTooLarge):
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Arquivo excede o limite de 25 MB")
		case errors.Is(err, ErrUnsupportedMediaType):
			httpx.WriteError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Tipo de arquivo não suportado")
		case errors.Is(err, ErrEmptyFile):
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Arquivo vazio")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Falha ao processar upload")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, uploaded)
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Não autenticado")
		return
	}

	uploadID := chi.URLParam(r, "id")
	rec, rc, err := h.service.OpenAuthorized(r.Context(), uploadID, sess.User.ID)
	if err != nil {
		if errors.Is(err, ErrUploadNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Arquivo não encontrado")
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Erro ao acessar arquivo")
		return
	}
	defer func() {
		_ = rc.Close()
	}()

	w.Header().Set("Content-Type", rec.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", rec.FileName))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

