// Package notif implements NETCONF event notifications: the
// <create-subscription> operation of RFC 5277 and the dispatcher that turns
// events from the EventBus into <notification> documents for every subscribed
// session.
//
// The session, the framing and the <rpc> envelope live in the parent netconf
// package; this package owns the RFC 5277 surface: one stream (sim-events), the
// notification document, and the subscription registry. Replay and filters are
// not implemented.
package notif
