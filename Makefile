PLAYDATE_SDK_VERSION ?= 3.1.1

.PHONY: build up up-visual up-visual-wsl up-vnc up-byos byos-load byos-watch check-game-dir down shell smoke-check test-c-harness sdk-contract-check test-byos-unit byos-check test-byos-types test-byos-browser go-build go-test

build:
	PLAYDATE_SDK_VERSION=$(PLAYDATE_SDK_VERSION) docker compose build

test-c-harness:
	docker build --target c-harness-test --build-arg PLAYDATE_SDK_VERSION=$(PLAYDATE_SDK_VERSION) -t open-crank-mcp-c-harness-test .
	docker run --rm open-crank-mcp-c-harness-test bash scripts/run-c-harness-tests.sh

up: build
	docker compose up simulator

up-visual: build
	bash scripts/ensure-xauth.sh
	docker compose --profile visual up simulator-visual

up-visual-wsl: build
	docker compose --profile wsl up simulator-visual-wsl

up-vnc: build
	docker compose --profile vnc up simulator-vnc

# Detached, unlike every other up* target. Stays running in the background
# so an MCP client can attach to it separately afterward. See README's
# "Bring your own simulator" section.
#
# GAME_DIR is checked here rather than in docker-compose.yml. A compose-level
# `${GAME_DIR:?}` guard would also fire on `make down`, which parses the byos
# profile too, so a missing var would block cleanup.
# Builds its own service rather than depending on `build`. Plain
# `docker compose build` skips every service that declares a profile, so
# `build` only ever rebuilds `simulator`. Depending on it would leave a
# stale byos image in place after any edit to the Dockerfile or run-vnc.sh.
up-byos: check-game-dir
	PLAYDATE_SDK_VERSION=$(PLAYDATE_SDK_VERSION) docker compose --profile byos build simulator-byos
	docker compose --profile byos up -d simulator-byos

check-game-dir:
	@test -n "$(GAME_DIR)" || { \
		echo "GAME_DIR is not set."; \
		echo "Run: GAME_DIR=/absolute/path/to/your-game make up-byos"; \
		exit 1; }
	@case "$(GAME_DIR)" in /*) ;; *) \
		echo "GAME_DIR must be an absolute path, got: $(GAME_DIR)"; \
		echo "A relative path resolves against docker-compose.yml, not your shell."; \
		exit 1;; esac

down:
	docker compose --profile visual --profile wsl --profile vnc --profile byos down

shell: build
	docker compose run --rm simulator /bin/bash

smoke-check: build
	docker compose run --rm simulator go run ./cmd/smoke-check

sdk-contract-check: build
	docker compose run --rm simulator go test ./internal/contracttest/... -v

# Recreates the container against the current GAME_DIR, then builds and launches
# the game by driving the MCP server exactly as a client would. This is the one
# command to reach for: GAME_DIR is fixed when the container starts, so a
# container left over from another game would otherwise keep serving that game.
#
# Pass -keep-container to reuse a running one, which is faster and keeps the
# volume slider and the VNC connection, at the cost of keeping whatever GAME_DIR
# it started with:
#   go run ./cmd/byos-load -keep-container
byos-load: check-game-dir
	go run ./cmd/byos-load -compose-file $(CURDIR)/docker-compose.yml

# Rebuilds and reloads on save, using the Simulator's own Ctrl-R, which
# re-reads the .pdx from disk in the same process - so the display, the
# container and your browser tab all stay put. The game restarts each time:
# Reset is the only reload the SDK has.
#
# Piped in rather than run from the image, so editing the script takes effect
# immediately instead of needing a rebuild.
byos-watch:
	docker compose exec -T simulator-byos bash -s < scripts/byos-watch.sh

# Unit tests for the byos helpers: the volume-slider parser and the window
# geometry formula. Pure awk and bash, so no container and no display.
test-byos-unit:
	bash scripts/run-byos-unit-tests.sh

# Boots the byos container against the in-repo Lua fixture and asserts the
# workspace invariants: pages served, window manager configuration, where the
# volume slider was found, and that clicking it works.
byos-check:
	bash scripts/byos-check.sh

# The image tag must match the @playwright/test version in
# tests/browser/package.json: the image is what carries the browsers, and the
# package is what drives them. Node is the runtime because Playwright's test
# runner requires it; Bun and Deno can drive playwright-core but not this
# runner. It all lives in the container, so the host needs neither.
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.62.0-noble
# Must match the default in scripts/byos-check.sh.
CHECK_VNC_PORT ?= 6180
BROWSER_TESTS = docker run --rm --network host \
	-v "$(CURDIR)/tests/browser:/work" -w /work $(PLAYWRIGHT_IMAGE) \
	sh -c "npm install --no-audit --no-fund --loglevel=error >/dev/null &&

# Typechecks the browser tests with tsgo, the Go port of tsc. Playwright's
# runner transpiles TypeScript itself, so this is a check rather than a build
# step, and nothing is emitted.
test-byos-types:
	$(BROWSER_TESTS) npx tsgo --noEmit"

# Browser behaviour for the VNC pages: the scaling redirect, the hidden player,
# and the mapping from a click on the Playdate's slider to the player's volume.
# Host networking is how the container reaches port 6080.
# BYOS_URL points the browser tests at the isolated container byos-check starts,
# not at whatever is on the default port. The teardown names the same project, so
# a byos container of your own survives a test run untouched.
test-byos-browser: test-byos-types
	bash scripts/byos-check.sh --keep
	$(BROWSER_TESTS) BYOS_URL=http://localhost:$(CHECK_VNC_PORT) npx playwright test"
	COMPOSE_PROJECT_NAME=open-crank-mcp-check docker compose --profile byos down

go-build:
	go build ./...

go-test:
	go test ./...
