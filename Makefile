PLAYDATE_SDK_VERSION ?= 3.1.1

.PHONY: build up up-visual up-visual-wsl up-vnc up-shared shared-load shared-watch check-game-dir down shell smoke-check test-c-harness sdk-contract-check test-shared-unit shared-check test-shared-types test-shared-browser go-build go-build-cross go-test mutation-test test hooks sdk-path smoke-check-native sdk-contract-check-native

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
# so an MCP client can attach to it separately afterward. See the README's
# "Shared, watchable session" section.
#
# GAME_DIR is checked here rather than in docker-compose.yml. A compose-level
# `${GAME_DIR:?}` guard would also fire on `make down`, which parses the shared
# profile too, so a missing var would block cleanup.
# Builds its own service rather than depending on `build`. Plain
# `docker compose build` skips every service that declares a profile, so
# `build` only ever rebuilds `simulator`. Depending on it would leave a
# stale shared-profile image in place after any edit to the Dockerfile or
# run-vnc.sh.
up-shared: check-game-dir
	PLAYDATE_SDK_VERSION=$(PLAYDATE_SDK_VERSION) docker compose --profile shared build simulator-shared
	docker compose --profile shared up -d simulator-shared

check-game-dir:
	@test -n "$(GAME_DIR)" || { \
		echo "GAME_DIR is not set."; \
		echo "Run: GAME_DIR=/absolute/path/to/your-game make up-shared"; \
		exit 1; }
	@case "$(GAME_DIR)" in /*) ;; *) \
		echo "GAME_DIR must be an absolute path, got: $(GAME_DIR)"; \
		echo "A relative path resolves against docker-compose.yml, not your shell."; \
		exit 1;; esac

down:
	docker compose --profile visual --profile wsl --profile vnc --profile shared down

shell: build
	docker compose run --rm simulator /bin/bash

smoke-check: build
	docker compose run --rm simulator go run ./cmd/smoke-check

sdk-contract-check: build
	docker compose run --rm simulator env OPEN_CRANK_SDK_CONTRACT=1 go test ./internal/contracttest/... -v

# Recreates the container against the current GAME_DIR, then builds and launches
# the game by driving the MCP server exactly as a client would. This is the one
# command to reach for: GAME_DIR is fixed when the container starts, so a
# container left over from another game would otherwise keep serving that game.
#
# Pass -keep-container to reuse a running one, which is faster and keeps the
# volume slider and the VNC connection, at the cost of keeping whatever GAME_DIR
# it started with:
#   go run ./cmd/shared-load -keep-container
shared-load: check-game-dir
	go run ./cmd/shared-load -compose-file $(CURDIR)/docker-compose.yml

# Rebuilds and reloads on save, using the Simulator's own Ctrl-R, which
# re-reads the .pdx from disk in the same process - so the display, the
# container and your browser tab all stay put. The game restarts each time:
# Reset is the only reload the SDK has.
#
# Piped in rather than run from the image, so editing the script takes effect
# immediately instead of needing a rebuild.
shared-watch:
	docker compose exec -T simulator-shared bash -s < scripts/shared-watch.sh

# Unit tests for the shared helpers: the volume-slider parser and the window
# geometry formula. Pure awk and bash, so no container and no display.
test-shared-unit:
	bash scripts/run-shared-unit-tests.sh

# Boots the shared container against the in-repo Lua fixture and asserts the
# workspace invariants: pages served, window manager configuration, where the
# volume slider was found, and that clicking it works.
shared-check:
	bash scripts/shared-check.sh

# The image tag must match the @playwright/test version in
# tests/browser/package.json: the image is what carries the browsers, and the
# package is what drives them. Node is the runtime because Playwright's test
# runner requires it; Bun and Deno can drive playwright-core but not this
# runner. It all lives in the container, so the host needs neither.
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.62.0-noble
# Must match the default in scripts/shared-check.sh.
CHECK_VNC_PORT ?= 6180
BROWSER_TESTS = docker run --rm --network host \
	-v "$(CURDIR)/tests/browser:/work" -w /work $(PLAYWRIGHT_IMAGE) \
	sh -c "npm install --no-audit --no-fund --loglevel=error >/dev/null &&

# Typechecks the browser tests with tsgo, the Go port of tsc. Playwright's
# runner transpiles TypeScript itself, so this is a check rather than a build
# step, and nothing is emitted.
test-shared-types:
	$(BROWSER_TESTS) npx tsgo --noEmit"

# Browser behaviour for the VNC pages: the scaling redirect, the hidden player,
# and the mapping from a click on the Playdate's slider to the player's volume.
# Host networking is how the container reaches port 6080.
# SHARED_URL points the browser tests at the isolated container shared-check starts,
# not at whatever is on the default port. The teardown names the same project, so
# a shared container of your own survives a test run untouched.
test-shared-browser: test-shared-types
	bash scripts/shared-check.sh --keep
	$(BROWSER_TESTS) SHARED_URL=http://localhost:$(CHECK_VNC_PORT) npx playwright test"
	COMPOSE_PROJECT_NAME=open-crank-mcp-check docker compose --profile shared down

# Emits the binary, rather than just proving it compiles. An MCP client running
# the server natively is configured with a path to this file, so a target that
# writes nothing would leave those instructions pointing at nothing.
#
# ./cmd/open-crank-mcp, not ./... - the latter builds every package including
# the two other commands, and discards all of it.
go-build:
	go build -o open-crank-mcp ./cmd/open-crank-mcp

go-test:
	go test ./...

# Builds for every platform the server is meant to run on, plus vet, without
# needing any of them present. This is the only cross-platform claim provable
# from one machine, so it is the gate that keeps native mode's per-OS code
# honest between here and a real macOS install.
#
# windows is in this list even though Windows-native is unsupported (see
# docs/ROADMAP.md). Keeping it compiling is cheap, and it is what makes
# promoting Windows later additive rather than a rewrite. Note the limit of
# what this proves: it catches a construct that does not exist on a platform,
# not one that exists and behaves differently. internal/simulator's Exited()
# was exactly the second kind.
CROSS_PLATFORMS = linux darwin windows

go-build-cross:
	@for os in $(CROSS_PLATFORMS); do \
		printf '%-8s ' "$$os"; \
		GOOS=$$os go build ./... || exit 1; \
		GOOS=$$os go vet ./... || exit 1; \
		echo 'build + vet ok'; \
	done

# Mutation testing: gremlins changes the code in small ways and checks the test
# suite notices. Catches tests that execute a line without asserting anything
# about it, which coverage alone reports as covered.
#
# Run through `go run` with a pinned version rather than installed, so there is
# nothing to set up on the host and no way for a local run to drift from CI.
# Keep this version matching the one in .github/workflows/ci.yml. Thresholds and
# the exclude list live in .gremlins.yaml.
GREMLINS_VERSION ?= v0.6.0

mutation-test:
	go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) unleash

# Points git at the tracked hooks in .githooks. Not a copy into .git/hooks: a
# copy goes stale the moment the tracked hook changes, and nothing tells you.
# core.hooksPath always runs what is in the repo.
#
# Opt-in rather than automatic, because git has no way to enable a hook on
# clone, and a repo that silently starts running a two-minute suite on every
# commit would be a surprise worth avoiding.
hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit hook enabled. Bypass a single commit with --no-verify."
	@echo "disable with: git config --unset core.hooksPath"

# Prints the SDK internal/sdk would resolve, and which of the three sources
# found it. The first thing to reach for when detection picks the wrong SDK, or
# picks none: it turns an invisible decision into one line.
sdk-path:
	@go run ./cmd/sdk-path

# The native counterparts of smoke-check and sdk-contract-check: same subject,
# no container. `-native` is a suffix rather than a prefix so `make smoke-check`
# keeps meaning what it always did, and so tab completion groups by subject.
#
# Both need an SDK on this machine. OPEN_CRANK_SDK_CONTRACT is what tells the
# contract tests they are wanted; without it they skip, which is what keeps them
# from failing on a host that happens to have PLAYDATE_SDK_PATH set but no
# display.
smoke-check-native:
	go run ./cmd/smoke-check

sdk-contract-check-native:
	OPEN_CRANK_SDK_CONTRACT=1 go test ./internal/contracttest/... -v

# Everything, ordered so it fails as fast as it can. The three host-only suites
# run first: a broken parser or a Go typo then fails in seconds instead of
# after several minutes of container boots.
#
# test-shared-browser stands in for both test-shared-types and shared-check
# rather than duplicating them. It declares the typecheck as a prerequisite,
# and it runs shared-check.sh --keep, where --keep only skips the teardown -
# every assertion in that script still runs and still decides the exit status.
# Listing shared-check here too would boot a second container to repeat two
# dozen checks that had just passed.
#
# Sequential $(MAKE) calls rather than prerequisites, because prerequisites are
# fair game for `make -j` to run concurrently and these suites cannot overlap:
# they each want Docker, and shared-check and test-shared-browser share one
# fixed port and one compose project name.
test:
	$(MAKE) go-test
	$(MAKE) test-shared-unit
	$(MAKE) mutation-test
	$(MAKE) test-c-harness
	$(MAKE) smoke-check
	$(MAKE) sdk-contract-check
	$(MAKE) test-shared-browser
