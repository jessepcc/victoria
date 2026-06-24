# Architecture

This document maps Victoria's code to its product model so a new contributor can
find their way around in a few minutes. For the product thesis and full design
rationale, see [`doc/`](doc/README.md).

## What Victoria does

Victoria is a **sandbox correction engine** for chat-native workflow automation.
An operator's real workflow (e.g. "triage a new customer enquiry") is run against
realistic-but-isolated tools; Victoria presents a **review packet** of what it
did; the operator **corrects** concrete decisions from their phone; each
correction becomes structured knowledge that is promoted into a versioned,
replayable **rule**. That correction loop is both the onboarding experience and
the ongoing improvement mechanism.

## Layering

Dependencies point inward. An arrow `A → B` means "A imports B". `internal/domain`
sits at the centre and imports nothing else in the tree.

```
                 ┌─────────────────────────────┐
   HTTP clients  │        cmd/victoria         │  process wiring + lifecycle
   ───────────►  │   (composition root, main)  │
                 └───────────────┬─────────────┘
                                 │ wires together
        ┌────────────────────────┼─────────────────────────┐
        ▼                        ▼                          ▼
 ┌─────────────┐         ┌──────────────┐          ┌─────────────────┐
 │  httpapi    │ ──────► │     app      │ ◄─────── │   channel/*     │
 │ (transport) │         │  (engine)    │          │ (wa, telegram)  │
 └─────────────┘         └──────┬───────┘          └────────┬────────┘
                                │ Store iface               │
                         ┌──────▼───────┐                   │
                         │   store/*    │                   │
                         │ memory / pg  │                   │
                         └──────┬───────┘                   │
                                ▼                           ▼
                         ┌──────────────────────────────────────┐
                         │              domain                   │
                         │  pure types · errors · hash · scoring │
                         └──────────────────────────────────────┘
```

| Package | Responsibility | Imports |
|---|---|---|
| [`internal/domain`](internal/domain) | Pure types, sentinel errors, content hashing, confidence scoring. The vocabulary every other layer speaks. | — |
| [`internal/app`](internal/app) | The engine: tenant provisioning, sandbox/live case execution, review packets, gateway reply parsing, correction → candidate → rule → skill-version loop, MCP write gates. | `domain` |
| [`internal/store`](internal/store) | Persistence behind the `app.Store` interface: `memory` (zero-dependency) and `postgres` (self-migrating). | `domain` |
| [`internal/channel`](internal/channel) | Inbound/outbound channel adapters (`whatsapp` via whatsmeow, `telegram`) behind a narrow `Adapter` seam; review-packet rendering. | `domain`, `channel` |
| [`internal/httpapi`](internal/httpapi) | chi HTTP transport: routing, tenant auth, JSON envelopes. No business logic. | `app`, `domain` |
| [`cmd/victoria`](cmd/victoria) | Composition root — chooses a store, wires adapters, owns the server lifecycle. | all of the above |

Two seams keep the layers honest and testable:

- **`app.Store`** ([`internal/app/store.go`](internal/app/store.go)) — the
  interface is defined where it is *consumed* (the engine), per Go convention.
  `memory` and `postgres` are interchangeable implementations; the in-memory one
  lets the entire engine and HTTP surface run and be tested with zero external
  dependencies.
- **`channel.Adapter`** ([`internal/channel/adapter.go`](internal/channel/adapter.go))
  — deliberately two methods (`SendOutbound`, `NormalizeInboundWebhook`) plus
  `Channel()`. Anything wider (whatsmeow sessions, Telegram bot tokens) stays
  concrete inside each adapter.

## The correction loop (request lifecycle)

```
customer message ─► POST /ingest/customer-message ─► enquiry_triage CaseRun
       │                                                     │
       │                                          review packet rendered
       │                                                     ▼
       │                            operator receives it on WhatsApp/Telegram
       │                                                     │
       │                          taps a button or replies in natural language
       ▼                                                     ▼
  POST /gateway/inbound ──► parser cascade ──► 16-field approval signal
                                  │
                  ┌──── approve ──┴── correct ────┐
                  ▼                               ▼
            audit event                     Correction persisted
                                                  │
                                       matched into a RuleCandidate
                                                  │
                       POST /admin/candidates/.../promote (human gate)
                                                  ▼
                              new immutable SkillVersion (pinned, replayable)
```

Idempotency and immutability are first-class: customer messages, signals, and
outbound-to-customer rows are deduplicated, and audit events are append-only
(verified by the Postgres integration test).

## Customer-inbound channel tiers

| Tier | What it is |
|---|---|
| **00** | Customer enquiries ingested from email + Telegram, normalized into `CaseRun`s |
| **A0** | Read-only Victoria on the operator's existing WhatsApp number — drafts replies, operator forwards manually |
| **A1** | Dedicated WhatsApp number (whatsmeow) — Victoria handles inbound + outbound end-to-end |

## The dev/prod build-tag split

Demo and operator-debug endpoints (`/admin/dev/*`) and helpers are compiled in
**only** with `-tags dev`, using a `*_dev.go` (`//go:build dev`) /
`*_stub.go` (`//go:build !dev`) pair in both `internal/httpapi` and
`cmd/victoria`. In a production build the routes do not exist in the binary —
not merely gated by an env var — and
[`test/e2e/http_e2e_proddev_test.go`](test/e2e/http_e2e_proddev_test.go) asserts
their absence. This is the pattern to follow for any never-in-prod surface.

## Persistence

The Postgres store self-migrates on `Connect()` (idempotent `CREATE TABLE IF NOT
EXISTS`), so there is no separate migration step. Table groups:

- **Tenancy & routing:** `tenants`, `workflow_templates`, `channel_bindings`,
  `outbound_queue`.
- **Execution:** `case_runs`, `decision_points`, `artifacts`, `review_packets`.
- **Customer I/O:** `customer_messages`, `outbound_to_customer`.
- **Correction loop:** `signals`, `corrections`, `rule_candidates`,
  `validated_rules`, `skill_versions`, `active_skill_versions`.
- **Audit:** `audit_events` (append-only).

Every query is scoped by `tenant_id` for isolation, and all queries are
parameterized.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `VICTORIA_ADDR` | `:8080` | Listen address |
| `VICTORIA_DATABASE_URL` | _(unset)_ | Postgres DSN; when unset, the in-memory store is used |
| `VICTORIA_GATEWAY_INBOUND_TOKEN` | _(required)_ | Shared secret authenticating `/gateway/inbound` posts |
| `VICTORIA_ADMIN_TOKEN` | _(required)_ | Shared secret authenticating the privileged `/admin/*` control plane |
| `VICTORIA_WHATSAPP_DISABLED` | _(unset)_ | Set to `1` to skip whatsmeow (e.g. CI without Postgres) |
| `VICTORIA_TEST_DATABASE_URL` | _(unset)_ | Enables the Postgres integration tests |

## Testing strategy

- **Unit + e2e** (`go test ./...`) run with zero external dependencies — the
  in-memory store backs the HTTP end-to-end correction-loop test.
- **Build-tag e2e** asserts dev routes are absent from production builds.
- **Postgres integration** runs only when `VICTORIA_TEST_DATABASE_URL` is set,
  and otherwise skips — so the default test command is always safe to run.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the commands.
