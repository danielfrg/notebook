set dotenv-load := true

default:
    @just --list

# Install the frontend dependencies
install:
    pnpm --dir frontend install --frozen-lockfile

# Go API (serves the frontend embedded by the last `just build`)
api:
    go run ./cmd/notebook

# Vite dev server with HMR (proxies the API)
frontend:
    pnpm --dir frontend dev

# Format the Go and frontend code
fmt:
    gofmt -w cmd internal
    pnpm --dir frontend fmt

# Everything: format, lint, vet, tests and type checks
check:
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    go test -race ./... -count=1
    pnpm --dir frontend fmt:check
    pnpm --dir frontend lint
    pnpm --dir frontend test
    pnpm --dir frontend check

# Build the frontend and copy it into the Go embed folder
embed: install
    pnpm --dir frontend build
    test -s frontend/dist/index.html
    rm -rf internal/web/dist
    mkdir -p internal/web/dist
    touch internal/web/dist/.keep
    cp -R frontend/dist/. internal/web/dist/
    test -s internal/web/dist/index.html

# Build the single binary with the frontend embedded (no tests: see `check`)
build: embed
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o notebook ./cmd/notebook

# Build, then run the binary
run: build
    ./notebook

image:
    docker build -t notebook:local .

image-check: image
    bash scripts/container-smoke.sh notebook:local
