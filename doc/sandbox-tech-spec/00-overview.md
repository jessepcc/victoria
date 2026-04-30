# Victoria — Tech Spec Set (Moderator Synthesis)

**Status:** Five rounds of cross-team review complete. Devil's-advocate sign-off: **SHIPPABLE**. Spec set ready for development handoff.

This document is **derived** from the five architect specs in this directory. It is not authoritative on any contract — every contract below names the spec section that owns it. Use this overview to navigate; trust the spec files for detail.

---

## 1. The five specs

| File | Author role | What it owns | Lines |
|---|---|---|---|
| `01-control-plane.md` | Control Plane Architect | API gateway (Go), tenant registry & provisioning, Rule Review Console, evaluation/replay scheduler, observability + federated audit query authority, billing, internal mTLS CA, tenant-context propagation | 1,834 |
| `02-execution-plane.md` | Execution Plane / Agent Runtime Architect | Hermes runtime in Docker, Temporal worker (workflows + activities), MCP servers (sandbox-email, sandbox-drive, sandbox-invoice), per-client deployment unit, sandbox case seed pipeline, replay determinism | 1,595 |
| `03-correction-loop.md` | Correction Loop & Learning System Architect | Postgres data model for all entities (DDL+RLS), candidate-match & confidence algorithms (Wilson lower bound), ValidatedRule versioning, SkillVersion model, replay semantics, AuditEvent canonical schema + writer registry | 2,632 |
| `04-operator-experience.md` | Operator Experience & Messaging Architect | WhatsApp (whatsmeow embedded) + Telegram dev gateway, review packet schema, correction action set & follow-up flow, parser cascade, artifact preview, signal contract gateway-side, channel-binding tenant validation | 1,299 |
| `05-architecture-integration-critique.md` | Devil's Advocate / Architecture Integrity Critic | Cross-component contracts, security/isolation invariants, end-to-end acceptance scenarios (SC-01..SC-08), TDD posture audits per round, OE/UE smell hunt, **R5 final sign-off** | 1,460 |

**Total: 9,011 lines** (excluding this overview).

---

## 2. Final per-spec scores (Round 5 sign-off)

Rubric: TDD discipline (1–5) + isolation completeness (1–5), per devil's-advocate `05-architecture-integration-critique.md` §Round 5.

| Spec | TDD | Isolation | Total | Notes |
|---|---|---|---|---|
| `01-control-plane.md` | 5 | 5 | **10/10** | §12.6 RLS+JWT+idempotency property tests; §19 23-row cross-component contract test table; §20 SC participation map; §18 storage topology defers to learning-loop §2.0; mTLS in §6.2 with 7 invariants and 6 contract tests. |
| `02-execution-plane.md` | 5 | 5 | **10/10** | Top-of-team since Round 3. §5.6 single-store mTLS read; §13.4 storage topology contribution; §11 11-row contract test table; dual-path Temporal IAM tests. |
| `03-correction-loop.md` | 5 | 5 | **10/10** | §13.0 13-row contract test table CT-LL-1..13; PROPERTY MERGE-1..9 (incl. PII-classifier with Unicode/base64/JSON/whitespace adversarial generators); §11.0 single-store audit topology; INV-05a/b/c/d in §13.5 with literal `"ABC Realty"` PII assertion. |
| `04-operator-experience.md` | 4.5 | 4.5 | **9/10** | §3.1 7-row contract test table; §3.2 4 fuzz tests; §3.3 SC participation; OPEN-Envelope-Naming and OPEN-Envelope-ScopeHint closed in §2.2 body. Half-point withhold: bridge-container-crash recovery is asserted against WhatsApp's 14-day retention rather than tested at the gateway boundary. |

---

## 3. The architectural backbone (binding consensus after Round 5)

Each row is **agreed across all relevant specs**. Cite the listed sections as the authoritative source when implementing.

| # | Decision | Authoritative section(s) |
|---|---|---|
| **A1** | Single `validated_rules` table — all four scopes (`case`, `tenant`, `vertical`, `default`) — physically located in **control-plane DB**, secured by row-level security (RLS). `corrections` and `rule_candidates` stay in **per-tenant execution DB**. | `03-correction-loop.md` §2.0, §2.7, §2.8; `01-control-plane.md` §13 |
| **A2** | Single rule-consumption artifact = **`SkillVersion`**, identified by `skill_version_id`. `case_runs.skill_version_id` is the time-travel anchor. `LoadSkillVersion(tenant_id, workflow_slug, as_of?)` consumes it; manifest fed to Hermes as system-prompt context (Model C: pull-at-workflow-start, pin-for-run). | `03-correction-loop.md` §9; `02-execution-plane.md` §8 |
| **A3** | Approval signal transport: **gateway → Temporal SDK direct** (control plane is not on the hot path). Gateway holds a per-tenant signal-only Temporal credential in `gateway/tenants/{tenant_id}/temporal_signal_client`; control plane provisions. Async `APPROVAL_RECEIVED` ingest reconciles the audit chain. | `02-execution-plane.md` §7 (RESOLVED-C3); `01-control-plane.md` §6.6, §6.8; `04-operator-experience.md` §2.3, §10 |
| **A4** | Replay determinism = **four commitments**: (1) `skill_version_id` pinned at workflow start; (2) LLM `temperature=0` on replay only; (3) MCP read-tool snapshots stored as `mcp_read_snapshot` artifacts on the original run and replayed from artifact store; (4) regression contract is at decision-point outcome level, not artifact bytes. | `02-execution-plane.md` §4.3, §10.8; `03-correction-loop.md` §10, PROPERTY MERGE-8 |
| **A5** | WRITE_EXTERNAL approval gate is enforced **inside the MCP server** as a synchronous Postgres `SELECT` on the **control-plane** `audit_events` via `mcp_audit_reader` role + `mcp_approval_events` view over **mTLS** (3 independent gates per tool call: tenant-header binding, sandbox-mode, approval-received). | `02-execution-plane.md` §5.6; `03-correction-loop.md` §11.5; `01-control-plane.md` §9.3 |
| **A6** | Sandbox vs. live MCP servers are **separate processes**, not a runtime flag. Sandbox-mode enforced at four independent layers: workflow input, MCP tool manifest exclusion, Temporal activity registration, provisioning manifest immutability. Canonical encoding: `mode: 'sandbox' \| 'live'` enum (default-deny on missing). | `02-execution-plane.md` §5, §6.3, INV-T5 |
| **A7** | `audit_events` immutability enforced at the **storage layer**: `audit_writer` Postgres role with INSERT-only privilege; `BEFORE UPDATE OR DELETE OR TRUNCATE` trigger. Single authoritative `audit_events` in **control plane** + per-tenant `audit_events_outbox` as **drain buffer only** (not a read source). MCP preflight reads control-plane store over mTLS. | `03-correction-loop.md` §11.0, §11.5; `01-control-plane.md` §9.3, §9.4 |
| **A8** | `corrections` row + `correction_received` audit event have **one writer**: the execution plane's `PersistCorrection` activity (driven by inbound Temporal signal). The 16-field `ApprovalSignalEnvelope` carries enough payload that no extra fetch is required. | `03-correction-loop.md` §3.5, §11.6; `02-execution-plane.md` §7.4; `04-operator-experience.md` §2.2 |
| **A9** | Rule fetch model = **pull at workflow-start (authoritative)**. Control-plane → execution-plane push is downgraded to a **non-authoritative cache invalidation hint**. | `03-correction-loop.md` §9.5 |
| **A10** | Tenant context is JWT-claim-only via `context.Context` propagation; Postgres RLS as defense-in-depth. **`tenant_id` is never accepted from a client-supplied request parameter.** Inbound webhook tenant-id is derived solely from `channel_bindings.provider_number → tenant_id`. | `01-control-plane.md` §3, §6.3; `04-operator-experience.md` §4.9 |
| **A11** | All service-to-service auth uses **mTLS** with leaf certificates, SAN-encoded workload identity, 30-day rotation with 24-hour overlap. Internal CA is operated by the control plane. | `01-control-plane.md` §6.2; `02-execution-plane.md` §13.1 INV-RPC1..4 (canonical) |
| **A12** | Idempotency key composition: **SHA-256 derivation rule** with 10-row registry. Applied uniformly across `signal_id`, `packet_id`, `correction.idempotency_key`, MCP tool keys, replay keys. | `01-control-plane.md` §17; consumed in exec-plane §15, learning-loop §3.9, operator-ux §9.6 |
| **A13** | ChannelAdapter is **narrowed to two methods** (`SendOutbound`, `NormalizeInboundWebhook`) — kept as a deliberate test seam, not a future-channel abstraction. WhatsApp (whatsmeow embedded) and Telegram (dev) only. | `04-operator-experience.md` §4.5 |
| **A14** | WhatsApp session lifecycle: 15s heartbeat, 60s disconnect threshold, durable per-tenant outbound queue (depth 100, FIFO drain on reconnect, oldest-tombstone overflow), inbound buffering via WhatsApp's 14-day server-side retention, alerting tiered at 5/60/1440 min. **CT-Outage-01** simulates 30-minute disconnect, asserts no message loss. | `04-operator-experience.md` §4.7, §4.7.6 |
| **A15** | Parsing pipeline split: **Stage A (gateway)** = button enum, scope_hint extraction, follow-up flow, dead-letter routing. **Stage B (learning)** = semantic conversion of `free_text + follow_up_answer → parsed_conditions / parsed_action`. | `04-operator-experience.md` §7.1; `03-correction-loop.md` §3.6 |
| **A16** | WhatsApp session storage provisioned by `TenantProvisioningWorkflow`; whatsmeow embedded in Go gateway. Operator JWT issuance: control-plane Auth Service is sole issuer (24h TTL default). | `01-control-plane.md` §5.7, §6.7 |

---

## 4. End-to-end acceptance scenarios (8/8 testable)

Devil's-advocate's SC-01..SC-08 in `05-architecture-integration-critique.md` §5 — every scenario has named contract tests at every component boundary.

| Scenario | Verdict | Owning tests (selected) |
|---|---|---|
| SC-01 Golden-path correction | TESTABLE | exec-plane SC-01 row §10.7; learning-loop CT-LL-1, GOLDEN PROMOTE-1; ctrl-plane `test_provisioning_manifest_delivered`; operator-ux CT-Outbound-01 |
| SC-02 Multi-correction promotion | TESTABLE | learning-loop CT-LL-5, PROPERTY MERGE-2/MERGE-7; ctrl-plane `test_replay_scheduler_trigger` |
| SC-03 Contradicting-correction supersession | TESTABLE | learning-loop §8.4 VER-1..3, GOLDEN PROMOTE-2 |
| SC-04 Abandoned packet | TESTABLE | exec-plane `test_no_persist_correction_on_abandon`; operator-ux CT-Expiry-01 |
| SC-05 Tenant isolation leak attempt | TESTABLE | learning-loop CT-LL-3 (RLS), CT-LL-6/7 (PII strip); ctrl-plane `test_rls_cross_tenant_select`; operator-ux CT-TenantBinding-01 |
| SC-06 Sandbox / approval-bypass attempt | TESTABLE | exec-plane SC-06 row + `fuzz_mcp_write_final_three_gate` (24-cell Cartesian); learning-loop CT-LL-8/9/11 |
| SC-07 Replay determinism check | TESTABLE | exec-plane `prop_replay_determinism_n_runs` (N=60 derived from binomial); learning-loop CT-LL-12, PROPERTY MERGE-8 |
| SC-08 Double-tap approval idempotency | TESTABLE | operator-ux FT-DoubleTap-Approve; exec-plane `test_approved_signal_idempotency`; learning-loop CT-LL-2 |

---

## 5. The system in one diagram

```text
┌─── Operator (WhatsApp; Telegram dev) ────────────────────────────────────────┐
└────────────┬─────────────────────────────────────────────────────────────────┘
             │ inbound msgs / outbound packets
             ▼
┌─── Messaging Gateway (operator-ux) ──────────────────────────────────────────┐
│  • whatsmeow embedded per tenant; channel_bindings.provider_number → tenant_id │
│  • Reply parser cascade (button → text-match → follow-up → DLQ)              │
│  • Multi-turn follow-up state in gateway Postgres (1h TTL)                   │
│  • Emits 16-field ApprovalSignalEnvelope via Temporal SDK direct ─signal─┐   │
└──────────────┬───────────────────────────────────────────────────────────┼───┘
               │ JWT verify / OTP issue (mTLS)                              │
               ▼                                                            │
┌─── Control Plane (ctrl-plane) ─────────────────────────────────────────────┼──┐
│  • API gateway (Go) · Tenant registry · Provisioning workflow               │  │
│  • Rule Review Console (review/promote/reject/rollback ValidatedRules)      │  │
│  • Evaluation service / replay scheduler (POST /admin/replays)              │  │
│  • Observability + federated audit query authority                          │  │
│  • Internal mTLS CA (leaf certs, SAN workload identity, 30d rotation)       │  │
│  • SINGLE control-plane DB:                                                 │  │
│      - validated_rules (all scopes, RLS) · skill_versions · audit_events    │  │
│      - tenants · workflow_templates · channel_bindings                      │  │
│      - quarantined_aggregates · vertical_candidate_aggregates               │  │
└────┬──────────────────┬──────────────────────────────────────────────────┬─┘  │
     │ provisioning     │ LoadSkillVersion (pull-at-start, pinned, mTLS)   │    │
     │ manifest         │ rule push (cache hint only)                      │    │
     ▼                  ▼                                                  │    │
┌─── Per-Client Execution Plane (exec-plane) ─────────────────────────────┘    │
│                                                                              │
│  ┌────────────────┐   ┌──────────────────────┐   ┌─────────────────────────┐│
│  │ Hermes runtime │←─►│  Temporal worker     │   │  Sandbox MCP servers    ││
│  │ (Docker pin)   │   │  task-queue per      │←─►│   - sandbox-email       ││
│  │ data vol per   │   │  tenant              │   │   - sandbox-drive       ││
│  │ tenant         │   │  WorkflowID =        │   │   - sandbox-invoice     ││
│  │ (persona +     │   │  case_run_id         │   │  3-gate WRITE_EXTERNAL: ││
│  │ starter skills │   │  Signal: CaseApproved│   │   tenant-header binding,│◄─ Signal
│  │ ONLY — no rule│   │  / CaseRejected      │   │   sandbox-mode,         ││
│  │ manifests)     │   │                      │   │   approval_received     ││
│  └────────────────┘   └─────────────┬────────┘   │   (SELECT control-plane ││
│                                     │            │   audit_events via mTLS)││
│                                     │            └─────────────────────────┘│
│  Per-tenant Postgres execution DB:                                           │
│    corrections, rule_candidates, case_runs, decision_points, artifacts       │
│    (incl. mcp_read_snapshot), audit_events_outbox (drain buffer only)        │
│  Per-tenant object store (artifact bytes)                                    │
│  Per-tenant secret scope (LLM keys, MCP credentials, mTLS leaf certs)        │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Recommended development entry-point order

From devil's-advocate sign-off, `05-architecture-integration-critique.md` §5. **Constraint-derived, not schedule-derived** — each step's gating tests must pass before the next.

1. **Tenant provisioning workflow + per-tenant DB schema migration** — `ctrl-plane §5`, `learning-loop §2.0–§2.10` DDL. Everything per-tenant depends on this.
2. **Internal CA + mTLS issuance** — `ctrl-plane §6.2`. Every internal call needs leaf certs.
3. **Hermes runtime container + provisioning manifest config-mount** — `exec-plane §2`. Gating tests: `test_provisioning_manifest_delivered`, `test_hermes_volume_tenant_label_match`.
4. **Sandbox MCP servers (email, drive, invoice)** — `exec-plane §5`. Gating: `fuzz_mcp_write_final_three_gate` (24-cell Cartesian) before any draft tool exposed to Hermes.
5. **Single-tenant Temporal worker + EnquiryTriageWorkflow** — `exec-plane §3, §4`. Pick simplest workflow first.
6. **Learning-loop candidate matcher + confidence scorer** — `learning-loop §4, §5`. Gating: PROPERTY MERGE-1..9 pass.
7. **Rule Review Console + promotion pipeline** — `ctrl-plane §7` + `learning-loop §7`.
8. **Audit-events store + outbox drain protocol** — `ctrl-plane §9.3/§9.4` + `learning-loop §11.0/§2.0.1`.
9. **WhatsApp session (whatsmeow) + gateway** — `ctrl-plane §5.7` + `operator-ux §4`. **Last**: highest operational risk; everything before this can be exercised via Telegram + Temporal CLI.
10. **Operator OTP issuance + JWT** — `ctrl-plane §6.7`.

### Six gating integration tests before any cross-component PR merges

(per `05-architecture-integration-critique.md` §5)

1. `test_provisioning_manifest_delivered` (ctrl-plane §19) + `test_hermes_volume_tenant_label_match` (exec-plane §10.6) — proves provisioning end-to-end.
2. `test_mtls_handshake_required`, `test_mtls_san_workload_identity`, `test_mtls_san_mismatch_rejected` (ctrl-plane §6.2.2) — proves transport isolation.
3. `test_persist_correction_from_signal_alone` / CT-LL-1 (learning-loop §13.0; exec-plane §11) — proves the 16-field envelope handoff.
4. `test_rls_cross_tenant_select` (ctrl-plane §12.6.1) — proves storage-layer tenant isolation.
5. `test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_sandbox_blocks_even_with_approval` (exec-plane §10.4) — proves the 3-gate preflight.
6. `CT-Outage-01` (operator-ux §4.7.6) — proves WhatsApp session outage no-loss for ≤30min.

---

## 7. Final OPEN inventory (10 cross-component items, all non-blocking)

Categorized in `05-architecture-integration-critique.md` §3. None block development handoff.

| Tag | Topic | Owner | What would resolve |
|---|---|---|---|
| OPEN-PRODUCT-INPUT-1 | Min legal retention for audit events (AU/UK/US) | Product / Legal | Legal sign-off; default 7y stands until then. |
| OPEN-PRODUCT-INPUT-2 | Operator JWT lifetime (24h vs. 7d vs. 30d) | Product | Default 24h shipped; product decision on session length. |
| OPEN-PRODUCT-INPUT-3 | Vertical assignment mutability and multi-vertical tenants | Learning Architect + Product | Product decision; current schema = single vertical, immutable. |
| OPEN-PRODUCT-INPUT-4 | RuleCandidate confidence recalc trigger | Learning Architect | Internal architectural choice; doesn't block first pilot. |
| OPEN-PRODUCT-INPUT-5 | Condition schema ownership at replay-assertion time | Learning Architect | Internal architectural choice; affects Evaluation Service impl. |
| OPEN-MEASUREMENT-1 | mTLS Postgres RTT on MCP `WRITE_EXTERNAL` preflight (P99 ≤50ms target) | Exec-plane / Observability | Production telemetry. Fallback designed: revert to per-tenant outbox-as-read-source if SLO missed. |
| OPEN-MEASUREMENT-2 | Temporal native IAM granularity (path A vs. path B sidecar) | Exec-plane | Deploy-time check. Path A native default; path B sidecar fully specified as fallback (operator-ux §10.4). |
| OPEN-OUT-OF-SCOPE-1 | Approval-gated shadow + autopilot modes | All | Phase 2; `mode: sandbox\|live` truncates 4-state progression to 2 deliberately. |
| OPEN-OUT-OF-SCOPE-2 | Reviewer-facing browser UI | Ctrl-plane | Phase 1 uses API + minimal admin UI; full reviewer UI deferred. |
| OPEN-OUT-OF-SCOPE-3 | Multi-channel beyond WhatsApp + Telegram-dev | Operator-ux | Deferred until 3rd channel exists; ChannelAdapter is test seam, not future-channel abstraction. |

**Per-spec OPEN items.** Individual specs carry additional OPEN items (OPEN-CROSS-SPEC, OPEN-IMPLEMENTATION, OPEN-DA-DISSENT, OPEN-LEGAL-INPUT, OPEN-PRODUCT-INPUT-OBSERVABILITY, OPEN-PRODUCT-INPUT-ROUTING) that track per-spec coordination and implementation tasks. These are non-blocking and scoped to their owning spec. See each spec's open-item section for the complete per-spec inventory: `01-control-plane.md` §15, `03-correction-loop.md` §15, `04-operator-experience.md` §14.

---

## 8. Surviving architectural risks

Honest list of risks that survived all 5 rounds (`05-architecture-integration-critique.md` §6). Each has a documented response path; none should hold up development.

| Risk | Severity | Trigger for re-architecture | Production monitor |
|---|---|---|---|
| MCP `WRITE_EXTERNAL` preflight latency on hot path (~5–10ms intra-VPC mTLS Postgres read on every operator-approved external action) | HIGH | P99 of Gate-2 read >50ms post-MVP → fall back to per-tenant outbox-as-read-source per OPEN-MEASUREMENT-1 (R3 design preserved). | P99 of `mcp_audit_reader` SELECT per tenant per workflow; alert if median >25ms. |
| whatsmeow session failure is a per-tenant risk (session crash queues outbound packets; recovery depends on WhatsApp's 14-day server retention) | MEDIUM | Session outages causing measurable correction-loss → migrate to WhatsApp Business API (interface already test-seamed). | Session failure count / tenant / week; outbound retry count; gateway-side `packet_tombstoned` events. |
| DA dissent on unified `validated_rules` storage (RLS bypass via privileged role escalation is the failure mode) | LOW | Successful cross-tenant SELECT under expected RLS / Postgres CVE / `app.bypass_tenant_check` misuse → revert case/tenant rules to per-tenant DB (R1 split). | Quarterly forensic test against integration env (learning-loop §2.0.1). |
| LLM-temperature determinism for replay (provider-side changes can break determinism) | MEDIUM | `prop_replay_determinism_n_runs` failure → freeze model+provider version, capture LLM I/O as `mcp_read_snapshot` artifacts. | CI failure rate of replay-determinism property; replay-diff regression rate per WorkflowTemplate. |
| WhatsApp non-Business-API as production transport (ban / protocol change / rate-limit) | MEDIUM | WhatsApp ban or rate-limit affecting ≥1 production tenant → migrate to WhatsApp Business API (per-tenant verification cost non-trivial). | Ban/rate-limit incidents per tenant per month; `channel_session_suspended` audit events. |
| Confidence-scoring formula chosen ahead of data (Wilson lower bound × recency × scope-consistency with hand-tuned constants; thresholds 0.55 / evidence ≥3 unvalidated) | LOW | Promotion patterns at first 10 customers don't match the threshold model → retune defaults (per-tenant overridable via `tenants.conf_*` columns; no schema change needed). | Promotion:rejection ratio per tenant; time-from-first-evidence-to-promotion distribution; reviewer-override rate. |

---

## 9. How the discussion ran

Five rounds, four component architects on Sonnet, devil's advocate on Sonnet, moderator on Opus 4.7.

| Round | Outcome |
|---|---|
| Round 1 — Initial drafts | Each architect produced an opinionated initial spec; devil's advocate produced a critique framework with 8 cross-component contracts (C1–C8), 6 isolation invariants (INV-01–INV-06), 5 over-engineering smells (OE-01–OE-05), 7 under-engineering smells (UE-01–UE-07), and 8 end-to-end acceptance scenarios (SC-01–SC-08). |
| Round 2 — Cross-review | 14 conflicts (C1–C14) resolved across architects. Devil's advocate appended per-spec audit. Specs grew 4,224 → 5,676 lines (+34%), concentrated in resolution language. |
| Round 3 — Conflict resolution | 14+ items resolved including unified `validated_rules` DDL+RLS, 16-field `ApprovalSignalEnvelope`, mTLS for service-to-service auth, canonical `audit_events` schema, `mcp_read_snapshot` artifact_type, `mcp_audit_reader` Postgres role. |
| Round 4 — TDD strategy depth | Each spec converted to invariant-first throughout; cross-component contract test tables published; property/fuzz tests with derivable rigor (e.g., N=60 from binomial discriminator). Two NEW conflicts surfaced: HMAC vs mTLS (R4-CONFLICT-1), single-store vs two-store audit topology (R4-CONFLICT-2). |
| Round 5 — Final consolidation | Both R4 conflicts resolved (mTLS adopted; single-store control-plane authoritative + per-tenant outbox drain). Each spec added Implementation Handoff section. Devil's advocate signed off **SHIPPABLE**. |

The discussion stayed productive because the moderator (a) fed each agent the relevant peer deltas every round, (b) named cross-architect conflicts explicitly with assigned owners and gating questions, (c) refused to converge prematurely (R3 produced 14 resolutions but R4 deliberately surfaced two more architectural conflicts the team had been papering over), and (d) had the devil's advocate audit on stale data once — it transparently corrected itself in R4 and apologized to the team.

---

## 10. How to use these specs for implementation

1. **Start with §3 (the backbone) and §6 (development entry-point order).** Build in dependency order; the 6 gating integration tests must pass before any cross-component PR merges.
2. **Read the four component specs in this order**:
    1. `03-correction-loop.md` — canonical data model (Postgres DDL + JSON shapes) referenced by every other spec.
    2. `02-execution-plane.md` — workflow + activity contracts that drive the runtime.
    3. `04-operator-experience.md` — gateway-side contracts.
    4. `01-control-plane.md` — service boundaries, auth, observability, tenant lifecycle.
3. **`05-architecture-integration-critique.md` is the integration test plan.** Its §5 (R5 sign-off) lists the 6 gating tests; its §1–§5 of Round 5 cover per-spec scoring, contract test inventory union, OPEN classification, scenario testability, and signed verdict.
4. **Sections beginning with `RESOLVED-X:`** are binding cross-component consensus. The gating question for the original conflict is in this overview's §7 or in the relevant per-round audit in `05`.
5. **Sections beginning with `OPEN-PRODUCT-INPUT-X:` / `OPEN-MEASUREMENT-X:` / `OPEN-OUT-OF-SCOPE-X:`** are explicitly non-blocking. Do not encode their resolution in code without the gating decision.

---

*Last refreshed by moderator after Round 5 completion. Verdict per `05-architecture-integration-critique.md` §5: **SHIPPABLE.** The team is ready for development handoff.*
