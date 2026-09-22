// Package sync implements the synchronization domain: the PTP state machine
// (freerun, master, holdover), SyncE source selection, ESMC/SSM quality
// levels, holdover timing and simulated offset/jitter.
//
// PTP and SyncE are simplified state machines, not real protocol stacks. All
// timers go through internal/clock so tests stay deterministic. The domain
// reacts to events from internal/event and never imports the other domains.
package sync
