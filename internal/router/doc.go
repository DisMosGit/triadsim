// Package router maps between management-plane addresses and the managed
// objects in internal/model.
//
// It owns two mappings — path to model navigation via the path tags
// (a/b[c=d]/e) and OID to path for the SNMP MIB tables — plus the RPC dispatch
// used by every management plane.
package router
