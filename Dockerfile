# syntax=docker/dockerfile:1

# --- Builder ----------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# CGO disabled -> static binary that runs on a minimal/distroless base.
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /server ./cmd/server

# --- Runtime ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /

COPY --from=builder /server /server

EXPOSE 8080

# distroless/static-debian12:nonroot already runs as a non-root "nonroot" user.
USER nonroot:nonroot

ENTRYPOINT ["/server"]
