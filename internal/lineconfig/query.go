package lineconfig

import (
	"fmt"
	"sort"

	"github.com/wyw14/solo-036/internal/model"
)

// Get returns a single version by ID.
func (s *Store) Get(id string) (model.LineVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	version, ok := s.versions[id]
	return version, ok
}

// GetPublished returns a published revision by monotonically increasing number.
func (s *Store) GetPublished(number int) (model.LineVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, version := range s.versions {
		if version.Status == model.LineVersionPublished && version.Number == number {
			return version, true
		}
	}
	return model.LineVersion{}, false
}

// List returns every version, drafts first then published revisions ordered
// by descending number, matching audit-review usage.
func (s *Store) List() []model.LineVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.LineVersion, 0, len(s.order))
	for _, id := range s.order {
		if version, ok := s.versions[id]; ok {
			out = append(out, version)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Status == model.LineVersionDraft) != (out[j].Status == model.LineVersionDraft) {
			return out[i].Status == model.LineVersionDraft
		}
		return out[i].Number > out[j].Number
	})
	return out
}

// Snapshot serialises the registry for the durable control snapshot.
func (s *Store) Snapshot() (versions []model.LineVersion, activeID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions = make([]model.LineVersion, 0, len(s.order))
	for _, id := range s.order {
		if version, ok := s.versions[id]; ok {
			versions = append(versions, version)
		}
	}
	return versions, s.activeID
}

// MustActiveVersion returns the active version or a descriptive error used at
// bootstrapping where its absence is fatal to configuration consistency.
func (s *Store) MustActiveVersion() (model.LineVersion, error) {
	version, ok := s.ActiveVersion()
	if !ok {
		return model.LineVersion{}, fmt.Errorf("line version registry has no active revision")
	}
	return version, nil
}
