#!/usr/bin/env bash
#
# Stub for the canonical cross-domain demo scenario (see .docs/desicion.md 3.6):
#
#   POST /api/simulate/radio-failure
#     -> radio RSSI/fade margin drop -> AlarmRaised
#     -> PTP state transition to holdover
#     -> SNMP trap 1.3.6.1.4.1.99999.0.1 radioLinkDown on :1162
#     -> NETCONF <notification> to subscribed sessions
#     -> simulator_alarms_total{type="radio"} +1
#
# The simulation API is Phase 6 work and the script is completed in Phase 7.
set -euo pipefail

echo "demo.sh: the cross-domain scenario is not implemented yet." >&2
echo "It arrives with the simulation API (Phase 6); see ROADMAP.md." >&2
exit 1
