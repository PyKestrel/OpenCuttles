SHELL := bash

.PHONY: all test test-backend test-frontend build build-backend build-frontend lint package clean

all: test build

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test -- --run

build: build-backend build-frontend

build-backend:
	mkdir -p dist
	cd backend && go build -trimpath -ldflags="-s -w" -o ../dist/opencuttles-api ./cmd/opencuttles-api

build-frontend:
	cd frontend && npm ci && npm run build

lint:
	cd backend && go test ./...
	cd frontend && npm run lint
	bash -n scripts/ubuntu/*.sh

package: build
	mkdir -p dist/package/opt/opencuttles/bin dist/package/opt/opencuttles/frontend
	cp dist/opencuttles-api dist/package/opt/opencuttles/bin/
	cp -R frontend/dist dist/package/opt/opencuttles/frontend/dist
	cp -R deploy dist/package/
	cp -R scripts dist/package/
	cp -R docs dist/package/

clean:
	rm -rf dist frontend/dist
