# Victoria

**Workflow automation that onboards by correction, not configuration — for operators who run their business from a phone.**

[![CI](https://github.com/jessepcc/victoria/actions/workflows/ci.yml/badge.svg)](https://github.com/jessepcc/victoria/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)

> Small business owners shouldn't be asked to *design* automation. They should be
> shown a draft run in the tools they already use, and allowed to correct it from
> their phone.

**The problem.** Plenty of solo operators run six-figure businesses entirely from
WhatsApp, email, and a pocket notepad. They won't log into a dashboard or map
their processes in a workflow builder — the business logic lives in their head
and only surfaces as exceptions and corrections.

**Victoria's approach.** Instead of asking an operator to define a workflow up
front, Victoria runs a *draft* of their real workflow (e.g. "triage a new
customer enquiry") against realistic-but-isolated tools, shows them exactly what
it did in a **review packet**, and lets them correct concrete decisions —
*"wrong template", "ask for photos first", "don't send yet"* — straight from
chat. Each correction becomes structured, versioned, replayable operating logic
instead of a message lost in chat history. That **correction loop** is both the
onboarding experience and the ongoing improvement mechanism.

This repository is a production-oriented Go implementation of that engine. For
the full product thesis, ICP, and design, see [`doc/`](doc/README.md); for how
the code is organized, see [ARCHITECTURE.md](ARCHITECTURE.md).

## How the correction loop works

```
customer message ─► ingest ─► sandbox CaseRun ─► review packet ─► operator
                                                                     │
                          ┌──────────── corrects from chat ──────────┘
                          ▼
   Correction ─► RuleCandidate ─► (human promotion) ─► immutable SkillVersion
                          │                                     │
                          └─────────── replayable, audited ─────┘
```

Idempotency and immutability are first-class: customer messages, signals, and
outbound messages are deduplicated, and the audit log is append-only.

## Quickstart

The default in-memory store needs no external dependencies:

```sh
# Both tokens are required at startup — the control plane and webhook are
# default-deny (the server refuses to start without them):
export VICTORIA_GATEWAY_INBOUND_TOKEN=dev-token   # authenticates /gateway/inbound
export VICTORIA_ADMIN_TOKEN=dev-admin             # authenticates the /admin/* control plane
go run ./cmd/victoria
# → victoria listening on :8080
```

With Postgres (enables persistence and the WhatsApp adapter):

```sh
export VICTORIA_GATEWAY_INBOUND_TOKEN=dev-token
export VICTORIA_ADMIN_TOKEN=dev-admin
VICTORIA_DATABASE_URL='postgres://user:pass@localhost:5432/victoria?sslmode=disable' \
  go run ./cmd/victoria
```

Common tasks are wrapped in the [`Makefile`](Makefile) — `make build`,
`make test`, `make lint`, `make check`. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Architecture

Victoria is a cleanly layered Go application; dependencies point inward and
`internal/domain` (the shared type vocabulary) imports nothing else in the tree.

| Layer | Package | Role |
|---|---|---|
| Transport | `internal/httpapi` | chi router, tenant auth, JSON envelopes (no business logic) |
| Engine | `internal/app` | the correction loop, provisioning, MCP gates; owns the `Store` interface |
| Persistence | `internal/store/{memory,postgres}` | interchangeable `Store` implementations |
| Channels | `internal/channel/{whatsapp,telegram}` | inbound/outbound adapters behind a 2-method seam |
| Domain | `internal/domain` | pure types, errors, hashing, confidence scoring |
| Wiring | `cmd/victoria` | composition root + lifecycle |

A notable detail: the concurrency-heavy `whatsapp.Manager` does **not** import
`app` — it inverts the dependency through callback fields wired in `main.go`,
keeping the channel and engine layers decoupled. Full map in
[ARCHITECTURE.md](ARCHITECTURE.md).

## HTTP API surface

| Endpoint | Purpose |
|---|---|
| `POST /admin/tenants` | Provision a tenant, channel binding, workflow templates, initial `SkillVersion` |
| `POST /cases` | Start a sandbox/live case for the authenticated tenant |
| `POST /ingest/customer-message` | Ingest a canonical customer message (00-channel) → `enquiry_triage` case |
| `POST /gateway/inbound` | Accept a resolved operator reply, emit the 16-field approval signal |
| `GET /candidates` | List rule candidates for the tenant |
| `POST /admin/candidates/{tenant_id}/{candidate_id}/promote` | Promote a candidate → new immutable `SkillVersion` |
| `POST /admin/replays` | Replay a case with a pinned or current skill version |
| `GET /mcp/tools?mode=sandbox\|live` | Effective MCP tool manifest |
| `POST /mcp/write-final` | MCP three-gate preflight (tenant binding, sandbox mode, approval audit) |
| `POST /channel-bindings/whatsapp/consent` | Record WhatsApp consent + mode before pairing |
| `POST`/`DELETE /channel-bindings/whatsapp/customers` | Manage the A0 customer JID allowlist |
| `POST /channel-bindings/whatsapp/init` | Start whatsmeow pairing after consent |
| `GET /channel-bindings/whatsapp/qr.png` | Renderable pairing QR — see [doc/whatsapp-setup.md](doc/whatsapp-setup.md) |

The privileged `/admin/*` control-plane routes require
`Authorization: Bearer admin:<VICTORIA_ADMIN_TOKEN>` (default-deny — they return
503 until the token is configured). Per-tenant routes carry tenant context via
`Authorization: Bearer tid:<tenant_id>` — a **sandbox/development identity
scheme** that must run behind a trusted authenticating gateway. See
[SECURITY.md](SECURITY.md) for the full threat model.

WhatsApp is a real adapter built on [`go.mau.fi/whatsmeow`](https://pkg.go.dev/go.mau.fi/whatsmeow);
the Hermes, Temporal, and MCP sidecars are still local adapters. Set
`VICTORIA_WHATSAPP_DISABLED=1` to skip whatsmeow (e.g. CI without Postgres).

## Customer-inbound channels (Beta)

See [doc/customer-inbound-spec.md](doc/customer-inbound-spec.md) for the full
spec, acceptance criteria, and phased rollout (P0 → P10).

| Tier | What it is | Pricing position |
|---|---|---|
| **00** | Customer enquiries ingested from email + Telegram → become `CaseRun`s | Floor requirement for A0 and A1 |
| **A0** | Read-only Victoria on the operator's existing WA number. Drafts replies; operator forwards manually. | Lower tier — back-office assistant |
| **A1** | Dedicated WA number (whatsmeow); Victoria handles inbound + outbound to customers end-to-end. | Premium tier — front-line agent |

A1-BSP (WhatsApp Business API) is post-funding scope.

## Demos / showcase scripts

End-to-end storyline scripts live under [`scripts/`](scripts/). Each script has a
header documenting its pitch and exact run command.

| Script | What it shows | Build |
|---|---|---|
| `cases-simulator.sh` | Posts randomized enquiries to `/ingest/customer-message` | default |
| `showcase-1-teach-by-example.sh` | Operator teaches a new rule in 5 messages | default |
| `showcase-2-rules-generalize.sh` | Three SG corrections produce a rule that also handles US suppliers | default |
| `showcase-3-conflict-detection.sh` | Victoria surfaces contradicting corrections for senior review | default |
| `showcase-4-data-inbound-00.sh` | Email/Telegram ingest → automatic `enquiry_triage` (pure HTTP) | default |
| `showcase-5-data-inbound-a0.sh` | Read-only WhatsApp (A0) inbound flow | `-tags dev` |
| `showcase-6-data-inbound-a1.sh` | Dedicated-number WhatsApp (A1) end-to-end | `-tags dev` |

Showcases 5–6 drive the demo via the `/admin/dev/*` simulation endpoints, which
exist **only** in dev builds. Start the server with those routes mounted:

```sh
VICTORIA_GATEWAY_INBOUND_TOKEN=demo-secret VICTORIA_ADMIN_TOKEN=demo-admin go run -tags dev ./cmd/victoria
```

The `dev` build tag is a deliberate safety boundary: impersonation/simulation
routes are compiled out of production binaries entirely (verified by
`test/e2e/http_e2e_proddev_test.go`), not merely gated by an env var.

## Documentation

| Doc | Audience | Purpose |
|---|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Contributors | How the `internal/` packages map to the product |
| [doc/sandbox-tech-spec/](doc/sandbox-tech-spec/) | Architects | Signed-off base architecture (5 specs) |
| [doc/customer-inbound-spec.md](doc/customer-inbound-spec.md) | R&D | Beta launch spec for customer-inbound channels |
| [doc/whatsapp-setup.md](doc/whatsapp-setup.md) | Operators | Runbook for pairing a WhatsApp number to a tenant |

## Testing

```sh
go test ./...                                   # unit + e2e, zero external deps
```

The Postgres integration tests run only when their DSN is set, and skip
otherwise:

```sh
VICTORIA_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/victoria_test?sslmode=disable' \
  go test -count=1 ./internal/store/postgres
```

## Status

Pre-1.0 and under active development. The core correction loop, persistence, and
customer-inbound channels are implemented; the auth scheme and several sidecars
are intentionally MVP-grade (see [SECURITY.md](SECURITY.md)).

## Contributing

Issues and PRs are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
layering rules, the dev build-tag convention, and how to run the test suite.
By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

[Apache 2.0](LICENSE) © Jesse Chow
