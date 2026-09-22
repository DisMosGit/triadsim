// Package sync implements the synchronization domain: the PTP state machine
// (the G.8275.1 clock states freerun, acquiring, locked, holdover-in-spec and
// holdover-out-of-spec), SyncE source selection, ESMC/SSM quality levels,
// holdover timing and simulated offset/jitter.
//
// The manager (manager.go) owns the periodic work and the datastore reads and
// writes, the state machine lives in ptp.go, the source selection in synce.go
// and the quality-level mapping in esmc.go.
//
// PTP and SyncE are simplified state machines, not real protocol stacks. All
// timers go through internal/clock so tests stay deterministic. The domain
// reacts to events from internal/event and never imports the other domains.
package sync
