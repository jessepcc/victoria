# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a static production binary (no dev endpoints; CGO disabled).
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/victoria ./cmd/victoria

# ---- runtime stage ----
# Distroless static: no shell, non-root by default, minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/victoria /usr/local/bin/victoria

EXPOSE 8080
USER nonroot:nonroot

# VICTORIA_GATEWAY_INBOUND_TOKEN is required at startup. Provide it (and, for
# persistence, VICTORIA_DATABASE_URL) at runtime, e.g.:
#   docker run -e VICTORIA_GATEWAY_INBOUND_TOKEN=... -p 8080:8080 victoria
ENTRYPOINT ["/usr/local/bin/victoria"]
