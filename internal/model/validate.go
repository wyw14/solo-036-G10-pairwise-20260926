package model

func ValidateRun(r Run) bool {
	return r.ID != "" && r.Station != "" && r.State != "" && r.Epoch > 0 && r.LineVersion > 0
}
func ValidEvidence(e PassageEvidence) bool {
	return e.RunID != "" && e.Station != "" && e.SourceID != "" && e.Generation > 0 && e.Valid
}
func NewRun(id, station string, epoch int64, lineVersion int) Run {
	return Run{ID: id, Station: station, State: StateLoaded, Epoch: epoch, LineVersion: lineVersion}
}
