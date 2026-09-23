#!/usr/bin/env bash
# Shared helpers for Phase 0 spikes. Source, don't execute.
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="$SPIKE_DIR/results"
mkdir -p "$RESULTS_DIR"

SPIKE_ID="${SPIKE_ID:-$(basename "$0" .sh)}"
RESULT_FILE="$RESULTS_DIR/$SPIKE_ID.txt"
: > "$RESULT_FILE"

FAILS=0
CHECKS=0

log() { printf '%s\n' "$*" | tee -a "$RESULT_FILE"; }
section() { log ""; log "== $*"; }
measure() { log "MEASURE $1 = $2"; }

check() {
  local name="$1"; shift
  CHECKS=$((CHECKS + 1))
  if "$@" >/dev/null 2>&1; then
    log "PASS  $name"
  else
    FAILS=$((FAILS + 1))
    log "FAIL  $name"
  fi
}

check_out() {
  local name="$1"; shift
  local out
  CHECKS=$((CHECKS + 1))
  if out="$("$@" 2>&1)"; then
    log "PASS  $name: $out"
  else
    FAILS=$((FAILS + 1))
    log "FAIL  $name: $out"
  fi
}

now_ms() { date +%s%3N; }

finish() {
  log ""
  if [ "$FAILS" -eq 0 ]; then
    log "RESULT $SPIKE_ID PASS ($CHECKS checks)"
  else
    log "RESULT $SPIKE_ID FAIL ($FAILS of $CHECKS checks failed)"
  fi
  [ "$FAILS" -eq 0 ]
}

cleanup_containers() {
  local names=("$@")
  docker rm -fv "${names[@]}" >/dev/null 2>&1 || true
}
