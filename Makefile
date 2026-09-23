PLAYDATE_SDK_VERSION ?= 3.1.2

.PHONY: build up up-visual up-visual-wsl up-vnc up-shared shared-load shared-watch check-game-dir down shell smoke-check test-c-harness sdk-contract-check test-shared-unit plugin-check plugin-upstream-check sdk-pin-check shared-check test-shared-types test-shared-browser go-build go-build-cross go-test mcp-schema mcp-schema-check mcp-auto-test check-doc-links no-regex mutation-test mutation-test-scan mutation-test-rest mutation-test-diff test hooks sdk-path smoke-check-native sdk-contract-check-native

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

# Detached so an MCP client can attach later; see guides/shared-session.md.
# GAME_DIR is checked here, not in compose, where the guard would fire on `make
# down` too. Builds its own service: `docker compose build` skips profiles.
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

# Recreates the container for the current GAME_DIR, then builds and launches by
# driving the MCP server as a client would. GAME_DIR is fixed at container start,
# so a leftover container keeps serving the old game. -keep-container reuses one.
shared-load: check-game-dir
	go run ./cmd/shared-load -compose-file $(CURDIR)/docker-compose.yml

# Rebuild and reload on save via the Simulator's own Ctrl-R, so the display,
# container and browser tab survive. The game still restarts - Reset is the only
# reload the SDK has. Piped in so editing the script needs no rebuild.
shared-watch:
	docker compose exec -T simulator-shared bash -s < scripts/shared-watch.sh

# Unit tests for the shared helpers: the volume-slider parser and the window
# geometry formula. Pure awk and bash, so no container and no display.
test-shared-unit:
	bash scripts/run-shared-unit-tests.sh

# The plugin's manifests: versions agree, the portable pair satisfies the 1.0.0
# schemas, plus the rules a schema cannot express. Offline - schemas are vendored
# at plugin/schemas/1.0.0. `go test ./...` runs it; this is for discoverability.
plugin-check:
	go test ./internal/plugincontract

# Notices upstream moving: the vendored schemas being edited, or a newer spec
# version being published. Weekly, not per-PR - see the script for why.
plugin-upstream-check:
	bash scripts/plugin-upstream-check.sh

# Notices when PLAYDATE_SDK_VERSION falls behind what Panic ships. Weekly, not
# per-PR: see the header of the script.
sdk-pin-check:
	bash scripts/sdk-pin-check.sh

# Boots the shared container against the in-repo Lua fixture and asserts the
# workspace invariants: pages, window manager, slider location, clicking it.
shared-check:
	bash scripts/shared-check.sh

# Tag must match @playwright/test in tests/browser/package.json: the image
# carries the browsers, the package drives them. Node because the runner needs
# it - Bun and Deno drive playwright-core but not this runner.
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v1.62.0-noble
# Must match the default in scripts/shared-check.sh.
CHECK_VNC_PORT ?= 6180
BROWSER_TESTS = docker run --rm --network host \
	-v "$(CURDIR)/tests/browser:/work" -w /work $(PLAYWRIGHT_IMAGE) \
	sh -c "npm install --no-audit --no-fund --loglevel=error >/dev/null &&

# Typechecks the browser tests with tsgo. Playwright transpiles TypeScript
# itself, so this is a check rather than a build step.
test-shared-types:
	$(BROWSER_TESTS) npx tsgo --noEmit"

# Browser behaviour for the VNC pages. Host networking reaches port 6080, and
# SHARED_URL points at the isolated container shared-check starts, so a shared
# container of your own survives the run.
test-shared-browser: test-shared-types
	bash scripts/shared-check.sh --keep
	$(BROWSER_TESTS) SHARED_URL=http://localhost:$(CHECK_VNC_PORT) npx playwright test"
	COMPOSE_PROJECT_NAME=open-crank-mcp-check docker compose --profile shared down

# Emits the binary, not just a compile check: a native client is configured with
# a path to this file. ./cmd/open-crank-mcp, not ./..., which discards its output.
go-build:
	go build -o open-crank-mcp ./cmd/open-crank-mcp

go-test:
	go test ./...

# Regenerate after changing a tool's name, description or types, and commit it:
# the diff in docs/mcp-schema.json is the point. The generator is the test with
# -update, so nothing can produce a file disagreeing with what is served.
mcp-schema:
	go test ./internal/mcpcontract -update

# Fails if docs/mcp-schema.json drifts from what the server serves, or if a schema
# is one a client would reject. CI verifies and never regenerates: the point is a
# schema change appearing in a pull request where a person sees it.
mcp-schema-check:
	go test ./internal/mcpcontract

# Specmatic generates inputs from each tool's declared schema and checks the
# responses against it. No spec file - it reads tools/list off the running server
# - so nothing can drift. See the script for what it does not cover.
mcp-auto-test: go-build
	bash scripts/mcp-auto-test.sh

# Every relative Markdown link and heading anchor resolves. It cannot read prose,
# so it catches a hand-written path or a mis-slugged anchor - GitHub's anchor
# rules are not guessable. No network, so a dead external link never blocks you.
check-doc-links:
	bash scripts/check-doc-links.sh

# Guards the no-regex rule (https://regexlicensing.org/). No allowlist,
# deliberately - an exemption list only ever grows. internal/scan holds the
# byte-level scanning that replaced patterns; command-line grep is fine.
no-regex:
	@bad=$$(git ls-files --cached --others --exclude-standard '*.go' \
		| xargs -r grep -lE '"regexp(/[a-z]+)?"' 2>/dev/null || true); \
	if [ -n "$$bad" ]; then \
		echo "regexp is imported by:"; \
		echo "$$bad" | sed 's/^/  /'; \
		echo "there is no allowlist - use internal/scan"; \
		exit 1; \
	fi; \
	echo "no-regex: ok"

# Builds and vets every supported platform from one machine - the only
# cross-platform claim provable here. windows is included though unsupported, to
# keep it compiling. Catches a missing construct, not one that behaves differently.
CROSS_PLATFORMS = linux darwin windows

go-build-cross:
	@for os in $(CROSS_PLATFORMS); do \
		printf '%-8s ' "$$os"; \
		GOOS=$$os go build ./... || exit 1; \
		GOOS=$$os go vet ./... || exit 1; \
		echo 'build + vet ok'; \
	done

# gremlins changes the code and checks the tests notice, catching lines that run
# without being asserted on. Pinned via `go run` so a local run cannot drift from
# CI - keep in step with ci.yml. Thresholds and excludes in .gremlins.yaml.
GREMLINS_VERSION ?= v0.6.0

mutation-test:
	go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) unleash

# Split in two for CI, not for speed: gremlins sizes its per-mutant timeout from
# the scope's baseline, so internal/scan's hanging byte-loop mutants cost ~60s
# each whole-module and ~3s scoped. mutation-test-rest derives its config from
# .gremlins.yaml so the excludes cannot drift.
mutation-test-scan:
	go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) unleash ./internal/scan

mutation-test-rest:
	@tmp=$$(mktemp); \
	sed 's|^  exclude-files:|  exclude-files:\n    - "^internal/scan/"|' .gremlins.yaml > $$tmp; \
	go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) unleash . --config $$tmp; \
	status=$$?; rm -f $$tmp; exit $$status

# Only the lines changed against a ref, for the pre-commit hook. Not a
# replacement for the full run, which sees tests weakened for untouched code.
# An all-skipped run is a pass: gremlins reports "no mutants" as 0% efficacy.
MUTATION_DIFF_REF ?= HEAD

mutation-test-diff:
	@out=$$(go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash --diff $(MUTATION_DIFF_REF) 2>&1); rc=$$?; \
	echo "$$out" | tail -4; \
	if echo "$$out" | grep -q "Killed: 0, Lived: 0, Not covered: 0"; then \
		echo "  no mutable changes in the diff, nothing to test"; exit 0; \
	fi; \
	exit $$rc

# Points core.hooksPath at .githooks rather than copying into .git/hooks, where a
# copy goes stale silently. Opt-in: git cannot enable a hook on clone, and a
# surprise two-minute suite on every commit is not a good greeting.
hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit hook enabled. Bypass a single commit with --no-verify."
	@echo "disable with: git config --unset core.hooksPath"

# Prints the SDK internal/sdk resolves and which source found it. The first thing
# to reach for when detection picks the wrong SDK, or none.
sdk-path:
	@go run ./cmd/sdk-path

# Native counterparts of smoke-check and sdk-contract-check: same subject, no
# container. Both need a host SDK. OPEN_CRANK_SDK_CONTRACT opts the contract
# tests in; without it they skip, so a host with no display does not fail.
smoke-check-native:
	go run ./cmd/smoke-check

sdk-contract-check-native:
	OPEN_CRANK_SDK_CONTRACT=1 go test ./internal/contracttest/... -v

# Everything, host-only suites first so a Go typo fails in seconds rather than
# after several container boots. test-shared-browser already runs both
# test-shared-types and shared-check, so neither is listed again. Sequential
# $(MAKE) calls, not prerequisites, because `make -j` would overlap suites that
# share Docker, a port and a compose project name.
test:
	$(MAKE) no-regex
	$(MAKE) check-doc-links
	$(MAKE) go-test
	$(MAKE) test-shared-unit
	$(MAKE) mutation-test
	$(MAKE) test-c-harness
	$(MAKE) smoke-check
	$(MAKE) sdk-contract-check
	$(MAKE) test-shared-browser
