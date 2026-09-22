// Package model defines the managed objects of the simulated device as Go
// structs carrying path, xml and json tags.
//
// The structs are the runtime schema: YANG files under yang/ are documentation
// only and are never parsed. Every model type implements Validate() error;
// read-only nodes carry the config:"false" tag and are rejected by the router
// when a management plane tries to write them.
//
// The package groups its objects by domain: Device and SystemInfo are the root
// (device.go), Interface and the L2 objects live in l2.go, RadioLink and the
// radio objects in radio.go, and PTPClock, SyncEState, QL and ESMC in sync.go.
// Validate enforces the documented ranges and enumerations; the accepted
// values are exported as constants next to each type.
package model
