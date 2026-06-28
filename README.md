# Victoria

**Workflow automation that onboards by correction, not configuration — for operators who run their business from a phone.**

[![CI](https://github.com/jessepcc/victoria/actions/workflows/ci.yml/badge.svg)](https://github.com/jessepcc/victoria/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)

> 🧭 **An architecture study** — a working *harness* for a correction-loop
> product, not a shipped SaaS. Read [What this is](#what-this-is-and-isnt) and
> [DESIGN-NOTES.md](DESIGN-NOTES.md) for an honest map of what's real vs. stubbed.

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

### What this is (and isn't)

This repository is an **architecture study** — a working, production-*shaped*
**harness** for that correction loop, not a shipped product. It exists to explore
one question in code: *can an operator onboard automation by correcting a draft
instead of configuring a workflow?* — and to build the engine such a product
would need.

**What's real and worth reading:** the correction-loop data model, confidence-
scored rule promotion (Wilson lower bound + recency + scope), an idempotent
append-only audit trail, a compile-time dev/prod safety split, a real WhatsApp
operator surface (tappable review packets that drive corrections), an optional
real LLM agent — all behind clean, inward-pointing layering with a zero-
dependency test suite.

**What's deliberately *not* here:** real per-tenant authentication, hard
multi-tenant isolation, transactional integrity across the correction op, and a
generalizable rule-extraction model. These are the miles between a harness and a
product; they're catalogued honestly — with the reasoning — in
[DESIGN-NOTES.md](DESIGN-NOTES.md).

For an honest map of what's real vs. stubbed, see
[DESIGN-NOTES.md](DESIGN-NOTES.md); for how the code is organized, see
[ARCHITECTURE.md](ARCHITECTURE.md).

## How the correction loop works

<p align="center">
  <img src="doc/assets/whatsapp-correction-demo.gif" width="320" alt="An operator correcting a Victoria draft from WhatsApp">
</p>

> The loop on a real phone (A0 self-chat): Victoria posts a review packet with a
> **real, agent-drafted** reply; the operator corrects it in plain language
> (*"ask for photos first"*); Victoria records the correction and hands back an
> updated, forward-ready draft. No dashboard, no forms.

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

**Reasoning agent (optional).** Set `DEEPSEEK_API_KEY` to put a real agent
([DeepSeek](https://api.deepseek.com), V4 Pro) behind the draft: it extracts the
facts, picks the action, and writes the customer-facing draft the operator then
corrects. Without the key the engine drafts deterministically, so every other
path (and the whole test suite) runs unchanged with zero external dependencies.

```sh
export DEEPSEEK_API_KEY=sk-...            # enables the agent
export VICTORIA_AGENT_MODEL=deepseek-v4-pro          # optional override
export VICTORIA_AGENT_BASE_URL=https://api.deepseek.com  # optional override
```

(`VICTORIA_AGENT_MODEL` defaults to `deepseek-v4-pro`; set it to whatever model
id your DeepSeek account exposes. Any OpenAI-compatible endpoint works via
`VICTORIA_AGENT_BASE_URL`.)

The agent runs **only when Victoria has no learned rule for the case** — a real
decision point. Once a correction is promoted into a rule, that case is decided
deterministically with no LLM call, so the correction loop *reduces* LLM cost as
Victoria learns rather than paying per case forever.

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

The primary correction-loop routes (health, audit-read, skill-version, and a few
WhatsApp lifecycle routes are mounted too but omitted here for brevity — see
`internal/httpapi/server.go`):

| Endpoint | Purpose |
|---|---|
| `POST /admin/tenants` | Provision a tenant, channel binding, workflow templates, initial `SkillVersion` |
| `POST /cases` | Start a sandbox/live case for the authenticated tenant |
| `POST /ingest/customer-message` | Ingest a canonical customer message (00-channel) → `enquiry_triage` case |
| `POST /gateway/inbound` | Accept a resolved operator reply, emit the structured approval-signal envelope (`domain.ApprovalSignalEnvelope`) |
| `GET /candidates` | List rule candidates for the tenant |
| `POST /admin/candidates/{tenant_id}/{candidate_id}/promote` | Promote a candidate → new immutable `SkillVersion` |
| `POST /admin/replays` | Replay a case with a pinned or current skill version |
| `GET /mcp/tools?mode=sandbox\|live` | Effective MCP tool manifest |
| `POST /mcp/write-final` | MCP three-gate preflight (tenant binding, sandbox mode, approval audit) |
| `POST /channel-bindings/whatsapp/consent` | Record WhatsApp consent + mode before pairing |
| `POST`/`DELETE /channel-bindings/whatsapp/customers` | Manage the A0 customer allowlist (body field `customer` — a number/identifier normalized to a JID) |
| `POST /channel-bindings/whatsapp/init` | Start whatsmeow pairing after consent |
| `GET /channel-bindings/whatsapp/qr.png` | Renderable pairing QR — see [doc/whatsapp-setup.md](doc/whatsapp-setup.md) |

The privileged `/admin/*` control-plane routes require
`Authorization: Bearer admin:<VICTORIA_ADMIN_TOKEN>` (default-deny — they return
503 until the token is configured). Per-tenant routes carry tenant context via
`Authorization: Bearer tid:<tenant_id>` — a **sandbox/development identity
scheme** that must run behind a trusted authenticating gateway. See
[SECURITY.md](SECURITY.md) for the full threat model.

WhatsApp is a real adapter built on [`go.mau.fi/whatsmeow`](https://pkg.go.dev/go.mau.fi/whatsmeow),
and the reasoning agent is a real DeepSeek integration (enabled by
`DEEPSEEK_API_KEY`); the Temporal and MCP sidecars are still local adapters. Set
`VICTORIA_WHATSAPP_DISABLED=1` to skip whatsmeow (e.g. CI without Postgres).

## Customer-inbound channel tiers (design)

The product design splits customer-inbound into three tiers (the implementation
reality of each is noted below):

| Tier | What it is | In this repo |
|---|---|---|
| **00** | Customer enquiries ingested from email/Telegram → become `CaseRun`s | ingest endpoint is real; the email/Telegram *adapters* are stubs (you POST the normalized event) |
| **A0** | Read-only Victoria on the operator's existing WA number — drafts replies, operator forwards | real (whatsmeow), needs Postgres |
| **A1** | Dedicated WA number; Victoria handles inbound + outbound end-to-end | real (whatsmeow), needs Postgres |

## Demos / showcase scripts

End-to-end storyline scripts live under [`scripts/`](scripts/). Each script has a
header documenting its pitch and exact run command.

**Start here — the turnkey one.** `showcase-4` runs the full ingest → case →
review-packet → approve loop over plain HTTP against the in-memory server, with
no Postgres, no WhatsApp, and no phone:

```sh
VICTORIA_GATEWAY_INBOUND_TOKEN=demo VICTORIA_ADMIN_TOKEN=demo go run ./cmd/victoria &
VICTORIA_ADDR=http://localhost:8080 \
  VICTORIA_GATEWAY_INBOUND_TOKEN=demo VICTORIA_ADMIN_TOKEN=demo \
  bash scripts/showcase-4-data-inbound-00.sh
```

(Set `DEEPSEEK_API_KEY` first to watch the real agent draft each reply; without
it the drafts are deterministic.)

The rest need more setup — listed honestly so nothing surprises you:

| Script | What it shows | Needs |
|---|---|---|
| `showcase-4-data-inbound-00.sh` | Email/Telegram ingest → `enquiry_triage`, idempotency, approval | **nothing — turnkey, in-memory** ⭐ |
| `showcase-1-teach-by-example.sh` | Operator teaches a new rule in 5 messages | Postgres (`victoria_demo`), paired tenant, **interactive** (waits for WhatsApp replies) |
| `showcase-2-rules-generalize.sh` | Three corrections produce a rule that generalizes | same as 1 |
| `showcase-3-conflict-detection.sh` | Victoria surfaces contradicting corrections | same as 1 |
| `showcase-5-data-inbound-a0.sh` | Read-only WhatsApp (A0) inbound flow | `-tags dev` server |
| `showcase-6-data-inbound-a1.sh` | Dedicated-number WhatsApp (A1) end-to-end | `-tags dev` server |
| `cases-simulator.sh` | Synthetic traffic generator (infinite loop) | run with the **agent off** (unset `DEEPSEEK_API_KEY`) — it posts new cases forever |

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
| [DESIGN-NOTES.md](DESIGN-NOTES.md) | **Reviewers / engineers** | **What's real vs. stubbed, known limitations, and what I'd do next — read this for an honest map** |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Contributors | How the `internal/` packages map to the product |
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

**A working architecture study, not a production system.** The core correction
loop, persistence, customer-inbound ingest, the WhatsApp operator surface
(tappable review packets that drive corrections), and an optional real reasoning
agent (DeepSeek) are implemented and tested. The per-tenant auth scheme, tenant
isolation, transactional integrity, and the rule-extraction model are
intentionally MVP-grade or stubbed — the harness exists to demonstrate the shape
of the engine, not to handle real customer data. The gap between this and a
shippable product is mapped, with reasoning, in
[DESIGN-NOTES.md](DESIGN-NOTES.md) (and the threat model in
[SECURITY.md](SECURITY.md)). Not maintained as an actively developed product.

## Contributing

Issues and PRs are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
layering rules, the dev build-tag convention, and how to run the test suite.
By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

[Apache 2.0](LICENSE) © Jesse Chow
