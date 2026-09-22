// Package snmp implements the SNMP v2c management plane: the agent serving
// get/get-next/get-bulk for community "public", the OID tree built from
// internal/router, and the trap sender used for alarm notifications.
//
// SNMP v3, informs and authentication are out of scope.
package snmp
