# Contributing to Victoria

Thanks for your interest in Victoria. This document covers how the codebase is
laid out, how to build and test it, and the conventions we hold contributions
to. It should be enough to get a first PR merged without surprises.

## Ground rules

- Be kind and assume good faith. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
- Discuss non-trivial changes in an issue before opening a large PR.
- Keep PRs focused. One logical change per PR is much easier to review.
- Every behavioural change ships with a test.

## Prerequisites

- **Go 1.26+** (the toolchain version is pinned in [`go.mod`](go.mod)).
- **PostgreSQL 15+** — only needed for the Postgres store and integration
  tests. The default in-memory store needs nothing.
- Optional tooling installed by `make tools`: `golangci-lint`, `govulncheck`.

## Quick start

```sh
# Build and run with the in-memory store (no external dependencies)
export VICTORIA_GATEWAY_INBOUND_TOKEN=dev-token   # required; authenticates /gateway/inbound
go run ./cmd/victoria
# → victoria listening on :8080
```

Common workflows are wrapped in the [`Makefile`](Makefile):

```sh
make build          # production binary (no dev endpoints)
make build-dev      # binary with -tags dev (mounts /admin/dev/* demo routes)
make test           # unit + e2e tests, whatsmeow disabled
make test-integration   # also runs the Postgres-backed tests (needs a database)
make lint           # gofmt check, go vet, golangci-lint
make vuln           # govulncheck
make check          # everything CI runs
```

## Architecture at a glance

Victoria is a layered Go application. Dependencies point **inward** — outer
layers depend on inner ones, never the reverse. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the full map.

| Package | Responsibility | May import |
|---|---|---|
| `internal/domain` | Pure types, errors, hashing, confidence scoring | (nothing internal) |
| `internal/app` | The engine: tenants, cases, corrections, candidates, skill versions, gateway parsing, MCP gates | `domain` |
| `internal/store/*` | Persistence (`memory`, `postgres`) implementing `app.Store` | `domain` |
| `internal/channel/*` | Inbound/outbound channel adapters (WhatsApp, Telegram) | `domain`, `channel` |
| `internal/httpapi` | chi HTTP transport, auth, JSON envelopes | `app`, `domain` |
| `cmd/victoria` | Process wiring and lifecycle | all of the above |

**Please respect these boundaries.** `internal/domain` must stay dependency-free,
and business logic belongs in `internal/app`, not in HTTP handlers.

### The dev/prod build-tag split

Demo-only and operator-debug endpoints (`/admin/dev/*`) are compiled in **only**
with `-tags dev`. In a production build they do not exist in the binary — this
is verified by `test/e2e/http_e2e_proddev_test.go`. When you add a route that
should never reach production, put it behind the `dev` tag using the
`*_dev.go` / `*_stub.go` pair pattern already in `internal/httpapi` and
`cmd/victoria`.

## Testing

```sh
go test ./...                                   # unit + e2e (whatsmeow auto-disabled in tests)
VICTORIA_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/victoria_test?sslmode=disable' \
  go test ./internal/store/postgres -count=1    # Postgres integration (self-migrates schema)
go build -tags dev ./... && go vet -tags dev ./...   # keep the dev build green too
```

Integration tests **skip** automatically when their backing service env var is
unset, so `go test ./...` is always safe to run with zero setup.

## Code style

- `gofmt` is enforced (CI fails on unformatted files). Run `make fmt`.
- Wrap errors with `%w` and add context: `fmt.Errorf("create tenant: %w", err)`.
- Prefer table-driven tests.
- Keep exported identifiers documented with a comment that starts with the name.

## Commit & PR conventions

- Use clear, imperative commit subjects (`Add Postgres outbound queue depth cap`).
- Reference the issue you're closing in the PR description.
- CI must be green: build (default **and** `dev`), `go vet`, `golangci-lint`,
  `gofmt`, and tests.

## Reporting security issues

Please do **not** open public issues for vulnerabilities. See
[SECURITY.md](SECURITY.md).
