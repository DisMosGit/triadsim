// Package ops implements the NETCONF configuration operations of the simulator:
// get-config, edit-config, commit (including the confirmed commit of RFC 4741
// §8.4) and discard-changes.
//
// The operations work on the router schema, on the running/candidate/startup
// datastores and on the event bus. The SSH transport, the hello exchange, the
// framing and the RPC envelope live in the parent netconf package, which calls
// these functions with a parsed <rpc> payload and turns the returned Error into
// an <rpc-error>.
package ops
