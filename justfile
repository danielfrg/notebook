set dotenv-load := true

install:
    pnpm --dir frontend install --frozen-lockfile

api:
    go run ./cmd/notebook

frontend:
    pnpm --dir frontend dev

check:
    go test -race ./...
    go vet ./...
    pnpm --dir frontend test
    pnpm --dir frontend check

build: install
    pnpm --dir frontend test
    pnpm --dir frontend check
    pnpm --dir frontend build
    test -s frontend/dist/index.html
    rm -rf internal/web/dist
    mkdir -p internal/web/dist
    touch internal/web/dist/.keep
    cp -R frontend/dist/. internal/web/dist/
    test -s internal/web/dist/index.html
    go test ./...
    go vet ./...
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o notebook ./cmd/notebook

image:
    docker build -t notebook:local .

image-check: image
    bash scripts/container-smoke.sh notebook:local
