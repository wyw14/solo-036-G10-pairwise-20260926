package lineconfig

import (
	"errors"
	"sync"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// Sentinel errors describing lifecycle failures. They wrap model errors so
// callers can distinguish a missing version from an invalid state change.
var (
	// ErrNotDraft is returned when a draft-only action hits a published version.
	ErrNotDraft = errors.New("line version is not a draft")
	// ErrInvalidDraft is returned when validation blocks a publish.
	ErrInvalidDraft = errors.New("line draft failed validation")
	// ErrInFlight is returned by the service when operations block cutover.
	ErrInFlight = errors.New("operations are in flight")
)

// Store keeps every line version (drafts and published revisions) and
// tracks which published version is active for new operations.
type Store struct {
	mu            sync.RWMutex
	versions      map[string]model.LineVersion
	order         []string
	activeID      string
	nextPublished int
	now           func() time.Time
	newID         func() string
}

// NewStore seeds the registry with an immutable published v1 built from the
// default compiled-in line, so a fresh deployment has an active version
// before operators publish anything.
func NewStore(seed model.LineConfig) *Store {
	store := &Store{
		versions:      make(map[string]model.LineVersion),
		now:           func() time.Time { return time.Now().UTC() },
		newID:         model.NewRevision,
		nextPublished: 1,
	}
	store.seed(seed)
	return store
}

func (s *Store) seed(config model.LineConfig) {
	now := s.now()
	version := model.LineVersion{
		ID:          "line-v1",
		Number:      1,
		Status:      model.LineVersionPublished,
		Name:        "Default line",
		Notes:       "Compiled-in default topology active before the first operator release.",
		Config:      config.Normalize(),
		Active:      true,
		CreatedBy:   "system",
		CreatedAt:   now,
		UpdatedAt:   now,
		PublishedAt: now,
		PublishedBy: "system",
		LastValid:   true,
		LastChecked: now,
	}
	s.versions[version.ID] = version
	s.order = append(s.order, version.ID)
	s.activeID = version.ID
	s.nextPublished = 2
}

// Restore rebuilds a registry from a persisted snapshot. The seeded default
// is replaced entirely by the persisted history.
func Restore(versions []model.LineVersion, activeID string) *Store {
	store := &Store{
		versions: make(map[string]model.LineVersion),
		now:      func() time.Time { return time.Now().UTC() },
		newID:    model.NewRevision,
	}
	highest := 0
	for _, version := range versions {
		if err := version.Status.Validate(); err != nil {
			continue
		}
		stored := version
		if stored.Status == model.LineVersionPublished && stored.Number > highest {
			highest = stored.Number
		}
		if stored.Active && stored.Status == model.LineVersionPublished {
			store.activeID = stored.ID
		}
		store.versions[stored.ID] = stored
		store.order = append(store.order, stored.ID)
	}
	store.nextPublished = highest + 1
	if store.activeID == "" {
		// Fall back to the highest-numbered published revision.
		for _, version := range versions {
			if version.Status == model.LineVersionPublished && version.Number == highest {
				store.activeID = version.ID
				stored := store.versions[version.ID]
				stored.Active = true
				store.versions[version.ID] = stored
			}
		}
	}
	return store
}
