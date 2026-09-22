// Package model defines the managed objects of the simulated device as Go
// structs carrying path, xml and json tags.
//
// The structs are the runtime schema: YANG files under yang/ are documentation
// only and are never parsed. Every model type implements Validate() error;
// read-only nodes carry the config:"false" tag and are rejected by the router
// when a management plane tries to write them.
package model
