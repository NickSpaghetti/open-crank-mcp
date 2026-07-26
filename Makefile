PLAYDATE_SDK_VERSION ?= 3.1.1

.PHONY: build up up-visual up-visual-wsl up-vnc down smoke-check go-build go-test

build:
	PLAYDATE_SDK_VERSION=$(PLAYDATE_SDK_VERSION) docker compose build

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

smoke-check: build
	docker compose run --rm simulator bash /workspace/scripts/smoke-check.sh

go-build:
	go build ./...

go-test:
	go test ./...
