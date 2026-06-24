# Customer-Inbound Channels — Implementation Spec

**Status:** Partially implemented for Beta. Core P0-P7 slice landed in this repo; see implementation notes below.
**Predecessors:** `doc/sandbox-tech-spec/` (Round 5 SHIPPABLE) — base architecture.
**Scope:** Three customer-inbound channels (00, A0, A1-whatsmeow). A1-BSP deferred post-funding.

---

## 0. Implementation status in this repo

Implemented:

- PRIV-2 retention sweep (§5.7 / OQ-1): a goroutine in `cmd/victoria` runs
  every 15 minutes and purges `whatsmeow_message_secrets` rows whose
  `chat_jid` is NOT on the tenant's customer allowlist (and is not the
  operator's self-chat). Bounds the worst-case window for non-customer
  body reconstruction at ~30 min. A0 only — A1 sessions are exempt.
- `App.IngestCustomerMessage(ctx, IngestionEvent)` with idempotency on
  `(tenant_id, channel, source_message_id)`, durable `customer_messages`
  storage, automatic `enquiry_triage` case creation, and
  `customer_message_received` audit events.
- Additive `ChannelBinding` fields for `inbound_mode`, command identities,
  customer allowlist, consent, pause, retention, and draft delivery routing.
- Additive `customer_messages` and `outbound_to_customer` persistence in both
  memory and Postgres stores.
- HTTP `POST /ingest/customer-message` for 00-channel adapters and demos.
- `scripts/cases-simulator.sh` for randomized customer-message demo traffic.
- A0 consent gate before WhatsApp pairing, allowlist API, WhatsApp allowlist
  commands, pause/resume, sender classifier, and hard outbound guard with
  `outbound_blocked_to_customer` audit.
- A0 approval behavior: approved replies are delivered as draft text to the
  operator delivery JID only; no `outbound_to_customer` row is written.
- A1 command identity support in the binding model, customer/operator sender
  classifier, approval-gated customer outbound, idempotent `outbound_to_customer`
  records, and retry of queued records after provider send failure.
- Telegram customer chat classification via `telegram_customer_chats`.

Still deferred or not fully wired:

- Real IMAP poller and Telegram Bot API polling/webhook adapter. The canonical
  ingestion path and HTTP surface are ready for those adapters.
- WhatsApp A1 repair/ban-response endpoints, heartbeat metrics, and queued
  customer-outbound drain after repair.
- RLS/role-grant hardening beyond the current JSONB-backed migration style.
- Observability metric emission. Audit events are implemented; Prometheus
  counters/gauges remain future work.
- `customer_message_filtered` audit emission. Wired into the audit event
  catalog (§7.3) but only emitted by the deferred 00-channel adapters
  (web-form, CRM, IMAP `subject_filter_regex`).

Verification:

- `go test ./...`
- `test/e2e` includes HTTP coverage for customer ingestion, consent, and A0
  allowlist management.
- Storyline showcases under `scripts/`:
  - `showcase-4-data-inbound-00.sh` — pure HTTP, exercises 00-channel
    ingestion + idempotency + operator approval over `/gateway/inbound`.
  - `showcase-5-data-inbound-a0.sh` — A0 allowlist gate, in-band WA
    commands, draft-to-operator delivery (uses dev shim).
  - `showcase-6-data-inbound-a1.sh` — A1 register-via-secret,
    `no_command_identity_registered` queue + drain, customer outbound +
    `customer_outbound_sent` (uses dev shim).
- Dev-only HTTP shim mounted under `/admin/dev/*` when
  `VICTORIA_DEV_ENDPOINTS=1` (see `cmd/victoria/main.go`). It can impersonate
  any operator (`IsFromMe=true`) and MUST NOT be enabled in production.

## 1. Product value (engineering framing)

Victoria's correction loop is functional today, but cases must originate **somewhere**. Without customer-inbound, Victoria is internal tooling — operators hand-feed cases via API. Closing this gap is what turns Victoria into a product.

Two distinct product positions are validated separately at Beta:

- **A0 — back-office assistant:** Victoria reads customer messages on the operator's existing WhatsApp account, drafts a proposed reply, and surfaces it for the operator to approve. **Operator manually sends the final reply to the customer** (Victoria does not auto-send to anyone but the operator). Lower price tier; smaller trust ask; works on day-one with no number procurement.
- **A1-whatsmeow — front-line agent:** Each tenant gets a dedicated WhatsApp number that Victoria controls end-to-end. Customers message that number; Victoria reads, drafts, and after operator approval **sends the reply directly to the customer**. Higher price tier; requires per-tenant number procurement; subject to whatsmeow ban risk.

**00** (email / web form / CRM / Telegram bot ingestion) is a **floor requirement** for both tiers. Customers' existing customer-intake pipelines must funnel into Victoria, regardless of which WhatsApp tier they're on.

The operator's actual problem we're solving: every customer message currently demands the operator's full attention to formulate a reply. Victoria reduces that to a 1-tap approval (A0/A1) or a 1-line correction that *teaches Victoria* so the next similar case is handled without intervention. Engineering implication: the value is in the **correction-loop integration**, not the messaging plumbing — but if the messaging plumbing isn't production-grade, the correction loop is irrelevant.

---

## 2. Scope and non-scope

### In scope (Beta)

- **00** — Customer-message ingestion from **email** + **Telegram** (the two channels Beta ICP actually uses). Web-form and CRM-webhook adapters deferred until first Beta customer requests them (§4.3, §4.4).
- **A0** — Read-only Victoria on operator's existing WA account. Sender classifier, customer chat allowlist, outbound guard, operator notification with approved-draft delivery.
- **A1-whatsmeow** — Dedicated tenant WA number, full inbound + outbound, command-identity routing, ban-response runbook.
- Cross-tier: customer-message ingestion path (`App.IngestCustomerMessage`), per-tenant binding configuration, audit events, observability metrics.

### Explicitly out of scope (Beta — defer post-funding)

- **A1-BSP** (WhatsApp Business API). Architecture must accommodate it as a future `ChannelAdapter` swap; no implementation now.
- **LLM-assisted parser cascade** (operator-ux RESOLVED-OE-OU-2). Stay on keyword/text-match for both directions.
- **Multi-region deployment** of whatsmeow sessions.
- **Customer-side rich attachments** (images, PDFs, voice). Beta is text-only customer messages; attachments accepted but not parsed.
- **Real-time admin web console.** Allowlist + binding management is API + WhatsApp commands; web UI deferred.

---

## 3. Architectural overview

The existing `ChannelAdapter` interface (`internal/channel/adapter.go`, operator-ux §4.5) is the seam. This spec extends it via per-tenant binding configuration; the interface itself doesn't change.

```text
                        ┌─────────────────────────────────────┐
   Customer (any        │  Customer-Inbound Channels (00)     │
   non-WA channel)  ───►│  • email IMAP poller     [Beta]     │
                        │  • Telegram bot          [Beta]     │
                        │  • web-form HTTP recv    [deferred] │
                        │  • CRM webhook receiver  [deferred] │
                        │  → IngestionEvent (canonical)       │
                        └──────────────┬──────────────────────┘
                                       │
   Customer (WA, A0)  ───►┌────────────▼──────────────────────┐
   to operator's number   │  WhatsApp Adapter (whatsmeow)     │
                          │  Per-tenant tenantClient          │
   Operator (instructions │  dispatchInbound:                 │
   over their own WA)     │    sender classifier              │
                       ───┤      A0: IsFromMe → operator      │
                          │      A0: !IsFromMe ∧ allowlisted  │
                          │           → customer              │
                          │      A0: !IsFromMe ∧ !allowlisted │
                          │           → ignored (no act)      │
                          │      A1: Sender == cmd_id → op    │
                          │      A1: Sender != cmd_id → cust  │
                          │  outbound guard:                  │
                          │      A0: refuse if dst != op JID  │
                          │      A1: allow if MCP approved    │
                          └──────────────┬────────────────────┘
                                         │
                       ┌─────────────────▼──────────────────────┐
                       │  App                                   │
                       │  • IngestCustomerMessage  → StartCase  │
                       │  • HandleOperatorReply    → existing   │
                       │      correction loop                   │
                       └────────────────────────────────────────┘
```

Cross-cutting components:

- **`ChannelBinding`** (existing `domain.ChannelBinding`) gains `inbound_mode`, `command_identity`, `customer_allowlist`, `consent_acknowledged_at` fields.
- **`customer_messages`** (new table) records every customer message ingested, with idempotency.
- **`outbound_to_customer`** (new table) records every customer-bound reply — A1 only; A0 never writes here.
- **`AuditEvent`** types extended (see §8).

---

## 4. Channel 00 — Non-WhatsApp ingestion

### 4.1 Common contract

All 00-channel adapters share:

- A canonical `IngestionEvent`:
  ```go
  type IngestionEvent struct {
      TenantID          string
      Channel           string  // "email" | "web_form" | "crm:<vendor>" | "telegram"
      SourceMessageID   string  // provider-supplied; idempotency key
      CustomerIdentifier string // email / phone / form-submission-id / chat-id
      ReceivedAt        time.Time
      Subject           string  // optional, per channel
      BodyText          string
      Metadata          map[string]any  // channel-specific (e.g. CRM record id)
  }
  ```
- A single ingestion entry-point: `App.IngestCustomerMessage(ctx, IngestionEvent) (case_run_id, error)`.
- Idempotency: `(tenant_id, channel, source_message_id)` UNIQUE. Re-deliveries return the existing `case_run_id` without creating a new case.
- Audit: every successful ingestion writes a `customer_message_received` audit event.

### 4.2 00.1 — Email adapter

**Mechanism.** Per-tenant IMAP credentials (stored in tenant secret scope per ctrl-plane §5.7 pattern). Background poller (interval per tenant, default 60s) fetches `UNSEEN` messages from a configured folder, normalizes to `IngestionEvent`, calls `IngestCustomerMessage`. Marks fetched messages `\Seen` only on successful ingestion.

**Configuration per tenant:**

- `imap_host`, `imap_port`, `imap_username`, `imap_password` (secret)
- `imap_folder` (default `INBOX`)
- `poll_interval_seconds` (default `60`, min `15`)
- `subject_filter_regex` (optional; only ingest matching subjects)

**Acceptance criteria:**

| ID | Given | When | Then |
|---|---|---|---|
| AC-00.1.1 | Tenant configured with valid IMAP creds | Poller runs, finds 1 UNSEEN message | A `case_run` is created with payload populated from email; message marked Seen; `customer_message_received` audit fires |
| AC-00.1.2 | Same email re-fetched after a restart (e.g., poller crash before Seen-flag write) | Poller runs | `IngestCustomerMessage` returns existing `case_run_id`; **no duplicate** case_run; audit fires once total |
| AC-00.1.3 | Email matches `subject_filter_regex` rejection | Poller runs | Message marked Seen; `customer_message_filtered` audit; no case_run |
| AC-00.1.4 | IMAP auth fails 3 consecutive polls | Background loop | `imap_auth_failed` audit; alert raised; subsequent polls back off exponentially up to 1h |
| AC-00.1.5 | Tenant deprovisioned mid-poll | Mid-poll | Poller exits cleanly; no case_runs created post-deprovision |

### 4.3 00.2 — Web-form adapter (DEFERRED — post-Beta)

**Status:** Deferred. ICP for Beta does not have web forms or multi-channel intake. Revisit when a Beta customer requests it.

When built, planned shape: HTTP endpoint `POST /ingest/web-form/{tenant_id}`, HMAC-signed with tenant-issued secret, JSONPath field mapping per tenant. Same acceptance pattern as 00.1.

### 4.4 00.3 — CRM webhook adapter (DEFERRED — post-Beta)

**Status:** Deferred. ICP for Beta does not run on a SaaS CRM stack. Revisit when first Beta customer with a CRM (HubSpot / Salesforce / Pipedrive / Zoho) onboards.

When built, planned shape: same HMAC + JSONPath pattern as 00.2 with per-vendor pre-canned `field_map` templates as fixtures.

### 4.5 00.4 — Telegram bot adapter

Existing `internal/channel/telegram/adapter.go` already handles inbound webhook normalization for the **operator-side** dev use case. Extend with sender classification: messages from chat IDs marked as customer chats become `IngestionEvent`s; others are operator instructions.

**New configuration per tenant:**
- `telegram_bot_token` (per tenant — limits scope; tenant-bot is the inbound surface)
- `telegram_customer_chats` (jsonb array of chat_ids treated as customer enquiries)

**Acceptance criteria:** mirrors 00.2 but with Telegram-Bot-API webhook semantics.

### 4.6 00 — Demo simulator (build first)

Before building any real adapter, ship `scripts/cases-simulator.sh` that POSTs randomized realistic-looking case payloads every N seconds. Single script, ~50 lines. **Critical for the demo** because it makes the inbound feed visible without committing to one specific channel during the pitch.

---

## 5. Channel A0 — Operator's existing WhatsApp (read-only Victoria)

### 5.1 Pairing

Existing flow (`whatsmeow.Manager.BeginPairing` → operator scans QR). No change beyond:

- **Consent capture:** before generating the QR, operator must POST consent acknowledging that Victoria sees all WA traffic on this account. Stored as `channel_bindings.consent_acknowledged_at`. Refusing → no QR.
- **Mode flag:** binding stored with `inbound_mode = 'read_only'`.

### 5.2 Sender classifier (replaces current naive dispatch)

In `tc.dispatchInbound`:

```go
// Pseudocode
switch {
case evt.Info.IsFromMe:
    // From operator's own account — instruction to Victoria.
    routeOperatorReply(evt)
case isAllowlisted(tenantID, evt.Info.Sender):
    // Customer message we should act on.
    routeCustomerMessage(evt)
default:
    // Personal/non-customer message. Ignore — but emit a low-cardinality
    // metric so we can audit volume without storing content.
    metrics.WAIgnoredMessages.Inc(tenantID)
}
```

### 5.3 Customer chat allowlist

Per-tenant list of WhatsApp JIDs Victoria reacts to. Operator manages via:

- HTTP API: `POST/DELETE /channel-bindings/whatsapp/customers` (auth: tenant JWT).
- WhatsApp commands (operator messages Victoria from their own JID):
  - `add customer +<phone>` — adds JID to allowlist; reply `✓ added`
  - `remove customer +<phone>` — removes; reply `✓ removed`
  - `list customers` — reply with current allowlist
  - `pause` — temporarily ignore all customers (set `customer_intake_paused_until`)
  - `resume` — reverse pause

Allowlist stored as `channel_bindings.customer_allowlist` (jsonb array of JID strings).

### 5.4 Outbound guard (HARD — security invariant)

`whatsmeow.Manager.SendOutbound` MUST refuse to send unless the destination JID equals the paired account's own JID (Message Yourself). Enforced in code AND verified by automated test.

**Invariant A0-OUT-1:** In `inbound_mode = 'read_only'`, the WhatsApp adapter MUST NOT send a message to any JID other than the operator's own account JID. Violations:

- Refuse the send (do not even queue).
- Emit `outbound_blocked_to_customer` audit event with the offending JID and full call site stack.
- Return a non-nil error to the caller.

### 5.5 Customer message ingestion

For each customer message that passes the allowlist:

- Build `IngestionEvent` with `Channel="whatsapp_a0"`, `SourceMessageID=evt.Info.ID`, `CustomerIdentifier=evt.Info.Sender.User`, `BodyText=extractText(evt.Message)`.
- Call `App.IngestCustomerMessage`.
- Operator gets a Message Yourself notification: rendered ReviewPacket (existing `RenderPacket` flow).

### 5.6 Operator-facing draft delivery (the "back-office" half)

When operator approves with `Y`:

- A0 **does not auto-send to the customer.**
- Victoria sends a follow-up to the operator's **dedicated draft-delivery chat** (per OQ-2). Default destination is the operator's Message Yourself JID; tenant-configurable to a different operator JID at consent time. The message contains the **proposed reply text**, formatted so the operator can long-press → forward → send to the customer's chat:
  ```
  ✅ Approved. Here's the draft to send to <customer_name>:
  
  ──────
  Hi! Thanks for reaching out about [...]. Yes, we can quote
  for that — could you send 2-3 photos so we can size it right?
  ──────
  
  Long-press, Forward, pick the customer's chat.
  ```
- An `outbound_draft_delivered_to_operator` audit event records the draft hash.

When operator corrects with `N <reason>` or just `N` then follow-up:
- Existing correction loop runs unchanged. Drafts are re-rendered next time the same case pattern arises.

### 5.7 Privacy & retention

- Non-allowlisted customer messages: never reach `IngestCustomerMessage`. Their decrypted bodies live only in whatsmeow's session store (encrypted at rest at the OS/DB level).
- Configurable per-tenant retention: `whatsmeow_message_secrets` rows for non-allowlisted JIDs purged after `retention_minutes` (**default 30 min** per OQ-1). Implemented as a periodic background sweep at half-cadence (≤15 min by default).
- `customer_messages` table: stores ingested customer message metadata + body text; subject to the standard 7-year audit retention.

### 5.8 Acceptance criteria — A0

| ID | Given | When | Then |
|---|---|---|---|
| AC-A0.1 | Tenant pairs without first POSTing consent | `BeginPairing` called | 403 `consent_required`; no QR generated |
| AC-A0.2 | Tenant in A0 mode, customer NOT on allowlist | Customer message arrives | No case_run; no audit-event-with-content; metric `wa_ignored_messages_total{tenant=...}` increments |
| AC-A0.3 | Tenant in A0 mode, customer IS on allowlist | Customer message arrives | `case_run` created; operator gets a notification at their `draft_delivery_jid` (Message Yourself fallback if unset); `customer_message_received` audit |
| AC-A0.4 | Tenant in A0 mode | Application code calls `WhatsAppManager.SendOutbound` with destination ≠ operator JID | Send refused; `outbound_blocked_to_customer` audit; error returned |
| AC-A0.5 | Operator approves a case via `Y` | Approval audit fires | Victoria sends rendered draft to `draft_delivery_jid`; `outbound_draft_delivered_to_operator` audit; **no** outbound to customer |
| AC-A0.6 | Operator messages `add customer +85299999999` | WA reply received | Allowlist updated; `✓ added` reply; subsequent customer messages from that JID ingest correctly |
| AC-A0.7 | Operator messages `pause` | WA reply received | `customer_intake_paused_until` set to +24h (default); subsequent customer messages emit `customer_intake_paused` metric instead of ingesting |
| AC-A0.8 | Tenant in A0 mode, message in whatsmeow store from non-allowlisted JID is older than retention TTL | Background sweep runs | The corresponding `whatsmeow_message_secrets` row is deleted |

---

## 6. Channel A1-whatsmeow — Dedicated Victoria number (PREMIUM tier)

A1 is the **premium-priced tier**. As part of that price, Victoria supplies the WhatsApp number to the tenant. The operator's contractual deliverables include a dedicated WA-activated number bound to their tenant — they do not procure it themselves.

### 6.1 Number procurement (ops responsibility — non-technical)

For the implementing engineering team this is a **given assumption**: by the time A1 onboarding hits the code, a fresh WhatsApp-capable number has already been procured, activated, and is ready to pair. The technical solution treats the number as input.

Ops responsibilities (separate from this spec):
- Maintain a procurement pipeline of WhatsApp-capable numbers.
- Activate each number for WhatsApp on a dedicated phone/eSIM long enough to scan the pairing QR (~5 min). After that the activation phone can stay offline; whatsmeow's multi-device session has a 14-day inactivity grace.
- Per OQ-7 below, hold a **pool of pre-activated spare numbers** so a banned tenant can be cut over fast.

The pairing API (§6.2) accepts an already-active number. No technical work is required to source it.

### 6.2 Pairing

Same QR flow. Differences:

- Binding stored with `inbound_mode = 'full_control'`.
- After pairing, the tenant must register at least one **command identity** (the operator's personal/business WA JID(s) that will issue instructions). Multi-operator tenants register multiple.
- Command identity registration: the operator messages the Victoria number from their own WA with `register me as operator <secret>` where `<secret>` is provided once via the admin API at provisioning. Victoria validates and persists the operator JID into `channel_bindings.command_identities` (jsonb array).

### 6.3 Sender classifier (A1)

In `tc.dispatchInbound`, after IsFromMe filter:

```go
switch {
case evt.Info.IsFromMe:
    return  // echo of our own outbound — drop
case isCommandIdentity(tenantID, evt.Info.Sender):
    routeOperatorReply(evt)
default:
    routeCustomerMessage(evt)
}
```

There is **no** chat allowlist for A1. Anyone messaging the dedicated Victoria number is by definition either a customer or an operator (latter classified by command-identity JID match).

### 6.4 Outbound to customers (the "front-line agent" half)

When operator approves a case:

- A1 sends the rendered reply text **directly to the customer's JID** via `whatsmeow.SendMessage`.
- Subject to MCP `write_final` 3-gate (sandbox-mode + tenant-binding + approval-audit). Outbound to customer is a `write_final` side-effect class.
- Records `outbound_to_customer` row with `case_run_id`, `recipient_jid`, `body_hash`, `mcp_approval_audit_id`, `sent_at`, `provider_message_id`.
- `customer_outbound_sent` audit event fires.

When operator corrects: existing correction loop. Customer doesn't receive anything until a corrected proposal is approved.

### 6.5 Operator-facing notification routing

By default, A1 sends operator notifications to the **first registered command identity** (typically the operator's own WA number). Multi-operator tenants can configure round-robin, broadcast, or "first available" routing per tenant. Beta ships with **first-registered only**; multi-operator strategies deferred.

### 6.6 Ban response

Spec acknowledges A1-whatsmeow ban risk as **HIGH**. Required infrastructure for production:

- **Heartbeat metric:** `whatsmeow_session_status{tenant=...}` continuously emitted; alert on `disconnected` >5min, `suspended` immediately.
- **Auto-reconnect:** whatsmeow handles transient disconnects internally. Persistent disconnect (>60min) → escalation alert; >24h → status flips to `suspended` automatically.
- **Re-pair runbook:** `POST /channel-bindings/whatsapp/repair` with new QR, operator scans on the same number's phone. If the number itself was banned, tenant supplies a new number; binding updated; existing case data preserved.
- **Anti-loop guard:** `repair` calls rate-limited to 1 per hour per tenant; alerts ops if a tenant trips the limit.
- **Customer continuity messaging:** when in `suspended`, queued outbound is NOT auto-tombstoned; held for 24h pending re-pair (config knob `ban_grace_hours`, default `24`).

### 6.7 Acceptance criteria — A1

| ID | Given | When | Then |
|---|---|---|---|
| AC-A1.1 | Tenant pairs but no command identity registered | Customer message arrives | Case ingested; operator notification queued but NOT sent; alert `no_command_identity_registered` |
| AC-A1.2 | Operator messages `register me as operator <correct secret>` from their JID | WA reply received | JID added to `command_identities`; reply `✓ registered as operator`; subsequent operator messages route as instructions |
| AC-A1.3 | Random JID messages `register me as operator <wrong secret>` | WA reply received | No state change; reply `unknown command`; `command_registration_rejected` audit |
| AC-A1.4 | Customer message arrives, operator approves with `Y` | Approval audit fires | Outbound message sent via whatsmeow to customer JID; MCP 3-gate audit fires; `customer_outbound_sent` audit |
| AC-A1.5 | Customer message arrives, operator approves but MCP gate-3 (approval) absent | `SendOutbound` called | Send refused; `mcp_blocked_write_attempted` audit; no message reaches WhatsApp |
| AC-A1.6 | Session disconnected >60min | Heartbeat loop | `wa_session_disconnect_warning` alert; queue accumulates; no message loss |
| AC-A1.7 | Session suspended (banned) | LoggedOut event from whatsmeow | `wa_session_suspended` alert; queue holds for `ban_grace_hours`; status visible in admin API |
| AC-A1.8 | Operator runs repair within `ban_grace_hours`, scans new QR | Repair flow | Session resumes; queued outbound drains in FIFO order; `customer_outbound_sent` audits fire |
| AC-A1.9 | Repair attempted twice within 1h | Second attempt | 429 `repair_rate_limited`; ops alert |

---

## 7. Cross-cutting

### 7.1 Privacy invariants

- **PRIV-1 (A0 only):** Non-allowlisted customer message bodies MUST NOT enter `customer_messages` table. (Strictly: never reach `IngestCustomerMessage`.)
- **PRIV-2 (A0 only):** `whatsmeow_message_secrets` rows for non-allowlisted JIDs purged within `retention_minutes` (default `30`).
- **PRIV-3:** Operator consent (`channel_bindings.consent_acknowledged_at`) required before any whatsmeow pairing; blocking check at `BeginPairing`.
- **PRIV-4:** All channels: customer body text in `customer_messages` is subject to PII redaction in vertical aggregates (existing `redactAggregateConditions` logic) — no new code path; verify coverage in tests.

### 7.2 Idempotency invariants

- **IDEMP-1:** `App.IngestCustomerMessage` idempotent on `(tenant_id, channel, source_message_id)`. Duplicate calls return existing `case_run_id`, no new audit, no duplicate `customer_messages` row.
- **IDEMP-2:** A1 `outbound_to_customer` idempotent on `(tenant_id, case_run_id, body_hash)`. Replay-safe.
- **IDEMP-3:** Sender classifier deterministic — same input → same routing decision. Verified by property test.

### 7.3 New audit event types

| Event | Emitted by | Payload |
|---|---|---|
| `customer_message_received` | All channels on successful ingest | tenant_id, channel, source_message_id, customer_identifier, case_run_id |
| `customer_message_filtered` | 00 channels with subject/regex filter | reason, channel, source_message_id |
| `customer_intake_paused` | A0 sender classifier when `pause` active | tenant_id, customer_identifier |
| `outbound_blocked_to_customer` | A0 outbound guard | dst_jid, call_site_stack, body_hash |
| `outbound_draft_delivered_to_operator` | A0 approval path | case_run_id, body_hash |
| `outbound_correction_draft_delivered` | A0 correction path: 2-message forwardable draft sent after a correction whose parser yielded a known recommended_action | correction_id, body_hash, recommended_action |
| `customer_outbound_sent` | A1 approval path | case_run_id, recipient_jid, body_hash, mcp_approval_audit_id, provider_message_id |
| `command_registration_rejected` | A1 command-identity flow | sender_jid, reason |
| `no_command_identity_registered` | A1 review-packet path when no operator JID is registered yet | channel, packet_id |
| `repair_rate_limited` | A1 repair endpoint | tenant_id, last_repair_at |

All audit events follow the existing immutable-insert-only pattern (postgres trigger + `audit_writer` role per spec correction-loop §11.0).

### 7.4 Observability

Per-tenant metrics (Prometheus-style names):

- `victoria_inbound_messages_total{tenant, channel, kind="customer|operator|ignored"}` — counter
- `victoria_inbound_processing_duration_seconds{tenant, channel}` — histogram
- `victoria_outbound_to_customer_total{tenant, channel, status}` — counter
- `victoria_outbound_to_customer_blocked_total{tenant, reason="A0_guard|MCP_gate"}` — counter
- `whatsmeow_session_status{tenant}` — gauge (0=qr_needed, 1=connecting, 2=active, 3=disconnected, 4=suspended)
- `whatsmeow_session_disconnect_duration_seconds{tenant}` — gauge

Alerts (per spec operator-ux §4.7.3 thresholds):
- `wa_session_disconnect_warning` (>5min)
- `wa_session_disconnect_error` (>60min)
- `wa_session_suspended` (immediate, A1 only — A0 too but lower severity)
- `customer_intake_volume_anomaly` (per-tenant 7-day moving average ±3σ)

---

## 8. Data model deltas

### 8.1 `channel_bindings` (existing — additive columns)

```sql
ALTER TABLE channel_bindings
  ADD COLUMN inbound_mode TEXT NOT NULL DEFAULT 'read_only'
    CHECK (inbound_mode IN ('read_only', 'full_control')),
  ADD COLUMN command_identities JSONB NOT NULL DEFAULT '[]',
  ADD COLUMN customer_allowlist JSONB NOT NULL DEFAULT '[]',
  ADD COLUMN consent_acknowledged_at TIMESTAMPTZ,
  ADD COLUMN customer_intake_paused_until TIMESTAMPTZ,
  ADD COLUMN retention_minutes INT NOT NULL DEFAULT 30,        -- OQ-1
  ADD COLUMN draft_delivery_jid TEXT;                          -- OQ-2 (NULL = self-chat fallback)
```

### 8.2 `customer_messages` (new)

```sql
CREATE TABLE customer_messages (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  source_message_id TEXT NOT NULL,
  customer_identifier TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL,
  body_text TEXT NOT NULL,
  metadata JSONB,
  case_run_id TEXT,           -- nullable; set when StartCase succeeds
  status TEXT NOT NULL DEFAULT 'ingested',  -- ingested | failed | filtered
  UNIQUE (tenant_id, channel, source_message_id)
);
CREATE INDEX customer_messages_tenant_idx ON customer_messages (tenant_id, received_at DESC);
CREATE INDEX customer_messages_case_idx ON customer_messages (case_run_id);
```

### 8.3 `outbound_to_customer` (new — A1 only)

```sql
CREATE TABLE outbound_to_customer (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  case_run_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  recipient_identifier TEXT NOT NULL,
  body_hash TEXT NOT NULL,
  mcp_approval_audit_id TEXT NOT NULL,
  provider_message_id TEXT,
  sent_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'queued',  -- queued | sent | failed
  UNIQUE (tenant_id, case_run_id, body_hash)
);
CREATE INDEX outbound_to_customer_tenant_idx ON outbound_to_customer (tenant_id, sent_at DESC);
```

### 8.4 RLS / role grants

Both new tables follow correction-loop §2.8.2 RLS pattern (tenant-id isolation). `audit_writer` Postgres role gets INSERT-only on the new audit event types.

---

## 9. Migration & rollout

Phased to keep main branch green at all times.

| Phase | Deliverable | Gating tests | Owner |
|---|---|---|---|
| **P0** | DB migrations (additive columns, new tables, RLS policies) | Postgres integration test passes; existing tests unaffected | Backend |
| **P1** | `App.IngestCustomerMessage` + `customer_messages` storage + `customer_message_received` audit | Unit tests for ingestion idempotency + audit emission | Backend |
| **P2** | 00-simulator (`scripts/cases-simulator.sh`) | Manual: produces ~1 case/30s with realistic payloads | Backend (½ day) |
| **P3** | A0 sender classifier + outbound guard + allowlist API | Unit + integration tests for AC-A0.* matrix | WA team |
| **P4** | A0 WhatsApp commands (`add customer`, `pause`, etc.) | Integration test against real test number | WA team |
| **P5** | A0 operator-draft delivery to `draft_delivery_jid` (Message Yourself fallback when unset) | E2E test with two-tenant fixture | WA team |
| **P6** | A1 command-identity registration + classifier | Unit + integration tests for AC-A1.1–A1.3 | WA team |
| **P7** | A1 outbound to customer (with MCP gate enforcement) | E2E test: case → approve → send-to-customer → MCP audit | WA team |
| **P8** | A1 ban-response runbook (heartbeat, alerts, repair) | Chaos test: kill whatsmeow session, observe alerts; manual repair | WA team + Ops |
| **P9** | 00 email adapter | Integration test against test IMAP | Backend |
| **P10** | 00 Telegram bot adapter (per-tenant token, customer-chats classifier) | Integration test against test bot | Backend |
| **P-deferred** | 00 web-form adapter, 00 CRM webhook adapter | Activated when first Beta customer requests | Backend |

Beta launch unblocked at **P8 complete + at least one of {P9 email, P10 Telegram} per first-customer needs**.

---

## 10. Decisions (resolved)

All Beta decisions are recorded below. Implementation must follow these — re-open only with explicit Product + Eng sign-off.

| ID | Decision | Implementation notes |
|---|---|---|
| **OQ-1 RESOLVED** | A0 retention default = **30 minutes**, tunable per tenant via `channel_bindings.retention_minutes`. | Background sweep cadence ≤ retention/2 (i.e. ≤15 min) so worst-case overflow is bounded. Test AC-A0.8 must use this default. |
| **OQ-2 RESOLVED** | A0 operator-draft delivery uses a **dedicated chat**, not Message Yourself. Operator opens the Victoria-Operator chat (the JID Victoria sends to) to read drafts and forward them. | Pairing flow records the operator's preferred draft-delivery JID at consent time (default: their own JID = Message Yourself fallback when nothing else specified). All A0 outbound-to-operator targets this JID. |
| **OQ-3 RESOLVED** | A1 command-identity registration secret is delivered to the operator via **email** at provisioning. | Provisioning workflow generates the secret, surfaces it once via the admin response, and triggers an email send to the operator's contact-on-file. Secret is single-use; further `register me as operator` attempts after first success are rejected unless explicitly re-issued. |
| **OQ-4 RESOLVED** | A1 multi-operator routing for Beta = **first-registered command identity wins** (all notifications go to the first JID registered). | Document as a Beta limitation in the operator runbook. Multi-operator routing strategies (broadcast / round-robin / on-call) deferred. |
| **OQ-5 RESOLVED** | Per-tenant message metering tracked at **adapter layer** (whatsmeow Manager emits counters per inbound + outbound). | Counters are the source of truth for billing. `outbound_to_customer` table is the durability/audit source; metering reads from adapter metrics. Cross-check job (eventual consistency, daily) reconciles drift. |
| **OQ-6 RESOLVED** | A0 chat allowlist = **JIDs only** for Beta. Contact names and aliases not supported. | Operator commands like `add customer +85299999999` accept E.164 phone (parsed to JID); WhatsApp display names are intentionally excluded — they're mutable and unreliable. |
| **OQ-7 RESOLVED** | A1-whatsmeow ban exposure mitigated by a **pool of pre-activated spare numbers** held by Ops. On ban: cut over to a spare; operator emails customers the new number; outbound queue drains to the new session within `ban_grace_hours`. | Ban risk is acknowledged. Reference: OpenClaw, Hermes ship on whatsmeow at production scale; this is a known-managed risk, not a blocker. Spare-pool inventory + cutover runbook are Ops responsibilities; engineering surface is the existing repair endpoint (`POST /channel-bindings/whatsapp/repair`) accepting a new number. |
| **OQ-8 RESOLVED** | A0 outbound-guard violation = **refuse + audit + log** (no paging). | If `outbound_blocked_to_customer` event count crosses a per-tenant threshold (default: 5/hour, tunable), an aggregated alert fires — but per-event paging is intentionally suppressed to avoid alert fatigue. |

---

## 11. References

- `doc/sandbox-tech-spec/04-operator-experience.md` — base ChannelAdapter spec and operator-side UX
- `doc/sandbox-tech-spec/02-execution-plane.md` §5.6 — MCP 3-gate (applies to A1 outbound)
- `doc/sandbox-tech-spec/03-correction-loop.md` §2.0–§2.10 — DDL patterns, RLS
- `doc/sandbox-tech-spec/05-architecture-integration-critique.md` — surviving risks (whatsmeow ban, latency)
- `doc/whatsapp-setup.md` — operator runbook (will be updated as part of P3-P5)

---

*Spec status: DRAFT. Reviewed by: TBD. Approved for implementation by: TBD.*
