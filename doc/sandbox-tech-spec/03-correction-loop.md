# 03 — Correction Loop & Learning System

**Victoria Tech Spec · Round 5 Revision (final consolidation round)**
**Domain:** Correction Loop & Learning System Architect
**Model:** Claude Sonnet 4.6
**Date:** 2026-04-27 (Round 5)

---

## Round 5 Conflict Resolution Index

Cumulative status across Rounds 1–5. Round 5 changes are marked **(R5)**. **No new architectural decisions in R5** — consolidation, naming alignment, semantics formalization, and handoff documentation only.

| ID | Topic | Status | Where resolved |
|---|---|---|---|
| **C1** | Where ValidatedRules and RuleCandidates physically live | **RESOLVED-C1** (R3 lock — DA dissent noted) | §2.0, §2.8 |
| **C2** | SkillVersion vs. system-prompt context block (one name, one schema) | **RESOLVED-C2** | §9 |
| **C5** | Replay determinism: snapshot binding and diff scope | **RESOLVED-C5** | §10 |
| **C8** | AuditEvent immutability at the storage layer | **RESOLVED-C8** | §11.5 |
| **C9** | Audit store topology — local buffer + control-plane authoritative | **RESOLVED-C9** (R3 reconciled with exec-plane local-read) | §11.0 |
| **C10** | Who writes the `corrections` row and `correction_received` event | **RESOLVED-C10** | §3.5, §11.6 |
| **C11** | Rule fetch model: pull at workflow-start vs. push from control plane | **RESOLVED-C11** | §9.5 |
| **C12** (R3) | ctrl-plane adopts unified `validated_rules` DDL + RLS | **RESOLVED-C12** | §2.8.1, §2.8.2 |
| **C13** (R3) | `ApprovalSignalEnvelope` payload completeness for `PersistCorrection` | **RESOLVED-C13** — published required-field list, awaiting operator-ux confirmation | §3.8 |
| **C14** (R3) | Field-name alignment in SkillVersion ↔ Hermes prompt block | **RESOLVED-C14** (concession: exec-plane projects locally; manifest names hold) | §9.3 |
| **AUDIT-CANON** (R3) | Canonical `audit_events` schema, event_type registry, writer registry | **RESOLVED-AUDIT-CANON** | §11.2, §11.3, §11.6 |
| **MCP-SNAPSHOT** (R3) | `mcp_read_snapshot` artifact_type + `mcp_audit_reader` role | **RESOLVED-MCP-SNAPSHOT** | §2.5, §11.7 |
| **N1** (R3) | Sandbox-mode canonical encoding | **RESOLVED-N1** as `mode: 'sandbox' \| 'live'` enum | §2.0c, §11.3 |
| **N3** (R3) | `vertical_candidate_aggregates` PII-strip classifier rules | **RESOLVED-N3** with concrete value classifier + contract test | §2.7.2, §13.5 |
| **N6** (R3) | Idempotency-key composition rule | **OPEN-N6** — inventory published; composition rule deferred to ctrl-plane | §3.9 |
| **OQ-Parse** | Parsing pipeline ownership | **RESOLVED** as 2-stage split | §3.6 |
| **CorrShape** | Operator UX `Correction` shape | **ACCEPTED with mappings** | §3 |
| INV-05 | Cross-tenant vertical promotion strips tenant identifiers | **RESOLVED** via aggregate view + classifier (§2.7.2) | §2.7, §13.5 |
| **AUDIT-TOPOLOGY-R4** (R4) | Single-store audit + per-tenant outbox; MCP reads control-plane via mTLS | **RESOLVED-AUDIT-TOPOLOGY-R4** | §2.0.1, §11.0 |
| **AUDIT-CANON gateway expansion** (R4) | All 14 operator-ux gateway event_types added | **RESOLVED** | §11.3, §11.6 |
| **STORAGE-TOPOLOGY** (R4) | Canonical storage-topology table | **RESOLVED-STORAGE-TOPOLOGY-R4** | §2.0 |
| **CONTRACT-TESTS** (R4) | 13 cross-component contract tests at every boundary this spec owns | **RESOLVED** | §13.0 |
| **E2E-SCENARIOS** (R4) | All 8 SC-01..SC-08 fully testable | **RESOLVED** | §13.1.1 |
| **OPEN-AUDIT-CANON-CONFORMANCE-R4** (R5) | All 7 op-ux pushed event_types present in §11.3; channel-agnostic names adopted (`channel_session_*`, `security_violation_inbound`, `correction_resolved_at_gateway`) | **RESOLVED-CLOSED-R5** | §11.3, §11.6 |
| **OUTBOX-SEMANTICS-R5** (R5) | At-least-once drain, dedup on `idempotency_key`, durability boundary, drain-worker sole reader, control-plane outage handling | **RESOLVED-OUTBOX-SEMANTICS-R5** | §11.0 (R5 formalization) |
| **HANDOFF** (R5) | Implementation handoff section: migration order, integration points, owned fixtures, pre-commit invariants | **RESOLVED-HANDOFF-R5** | §16 |

---

## Table of Contents

1. [Purpose & Scope](#1-purpose--scope)
2. [Postgres Schema](#2-postgres-schema)
3. [Correction Shape (Operator UX Contract)](#3-correction-shape-operator-ux-contract)
4. [Candidate-Match Algorithm](#4-candidate-match-algorithm)
5. [Confidence-Scoring Model](#5-confidence-scoring-model)
6. [Scope Escalation](#6-scope-escalation)
7. [Promotion Flow](#7-promotion-flow)
8. [ValidatedRule Versioning](#8-validatedrule-versioning)
9. [SkillVersion Design](#9-skillversion-design)
10. [Replay Semantics](#10-replay-semantics)
11. [AuditEvent Model](#11-auditevent-model)
12. [Failure Modes](#12-failure-modes)
13. [Test Strategy (TDD)](#13-test-strategy-tdd)
14. [Decisions & Rationale](#14-decisions--rationale)
15. [Open Questions & Conflicts](#15-open-questions--conflicts--round-5-status-final)
16. [Implementation Handoff](#16-implementation-handoff)

---

## 1. Purpose & Scope

### What this document owns

This spec defines the **data model, algorithms, and contracts** for the correction loop — the path from a raw operator correction through to a validated rule that Hermes applies on future runs. Specifically:

- Full Postgres DDL (as sketch) for all ten core entities
- The structured `Correction` shape that the Operator UX layer must emit
- The candidate-match algorithm (how new corrections find or create candidates)
- The confidence-scoring formula and threshold reasoning
- Scope escalation logic (case → tenant → vertical → default)
- Promotion flow data shapes (what the Rule Review Console receives and returns)
- ValidatedRule versioning, supersession, and rollback semantics
- SkillVersion design: how validated rules are surfaced to Hermes
- Replay semantics: what "re-run against a new rule set" means
- AuditEvent model: schema, immutability, writer/reader contract
- Failure mode analysis and mitigations
- TDD strategy: invariants and test cases for every algorithm

### What this document does NOT own

| Out of scope | Owner (assumption) |
|---|---|
| Service code, REST/GraphQL API design, auth | Control Plane Architect |
| Rule Review Console UI | Control Plane Architect |
| Hermes runtime configuration, how it invokes rules | Execution Plane Architect |
| Temporal workflow definitions | Execution Plane Architect |
| WhatsApp gateway, reply parsing, review packet rendering | Operator UX Architect |
| Cross-component contract tests | Devil's Advocate |

This document defines **data shapes** that peer architects build to. The control plane reads candidate/rule views; this spec defines those views. The execution plane calls a `get_applicable_rules` function; this spec defines what it returns.

### Boundary: data model vs. services

```
[Operator UX]                [Control Plane]
     |                            |
     | Correction{...}            | promote/reject command
     v                            v
[THIS SPEC: parsing, matching, confidence, versioning, replay, audit]
     |                            ^
     | get_applicable_rules(...)  | RuleCandidate read view
     v                            |
[Execution Plane: Hermes]    [Rule Review Console]
```

---

## 2. Postgres Schema

### 2.0 Storage Topology — Canonical Table (R4 lock; ctrl-plane and exec-plane reference)

**This is the artifact Devil's Advocate has been demanding for three rounds.** Every entity in the data model with location, writer, readers, RLS policy, retention, and PII flag. Ctrl-plane R3 §13 references this section as canonical. Exec-plane R3 §13.4 references this section as canonical. Operator-ux R3 §9.7 references this section as canonical.

| Entity / Table | Database | Writer (single) | Readers | RLS / Access policy | Retention | PII | DDL ref |
|---|---|---|---|---|---|---|---|
| `tenants` | Control plane | Ctrl-plane provisioning workflow | Ctrl-plane services; reviewer console | Reviewer-RBAC at app layer | Tenant lifetime + 90d | None | §2.1 |
| `workflow_templates` | Control plane | Ctrl-plane (admin) | All services | Reviewer-RBAC; readable by all execution-plane services | Indefinite | None | §2.2 |
| `case_runs` | Per-tenant exec | Exec-plane Temporal worker | Exec-plane; ctrl-plane query API (federated) | Per-tenant DB connection scoping | Tenant lifetime + 90d | Operator submitted data (`input_payload`); flag at column level | §2.4 |
| `decision_points` | Per-tenant exec | Exec-plane Temporal worker | Exec-plane; candidate-matcher | Per-tenant DB connection scoping | Tied to `case_runs` | None directly; references operator data via case_run | §2.4 |
| `artifacts` | Per-tenant exec | Exec-plane MCP servers; replay diff job | Exec-plane; operator-ux gateway (preview URL) | Per-tenant DB connection scoping | Tied to `case_runs` | Operator customer data (drafts, attachments) | §2.5 |
| `corrections` | **Per-tenant exec** | **Exec-plane `PersistCorrection` activity (sole writer; RESOLVED-C10)** | Candidate-matcher (Stage B) | Per-tenant DB connection scoping | 7 years (then archive) | Operator free text — DEFEND boundary | §2.6 |
| `rule_candidates` | **Per-tenant exec** | Candidate-matcher | Candidate-matcher; aggregation job (read-only over per-tenant connection) | Per-tenant DB connection scoping | 7 years | Inherits PII via `parsed_conditions` derived from operator free text — DEFEND boundary | §2.7 |
| `validated_rules` (all 4 scopes) | **Control plane** | Promotion pipeline (control-plane worker; RESOLVED-C12) | Exec-plane `LoadSkillVersion`; reviewer console | RLS policies `vr_read_isolation` / `vr_write_isolation` / `vr_update_isolation` (§2.8.2) | Indefinite (versioned, never deleted) | None at vertical/default; tenant-scope rows isolated by RLS | §2.8 |
| `skill_versions` | Control plane | Promotion pipeline | Exec-plane `LoadSkillVersion` | RLS by `tenant_id` (where set); shared rows visible per workflow_slug | Indefinite | None | §2.9 |
| **`audit_events` (authoritative)** | **Control plane** | All registered writers via `audit_writer` role (registry §11.6); exec-plane drains its outbox here (§11.0) | Ctrl-plane query API (`audit_reader`); MCP servers via `mcp_audit_reader` role over mTLS (RESOLVED-AUDIT-TOPOLOGY-R4) | RLS by `tenant_id`; `mcp_audit_reader` further restricted to `event_type='approval_received'` for own tenant | 13mo hot + 7yr WORM archive | Operator messaging IDs in `actor_id` | §2.10, §11 |
| `audit_events_outbox` (durable buffer; rename of R3 "local table") | Per-tenant exec | Exec-plane Temporal worker / MCP servers | Drain worker (writes to control plane and marks drained) | Per-tenant DB connection scoping | 24h post-drain (then deleted) | Operator messaging IDs (transient) | §11.0, ctrl-plane §9.4 |
| `vertical_candidate_aggregates` | Control plane | Aggregation job (control-plane scheduled worker) | Reviewer console | Reviewer-only at app layer; PII strip enforced at write (classifier §2.7.2) | Recomputed; rows replaced on each refresh | None (PII strip invariant) | §2.7 |
| `quarantined_aggregates` | Control plane | Aggregation job | Reviewer console | Reviewer-only | Recomputed | None (redacted-only) | §2.7.2 |

**Two notes on this table:**

1. **The R3 audit_events local table is renamed `audit_events_outbox`** to make its non-authoritative role unambiguous (R4 RESOLVED-AUDIT-TOPOLOGY-R4 below).
2. **DEFEND boundary** marks tables this spec defends as per-tenant against any future centralization argument. The R3 lock on `validated_rules` is operational, not architectural; the per-tenant location of `corrections` and `rule_candidates` is architectural and not negotiable.

### 2.0.1 RESOLVED-AUDIT-TOPOLOGY-R4 — single authoritative store, mTLS read for MCP

**Conflict in R3 (residual).** This spec R3 §11.0 placed MCP synchronous reads against a **per-tenant local `audit_events`** table (write-through cache). Ctrl-plane R3 §9.3 placed MCP reads against the **single control-plane `audit_events`** table directly over mTLS, with `mcp_audit_reader` role + RLS at the control plane. Exec-plane R3 §5.6 followed this spec's R3 position.

**R4 resolution: ctrl-plane's design wins.** This spec collapses the dual-read topology to a **single authoritative store** in the control-plane DB, with MCP reads going there over mTLS. Reasons:

1. **Single source of truth.** A `approval_received` event that is locally committed but not yet drained is invisible to a sister-tenant MCP only in a contrived setup; collapsing to one store eliminates the entire class of "buffer-staleness" failure modes by construction.
2. **The buffer keeps its outage-tolerance role.** Renamed `audit_events_outbox` per §2.0. Writes still go local-first (transactionally with the business write — e.g., `PersistCorrection` writes corrections + outbox row in one transaction) and drain upstream. Reads no longer hit the outbox. Two specs already adopted ctrl-plane's role-grant + RLS notation; aligning to it costs less revision.
3. **Latency tradeoff explicit.** Intra-VPC mTLS Postgres connection adds ~5–10ms vs. local read. Acceptable on the WRITE_EXTERNAL hot path because that path already takes ~100s of ms (LLM call → tool call → preflight → write). Surfaced as `OPEN-AUDIT-LATENCY-R4` for measurement post-MVP.
4. **MCP role grant lives in control-plane DB only** (per ctrl-plane §9.3). The `mcp_approval_events` view in this spec §11.5 is updated to live in the control plane (R4 below).

**What the local outbox still does:** transactional buffer for any writer in the execution plane (Temporal worker, MCP servers); drain protocol per ctrl-plane §9.4. The contract test for "no-loss across control-plane outage" (`INGEST_04` in ctrl-plane §9.4) still proves the outbox role.

**Devil's Advocate dissent on `validated_rules` placement preserved.** DA Round 2 §C audit (line 762) ranked: split (best) > hybrid > unified (wrong). The R3 lock on unified `validated_rules` is operational, not architectural. **Gating question for revert — `OPEN-DA-DISSENT-C1` failure-test:**

> If post-MVP forensic reveals any of:
> 1. A successful cross-tenant `SELECT * FROM validated_rules WHERE tenant_id = 't_other'` returning rows under expected RLS settings (i.e., RLS bypass via privileged role escalation in the application layer);
> 2. A Postgres CVE that demonstrably bypasses `current_setting('app.current_tenant')` RLS at the storage layer;
> 3. Audit-log evidence that a control-plane operator with `app.bypass_tenant_check = true` accessed tenant-scope rows for a tenant not in their assignment;
> 
> then `validated_rules` at scope `case`/`tenant` reverts to per-tenant DB (R1 split layout). Vertical/default rules remain in the control-plane DB. The forensic test runs against the integration-test environment quarterly post-MVP and is owned by Devil's Advocate.

### 2.0a Effective rule-set resolver (used by exec-plane LoadSkillVersion)

When exec-plane's `LoadSkillVersion` activity runs (per exec-plane §8.2), the control plane resolver returns the effective rule set in scope-precedence order with RLS applied per the connecting service identity:

```sql
-- Connection sets app.current_tenant for the calling tenant; RLS policy in §2.8.1
-- restricts case/tenant rows to the current tenant; vertical/default are visible to all.
SELECT vr.*
FROM validated_rules vr
WHERE vr.workflow_template_id = $workflow_template_id
  AND vr.status = 'active'
  AND vr.decision_type = ANY ($decision_types)
  AND (
    (vr.scope = 'case'     AND vr.case_run_id = $case_run_id)
    OR (vr.scope = 'tenant'   AND vr.tenant_id   = $tenant_id)
    OR (vr.scope = 'vertical' AND vr.vertical    = $vertical)
    OR (vr.scope = 'default')
  )
ORDER BY
  CASE vr.scope WHEN 'case' THEN 4 WHEN 'tenant' THEN 3 WHEN 'vertical' THEN 2 ELSE 1 END DESC,
  vr.promoted_at DESC;
```

The result is wrapped into a `SkillVersion` manifest (§9.1). The single resolver query lives in one DB; no cross-DB merge is needed. Tiebreaker for same-scope conflicts: `promoted_at DESC` (matches ctrl-plane §13.2).

### 2.0b Cross-tenant vertical promotion path (INV-05 compliance)

When a reviewer in the Rule Review Console requests a vertical-scope promotion, the control plane runs an aggregation that **never** copies tenant_id, source_correction_ids, source_case_run_ids, or unstripped operator-supplied condition values into the result.

The PII-strip rules and contract test are in §2.7.2 below. The aggregate is materialized into `vertical_candidate_aggregates`; rows that fail strip classification go to `quarantined_aggregates` for reviewer normalization. The reviewer never sees per-tenant detail for vertical/default promotion decisions.

### 2.0c Sandbox-mode canonical encoding — RESOLVED-N1 (R3)

**Single canonical type:** `mode TEXT NOT NULL CHECK (mode IN ('sandbox', 'live'))` — a 2-value enum.

The product spec's "approval-gated shadow" and "partial autopilot" progression states are decomposed into two orthogonal axes; only `mode` (sandbox vs. live) is encoded at this layer. The autonomy axis (`approval_required` vs. `autopilot`) is a separate concern, deferred until Phase 2. This avoids overloading a single field that exec-plane's `sandbox_mode` boolean and operator-ux's `run_mode` string both assumed was binary.

| Surface | Field | Type |
|---|---|---|
| `case_runs` table (this spec §2.4) | `mode` | enum `'sandbox' \| 'live'` |
| Workflow input (exec-plane §4.4) | `mode` | enum `'sandbox' \| 'live'` |
| `audit_events.payload.mode` (this spec §11.2) | `mode` (when relevant) | enum |
| `skill_versions.metadata.mode` (this spec §9) | `mode` | enum (Phase 1 always `'sandbox'`) |
| Tenant manifest (ctrl-plane §5.6) | `mode` | enum |
| `tenants.default_mode` | `default_mode` | enum (R3 added; see §2.1 `tenants` DDL) |

Default-deny rule (consistent with exec-plane INV-T5): if `mode` is absent or NULL on any of these surfaces, treat it as `'sandbox'`. Never default to `'live'`.

This unification is the resolution of **N1** from devil's advocate Round 2 audit (line 780). Peer specs that currently use a boolean (exec-plane §4.4, ctrl-plane §10.2) will be updated to the enum in their R3 revision.

---

### 2.1 `tenants` (control plane DB)

```sql
CREATE TABLE tenants (
  id                    TEXT        PRIMARY KEY,            -- e.g. "t_abc123"
  name                  TEXT        NOT NULL,
  vertical              TEXT        NOT NULL,               -- e.g. "roofing", "plumbing"
  status                TEXT        NOT NULL                -- "onboarding" | "active" | "suspended"
                          CHECK (status IN ('onboarding', 'active', 'suspended')),
  default_mode          TEXT        NOT NULL DEFAULT 'sandbox'  -- RESOLVED-N1 (R3); applied to new case_runs
                          CHECK (default_mode IN ('sandbox', 'live')),
  onboarding_completed_at TIMESTAMPTZ,
  feature_flags         JSONB       NOT NULL DEFAULT '{}',
  -- Configurable thresholds (override system defaults per tenant)
  conf_under_review_threshold   NUMERIC(4,3) DEFAULT NULL,  -- NULL means use system default
  conf_auto_promote_threshold   NUMERIC(4,3) DEFAULT NULL,
  conf_min_evidence_count       INT          DEFAULT NULL,
  conf_candidate_ttl_days       INT          DEFAULT NULL,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tenants_vertical_idx ON tenants (vertical);
CREATE INDEX tenants_status_idx ON tenants (status);
```

**Notes:**
- `vertical` drives which default `WorkflowTemplate` rows are inherited.
- `conf_*` columns allow per-tenant threshold overrides (see §5 for system defaults and justification).
- No PII stored directly. Operator identity is in the messaging layer.

---

### 2.2 `workflow_templates` (control plane DB)

```sql
CREATE TABLE workflow_templates (
  id                  TEXT        PRIMARY KEY,              -- e.g. "wt_quote_roofing"
  slug                TEXT        NOT NULL UNIQUE,          -- e.g. "quote_drafting"
  vertical            TEXT,                                 -- NULL = all verticals
  display_name        TEXT        NOT NULL,
  description         TEXT,
  version             INT         NOT NULL DEFAULT 1,
  decision_types      JSONB       NOT NULL DEFAULT '[]',    -- known decision_type values
  default_skill_version_id TEXT   REFERENCES skill_versions(id) ON DELETE SET NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  deprecated_at       TIMESTAMPTZ
);

CREATE INDEX wf_templates_vertical_idx ON workflow_templates (vertical);
CREATE INDEX wf_templates_slug_idx ON workflow_templates (slug);
```

**Notes:**
- `decision_types` is an enumerated list of valid `decision_type` values for this workflow (e.g., `["send_or_hold", "template_selection", "tax_treatment"]`). Corrections that reference an unknown `decision_type` are flagged for review rather than silently creating candidates.

---

### 2.3 `case_runs` (per-tenant execution DB)

```sql
CREATE TABLE case_runs (
  id                  TEXT        PRIMARY KEY,              -- e.g. "cr_xyz789"
  tenant_id           TEXT        NOT NULL,
  workflow_template_id TEXT       NOT NULL,
  workflow_slug       TEXT        NOT NULL,
  status              TEXT        NOT NULL
                        CHECK (status IN ('running', 'awaiting_review', 'approved',
                                          'corrected', 'replayed', 'failed', 'cancelled')),
  mode                TEXT        NOT NULL DEFAULT 'sandbox'
                        CHECK (mode IN ('sandbox', 'live')),  -- RESOLVED-N1: 2-value enum (R3)
                                                              -- "shadow"/"autopilot" decompose to (mode='live', autonomy=...)
                                                              -- which is a separate Phase 2 axis.
  input_hash          TEXT,                                 -- SHA-256 of normalized input; used for replay dedup
  input_payload       JSONB       NOT NULL,                 -- Full input; NOT displayed without masking
  skill_version_id    TEXT,                                 -- SkillVersion active at execution time
  started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at        TIMESTAMPTZ,
  replayed_from_id    TEXT        REFERENCES case_runs(id) ON DELETE SET NULL
);
-- PII flag: input_payload may contain operator-submitted data. Treat as sensitive.

CREATE INDEX case_runs_tenant_idx ON case_runs (tenant_id);
CREATE INDEX case_runs_workflow_idx ON case_runs (workflow_template_id);
CREATE INDEX case_runs_status_idx ON case_runs (status);
CREATE INDEX case_runs_started_at_idx ON case_runs (started_at DESC);
-- Partitioning hint: partition by tenant_id range or hash for multi-tenant deployments
-- with high volume. Use range partitioning on started_at for archival.
```

**Retention:** case_runs are retained for the lifetime of the tenant plus a 90-day grace period post-offboarding. `input_payload` should be encrypted at rest; consider column-level encryption for fields flagged as PII.

---

### 2.4 `decision_points` (per-tenant execution DB)

```sql
CREATE TABLE decision_points (
  id                  TEXT        PRIMARY KEY,              -- e.g. "dp_a1b2"
  case_run_id         TEXT        NOT NULL REFERENCES case_runs(id) ON DELETE CASCADE,
  tenant_id           TEXT        NOT NULL,
  decision_type       TEXT        NOT NULL,                 -- e.g. "send_or_hold"
  sequence_number     INT         NOT NULL,                 -- ordinal within the case run
  agent_input         JSONB       NOT NULL,                 -- facts available at decision time
  agent_output        JSONB       NOT NULL,                 -- action proposed / branch taken
  applied_rule_ids    TEXT[]      NOT NULL DEFAULT '{}',    -- ValidatedRule IDs active at this point
  outcome             TEXT        CHECK (outcome IN ('approved', 'corrected', 'skipped', 'pending')),
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX dp_case_run_idx ON decision_points (case_run_id);
CREATE INDEX dp_tenant_type_idx ON decision_points (tenant_id, decision_type);
CREATE INDEX dp_created_at_idx ON decision_points (created_at DESC);
```

**Notes:**
- `agent_input` captures the structured facts (not raw text) available to Hermes at that step. This is the input that Correction parsing uses to extract conditions.
- `applied_rule_ids` is an array snapshot — it records which rules Hermes was given, enabling exact replay audits ("was this rule in scope when the mistake happened?").

---

### 2.5 `artifacts` (per-tenant execution DB)

```sql
CREATE TABLE artifacts (
  id                  TEXT        PRIMARY KEY,              -- e.g. "art_q77"
  tenant_id           TEXT        NOT NULL,
  case_run_id         TEXT        NOT NULL REFERENCES case_runs(id) ON DELETE CASCADE,
  decision_point_id   TEXT        REFERENCES decision_points(id) ON DELETE SET NULL,
  artifact_type       TEXT        NOT NULL
                        CHECK (artifact_type IN ('email_draft', 'pdf_proposal', 'invoice_draft',
                                                  'extracted_facts', 'summary_card', 'replay_diff',
                                                  'mcp_read_snapshot')),  -- RESOLVED-MCP-SNAPSHOT (R3)
  mcp_tool_name       TEXT,                                 -- populated only for artifact_type='mcp_read_snapshot'
  mcp_idempotency_key TEXT,                                 -- populated only for mcp_read_snapshot; matches MCP idempotency_log
  case_replay_role    TEXT        CHECK (case_replay_role IN ('original', 'replay', NULL)),  -- replay-tagging
  storage_uri         TEXT        NOT NULL,                 -- s3:// or r2:// path
  content_hash        TEXT        NOT NULL,                 -- SHA-256 of object bytes
  mime_type           TEXT        NOT NULL,
  size_bytes          BIGINT,
  preview_uri         TEXT,                                 -- signed URL (short TTL, generated on demand)
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at          TIMESTAMPTZ                           -- for sandbox ephemeral artifacts
);
-- PII flag: may contain operator customer data. Storage bucket must be tenant-isolated.

CREATE INDEX artifacts_case_run_idx ON artifacts (case_run_id);
CREATE INDEX artifacts_tenant_idx ON artifacts (tenant_id);
```

**Notes:**
- `content_hash` enables deduplication and replay equivalence checks (same hash = artifact unchanged by new rule set).
- `preview_uri` is NOT persisted permanently; generated on demand with a short TTL.

**`mcp_read_snapshot` artifact_type — RESOLVED-MCP-SNAPSHOT (R3).** Per exec-plane §4.3 commitment 3, MCP read-tool outputs (e.g., `list_inbox_threads`, `read_file_content`) are recorded on the **original** case run as artifacts so that **replay** runs can deterministically retrieve the same response without re-executing against live data. Storage layout:

- `artifact_type = 'mcp_read_snapshot'`
- `mcp_tool_name`: e.g. `'sandbox_email.list_inbox_threads'`
- `mcp_idempotency_key`: matches the MCP server's idempotency-log key for the original call (`{case_run_id}:{decision_point_id}:{tool_name}`)
- `storage_uri`: object-store path under `cases/{case_run_id}/snapshots/{mcp_idempotency_key}.json`
- `case_replay_role = 'original'` on the original case; replay reads with `case_replay_role = 'original'` filter so a replay's own snapshots are not consulted.

The exec-plane is the writer (the MCP server's idempotency-log hook records snapshots on first successful read in an `original` case run). On replay, exec-plane consults the snapshot artifact instead of re-executing the read MCP call. This eliminates time-of-call non-determinism (e.g., inbox state changes between original and replay).

---

### 2.6 `corrections` (per-tenant execution DB)

```sql
CREATE TABLE corrections (
  id                  TEXT        PRIMARY KEY,              -- e.g. "corr_789"
  idempotency_key     TEXT        NOT NULL UNIQUE,          -- caller-generated; prevents duplicate submission
  tenant_id           TEXT        NOT NULL,
  case_run_id         TEXT        NOT NULL REFERENCES case_runs(id) ON DELETE RESTRICT,
  decision_point_id   TEXT        NOT NULL REFERENCES decision_points(id) ON DELETE RESTRICT,
  operator_id         TEXT        NOT NULL,                 -- messaging-layer user identifier (NOT email)
  action_button       TEXT        NOT NULL
                        CHECK (action_button IN ('approve', 'wrong_facts', 'wrong_action',
                                                  'missing_condition', 'use_different_template', 'add_note')),
  free_text           TEXT,                                 -- operator natural language; MAY be NULL
  follow_up_answer    TEXT,                                 -- concatenated answers to follow-up questions
  follow_up_answers_json JSONB,                             -- structured: operator-ux's follow_up_answers array
  scope_hint          TEXT        CHECK (scope_hint IN ('case', 'tenant', NULL)),
                                                            -- mapped from operator-ux: "always"→tenant, "this_case"→case, null→null
  parse_method        TEXT        CHECK (parse_method IN ('button', 'button_fallback', 'text_match', 'llm', NULL)),
  condition_hints     JSONB,                                -- Stage A output from operator-ux (raw)
  parsed_conditions   JSONB,                                -- Stage B canonical output (this spec)
  parsed_action       TEXT,                                 -- recommended_action extracted by parsing pipeline
  parse_confidence    NUMERIC(4,3),                         -- pipeline confidence in parse result (from operator-ux)
  parse_status        TEXT        NOT NULL DEFAULT 'pending'
                        CHECK (parse_status IN ('pending', 'parsed', 'parse_failed', 'manual_review')),
  resulting_candidate_id TEXT,                              -- FK set after candidate match/create
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- PII flag: free_text, follow_up_answer contain operator natural language. Handle accordingly.

CREATE INDEX corrections_tenant_idx ON corrections (tenant_id);
CREATE INDEX corrections_case_run_idx ON corrections (case_run_id);
CREATE INDEX corrections_dp_idx ON corrections (decision_point_id);
CREATE INDEX corrections_idempotency_idx ON corrections (idempotency_key);
CREATE INDEX corrections_parse_status_idx ON corrections (parse_status) WHERE parse_status != 'parsed';
```

**Notes:**
- `idempotency_key` is critical: the WhatsApp layer may deliver messages more than once; retrying must not create duplicate candidates.
- `resulting_candidate_id` is set by the parsing pipeline after it determines whether this correction merges into an existing candidate or creates a new one.

---

### 2.7 `rule_candidates` (per-tenant execution DB; stripped replica in control plane)

```sql
CREATE TABLE rule_candidates (
  id                  TEXT        PRIMARY KEY,              -- e.g. "rc_a91f"
  tenant_id           TEXT        NOT NULL,
  workflow_template_id TEXT       NOT NULL,
  workflow_slug       TEXT        NOT NULL,
  decision_type       TEXT        NOT NULL,
  -- Canonical condition representation (see §4 for normalization rules)
  conditions_canonical JSONB      NOT NULL,                 -- sorted, normalized condition array
  conditions_hash     TEXT        NOT NULL,                 -- SHA-256 of canonicalized JSON; used for exact match
  recommended_action  TEXT        NOT NULL,
  scope               TEXT        NOT NULL DEFAULT 'case'
                        CHECK (scope IN ('case', 'tenant', 'vertical', 'default')),
  status              TEXT        NOT NULL DEFAULT 'candidate'
                        CHECK (status IN ('candidate', 'under_review', 'merged', 'rejected', 'promoted')),
  confidence          NUMERIC(4,3) NOT NULL DEFAULT 0.0,
  evidence_count      INT         NOT NULL DEFAULT 0,
  contradicting_count INT         NOT NULL DEFAULT 0,
  source_correction_ids TEXT[]    NOT NULL DEFAULT '{}',
  source_case_run_ids   TEXT[]    NOT NULL DEFAULT '{}',
  conflicts_with        TEXT[]    NOT NULL DEFAULT '{}',    -- IDs of conflicting rule_candidates
  merged_into_id      TEXT        REFERENCES rule_candidates(id) ON DELETE SET NULL,
  promoted_to_rule_id TEXT,                                 -- set after promotion
  stale_at            TIMESTAMPTZ,                          -- set if evidence_count = 1 and no activity for TTL
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  under_review_at     TIMESTAMPTZ,
  resolved_at         TIMESTAMPTZ
);

CREATE INDEX rc_tenant_workflow_idx ON rule_candidates (tenant_id, workflow_slug, decision_type);
CREATE INDEX rc_conditions_hash_idx ON rule_candidates (tenant_id, conditions_hash);
CREATE INDEX rc_status_idx ON rule_candidates (status);
CREATE INDEX rc_confidence_idx ON rule_candidates (confidence DESC) WHERE status = 'candidate';
```

**Single-tenant reviewer access:** When a reviewer drills into one tenant's candidates, the Rule Review Console connects to that tenant's per-tenant DB through a control-plane proxy. There is no central replica of per-tenant candidates. Free text never crosses tenant boundaries.

**Cross-tenant aggregation for vertical promotion** (`vertical_candidate_aggregates` in control plane DB):

```sql
-- Materialized at promotion-request time only (not continuously replicated).
-- Computed by a control-plane job that connects to each per-tenant DB,
-- aggregates per (workflow_slug, decision_type, conditions_hash, recommended_action),
-- and writes ONLY the stripped projection below. No tenant_id, no correction_id,
-- no case_run_id, no free text. INV-05 contract test in §13.4 enforces this.
CREATE TABLE vertical_candidate_aggregates (
  id                  TEXT        PRIMARY KEY,                -- e.g. "vca_b88e"
  vertical            TEXT        NOT NULL,
  workflow_slug       TEXT        NOT NULL,
  decision_type       TEXT        NOT NULL,
  conditions_canonical JSONB      NOT NULL,
  conditions_hash     TEXT        NOT NULL,
  recommended_action  TEXT        NOT NULL,
  -- Stripped cross-tenant counts ONLY:
  distinct_tenant_count   INT     NOT NULL,
  total_evidence_count    INT     NOT NULL,
  total_contradicting_count INT   NOT NULL,
  earliest_seen_at        TIMESTAMPTZ NOT NULL,
  latest_seen_at          TIMESTAMPTZ NOT NULL,
  computed_at         TIMESTAMPTZ NOT NULL DEFAULT now()
  -- DELIBERATELY ABSENT: tenant_id, source_correction_ids, source_case_run_ids,
  -- contributing_tenant_ids, free_text, parsed_conditions of corrections.
);

CREATE INDEX vca_vertical_workflow_idx ON vertical_candidate_aggregates (vertical, workflow_slug, decision_type);
CREATE INDEX vca_count_idx ON vertical_candidate_aggregates (distinct_tenant_count DESC, total_evidence_count DESC);
```

**Aggregation algorithm** (control plane job, runs on reviewer request for "show me vertical candidates"):

```
For each tenant T in vertical V:
  Open per-tenant connection
  SELECT workflow_slug, decision_type, conditions_hash, conditions_canonical,
         recommended_action, evidence_count, contradicting_count, created_at, last_seen_at
  FROM rule_candidates
  WHERE status IN ('candidate', 'under_review')
    AND evidence_count >= min_evidence_for_vertical_consideration   -- e.g. 2
  Close connection (do NOT store T's data)

In control-plane memory (per (workflow_slug, decision_type, conditions_hash, recommended_action) tuple):
  distinct_tenant_count    = COUNT(DISTINCT contributing T)
  total_evidence_count     = SUM(evidence_count)
  total_contradicting_count = SUM(contradicting_count)
  earliest_seen_at         = MIN(created_at)
  latest_seen_at           = MAX(last_seen_at)

INSERT INTO vertical_candidate_aggregates (...)  -- with no tenant_id ever written
```

The list of contributing tenants is held in the job's process memory only for the duration of the aggregation and is never persisted. The reviewer sees only the aggregate row.

#### 2.7.2 RESOLVED-N3: PII-strip classifier for `conditions_canonical` and `recommended_action`

Devil's Advocate UE-LL-1 (§C, line 712) and N3 (§B, line 796) raised a real gap: a `rule_candidate.conditions_canonical` array can contain operator-supplied **values** like `{field: "client_name", operator: "=", value: "ABC Realty"}`. Stripping `tenant_id` and `source_correction_ids` is not enough — the **value field itself** can carry tenant-identifying content.

**Classifier rules.** Before a tuple is added to `vertical_candidate_aggregates`, every `value` in `conditions_canonical` is classified:

| Value class | Detection | Action |
|---|---|---|
| Boolean | JSON type `boolean` | **PASS** unchanged |
| Number | JSON type `number` (int or float) | **PASS** unchanged |
| Null | JSON `null` | **PASS** unchanged |
| Enum (workflow-template-defined) | String matches a value listed in `workflow_templates.condition_value_enums[field_name]` for the corresponding `field` | **PASS** unchanged (e.g., `client_type IN {'new', 'repeat'}`, `tax_treatment IN {'gst', 'no_gst'}`) |
| Email | String matches RFC 5322 simple email regex | **REDACT to type token** `"<email>"` |
| Phone | String matches E.164 or common national patterns | **REDACT** `"<phone>"` |
| URL | String matches `https?://...` | **REDACT** `"<url>"` |
| Date/timestamp | ISO 8601 | **REDACT** to `"<date>"` (granularity preserved if needed by reviewer) |
| Free-text string (no class above) | Anything else | **QUARANTINE** — do NOT publish to `vertical_candidate_aggregates`; insert into `quarantined_aggregates` for reviewer normalization |

The `recommended_action` field is treated as an enum: it must match a value in `workflow_templates.allowed_actions[decision_type]` to PASS. Otherwise it is QUARANTINEd.

**The `workflow_templates` schema gains an enum-allowlist column** to support the classifier:

```sql
ALTER TABLE workflow_templates
  ADD COLUMN condition_value_enums JSONB NOT NULL DEFAULT '{}',
  -- e.g. {"client_type": ["new", "repeat", "commercial"], "supplier_country": ["AU","SG","NZ"]}
  ADD COLUMN allowed_actions JSONB NOT NULL DEFAULT '{}';
  -- e.g. {"send_or_hold": ["send_quote", "hold_and_request_more_info"], ...}
```

**`quarantined_aggregates` table:**

```sql
CREATE TABLE quarantined_aggregates (
  id                  TEXT        PRIMARY KEY,
  vertical            TEXT        NOT NULL,
  workflow_slug       TEXT        NOT NULL,
  decision_type       TEXT        NOT NULL,
  conditions_redacted JSONB       NOT NULL,             -- canonical with non-passing values replaced by "<quarantined:{class}>"
  recommended_action_redacted TEXT,                      -- "<quarantined>" if action failed enum check
  reason              TEXT        NOT NULL,             -- e.g. "value at conditions[1] failed classification"
  distinct_tenant_count INT       NOT NULL,
  total_evidence_count INT        NOT NULL,
  computed_at         TIMESTAMPTZ NOT NULL DEFAULT now()
  -- DELIBERATELY ABSENT: tenant_id, original unredacted values, correction IDs.
);
```

A reviewer triages quarantined rows by either: (a) extending the relevant `condition_value_enums` to admit the value pattern (then re-running aggregation will move it to `vertical_candidate_aggregates`), or (b) discarding the quarantined entry as not generalizable. The reviewer can see the redacted shape but not the original unredacted value (which was never copied out of the per-tenant DB). To inspect originals, the reviewer must drill into a single tenant's candidates via the per-tenant proxy, which is logged.

**Order of operations in the aggregation job:**

```
For each tenant T in vertical V:
  Open per-tenant connection
  Read candidates per the aggregation algorithm above in §2.7.
  Close connection.

For each (workflow_slug, decision_type, conditions_hash, recommended_action) tuple in memory:
  classify each value in conditions_canonical
  classify recommended_action

  if all values PASS:
    write to vertical_candidate_aggregates
  else:
    write redacted shape to quarantined_aggregates
  In neither case write any tenant_id, correction_id, or case_run_id.
```

**Contract test for INV-05 (R3 strengthened, see §13.5):** Insert a `rule_candidate` with `conditions_canonical: [{field:"client_name", operator:"=", value:"ABC Realty"}]` for `t_A`. Run aggregation. Assert that no row in `vertical_candidate_aggregates` contains the string `"ABC Realty"` anywhere in its serialized JSON; assert a `quarantined_aggregates` row was created with `conditions_redacted` containing the redacted token. Run the same assertion as a standing scheduled check.

---

### 2.8 `validated_rules` — unified store, all scopes (RESOLVED-C12)

**R3 lock.** This is the canonical DDL for `validated_rules`. Ctrl-plane §13 will reference this section verbatim in their R3 revision. Any divergence (different column names, additional or omitted columns, different RLS policy) is a contract bug to be raised against this spec, not a parallel implementation.

#### 2.8.1 Locked DDL

```sql
-- Single table in the control-plane DB; covers scopes case, tenant, vertical, default.
CREATE TABLE validated_rules (
  id                          TEXT        PRIMARY KEY,                 -- e.g. "vr_0042"
  tenant_id                   TEXT,                                     -- NOT NULL for case/tenant scope
  case_run_id                 TEXT,                                     -- NOT NULL for case scope only
  vertical                    TEXT,                                     -- NOT NULL for vertical scope only
  workflow_template_id        TEXT        NOT NULL REFERENCES workflow_templates(id),
  workflow_slug               TEXT        NOT NULL,
  decision_type               TEXT        NOT NULL,
  conditions_canonical        JSONB       NOT NULL,                    -- canonical, sorted condition array (§4.2)
  conditions_hash             TEXT        NOT NULL,                    -- SHA-256 of conditions_canonical
  recommended_action          TEXT        NOT NULL,
  scope                       TEXT        NOT NULL
                                CHECK (scope IN ('case', 'tenant', 'vertical', 'default')),
  version                     INT         NOT NULL DEFAULT 1,
  supersedes                  TEXT        REFERENCES validated_rules(id) ON DELETE SET NULL,
  rollback_of                 TEXT        REFERENCES validated_rules(id) ON DELETE SET NULL,
  promoted_from_candidate_id  TEXT,                                     -- references per-tenant rule_candidates.id; NOT FK-enforced
  promoted_from_aggregate_id  TEXT REFERENCES vertical_candidate_aggregates(id) ON DELETE SET NULL,  -- vertical/default only
  promoted_by                 TEXT        NOT NULL,                    -- "reviewer:<id>" | "system:auto_promote"
  promoted_at                 TIMESTAMPTZ NOT NULL,
  rationale                   TEXT,
  status                      TEXT        NOT NULL DEFAULT 'active'
                                CHECK (status IN ('active', 'deprecated', 'rolled_back')),
  deprecated_at               TIMESTAMPTZ,
  deprecated_by               TEXT,
  -- Scope coherence: exactly the right set of identity columns is populated for each scope.
  CONSTRAINT vr_scope_consistency CHECK (
       (scope = 'case'     AND tenant_id IS NOT NULL AND case_run_id IS NOT NULL AND vertical IS NULL)
    OR (scope = 'tenant'   AND tenant_id IS NOT NULL AND case_run_id IS NULL     AND vertical IS NULL)
    OR (scope = 'vertical' AND tenant_id IS NULL     AND case_run_id IS NULL     AND vertical IS NOT NULL)
    OR (scope = 'default'  AND tenant_id IS NULL     AND case_run_id IS NULL     AND vertical IS NULL)
  ),
  -- INV-05 enforcement: vertical/default rules MUST NOT carry per-tenant provenance.
  CONSTRAINT vr_no_tenant_provenance_on_shared CHECK (
       scope IN ('case', 'tenant')
    OR (promoted_from_candidate_id IS NULL AND tenant_id IS NULL)
  )
);

CREATE INDEX vr_resolver_idx     ON validated_rules (workflow_template_id, decision_type, scope, status);
CREATE INDEX vr_tenant_idx       ON validated_rules (tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX vr_vertical_idx     ON validated_rules (vertical)  WHERE vertical  IS NOT NULL;
CREATE INDEX vr_hash_active_idx  ON validated_rules (conditions_hash) WHERE status = 'active';
CREATE INDEX vr_supersedes_idx   ON validated_rules (supersedes) WHERE supersedes IS NOT NULL;
```

#### 2.8.2 Locked RLS policy

```sql
-- Connection contract (owned by ctrl-plane §3.5):
--   Every transaction that reads/writes validated_rules first runs:
--     SET LOCAL app.current_tenant = '<tenant_id>';   -- for tenant-scoped service callers
--   For reviewer/admin sessions, the connection runs as a Postgres role with
--   role-bypass on this policy (or sets app.bypass_tenant_check='true' in a
--   trusted code path that is itself audit-logged).

ALTER TABLE validated_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE validated_rules FORCE  ROW LEVEL SECURITY;     -- applies to table owner too

CREATE POLICY vr_read_isolation ON validated_rules
  FOR SELECT
  USING (
       scope IN ('vertical', 'default')                                       -- shared rules visible to all
    OR (scope IN ('case', 'tenant') AND tenant_id = current_setting('app.current_tenant', true))
    OR current_setting('app.bypass_tenant_check', true) = 'true'              -- reviewer/admin role
  );

CREATE POLICY vr_write_isolation ON validated_rules
  FOR INSERT WITH CHECK (
       (scope IN ('case', 'tenant') AND tenant_id = current_setting('app.current_tenant', true))
    OR (scope IN ('vertical', 'default') AND current_setting('app.bypass_tenant_check', true) = 'true')
  );

-- UPDATE policy: only the promotion pipeline (with bypass) may update status fields.
-- Rules are otherwise immutable; rollback/supersession creates new rows (§8).
CREATE POLICY vr_update_isolation ON validated_rules
  FOR UPDATE USING (current_setting('app.bypass_tenant_check', true) = 'true');
```

**Roles (control-plane DB):**

| Role | Privileges | Used by |
|---|---|---|
| `validated_rules_reader` | SELECT (RLS applied) | Execution plane `LoadSkillVersion` (per-tenant context) |
| `validated_rules_writer` | INSERT, UPDATE (RLS applied; no DELETE) | Promotion pipeline (control-plane worker) — sets `app.bypass_tenant_check` |
| `validated_rules_admin` | All of writer + reviewer-bypass policy | Reviewer/admin actions (Rule Review Console) |

**Note on `promoted_from_candidate_id`:** Because `rule_candidates` lives in a per-tenant DB and `validated_rules` lives in the control plane, this is logically referenced, NOT FK-enforced. The promotion pipeline (§7.4) writes the candidate ID into the rule and into the audit event. For vertical/default promotions, `promoted_from_candidate_id` is NULL and `promoted_from_aggregate_id` references the `vertical_candidate_aggregates` row.

#### 2.8.3 RLS contract test (C12 acceptance)

```
TEST C12-RLS-CROSS-TENANT-DENY:
  Setup:
    Insert vr_A1 with scope='tenant', tenant_id='t_A'.
    Insert vr_B1 with scope='tenant', tenant_id='t_B'.
    Insert vr_V1 with scope='vertical', vertical='roofing', tenant_id=NULL.
  When connected as validated_rules_reader with SET LOCAL app.current_tenant='t_A':
    SELECT * FROM validated_rules WHERE id='vr_B1';  -- expect 0 rows
    SELECT * FROM validated_rules WHERE id='vr_A1';  -- expect 1 row
    SELECT * FROM validated_rules WHERE id='vr_V1';  -- expect 1 row (shared scope)
  When connected as validated_rules_reader with NO app.current_tenant set:
    SELECT * FROM validated_rules;                   -- expect only scope IN ('vertical','default')
  When connected as validated_rules_admin (or with bypass):
    SELECT * FROM validated_rules;                   -- expect all rows
  Negative test (RLS cannot be bypassed by SET app.current_tenant):
    Connected as validated_rules_reader, SET LOCAL app.current_tenant='t_B':
    The RLS policy uses current_setting; this test asserts that a tenant-context
    forgery is itself prevented at the application layer — the AsyncLocalStorage
    propagation (ctrl-plane §3.4) is the policy's source. Test: any path where
    app.current_tenant is set without a verified JWT must be rejected before reaching
    the DB. Owned by ctrl-plane §3.4; named here for cross-spec traceability.
```

**Owner of the test:** Devil's Advocate (cross-component); implementation in the integration test suite. Both ctrl-plane and this spec reference this test.

---

### 2.9 `skill_versions` (control plane DB)

```sql
CREATE TABLE skill_versions (
  id                  TEXT        PRIMARY KEY,              -- e.g. "sv_r7q1"
  tenant_id           TEXT,                                 -- NULL = vertical/default skill
  workflow_template_id TEXT       NOT NULL,
  workflow_slug       TEXT        NOT NULL,
  version             INT         NOT NULL,
  -- Ordered, immutable list of ValidatedRule IDs bundled in this skill version.
  -- References both validated_rules (per-tenant) and validated_rules_shared.
  rule_manifest       JSONB       NOT NULL,                 -- [{rule_id, scope, version, decision_type}]
  generated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  generated_by        TEXT        NOT NULL,                 -- "system:promotion" | "reviewer:<id>"
  supersedes          TEXT        REFERENCES skill_versions(id) ON DELETE SET NULL,
  status              TEXT        NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'deprecated', 'rolled_back')),
  UNIQUE (tenant_id, workflow_slug, version)
);

CREATE INDEX sv_tenant_workflow_idx ON skill_versions (tenant_id, workflow_slug);
CREATE INDEX sv_status_idx ON skill_versions (status) WHERE status = 'active';
```

---

### 2.10 `audit_events` — single authoritative store + outbox (R4)

Per RESOLVED-AUDIT-TOPOLOGY-R4 (§2.0.1, §11.0):

| Table location | Name | Role | Authoritative? |
|---|---|---|---|
| Control plane DB | `audit_events` | System of record; partitioned by `tenant_id` then `occurred_at` | **Yes** |
| Per-tenant execution DB | `audit_events_outbox` | Transactional durable buffer; drains upstream; not a read source | No (derivative) |

The outbox row schema is the same as `audit_events` plus a `drained` boolean column. Storage-layer immutability (§11.5) applies at both stores via the `audit_writer` role + BEFORE UPDATE/DELETE/TRUNCATE trigger. See §11 for full schema, partitioning, RLS, role grants, and the writer registry.

---

## 3. Correction Shape (Operator UX Contract)

### 3.1 ACCEPTED: operator-ux's `StructuredCorrection` shape

**Operator-ux §2.2 defined a `StructuredCorrection` shape. This spec ACCEPTS that shape as the contract.** The Round 1 shape proposed by this spec is superseded. The accepted shape:

```json
{
  "correction_id": "corr_abc1",                   // UUID v4, generated by operator-ux at parse time
  "packet_id": "pkt_7a3c",
  "tenant_id": "t_123",
  "case_run_id": "cr_456",
  "decision_point_id": "dp_789",
  "button_action": "wrong_action",                // enum (see 3.2)
  "free_text": "<operator's verbatim free text, if any>",
  "scope_hint": "always",                         // "always" | "this_case" | null
  "condition_hints": [
    { "field": "<field name>", "operator": "=", "value": "<value>" }
  ],
  "follow_up_answers": [
    { "question_key": "scope_qualifier", "answer": "always_when_new_client" }
  ],
  "parse_method": "button",                       // "button" | "button_fallback" | "text_match" | "llm"
  "parse_confidence": 1.0,                        // 0.0..1.0
  "raw_inbound_message_id": "wa_msg_xyz",
  "received_at": "2026-04-27T08:14:00Z",
  "parse_status": "resolved"                      // "resolved" | "needs_followup" | "dead_lettered"
}
```

### 3.2 `button_action` enum (canonical)

The six-value enum from the product spec, mirrored across operator-ux §2.2 and this spec:

| Value | Meaning |
|---|---|
| `approve` | Operator confirms the decision — no candidate generated |
| `wrong_facts` | Agent extracted incorrect facts |
| `wrong_action` | Agent chose the wrong action |
| `missing_condition` | A condition that should have affected the decision was missing |
| `use_different_template` | Wrong template, artifact style, or format |
| `add_note` | Operator note; not directly actionable |

### 3.3 Mapping to `corrections` table columns

| StructuredCorrection field | `corrections` column | Notes |
|---|---|---|
| `correction_id` | `id` | Renamed in DB schema |
| `tenant_id`, `case_run_id`, `decision_point_id` | same | Direct copy |
| `button_action` | `action_button` | DB column kept under existing name |
| `free_text` | `free_text` | Direct |
| `scope_hint` | `scope_hint` | Mapped: `"always"` → `'tenant'`, `"this_case"` → `'case'`, null → null |
| `condition_hints` | merged into `parsed_conditions` (Stage B; see §3.6) | See parsing pipeline |
| `follow_up_answers` | stored under `follow_up_answer` as concatenated text + JSON copy in `parsed_conditions.follow_up_answers` | |
| `parse_method` | `parse_method` (new column, add) | Used to weigh `parse_confidence` |
| `parse_confidence` | `parse_confidence` | Used by candidate-match algorithm to gate Stage B |
| `raw_inbound_message_id` | maps to `idempotency_key` (with `wa_msg_xyz` form) | Format: `<provider>:<message_id>` |
| `received_at` | `created_at` | Direct |
| `parse_status` | `parse_status` | Values aligned: `"resolved"` → `'parsed'`, `"needs_followup"` → `'pending'`, `"dead_lettered"` → `'parse_failed'` |

The `corrections` DDL in §2.6 is updated to add `parse_method` and align enum values; see §3.5 for the writer.

### 3.4 Parsing pipeline triggers

- `approve` → no candidate; the workflow proceeds (per exec-plane §7.2). An `AuditEvent{event_type: 'correction_approve'}` is emitted by the writer (§3.5).
- All other `button_action` values (and `parse_status = 'resolved'`) → trigger the candidate-match algorithm (§4).
- `add_note` → stored as a correction record only; no candidate.
- `parse_status = 'dead_lettered'` → never reaches the matcher (operator-ux §3 invariant I-07); recorded with `parse_status = 'parse_failed'` for human-only review.

### 3.5 RESOLVED-C10: Writer of `corrections` row and `correction_received` audit event

**Writer:** Exec-plane's `PersistCorrection` Temporal activity (exec-plane §3.4) is the sole writer of:
- The `corrections` row (per-tenant DB).
- The `correction_received` audit event (control-plane DB; see §11.0).

**Why exec-plane writes, not operator-ux:**
1. The Temporal workflow is the durable owner of the case; it must persist the correction in the same transaction context as the workflow's state transition (CaseRejected → branch).
2. Operator-ux delivers the signal envelope (operator-ux §2.3); exec-plane's signal handler picks it up and invokes `PersistCorrection`.
3. The `corrections` row needs a database connection scoped to the per-tenant DB. The Temporal worker has that connection; the gateway does not.

**Operator-ux's audit event** for `packet_sent` and the inbound dedup row (operator-ux §9.2) are gateway-side concerns and are emitted by the gateway. Those are different events from `correction_received`.

### 3.6 RESOLVED-OQ-Parse: Two-stage parsing pipeline split

**Stage A — owned by operator-ux** (gateway parser, operator-ux §7.1):
- Inputs: WhatsApp/Telegram inbound message + button payload + follow-up state.
- Output: `StructuredCorrection` with `condition_hints`, `scope_hint`, `parse_method`, `parse_confidence`.
- Stage A produces `condition_hints` from button taps, text synonyms, or LLM parse — at the message-content level. It does NOT need access to `decision_point.agent_input`.

**Stage B — owned by this spec** (candidate-matcher, §4):
- Inputs: `StructuredCorrection.condition_hints` + `decision_points.agent_input` (joined on `decision_point_id`).
- Output: canonical `parsed_conditions` array + `conditions_hash`.
- Stage B reconciles operator-stated condition hints against the actual facts available at the decision point. Example: operator says "client is new"; agent_input shows `{client_type: "new", customer_id: null}` — Stage B canonicalizes to `[{field:"client_type", operator:"=", value:"new"}]`.

**If `parse_confidence < parse_low_confidence_threshold` (default 0.6):** Stage B does NOT auto-create a candidate. The correction is stored with `parse_status = 'manual_review'` and surfaced to the reviewer for human-assisted condition extraction. This protects against noisy LLM parses creating spurious candidates.

### 3.7 Validation invariants

1. `case_run_id` and `decision_point_id` must exist in the per-tenant DB and be consistent with each other.
2. `decision_point_id.case_run_id` must equal the submitted `case_run_id`.
3. `idempotency_key` (derived from `raw_inbound_message_id`) uniqueness is enforced at the DB level (`UNIQUE` on `corrections.idempotency_key`).
4. `tenant_id` must match the authenticated tenant context — never trusted from the request body (per devil's advocate §1.1 and ctrl-plane §3.3).
5. `parse_status = 'dead_lettered'` MUST NOT trigger candidate creation (operator-ux invariant I-07).

### 3.8 RESOLVED-C13: `ApprovalSignalEnvelope` payload required for `PersistCorrection`

**Status: RESOLVED.** Per moderator R3 brief task #6, this section publishes the **required field set** for the Temporal signal envelope so that `PersistCorrection` can build a `corrections` row and emit the `correction_received` audit event without any extra fetch.

**Why this matters.** Operator-ux R2 §2.3's `CaseApproved`/`CaseRejected` payloads are narrower than the inbound `Correction` JSON (operator-ux §2.2). Without the wider payload on the signal, `PersistCorrection` would have to call back into the gateway to reconstruct the row — adding a hop, an availability dependency, and a second-source-of-truth risk.

**Required fields on every `CaseApproved`/`CaseRejected` signal envelope** (R = required, O = optional):

| # | Field | R/O | Source | Used by `PersistCorrection` for |
|---|---|---|---|---|
| 1 | `idempotency_key` | R | Gateway: `corr.idempotency_key` (derived from `raw_inbound_message_id`) | UNIQUE on `corrections`; signal dedup |
| 2 | `tenant_id` | R | Gateway: `channel_bindings.provider_number → tenant_id` (operator-ux §4.9) | Per-tenant DB connection scoping |
| 3 | `case_run_id` | R | From the packet correlation | `corrections.case_run_id`; FK target |
| 4 | `decision_point_id` | R | From the packet correlation | `corrections.decision_point_id`; FK target |
| 5 | `action_button` | R | Gateway parser (Stage A) | `corrections.action_button` |
| 6 | `free_text` | O (NULL allowed) | Operator's reply text | `corrections.free_text` |
| 7 | `follow_up_answer` | O (NULL allowed) | Concatenated follow-up answers | `corrections.follow_up_answer` |
| 8 | `scope_hint` | O (NULL allowed) | Gateway parser | `corrections.scope_hint` |
| 9 | `operator_id` | R | Gateway: `op_whatsapp:<E.164>` | `corrections.operator_id`; audit `actor_id` |
| 10 | `source_message_id` | R | Provider message ID (WhatsApp/Telegram) | gateway-side dedup; recorded for forensic trace |
| 11 | `channel` | R | `'whatsapp' \| 'telegram'` | recorded; informs PII handling |
| 12 | `raw_inbound_message_id` | R | Same as #10 in our case (kept distinct for parser-internal evolution) | reconstructs idempotency_key derivation |
| 13 | `parse_method` | R | Gateway parser | `corrections.parse_method` |
| 14 | `parse_confidence` | R | Gateway parser | `corrections.parse_confidence`; gates Stage B |
| 15 | `packet_id` | R | The `ReviewPacket.packet_id` this is a reply to | audit trail; correlates to gateway audit |
| 16 | `ts` | R | Operator-ux `received_at` (ISO 8601 UTC) | `corrections.created_at` (ingest writes this) |

The 16 fields are exactly enumerated for traceability. `CaseApproved` may safely omit fields 6, 7, 8, 13, 14 (no parsed correction content is needed for an approve), but the same envelope schema is recommended for both signals so the consumer treats them uniformly.

**Gating question for operator-ux R3:** Does the gateway commit to populating all 16 fields on every `CaseRejected` signal, with the formats specified above? If the gateway prefers to keep its `CaseRejected` payload narrow (R2 §2.3), then `PersistCorrection` MUST receive a separate "Correction" envelope (operator-ux §2.2 inbound shape) routed through the same Temporal signal layer or via a parallel control-plane ingest. Either contract is acceptable; this spec needs operator-ux to choose one.

**Default position pending confirmation:** option A — extend the signal payload to carry the 16 fields. This avoids a second routing path. If operator-ux pushes back, fallback to option B — gateway POSTs the `Correction` JSON to a control-plane `/internal/corrections/ingest` endpoint that, in turn, emits a richer Temporal signal carrying the row contents.

### 3.9 OPEN-N6: Idempotency-key inventory

**Status: OPEN — composition rule deferred to ctrl-plane (per moderator R3 §6 row N6).** This section enumerates every idempotency key this spec produces or consumes.

| Key name | Source | Lifetime/scope | Producer | Consumer |
|---|---|---|---|---|
| `corrections.idempotency_key` | Gateway-derived from `<provider>:<message_id>` (operator-ux §2.2) | Per inbound operator message | Gateway (Stage A) → flows in signal envelope | `PersistCorrection` (UNIQUE constraint on `corrections`) |
| `audit_events.idempotency_key` | Caller-supplied per event; required at INSERT (§11.2) | Per audit event | Each writer (per writer registry §11.6) | `audit_events` UNIQUE constraint |
| `case_runs.input_hash` | SHA-256 of normalized input payload | Per case run; used as replay-equivalence anchor (§10) | Exec-plane workflow start | `case_runs.input_hash`; replay validation |
| MCP idempotency log key (consumed only) | `{case_run_id}:{decision_point_id}:{tool_name}` (exec-plane §5.1) | Per MCP tool call | Exec-plane | MCP server `mcp_idempotency_log`; this spec consumes the key as `mcp_read_snapshot.mcp_idempotency_key` (§2.5) |
| `signal_id` (consumed in audit `related_ids`) | Gateway-generated per Temporal signal (operator-ux §2.3) | Per signal delivery | Gateway | Recorded in `audit_events.related_ids.signal_id` for `correction_received`/`approval_received` |

**Composition rule:** deferred. The moderator's overview §6 N6 row assigns the rule to ctrl-plane, with proposed: `sha256(tenant_id ‖ decision_point_id ‖ packet_id ‖ action_button)`. This spec will adopt whatever ctrl-plane publishes, with one constraint: the rule MUST be deterministic from gateway-visible inputs alone, so the gateway can compute the key without round-tripping to ctrl-plane.

**This spec does NOT independently invent a composition rule.** The keys above are listed at their existing names so ctrl-plane's R3 §N6 can map them.

---

## 4. Candidate-Match Algorithm

### 4.1 Invariants (define before algorithm)

```
INVARIANT C1: Idempotence
  Submitting the same Correction twice (same idempotency_key) must produce
  the same candidate state as submitting it once.

INVARIANT C2: Monotone evidence
  Processing a new non-contradicting Correction into an existing candidate
  must never decrease evidence_count or confidence.

INVARIANT C3: No silent merge
  Two candidates may only be merged if their conditions_hash values are
  identical OR a schema-aware similarity check is explicitly triggered
  and recorded in the AuditEvent trail.

INVARIANT C4: Conflict tracking
  If a Correction's parsed_action contradicts an existing candidate with the
  same conditions_hash, the contradiction must be recorded in conflicts_with
  on both candidates, and contradicting_count incremented on the existing one.

INVARIANT C5: Orphan prevention
  Every Correction that is not 'approve' or 'add_note' must result in either:
  (a) a new RuleCandidate row created, OR
  (b) an existing RuleCandidate row updated,
  OR a parse_failed correction record — never silent discard.
```

### 4.2 Algorithm

**Step 1 — Canonicalize conditions.**

The parsing pipeline (consuming `free_text`, `follow_up_answer`, `decision_point.agent_input`) produces a `parsed_conditions` array. Before any match attempt, canonicalize:

```
canonicalize(conditions):
  1. Sort conditions by (field ASC, operator ASC, value ASC)
  2. Normalize value types: booleans to boolean, numbers to numeric, strings to lowercase trimmed
  3. Normalize operators: "equals" → "=", "not_equal" → "!=", "greater_than" → ">"
  4. Return JSON array of {field, operator, value} objects in sorted order
  5. Compute SHA-256(JSON.stringify(canonicalized_array)) → conditions_hash
```

This canonicalization is deterministic and schema-aware (not semantic). Two conditions are an exact match if and only if their `conditions_hash` values are equal.

**Step 2 — Exact-match lookup.**

```
lookup_exact(tenant_id, workflow_slug, decision_type, conditions_hash):
  SELECT * FROM rule_candidates
  WHERE tenant_id = ? AND workflow_slug = ? AND decision_type = ?
    AND conditions_hash = ?
    AND status NOT IN ('merged', 'rejected', 'promoted')
  LIMIT 1
```

If a match is found:
- If `recommended_action` matches the correction's `parsed_action` → merge (increase evidence).
- If `recommended_action` differs → flag as contradiction (see §12 Conflicting Candidates).

**Step 3 — Near-match lookup (schema-aware similarity).**

Near-match is triggered ONLY when exact match returns nothing. Definition: two condition sets are near-matches if:
- They reference the same set of `field` names (exact equality on field names).
- At least one condition's `operator` or `value` differs by a bounded range (e.g., numeric within ±20%, categorical variant of same enum).

Near-match produces a **candidate suggestion**, not an automatic merge. A near-match suggestion is written to `AuditEvent` and surfaced to the reviewer in the Rule Review Console for manual merge decision.

**No semantic/embedding similarity in v1.** Embedding-based fuzzy condition matching is explicitly deferred. The risk of false merges (two "similar-sounding" rules that are logically different) outweighs the benefit at this scale. Flag as a future enhancement.

**Step 4 — Create or update.**

```
if exact_match AND same_action:
  update rule_candidates:
    evidence_count += 1
    source_correction_ids = array_append(source_correction_ids, correction.id)
    source_case_run_ids = array_append(source_case_run_ids, case_run_id)
    last_seen_at = now()
    confidence = compute_confidence(evidence_count, contradicting_count, recency_factor)
    scope = max(current_scope, scope_hint)   -- scope can only escalate, never de-escalate
  emit AuditEvent{event_type: 'candidate_evidence_added'}

elif exact_match AND different_action:
  -- contradiction: see §12
  increment contradicting_count on existing candidate
  create new candidate for the contradicting action
  link conflicts_with on both
  emit AuditEvent{event_type: 'candidate_contradiction_detected'}

else:  -- no match (or near-match only)
  create new rule_candidate:
    conditions_canonical = canonicalized_conditions
    conditions_hash = computed_hash
    recommended_action = parsed_action
    scope = scope_hint OR 'case' (default)
    evidence_count = 1
    confidence = compute_confidence(1, 0, recency_factor=1.0)
  emit AuditEvent{event_type: 'candidate_created'}
```

### 4.3 Test Cases

```
TEST MATCH-1: Exact match, same action
  Given: existing candidate rc_X with conditions_hash=H, recommended_action="hold"
  When: correction parsed to conditions_hash=H, parsed_action="hold"
  Then: rc_X.evidence_count = rc_X.evidence_count + 1; no new candidate created

TEST MATCH-2: Exact match, different action (contradiction)
  Given: existing candidate rc_X with conditions_hash=H, recommended_action="hold"
  When: correction parsed to conditions_hash=H, parsed_action="send"
  Then: rc_X.contradicting_count += 1; new candidate rc_Y created with recommended_action="send";
        rc_X.conflicts_with includes rc_Y.id; rc_Y.conflicts_with includes rc_X.id

TEST MATCH-3: No match — new candidate
  Given: no existing candidate with conditions_hash=H
  When: correction parsed to conditions_hash=H
  Then: new candidate created with evidence_count=1

TEST MATCH-4: Idempotence
  Given: correction corr_A with idempotency_key=K already processed
  When: same payload submitted again (same K)
  Then: DB unique constraint violation caught; returns existing correction record;
        candidate evidence_count unchanged

TEST MATCH-5: Canonicalization order independence
  Given: correction A has conditions [{"field":"x","op":"=","value":"a"}, {"field":"y","op":"=","value":"b"}]
  Given: correction B has conditions [{"field":"y","op":"=","value":"b"}, {"field":"x","op":"=","value":"a"}]
  Then: both produce the same conditions_hash

TEST MATCH-6: Near-match does NOT auto-merge
  Given: candidate rc_X with conditions [{field:"photos_complete",op:"=",value:false}]
  When: correction parsed to [{field:"photos_complete",op:"=",value:true}]
  Then: no auto-merge; near-match suggestion emitted to AuditEvent; new candidate rc_Y created
```

---

## 5. Confidence-Scoring Model

### 5.1 Invariants

```
INVARIANT CONF-1: Bounded range
  confidence ∈ [0.0, 1.0] at all times.

INVARIANT CONF-2: Monotone with evidence (absent contradictions)
  If contradicting_count is unchanged and evidence_count increases by 1,
  confidence must not decrease.

INVARIANT CONF-3: Contradictions lower confidence
  If evidence_count is unchanged and contradicting_count increases by 1,
  confidence must decrease.

INVARIANT CONF-4: Threshold crossing triggers status change
  When confidence crosses under_review_threshold AND evidence_count >= min_evidence_count,
  status changes to 'under_review' atomically.

INVARIANT CONF-5: Recency decay is bounded
  recency_factor ∈ (0.0, 1.0]; a candidate observed today has recency_factor = 1.0;
  a candidate with no new evidence in > recency_window days approaches recency_floor.
```

### 5.2 Formula

```
confidence = wilson_lower(evidence_count, evidence_count + contradicting_count, z=1.28)
             * recency_factor(last_seen_at)
             * scope_consistency_factor(source_case_run_ids, tenant_id)
```

**Component 1 — Wilson score lower bound (agreement rate)**

The Wilson lower bound on the agreement rate is used rather than a raw ratio `e/(e+c)` because:
- It is conservative: with `e=1, c=0`, raw ratio = 1.0 (overconfident); Wilson lower bound = 0.21 (appropriately uncertain).
- It penalizes small sample sizes by construction.
- `z=1.28` corresponds to 80% confidence interval (less conservative than 95% / z=1.96), appropriate for a system where under-promotion is safer than over-promotion but reviewers provide the final gate.

```
wilson_lower(e, n, z=1.28):
  p_hat = e / n
  denominator = 1 + z^2/n
  centre = p_hat + z^2/(2*n)
  margin = z * sqrt(p_hat*(1-p_hat)/n + z^2/(4*n^2))
  return (centre - margin) / denominator
```

Reference: Wilson, E.B. (1927). "Probable Inference, the Law of Succession, and Statistical Inference." JASA.

**Component 2 — Recency factor**

```
recency_factor(last_seen_at):
  age_days = (now() - last_seen_at) / 86400
  return max(recency_floor, exp(-age_days / recency_half_life))
```

- `recency_half_life = 30` (days): confidence halves if no corroborating evidence in 30 days. Justified: workflows that haven't triggered a matching correction in a month are likely edge cases; we should require fresher evidence before promoting.
- `recency_floor = 0.1`: prevents confidence from dropping to zero (candidate is not discarded, just deprioritized).
- Both are reconfigurable in system config and overridable per-tenant.

**Component 3 — Scope consistency factor**

```
scope_consistency_factor(source_case_run_ids, tenant_id):
  -- If all source cases are from a single case run: factor = 0.8 (low diversity)
  -- If from 2 distinct case runs, same tenant: factor = 0.9
  -- If from >= 3 distinct case runs or multiple tenants: factor = 1.0 (capped)
  return min(1.0, 0.8 + 0.1 * (distinct_case_count - 1) + 0.05 * (distinct_tenant_count - 1))
```

Justification: a rule seen in only one case run might be a quirk of that specific input. Multiple distinct case runs provide stronger signal. Cap at 1.0 since confidence is a probability-adjacent score.

### 5.3 Default Thresholds

**Note on product spec example values:** The worked example in the product spec shows `confidence` values of 0.55, 0.66, and 0.72 as evidence accumulates. Those are illustrative values consistent with a Bayesian-smoothed Beta posterior (`(e + α) / (e + c + α + β)`), not the Wilson formula. This spec uses Wilson lower bound because it is more conservative and better-understood; **the resulting numbers will differ from the product spec illustration**. That is intentional — this is a refinement, not a contradiction.

**Wilson lower bound math for key values** (computed at `z=1.28`, 80% one-sided CI):

Using `wilson_lower(e, n, z) = ((p̂ + z²/2n) − z·√(p̂(1−p̂)/n + z²/4n²)) / (1 + z²/n)` with `p̂ = e/n`:

- `wilson_lower(1, 1, z=1.28)`: p̂=1.0; numerator = 1.819 − 1.28·√(0 + 0.41) = 1.819 − 0.819 = 1.0; denominator = 1 + 1.638 = 2.638; **≈ 0.379**.
- `wilson_lower(2, 2, z=1.28)`: p̂=1.0; numerator = 1.41 − 1.28·√(0 + 0.1024) = 1.41 − 0.41 = 1.0; denominator = 1 + 0.819 = 1.819; **≈ 0.550**.
- `wilson_lower(3, 3, z=1.28)`: p̂=1.0; numerator = 1.273 − 0.273 = 1.0; denominator = 1.546; **≈ 0.647**.
- `wilson_lower(3, 4, z=1.28)` (one contradiction): p̂=0.75; numerator = 0.955 − 0.293 = 0.662; denominator = 1.41; **≈ 0.470**.

Applied with multipliers:
- `e=1, c=0`, scope=0.8: `0.379 × 0.8 ≈ 0.303`.
- `e=2, c=0`, scope=0.9: `0.550 × 0.9 ≈ 0.495`.
- `e=3, c=0`, scope=0.9: `0.647 × 0.9 ≈ 0.582`.
- `e=3, c=1`, scope=0.9: `0.470 × 0.9 ≈ 0.423` (still below threshold, contradiction also blocks).

These values justify threshold placement: `under_review_threshold = 0.55` ensures at least `e=3` clean corrections (with reasonable scope diversity) before review; `e=2` is borderline and requires exact threshold tuning; `e=1` alone never crosses.

| Parameter | Default | Justification | Reconfigure via |
|---|---|---|---|
| `under_review_threshold` | 0.55 | `wilson_lower(2, 2, z=1.28) × scope_consistency(2 cases) ≈ 0.495`; `wilson_lower(3, 3, z=1.28) × scope_consistency(2 cases) ≈ 0.582`. Threshold of 0.55 requires `e=3` with moderate scope diversity or `e=2` with maximal scope. Evidence count guard remains primary. | `tenants.conf_under_review_threshold` |
| `min_evidence_count` | 3 | 1–2 corrections could reflect a single edge case; 3 is the minimum meaningful pattern signal. Below 3 we have less than 50% bayesian confidence even with 0 contradictions. | `tenants.conf_min_evidence_count` |
| `auto_promote_threshold` | NOT ENABLED in v1 | Auto-promotion without human review is out of scope for MVP. When enabled, suggested threshold = 0.80 (Wilson lower at `e=8, c=0`), with 0 contradictions required. | `tenants.conf_auto_promote_threshold` (future) |
| `no_contradictions_required` | `true` for `under_review`; `true` for promotion | A candidate with even one contradiction must be manually reviewed — contradictions represent unresolved business rule ambiguity. | System config only (not per-tenant) |
| `recency_half_life` | 30 days | See Component 2 reasoning above | System config |
| `candidate_ttl_days` | 60 days with `evidence_count = 1` | Orphan candidates with a single correction and no follow-up in 60 days are likely noise; mark `stale_at` for soft deletion. | `tenants.conf_candidate_ttl_days` |

**The `0.72` in the product spec worked example** is a specific point value, not the threshold. It reflects `wilson_lower(3, 3, 1.28) * recency_factor(4 days) * scope_consistency(2 case runs)` ≈ 0.72. This is consistent with the formula above — no magic.

### 5.4 Test Cases

```
TEST CONF-1: Single evidence, no contradictions
  wilson_lower(1, 1, 1.28) ≈ 0.379; recency=1.0; scope=0.8
  Assert: confidence ≈ 0.303; status remains 'candidate'

TEST CONF-2: Three evidences, no contradictions, 2 distinct case runs
  wilson_lower(3, 3, 1.28) ≈ 0.647; recency=1.0; scope=0.9
  Assert: confidence ≈ 0.582; crosses under_review_threshold of 0.55; status transitions to 'under_review'

TEST CONF-3: Three evidences, one contradiction
  wilson_lower(3, 4, 1.28) ≈ 0.470; recency=1.0; scope=0.9
  Assert: confidence ≈ 0.423; status remains 'candidate' (below threshold AND contradicting_count > 0
  blocks transition regardless of confidence per §5.3 no_contradictions_required).

TEST CONF-4: Recency decay
  Given: candidate last seen 60 days ago, half_life=30
  exp(-60/30) = exp(-2) ≈ 0.135; combined: 0.647 * 0.135 * 0.9 ≈ 0.079
  Assert: confidence drops below under_review_threshold; no status change triggered

TEST CONF-5: Invariant CONF-2 check (property test)
  For all (e in 1..20, c in 0..e):
    confidence(e+1, c) >= confidence(e, c)
```

---

## 6. Scope Escalation

### 6.1 Scope levels (ordered, lowest to highest)

```
case  <  tenant  <  vertical  <  default
```

A rule's scope defines which future case runs it applies to. Scope can only increase, never decrease once set.

### 6.2 Escalation rules

**case → tenant**

Triggered when the `scope_hint` from `follow_up_answer` is `"tenant"`, OR when `evidence_count >= 2` from at least 2 distinct `case_run_id` values within the same tenant. Rationale: if an operator corrects the same pattern across two different cases, it is almost certainly a tenant-level preference, not a quirk of one case.

**tenant → vertical**

Triggered ONLY by a reviewer action in the Rule Review Console — never automatically. Requires: the reviewer observes that the same `conditions_hash` + `recommended_action` appears in `rule_candidates_control` for `>= 3` distinct tenants within the same vertical. Rationale: vertical escalation affects all new tenants in the vertical; it must be a deliberate reviewer decision, not an algorithmic one.

**vertical → default**

Triggered ONLY by a reviewer action with elevated privileges. Requires: the pattern is observed across at least 2 distinct verticals and is logically domain-agnostic. This should be rare.

### 6.3 Scope resolution at runtime

When Hermes queries for applicable rules at a decision point, the resolution order is:
```
case-scoped rules (this case_run_id)   [highest priority]
  ↓
tenant-scoped rules (this tenant_id)
  ↓
vertical-scoped rules (this tenant's vertical)
  ↓
default rules                          [lowest priority]
```

More specific rules override less specific ones when they conflict on the same `(decision_type, conditions_hash)`.

### 6.4 Invariants

```
INVARIANT SCOPE-1: Monotone escalation
  A candidate's scope may only move in the direction: case → tenant → vertical → default.
  No downgrade is permitted.

INVARIANT SCOPE-2: Vertical/default escalation is reviewer-gated
  scope changes to 'vertical' or 'default' must have a corresponding AuditEvent
  with event_type = 'scope_escalation_reviewer_approved' and reviewer_id set.

INVARIANT SCOPE-3: More specific wins
  When two active ValidatedRules match the same (decision_type, conditions_hash),
  the one with the more specific scope is applied. Ties are broken by promoted_at DESC.
```

---

## 7. Promotion Flow

### 7.1 Trigger: under_review

When `confidence >= under_review_threshold AND evidence_count >= min_evidence_count AND contradicting_count = 0`, the candidate's `status` (in the per-tenant DB) is set to `'under_review'` and `under_review_at = now()`. An `AuditEvent{event_type: 'candidate_under_review'}` is written to the control-plane `audit_events` table by the candidate-matcher service. The Rule Review Console reads candidates by connecting to the relevant per-tenant DB through the control-plane proxy (no continuous replica).

### 7.2 Read payload: Rule Review Console receives

The Rule Review Console queries the following view per candidate in `under_review` status:

```json
{
  "candidate": {
    "id": "rc_a91f",
    "tenant_id": "t_123",
    "vertical": "roofing",
    "workflow_slug": "quote_drafting",
    "decision_type": "send_or_hold",
    "conditions_canonical": [
      {"field": "photos_complete", "operator": "=", "value": false},
      {"field": "client_type", "operator": "=", "value": "new"}
    ],
    "recommended_action": "hold_and_request_more_info",
    "scope": "tenant",
    "confidence": 0.55,
    "evidence_count": 3,
    "contradicting_count": 0,
    "under_review_at": "2026-04-27T09:00:00Z"
  },
  "source_case_runs": [
    {
      "case_run_id": "cr_456",
      "workflow_slug": "quote_drafting",
      "decision_point_id": "dp_a1b2",
      "agent_output": {"action": "send_quote", "template": "standard"},
      "correction_action_button": "wrong_action",
      "correction_scope_hint": "tenant"
    }
    // ... up to 5 most recent source case runs
  ],
  "vertical_aggregate": {
    // optional: present only when reviewer requests "show me cross-tenant pattern for this candidate"
    // Populated from vertical_candidate_aggregates (§2.7) — fully PII-stripped (INV-05).
    // Never contains tenant_id or any tenant-identifying fields.
    "distinct_tenant_count": 4,
    "total_evidence_count": 11,
    "total_contradicting_count": 0
  },
  "conflicts_with": []
}
```

### 7.3 Write payload: reviewer returns one of

**Promote:**
```json
{
  "action": "promote",
  "candidate_id": "rc_a91f",
  "rationale": "3 matching corrections, no contradictions, aligns with stated policy.",
  "scope_override": null,  // null = use candidate's current scope
  "reviewer_id": "reviewer:alice@victoria.app"
}
```

**Reject:**
```json
{
  "action": "reject",
  "candidate_id": "rc_a91f",
  "rationale": "Edge case specific to one customer's unusual setup.",
  "reviewer_id": "reviewer:alice@victoria.app"
}
```

**Merge into existing candidate:**
```json
{
  "action": "merge",
  "candidate_id": "rc_a91f",
  "merge_target_id": "rc_b33c",
  "rationale": "Same semantic rule, different condition wording; merge into rc_b33c.",
  "reviewer_id": "reviewer:alice@victoria.app"
}
```

**Request more evidence:**
```json
{
  "action": "return_to_candidate",
  "candidate_id": "rc_a91f",
  "rationale": "Need more cross-case evidence before promoting.",
  "reviewer_id": "reviewer:alice@victoria.app"
}
```

### 7.4 Promotion effects (atomic transaction)

The Rule Review Console (ctrl-plane §7.6) executes the promote command. The promotion pipeline (ctrl-plane-side worker, owns the transaction):

1. **Control plane DB transaction** — single transaction:
   - Create `validated_rules` row (in unified control-plane store, per §2.8).
   - If a prior active rule has same `(tenant_id|vertical, workflow_slug, decision_type, conditions_hash)`, set it to `deprecated`, set `new_vr.supersedes = old_vr.id`.
   - Compute the new `SkillVersion` manifest (§9) for affected `(tenant_id, workflow_slug)` tuples.
   - Insert new `skill_versions` row.
   - Insert `audit_events` rows: `rule_promoted` and `skill_version_created`.
2. **Per-tenant DB write** (separate transaction; eventually consistent with control plane):
   - Set `rule_candidates.status = 'promoted'` and `rule_candidates.promoted_to_rule_id = vr.id`.
   - If this write fails, the candidate may be left in `under_review` status; a reconciliation job (control-plane scheduled task) re-asserts the status from the rule's `promoted_from_candidate_id` field. The audit chain is intact even if the candidate row update is delayed.

Vertical/default promotions skip step 2 (no per-tenant candidate to update; the source is the aggregate view).

### 7.5 Audit chain

```
Correction corr_A  → AuditEvent{event_type:'correction_received', ref_id:corr_A.id}
    ↓
Candidate rc_X     → AuditEvent{event_type:'candidate_created', ref_id:rc_X.id}
    ↓ (more evidence)
               AuditEvent{event_type:'candidate_evidence_added', ref_id:rc_X.id, evidence_correction_id:corr_B.id}
    ↓
Status → under_review  → AuditEvent{event_type:'candidate_under_review', ref_id:rc_X.id}
    ↓
Reviewer promote   → AuditEvent{event_type:'rule_promoted', ref_id:vr_Y.id, candidate_id:rc_X.id, reviewer_id:...}
    ↓
SkillVersion sv_Z  → AuditEvent{event_type:'skill_version_created', ref_id:sv_Z.id, rule_ids:[vr_Y.id,...]}
```

Every step is linked by `ref_id` and `related_ids[]` in the AuditEvent schema (§11). The chain from a specific `Correction` to the `ValidatedRule` it contributed to is always reconstructable.

---

## 8. ValidatedRule Versioning

### 8.1 Version semantics

Each `ValidatedRule` is **immutable once created**. To change a rule, a new version row is created. The version number is per `(tenant_id, workflow_slug, decision_type, conditions_hash)` — not a global counter.

### 8.2 Supersession

When a `promote` action would create a new `ValidatedRule` for a `(tenant_id, workflow_slug, decision_type)` combination that already has an active rule:

1. If `conditions_hash` is the same: the new rule is a refinement (e.g., more specific conditions were added; `recommended_action` may differ). New rule version = `old.version + 1`; `new.supersedes = old.id`; old is set to `deprecated`.
2. If `conditions_hash` differs: the new rule is an additional rule, not a replacement. Both remain `active`. Resolution at runtime via scope priority (§6.3).

**Contradiction case:** when a contradicting correction arrives after a `ValidatedRule` is active, the parsing pipeline creates a new `RuleCandidate` with conflicting `recommended_action`. When this candidate is eventually promoted, the review console marks which existing rule is superseded. The reviewer must specify `supersedes` explicitly in the promote payload for contradiction promotions.

### 8.3 Rollback

A rollback is a first-class operation, not an undo:

1. Reviewer issues a `rollback` command: `{action: "rollback", rule_id: "vr_0042_v2", reviewer_id: ..., rationale: ...}`.
2. System:
   - Sets `vr_0042_v2.status = 'rolled_back'`.
   - Creates `vr_0042_v3` with the same `conditions_canonical` and `recommended_action` as `vr_0042_v1`.
   - Sets `vr_0042_v3.rollback_of = "vr_0042_v2"` and `vr_0042_v3.supersedes = "vr_0042_v2"`.
   - Creates a new `SkillVersion` referencing v3 in place of v2.
3. `AuditEvent{event_type: 'rule_rolled_back', ref_id: vr_0042_v3.id, rolled_back_rule_id: vr_0042_v2.id}` is emitted.

Hermes is not directly notified. Instead: a new `SkillVersion` is created (§9); the execution plane is responsible for detecting that a new `SkillVersion` exists and reloading rules at the next invocation.

### 8.4 Test Cases

```
TEST VER-1: Supersession creates new version
  Given: vr_A with status=active, version=1, conditions_hash=H, action="hold"
  When: promote new candidate with same conditions_hash=H, action="send_with_note"
  Then: vr_A.status='deprecated'; new vr_B with version=2, supersedes=vr_A.id, status='active'

TEST VER-2: Rollback is a new row, not a mutation
  Given: vr_A (v1, active), vr_B (v2, active, supersedes=vr_A)
  When: rollback(vr_B)
  Then: vr_B.status='rolled_back'; new vr_C created with rollback_of=vr_B, version=3;
        vr_C has same conditions/action as vr_A; no mutation to vr_A or vr_B.

TEST VER-3: No active rule left orphaned after rollback
  After rollback(vr_B): exactly one active rule exists for (tenant, workflow, decision_type, conditions_hash)
  Assert: count(status='active' WHERE conditions_hash=H) = 1
```

---

## 9. SkillVersion Design — RESOLVED-C2

### 9.0 RESOLVED-C2: ONE name, ONE schema, ONE source of truth

**Devil's Advocate §1.4 demanded a single rule-consumption model. Exec-plane §8 chose model C ("system-prompt context fetched at workflow start"). This spec called it "SkillVersion manifest." They are the same thing.**

**Canonical decisions:**

1. **One artifact name: `SkillVersion`.** The product spec entity list uses this term. Exec-plane's `rule_snapshot_id` and this spec's `skill_version_id` are **the same identifier**. Going forward, the canonical name is `skill_version_id`.

   ```
   skill_version_id == rule_snapshot_id   (alias; exec-plane may keep the second name in code, but they reference the same row)
   ```

2. **One source of truth: `skill_versions` table** in the control-plane DB. SkillVersion manifests are immutable rows. No skill files. No filesystem-resident artifacts. Exec-plane fetches the manifest on every workflow start.

3. **One fetch path: exec-plane's `FetchValidatedRules` activity.** Pull-at-workflow-start is authoritative (RESOLVED-C11). The `FetchValidatedRules` activity:
   - Calls control-plane endpoint `GET /internal/tenants/:tenant_id/skill-versions/active?workflow_slug=...&as_of=<optional>`.
   - Receives a `SkillVersion` manifest (shape below).
   - Records the returned `skill_version_id` into `case_runs.skill_version_id`.
   - This ID is pinned for the duration of the case run; subsequent activities re-use it for consistent rule application within the run.

4. **One versioning rule:** SkillVersion is **immutable**. Every promotion or rollback writes a new SkillVersion row. The active SkillVersion for a `(tenant_id, workflow_slug)` is the row with the highest `version` and `status = 'active'`.

5. **One consumption model — model C from devil's advocate §1.4:** rules are fed via system-prompt context, fetched at workflow start, pinned by `skill_version_id`. **NOT model A (per-call refetch), NOT model B (baked into skill file), NOT model D (hybrid).**

### 9.1 SkillVersion manifest schema

The single canonical schema. This is what `FetchValidatedRules` returns and what is persisted in `skill_versions.rule_manifest`. The execution plane projects this into Hermes's prompt format; the projection is owned by exec-plane (§9.3 below).

```json
{
  "skill_version_id": "sv_r7q1",                  // canonical ID (= rule_snapshot_id)
  "tenant_id": "t_123",
  "workflow_template_id": "wt_quote_roofing",
  "workflow_slug": "quote_drafting",
  "version": 4,
  "vertical": "roofing",                          // tenant's vertical at SkillVersion generation time
  "generated_at": "2026-04-27T15:30:00Z",
  "rule_manifest": [
    {
      "rule_id": "vr_0042",
      "scope": "tenant",
      "version": 1,
      "decision_type": "send_or_hold",
      "conditions_canonical": [
        {"field": "photos_complete", "operator": "=", "value": false},
        {"field": "client_type", "operator": "=", "value": "new"}
      ],
      "recommended_action": "hold_and_request_more_info",
      "rationale": "Always hold quote for new clients when photos are incomplete.",
      "priority": 10
    },
    {
      "rule_id": "vr_0017",
      "scope": "vertical",
      "version": 2,
      "decision_type": "template_selection",
      "conditions_canonical": [
        {"field": "enquiry_type", "operator": "=", "value": "commercial"}
      ],
      "recommended_action": "use_corporate_template",
      "rationale": "Commercial enquiries get the corporate template.",
      "priority": 5
    }
  ]
}
```

`priority` is computed at manifest generation: `case` > `tenant` > `vertical` > `default`; within same scope, `promoted_at DESC`.

### 9.2 RESOLVED-C11: Pull at workflow-start is authoritative

**Conflict:** Ctrl-plane §7.6 proposed `POST /internal/rules/push` from control plane to execution plane on each promotion. Exec-plane §8.1 specified pull at workflow-start via `FetchValidatedRules`.

**Resolution:**
- **Pull at workflow-start is the authoritative path.** This is what creates the immutable `skill_version_id` snapshot on `case_runs`.
- **Push is downgraded to a non-authoritative cache-warming hint.** The control plane MAY notify execution planes when a new SkillVersion is generated, allowing exec-plane to invalidate any cached manifest before the next workflow starts. Exec-plane MAY ignore the push without correctness impact — the next pull will fetch the latest.
- The "ack means active" contract from ctrl-plane §7.6 step 6 changes meaning: ack indicates the rule is **available for the next pull**, not that it is loaded into running workflows. In-flight workflows use the SkillVersion they snapshotted at start.

This means: **a rule promoted mid-workflow does NOT affect that workflow.** This matches exec-plane §8.2's stated behavior and is required for replay determinism (§10).

### 9.3 RESOLVED-C14: Field-name alignment between manifest and prompt block

**Conflict.** Exec-plane R2 §8.3's prompt block uses `if_conditions`/`then_action`. This spec's manifest (§9.1) uses `conditions_canonical`/`recommended_action` — the same names as the underlying schema columns on `validated_rules`.

**Resolution (R3).** The manifest names `conditions_canonical`/`recommended_action` hold. They are schema column names; renaming them at the manifest layer would force a translation layer between the DB row and the manifest, with the rename living in two places (resolver query + manifest serializer) and tests of the manifest having to reverse-translate.

**Concession to exec-plane.** Exec-plane MAY locally project the manifest into its prompt-friendly names at `InvokeHermesReasoning` time. The projection is:

```
prompt_rule = {
  rule_id:        manifest_rule.rule_id,
  decision_type:  manifest_rule.decision_type,
  if_conditions:  manifest_rule.conditions_canonical,
  then_action:    manifest_rule.recommended_action,
  scope:          manifest_rule.scope
}
```

This projection is **owned by exec-plane**, lives in the prompt-template wrapper, and is invisible to the rest of the system. The wire format that crosses the spec boundary (the `SkillVersion.rule_manifest`) uses this spec's names. Storage/DDL/audit fields all use this spec's names. Exec-plane's R3 §8.3 should describe the local projection so the field-name divergence is contained inside one component.

**Why not adopt exec-plane's names at the manifest layer:**
- `conditions_canonical` (plural, "canonical" prefix) signals that this is the post-canonicalization output of §4.2. `if_conditions` loses that signal.
- `recommended_action` is a domain term used in the product spec and across this spec's algorithm sections. `then_action` is a presentation choice for the prompt, not a domain choice.
- The manifest is consumed by potential future readers (replay diff, observability dashboards, regression test harness). Tying the wire format to a single consumer's prompt-readability preference creates coupling without payoff.

The contract test for C14: read a `validated_rules` row, build the manifest, assert manifest fields match the schema column names. Then run exec-plane's projection and assert the prompt block has `if_conditions`/`then_action`. Both layers tested separately.

### 9.4 SkillVersion lifecycle trigger

A new `SkillVersion` row is generated when any of:
1. A `ValidatedRule` is promoted (rule added to the manifest).
2. A `ValidatedRule` is deprecated or rolled back.
3. A tenant's vertical assignment changes (vertical-scope rules in the manifest change).

The trigger is in the control-plane promotion pipeline (§7.4). The new SkillVersion is generated atomically in the same transaction as the rule write.

SkillVersions are **never mutated**. To "update" a SkillVersion, generate a new one with a higher `version`.

### 9.5 SkillVersion lookup with `as_of` (replay support)

`FetchValidatedRules` accepts an optional `as_of` timestamp:
- `as_of` absent → returns the current active SkillVersion.
- `as_of = <ts>` → returns the SkillVersion that was active at `ts` (used for historical replay; see §10).

Lookup query:
```sql
SELECT * FROM skill_versions
WHERE tenant_id = $tenant_id
  AND workflow_slug = $workflow_slug
  AND generated_at <= $as_of
  AND (deprecated_at IS NULL OR deprecated_at > $as_of)
ORDER BY version DESC LIMIT 1
```

---

## 10. Replay Semantics

### 10.1 What is a replay?

A replay is a new `CaseRun` with `replayed_from_id` set to the original, executed against the current active `SkillVersion` (or a specified `SkillVersion`). The input payload is exactly the same (`input_hash` must match).

### 10.2 What is deterministic, what is not

| Component | Deterministic? | Notes |
|---|---|---|
| Input payload | Yes | Same `input_hash` required |
| SkillVersion / rule set | Yes | Recorded in `case_runs.skill_version_id` |
| Decision point sequence | Partially | Same decision types in same order, but LLM may produce varying verbatim text |
| Artifact byte content | No | LLM generation is non-deterministic; artifact bytes will vary |
| Decision-point outcomes (action chosen, branch taken) | Should be deterministic given rules + input | This is the regression contract |
| Extracted facts | Should be stable | Fact extraction from structured inputs is largely deterministic |

**The regression contract is at the decision-point outcome level, not at the artifact byte level.**

### 10.3 Replay comparison algorithm

```
compare_replay(original_case_run_id, replay_case_run_id):
  for each (original_dp, replay_dp) in zip(original_decision_points, replay_decision_points):
    where original_dp.decision_type = replay_dp.decision_type
    and   original_dp.sequence_number = replay_dp.sequence_number
    compare:
      agent_output.action_taken  -- "send_quote" vs "hold_and_request_more_info"
      agent_output.branch_taken  -- for routing decisions
      agent_output.extracted_facts -- structured fact extraction output
```

**Regression = a case where `original_dp.agent_output.action_taken != replay_dp.agent_output.action_taken` and the original was previously `approved` or a reference run.**

### 10.4 Replay diff artifact

A replay diff is stored as an `Artifact{artifact_type: 'replay_diff'}`:

```json
{
  "original_case_run_id": "cr_456",
  "replay_case_run_id": "cr_612",
  "skill_version_id": "sv_r7q1",
  "decision_point_diffs": [
    {
      "sequence_number": 2,
      "decision_type": "send_or_hold",
      "original_action": "send_quote",
      "replay_action": "hold_and_request_more_info",
      "is_regression": false,   // replay_action matches a validated rule → improvement, not regression
      "applied_rule_id": "vr_0042"
    }
  ],
  "summary": {
    "improved_count": 1,
    "regressed_count": 0,
    "unchanged_count": 3
  }
}
```

`is_regression = true` only when the replay action diverges from an approved reference AND does not match a ValidatedRule recommendation. A change that aligns with a validated rule is classified as an **improvement**, not a regression.

### 10.5 Invariants — RESOLVED-C5

```
INVARIANT REPLAY-1: Same input
  replay.input_hash must equal original.input_hash.
  Violation must block the replay (not silently continue with different input).

INVARIANT REPLAY-2: Immutable original
  A CaseRun that is replayed must not be modified. The original's outcome, decision_points,
  and artifacts are frozen. Only replayed_case_run_id is added via a FK, never in-place edit.
  (Aligns with exec-plane INV-CR2.)

INVARIANT REPLAY-3: Regression definition is stable
  The definition of "regression" (action diverges from approved reference, not matching a
  ValidatedRule) must be implemented as a pure function of the diff data — not dependent on
  any mutable state at the time of comparison.

INVARIANT REPLAY-4: skill_version_id pin (RESOLVED-C5)
  Every replay receives an explicit skill_version_id (from §9.5 lookup). The replay activity
  passes this ID into FetchValidatedRules; the manifest is fetched at that pinned version,
  not at the current active version. Same-skill_version_id + same-input → same decision-point
  outcomes. This is the core determinism guarantee.

INVARIANT REPLAY-5: LLM determinism configured by exec-plane
  Replay LLM calls must run with temperature=0 (or provider-equivalent maximum-determinism
  setting). Exec-plane is the implementer (it owns InvokeHermesReasoning); this spec owns
  the contract test that asserts identical decision-point outcomes given identical inputs
  and identical skill_version_id. (Devil's advocate UE-03 satisfied by this pairing.)
```

### 10.6 Replay determinism contract (RESOLVED-C5 detail)

The pass/fail criterion for a "deterministic replay":

**Given:** original `CaseRun` with `id = cr_X`, `input_hash = H`, `skill_version_id = sv_S`.
**When:** replay started with `replay_of = cr_X`, same `input_hash = H`, same `skill_version_id = sv_S` (passed explicitly to `FetchValidatedRules`).
**Then (PASS):** for every decision point in the original at sequence number N with `decision_type = T`:
- The replay produces a decision point at sequence number N with `decision_type = T`.
- `replay_dp.agent_output.action_taken == original_dp.agent_output.action_taken`.
- `replay_dp.agent_output.branch_taken == original_dp.agent_output.branch_taken`.
- `replay_dp.agent_output.extracted_facts` matches structurally (same keys, same values; whitespace and ordering normalized).

**Excluded from the determinism contract:**
- Artifact byte equivalence (LLM-generated text varies; this is acceptable).
- Timestamps in artifact metadata.
- LLM-generated rationale strings (free-text fields are not part of the deterministic contract).

This contract is owned and tested by this spec (golden test §13.3 REPLAY TEST-1, test 5). Implementation responsibility is split:
- **Exec-plane:** ensures `temperature=0`, ensures the SkillVersion fetch uses the pinned `skill_version_id`.
- **This spec:** owns the comparison algorithm and the contract test.

### 10.7 Golden cases

Each `WorkflowTemplate` should have a set of golden case input payloads stored as test fixtures. A replay of these golden cases against any new `SkillVersion` constitutes the regression test suite. Golden cases are owned by the Control Plane (they live in the control plane DB or test fixture store), but their format must match the `CaseRun.input_payload` schema exactly.

---

## 11. AuditEvent Model

### 11.0 Audit storage topology (R4 architecture, R5 semantics formalized)

**Single authoritative store in the control-plane DB. Per-tenant `audit_events_outbox` is a durable write-through buffer for outage tolerance only, not a read source.** This collapses the R3 dual-read topology in favor of ctrl-plane R3 §9.3's design (see §2.0.1 for the reconciliation rationale).

| Store | Role | Authority |
|---|---|---|
| Control-plane `audit_events` | System of record; partitioned by `tenant_id` then `occurred_at` (monthly); served via federated query API; MCP synchronous reads via `mcp_audit_reader` role + RLS over mTLS | **Authoritative** |
| Per-tenant `audit_events_outbox` | Durable write-through buffer (transactional with business write); drains upstream to authoritative store; never read by MCP | NOT authoritative; non-readable from outside the drain worker |

**Write protocol (every exec-plane audit write):**

```
BEGIN;
  INSERT INTO corrections (...);                           -- business write
  INSERT INTO audit_events_outbox (event_type, ..., 
                                    idempotency_key, drained=false);
COMMIT;
-- Drain worker polls audit_events_outbox WHERE drained=false ORDER BY id;
-- For each: POST /internal/audit/events to control-plane (mTLS; idempotent on idempotency_key);
-- On 200 ack: UPDATE audit_events_outbox SET drained=true WHERE id=$id;
-- After 24h post-drained, the row is purged (operational housekeeping).
```

The outbox row exists only until drained + 24h; it is not retained for replay or read access. Control-plane is queried for any historical lookup.

**Read protocol (MCP synchronous WRITE_EXTERNAL preflight):**

The MCP server connects to the **control-plane DB** as `mcp_audit_reader` over mTLS (intra-VPC). RLS limits visible rows to `event_type='approval_received' AND tenant_id=current_setting('app.current_tenant')`. The query is:

```sql
SELECT 1 FROM audit_events
WHERE event_type = 'approval_received'
  AND case_run_id = $case_run_id
  AND decision_point_id = $decision_point_id
LIMIT 1;
```

The mTLS round-trip latency (~5–10ms intra-VPC) is acceptable on the WRITE_EXTERNAL hot path. Surfaced as **OPEN-AUDIT-LATENCY-R4** for post-MVP measurement; if the latency proves load-bearing, the fallback is to reintroduce the local outbox as a read source with explicit drain-lag semantics (R3 design).

**Control-plane-only events** (provisioning, promotion, reviewer actions, vertical aggregation) write directly to the control-plane store with no outbox.

**Invariants (R4 + R5 formalization):**

- `INV-AUDIT-WRITE-THROUGH-R4`: Every audit row in the control-plane store with `source='execution_plane'` was previously written to a per-tenant outbox row (now drained). The outbox enforces durability across control-plane outages.
- `INV-AUDIT-READ-SINGLE-STORE-R4`: All audit reads (federated query, MCP preflight, observability) hit the **control-plane DB** only. No reader queries the outbox.
- `INV-AUDIT-OUTBOX-NOT-AUTHORITATIVE-R4`: A row drained and purged from the outbox does NOT cause its presence in the control-plane store to disappear. Disaster recovery rebuilds outboxes from control-plane; the converse is not required.
- `INV-AUDIT-OUTBOX-SOLE-READER-R5`: The drain worker (running in the execution plane) is the **sole reader** of `audit_events_outbox`. No service — including MCP servers, replay workflows, observability, or operator-facing APIs — reads from the outbox. The outbox is non-readable from outside the drain process. This invariant is enforced by Postgres role grants: only the `drain_worker` role has SELECT on the outbox; all other roles (including `audit_writer`, `mcp_audit_reader`, `audit_reader`) have INSERT-only or no grant.
- `INV-AUDIT-OUTBOX-AT-LEAST-ONCE-R5`: The drain protocol is at-least-once. The control-plane endpoint `POST /internal/audit/events` MUST be idempotent on `idempotency_key` (`INSERT ... ON CONFLICT (idempotency_key) DO NOTHING` per ctrl-plane §9.4). A drain retry after a partial failure may resubmit a row whose first attempt actually committed; the duplicate is silently absorbed at the control-plane store.

**R5 — formalized outbox semantics (per moderator brief task #2):**

| Property | Specification |
|---|---|
| **Reader** | Drain worker only (one process per execution plane). No other service has SELECT on the outbox. Postgres grant: `GRANT SELECT, UPDATE(drained, drained_at) ON audit_events_outbox TO drain_worker`. |
| **Writer** | All exec-plane services that emit audit events (Temporal worker, MCP servers) via the `audit_writer` role. INSERT only on the outbox; the outbox shares the BEFORE UPDATE/DELETE/TRUNCATE trigger with the control-plane `audit_events` (immutable except for the `drained` flag, which is mutated only by the drain worker). |
| **Delivery semantics** | At-least-once with idempotent absorption at control plane. If the drain POST receives a 200 ack, the row is marked `drained=true`. If the POST fails (network, 5xx), the row remains `drained=false` and is retried on the next drain cycle. |
| **Dedup key** | `audit_events.idempotency_key` (UNIQUE in control-plane store). Computed per ctrl-plane §17 as `sha256(tenant_id ‖ event_type ‖ ref_id ‖ sequence_or_timestamp)`. The outbox row uses the same key; duplicate POSTs return 200 with no insert. |
| **Durability boundary** | An audit row is durable in two places once drained: the per-tenant outbox (until purged at drained+24h) and the control-plane authoritative store (permanent). Before drain, durability is **per-tenant outbox only**. The business write (e.g., `corrections` INSERT) and outbox write are in the **same transaction**, so a committed business write implies a durable outbox row. |
| **Control-plane outage handling** | The outbox grows; the drain worker emits a metric on outbox depth. An alert fires (via `POST /internal/alerts` per ctrl-plane §9.6.1) if depth exceeds a configured threshold (proposed default: 10,000 rows). On recovery, the drain worker resumes from the lowest `drained=false` row in FIFO order. |
| **Drain worker failure** | If the drain worker itself dies, outbox rows remain `drained=false` indefinitely; the worker restarts and resumes. Outbox writes continue uninterrupted. |
| **Permanent rejection** | A row that is rejected by the control-plane store with a non-retryable error (e.g., schema-version mismatch, invalid `event_type`) is moved to a `audit_events_outbox_dead_letter` table with the `last_error` column populated; an alert fires. The dead-letter table is reviewer-inspectable but never auto-replayed. |
| **Retention of drained rows** | Drained rows are purged from the outbox 24h after `drained_at`. The 24h window supports operational reconciliation (e.g., asking "did this event actually drain?"). After purge, the row exists only in the control-plane authoritative store. |

This satisfies devil's advocate UE-04 (storage-layer immutability — applied at the control plane; the outbox uses the same `audit_writer` role with INSERT-only privilege per §11.5) and the moderator's overview §A8 ("single authoritative store in control plane; exec-plane writes upstream via durable buffer") — the outbox is the durable buffer.

### 11.1 Design principles

- **Immutable and append-only.** No UPDATE or DELETE on the audit table. Corrections are handled by a new event, not a row mutation.
- **Every entity write has an audit event.** No "dark" writes.
- **Single authoritative store** (RESOLVED-C9): control-plane `audit_events`, partitioned by `tenant_id`.
- **Single writer per event type.** Each `event_type` has exactly one system component responsible for emitting it (§11.6). This prevents duplicate attribution.

### 11.2 Schema

```sql
CREATE TABLE audit_events (
  id                  TEXT        NOT NULL,                  -- UUID v4
  tenant_id           TEXT,                                 -- NULL for control-plane-only events (e.g. tenant-create)
  event_type          TEXT        NOT NULL,                 -- see event type registry §11.6
  actor_type          TEXT        NOT NULL
                        CHECK (actor_type IN ('operator', 'system', 'reviewer', 'hermes', 'admin')),
  actor_id            TEXT        NOT NULL,                 -- matches actor_type semantics
  -- PII flag: actor_id for operator events may contain messaging identifiers (e.g. "op_whatsapp:<id>").
  -- Per ctrl-plane §9.5: phone numbers must NOT be embedded; use the internal identity ID.
  ref_entity_type     TEXT        NOT NULL,                 -- e.g. "correction", "rule_candidate"
  ref_id              TEXT        NOT NULL,                 -- ID of the primary entity
  related_ids         JSONB       NOT NULL DEFAULT '{}',    -- {"correction_id": ..., "case_run_id": ...}
  payload             JSONB       NOT NULL DEFAULT '{}',    -- event-specific data
  idempotency_key     TEXT        NOT NULL,                  -- caller-supplied; per ctrl-plane §9.3; uniqueness via composite constraint below
  trace_id            TEXT,                                 -- OpenTelemetry trace ID
  span_id             TEXT,                                 -- OpenTelemetry span ID
  source              TEXT        NOT NULL                  -- "execution_plane" | "control_plane"
                        CHECK (source IN ('execution_plane', 'control_plane')),
  occurred_at         TIMESTAMPTZ NOT NULL,                 -- when event happened (in originating component)
  ingested_at         TIMESTAMPTZ NOT NULL DEFAULT now()    -- when control plane recorded it
  -- Note: composite partition key requires tenant_id + occurred_at in PK and UNIQUE constraints.
  , PRIMARY KEY (id, tenant_id, occurred_at)
  , UNIQUE (idempotency_key, tenant_id, occurred_at)
) PARTITION BY LIST (tenant_id);
-- Each tenant gets a list partition, sub-partitioned by RANGE on occurred_at (monthly).
-- A DEFAULT partition catches control-plane-only events where tenant_id IS NULL.
--
-- Example partition DDL:
--   CREATE TABLE audit_events_t_a PARTITION OF audit_events FOR VALUES IN ('t_a')
--     PARTITION BY RANGE (occurred_at);
--   CREATE TABLE audit_events_t_a_2026_04 PARTITION OF audit_events_t_a
--     FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
--   CREATE TABLE audit_events_default PARTITION OF audit_events DEFAULT
--     PARTITION BY RANGE (occurred_at);

CREATE INDEX ae_tenant_type_idx ON audit_events (tenant_id, event_type);
CREATE INDEX ae_ref_idx ON audit_events (ref_entity_type, ref_id);
CREATE INDEX ae_occurred_at_idx ON audit_events (occurred_at DESC);
CREATE INDEX ae_idempotency_idx ON audit_events (idempotency_key);

-- Partitioning: LIST by tenant_id, then 1-month RANGE sub-partitions on occurred_at;
-- 13-month rolling window; archived partitions exported to WORM-locked S3
-- (per ctrl-plane §9.3 "WORM-locked" archives).
```

### 11.3 Event type registry — RESOLVED-AUDIT-CANON (R3 canonical)

**Naming convention (R3 lock):** all `event_type` values are `lower_snake_case`. Ctrl-plane R2 used UPPER_SNAKE in some examples (`CASE_RUN_STARTED`, `RULE_PROMOTED`); ctrl-plane R3 will normalize to lower_snake_case.

The exhaustive registry below covers every event written by ANY spec. Columns:
- **Emitter (writer)** — exactly one service is permitted to insert this event_type. Enforced by the writer-registry contract test (§11.7).
- **Store** — `local` (per-tenant exec-plane DB, written first), `control` (control-plane DB only), `both` (write-through per §11.0).
- **`ref_entity_type`** — the primary entity the event references.

| `event_type` | `actor_type` | Emitter (writer) | Store | `ref_entity_type` | Origin spec |
|---|---|---|---|---|---|
| **Case lifecycle (exec-plane)** | | | | | |
| `case_run_started` | `system` | Exec-plane Temporal worker | both | `case_run` | exec §3.4 |
| `case_run_completed` | `system` | Exec-plane Temporal worker | both | `case_run` | exec §3.4 |
| `case_abandoned` | `system` | Exec-plane Temporal worker (timeout) | both | `case_run` | exec §3.5, devils-adv UE-07 |
| `awaiting_timeout` | `system` | Exec-plane Temporal worker (signal-wait timeout) | both | `case_run` | exec §3.5 |
| `replay_started` | `system` | Exec-plane Temporal worker | both | `case_run` | exec §4.3 |
| `replay_completed` | `system` | Exec-plane Temporal worker | both | `case_run` | exec §4.3 |
| **Approval & correction (exec-plane)** | | | | | |
| `approval_received` | `operator` | Exec-plane `WriteApprovalAuditEvent` activity (writes synchronously before any `write_final` activity per exec §5.6) | both | `decision_point` | exec §5.6 |
| `correction_received` | `operator` | Exec-plane `PersistCorrection` activity (RESOLVED-C10) | both | `correction` | this §3.5 |
| `correction_approve` | `operator` | Exec-plane `PersistCorrection` activity | both | `correction` | this §3.5 |
| `correction_parse_failed` | `system` | Candidate-matcher (Stage B; this spec) | both | `correction` | this §3.6 |
| **Gateway (operator-ux)** | | | | | |
| `packet_sent` | `system` | Operator-ux gateway (`audit_writer` against control plane via internal API; gateway has no per-tenant DB access) | control | `case_run` | op-ux §10 |
| `packet_delivery_failed` | `system` | Operator-ux gateway | control | `case_run` | op-ux §11 |
| `correction_expired` | `system` | Operator-ux gateway | control | `case_run` | op-ux §10.2 |
| `stale_reply` | `system` | Operator-ux gateway | control | `case_run` | op-ux §9.4 |
| `security_violation_inbound` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4) — inbound `tenant_id` payload disagrees with `channel_bindings`. R5 rename from R4 `tenant_binding_mismatch` to match op-ux's canonical R4 name. |
| `rate_limit_hit` | `system` | Operator-ux gateway | control | `tenant` | op-ux §4.8 |
| `channel_session_disconnected` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4) — channel-agnostic name (covers WhatsApp + Telegram + future channels). R5 rename from R4 `wa_session_disconnected`. |
| `channel_session_recovered` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4). R5 rename from R4 `wa_session_reconnected`. |
| `channel_session_suspended` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4) — disconnect > 24h or abuse-triggered. R5 rename from R4 `wa_session_suspended`. |
| `channel_session_disconnect_warning` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 + ctrl-plane §9.3 alert path. R5 rename from R4 `wa_session_disconnect_warning`. |
| `packet_tombstoned` | `system` | Operator-ux gateway | control | `case_run` | op-ux §9.7 (R4) — outbound queue overflow drop |
| `correction_resolved_at_gateway` | `system` | Operator-ux gateway | control | `correction` | op-ux §9.7 (R4). Distinct from exec-plane's `correction_received` (persistence to `corrections` table). The gateway event marks "envelope resolved at gateway"; both events exist for an end-to-end correction. R5 rename from R4 `correction_received_at_gateway` to match op-ux R4. |
| `correction_dead_lettered` | `system` | Operator-ux gateway | control | `correction` | op-ux §9.7 (R4) — parse cascade ended at dead-letter |
| `unbound_inbound_dropped` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4) — inbound webhook with unknown `provider_number` |
| `outbound_queue_overflow` | `system` | Operator-ux gateway | control | `tenant` | op-ux §9.7 (R4) |
| `signal_delivery_failed` | `system` | Operator-ux gateway | control | `correction` | op-ux §9.7 (R4) — Temporal `SignalWorkflow` non-retryable error |
| **MCP server gates (exec-plane MCP processes)** | | | | | |
| `sandbox_escape_blocked` | `system` | MCP server (any sandbox) | both | `decision_point` | exec §5.6 |
| `blocked_write_attempted` | `system` | MCP server (live mode, no approval) | both | `decision_point` | exec §5.6 |
| `security_violation` | `system` | MCP server (tenant-id mismatch) | both | `tenant` | exec §5.7 |
| **Candidate lifecycle (this spec)** | | | | | |
| `candidate_created` | `system` | Candidate-matcher | both | `rule_candidate` | this §4 |
| `candidate_evidence_added` | `system` | Candidate-matcher | both | `rule_candidate` | this §4 |
| `candidate_contradiction_detected` | `system` | Candidate-matcher | both | `rule_candidate` | this §4 |
| `candidate_under_review` | `system` | Confidence scorer (this spec) | both | `rule_candidate` | this §7.1 |
| `candidate_near_match_flagged` | `system` | Candidate-matcher | both | `rule_candidate` | this §4 |
| `candidate_stale_marked` | `system` | Candidate TTL background job | both | `rule_candidate` | this §12.3 |
| `candidate_cap_exceeded` | `system` | Candidate-matcher | both | `rule_candidate` | this §12.3 |
| `scope_escalation` | `system` | Candidate-matcher (auto case→tenant) | both | `rule_candidate` | this §6.2 |
| **Promotion / rules (control-plane only)** | | | | | |
| `scope_escalation_reviewer_approved` | `reviewer` | Promotion pipeline (control-plane worker) | control | `rule_candidate` | this §6.2 |
| `rule_promoted` | `reviewer` | Promotion pipeline | control | `validated_rule` | this §7.4 |
| `rule_rejected` | `reviewer` | Rule Review Console | control | `rule_candidate` | this §7.3 |
| `rule_merged` | `reviewer` | Rule Review Console | control | `rule_candidate` | this §7.3 |
| `rule_deprecated` | `system` | Promotion pipeline | control | `validated_rule` | this §8.2 |
| `rule_rolled_back` | `reviewer` | Promotion pipeline (rollback flow) | control | `validated_rule` | this §8.3 |
| `skill_version_created` | `system` | Promotion pipeline | control | `skill_version` | this §9.4 |
| `vertical_aggregate_computed` | `system` | Aggregation job (control-plane) | control | `vertical_candidate_aggregate` | this §2.7.2 |
| `aggregate_quarantined` | `system` | Aggregation job (control-plane) | control | `quarantined_aggregate` | this §2.7.2 |
| **Tenant lifecycle (control-plane only)** | | | | | |
| `tenant_provisioned` | `admin` | Provisioning workflow | control | `tenant` | ctrl §5.2 |
| `tenant_suspended` | `admin` | Provisioning workflow | control | `tenant` | ctrl §5.4 |
| `tenant_deprovisioned` | `admin` | Provisioning workflow | control | `tenant` | ctrl §5.5 |

**Adding a new event_type is a contract change:** any new value not in this table is rejected by the writer-registry contract (§11.7). The producer must add a row here first, then implement the writer; ctrl-plane and operator-ux extend their R3 specs to reference any new entries here.

### 11.3.1 Payload shape contract

Every `audit_events.payload` is a JSONB object that conforms to the **base shape** below. Specific event_types extend with additional keys; representative payloads are shown for 5 event_types; the remainder follow the same pattern.

**Base shape (every event):**

```json
{
  "mode": "sandbox | live | null",        // RESOLVED-N1; null for events that pre-date a workflow start
  "schema_version": "1"                    // payload schema version; bumped if shape changes
}
```

**Representative payloads:**

```json
// correction_received
{
  "mode": "sandbox",
  "schema_version": "1",
  "action_button": "wrong_action",
  "free_text_present": true,
  "free_text_length": 38,
  "follow_up_present": true,
  "scope_hint": "tenant",
  "parse_method": "button",
  "parse_confidence": 1.0,
  "parser_owner": "operator-ux"
}
// NOTE: free_text itself is NOT copied into the audit payload. The corrections table holds
// the verbatim text under per-tenant DB protection. Audit records meta-attributes only.

// approval_received
{
  "mode": "live",
  "schema_version": "1",
  "approved_at": "2026-04-27T09:14:00Z",
  "signal_id": "sig_abc",
  "source_message_id": "wamsg_5G3K7"
}

// rule_promoted
{
  "mode": null,
  "schema_version": "1",
  "scope": "tenant",
  "version": 1,
  "supersedes": null,
  "rationale": "3 matching corrections; no contradictions.",
  "reviewer_id": "reviewer:alice@victoria.app"
}

// sandbox_escape_blocked
{
  "mode": "sandbox",
  "schema_version": "1",
  "tool_name": "send_draft_email",
  "blocked_reason": "SANDBOX_MODE",
  "mcp_server": "sandbox_email"
}

// packet_sent
{
  "mode": "sandbox",
  "schema_version": "1",
  "packet_id": "pkt_7a3c",
  "channel": "whatsapp",
  "delivery_status": "delivered",
  "outbound_message_id": "wamsg_outbound_xyz"
}
```

**For all other event_types**, the payload conforms to the base shape plus any event-specific keys the emitter documents in its own spec. The shape contract test (§11.7 below) checks: (a) `mode` is present and matches the §2.0c enum or is null; (b) `schema_version` is present; (c) no field named `tenant_id` appears inside `payload` (tenant_id is a column, not a payload key — prevents accidental nesting bugs).

### 11.4 Span attributes vs. events

| Concern | Mechanism | Rationale |
|---|---|---|
| Timing, latency, service-to-service calls | OpenTelemetry span attribute | Live tracing; high cardinality; not stored in audit_events |
| Business-significant state changes (correction received, rule promoted, rollback) | AuditEvent | Immutable, queryable by entity ID, human-readable |
| LLM token counts, tool call details | Span attribute + log | Not business-significant; too high volume for audit table |
| Error events with business impact (parse_failed, contradiction) | AuditEvent | Need to be surfaced to reviewers |

### 11.5 Immutability — RESOLVED-C8 (storage-layer enforced)

Devil's Advocate UE-04 demanded that immutability be enforced **by the storage layer, not by application convention**. This spec commits to a three-layer defense:

**Layer 1 — Postgres role grants (no privilege to mutate):**

```sql
-- Dedicated writer role: INSERT only, no UPDATE, no DELETE, no TRUNCATE.
CREATE ROLE audit_writer NOLOGIN;
GRANT INSERT ON audit_events TO audit_writer;
-- Explicitly NOT granted: UPDATE, DELETE, TRUNCATE, REFERENCES, CREATE.

-- Dedicated reader role: SELECT only.
CREATE ROLE audit_reader NOLOGIN;
GRANT SELECT ON audit_events TO audit_reader;

-- Application service accounts assume audit_writer; never the table owner role.
-- The table owner is held by a migration role used only by schema migrations.
```

**Layer 2 — BEFORE trigger that raises on UPDATE or DELETE:**

```sql
CREATE OR REPLACE FUNCTION audit_events_immutable_trigger()
RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only; UPDATE/DELETE not permitted (TG_OP=%)', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_no_update
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION audit_events_immutable_trigger();

-- TRUNCATE is also blocked at the table level:
-- (Postgres TRUNCATE bypasses ROW triggers; we rely on the role grant absence above
-- and additionally on a STATEMENT-level trigger:)
CREATE TRIGGER audit_events_no_truncate
BEFORE TRUNCATE ON audit_events
FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable_trigger();
```

**Layer 3 — WORM archive on partition rotation:**
- Partitions older than 13 months are exported to S3 with Object Lock (Compliance mode) before being detached.
- Detached partitions are dropped, not moved; the WORM-locked S3 copy is the long-term record.

**Roles (R3 expanded — RESOLVED-MCP-SNAPSHOT):**

```sql
-- 1. Generic writer role: INSERT only. Used by services that emit any of their permitted
--    event_types (the writer-registry contract enforces per-event_type authorization at the
--    application layer; see §11.7).
CREATE ROLE audit_writer NOLOGIN;
GRANT INSERT ON audit_events TO audit_writer;
-- audit_writer additionally INSERTs into the per-tenant audit_events_outbox per §11.0.
-- The same role name is used in both DBs for clarity; the role is created in each DB.

-- 2. MCP synchronous-read role (R4 — UPDATED to ctrl-plane §9.3 design): scoped narrowly
--    to the events that gate WRITE_EXTERNAL preflight per exec-plane §5.6. The MCP server
--    uses this role for the synchronous SELECT against the **control-plane** audit_events
--    table over mTLS (RESOLVED-AUDIT-TOPOLOGY-R4 — single store, no per-tenant local read).
CREATE ROLE mcp_audit_reader NOLOGIN;
GRANT SELECT ON audit_events TO mcp_audit_reader;
-- RLS narrows visible rows to approval events for the bound tenant only.
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY mcp_audit_reader_approval_only ON audit_events
  FOR SELECT TO mcp_audit_reader
  USING (
    event_type = 'approval_received'
    AND tenant_id = current_setting('app.current_tenant', true)
  );
-- The MCP server's connection sets `SET LOCAL app.current_tenant = '<TENANT_ID>'` on every
-- transaction. The role's SELECT cannot return rows for any other tenant or any other
-- event_type. No grant on UPDATE/INSERT/DELETE.

-- 3. Reader role: full SELECT for observability/admin; assigned only to control-plane
--    services (federated query API, Rule Review Console, Observability stack).
CREATE ROLE audit_reader NOLOGIN;
GRANT SELECT ON audit_events TO audit_reader;

-- 4. RLS policies for audit_reader and audit_writer.
--    RLS is enabled above (line: ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY).
--    Without explicit permissive policies for these roles, the GRANTs above would be
--    dead — RLS denies all rows when no policy matches.
CREATE POLICY audit_reader_select ON audit_events
  FOR SELECT TO audit_reader
  USING (true);  -- full read access; scoping enforced at application layer

CREATE POLICY audit_writer_insert ON audit_events
  FOR INSERT TO audit_writer
  WITH CHECK (true);  -- all inserts permitted; writer-registry authorization enforced at application layer (§11.7)

-- Application service accounts assume the appropriate role. Migration role (CREATE/ALTER)
-- is held only by the schema migration tool and is never assumed by runtime services.
```

**Contract tests (UE-04 / C8 / MCP-SNAPSHOT):**

```
TEST C8-AUDIT-IMMUTABLE-WRITER:
  Connect as audit_writer. Insert audit_events row id='ae_test_001'.
  Attempt UPDATE audit_events SET payload='{}' WHERE id='ae_test_001';
    → expect Postgres error (permission denied OR trigger exception).
  Attempt DELETE FROM audit_events WHERE id='ae_test_001';
    → expect Postgres error.
  Attempt TRUNCATE audit_events;
    → expect Postgres error.

TEST MCP-AUDIT-READER-RLS-SCOPED (R4 — control-plane DB):
  Setup: insert audit_events rows in the control-plane DB:
         {tenant_id:'t_A', event_type:'approval_received', ...}
         {tenant_id:'t_A', event_type:'correction_received', ...}
         {tenant_id:'t_B', event_type:'approval_received', ...}
  Connect as mcp_audit_reader (over mTLS) with SET LOCAL app.current_tenant='t_A':
    SELECT * FROM audit_events;                      -- expect 1 row (t_A approval only)
    SELECT * FROM audit_events WHERE event_type='correction_received';
                                                     -- expect 0 rows (RLS blocks)
    SELECT * FROM audit_events WHERE tenant_id='t_B';
                                                     -- expect 0 rows (RLS blocks)
  With SET LOCAL app.current_tenant='t_B':
    SELECT * FROM audit_events;                      -- expect 1 row (t_B approval only)
  Negative: attempt INSERT/UPDATE/DELETE on audit_events:
    → expect Postgres error (mcp_audit_reader has no DML grants).
```

**Retention:**
- Audit events: retained for **7 years** in WORM-locked archives (financial audit standard; rule promotions and corrections may carry compliance implications). Hot storage retention: 13 months in `audit_events` partitions; older partitions are archived.
- This supersedes Round 1's "tenant lifetime + 90 days" — single retention policy across all events. The 7-year minimum is conservative and covers AU, UK, US base requirements pending legal sign-off (ctrl-plane OQ-05).

### 11.6 Writer registry — RESOLVED-AUDIT-CANON contract

The writer registry below is the authoritative mapping from `event_type` to permitted writer service. **Enforced by the contract test in §11.7** (any service attempting to insert an event_type it is not registered for must be rejected).

| Service / role | event_types permitted |
|---|---|
| `exec_plane.temporal_worker` (writes via `audit_writer` role) | `case_run_started`, `case_run_completed`, `case_abandoned`, `awaiting_timeout`, `replay_started`, `replay_completed`, `correction_received`, `correction_approve`, `approval_received` |
| `exec_plane.mcp_server` (writes via `audit_writer` role) | `sandbox_escape_blocked`, `blocked_write_attempted`, `security_violation` |
| `learning.candidate_matcher` (writes via `audit_writer` role) | `candidate_created`, `candidate_evidence_added`, `candidate_contradiction_detected`, `candidate_near_match_flagged`, `candidate_under_review`, `candidate_stale_marked`, `candidate_cap_exceeded`, `scope_escalation`, `correction_parse_failed` |
| `learning.aggregation_job` (writes via `audit_writer` role) | `vertical_aggregate_computed`, `aggregate_quarantined` |
| `ctrl_plane.promotion_pipeline` (writes via `audit_writer` role) | `rule_promoted`, `rule_deprecated`, `rule_rolled_back`, `skill_version_created`, `scope_escalation_reviewer_approved` |
| `ctrl_plane.rule_review_console` (writes via `audit_writer` role) | `rule_rejected`, `rule_merged` |
| `ctrl_plane.tenant_provisioning` (writes via `audit_writer` role) | `tenant_provisioned`, `tenant_suspended`, `tenant_deprovisioned` |
| `gateway` (operator-ux; writes via ctrl-plane `POST /internal/audit/events` per op-ux §9.7) | `packet_sent`, `packet_delivery_failed`, `packet_tombstoned`, `correction_expired`, `correction_resolved_at_gateway`, `correction_dead_lettered`, `stale_reply`, `rate_limit_hit`, `channel_session_disconnected`, `channel_session_recovered`, `channel_session_suspended`, `channel_session_disconnect_warning`, `security_violation_inbound`, `unbound_inbound_dropped`, `outbound_queue_overflow`, `signal_delivery_failed` |

**Reader contract:**

| Component | Reads |
|---|---|
| Exec-plane MCP servers | `approval_received` events for the bound tenant via `mcp_audit_reader` role + RLS on **control-plane** `audit_events` over mTLS (R4 — RESOLVED-AUDIT-TOPOLOGY-R4) |
| Ctrl-plane federated query API | All events (control-plane store), with reviewer-RBAC at the API layer |
| Observability stack | All events (control-plane store) |
| Hermes | Nothing (rules are fed via SkillVersion manifest) |

### 11.7 Invariants and writer-registry contract test

```
INVARIANT AUDIT-1: Completeness
  Every Correction row must have exactly one AuditEvent with event_type='correction_received'
  referencing it.

INVARIANT AUDIT-2: Promotion chain completeness
  Every ValidatedRule row must have exactly one AuditEvent with event_type='rule_promoted'
  referencing it.

INVARIANT AUDIT-3: Skill version audit
  Every SkillVersion row must have exactly one AuditEvent with event_type='skill_version_created'.

INVARIANT AUDIT-4: Rollback traceability
  Every ValidatedRule with status='rolled_back' must have an AuditEvent with
  event_type='rule_rolled_back' whose payload references that rule's id.

INVARIANT AUDIT-5: No orphan candidates
  Every RuleCandidate with status='promoted' must have a corresponding
  AuditEvent{event_type='rule_promoted'} with related_ids.candidate_id = candidate.id.

INVARIANT AUDIT-6 (R3, RESOLVED-AUDIT-CANON): Writer-registry authorization
  An audit_events row inserted by service S with event_type E is accepted iff (S, E) appears
  in the writer registry table (§11.6). Any other (S, E) pair is rejected before INSERT.

INVARIANT AUDIT-7 (R3): Write-through causal direction
  Every audit_events row in the control-plane store with source='execution_plane' and
  payload.mode IS NOT NULL must have been previously inserted into the originating per-tenant
  audit_events_outbox. Drained=true implies outbox copy exists. (Per §11.0.)

INVARIANT AUDIT-8 (R3): Payload mode field
  audit_events.payload.mode, when present, must equal one of {'sandbox','live'}. Any other
  value (including legacy strings 'shadow','autopilot') is rejected. (RESOLVED-N1.)
```

**Writer-registry contract test (R3 — RESOLVED-AUDIT-CANON):**

```
TEST AUDIT-WRITER-REGISTRY (cross-component, owned by Devil's Advocate):
  For each (service, event_type) pair in the writer registry §11.6:
    Attempt to insert from the service-account's role.
    Assert: INSERT succeeds; row appears in audit_events.
  For each (service, event_type) pair NOT in the registry (negative cases):
    e.g. (exec_plane.temporal_worker, "rule_promoted")
    e.g. (gateway, "candidate_created")
    Attempt the insert.
    Assert: INSERT is rejected.

  Implementation note: enforcement happens at the application's audit-writer wrapper,
  which checks (caller_service_id, event_type) against a hardcoded allowlist before
  issuing INSERT. The wrapper is shared by all writers and is the single enforcement
  point. The Postgres audit_writer role permits the INSERT; the wrapper provides the
  per-event_type discrimination.

  Storage-layer alternative (deferred): create one Postgres role per service
  (audit_writer_exec, audit_writer_gateway, audit_writer_promotion, etc.) and grant
  INSERT on a partitioned/typed view per role. Not adopted for R3 because the
  application-wrapper approach is simpler at MVP scale; R3 documents the wrapper as
  the contract.
```

---

## 12. Failure Modes

### 12.1 Duplicate corrections

**Cause:** WhatsApp message re-delivery; operator double-taps button; retry on timeout.

**Mitigation:** `idempotency_key` with `UNIQUE` constraint on `corrections`. The parsing pipeline checks `idempotency_key` before processing; a duplicate is acknowledged as success (200 OK) but no new DB work is done. The `AuditEvent` is NOT emitted twice.

**Test:**
```
TEST FAIL-1: Duplicate submission with same idempotency_key
  Given: correction corr_A with key K already processed
  When: identical payload with key K submitted again
  Then: HTTP 200 returned; corrections table unchanged; no new AuditEvent emitted;
        rule_candidates evidence_count unchanged
```

### 12.2 Conflicting candidates

**Cause:** Two operators (or the same operator at different times) correct the same decision point in opposite directions.

**Mitigation:** `conflicts_with` array and `contradicting_count` in `rule_candidates`. Both candidates remain in the system. Neither can pass the `no_contradictions_required` guard to reach `under_review` while the other is `active`. A reviewer must resolve the conflict by rejecting one and promoting the other, or promoting a merged/narrower candidate that resolves the conflict.

**The system does NOT automatically discard either candidate.** Both are surfaced to the reviewer with the full evidence trail.

### 12.3 Candidate explosion

**Cause:** Each unique combination of condition values creates a new candidate. A workflow with many independent boolean fields could produce 2^N candidate hashes.

**Mitigations:**
1. **Cap per `(tenant_id, workflow_slug, decision_type)`:** maximum 50 active candidates (status in `candidate` or `under_review`). Excess candidates are flagged for reviewer attention rather than silently dropped.
2. **TTL on single-evidence orphans:** if `evidence_count = 1` and `last_seen_at < now() - candidate_ttl_days`, set `stale_at`. Stale candidates are excluded from match lookups (reducing explosion growth) but retained for audit.
3. **Parse pipeline validation:** `decision_types` in `WorkflowTemplate` limits which `decision_type` values are valid. Corrections referencing unknown `decision_type` values go to `parse_status = 'manual_review'` rather than spawning new candidates.

**Test:**
```
TEST FAIL-2: Candidate cap enforcement
  Given: 50 active candidates for (tenant, "quote_drafting", "send_or_hold")
  When: a new correction with a novel conditions_hash arrives
  Then: no new candidate is created; correction.parse_status = 'manual_review';
        AuditEvent{event_type:'candidate_cap_exceeded'} emitted
```

### 12.4 Reviewer error

**Cause:** Reviewer promotes a candidate that turns out to be wrong (incorrect rule).

**Mitigation:** Rollback operation (§8.3) creates a new `ValidatedRule` version with `status = 'rolled_back'` and a corrective v(N+1). The audit chain captures the reviewer ID and rationale for both the original promotion and the rollback. Rollback triggers a new `SkillVersion`, which the execution plane picks up on next invocation.

**There is no "undo" — rollback is a forward action.** This preserves the immutability of the audit trail.

### 12.5 Parse failure

**Cause:** Operator's `free_text` is ambiguous, empty, or refers to a concept the parsing pipeline cannot extract structured conditions from.

**Mitigation:** Correction row is stored with `parse_status = 'parse_failed'`. An `AuditEvent{event_type: 'correction_parse_failed'}` is emitted. No candidate is created. The operator is NOT notified of the parse failure — the correction is simply stored for manual review. The correction is NOT discarded; it remains queryable for human-assisted annotation.

---

## 13. Test Strategy (TDD)

Tests are grouped by layer. All invariants from preceding sections define the test contracts; this section adds integration and property tests.

### 13.0 Cross-component contract tests (R4 — table at every boundary I own)

Each test below is a **boundary contract test**: input fixture, the side I own, and the expected output. Owned by this spec; integration-suite implementation is Devil's Advocate's domain.

| # | Test name | Input fixture | Boundary | Expected output | Maps to invariant |
|---|---|---|---|---|---|
| **CT-LL-1** | `test_persist_correction_from_signal_alone` | 16-field `ApprovalSignalEnvelope` (per exec-plane §7.4); gateway DB unreachable; envelope action_button=`wrong_action` | `PersistCorrection` activity (exec-plane writer; this spec defines target schema) | (1) Exactly one `corrections` row whose every column matches the field map in §3.3; (2) exactly one `correction_received` audit row in `audit_events_outbox` (drained to control plane); (3) the `corrections.idempotency_key` equals `sha256(tenant_id ‖ decision_point_id ‖ packet_id ‖ action_button)` per ctrl-plane §17 | `INV-A5` (exec-plane); RESOLVED-C13; RESOLVED-C10 |
| **CT-LL-2** | `test_persist_correction_idempotent` | Submit the same envelope twice (same `signal_id` → same SHA-256 `corrections.idempotency_key`) | `PersistCorrection` boundary | First call: `corrections` row inserted + 1 audit row. Second call: `corrections` UNIQUE constraint short-circuits; row count remains 1; `correction_received` audit count remains 1 | `INVARIANT C1` (idempotence); `IDEMP_02` (ctrl-plane §17) |
| **CT-LL-3** | `test_validated_rules_rls_cross_tenant_deny` | Two tenants `t_A`/`t_B`; insert `vr_A1` (tenant scope, t_A), `vr_B1` (tenant scope, t_B), `vr_V1` (vertical scope, vertical='roofing') | `validated_rules` RLS at control-plane DB | Connection with `app.current_tenant='t_A'`: SELECT returns `vr_A1` + `vr_V1` only. Connection with `t_B`: returns `vr_B1` + `vr_V1` only. Connection without tenant_id: returns `vr_V1` only | `RULE_RLS_01..04` (ctrl-plane §13.2); §2.8.3 |
| **CT-LL-4** | `test_validated_rules_scope_consistency_check` | INSERT a row with `scope='vertical'` and `tenant_id='t_A'` (non-NULL) | `validated_rules` CHECK constraint | Postgres raises CHECK violation; the row is not inserted | `INV-05` (DA); §2.8.1 `vr_scope_consistency` |
| **CT-LL-5** | `test_candidate_merge_deterministic` | Sequence of 3 corrections with identical `conditions_hash` arriving in order [A, B, C] vs. [C, B, A] vs. [B, A, C] | Candidate-matcher (§4) | All three orderings produce identical final candidate state: `evidence_count=3`, same `conditions_hash`, same `source_correction_ids` (as a set, not list), same `confidence` value | `PROPERTY MERGE-2` (commutativity); §4.3 MATCH-1 |
| **CT-LL-6** | `test_pii_strip_quarantines_customer_name` | `rule_candidate` in t_A's DB with `conditions_canonical: [{field:"client_name", operator:"=", value:"ABC Realty"}]` | Aggregation job (§2.7.2 classifier) | (1) Zero rows in `vertical_candidate_aggregates` containing `"ABC Realty"`. (2) One row in `quarantined_aggregates` with redacted form `[{field:"client_name", operator:"=", value:"<quarantined:freetext>"}]`. (3) `aggregate_quarantined` audit event emitted with no PII in payload | `INV-05`; §13.5 INV-05b |
| **CT-LL-7** | `test_pii_strip_redacts_email` | `rule_candidate` with value `"alice@abcrealty.com"` | Aggregation job | One row in `vertical_candidate_aggregates` with value `"<email>"` verbatim. The substring `"alice@abcrealty.com"` does not appear anywhere in the row's serialized form | `INV-05`; §13.5 INV-05c |
| **CT-LL-8** | `test_audit_events_immutable_storage` | Insert one `audit_events` row as `audit_writer`; then attempt UPDATE/DELETE/TRUNCATE (a) as `audit_writer`, (b) as the platform migration role with elevated privileges | `audit_events` storage-layer immutability | All three DML attempts raise Postgres errors. UPDATE/DELETE: trigger raises. TRUNCATE: trigger raises. From `audit_writer`: permission denied (no UPDATE/DELETE grant). The row remains queryable; payload bytes unchanged | `AUDIT_IMM_01..04` (ctrl-plane §9.3); §11.5; UE-04 |
| **CT-LL-9** | `test_mcp_audit_reader_rls_scope` | Insert audit_events: t_A approval, t_A correction, t_B approval. Connect as `mcp_audit_reader` with `app.current_tenant='t_A'` | Control-plane `audit_events` RLS for MCP role | SELECT returns exactly 1 row (t_A approval). With `app.current_tenant='t_B'`: returns exactly 1 row (t_B approval). INSERT/UPDATE/DELETE attempts raise (no grants) | `INV-03` (DA); RESOLVED-AUDIT-TOPOLOGY-R4; §11.5 |
| **CT-LL-10** | `test_writer_registry_authorization` | For each (service, event_type) in §11.6: attempt INSERT with that service's role. For each (service, event_type) NOT in the registry: same | `audit_events` writer-registry contract | All registered pairs succeed. All non-registered pairs are rejected by the application-layer wrapper. The wrapper returns a structured error; no row is inserted | `AUDIT-6`; §11.7 |
| **CT-LL-11** | `test_audit_outbox_drains_no_loss` | (cross-spec with exec-plane §13.4) Stop control-plane DB; emit 100 audit events from a Temporal worker test harness; restart control-plane | Outbox → control-plane drain | After restart, exactly 100 rows present in control-plane `audit_events`; `audit_events_outbox` either empty or all rows drained=true; idempotency_key UNIQUE constraint prevents any duplicate inserts during retry storms | `INGEST_04` (ctrl-plane §9.4); RESOLVED-AUDIT-TOPOLOGY-R4 |
| **CT-LL-12** | `test_skill_version_pinning_for_replay` | Promote vr_X at T_0; create case_run cr_A at T_0 (snapshots `skill_version_id=sv_1`); promote vr_Y at T_1 (creates sv_2). Replay cr_A at T_2 with explicit `skill_version_id=sv_1` | `LoadSkillVersion` resolver (§9.5) | Replay receives `rule_manifest` byte-equal to the sv_1 manifest at T_0; vr_Y is NOT in the manifest; replay decision-point outcomes match the original | `INVARIANT REPLAY-4`; §10.5 |
| **CT-LL-13** | `test_skill_manifest_field_parity` | Build a `SkillVersion.rule_manifest` from the resolver SQL output | Manifest schema (§9.1) ↔ `validated_rules` columns | Each manifest entry's keys exactly match the schema column names: `rule_id`, `scope`, `version`, `decision_type`, `conditions_canonical`, `recommended_action`, `priority`. No renames (e.g., no `if_conditions`); exec-plane §8.3 projects locally if needed | RESOLVED-C14 |

### 13.1 Unit / property tests — candidate merge algorithm

```
PROPERTY MERGE-1: Idempotence
  For any correction C, processing C twice yields the same DB state as processing C once.
  (Enforced by idempotency_key uniqueness; property test verifies no side effects.)

PROPERTY MERGE-2: Commutativity of evidence
  Processing corrections [C1, C2] in either order results in the same evidence_count
  and conditions_hash on the target candidate.

PROPERTY MERGE-3: Confidence monotonicity (absent contradictions)
  For all (e in 1..100, c=0): confidence(e+1, 0) >= confidence(e, 0)

PROPERTY MERGE-4: Contradiction causes confidence drop
  For all (e in 2..100, c in 0..e-1): confidence(e, c+1) < confidence(e, c)

PROPERTY MERGE-5: No negative confidence
  For all (e in 0..100, c in 0..100): confidence(e, c) >= 0.0

PROPERTY MERGE-6: Hash stability
  canonicalize(conditions) is a pure function;
  same input always produces same conditions_hash regardless of environment or time.

PROPERTY MERGE-7 (R4): Promotion-threshold floor
  For every correction sequence S whose confidence(S) < under_review_threshold AND
  whose evidence_count(S) < min_evidence_count: the pipeline produces NO ValidatedRule.
  Generated test inputs: random e ∈ [0, 100], c ∈ [0, e+5], distinct case_run_count ∈ [1, e].
  Quickcheck-style property: for the subset where confidence < threshold OR e < min, no
  rule is promoted; the candidate's status is in {candidate, under_review, rejected}.

PROPERTY MERGE-8 (R4): Replay-determinism property
  For any seed (input_hash, skill_version_id) where skill_version_id is pinned and
  the workflow's MCP read-tools are sourced from `mcp_read_snapshot` artifacts (not live):
    Replay 1 produces decision_point outputs DP_1.
    Replay 2 produces decision_point outputs DP_2.
  Property: DP_1.action_taken == DP_2.action_taken AND DP_1.branch_taken ==
  DP_2.branch_taken AND DP_1.extracted_facts ≡ DP_2.extracted_facts (structurally,
  per §10.6 normalization). Implementation requires LLM `temperature=0` (exec-plane).
  Test runs the property over 100 random (skill_version_id × input) combinations.

PROPERTY MERGE-9 (R4): PII classifier — failure mode is QUARANTINE
  Adversarial value generator produces strings classified by intent:
    (a) Unicode lookalikes: "АBC Realty" (Cyrillic A), "ＡＢＣ Realty" (full-width), "ABC​Realty"
        (zero-width space), "Аlice@abcrealty.com" (Cyrillic А)
    (b) Base64-encoded names: base64("ABC Realty"), base64("alice@example.com")
    (c) JSON-embedded names: '{"name":"ABC Realty"}', '[{"customer":"ABC"}]'
    (d) URL-embedded: "https://abcrealty.com/quote", "tel:+61400000000"
    (e) Whitespace tricks: "  ABC Realty  ", "ABC\tRealty", "ABC\nRealty"
  Property: for every adversarial value V, classify(V) returns either REDACT-to-type-token
  OR QUARANTINE. NEVER PASS. The classifier MUST default-deny; any string not matching
  an explicit allowed pattern (boolean, number, enum-listed value, type-redacted) is
  quarantined. The contract test asserts: for the entire generated set, count(PASS) == 0.
```

### 13.1.1 e2e scenario participation — data-model contract tests by SC (R4)

For each Devil's Advocate SC-01..SC-08 (`05-architecture-integration-critique.md` §5), the data-model side contract tests this spec contributes. Other components contribute their own tests (signal envelope, MCP gates, gateway dedup, etc.) per their own specs.

| Scenario | Data-model contract tests this spec owns | DA R3 audit verdict | Now testable per R4? |
|---|---|---|---|
| **SC-01** Golden-path correction | CT-LL-1 (PersistCorrection writer); CT-LL-2 (idempotency); GOLDEN PROMOTE-1 (§13.2); CT-LL-13 (manifest field parity) | "PARTIALLY TESTABLE" (R3) — C13 envelope + C12 storage | **YES.** C13 envelope locked at exec-plane §7.4; C12 unified store locked at §2.8 + ctrl-plane §13. |
| **SC-02** Multi-correction promotion | CT-LL-5 (merge determinism); GOLDEN PROMOTE-1; PROPERTY MERGE-2 (commutativity); PROPERTY MERGE-7 (threshold floor) | "PARTIALLY TESTABLE" (R3) | **YES.** Resolver is single-DB (§2.0a); promotion path uses unified `validated_rules`. |
| **SC-03** Contradicting-correction supersession | GOLDEN PROMOTE-2 (§13.2); VER-1, VER-2, VER-3 (§8.4) | "TESTABLE" (R3) | YES (held). |
| **SC-04** Abandoned packet | (gateway/exec-plane own primary tests) — this spec contributes: assertion that no `corrections` row OR `correction_received` audit row exists for the abandoned `case_run_id` | "TESTABLE" (R3) | YES (held). |
| **SC-05** Tenant isolation leak attempt | CT-LL-3 (validated_rules RLS cross-tenant deny); CT-LL-4 (scope CHECK); CT-LL-6 + CT-LL-7 (PII strip on cross-tenant aggregate); INV-05a/b/c/d (§13.5) | "PARTIALLY TESTABLE" (R3) — N3 condition-value scrub unwritten | **YES.** N3 classifier locked at §2.7.2 + §13.5 R3 strengthened tests. |
| **SC-06** Sandbox / approval-bypass | CT-LL-8 (audit immutability); CT-LL-9 (mcp_audit_reader RLS); CT-LL-11 (outbox no-loss). The MCP gate itself is exec-plane's §10.4 test; this spec proves the data-model preconditions hold | "NOT TESTABLE in implementation" (R3) — `approval_received` event, `mcp_audit_reader` role, `mcp_read_snapshot` artifact_type all missing | **YES.** R3 added all three (§11.3 registry, §11.5 role, §2.5 enum). R4 collapses MCP read to control-plane DB per RESOLVED-AUDIT-TOPOLOGY-R4. |
| **SC-07** Replay determinism | CT-LL-12 (skill_version pinning); PROPERTY MERGE-8 (replay-determinism property); REPLAY TEST-1..4 (§13.3) | "PARTIALLY TESTABLE" (R3) — `mcp_read_snapshot` enum + temperature test missing | **YES.** R3 added `mcp_read_snapshot` (§2.5); exec-plane R3 §10.3 added the temperature test (`test_replay_uses_temperature_zero`). PROPERTY MERGE-8 codifies the joint property. |
| **SC-08** Double-tap approval idempotency | CT-LL-2 (idempotency on SHA-256 key) — joint with operator-ux CT-Inbound-04 and exec-plane `test_approved_signal_idempotency` | "TESTABLE" (R3) | YES (held). |

**Summary of R4 testability progress:**
- SC-01, SC-02 moved from PARTIALLY → fully TESTABLE.
- SC-05 moved from PARTIALLY → fully TESTABLE.
- SC-06 moved from NOT TESTABLE → fully TESTABLE (the major R4 unblock).
- SC-07 moved from PARTIALLY → fully TESTABLE.
- All 8 scenarios are now fully testable against the four spec bodies.

### 13.2 Golden tests — promotion

```
GOLDEN PROMOTE-1: Standard promotion flow
  Given: 3 corrections, same conditions_hash, no contradictions, scope_hint='tenant'
  When: pipeline runs; confidence crosses 0.50
  Then: status='under_review'; Rule Review Console receives expected payload shape;
        on approve: validated_rule created with version=1, status='active';
        skill_version created with rule in manifest;
        audit chain complete (5 AuditEvents, all linked)

GOLDEN PROMOTE-2: Promotion with supersession
  Given: active vr_A (v1) for (tenant, workflow, decision_type, conditions_hash=H)
  When: new promotion for same conditions_hash=H with different recommended_action
  Then: vr_A.status='deprecated'; new vr_B version=2, supersedes=vr_A.id

GOLDEN PROMOTE-3: Reject flow
  Given: candidate under_review
  When: reviewer rejects with rationale
  Then: candidate.status='rejected'; no ValidatedRule created; AuditEvent emitted

GOLDEN PROMOTE-4: Rollback
  Given: vr_B (v2, active), vr_A (v1, deprecated, superseded by vr_B)
  When: reviewer rolls back vr_B
  Then: vr_B.status='rolled_back'; new vr_C (v3) created with same conditions/action as vr_A;
        vr_C.rollback_of=vr_B.id; new SkillVersion created; exactly one active rule for conditions_hash
```

### 13.3 Replay determinism tests

```
REPLAY TEST-1: Input hash consistency
  Given: two replays of the same case_run_id
  Then: both have input_hash = original.input_hash

REPLAY TEST-2: Regression detection
  Given: a golden case run where dp_X.agent_output.action_taken = "send_quote" (approved)
  When: replayed after promoting vr_0042 which recommends "hold"
  Then: diff shows dp_X action changed from "send_quote" to "hold";
        is_regression = false (change aligns with ValidatedRule)

REPLAY TEST-3: Actual regression
  Given: a golden case run where dp_Y.agent_output.action_taken = "send_quote" (approved)
  When: replayed after a SkillVersion update that should NOT affect decision_type of dp_Y
  Then: if dp_Y.action_taken changes, is_regression = true;
        alert emitted to observability stack

REPLAY TEST-4: Immutability of original
  Given: case run cr_A replayed as cr_B
  Then: cr_A row unchanged (same status, same decision_point rows, same artifact content_hashes)
```

### 13.4 Audit completeness invariants (run as scheduled DB checks)

These SQL checks run against the **single control-plane `audit_events` table** (RESOLVED-C9). The corrections existence check requires a per-tenant DB iteration since `corrections` lives per-tenant. Implementation: a control-plane scheduled job iterates each tenant DB and joins to control-plane audit; alerts on any tenant returning non-zero.

```sql
-- AUDIT-CHECK-1: Every correction has exactly one audit event
-- (Run per tenant DB; cross-references control plane audit_events.)
-- Per-tenant DB query:
SELECT c.id, c.tenant_id FROM corrections c
WHERE c.created_at >= now() - INTERVAL '13 months';
-- For each row returned, control plane verifies:
SELECT 1 FROM audit_events ae
WHERE ae.ref_entity_type = 'correction'
  AND ae.ref_id = $correction_id
  AND ae.tenant_id = $tenant_id
  AND ae.event_type IN ('correction_received', 'correction_approve', 'correction_parse_failed');
-- Any correction with no matching audit event is an alert.

-- AUDIT-CHECK-2: Every promoted ValidatedRule has a rule_promoted event (control plane only)
SELECT vr.id FROM validated_rules vr
WHERE vr.status != 'rolled_back'
AND NOT EXISTS (
  SELECT 1 FROM audit_events ae
  WHERE ae.event_type = 'rule_promoted'
    AND ae.ref_id = vr.id
);

-- AUDIT-CHECK-3: Every active SkillVersion has a skill_version_created event
SELECT sv.id FROM skill_versions sv
WHERE NOT EXISTS (
  SELECT 1 FROM audit_events ae
  WHERE ae.event_type = 'skill_version_created'
    AND ae.ref_id = sv.id
);

-- AUDIT-CHECK-4: Storage-layer immutability test (run daily as a contract test, not a query)
-- See §11.5 contract test description. Connects as audit_writer role, attempts UPDATE,
-- asserts the database raises an error.

-- AUDIT-CHECK-5 (INV-05): Vertical aggregate must not contain any contributing tenant_id
-- Run after each vertical_candidate_aggregates refresh:
-- The refresh job records (in process memory only) the set of contributing tenant_ids; the
-- contract test verifies no aggregate row's JSON-serialized representation contains any
-- of those tenant_ids as a substring. This is a defense-in-depth check; the schema
-- itself does not have a tenant_id column on vertical_candidate_aggregates.
```

### 13.5 INV-05 contract test — cross-tenant promotion strips identifiers AND value PII (R3 strengthened)

Devil's advocate INV-05 (R2 audit, line 712) demands a falsifiable test that vertical-scope rules contain no traceable tenant identifier — extended in R3 to cover the value classifier (§2.7.2). Three concrete tests:

```
TEST INV-05a: Cross-tenant promotion strips IDs (R2 carry-forward)
  Setup:
    Insert rule_candidate in t_A's DB: conditions_hash=H, source_case_run_ids=[cr_A1,cr_A2].
    Insert rule_candidate in t_B's DB: conditions_hash=H, source_case_run_ids=[cr_B1].
    Run aggregation; reviewer promotes the vertical aggregate.

  Verify:
    1. vertical_candidate_aggregates schema has no tenant_id column.
    2. Aggregate row has no source_correction_ids, no source_case_run_ids.
    3. Resulting validated_rules row (scope='vertical') has tenant_id IS NULL,
       promoted_from_candidate_id IS NULL, promoted_from_aggregate_id NOT NULL.
    4. Serialize the resulting validated_rule + the aggregate to JSON; assert the strings
       contain NONE of: "t_A", "t_B", "cr_A1", "cr_A2", "cr_B1", any correction ID from
       those tenants.
    5. As t_C (no contribution), the resolver returns the vertical rule with no traceability.

TEST INV-05b (R3): Value-classifier — customer name in conditions_canonical
  Setup:
    Insert rule_candidate in t_A's DB:
      conditions_canonical: [{field:"client_name", operator:"=", value:"ABC Realty"}],
      recommended_action: "hold_and_request_more_info".
    Run aggregation.

  Verify:
    1. NO row in vertical_candidate_aggregates contains the string "ABC Realty"
       anywhere in its serialized JSON (conditions_canonical, recommended_action,
       any other column).
    2. A row in quarantined_aggregates exists with conditions_redacted of the form
       [{field:"client_name", operator:"=", value:"<quarantined:freetext>"}].
    3. Reviewer cannot promote from quarantined_aggregates without first either
       (a) extending workflow_templates.condition_value_enums["client_name"] to
       admit "ABC Realty" (the reviewer is implicitly accepting the value as
       generalizable across tenants), or (b) discarding the quarantined entry.
    4. After quarantine review (option a): re-run aggregation. The row now appears in
       vertical_candidate_aggregates (because "ABC Realty" is now an enum value).
       Note: this means a reviewer DOES choose, for an open enum, to allow a value
       to cross the boundary. The decision is reviewer-explicit and audit-logged
       (event_type='aggregate_quarantined' on entry; the "release" is audit-logged
       as a workflow_templates change with the reviewer_id).

TEST INV-05c (R3): Value-classifier — email in conditions_canonical auto-redacts
  Setup:
    Insert rule_candidate with conditions_canonical:
      [{field:"contact_email", operator:"=", value:"alice@abcrealty.com"}].
    Run aggregation.

  Verify:
    1. vertical_candidate_aggregates row exists (the email matched the email regex,
       was auto-redacted, so the tuple PASSES the classifier).
    2. The row's conditions_canonical contains
       [{field:"contact_email", operator:"=", value:"<email>"}] — verbatim redaction.
    3. The string "alice@abcrealty.com" does not appear anywhere in the aggregate row.

TEST INV-05d (R3): Standing audit
  Run hourly: scan vertical_candidate_aggregates for any value containing characters
  outside the allowed redacted-token set (`[a-zA-Z0-9_<>]`) other than for fields
  whose enum allowlist permits the character set. Any violation is an alert.
```

### 13.6 Storage-layer immutability contract test (UE-04 / C8)

```
TEST AUDIT-IMMUTABILITY:
  Connect to control-plane DB assuming role 'audit_writer'.
  Insert a known audit_events row with id='ae_test_001'.
  Attempt: UPDATE audit_events SET payload = '{}' WHERE id = 'ae_test_001';
    Assert: database raises permission denied OR trigger exception.
    Assert: no application-layer mock; the error originates from PostgreSQL.
  Attempt: DELETE FROM audit_events WHERE id = 'ae_test_001';
    Assert: same.
  Attempt: TRUNCATE audit_events;
    Assert: same.
  Verify: the row remains queryable as inserted.
```

---

## 14. Decisions & Rationale (Round 3 cumulative)

| Decision | Choice | Rationale |
|---|---|---|
| **Rule physical location (RESOLVED-C1; locked R3 — DA dissent noted)** | All `validated_rules` (every scope) in unified control-plane DB with RLS (locked DDL §2.8.1). `rule_candidates` and `corrections` remain per-tenant DB. | Per moderator R3 brief — ctrl-plane adopts unified DDL. DA dissent (R2 §C, line 762) preserved as gating question §2.0. |
| **SkillVersion / rule snapshot identity (RESOLVED-C2)** | `skill_version_id == rule_snapshot_id`; one name; pull at workflow-start (RESOLVED-C11). | DA model C: system-prompt context fetched at workflow start, pinned for the run. |
| **Hermes manifest field names (RESOLVED-C14, R3)** | Manifest names `conditions_canonical/recommended_action` hold (schema column names). Exec-plane projects locally to `if_conditions/then_action` at prompt-injection time. | Schema column names are wire format; presentation projection is exec-plane local. |
| **Audit storage topology (RESOLVED-AUDIT-TOPOLOGY-R4)** | Single authoritative `audit_events` in control-plane DB. Per-tenant `audit_events_outbox` (renamed from R3 "local audit_events") is a transactional durable buffer; never a read source. MCP synchronous reads hit control-plane DB over mTLS. | Eliminates buffer-staleness failure mode; collapses dual-read topology. Latency tradeoff explicit (OPEN-AUDIT-LATENCY-R4). |
| **AuditEvent immutability (RESOLVED-C8)** | Storage-layer: `audit_writer` INSERT-only role + BEFORE UPDATE/DELETE/TRUNCATE trigger; applies at both stores. | Application-convention immutability is insufficient (DA UE-04). |
| **MCP read role (RESOLVED-MCP-SNAPSHOT + R4 update)** | `mcp_audit_reader` Postgres role with SELECT on **control-plane** `audit_events` over mTLS, scoped via RLS to `event_type='approval_received' AND tenant_id=current_setting('app.current_tenant')`. | Single store; minimum-grant; aligned with ctrl-plane §9.3. |
| **`mcp_read_snapshot` artifact_type (RESOLVED-MCP-SNAPSHOT, R3)** | Added to `artifacts.artifact_type` enum; original-run reads stored for replay determinism. | Eliminates time-of-call non-determinism on read MCP calls. |
| **Cross-tenant promotion PII strip (RESOLVED-N3, R3)** | Concrete value classifier (boolean/number/enum PASS; email/phone/URL/date REDACT to type token; free-text QUARANTINE). `quarantined_aggregates` table for reviewer normalization. | Closes UE-LL-1 (R2 audit). Tenant condition values like "ABC Realty" cannot reach cross-tenant aggregate. |
| **Sandbox-mode encoding (RESOLVED-N1, R3)** | `mode: 'sandbox' \| 'live'` 2-value enum. Autonomy axis (`approval_required` vs `autopilot`) deferred. | Decomposes the 4-state product progression into 2 orthogonal axes; one canonical type across all surfaces. |
| **Audit canonical schema (RESOLVED-AUDIT-CANON, R3)** | Single registry of `event_type` values (lower_snake_case); single writer per event_type; payload base shape with `mode` + `schema_version`; writer-registry contract test. | Makes this spec's §11 the source of truth for the multi-spec audit contract. |
| **Correction writer (RESOLVED-C10)** | Exec-plane `PersistCorrection` activity is sole writer of `corrections` and `correction_received`. | Workflow durability owner. |
| **Parsing pipeline split** | Stage A (operator-ux gateway): button → `condition_hints`. Stage B (this spec, candidate-matcher): canonical conditions + hash. | Clear ownership of message-content vs. canonical extraction. |
| **Candidate matching** | Exact hash match (v1); schema-aware near-match flagged for manual review; no embeddings. | Testable, auditable. |
| **Confidence formula** | Wilson lower bound × recency × scope consistency; thresholds per-tenant configurable. | Conservative; small-N penalty. |
| **Auto-promotion** | Disabled in v1. | Human review at MVP scale. |
| **Scope escalation** | Automatic case → tenant; vertical/default is reviewer-gated. | Tenants cannot accidentally write cross-tenant rules. |
| **Rollback semantics** | Forward-only (new row + pointer); generates new SkillVersion. | Preserves immutable audit trail. |
| **Replay regression contract (RESOLVED-C5)** | Decision-point outcome level; LLM `temperature=0` configured by exec-plane; comparison test owned here. | LLM bytes are non-deterministic; outcomes are the testable contract. |

---

## 15. Open Questions & Conflicts — Round 5 status (final)

### Resolved cumulatively (R1–R5)

| ID | Status | Where |
|---|---|---|
| **C12** (unified `validated_rules` DDL) | RESOLVED — DDL locked; RLS policy locked; contract test named | §2.8.1, §2.8.2, §2.8.3 |
| **C14** (field-name alignment) | RESOLVED — manifest names hold; exec-plane projects locally | §9.3 |
| **AUDIT-CANON** (canonical schema, registry, writer contract) | RESOLVED | §11.2, §11.3, §11.6, §11.7 |
| **MCP-SNAPSHOT** (`mcp_read_snapshot` artifact_type + `mcp_audit_reader` role) | RESOLVED | §2.5, §11.5 |
| **N1** (sandbox-mode canonical encoding) | RESOLVED — `mode: 'sandbox' \| 'live'` enum, all 4 specs | §2.0c |
| **N3** (vertical_candidate_aggregates PII strip) | RESOLVED — value classifier + quarantine + tests CT-LL-6/7 + INV-05a/b/c/d | §2.7.2, §13.5 |
| **N6** (idempotency-key composition) | RESOLVED — ctrl-plane §17 published SHA-256 derivation; consumed here | §3.9, ctrl-plane §17 |
| **C13** (envelope completeness) | RESOLVED — exec-plane §7.4 enumerates 16 fields verbatim; CT-LL-1 owns the test | §3.8, exec-plane §7.4, CT-LL-1 |
| **AUDIT-TOPOLOGY-R4** (R4) | RESOLVED — single store + outbox; MCP reads control-plane over mTLS | §2.0.1, §11.0 |
| **NEW-R3-1** (federated-query single source) | RESOLVED — ctrl-plane §9.3 collapsed to single store | ctrl-plane §9.3 |
| **NEW-R3-2** (gateway audit invariant scope) | RESOLVED — gateway-emit-via-API per op-ux §9.7; gateway events in §11.3 | §11.3, §11.6 |
| **NEW-R3-3** (lower_snake_case normalization) | RESOLVED — all 4 specs ratified | §11.3 |
| **e2e SC-01..SC-08** (R4) | RESOLVED — all 8 fully testable per §13.1.1 | §13.0, §13.1.1 |
| **OPEN-AUDIT-CANON-CONFORMANCE-R4** (R5 close) | RESOLVED-CLOSED — all 7 op-ux pushed event_types in §11.3; channel-agnostic R5 names adopted | §11.3, §11.6 |
| **OUTBOX-SEMANTICS-R5** (R5) | RESOLVED — drain-worker sole reader, at-least-once with idempotent absorption, 24h retention post-drain, dead-letter table, drain-depth alert threshold | §11.0 (R5 formalization) |

### Open after Round 5 — categorized

All remaining items are **non-blocking for development handoff**. Each is tagged with a category indicating who owns it and why it remains open.

| ID | Category | Owner | SLO / Gating question |
|---|---|---|---|
| **OPEN-MEASUREMENT-AUDIT-LATENCY** (was OPEN-AUDIT-LATENCY-R4) | Measurement (post-MVP) | exec-plane / observability | **SLO target: P99 < 50ms** for mTLS Postgres round-trip from MCP server to control-plane `audit_events` on the WRITE_EXTERNAL hot path. **Fallback if exceeded:** revert to per-tenant outbox as MCP read source (R3 design), with explicit drain-lag semantics documented at the MCP gate. |
| **OPEN-DA-DISSENT-C1** | Architectural-revert trigger (post-MVP forensic) | devil's advocate | RLS adequacy against compromised control-plane operator role. Failure-test at §2.0.1 (RLS-bypass attempt / Postgres CVE / app-layer privilege-escalation forensic). **Concrete trigger:** any one of (1) successful cross-tenant `SELECT * FROM validated_rules WHERE tenant_id = '<other>'` under expected RLS, (2) Postgres CVE proving `current_setting('app.current_tenant')` bypass, (3) audit-log evidence of `app.bypass_tenant_check=true` use outside reviewer assignment scope. Run quarterly post-MVP. |
| **OPEN-PRODUCT-INPUT-OBSERVABILITY** (was OPEN-OQ-1) | Product input | exec-plane | Hermes SkillVersion-loaded ack event. Useful for observability, not blocking. Held. |
| **OPEN-IMPLEMENTATION-CTRL-PLANE** (was OPEN-OQ-2) | Implementation | ctrl-plane | Promotion reconciliation when control-plane rule write succeeds but per-tenant candidate status update fails. Ctrl-plane §13.5 names the scheduled task; concrete implementation deferred. |
| **OPEN-IMPLEMENTATION-DA-INTEGRATION-RIG** (was OPEN-OQ-3) | Implementation | devil's advocate | The 13 contract tests in §13.0 require a 4-plane integration rig. DA owns the rig; named-tests-at-boundary are this spec's. |
| **OPEN-PRODUCT-INPUT-ROUTING** (was OPEN-OQ-4) | Product input | product | Surface `add_note` corrections to reviewers, or operator-only? Default behavior: operator-only (`parse_status='manual_review'`, no reviewer routing). |
| **OPEN-IMPLEMENTATION-OPERATOR-UX-R5** (was OPEN-OQ-5) | One-line confirmation | operator-ux | Confirm gateway emits empty `condition_hints` array when `parse_method='button'` and no free text. Default position: empty array; Stage B derives from `decision_point.agent_input` only. |
| **OPEN-LEGAL-INPUT-RETENTION** (was OPEN-OQ-05) | Legal input | product / legal | Confirm 7-year retention satisfies AU/UK/US jurisdictions. Default in §11.5 is 7 years pending sign-off. |

### NEW conflicts in Round 5

**NEW-R5-1 (with ctrl-plane):** Ctrl-plane R4 §18 storage-topology table contains a row for "per-tenant local `audit_events` (write-through cache)" alongside the `audit_events_outbox`. Per RESOLVED-AUDIT-TOPOLOGY-R4 (§2.0.1, §11.0), there is **only one** per-tenant audit-related table — the outbox. The R3-era write-through cache was collapsed in R4. Ctrl-plane R5 should drop the "per-tenant local `audit_events`" row and the `mcp_approval_events` view reference from §18; the outbox is the only per-tenant audit table. Non-blocking; cosmetic peer-spec drift.

---

## 16. Implementation Handoff

This section consolidates everything a build team needs to start implementation against this spec. **Cross-references peer specs at integration points; this spec is internally self-contained for the data-model layer.**

### 16.1 Data-model migrations — dependency order

Apply in the order below. Each step assumes prior steps are complete.

**Phase 0 — Postgres roles and policies (apply once at platform setup, before any DDL):**

1. Roles: `audit_writer` (INSERT only), `audit_reader` (SELECT only), `mcp_audit_reader` (SELECT only with RLS), `drain_worker` (SELECT + UPDATE on outbox `drained` flag), `validated_rules_reader` / `validated_rules_writer` / `validated_rules_admin` (per §2.8).
2. Audit immutability trigger function `audit_events_immutable_trigger()` (per §11.5).

**Phase 1 — Control-plane DB (single shared DB):**

1. `tenants` (§2.1) — must precede everything that references `tenant_id`.
2. `skill_versions` (§2.9) — must precede `workflow_templates` (which references `skill_versions(id)` via `default_skill_version_id` FK).
3. `workflow_templates` (§2.2) — references `skill_versions(id)`; must precede `validated_rules`.
4. `vertical_candidate_aggregates` (§2.7) and `quarantined_aggregates` (§2.7.2) — must precede `validated_rules` (which references `vertical_candidate_aggregates(id)` via `promoted_from_aggregate_id` FK). Review-only access at app layer.
5. `validated_rules` (§2.8.1) + `vr_scope_consistency` CHECK + `vr_no_tenant_provenance_on_shared` CHECK + `vr_read_isolation` / `vr_write_isolation` / `vr_update_isolation` RLS policies (§2.8.2). Mark `validated_rules` `FORCE ROW LEVEL SECURITY`.
6. `audit_events` (§11.2) + `audit_events_immutable_trigger` BEFORE UPDATE OR DELETE OR TRUNCATE (§11.5) + RLS policies `mcp_audit_reader_approval_only`, `audit_reader_select`, `audit_writer_insert` (§11.5) + partitioning by `tenant_id` (LIST) then `occurred_at` (RANGE) monthly.

**Phase 2 — Per-tenant execution-plane DB (per tenant, on provisioning):**

1. `case_runs` (§2.4) including `mode` enum default `'sandbox'` per RESOLVED-N1.
2. `decision_points` (§2.4) — FK to `case_runs`.
3. `artifacts` (§2.5) including `mcp_read_snapshot` artifact_type and supporting columns (`mcp_tool_name`, `mcp_idempotency_key`, `case_replay_role`).
4. `corrections` (§2.6) — `idempotency_key` UNIQUE; FK to `case_runs` and `decision_points`.
5. `rule_candidates` (§2.7) — FK to `corrections` (via `source_correction_ids` array; not enforced as DB FK).
6. `audit_events_outbox` (§2.10, §11.0) + same `audit_events_immutable_trigger` as control-plane store + `audit_writer` INSERT-only and `drain_worker` SELECT+UPDATE(drained) grants.

**Phase 3 — Indexes and partition seeding (after data populates):**

Indexes per each section's DDL block. Initial monthly partition for `audit_events` covering the current month + the next month.

### 16.2 Integration points with peer specs

| Boundary | Direction | This spec's contract | Peer ref |
|---|---|---|---|
| `LoadSkillVersion(tenant_id, workflow_slug, as_of?)` | exec-plane → control-plane | §2.0a resolver query → §9.1 manifest schema | exec-plane §8.2 |
| `PersistCorrection` writer | exec-plane writes `corrections` row + `correction_received` audit | §3.5 (sole writer), §3.3 (mapping), §3.8 (envelope completeness) | exec-plane §7.4 |
| `mcp_audit_reader` mTLS read | exec-plane MCP → control-plane `audit_events` | §11.0 (R5 outbox semantics), §11.5 (role + RLS) | ctrl-plane §9.3 |
| Gateway `POST /internal/audit/events` | gateway → control-plane | §11.3 (event_types), §11.6 (writer registry) | op-ux §9.7 |
| Promotion pipeline (writes `validated_rules` + `skill_versions` + `rule_promoted` audit) | control-plane worker | §7.4 (transaction), §8 (versioning), §9 (SkillVersion lifecycle) | ctrl-plane §13.5 |
| Idempotency-key derivation (SHA-256 per §17 of ctrl-plane) | all | §3.9 (consumer inventory) | ctrl-plane §17 |
| Audit drain `POST /internal/audit/events` (outbox → control-plane store) | exec-plane drain_worker → control-plane | §11.0 R5 semantics | ctrl-plane §9.4 |
| Vertical promotion aggregation | control-plane aggregation job reads per-tenant `rule_candidates` | §2.7 algorithm + §2.7.2 PII strip classifier | ctrl-plane §13.4 |

### 16.3 Test fixtures owned by learning-loop

| Fixture | Purpose | Where consumed |
|---|---|---|
| **PII-classifier corpora** | Adversarial values for the `vertical_candidate_aggregates` strip classifier (per MERGE-9): boolean/number/enum (PASS); email/phone/URL/date (REDACT); free-text and Unicode lookalikes / Base64 / JSON-embedded (QUARANTINE). | CT-LL-6, CT-LL-7, MERGE-9 |
| **Candidate-merge fixtures** | Sequences of corrections with identical/divergent `conditions_hash` arriving in multiple orders (commutativity of MERGE-2). Includes contradiction sequences (MERGE-4) and threshold-floor sequences (MERGE-7). | CT-LL-5, MERGE-2..7 |
| **Replay golden cases** | Per `WorkflowTemplate`: input payload + pinned `skill_version_id` + expected decision-point outcomes. Stored in control-plane fixture store; format matches `case_runs.input_payload`. | CT-LL-12, REPLAY TEST-1..4, MERGE-8 |
| **Audit-immutability negative tests** | UPDATE/DELETE/TRUNCATE attempts as `audit_writer` and as elevated migration role; assert all raise. | CT-LL-8, AUDIT_IMM_01..05 |
| **Writer-registry authorization fixtures** | Each (service, event_type) pair from §11.6 (positive) plus deliberate mismatches (negative). | CT-LL-10, AUDIT-WRITER-REGISTRY |

### 16.4 Pre-commit invariants (build-pipeline-enforced)

The build pipeline MUST enforce these invariants via lints / hooks before any commit lands. Each invariant maps to a contract test in this spec; failure of the lint blocks the merge.

1. **No `validated_rules` INSERT outside the promotion transaction.** Any INSERT in source code that targets `validated_rules` must originate from `ctrl-plane.promotion_pipeline` (per §7.4 atomic transaction). Lint: AST scan for `INSERT INTO validated_rules` outside promotion-pipeline modules.
2. **No `audit_events` row insert without registry lookup.** Every audit-write call site must reference the `(service_id, event_type)` pair against the §11.6 writer registry. Lint: function-call scan asserting `AuditWriter.emit(event_type, …)` is called only with constants in the registry; CT-LL-10 enforces at runtime.
3. **No `corrections` write outside `PersistCorrection`.** Writes to `corrections` must originate from `exec-plane.temporal_worker.PersistCorrection` (per RESOLVED-C10). Lint: AST scan rejecting `INSERT INTO corrections` from any other module.
4. **`event_type` lint matches §11.3 registry.** All `event_type` literal strings in source code must (a) match `^[a-z][a-z0-9_]*$` (lower_snake_case per RESOLVED-NEW-R3-3) and (b) appear in the §11.3 registry. Lint: regex scan + registry membership check.
5. **No `mcp_read_snapshot` artifact written outside an MCP server hook.** The `artifact_type='mcp_read_snapshot'` row may only be inserted by `exec-plane.mcp_server.snapshot_hook` (per §2.5). Lint: AST scan.
6. **`audit_events_outbox` SELECT only by `drain_worker`.** Application code that connects to a per-tenant DB and SELECTs from `audit_events_outbox` must be running as the `drain_worker` role (per INV-AUDIT-OUTBOX-SOLE-READER-R5). Lint: connection-role check at module boundary.
7. **PII-strip classifier default-deny.** The classifier in §2.7.2 must NEVER return PASS for a value that doesn't explicitly match an allowed pattern. Property test MERGE-9 enforces this at runtime; CI must run MERGE-9 on every commit affecting the classifier.

---

*End of Round 5 — 03-correction-loop.md. R5 resolved: OPEN-AUDIT-CANON-CONFORMANCE-R4 closed (channel-agnostic naming aligned with op-ux R4); OUTBOX-SEMANTICS-R5 formalized (drain-worker sole reader, at-least-once with idempotent absorption, 24h retention post-drain). R5 new: NEW-R5-1 (ctrl-plane §18 cosmetic drift, non-blocking). R5 added: §16 Implementation Handoff (migrations, integration points, fixtures, pre-commit invariants). All R1–R5 cumulative status in §14.*
