# Victoria

Victoria is a sandbox correction engine for chat-native workflow automation. This implementation follows `doc/sandbox-tech-spec` as a production-oriented MVP slice: tenant provisioning, sandbox workflow execution, review packets, gateway reply parsing, correction persistence, rule candidates, human promotion, skill-version pinning, replay, audit events, and MCP write gates.

## Documentation

| Doc | Audience | Purpose |
|---|---|---|
| [doc/sandbox-tech-spec/](doc/sandbox-tech-spec/) | Architects | Round-5-signed-off base architecture (5 specs, 9k lines) |
| [doc/customer-inbound-spec.md](doc/customer-inbound-spec.md) | R&D team | **Beta launch spec** for customer-inbound channels (00 email/Telegram, A0 read-only WhatsApp, A1 dedicated-number WhatsApp). Acceptance criteria + phased rollout. |
| [doc/whatsapp-setup.md](doc/whatsapp-setup.md) | Operators | Runbook for pairing a WhatsApp number to a tenant via QR |

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
- `POST /ingest/customer-message` accepts canonical customer messages from 00-channel adapters and idempotently creates an `enquiry_triage` case.
- `POST /gateway/inbound` accepts a resolved operator reply and emits the 16-field approval signal envelope.
- `GET /candidates` lists rule candidates for the authenticated tenant.
- `POST /admin/candidates/{tenant_id}/{candidate_id}/promote` promotes a candidate and creates a new immutable `SkillVersion`.
- `POST /admin/replays` replays a case with pinned or current skill version.
- `GET /mcp/tools?mode=sandbox|live` returns the effective MCP tool manifest.
- `POST /mcp/write-final` exercises the MCP three-gate preflight: tenant binding, sandbox mode, approval audit.
- `POST /channel-bindings/whatsapp/consent` records required WhatsApp consent and mode before pairing.
- `POST` / `DELETE /channel-bindings/whatsapp/customers` manage the A0 customer JID allowlist.
- `POST /channel-bindings/whatsapp/init` starts a fresh whatsmeow pairing after consent; `GET /channel-bindings/whatsapp/qr.png` returns a renderable QR. See [doc/whatsapp-setup.md](doc/whatsapp-setup.md).

The Hermes, Temporal, and MCP sidecars are still local adapters. WhatsApp is now a real adapter built on `go.mau.fi/whatsmeow` (operator-ux spec §4.1–4.7) — bring it up by setting `VICTORIA_DATABASE_URL` and following the runbook in [doc/whatsapp-setup.md](doc/whatsapp-setup.md). To skip whatsmeow entirely (e.g., when running CI without Postgres), set `VICTORIA_WHATSAPP_DISABLED=1`.

## Beta scope (in build)

Customer-inbound channels — see [doc/customer-inbound-spec.md](doc/customer-inbound-spec.md) for full implementation spec, acceptance criteria, and phased rollout (P0 → P10):

| Tier | What it is | Pricing position |
|---|---|---|
| **00** | Customer enquiries ingested from email + Telegram → become `CaseRun`s | Floor requirement for both A0 and A1 |
| **A0** | Read-only Victoria on the operator's existing WA number. Drafts replies; operator forwards manually. | Lower tier — back-office assistant |
| **A1-whatsmeow** | Dedicated WA number (Victoria-supplied as part of the plan); Victoria handles inbound + outbound to customers end-to-end. | Premium tier — front-line agent |

A1-BSP (WhatsApp Business API) is post-funding scope.

## Demo / showcase scripts

Three end-to-end storyline scripts under `scripts/` (run after pairing a tenant via `whatsapp-pair.sh`):

- `cases-simulator.sh` — posts randomized customer enquiries to `/ingest/customer-message` every N seconds.
- `showcase-1-teach-by-example.sh` — operator teaches Victoria a new business rule in 5 WhatsApp messages
- `showcase-2-rules-generalize.sh` — three Singapore corrections produce a rule that also handles US suppliers correctly
- `showcase-3-conflict-detection.sh` — Victoria detects contradicting operator corrections and surfaces them for senior review
