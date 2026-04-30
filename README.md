# Victoria

Victoria is a sandbox correction engine for chat-native workflow automation. This implementation follows `doc/sandbox-tech-spec` as a production-oriented MVP slice: tenant provisioning, sandbox workflow execution, review packets, gateway reply parsing, correction persistence, rule candidates, human promotion, skill-version pinning, replay, audit events, and MCP write gates.

## Run

In-memory mode:

```sh
go run ./cmd/victoria
```

Postgres mode:

```sh
VICTORIA_DATABASE_URL='postgres://user:pass@localhost:5432/victoria?sslmode=disable' go run ./cmd/victoria
```

The API listens on `:8080` by default. Override with `VICTORIA_ADDR`.

## Test

```sh
go test ./...
```

Optional Postgres integration test:

```sh
VICTORIA_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/victoria_test?sslmode=disable' go test ./internal/store/postgres -run TestPostgresStoreCorrectionLoopAndAuditImmutability -count=1
```

## Main API Surfaces

- `POST /admin/tenants` provisions a tenant, channel binding, workflow templates, and initial `SkillVersion`.
- `POST /cases` starts a sandbox/live case for the authenticated tenant. Tenant context comes only from `Authorization: Bearer tid:<tenant_id>`.
- `POST /gateway/inbound` accepts a resolved operator reply and emits the 16-field approval signal envelope.
- `GET /candidates` lists rule candidates for the authenticated tenant.
- `POST /admin/candidates/{tenant_id}/{candidate_id}/promote` promotes a candidate and creates a new immutable `SkillVersion`.
- `POST /admin/replays` replays a case with pinned or current skill version.
- `GET /mcp/tools?mode=sandbox|live` returns the effective MCP tool manifest.
- `POST /mcp/write-final` exercises the MCP three-gate preflight: tenant binding, sandbox mode, approval audit.

This code intentionally uses local adapters for Hermes, Temporal, WhatsApp, and MCP sidecars. The contracts are isolated behind application boundaries so production integrations can replace those adapters without changing the correction-loop behavior.
