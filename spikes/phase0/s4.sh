#!/usr/bin/env bash
# S4: PROMPT_COMMAND history hook coverage, and a concurrent checker exec does not disturb the shell.
source "$(dirname "$0")/lib.sh"

IMG=nsl/node
C=nsl-s4-node
trap 'cleanup_containers $C' EXIT
cleanup_containers $C

docker run -d --name "$C" --hostname web01 --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock --tmpfs /tmp "$IMG" >/dev/null
for _ in $(seq 1 120); do
  case "$(docker exec "$C" systemctl is-system-running 2>/dev/null || true)" in running|degraded) break;; esac; sleep 0.25
done
LOG=/var/log/nsl/commands.jsonl

section "interactive login shell as root (docker exec -i bash -il)"
(for i in $(seq 1 8); do docker exec "$C" sh -c "systemctl is-active ssh" >/dev/null; sleep 0.5; done) &
checker=$!
shell() { docker exec -i "$@" script -qfc "bash -il" /dev/null > /dev/null 2>&1 || true; }
printf '%s\n' 'cd /tmp' 'ls -d /etc' 'false' 'echo   "quoted   spaces"' 'true | false' 'exit' | shell "$C"
sleep 1
wait $checker
check "log file exists" docker exec "$C" test -s "$LOG"
log "$(docker exec "$C" cat "$LOG")"
check "records cd /tmp"           docker exec "$C" sh -c "grep -q '\"cmd\":\"cd /tmp\"' $LOG"
check "cwd updated after cd"      docker exec "$C" sh -c "grep '\"cmd\":\"ls -d /etc\"' $LOG | grep -q '\"cwd\":\"/tmp\"'"
check "exit code of false is 1"   docker exec "$C" sh -c "grep '\"cmd\":\"false\"' $LOG | grep -q '\"exit\":1'"
check "quotes preserved"          docker exec "$C" sh -c "grep -q 'quoted   spaces' $LOG"
check "pipeline recorded whole"   docker exec "$C" sh -c "grep -q '\"cmd\":\"true | false\"' $LOG"
check "every line is valid JSON"  docker exec "$C" sh -c "jq -e . $LOG >/dev/null"
check "no spurious entry from .bash_history at first prompt" docker exec "$C" sh -c "test \"\$(wc -l < $LOG)\" -eq 5"
check "user field is root"        docker exec "$C" sh -c "grep -q '\"user\":\"root\"' $LOG"

section "non-login interactive shell (bash -i) also hooked"
docker exec "$C" sh -c ": > $LOG"
printf '%s\n' 'id -un' 'exit' | docker exec -i "$C" script -qfc "bash -i" /dev/null >/dev/null 2>&1 || true
sleep 1
check "bash -i records" docker exec "$C" sh -c "grep -q '\"cmd\":\"id -un\"' $LOG"

section "nsl user, sudo -i and su"
docker exec "$C" sh -c ": > $LOG"
printf '%s\n' 'whoami' 'sudo -i' 'whoami' 'exit' 'exit' | shell -u nsl "$C"
sleep 1
log "$(docker exec "$C" cat "$LOG")"
check "nsl user commands recorded"      docker exec "$C" sh -c "grep '\"user\":\"nsl\"' $LOG | grep -q '\"cmd\":\"whoami\"'"
check "sudo -i itself recorded"         docker exec "$C" sh -c "grep -q '\"cmd\":\"sudo -i\"' $LOG"
check "commands inside sudo -i recorded as root" docker exec "$C" sh -c "grep '\"user\":\"root\"' $LOG | grep -q '\"cmd\":\"whoami\"'"

section "gaps (informational)"
docker exec "$C" sh -c ": > $LOG"
printf '%s\n' 'sh -c "echo inside-sh"' 'bash -c "echo inside-bash-c"' 'exit' | shell "$C"
sleep 1
log "non-interactive children are not recorded (expected): $(docker exec "$C" sh -c "grep -c inside- $LOG") outer entries, inner commands only visible as the outer string"

finish
