package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/lineconfig"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/service"
)

type draftRequestBody struct {
	Name          string           `json:"name"`
	Notes         string           `json:"notes"`
	Operator      string           `json:"operator"`
	BaseVersionID string           `json:"base_version_id,omitempty"`
	Config        model.LineConfig `json:"config"`
}

type publishRequestBody struct {
	Operator string `json:"operator"`
	Notes    string `json:"notes"`
}

func operatorFrom(r *http.Request, bodyOperator string) string {
	if operator := strings.TrimSpace(bodyOperator); operator != "" {
		return operator
	}
	return strings.TrimSpace(r.Header.Get("X-Operator"))
}

func (h *Handler) ListLineVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.runtime.ListLineVersions())
}

func (h *Handler) GetLineVersion(w http.ResponseWriter, r *http.Request) {
	version, err := h.runtime.GetLineVersion(chi.URLParam(r, "versionID"))
	if err != nil {
		if errors.Is(err, model.ErrLineVersionNotFound) {
			writeError(w, r, http.StatusNotFound, err)
			return
		}
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (h *Handler) CreateDraft(w http.ResponseWriter, r *http.Request) {
	var body draftRequestBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.CreateDraft(service.DraftInput{
		Name:     body.Name,
		Notes:    body.Notes,
		Config:   body.Config,
		Operator: operatorFrom(r, body.Operator),
	}, body.BaseVersionID)
	if err != nil {
		writeLineVersionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (h *Handler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	var body draftRequestBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.UpdateDraft(chi.URLParam(r, "versionID"), service.DraftInput{
		Name:     body.Name,
		Notes:    body.Notes,
		Config:   body.Config,
		Operator: operatorFrom(r, body.Operator),
	})
	if err != nil {
		writeLineVersionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (h *Handler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := h.runtime.DeleteDraft(chi.URLParam(r, "versionID"), operatorFrom(r, "")); err != nil {
		writeLineVersionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ValidateDraft(w http.ResponseWriter, r *http.Request) {
	report, err := h.runtime.ValidateDraft(chi.URLParam(r, "versionID"))
	if err != nil {
		writeLineVersionError(w, r, err)
		return
	}
	status := http.StatusOK
	if !report.Valid {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, report)
}

func (h *Handler) PublishLineVersion(w http.ResponseWriter, r *http.Request) {
	body := publishRequestBody{}
	if r.ContentLength != 0 {
		if err := decodeBody(w, r, &body); err != nil {
			writeError(w, r, http.StatusBadRequest, err)
			return
		}
	}
	version, err := h.runtime.PublishLineVersion(
		chi.URLParam(r, "versionID"),
		operatorFrom(r, body.Operator),
		body.Notes,
	)
	if err != nil {
		writeLineVersionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (h *Handler) OperationLineVersion(w http.ResponseWriter, r *http.Request) {
	view, err := h.runtime.OperationLineVersion(chi.URLParam(r, "runID"))
	if err != nil {
		if errors.Is(err, service.ErrRunNotFound) || errors.Is(err, service.ErrRunVersionMissing) {
			writeError(w, r, http.StatusNotFound, err)
			return
		}
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// writeLineVersionError maps line-version lifecycle failures to HTTP statuses.
func writeLineVersionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, model.ErrLineVersionNotFound):
		writeError(w, r, http.StatusNotFound, err)
	case errors.Is(err, lineconfig.ErrInFlight), errors.Is(err, lineconfig.ErrNotDraft):
		writeError(w, r, http.StatusConflict, err)
	case errors.Is(err, lineconfig.ErrInvalidDraft):
		writeError(w, r, http.StatusUnprocessableEntity, err)
	case errors.Is(err, service.ErrOperatorRequired):
		writeError(w, r, http.StatusBadRequest, err)
	default:
		writeError(w, r, http.StatusBadRequest, err)
	}
}
