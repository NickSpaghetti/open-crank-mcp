#!/usr/bin/env bash
set -euo pipefail

if ldd "$PLAYDATE_SDK_PATH/bin/PlaydateSimulator" | grep -q "not found"; then
  echo "missing shared libraries:"
  ldd "$PLAYDATE_SDK_PATH/bin/PlaydateSimulator" | grep "not found"
  exit 1
fi

pdc --version

Xvfb :99 -screen 0 1280x800x24 &
sleep 1

set +e
# SIGKILL, not the default SIGTERM: the simulator doesn't exit on SIGTERM.
timeout -s KILL 5 "$PLAYDATE_SDK_PATH/bin/PlaydateSimulator" >/tmp/sim.log 2>&1
code=$?
set -e

if grep -qiE "could not be initalized|could not be initialized|error|not found" /tmp/sim.log; then
  echo "simulator reported an error:"
  cat /tmp/sim.log
  exit 1
fi

# 137 (killed by our timeout) or 124 (timeout's own timeout code) both mean
# it was still running happily when we killed it - that's success for a GUI app.
if [ "$code" -ne 137 ] && [ "$code" -ne 124 ]; then
  echo "simulator exited early with code $code:"
  cat /tmp/sim.log
  exit 1
fi

echo "smoke check passed"
