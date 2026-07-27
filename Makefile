PLAYDATE_SDK_VERSION ?= 3.1.1

.PHONY: build up up-visual up-visual-wsl up-vnc down shell smoke-check test-c-harness sdk-contract-check go-build go-test

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

down:
	docker compose --profile visual --profile wsl --profile vnc down

shell: build
	docker compose run --rm simulator /bin/bash

smoke-check: build
	docker compose run --rm simulator go run ./cmd/smoke-check

sdk-contract-check: build
	docker compose run --rm simulator go test ./internal/contracttest/... -v

go-build:
	go build ./...

go-test:
	go test ./...
