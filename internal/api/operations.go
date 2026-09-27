package api

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	details := h.runtime.HealthDetails()
	if details["ready"] == false {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(details)
}
func (h *Handler) Operations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	if r.URL.Query().Get("include_history") == "true" {
		_ = json.NewEncoder(w).Encode(h.runtime.OperationsHistory())
		return
	}
	_ = json.NewEncoder(w).Encode(h.runtime.Operations())
}
func (h *Handler) Equipment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(h.runtime.Equipment())
}
func (h *Handler) Interlocks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(h.runtime.Interlocks())
}
func (h *Handler) Incidents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(h.runtime.Incidents())
}
