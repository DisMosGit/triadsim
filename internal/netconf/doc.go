// Package netconf implements the NETCONF management plane: the SSH subsystem,
// the hello exchange and capabilities, end-of-message and chunked framing, the
// <rpc> envelope and its dispatcher. The configuration operations themselves
// live in internal/netconf/ops (get-config, edit-config, commit including the
// confirmed commit, discard-changes), and the RFC 5277 event notifications in
// internal/netconf/notif.
//
// A session runs on one SSH subsystem channel: it sends the server hello, reads
// the client hello, negotiates the framing and then answers RPCs against the
// shared store until the client closes the session or the channel breaks. A
// subscribed session also receives <notification> documents on the same
// channel, written through the same message writer so the two never interleave.
package netconf
