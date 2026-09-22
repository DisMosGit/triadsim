// Package l2 implements the L2 switching domain: VLAN and QinQ, the MAC
// table, a simplified STP/RSTP state machine, LLDP neighbours, interface
// counters and broadcast-storm simulation.
//
// STP/RSTP is a simplified state machine, not a real protocol implementation.
// The domain reacts to events from internal/event and never imports the other
// domains.
package l2
