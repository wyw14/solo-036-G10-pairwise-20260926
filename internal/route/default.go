package route

import "github.com/wyw14/solo-036/internal/model"

// BuiltinLineContent 返回系统内置的第一版线路拓扑，
// 作为首次启动时已发布的基线版本，取代此前硬编码且无法回看的默认网络。
func BuiltinLineContent() model.LineContent {
	return model.LineContent{
		Note: "builtin baseline line",
		Stations: []model.Station{
			{ID: "station-a", Name: "North Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 12.5, Enabled: true},
			{ID: "station-b", Name: "Central Block Valve", Role: model.StationIntermediate, Kilometer: 86.4, PressureLimit: 11.8, Enabled: true},
			{ID: "station-c", Name: "South Receiver", Role: model.StationReceiver, Kilometer: 173.2, PressureLimit: 11.2, Enabled: true},
		},
		Segments: []Segment{
			{ID: "segment-a-b", From: "station-a", To: "station-b", Length: 86.4, MaximumPressure: 11.8, Enabled: true},
			{ID: "segment-b-c", From: "station-b", To: "station-c", Length: 86.8, MaximumPressure: 11.2, Enabled: true},
		},
	}
}
