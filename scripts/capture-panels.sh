#!/bin/sh
# Capture every Systemdoc panel from the real binary against live data.
#
# Runs bin/systemdoc inside a detached tmux server sized 160x44 with 24-bit
# colour, drives it with keystrokes, and converts each tmux capture into the
# same cell JSON that TestVisualReview exports, so scripts/render-preview.py
# and rsvg-convert produce the PNGs. Needs tmux, python3, rsvg-convert and
# JetBrainsMono Nerd Font. Uses a throwaway settings directory so the default
# theme and layout are shown; HOME is kept so kubeconfig and Docker work.
#
#   scripts/capture-panels.sh [output-dir]      default docs/assets
set -eu
OUT=${1:-docs/assets}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
SOCKET=systemdoc-shots
W=160
H=44
trap 'tmux -L "$SOCKET" kill-server 2>/dev/null || true; rm -rf "$WORK"' EXIT
mkdir -p "$OUT"
[ -x "$ROOT/bin/systemdoc" ] || { echo "build first: make build" >&2; exit 1; }

cat >"$WORK/tmux.conf" <<'CONF'
set -g default-terminal "tmux-256color"
set -as terminal-features ",*:RGB"
set -g status off
set -g escape-time 0
CONF
tmux -L "$SOCKET" -f "$WORK/tmux.conf" new-session -d -x "$W" -y "$H" -e COLORTERM=truecolor -e "XDG_CONFIG_HOME=$WORK/config" -c "$ROOT" "$ROOT/bin/systemdoc"

keys() { tmux -L "$SOCKET" send-keys -t 0 "$@"; }
type_text() { tmux -L "$SOCKET" send-keys -t 0 -l "$1"; }
shot() {
	name=$1
	tmux -L "$SOCKET" capture-pane -e -p -t 0 -S 0 -E $((H - 1)) >"$WORK/$name.ansi"
	python3 "$ROOT/scripts/ansi-to-cells.py" "$WORK/$name.ansi" "$W" "$H" "$WORK/$name.json"
	python3 "$ROOT/scripts/render-preview.py" "$WORK/$name.json" "${W}x${H}" "$WORK/$name.svg"
	rsvg-convert -o "$OUT/$name.png" "$WORK/$name.svg"
	echo "captured $OUT/$name.png"
}
# Replace the filter text: / focuses the field, Ctrl-U clears it, Enter returns to the table.
filter() { keys /; keys C-u; type_text "$1"; keys Enter; sleep 1; }
clear_filter() { keys /; keys C-u; keys Enter; sleep 1; }
# Choosers open with the search field focused: Enter moves to the list, Enter again picks the row.
pick_first() { keys Enter; sleep 1; keys Enter; sleep 1; }

sleep 7                                   # inventory, splash dismissed
keys Escape; sleep 1
filter "docker"; keys o; sleep 3
shot dashboard
clear_filter

keys 2; sleep 6                           # containers + pods
keys A; keys A; sleep 1                   # quick filter: needs attention
keys Home; keys j; keys o; sleep 4        # second attention row is shop/crashy
shot containers
keys d; sleep 3
shot pod-connections
keys A; sleep 1                           # back to all states
filter "systemdoc-k0s"; keys o; sleep 3
shot docker
clear_filter

keys 4; sleep 6                           # processes, sorted by CPU
keys Home; sleep 1
shot processes
filter "command:ghostty"
keys Enter; sleep 7                       # process activity, two samples in
shot process-activity
keys T; sleep 2; pick_first               # sysdig scope: this process only
shot sysdig-palette
pick_first; sleep 1; keys y; sleep 10     # live syscalls, container starts
shot sysdig-stream
keys Escape; sleep 1; keys Escape; sleep 1

keys 3; sleep 4; keys s; sleep 12; keys End; sleep 1   # speed needs two counter samples; End selects the busiest, last-named interface
shot network-speed
keys Escape; sleep 1

keys 5; sleep 4
shot storage
keys Escape; sleep 1

keys 1; sleep 2
filter "containerd.service"; keys x; sleep 4
shot constellation
keys Escape; sleep 1
clear_filter
keys 0; sleep 2
shot control-deck
keys Escape; sleep 1
keys q; sleep 1
