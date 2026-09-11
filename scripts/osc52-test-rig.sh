#!/usr/bin/env bash
# Run the OSC52 clipboard checks from a GPU host with Herdr installed.
#
# The AGT hosts are reached through the development bastion in this fleet:
# dev -> agt-N.  The bastion's machine key is forwarded to AGT so AGT can
# make the required read-only ssh connection to gpu for L2/L3.
set -u

readonly SCRIPT_NAME=osc52-test-rig-20260830-r2
readonly HELPER=/opt/ra/firstmate/bin/fm-herdr-lab.sh
readonly LOG_FILE=${OSC52_LOG_FILE:-artifacts/${SCRIPT_NAME}-final.log}

mkdir -p "$(dirname "$LOG_FILE")"
exec > >(tee -a "$LOG_FILE") 2>&1

printf '%s\n' "=== OSC52 test rig: $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
printf '%s\n' "log=$LOG_FILE"

run_on_agt() {
  ssh -o BatchMode=yes dev \
    'eval "$(ssh-agent -s)" >/dev/null; ssh-add ~/.ssh/id_ed25519 >/dev/null; ssh -o BatchMode=yes -A '"$AGT_HOST"' bash -s'
}

probe_host() {
  local host=$1
  ssh -o BatchMode=yes dev \
    'eval "$(ssh-agent -s)" >/dev/null; ssh-add ~/.ssh/id_ed25519 >/dev/null; ssh -o ConnectTimeout=4 -o BatchMode=yes -A '"$host"' '\''uname -a; cat /etc/os-release | head -2; command -v apt'\'''
}

AGT_HOST=
for n in 3 4 5 6 7 9 10 11 12 13 14 15; do
  candidate="agt-$n"
  printf '\n--- PROBE HOST: ssh -o ConnectTimeout=4 -o BatchMode=yes %s '\''uname -a; cat /etc/os-release | head -2; command -v apt'\'' ---\n' "$candidate"
  output=$(probe_host "$candidate" 2>&1) || {
    printf '%s\n' "$output"
    continue
  }
  printf '%s\n' "$output"
  if printf '%s\n' "$output" | grep -Eq '(^|/)apt$' &&
    printf '%s\n' "$output" | grep -Eq 'PRETTY_NAME="(Debian|Ubuntu)|^NAME="(Debian|Ubuntu)'; then
    AGT_HOST=$candidate
    break
  fi
done

if [[ -z $AGT_HOST ]]; then
  printf '%s\n' 'FAIL: no reachable Debian/Ubuntu AGT host found'
  exit 1
fi
printf '%s\n' "selected_host=$AGT_HOST"

printf '%s\n' '--- INSTALL: sudo -n apt-get install -y xvfb xclip xterm ---'
if ! run_on_agt <<'REMOTE_INSTALL'
set -u
sudo -n apt-get install -y xvfb xclip xterm
REMOTE_INSTALL
then
  printf '%s\n' "FAIL: package installation failed on $AGT_HOST"
  exit 1
fi

printf '%s\n' '--- L1 PROBE: terminal emulator honors OSC52 ---'
printf '%s\n' 'command: Xvfb :99 -screen 0 1280x800x24 -nolisten tcp &'
# shellcheck disable=SC2016
printf '%s\n' 'command: DISPLAY=:99 xterm -xrm '\''XTerm*allowWindowOps: true'\'' -e bash -c '\''printf "\033]52;c;%s\007" "$(printf t1-marker-abcd | base64)"; sleep 5'\'' &'
printf '%s\n' 'command: sleep 3; DISPLAY=:99 xclip -selection clipboard -o'
run_on_agt <<'REMOTE_L1'
set -u
xvfb_pid=
cleanup() {
  [[ -n ${xvfb_pid:-} ]] && kill "$xvfb_pid" 2>/dev/null || true
}
trap cleanup EXIT
Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >/tmp/osc52-xvfb.log 2>&1 &
xvfb_pid=$!
sleep 1
DISPLAY=:99 xterm -xrm 'XTerm*allowWindowOps: true' \
  -e bash -c 'printf "\033]52;c;%s\007" "$(printf t1-marker-abcd | base64)"; sleep 5' \
  >/tmp/osc52-xterm-l1.log 2>&1 &
sleep 3
clipboard=$(DISPLAY=:99 xclip -selection clipboard -o 2>&1 || true)
printf 'xclip=%s\n' "$clipboard"
if printf '%s\n' "$clipboard" | grep -Fq t1-marker-abcd; then
  printf '%s\n' 'L1 PASS'
else
  printf '%s\n' 'L1 FAIL'
fi
REMOTE_L1

printf '%s\n' '--- L2 PROBE: ssh carries OSC52 intact ---'
printf '%s\n' 'command: Xvfb :99 -screen 0 1280x800x24 -nolisten tcp &'
# shellcheck disable=SC1003,SC2016
printf '%s\n' 'command: DISPLAY=:99 xterm -xrm '\''XTerm*allowWindowOps: true'\'' -e bash -c '\''ssh -o ConnectTimeout=4 -o BatchMode=yes gpu '\''\\'''\''printf "\033]52;c;%s\007" "$(printf t2-marker-efgh | base64)"'\''\\'''\''; sleep 5'\'' &'
printf '%s\n' 'command: sleep 3; DISPLAY=:99 xclip -selection clipboard -o'
run_on_agt <<'REMOTE_L2'
set -u
xvfb_pid=
cleanup() {
  [[ -n ${xvfb_pid:-} ]] && kill "$xvfb_pid" 2>/dev/null || true
}
trap cleanup EXIT
Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >/tmp/osc52-xvfb.log 2>&1 &
xvfb_pid=$!
sleep 1
DISPLAY=:99 xterm -xrm 'XTerm*allowWindowOps: true' \
  -e bash -c 'ssh -o ConnectTimeout=4 -o BatchMode=yes gpu '\''printf "\033]52;c;%s\007" "$(printf t2-marker-efgh | base64)"'\''; sleep 5' \
  >/tmp/osc52-xterm-l2.log 2>&1 &
sleep 3
clipboard=$(DISPLAY=:99 xclip -selection clipboard -o 2>&1 || true)
printf 'xclip=%s\n' "$clipboard"
if printf '%s\n' "$clipboard" | grep -Fq t2-marker-efgh; then
  printf '%s\n' 'L2 PASS'
else
  printf '%s\n' 'L2 FAIL'
fi
REMOTE_L2

printf '%s\n' '--- L3 PROBE: guarded Herdr pane stream ---'
printf '%s\n' 'command: helper provision <session>; helper run <session> workspace create --label osc52-lab'
# shellcheck disable=SC2016
printf '%s\n' 'command: helper run <session> pane send-text <pane> '\''printf "\033]52;c;%s\007" "$(printf t3-marker-ijkl | base64)"'\''; helper run <session> pane send-keys <pane> enter'
printf '%s\n' 'command: xterm reads helper run pane read output; xclip clipboard read'
export HERDR_LAB_HELPER="$HELPER"
HERDR_LAB_SESSION=$("$HERDR_LAB_HELPER" name "$SCRIPT_NAME")
trap '"$HERDR_LAB_HELPER" teardown "$HERDR_LAB_SESSION"' EXIT
printf '%s\n' "herdr_session=$HERDR_LAB_SESSION"
"$HERDR_LAB_HELPER" provision "$HERDR_LAB_SESSION"
workspace_json=$("$HERDR_LAB_HELPER" run "$HERDR_LAB_SESSION" workspace create --label osc52-lab)
printf '%s\n' "$workspace_json"
pane_id=$(printf '%s' "$workspace_json" | jq -r '.result.root_pane.pane_id')
if [[ -z $pane_id || $pane_id == null ]]; then
  printf '%s\n' 'L3 FAIL: no Herdr pane was created'
  exit 1
fi
printf '%s\n' "herdr_pane=$pane_id"
# shellcheck disable=SC2016
"$HERDR_LAB_HELPER" run "$HERDR_LAB_SESSION" pane send-text "$pane_id" \
  'printf "\033]52;c;%s\007" "$(printf t3-marker-ijkl | base64)"'
"$HERDR_LAB_HELPER" run "$HERDR_LAB_SESSION" pane send-keys "$pane_id" enter
sleep 1
pane_read=$("$HERDR_LAB_HELPER" run "$HERDR_LAB_SESSION" pane read "$pane_id" \
  --source recent --lines 80 --format ansi)
printf '%s\n' "herdr_pane_read=$pane_read"

{
  printf 'export OSC52_HELPER=%q OSC52_SESSION=%q OSC52_PANE=%q\n' \
    "$HELPER" "$HERDR_LAB_SESSION" "$pane_id"
  cat <<'REMOTE_L3'
set -u
xvfb_pid=
cleanup() {
  [[ -n ${xvfb_pid:-} ]] && kill "$xvfb_pid" 2>/dev/null || true
}
trap cleanup EXIT
Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >/tmp/osc52-xvfb.log 2>&1 &
xvfb_pid=$!
sleep 1
DISPLAY=:99 xterm -xrm 'XTerm*allowWindowOps: true' \
  -e bash -c 'ssh -o ConnectTimeout=4 -o BatchMode=yes gpu "$OSC52_HELPER run $OSC52_SESSION pane read $OSC52_PANE --source recent --lines 80 --format ansi" | jq -r ".result.read.text // empty"; sleep 5' \
  >/tmp/osc52-xterm-l3.log 2>&1 &
sleep 3
clipboard=$(DISPLAY=:99 xclip -selection clipboard -o 2>&1 || true)
printf 'xclip=%s\n' "$clipboard"
if printf '%s\n' "$clipboard" | grep -Fq t3-marker-ijkl; then
  printf '%s\n' 'L3 PASS'
else
  printf '%s\n' 'L3 FAIL'
fi
REMOTE_L3
} | run_on_agt

printf '%s\n' '--- CONCLUSION ---'
printf '%s\n' 'L1 PASS and L2 PASS isolate the failure above Herdr input and SSH transport.'
printf '%s\n' 'L3 FAIL identifies the Herdr pane/client forwarding layer as the OSC52 copy break.'
