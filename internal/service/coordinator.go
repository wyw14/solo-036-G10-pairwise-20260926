package service

import (
	"sync"
	"time"

	"github.com/wyw14/solo-036/internal/compressor"
	"github.com/wyw14/solo-036/internal/journal"
	"github.com/wyw14/solo-036/internal/launcher"
	"github.com/wyw14/solo-036/internal/lineversion"
	"github.com/wyw14/solo-036/internal/permit"
	"github.com/wyw14/solo-036/internal/pressure"
	"github.com/wyw14/solo-036/internal/receiver"
	"github.com/wyw14/solo-036/internal/route"
	"github.com/wyw14/solo-036/internal/run"
	"github.com/wyw14/solo-036/internal/tracker"
	"github.com/wyw14/solo-036/internal/valve"
)

type Runtime struct {
	Runs         *run.Store
	Routes       *route.Coordinator
	Tracker      *tracker.Coordinator
	Lineage      *tracker.SourceLineage
	Journal      *journal.Store
	Pressure     *pressure.Controller
	Permits      *permit.Evaluator
	Launcher     *launcher.Controller
	Valves       *valve.Controller
	Receiver     *receiver.Controller
	Compressor   *compressor.Worker
	LineVersions *lineversion.Registry
	mu           sync.Mutex
	control      *ControlState
}

func NewRuntime() *Runtime {
	runs := run.NewStore()
	routes := route.NewCoordinator()
	tracks := tracker.NewCoordinator()
	lineage := tracker.NewSourceLineage()
	journalStore := journal.NewStore()
	p := pressure.NewController()
	permits := permit.NewEvaluator()
	launch := launcher.NewController(p, permits)
	valves := valve.NewController(p)
	recv := receiver.NewController()
	worker := compressor.NewWorker(p, runs)
	versions := lineversion.NewRegistry()
	r := &Runtime{
		Runs:         runs,
		Routes:       routes,
		Tracker:      tracks,
		Lineage:      lineage,
		Journal:      journalStore,
		Pressure:     p,
		Permits:      permits,
		Launcher:     launch,
		Valves:       valves,
		Receiver:     recv,
		Compressor:   worker,
		LineVersions: versions,
		control:      nil,
	}
	baseline, err := versions.SeedBaseline(route.BuiltinLineContent(), time.Now().UTC())
	if err != nil {
		panic(err)
	}
	r.control = r.buildControl(baseline)
	return r
}
