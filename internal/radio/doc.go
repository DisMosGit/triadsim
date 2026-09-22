// Package radio implements the radio-link (RRL) domain: link budget, RSSI,
// fade margin, capacity, ATPC, ACM and the radioLinkDown/radioLinkDegraded
// alarms.
//
// The domain is a deliberate simplification of a real radio stack. It never
// imports the other domains: Manager.Run recalculates every link on the
// injected clock and publishes its alarms on the event bus, which the sync
// domain and the management planes consume. RadioFailure and RadioRestore
// inject and clear a fade for the simulation API.
package radio
