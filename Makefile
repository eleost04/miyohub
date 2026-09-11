.PHONY: build frontend test check serve

frontend:
	cd web && npm ci && npm run build

build: frontend
	go build -trimpath -o bin/miyohub ./cmd/miyohub

test:
	go test -race ./...
	node --test scripts/actions-state.test.mjs

check: test
	go vet ./...
	node scripts/check-version.mjs
	cd web && npm run build

serve:
	go run ./cmd/miyohub serve
