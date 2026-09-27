package lineconfig

import (
	"fmt"
	"strings"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

// DraftRequest carries operator input for creating or updating a draft.
type DraftRequest struct {
	Name     string           `json:"name"`
	Notes    string           `json:"notes"`
	Config   model.LineConfig `json:"config"`
	Operator string           `json:"operator,omitempty"`
}

// CreateDraft opens a new editable draft. When baseVersionID is empty the
// draft starts from the operator-supplied config; otherwise it is pre-filled
// with that version's config (typically the active one) unless the request
// already carries content.
func (s *Store) CreateDraft(req DraftRequest, baseVersionID string) (model.LineVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config := req.Config
	if strings.TrimSpace(baseVersionID) != "" {
		base, ok := s.versions[baseVersionID]
		if !ok {
			return model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, baseVersionID)
		}
		if len(config.Stations) == 0 && len(config.Segments) == 0 {
			config = base.Config
		}
	}
	now := s.now()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Draft " + now.Format("20060102-150405")
	}
	version := model.LineVersion{
		ID:        s.newID(),
		Number:    0,
		Status:    model.LineVersionDraft,
		Name:      name,
		Notes:     strings.TrimSpace(req.Notes),
		Config:    config.Normalize(),
		CreatedBy: strings.TrimSpace(req.Operator),
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.versions[version.ID] = version
	s.order = append(s.order, version.ID)
	return version, nil
}

// UpdateDraft replaces the editable content of a draft.
func (s *Store) UpdateDraft(id string, req DraftRequest) (model.LineVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	if version.Status != model.LineVersionDraft {
		return model.LineVersion{}, fmt.Errorf("%w: version %s is %s", ErrNotDraft, id, version.Status)
	}
	version.Name = strings.TrimSpace(req.Name)
	version.Notes = strings.TrimSpace(req.Notes)
	version.Config = req.Config.Normalize()
	if operator := strings.TrimSpace(req.Operator); operator != "" {
		version.CreatedBy = operator
	}
	version.UpdatedAt = s.now()
	version.LastValid = false
	version.LastChecked = time.Time{}
	version.Issues = nil
	s.versions[id] = version
	return version, nil
}

// DeleteDraft abandons a draft. Published revisions are never deleted.
func (s *Store) DeleteDraft(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	if version.Status != model.LineVersionDraft {
		return fmt.Errorf("%w: version %s is %s", ErrNotDraft, id, version.Status)
	}
	delete(s.versions, id)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

// Validate runs content and reachability checks against a draft without
// changing its lifecycle state. The outcome is recorded on the draft for
// later review.
func (s *Store) Validate(id string) (model.ValidationReport, error) {
	s.mu.Lock()
	version, ok := s.versions[id]
	if !ok {
		s.mu.Unlock()
		return model.ValidationReport{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	s.mu.Unlock()

	report := model.ValidationReport{VersionID: id, CheckedAt: s.now()}
	issues := version.ValidateContent()
	if len(issues) == 0 {
		network, err := route.NetworkFromConfig(version.Config)
		if err != nil {
			issues = append(issues, model.LineVersionIssue{Message: err.Error()})
		} else if err := network.CheckReachability(); err != nil {
			issues = append(issues, model.LineVersionIssue{Message: err.Error()})
		}
	}
	report.Issues = issues
	report.Valid = len(issues) == 0

	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok = s.versions[id]
	if !ok {
		return model.ValidationReport{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	version.LastValid = report.Valid
	version.LastChecked = report.CheckedAt
	version.Issues = report.Issues
	version.UpdatedAt = report.CheckedAt
	s.versions[id] = version
	return report, nil
}
