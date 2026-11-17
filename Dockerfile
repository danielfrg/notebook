FROM --platform=$BUILDPLATFORM node:26.10.0-bookworm-slim@sha256:3ffc19ea878019d9e9ae8971732ad4a03cda44f167107173174b60ed7c65bed3 AS frontend
WORKDIR /src
RUN npm install --global pnpm@10.28.0
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm test && pnpm check && pnpm build && test -s dist/index.html

FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=frontend /src/dist/ ./internal/web/dist/
RUN test -s internal/web/dist/index.html && go test ./... && go vet ./...
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/notebook ./cmd/notebook

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/notebook /notebook
ARG VCS_REF
LABEL org.opencontainers.image.source="https://github.com/danielfrg/notebook" \
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.licenses="Apache-2.0"
ENV NOTEBOOK_ADDR=:8080
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/notebook"]
