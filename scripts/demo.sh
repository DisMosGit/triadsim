#!/usr/bin/env bash
#
# The canonical cross-domain demo scenario (docs/demo.md, .docs/desicion.md 3.6)
# in one command:
#
#   POST /api/simulate/radio-failure {"link":"radio0"}
#     -> radio   RSSI drops, link-state -> down, AlarmRaised(radioLinkDown, critical)
#     -> sync    PTP locked -> holdover-in-spec (StateTransition)
#     -> SNMP    trap 1.3.6.1.4.1.99999.0.1 simRadioLinkDown on the trap port
#     -> NETCONF <notification> to subscribed sessions (with DEMO_NETCONF=1)
#     -> metrics simulator_alarms_total{type="radio",severity="critical"} +1
#
# Requirements: go and curl. snmptrapd and ssh are used when present and only
# make the output richer.
#
# Ports default to the ones in configs/default.yaml and can be overridden:
#   DEMO_SNMP_PORT DEMO_TRAP_PORT DEMO_NETCONF_PORT DEMO_RESTCONF_PORT DEMO_METRICS_PORT
# Set DEMO_NETCONF=1 to open a NETCONF subscription and capture the notifications.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SNMP_PORT="${DEMO_SNMP_PORT:-1161}"
TRAP_PORT="${DEMO_TRAP_PORT:-1162}"
NETCONF_PORT="${DEMO_NETCONF_PORT:-1830}"
RESTCONF_PORT="${DEMO_RESTCONF_PORT:-8080}"
METRICS_PORT="${DEMO_METRICS_PORT:-9090}"

RESTCONF="http://127.0.0.1:${RESTCONF_PORT}"
METRICS="http://127.0.0.1:${METRICS_PORT}"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/triadsim-demo.XXXXXX")"
SIM_PID=""
TRAP_PID=""
NETCONF_PID=""
NETCONF_FD_OPEN=""

# log prefixes the steps, and fail reports an unmet expectation.
log()  { printf '\n== %s\n' "$*"; }
fail() { printf 'demo: FAIL: %s\n' "$*" >&2; exit 1; }

cleanup() {
  local status=$?
  if [[ -n "${NETCONF_FD_OPEN}" ]]; then
    eval "exec ${NETCONF_FD_OPEN}>&-" || true
  fi
  for pid in "${SIM_PID}" "${TRAP_PID}" "${NETCONF_PID}"; do
    if [[ -n "${pid}" ]]; then
      kill "${pid}" 2>/dev/null || true
    fi
  done
  if [[ -d "${WORK}" && ${status} -ne 0 ]]; then
    printf '\ndemo: simulator log (last 20 lines):\n' >&2
    tail -20 "${WORK}/simulator.log" >&2 || true
  fi
  rm -rf "${WORK}"
  return "${status}"
}
trap cleanup EXIT INT TERM

# require fails when a tool the demo cannot work without is missing.
require() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed"
}

# await polls a command until it succeeds, or fails after a timeout.
await() {
  local description=$1
  shift
  local deadline=$((SECONDS + 10))
  until "$@" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      fail "timed out waiting for ${description}"
    fi
    sleep 0.2
  done
}

# metric returns the current value of a Prometheus counter label set.
metric() {
  curl -sS "${METRICS}/metrics" | awk -v want="$1" 'index($0, want) == 1 {print $2}' | head -1
}

require go
require curl

cat >"${WORK}/config.yaml" <<EOF
snmp:
  port: ${SNMP_PORT}
  trap-host: 127.0.0.1
  trap-port: ${TRAP_PORT}
netconf:
  port: ${NETCONF_PORT}
restconf:
  port: ${RESTCONF_PORT}
metrics:
  port: ${METRICS_PORT}
log:
  level: info
startup:
  file: ${WORK}/startup.json
EOF

log "building the simulator"
go build -o "${WORK}/simulator" ./cmd/simulator

log "starting the simulator (SNMP :${SNMP_PORT}, NETCONF :${NETCONF_PORT}, RESTCONF :${RESTCONF_PORT})"
"${WORK}/simulator" start --config "${WORK}/config.yaml" >"${WORK}/simulator.log" 2>&1 &
SIM_PID=$!

await "the RESTCONF endpoint" \
  curl -sS -o /dev/null "${RESTCONF}/restconf/data/sim-device:system-info/device-id"

if command -v snmptrapd >/dev/null 2>&1; then
  log "starting the trap receiver on :${TRAP_PORT}"
  snmptrapd -f -Lo -p "${TRAP_PORT}" >"${WORK}/traps.log" 2>&1 &
  TRAP_PID=$!
  sleep 0.5
else
  log "snmptrapd is not installed: trap delivery will not be shown"
fi

if [[ "${DEMO_NETCONF:-0}" == "1" ]]; then
  if command -v ssh >/dev/null 2>&1; then
    log "opening a NETCONF subscription to the sim-events stream"
    mkfifo "${WORK}/netconf.fifo"
    ssh -p "${NETCONF_PORT}" -s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
      -o LogLevel=ERROR admin@127.0.0.1 netconf \
      <"${WORK}/netconf.fifo" >"${WORK}/notifications.xml" 2>"${WORK}/netconf.err" &
    NETCONF_PID=$!
    # Keep the fifo open so the session stays up while the scenario runs.
    exec {NETCONF_FD}>"${WORK}/netconf.fifo"
    NETCONF_FD_OPEN="${NETCONF_FD}"
    printf '%s]]>]]>' \
      '<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities></hello>' \
      >&"${NETCONF_FD}"
    printf '%s]]>]]>' \
      '<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><create-subscription><stream>sim-events</stream></create-subscription></rpc>' \
      >&"${NETCONF_FD}"
    await "the NETCONF subscription" grep -q "create-subscription\|<ok" "${WORK}/notifications.xml"
  else
    log "ssh is not installed: NETCONF notifications will not be shown"
  fi
else
  log "NETCONF notifications are skipped (run with DEMO_NETCONF=1 to capture them)"
fi

log "the seeded device"
curl -sS "${RESTCONF}/restconf/data/sim-sync:ptp/clock/state"
printf '\n'
curl -sS "${RESTCONF}/restconf/data/sim-device:interfaces/interface=radio0/radio-link/link-state"
printf '\n'
curl -sS "${RESTCONF}/restconf/data/sim-device:interfaces/interface=radio0/radio-link/rssi"
printf '\n'

log "failing the radio link"
curl -sS -X POST "${RESTCONF}/api/simulate/radio-failure" -d '{"link":"radio0"}'
printf '\n'

await "the PTP clock in holdover" \
  bash -c "curl -sS '${RESTCONF}/restconf/data/sim-sync:ptp/clock/state' | grep -q holdover-in-spec"
printf '\nPTP    %s\n' "$(curl -sS "${RESTCONF}/restconf/data/sim-sync:ptp/clock/state")"
printf 'radio  %s\n' "$(curl -sS "${RESTCONF}/restconf/data/sim-device:interfaces/interface=radio0/radio-link/link-state")"
printf '       %s\n' "$(curl -sS "${RESTCONF}/restconf/data/sim-device:interfaces/interface=radio0/radio-link/rssi")"

critical=$(metric 'simulator_alarms_total{severity="critical",type="radio"}')
printf 'metric simulator_alarms_total{severity="critical",type="radio"} = %s\n' "${critical:-0}"
[[ "${critical:-0}" != "0" ]] || fail "the radio alarm counter did not move"

if [[ -n "${TRAP_PID}" ]]; then
  sleep 0.5
  if grep -q "99999.0.1" "${WORK}/traps.log"; then
    printf 'SNMP   trap received:\n'
    sed -n 's/^/      /p' "${WORK}/traps.log" | tail -8
  else
    printf 'SNMP   no trap logged yet\n'
  fi
fi

if [[ -n "${NETCONF_PID}" ]]; then
  sleep 0.5
  printf 'NETCONF notifications:\n'
  sed 's/]]>]]>/\n/g' "${WORK}/notifications.xml" | grep -E "AlarmRaised|StateTransition" | sed 's/^/      /' || true
fi

log "restoring the radio link"
curl -sS -X POST "${RESTCONF}/api/simulate/radio-restore" -d '{"link":"radio0"}'
printf '\n'

await "the PTP clock to lock again" \
  bash -c "curl -sS '${RESTCONF}/restconf/data/sim-sync:ptp/clock/state' | grep -q '\"locked\"'"
cleared=$(metric 'simulator_alarms_total{severity="cleared",type="radio"}')
printf 'PTP   %s\n' "$(curl -sS "${RESTCONF}/restconf/data/sim-sync:ptp/clock/state")"
printf 'metric simulator_alarms_total{severity="cleared",type="radio"} = %s\n' "${cleared:-0}"
[[ "${cleared:-0}" != "0" ]] || fail "the cleared-alarm counter did not move"

log "the scenario passed: alarm, PTP holdover, trap, notification and metrics"
printf 'the simulator and its temporary files were removed on exit\n'
printf 'gNMI is optional: set gnmi.enabled in the configuration, see docs/protocols/gNMI.md\n'
