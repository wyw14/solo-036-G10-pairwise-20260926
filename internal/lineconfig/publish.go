package lineconfig

import (
	"fmt"
	"strings"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

// PublishRequest carries the operator identity for an audited release.
type PublishRequest struct {
	Operator string `json:"operator,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// Publish freezes a validated draft into an immutable published revision and
// makes it active. The caller (the service layer) is responsible for
// rejecting the cutover while operations are in flight; this method only
// guarantees the version content is valid and the numbering is monotonic.
func (s *Store) Publish(id string, req PublishRequest) (model.LineVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	if version.Status != model.LineVersionDraft {
		return model.LineVersion{}, fmt.Errorf("%w: version %s is %s", ErrNotDraft, id, version.Status)
	}
	if issues := version.ValidateContent(); len(issues) > 0 {
		version.LastValid = false
		version.LastChecked = s.now()
		version.Issues = issues
		version.UpdatedAt = version.LastChecked
		s.versions[id] = version
		return model.LineVersion{}, fmt.Errorf("%w: %s", ErrInvalidDraft, issues[0].Message)
	}
	network, err := route.NetworkFromConfig(version.Config)
	if err != nil {
		return model.LineVersion{}, fmt.Errorf("%w: cannot build network: %w", ErrInvalidDraft, err)
	}
	if err := network.CheckReachability(); err != nil {
		version.LastValid = false
		version.LastChecked = s.now()
		version.Issues = []model.LineVersionIssue{{Message: err.Error()}}
		version.UpdatedAt = version.LastChecked
		s.versions[id] = version
		return model.LineVersion{}, fmt.Errorf("%w: %w", ErrInvalidDraft, err)
	}
	now := s.now()
	previousActive := s.activeID
	version.Number = s.nextPublished
	s.nextPublished++
	version.Status = model.LineVersionPublished
	version.Active = true
	version.LastValid = true
	version.LastChecked = now
	version.Issues = nil
	version.PublishedAt = now
	version.PublishedBy = strings.TrimSpace(req.Operator)
	if notes := strings.TrimSpace(req.Notes); notes != "" {
		version.Notes = notes
	}
	version.UpdatedAt = now
	if previousActive != "" {
		if previous, ok := s.versions[previousActive]; ok {
			previous.Active = false
			s.versions[previousActive] = previous
		}
	}
	s.versions[id] = version
	s.activeID = id
	return version, nil
}

// Activate re-selects an already published, immutable revision as active for
// new operations. It is reserved for rollback-style cutovers and still
// requires a quiescent system.
func (s *Store) Activate(id, operator string) (model.LineVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	if version.Status != model.LineVersionPublished {
		return model.LineVersion{}, fmt.Errorf("line version %s is not published", id)
	}
	if s.activeID != "" {
		if previous, ok := s.versions[s.activeID]; ok {
			previous.Active = false
			s.versions[s.activeID] = previous
		}
	}
	version.Active = true
	if operator = strings.TrimSpace(operator); operator != "" {
		version.PublishedBy = operator
	}
	version.UpdatedAt = s.now()
	s.versions[id] = version
	s.activeID = id
	return version, nil
}

// ActiveVersion returns the published version new operations must use.
func (s *Store) ActiveVersion() (model.LineVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	version, ok := s.versions[s.activeID]
	return version, ok
}

// ActiveNetwork builds the routing graph of the active version.
func (s *Store) ActiveNetwork() (*route.Network, model.LineVersion, error) {
	version, ok := s.ActiveVersion()
	if !ok {
		return nil, model.LineVersion{}, fmt.Errorf("no active line version")
	}
	network, err := route.NetworkFromConfig(version.Config)
	if err != nil {
		return nil, model.LineVersion{}, err
	}
	return network, version, nil
}

// NetworkFor builds the routing graph captured by any version, so historical
// runs can be inspected against the line that was active at their time.
func (s *Store) NetworkFor(versionID string) (*route.Network, model.LineVersion, error) {
	s.mu.RLock()
	version, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok {
		return nil, model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, versionID)
	}
	network, err := route.NetworkFromConfig(version.Config)
	if err != nil {
		return nil, model.LineVersion{}, err
	}
	return network, version, nil
}
