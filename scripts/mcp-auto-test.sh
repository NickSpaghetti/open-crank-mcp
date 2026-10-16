#!/usr/bin/env bash
# Runs Specmatic's MCP auto-test: it reads tools/list off the running server and
# validates each tool against its own declared schemas. Specmatic only speaks
# Streamable HTTP, hence the server's -http flag.
set -euo pipefail

PORT="${MCP_AUTO_TEST_PORT:-8237}"
URL="http://127.0.0.1:${PORT}"
BIN="${MCP_AUTO_TEST_BIN:-./open-crank-mcp}"
REPORT_DIR="${MCP_AUTO_TEST_REPORT_DIR:-build/reports/specmatic}"
SPECMATIC_IMAGE="${SPECMATIC_IMAGE:-specmatic/specmatic:latest}"

# Auto-test calls each tool in isolation and can't launch a real .pdx first, so these
# only ever see "simulator not running". internal/contracttest covers them.
SKIP_NEEDS_SIMULATOR="launch_simulator,stop_simulator,restart_simulator,press_button,hold_button,release_button,reset_input,set_crank,get_screenshot,get_game_state,list_entities,get_logs,get_game_logs,read_save_data,write_save_data"
# build_game needs cmake/pdc; sdk-contract-check covers it.
SKIP_NEEDS_TOOLCHAIN="build_game"
# get_status takes no arguments, so Specmatic's negative case sends {} and scores its
# correct answer as a failure. internal/mcpcontract covers it.
SKIP_UNTESTABLE="get_status"

# A denylist, so a new tool joins the run automatically and fails loudly if it needs skipping.
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
# Absolute, because Docker volume sources must be.
REPORT_DIR_ABS="$(cd "$REPORT_DIR" && pwd)"
LOG="$REPORT_DIR_ABS/mcp_auto_test.log"

# A disposable game for setup/teardown, since setup patches the tree it's given. Hybrid
# C+Lua, because Specmatic calls setup with every language enum value and expects each to pass.
WORKDIR="$(mktemp -d)"
FIXTURE="$WORKDIR/game"
mkdir -p "$FIXTURE"
cp -R c-harness/test/fixture-game/. "$FIXTURE/"
cp lua/test-fixture/Source/main.lua "$FIXTURE/Source/main.lua"

# Pins fields to real values. Keyed by tool then field; Specmatic silently ignores a flat map.
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

# Wait for the bind; bash's /dev/tcp avoids needing curl.
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

# --network host to reach the server, which only binds loopback.
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

# `specmatic mcp test` exits 0 even when tools fail, so assert on its summary. Matching
# "Failed: 0" means an output-format change fails this rather than passing it.
if ! grep -q '^Failed: 0$' "$LOG"; then
  echo >&2
  echo "mcp-auto-test: FAILED - specmatic reported failures, or its report format changed." >&2
  echo "Full log:    $LOG" >&2
  echo "JSON report: $REPORT_DIR_ABS/mcp/mcp_test_report.json" >&2
  exit 1
fi

echo "mcp-auto-test: ok (report in $REPORT_DIR_ABS/mcp/)"
