# syntax=docker/dockerfile:1

# --- Builder ----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# TARGETOS/TARGETARCH are supplied by BuildKit and describe the platform the
# image is FOR, not the machine doing the building. Deriving GOARCH from
# TARGETARCH is what makes `docker build --platform linux/amd64` on an arm64
# Mac (and the reverse) produce a binary that actually runs.
#
# This previously set GOOS=linux and left GOARCH unset, so it silently inherited
# the builder's architecture. Building on Apple Silicon yielded a linux/arm64
# binary that died with "exec format error" on an x86_64 host -- and because both
# base images are multi-arch, nothing failed until runtime.
ARG TARGETOS
ARG TARGETARCH

# CGO disabled -> static binary that runs on a minimal/distroless base.
# -trimpath keeps local build paths out of the binary.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -o /server ./cmd/server

# Migrations are go:embed'ed (internal/db/migrate.go), so the binary is fully
# self-contained -- there is deliberately nothing else to COPY.

# --- Runtime ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /

COPY --from=builder /server /server

EXPOSE 8080

# distroless/static-debian12:nonroot already runs as a non-root "nonroot" user.
USER nonroot:nonroot

ENTRYPOINT ["/server"]
