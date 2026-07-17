SHELL := bash

WEB_EMBED := backend/internal/web/dist
RUNNER_EMBED := backend/internal/runnerdist/bin
# OS/arch pairs offered as dashboard downloads. Windows is amd64; macOS ships
# both Apple Silicon (arm64) and Intel (amd64).
RUNNER_TARGETS := windows/amd64 linux/amd64 darwin/amd64 darwin/arm64

.PHONY: all dev test test-backend test-frontend build build-backend build-frontend embed-frontend build-runners build-agent lint package update clean

all: test build

dev:
	bash scripts/dev/start.sh

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test -- --run

# The frontend is embedded into the Go binary (single-artifact deploy), so the
# backend build depends on the built + staged frontend assets.
build: build-backend

build-frontend:
	# Use a clean reproducible install when a lockfile exists (falling back to
	# npm install if it is out of sync), otherwise install fresh. This keeps a
	# fresh clone building without the noisy "npm ci needs a lockfile" error.
	cd frontend && { [ -f package-lock.json ] && npm ci || npm install; } && npm run build

# Stage the built SPA into the embed directory consumed by //go:embed.
embed-frontend: build-frontend
	find $(WEB_EMBED) -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true
	cp -R frontend/dist/. $(WEB_EMBED)/

# Cross-compile the desktop runner for each target into the embed directory, so
# the API can serve them as dashboard downloads. Named opencuttles-runner-<os>-
# <arch>[.exe] for runnerdist to parse. Runs before build-backend so `go build`
# of the API embeds them.
build-runners:
	find $(RUNNER_EMBED) -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true
	@set -e; for target in $(RUNNER_TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		out=opencuttles-runner-$$os-$$arch; \
		[ "$$os" = "windows" ] && out=$$out.exe || true; \
		echo "  runner -> $$out"; \
		( cd runner && GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../$(RUNNER_EMBED)/$$out . ); \
	done

build-backend: embed-frontend build-runners
	mkdir -p dist
	cd backend && go build -trimpath -ldflags="-s -w" -o ../dist/opencuttles-api ./cmd/opencuttles-api

# Build the Flue agent sidecar bundle (agent/dist/server.mjs) that the
# opencuttles-agent service runs. Kept separate from `build`/`package` because it
# needs Node >= 22.18 and runs from the source checkout (not the packaged API
# artifact). `update.sh` invokes this so a redeploy can't silently keep a stale
# sidecar; run it by hand after an agent change with:
#   make build-agent && sudo systemctl restart opencuttles-agent
build-agent:
	cd agent && { [ -f package-lock.json ] && npm ci || npm install; } && npx flue build --target node

lint:
	cd backend && go test ./...
	cd frontend && npm run lint
	bash -n scripts/ubuntu/*.sh

package: build
	rm -rf dist/package
	mkdir -p dist/package/opt/opencuttles/bin
	cp dist/opencuttles-api dist/package/opt/opencuttles/bin/
	cp -R deploy dist/package/
	cp -R scripts dist/package/
	cp -R docs dist/package/

update:
	bash scripts/ubuntu/update.sh

clean:
	rm -rf dist frontend/dist
	find $(WEB_EMBED) -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true
	find $(RUNNER_EMBED) -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true
