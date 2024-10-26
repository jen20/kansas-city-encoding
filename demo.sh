#!/usr/bin/env bash
set -euo pipefail

KCS=${KCS:-./kcs}
OUT=${OUT:-./demo-out}
MSG=${MSG:-"GREETINGS FROM KANSAS CITY"}

mkdir -p "$OUT"
[ -x "$KCS" ] || go build -o "$KCS" ./cmd/kcs

step() {
  echo
  echo "──────────────────────────────────────────────────────────────"
  echo "  $1"
  echo "──────────────────────────────────────────────────────────────"
  shift
  echo "\$ $*"
  echo
  read -rsp "" -n1 || true
  "$@"
  echo
  read -rsp "  [enter to continue]" || true
  echo
}

step "1. Encode a message. 27 bytes per second." \
  "$KCS" encode -text "$MSG" -out "$OUT/message.wav" -leader 1

echo "  Play it: afplay $OUT/message.wav"
read -rsp "  [enter to continue]" || true

step "2. Read it back" \
  "$KCS" decode -in "$OUT/message.wav" -v

step "3. Look at the two tone populations, and the gap between them" \
  "$KCS" analyze -in "$OUT/message.wav"

step "4. Now put it through a cassette deck from 1978" \
  "$KCS" stress -channel typical -text "$MSG" -save "$OUT/cassette.wav"

echo "  Hear the damage: afplay $OUT/cassette.wav"
read -rsp "  [enter to continue]" || true

step "5. A tape playing 15% fast. The decoder does not care." \
  "$KCS" stress -channel perfect -speed 1.15 -text "$MSG"

step "6. The whole tolerance envelope" \
  "$KCS" sweep

step "7. The input filter effect" \
  "$KCS" sweep -nofilter

step "8. And the tape that lived in a glovebox since 1975" \
  "$KCS" stress -channel awful -text "$MSG" -save "$OUT/awful.wav"

echo
echo "Files in $OUT — square-wave version for the Sound System:"
echo "  $KCS encode -text \"$MSG\" -wave square -out $OUT/pa.wav"
echo
