package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wyw14/solo-036/internal/lineconfig"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

// DraftInput is the service-level operator input for a line draft.
type DraftInput struct {
	Name     string           `json:"name"`
	Notes    string           `json:"notes"`
	Config   model.LineConfig `json:"config"`
	Operator string           `json:"operator,omitempty"`
}

// LineVersionView is a line version enriched for review, optionally pointing
// at the run it was historically bound to.
type LineVersionView struct {
	Version   model.LineVersion `json:"version"`
	BoundRun  string            `json:"bound_run,omitempty"`
	Retrieved time.Time         `json:"retrieved_at"`
}

func (r *Runtime) ListLineVersions() []model.LineVersion {
	return r.Versions.List()
}

func (r *Runtime) GetLineVersion(id string) (model.LineVersion, error) {
	version, ok := r.Versions.Get(id)
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %s", model.ErrLineVersionNotFound, id)
	}
	return version, nil
}

func (r *Runtime) CreateDraft(input DraftInput, baseVersionID string) (model.LineVersion, error) {
	if err := validateOperator(input.Operator); err != nil {
		return model.LineVersion{}, err
	}
	return r.Versions.CreateDraft(lineconfig.DraftRequest{
		Name:     input.Name,
		Notes:    input.Notes,
		Config:   input.Config,
		Operator: input.Operator,
	}, strings.TrimSpace(baseVersionID))
}

func (r *Runtime) UpdateDraft(id string, input DraftInput) (model.LineVersion, error) {
	if err := validateOperator(input.Operator); err != nil {
		return model.LineVersion{}, err
	}
	return r.Versions.UpdateDraft(id, lineconfig.DraftRequest{
		Name:     input.Name,
		Notes:    input.Notes,
		Config:   input.Config,
		Operator: input.Operator,
	})
}

func (r *Runtime) DeleteDraft(id string, operator string) error {
	if err := validateOperator(operator); err != nil {
		return err
	}
	return r.Versions.DeleteDraft(id)
}

func (r *Runtime) ValidateDraft(id string) (model.ValidationReport, error) {
	return r.Versions.Validate(id)
}

// PublishLineVersion validates and freezes a draft, then cuts new operations
// over to it. Cutover is refused while any operation is still in flight so
// live valves, pressures and routes never operate against a half-switched
// line; completed and cancelled runs keep their historical version.
func (r *Runtime) PublishLineVersion(id string, operator, notes string) (model.LineVersion, error) {
	if err := validateOperator(operator); err != nil {
		return model.LineVersion{}, err
	}
	// Serialise against Launch so the "no operations in flight" guarantee
	// holds until the new baseline is installed.
	r.lineSwitchMu.Lock()
	defer r.lineSwitchMu.Unlock()
	if active := r.activeOperations(); len(active) > 0 {
		return model.LineVersion{}, fmt.Errorf("%w: %d operation(s) still active", lineconfig.ErrInFlight, len(active))
	}
	published, err := r.Versions.Publish(id, lineconfig.PublishRequest{Operator: operator, Notes: notes})
	if err != nil {
		return model.LineVersion{}, err
	}
	baseline, err := baselineControlState(published)
	if err != nil {
		return model.LineVersion{}, fmt.Errorf("rebuild control baseline: %w", err)
	}
	r.mu.Lock()
	persistent := r.control.Persistent
	previous := r.control
	baseline.Persistent = persistent
	// Incidents are operational audit history and survive a line cutover.
	baseline.Incidents = append([]model.Incident(nil), previous.Incidents...)
	// Keep per-run history that still resolves against the new topology so
	// completed runs remain reviewable; records bound to removed stations or
	// changed mileage are pruned to keep the persisted snapshot valid.
	newStations := baseline.Stations
	for runID, points := range previous.Tracking {
		for _, point := range points {
			if _, ok := newStations[point.StationID]; !ok {
				continue
			}
			if err := point.Validate(newStations); err != nil {
				continue
			}
			baseline.Tracking[runID] = append(baseline.Tracking[runID], point)
		}
	}
	for runID, permits := range previous.Permits {
		baseline.Permits[runID] = append(baseline.Permits[runID], permits...)
	}
	r.control = baseline
	r.mu.Unlock()
	if err := r.appendEvent(model.Event{
		Kind:    "line_version_published",
		Payload: published.ID,
		At:      published.PublishedAt,
	}); err != nil {
		return model.LineVersion{}, err
	}
	return published, nil
}

// ErrRunNotFound means the referenced operation does not exist.
var ErrRunNotFound = errors.New("run not found")

// ErrRunVersionMissing means a run was created before versions were tracked.
var ErrRunVersionMissing = errors.New("run predates line version tracking")

// OperationLineVersion returns the exact line version a run was created
// against, allowing historical review even after newer versions are active.
func (r *Runtime) OperationLineVersion(runID string) (LineVersionView, error) {
	op, ok := r.Runs.Get(runID)
	if !ok {
		return LineVersionView{}, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	if strings.TrimSpace(op.LineVersionID) == "" {
		return LineVersionView{}, fmt.Errorf("%w: %s", ErrRunVersionMissing, runID)
	}
	version, err := r.GetLineVersion(op.LineVersionID)
	if err != nil {
		return LineVersionView{}, err
	}
	return LineVersionView{Version: version, BoundRun: runID, Retrieved: time.Now().UTC()}, nil
}

// NetworkForRun builds the routing graph captured in a run's bound version.
func (r *Runtime) NetworkForRun(runID string) (*route.Network, model.LineVersion, error) {
	op, ok := r.Runs.Get(runID)
	if !ok {
		return nil, model.LineVersion{}, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	if strings.TrimSpace(op.LineVersionID) == "" {
		return nil, model.LineVersion{}, fmt.Errorf("%w: %s", ErrRunVersionMissing, runID)
	}
	return r.Versions.NetworkFor(op.LineVersionID)
}

func (r *Runtime) activeOperations() []model.Run {
	active := make([]model.Run, 0)
	for _, op := range r.Runs.Snapshot() {
		if !op.State.Terminal() {
			active = append(active, op)
		}
	}
	return active
}

// ErrOperatorRequired means an audited line-version change lacked identity.
var ErrOperatorRequired = errors.New("operator identity is required for line version changes")

func validateOperator(operator string) error {
	if strings.TrimSpace(operator) == "" {
		return ErrOperatorRequired
	}
	return nil
}
