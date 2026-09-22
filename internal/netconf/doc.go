// Package netconf implements the NETCONF management plane: the SSH subsystem,
// the hello exchange and capabilities, end-of-message and chunked framing, the
// <rpc> envelope and its dispatcher. The configuration operations themselves
// live in internal/netconf/ops: get-config, edit-config, commit and
// discard-changes today, confirmed-commit and create-subscription notifications
// in a later phase.
//
// A session runs on one SSH subsystem channel: it sends the server hello, reads
// the client hello, negotiates the framing and then answers RPCs against the
// shared store until the client closes the session or the channel breaks.
package netconf
