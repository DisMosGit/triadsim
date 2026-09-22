// Package gnmi implements the optional gNMI management plane: a thin gRPC
// layer over internal/router offering Capabilities, Get, Set and a minimal
// Subscribe.
//
// The service is disabled by default (config GNMI.Enabled) and runs without
// TLS and without authentication, like the other planes. It reads and writes
// the running datastore only: a Set never touches candidate or startup.json,
// which mirrors RESTCONF.
//
// The implemented surface is deliberately small and the server advertises only
// what it serves:
//
//   - Capabilities: the four simulator models and the encodings JSON,
//     JSON_IETF and PROTO.
//   - Get: CONFIG, STATE, OPERATIONAL and ALL, delivered as one Update per
//     leaf whose path is relative to the requested path.
//   - Set: replace, update and delete. A path in replace or update must
//     address a leaf; delete accepts a leaf, a list entry or a container
//     subtree. Every operation of one request is applied in a single
//     data-tree edit, so the request is atomic. union_replace and subtree
//     values are not supported.
//   - Subscribe: ONCE and STREAM/ON_CHANGE. SAMPLE, TARGET_DEFINED and POLL
//     answer Unimplemented. ON_CHANGE maps EventBus events onto model paths
//     and re-reads the affected subtree.
//
// Extensions, heartbeat intervals, QoS marking and aggregation are ignored.
package gnmi
