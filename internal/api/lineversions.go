package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/lineversion"
	"github.com/wyw14/solo-036/internal/model"
)

type createDraftRequest struct {
	Note string `json:"note"`
}

type updateDraftRequest struct {
	Note     string          `json:"note"`
	Stations []model.Station `json:"stations"`
	Segments []model.Segment `json:"segments"`
}

type validationResponse struct {
	Valid   bool              `json:"valid"`
	Version model.LineVersion `json:"version"`
}

// ListLineVersions GET /api/line-versions
func (h *Handler) ListLineVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.runtime.ListLineVersions())
}

// CurrentLineVersion GET /api/line-versions/current
func (h *Handler) CurrentLineVersion(w http.ResponseWriter, r *http.Request) {
	version, err := h.runtime.CurrentLineVersion()
	if err != nil {
		writeError(w, r, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

// CreateLineDraft POST /api/line-versions
func (h *Handler) CreateLineDraft(w http.ResponseWriter, r *http.Request) {
	var request createDraftRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.CreateLineDraft(request.Note)
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

// GetLineVersion GET /api/line-versions/{version}
func (h *Handler) GetLineVersion(w http.ResponseWriter, r *http.Request) {
	number, err := parseVersionNumber(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.GetLineVersion(number)
	if err != nil {
		writeError(w, r, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

// UpdateLineDraft PUT /api/line-versions/{version}
func (h *Handler) UpdateLineDraft(w http.ResponseWriter, r *http.Request) {
	number, err := parseVersionNumber(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	var request updateDraftRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.UpdateLineDraft(number, model.LineContent{
		Note:     request.Note,
		Stations: request.Stations,
		Segments: request.Segments,
	})
	if err != nil {
		writeDraftError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

// ValidateLineDraft POST /api/line-versions/{version}/validate
func (h *Handler) ValidateLineDraft(w http.ResponseWriter, r *http.Request) {
	number, err := parseVersionNumber(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.ValidateLineDraft(number)
	if err != nil {
		if errors.Is(err, lineversion.ErrInvalid) {
			writeError(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		writeDraftError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, validationResponse{Valid: true, Version: version})
}

// PublishLineVersion POST /api/line-versions/{version}/publish
func (h *Handler) PublishLineVersion(w http.ResponseWriter, r *http.Request) {
	number, err := parseVersionNumber(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	version, err := h.runtime.PublishLineVersion(number)
	if err != nil {
		switch {
		case errors.Is(err, lineversion.ErrNotFound):
			writeError(w, r, http.StatusNotFound, err)
		case errors.Is(err, lineversion.ErrInvalid):
			writeError(w, r, http.StatusUnprocessableEntity, err)
		default:
			// 版本已发布不可变、仍有在途作业等冲突情形。
			writeError(w, r, http.StatusConflict, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, version)
}

// OperationLineVersion GET /api/operations/{runID}/line-version
// 旧作业据此回看当时采用的线路配置。
func (h *Handler) OperationLineVersion(w http.ResponseWriter, r *http.Request) {
	version, err := h.runtime.OperationLineVersion(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func parseVersionNumber(r *http.Request) (int, error) {
	value := chi.URLParam(r, "version")
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, errors.New("line version must be a positive integer")
	}
	return number, nil
}

func writeDraftError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, lineversion.ErrNotFound):
		writeError(w, r, http.StatusNotFound, err)
	case errors.Is(err, lineversion.ErrImmutable):
		writeError(w, r, http.StatusConflict, err)
	case errors.Is(err, lineversion.ErrInvalid):
		writeError(w, r, http.StatusUnprocessableEntity, err)
	default:
		writeError(w, r, http.StatusConflict, err)
	}
}
