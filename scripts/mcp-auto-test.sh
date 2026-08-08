#!/usr/bin/env bash
# Runs Specmatic's MCP auto-test against this server.
#
# Auto-test needs no spec file: it reads tools/list off the running server, generates
# inputs from each tool's declared schema, calls it, and validates the response against
# the declared output schema. There is no second artifact describing the tool surface, so
# there is nothing that can drift from it - which is why this layer is worth having
# alongside internal/mcpcontract's golden file rather than instead of it.
#
# It speaks Streamable HTTP and not stdio (--transport-kind accepts only
# STREAMABLE_HTTP), which is why the server has an -http flag at all.
#
# --enable-resiliency-tests is where most of the value is: on top of one call per tool it
# mutates each argument's type, drops mandatory keys, and walks every enum value,
# expecting the server to reject what it should reject. That turns two tools into about
# forty assertions.
set -euo pipefail

PORT="${MCP_AUTO_TEST_PORT:-8237}"
URL="http://127.0.0.1:${PORT}"
BIN="${MCP_AUTO_TEST_BIN:-./open-crank-mcp}"
REPORT_DIR="${MCP_AUTO_TEST_REPORT_DIR:-build/reports/specmatic}"
SPECMATIC_IMAGE="${SPECMATIC_IMAGE:-specmatic/specmatic:latest}"

# Tools this cannot reach, and why. Both groups are structural rather than an oversight.
#
# Needs a running Simulator: this server keeps its state in the connection - which
# Simulator is running, its data directory, its bundle ID - so a fresh session starts with
# nothing running. Auto-test calls each tool once, in isolation, with generated arguments,
# and cannot call launch_simulator with a real .pdx first, so nothing downstream of a
# launch can answer with anything but "simulator not running". Those tools are covered
# against a real Simulator by internal/contracttest instead.
SKIP_NEEDS_SIMULATOR="launch_simulator,stop_simulator,restart_simulator,press_button,hold_button,release_button,reset_input,set_crank,get_screenshot,get_game_state,list_entities,get_logs,get_game_logs,read_save_data,write_save_data"
# Needs a real SDK toolchain: build_game shells out to cmake/pdc, which a bare checkout
# has no reason to have. make sdk-contract-check covers it where a toolchain exists.
SKIP_NEEDS_TOOLCHAIN="build_game"
# get_status is skipped for a reason worth writing down, because it looks like the one
# tool that should work here and it half does. It takes no arguments, so resiliency
# testing has nothing to mutate, and its negative case - "send something invalid, expect
# an error" - reduces to sending {} and expecting a failure. get_status answers that
# honestly ("running": false, which is its whole job) and is scored as a failure for it.
# Not a defect, and not fixable from this side: a zero-argument tool cannot be sent an
# invalid argument. Its real coverage is internal/mcpcontract, which asserts exactly that
# it succeeds with nothing running.
SKIP_UNTESTABLE="get_status"

# The skip list is a denylist rather than an allowlist on purpose. A new tool joins the
# run automatically and shows up as a failure if it needs skipping, which is the loud
# direction. An allowlist would silently omit it and this would stay green having tested
# less than it did yesterday - the trap the paths-filter guards in
# .github/workflows/ci.yml exist for.
SKIP_TOOLS="${SKIP_NEEDS_SIMULATOR},${SKIP_NEEDS_TOOLCHAIN},${SKIP_UNTESTABLE}"

if [ ! -x "$BIN" ]; then
  echo "mcp-auto-test: $BIN is not there or not executable; run \`make go-build\` first" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "mcp-auto-test: Docker is not running, and specmatic runs in a container." >&2
  exit 1
fi

mkdir -p "$REPORT_DIR"
# Absolute, because it becomes a Docker volume source and a relative one would be
# resolved by the daemon rather than by this shell.
REPORT_DIR_ABS="$(cd "$REPORT_DIR" && pwd)"
LOG="$REPORT_DIR_ABS/mcp_auto_test.log"

# setup and teardown are the two tools that can be driven for real here, and they need a
# game to work on. Without one, auto-test generates a random string for source_dir and
# only ever exercises the not-a-project error path.
#
# A disposable copy, never the committed fixtures themselves: setup writes the harness
# into a source tree and patches its main.lua/CMakeLists.txt, so pointing this at
# lua/test-fixture would leave the repo dirty and the next run would test an
# already-patched project instead of a clean one.
#
# And a *hybrid* project - the C fixture plus the Lua fixture's main.lua - rather than
# either one alone. Resiliency testing walks every value of an enum, so declaring
# language as one now means auto-test calls setup with lua, c and hybrid in turn and
# expects all three to work. Only a project that is both can satisfy that: against the
# Lua fixture alone, setup(language: "c") correctly fails with "patching CMakeLists.txt:
# no such file", which is the server being right and the generator being unable to know
# it. Measured, not assumed - that failure is what sent this looking for a better
# fixture.
WORKDIR="$(mktemp -d)"
FIXTURE="$WORKDIR/game"
mkdir -p "$FIXTURE"
cp -R c-harness/test/fixture-game/. "$FIXTURE/"
cp lua/test-fixture/Source/main.lua "$FIXTURE/Source/main.lua"

# A Specmatic dictionary pins chosen fields to real values instead of generated ones.
# Keyed by tool name, then by field: a flat { "source_dir": ... } is silently ignored,
# which is worth recording because it looks like it works and changes nothing.
DICT="$WORKDIR/dictionary.json"
cat > "$DICT" <<EOF
{
  "setup":    { "source_dir": "$FIXTURE" },
  "teardown": { "source_dir": "$FIXTURE" }
}
EOF

"$BIN" -http "127.0.0.1:${PORT}" &
SERVER_PID=$!
# shellcheck disable=SC2317  # reached via the trap, not fallthrough
cleanup() {
  kill "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

# Wait for the bind rather than sleeping a guessed amount. bash's /dev/tcp keeps this
# dependency-free; the alternative is assuming curl on every machine that runs it.
for _ in $(seq 1 50); do
  if (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; then
    exec 3<&- 2>/dev/null || true
    break
  fi
  sleep 0.1
done
if ! kill -0 "$SERVER_PID" 2>/dev/null; then
  echo "mcp-auto-test: the server exited during startup" >&2
  exit 1
fi

echo "mcp-auto-test: driving $URL"
echo "mcp-auto-test: setup/teardown run against a scratch hybrid C+Lua fixture at $FIXTURE"
echo "mcp-auto-test: skipped, needs a running Simulator: ${SKIP_NEEDS_SIMULATOR}"
echo "mcp-auto-test: skipped, needs cmake/pdc:            ${SKIP_NEEDS_TOOLCHAIN}"
echo "mcp-auto-test: skipped, no argument to mutate:       ${SKIP_UNTESTABLE}"

# --network host so the container reaches a server bound to the host's loopback. The
# server refuses to bind anything but loopback, so publishing a port is not an option and
# is not wanted: nothing off this machine should be able to reach it.
set +e
docker run --rm --network host \
  -v "${REPORT_DIR_ABS}:/usr/src/app/build/reports/specmatic" \
  -v "${DICT}:/dictionary.json:ro" \
  "$SPECMATIC_IMAGE" mcp test \
  --url "$URL" \
  --transport-kind=STREAMABLE_HTTP \
  --dictionary-file=/dictionary.json \
  --enable-resiliency-tests \
  --skip-tools="${SKIP_TOOLS}" \
  2>&1 | tee "$LOG"
docker_status="${PIPESTATUS[0]}"
set -e

if [ "$docker_status" -ne 0 ]; then
  echo "mcp-auto-test: the specmatic container itself failed (exit $docker_status)" >&2
  exit 1
fi

# The exit code is not the verdict, and that is the trap this check exists for: measured,
# `specmatic mcp test` exits 0 with 17 of 19 tools failing. A target that trusted it would
# be permanently green while proving nothing - the same shape as a paths-filter that
# matches no files.
#
# So assert on the summary it prints. Matching "Failed: 0" rather than counting failures
# means an output-format change fails this check instead of quietly satisfying it, which
# is the direction to be wrong in.
if ! grep -q '^Failed: 0$' "$LOG"; then
  echo >&2
  echo "mcp-auto-test: FAILED - specmatic reported failures, or its report format changed." >&2
  echo "Full log:    $LOG" >&2
  echo "JSON report: $REPORT_DIR_ABS/mcp/mcp_test_report.json" >&2
  exit 1
fi

echo "mcp-auto-test: ok (report in $REPORT_DIR_ABS/mcp/)"
